package jellyfin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"coursefin/internal/catalog"
	"coursefin/internal/id"
	"coursefin/internal/media"
)

func (h *Handler) handleUserViews(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	libs, err := h.cat.ListLibraries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ident := h.getServerIdentity(r.Context())
	items := make([]BaseItemDto, 0, len(libs))
	for _, lib := range libs {
		items = append(items, mapLibraryDTO(lib, ident.ID))
	}
	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            items,
		TotalRecordCount: len(items),
		StartIndex:       0,
	})
}

func (h *Handler) handleItems(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	ctx := r.Context()
	ident := h.getServerIdentity(ctx)

	ids := parseCSVList(queryParam(r, "Ids"))
	for i := range ids {
		ids[i] = normalizeID(ids[i])
	}
	parentID := ""
	if raw := queryParam(r, "ParentId"); raw != "" {
		parentID = normalizeID(raw)
	}
	includeTypes := parseTypeSet(queryParam(r, "IncludeItemTypes"))
	excludeTypes := parseTypeSet(queryParam(r, "ExcludeItemTypes"))
	recursive := false
	if rb := queryBool(r, "Recursive"); rb != nil {
		recursive = *rb
	}

	tagsFilter := parseMultiFilter(queryParam(r, "Tags"))
	genresFilter := parseMultiFilter(queryParam(r, "Genres"))
	searchTerm := strings.TrimSpace(queryParam(r, "SearchTerm"))
	filtersSet := parseTypeSet(queryParam(r, "Filters"))
	isPlayedParam := queryBool(r, "IsPlayed")
	isResumableParam := queryBool(r, "IsResumable")
	fieldsSet := parseTypeSet(queryParam(r, "Fields"))
	includeMediaSources := fieldsSet["mediasources"] || fieldsSet["mediastreams"]

	var filterPlayed *bool
	if filtersSet["isplayed"] || (isPlayedParam != nil && *isPlayedParam) {
		v := true
		filterPlayed = &v
	} else if filtersSet["isunplayed"] || (isPlayedParam != nil && !*isPlayedParam) {
		v := false
		filterPlayed = &v
	}
	filterResumable := filtersSet["isresumable"] || (isResumableParam != nil && *isResumableParam)

	wantType := func(t string) bool {
		tl := strings.ToLower(t)
		if excludeTypes[tl] {
			return false
		}
		if len(includeTypes) == 0 {
			return true
		}
		return includeTypes[tl]
	}
	explicitType := func(t string) bool {
		tl := strings.ToLower(t)
		if excludeTypes[tl] {
			return false
		}
		return includeTypes[tl]
	}

	tagMap, err := h.loadTagIDMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tagIDsRaw := parseMultiFilter(queryParam(r, "TagIds")); len(tagIDsRaw) > 0 {
		tagsFilter = append(tagsFilter, resolveTagIDsToNames(tagIDsRaw, tagMap)...)
	}
	if genreIDsRaw := parseMultiFilter(queryParam(r, "GenreIds")); len(genreIDsRaw) > 0 {
		genresFilter = append(genresFilter, resolveTagIDsToNames(genreIDsRaw, tagMap)...)
	}
	courseImgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var items []BaseItemDto

	if len(ids) > 0 {
		idSet := make(map[string]bool, len(ids))
		for _, itemID := range ids {
			idSet[itemID] = true
		}
		if wantType("CollectionFolder") {
			libs, err := h.cat.ListLibraries(ctx)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, lib := range libs {
				if idSet[lib.ID] {
					items = append(items, mapLibraryDTO(lib, ident.ID))
				}
			}
		}
		if wantType("Series") {
			courses, err := h.cat.ListCourses(ctx, catalog.CourseFilter{IDs: ids})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, c := range courses {
				items = append(items, mapCourseDTO(c, ident.ID, tagMap, courseImgMap))
			}
		}
		if wantType("Season") || wantType("Episode") {
			for _, itemID := range ids {
				if wantType("Season") {
					if ch, err := h.cat.GetChapter(ctx, itemID); err == nil {
						if cd, cerr := h.cat.GetCourseDetail(ctx, ch.CourseID); cerr == nil {
							items = append(items, mapChapterDTO(*ch, cd.CourseSummary, ident.ID, courseImgMap))
						}
						continue
					}
				}
				if wantType("Episode") {
					if ld, err := h.cat.GetLesson(ctx, itemID); err == nil {
						ep, err := h.buildEpisodeDTOFromLesson(ctx, ld, ident.ID, courseImgMap, includeMediaSources, ac.Token)
						if err != nil {
							writeError(w, http.StatusInternalServerError, err.Error())
							return
						}
						items = append(items, ep)
					}
				}
			}
		}
	} else if parentID != "" {
		kind, err := h.detectItemKind(ctx, parentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		switch kind {
		case "library":
			if wantType("Series") {
				courses, err := h.cat.ListCourses(ctx, catalog.CourseFilter{LibraryID: parentID})
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				for _, c := range courses {
					items = append(items, mapCourseDTO(c, ident.ID, tagMap, courseImgMap))
				}
			}
			if recursive && explicitType("Season") {
				chapters, err := h.queryChapters(ctx, parentID, "")
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				items = append(items, chapters...)
			}
			wantEpisodes := recursive && (explicitType("Episode") || (len(includeTypes) == 0 && !excludeTypes["episode"] && (searchTerm != "" || filterResumable || filterPlayed != nil)))
			if wantEpisodes {
				eps, err := h.queryLessons(ctx, parentID, "", "", ident.ID, courseImgMap, includeMediaSources, ac.Token)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				items = append(items, eps...)
			}
		case "course":
			cd, err := h.cat.GetCourseDetail(ctx, parentID)
			if err != nil && !errors.Is(err, catalog.ErrNotFound) {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if cd != nil {
				wantSeasons := explicitType("Season") || (len(includeTypes) == 0 && !recursive && !excludeTypes["season"])
				wantEpisodes := explicitType("Episode") || (len(includeTypes) == 0 && recursive && !excludeTypes["episode"])
				if wantSeasons {
					for _, ch := range cd.Chapters {
						items = append(items, mapChapterDTO(ch, cd.CourseSummary, ident.ID, courseImgMap))
					}
				}
				if wantEpisodes {
					for _, ch := range cd.Chapters {
						for i := range ch.Lessons {
							ep := mapLessonDTO(ch.Lessons[i], cd.CourseSummary, ch.Title, ch.SortOrder, ident.ID, courseImgMap)
							if includeMediaSources {
								_ = h.attachMediaSources(ctx, &ep, &ch.Lessons[i], ac.Token)
							}
							items = append(items, ep)
						}
					}
				}
			}
		case "chapter":
			if wantType("Episode") {
				eps, err := h.queryLessons(ctx, "", "", parentID, ident.ID, courseImgMap, includeMediaSources, ac.Token)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				items = append(items, eps...)
			}
		}
	} else {
		if len(includeTypes) == 0 && !recursive {
			if wantType("CollectionFolder") {
				libs, err := h.cat.ListLibraries(ctx)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				for _, lib := range libs {
					items = append(items, mapLibraryDTO(lib, ident.ID))
				}
			}
		} else {
			if explicitType("CollectionFolder") {
				libs, err := h.cat.ListLibraries(ctx)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				for _, lib := range libs {
					items = append(items, mapLibraryDTO(lib, ident.ID))
				}
			}
			if wantType("Series") {
				courses, err := h.cat.ListCourses(ctx, catalog.CourseFilter{})
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				for _, c := range courses {
					items = append(items, mapCourseDTO(c, ident.ID, tagMap, courseImgMap))
				}
			}
			if explicitType("Season") {
				chapters, err := h.queryChapters(ctx, "", "")
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				items = append(items, chapters...)
			}
			wantEpisodes := explicitType("Episode") || (len(includeTypes) == 0 && recursive && !excludeTypes["episode"] && (searchTerm != "" || filterResumable || filterPlayed != nil))
			if wantEpisodes {
				eps, err := h.queryLessons(ctx, "", "", "", ident.ID, courseImgMap, includeMediaSources, ac.Token)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				items = append(items, eps...)
			}
		}
	}

	filtered := filterItems(items, tagsFilter, genresFilter, searchTerm, filterPlayed, filterResumable)
	sortItems(filtered, queryParam(r, "SortBy"), queryParam(r, "SortOrder"))

	startIndex := queryInt(r, "StartIndex", 0)
	limit := queryInt(r, "Limit", 0)
	total := len(filtered)
	paged := paginateItems(filtered, startIndex, limit)

	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            paged,
		TotalRecordCount: total,
		StartIndex:       startIndex,
	})
}

func (h *Handler) handleGetItem(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	ctx := r.Context()
	itemID := normalizeID(pathParam(r, "itemId"))
	ident := h.getServerIdentity(ctx)

	dto, err := h.loadSingleItemDTO(ctx, itemID, ident.ID, true, ac.Token)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (h *Handler) handleResume(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	ctx := r.Context()
	ident := h.getServerIdentity(ctx)
	startIndex := queryInt(r, "StartIndex", 0)
	limit := queryInt(r, "Limit", 20)
	parentID := ""
	if raw := queryParam(r, "ParentId"); raw != "" {
		parentID = normalizeID(raw)
	}

	fetchLimit := limit + startIndex
	if parentID != "" || limit <= 0 {
		fetchLimit = 1000
	}
	lessons, err := h.cat.GetResumeLessons(ctx, fetchLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	courseImgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	fieldsSet := parseTypeSet(queryParam(r, "Fields"))
	includeMediaSources := fieldsSet["mediasources"] || fieldsSet["mediastreams"]

	items := make([]BaseItemDto, 0, len(lessons))
	for _, l := range lessons {
		if parentID != "" && l.LibraryID != parentID && l.CourseID != parentID && l.ChapterID != parentID {
			continue
		}
		dto := mapLessonWithContextDTO(l, ident.ID, courseImgMap)
		if includeMediaSources {
			_ = h.attachMediaSources(ctx, &dto, &l.LessonDetail, ac.Token)
		}
		items = append(items, dto)
	}

	total := len(items)
	paged := paginateItems(items, startIndex, limit)
	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            paged,
		TotalRecordCount: total,
		StartIndex:       startIndex,
	})
}

func (h *Handler) handleLatest(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	ctx := r.Context()
	ident := h.getServerIdentity(ctx)
	limit := queryInt(r, "Limit", 20)
	if limit <= 0 {
		limit = 20
	}
	parentID := ""
	if raw := queryParam(r, "ParentId"); raw != "" {
		parentID = normalizeID(raw)
	}
	includeTypes := parseTypeSet(queryParam(r, "IncludeItemTypes"))

	courseImgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if includeTypes["episode"] && !includeTypes["series"] {
		eps, err := h.queryLessons(ctx, parentID, "", "", ident.ID, courseImgMap, false, ac.Token)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if isPlayed := queryBool(r, "IsPlayed"); isPlayed != nil {
			eps = filterItems(eps, nil, nil, "", isPlayed, false)
		}
		sortItems(eps, "DateCreated", "Descending")
		if len(eps) > limit {
			eps = eps[:limit]
		}
		writeJSON(w, http.StatusOK, eps)
		return
	}

	isPlayed := queryBool(r, "IsPlayed")
	var courses []catalog.CourseSummary
	if isPlayed != nil {
		courses, err = h.cat.ListCourses(ctx, catalog.CourseFilter{
			LibraryID: parentID,
			IsPlayed:  isPlayed,
			SortBy:    "created_at",
			SortDesc:  true,
			Limit:     limit,
		})
	} else {
		courses, err = h.cat.GetLatestCourses(ctx, parentID, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tagMap, err := h.loadTagIDMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := make([]BaseItemDto, 0, len(courses))
	for _, c := range courses {
		out = append(out, mapCourseDTO(c, ident.ID, tagMap, courseImgMap))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleNextUp(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	ctx := r.Context()
	ident := h.getServerIdentity(ctx)
	startIndex := queryInt(r, "StartIndex", 0)
	limit := queryInt(r, "Limit", 20)
	seriesID := ""
	if raw := queryParam(r, "SeriesId"); raw != "" {
		seriesID = normalizeID(raw)
	}
	parentID := ""
	if raw := queryParam(r, "ParentId"); raw != "" {
		parentID = normalizeID(raw)
	}

	fetchLimit := limit + startIndex
	if seriesID != "" || parentID != "" || limit <= 0 {
		fetchLimit = 1000
	}
	lessons, err := h.cat.GetNextUpLessons(ctx, fetchLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	courseImgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	fieldsSet := parseTypeSet(queryParam(r, "Fields"))
	includeMediaSources := fieldsSet["mediasources"] || fieldsSet["mediastreams"]

	items := make([]BaseItemDto, 0, len(lessons))
	for _, l := range lessons {
		if seriesID != "" && l.CourseID != seriesID {
			continue
		}
		if parentID != "" && l.LibraryID != parentID && l.CourseID != parentID {
			continue
		}
		dto := mapLessonWithContextDTO(l, ident.ID, courseImgMap)
		if includeMediaSources {
			_ = h.attachMediaSources(ctx, &dto, &l.LessonDetail, ac.Token)
		}
		items = append(items, dto)
	}

	total := len(items)
	paged := paginateItems(items, startIndex, limit)
	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            paged,
		TotalRecordCount: total,
		StartIndex:       startIndex,
	})
}

func (h *Handler) handleSeasons(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	seriesID := normalizeID(pathParam(r, "seriesId"))
	cd, err := h.cat.GetCourseDetail(ctx, seriesID)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ident := h.getServerIdentity(ctx)
	courseImgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]BaseItemDto, 0, len(cd.Chapters))
	for _, ch := range cd.Chapters {
		items = append(items, mapChapterDTO(ch, cd.CourseSummary, ident.ID, courseImgMap))
	}

	startIndex := queryInt(r, "StartIndex", 0)
	limit := queryInt(r, "Limit", 0)
	total := len(items)
	paged := paginateItems(items, startIndex, limit)

	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            paged,
		TotalRecordCount: total,
		StartIndex:       startIndex,
	})
}

func (h *Handler) handleEpisodes(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	ctx := r.Context()
	seriesID := normalizeID(pathParam(r, "seriesId"))
	cd, err := h.cat.GetCourseDetail(ctx, seriesID)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ident := h.getServerIdentity(ctx)
	courseImgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	seasonID := ""
	if raw := queryParam(r, "SeasonId"); raw != "" {
		seasonID = normalizeID(raw)
	}
	var seasonNum *int
	if raw := queryParam(r, "Season"); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil {
			seasonNum = &n
		}
	}

	fieldsSet := parseTypeSet(queryParam(r, "Fields"))
	includeMediaSources := len(fieldsSet) == 0 || fieldsSet["mediasources"] || fieldsSet["mediastreams"]

	items := make([]BaseItemDto, 0, cd.LessonCount)
	for _, ch := range cd.Chapters {
		if seasonID != "" && ch.ID != seasonID {
			continue
		}
		if seasonNum != nil && ch.SortOrder != *seasonNum {
			continue
		}
		for i := range ch.Lessons {
			ep := mapLessonDTO(ch.Lessons[i], cd.CourseSummary, ch.Title, ch.SortOrder, ident.ID, courseImgMap)
			if includeMediaSources {
				_ = h.attachMediaSources(ctx, &ep, &ch.Lessons[i], ac.Token)
			}
			items = append(items, ep)
		}
	}

	startIndex := queryInt(r, "StartIndex", 0)
	limit := queryInt(r, "Limit", 0)
	total := len(items)
	paged := paginateItems(items, startIndex, limit)

	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            paged,
		TotalRecordCount: total,
		StartIndex:       startIndex,
	})
}

func (h *Handler) handleGenres(w http.ResponseWriter, r *http.Request) {
	h.serveTagCollection(w, r, "Genre")
}

func (h *Handler) handleTags(w http.ResponseWriter, r *http.Request) {
	h.serveTagCollection(w, r, "Tag")
}

func (h *Handler) serveTagCollection(w http.ResponseWriter, r *http.Request, itemType string) {
	ctx := r.Context()
	tags, err := h.loadFilteredTags(ctx, queryParam(r, "ParentId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	searchLower := strings.ToLower(strings.TrimSpace(queryParam(r, "SearchTerm")))
	ident := h.getServerIdentity(ctx)

	items := make([]BaseItemDto, 0, len(tags))
	for _, t := range tags {
		if searchLower != "" && !strings.Contains(strings.ToLower(t.Name), searchLower) {
			continue
		}
		cc := t.CourseCount
		items = append(items, BaseItemDto{
			Name:              t.Name,
			SortName:          t.Name,
			ServerId:          ident.ID,
			Id:                t.ID,
			Type:              itemType,
			IsFolder:          false,
			ChildCount:        &cc,
			ImageTags:         map[string]string{},
			BackdropImageTags: []string{},
		})
	}

	startIndex := queryInt(r, "StartIndex", 0)
	limit := queryInt(r, "Limit", 0)
	total := len(items)
	paged := paginateItems(items, startIndex, limit)
	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            paged,
		TotalRecordCount: total,
		StartIndex:       startIndex,
	})
}

func (h *Handler) handleFilters(w http.ResponseWriter, r *http.Request) {
	tags, err := h.loadFilteredTags(r.Context(), queryParam(r, "ParentId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, t.Name)
	}
	writeJSON(w, http.StatusOK, QueryFiltersLegacy{
		Genres:          names,
		Tags:            names,
		OfficialRatings: []string{},
		Years:           []int{},
	})
}

func (h *Handler) handleFilters2(w http.ResponseWriter, r *http.Request) {
	tags, err := h.loadFilteredTags(r.Context(), queryParam(r, "ParentId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	genres := make([]NameGuidPair, 0, len(tags))
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		genres = append(genres, NameGuidPair{Name: t.Name, Id: t.ID})
		names = append(names, t.Name)
	}
	writeJSON(w, http.StatusOK, QueryFilters{
		Genres: genres,
		Tags:   names,
	})
}

func (h *Handler) handleSearchHints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	searchTerm := strings.TrimSpace(queryParam(r, "SearchTerm"))
	searchLower := strings.ToLower(searchTerm)
	parentID := ""
	if raw := queryParam(r, "ParentId"); raw != "" {
		parentID = normalizeID(raw)
	}
	includeTypes := parseTypeSet(queryParam(r, "IncludeItemTypes"))
	excludeTypes := parseTypeSet(queryParam(r, "ExcludeItemTypes"))
	limit := queryInt(r, "Limit", 20)

	wantType := func(t string) bool {
		tl := strings.ToLower(t)
		if excludeTypes[tl] {
			return false
		}
		if len(includeTypes) == 0 {
			return true
		}
		return includeTypes[tl]
	}

	courseImgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hints := make([]SearchHint, 0)

	if wantType("Series") {
		courses, err := h.cat.ListCourses(ctx, catalog.CourseFilter{LibraryID: parentID, Search: searchTerm})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, c := range courses {
			hints = append(hints, SearchHint{
				ItemId:          c.ID,
				Id:              c.ID,
				Name:            c.Title,
				MatchedTerm:     searchTerm,
				RunTimeTicks:    ticksPtr(c.TotalDurationMs),
				Type:            "Series",
				IsFolder:        true,
				PrimaryImageTag: coursePrimaryTag(c, courseImgMap),
			})
		}
	}

	if wantType("Episode") {
		ident := h.getServerIdentity(ctx)
		eps, err := h.queryLessons(ctx, parentID, "", "", ident.ID, courseImgMap, false, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, ep := range eps {
			if searchLower != "" &&
				!strings.Contains(strings.ToLower(ep.Name), searchLower) &&
				!strings.Contains(strings.ToLower(ep.Overview), searchLower) &&
				!strings.Contains(strings.ToLower(ep.SeriesName), searchLower) {
				continue
			}
			hints = append(hints, SearchHint{
				ItemId:            ep.Id,
				Id:                ep.Id,
				Name:              ep.Name,
				MatchedTerm:       searchTerm,
				IndexNumber:       ep.IndexNumber,
				ParentIndexNumber: ep.ParentIndexNumber,
				RunTimeTicks:      ep.RunTimeTicks,
				Type:              "Episode",
				MediaType:         "Video",
				IsFolder:          false,
				Series:            ep.SeriesName,
				SeriesId:          ep.SeriesId,
				SeasonId:          ep.SeasonId,
				PrimaryImageTag:   ep.ImageTags["Primary"],
			})
		}
	}

	total := len(hints)
	if limit > 0 && limit < len(hints) {
		hints = hints[:limit]
	}

	writeJSON(w, http.StatusOK, SearchHintResult{
		SearchHints:      hints,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleSimilar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	itemID := normalizeID(pathParam(r, "itemId"))
	limit := queryInt(r, "Limit", 12)
	ident := h.getServerIdentity(ctx)

	cd, err := h.cat.GetCourseDetail(ctx, itemID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{Items: []BaseItemDto{}, TotalRecordCount: 0, StartIndex: 0})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	allCourses, err := h.cat.ListCourses(ctx, catalog.CourseFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tagMap, err := h.loadTagIDMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	courseImgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	targetTags := make(map[string]bool, len(cd.Tags))
	for _, t := range cd.Tags {
		targetTags[strings.ToLower(t)] = true
	}

	type scored struct {
		course catalog.CourseSummary
		score  int
	}
	var cands []scored
	for _, c := range allCourses {
		if c.ID == cd.ID {
			continue
		}
		score := 0
		if c.LibraryID == cd.LibraryID {
			score++
		}
		for _, t := range c.Tags {
			if targetTags[strings.ToLower(t)] {
				score += 10
			}
		}
		if score > 0 {
			cands = append(cands, scored{course: c, score: score})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return cands[i].score > cands[j].score
	})
	if limit > 0 && limit < len(cands) {
		cands = cands[:limit]
	}

	items := make([]BaseItemDto, 0, len(cands))
	for _, c := range cands {
		items = append(items, mapCourseDTO(c.course, ident.ID, tagMap, courseImgMap))
	}
	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            items,
		TotalRecordCount: len(items),
		StartIndex:       0,
	})
}

func (h *Handler) handleIntros(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, QueryResult[BaseItemDto]{
		Items:            []BaseItemDto{},
		TotalRecordCount: 0,
		StartIndex:       0,
	})
}

func (h *Handler) handleSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []BaseItemDto{})
}

// --- Catalog helpers & DTO mappers ---

func (h *Handler) detectItemKind(ctx context.Context, itemID string) (string, error) {
	var exists int
	if err := h.db.QueryRowContext(ctx, `SELECT 1 FROM libraries WHERE id = ?`, itemID).Scan(&exists); err == nil {
		return "library", nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT 1 FROM courses WHERE id = ?`, itemID).Scan(&exists); err == nil {
		return "course", nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT 1 FROM chapters WHERE id = ?`, itemID).Scan(&exists); err == nil {
		return "chapter", nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT 1 FROM lessons WHERE id = ?`, itemID).Scan(&exists); err == nil {
		return "lesson", nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return "", nil
}

func (h *Handler) loadSingleItemDTO(ctx context.Context, itemID, serverID string, withMediaSources bool, token string) (BaseItemDto, error) {
	kind, err := h.detectItemKind(ctx, itemID)
	if err != nil {
		return BaseItemDto{}, err
	}
	switch kind {
	case "library":
		libs, err := h.cat.ListLibraries(ctx)
		if err != nil {
			return BaseItemDto{}, err
		}
		for _, lib := range libs {
			if lib.ID == itemID {
				return mapLibraryDTO(lib, serverID), nil
			}
		}
	case "course":
		cd, err := h.cat.GetCourseDetail(ctx, itemID)
		if err != nil {
			return BaseItemDto{}, err
		}
		tagMap, err := h.loadTagIDMap(ctx)
		if err != nil {
			return BaseItemDto{}, err
		}
		imgMap, err := h.loadCourseFallbackImageMap(ctx)
		if err != nil {
			return BaseItemDto{}, err
		}
		return mapCourseDTO(cd.CourseSummary, serverID, tagMap, imgMap), nil
	case "chapter":
		ch, err := h.cat.GetChapter(ctx, itemID)
		if err != nil {
			return BaseItemDto{}, err
		}
		cd, err := h.cat.GetCourseDetail(ctx, ch.CourseID)
		if err != nil {
			return BaseItemDto{}, err
		}
		imgMap, err := h.loadCourseFallbackImageMap(ctx)
		if err != nil {
			return BaseItemDto{}, err
		}
		return mapChapterDTO(*ch, cd.CourseSummary, serverID, imgMap), nil
	case "lesson":
		ld, err := h.cat.GetLesson(ctx, itemID)
		if err != nil {
			return BaseItemDto{}, err
		}
		imgMap, err := h.loadCourseFallbackImageMap(ctx)
		if err != nil {
			return BaseItemDto{}, err
		}
		return h.buildEpisodeDTOFromLesson(ctx, ld, serverID, imgMap, withMediaSources, token)
	}
	return BaseItemDto{}, catalog.ErrNotFound
}

func (h *Handler) buildEpisodeDTOFromLesson(ctx context.Context, ld *catalog.LessonDetail, serverID string, imgMap map[string]string, withMediaSources bool, token string) (BaseItemDto, error) {
	cd, err := h.cat.GetCourseDetail(ctx, ld.CourseID)
	if err != nil {
		return BaseItemDto{}, err
	}
	chapTitle := ""
	chapOrder := 1
	for _, ch := range cd.Chapters {
		if ch.ID == ld.ChapterID {
			chapTitle = ch.Title
			chapOrder = ch.SortOrder
			break
		}
	}
	dto := mapLessonDTO(*ld, cd.CourseSummary, chapTitle, chapOrder, serverID, imgMap)
	if withMediaSources {
		_ = h.attachMediaSources(ctx, &dto, ld, token)
	}
	return dto, nil
}

func (h *Handler) attachMediaSources(ctx context.Context, dto *BaseItemDto, ld *catalog.LessonDetail, token string) error {
	mf, err := h.cat.GetMediaFile(ctx, ld.MediaFileID)
	if err != nil {
		return err
	}
	streams, err := h.media.ResolveStreams(ctx, mf.ID)
	if err != nil {
		streams = []media.StreamDescriptor{}
	}
	pb := EvaluatePlaybackInfo(ld, mf, streams, PlaybackInfoRequest{}, token)
	dto.MediaSources = pb.MediaSources
	for _, st := range streams {
		if strings.EqualFold(st.Type, "subtitle") {
			dto.HasSubtitles = true
			break
		}
	}
	return nil
}

func (h *Handler) queryChapters(ctx context.Context, libraryID, courseID string) ([]BaseItemDto, error) {
	ident := h.getServerIdentity(ctx)
	imgMap, err := h.loadCourseFallbackImageMap(ctx)
	if err != nil {
		return nil, err
	}
	courses, err := h.cat.ListCourses(ctx, catalog.CourseFilter{LibraryID: libraryID})
	if err != nil {
		return nil, err
	}
	out := make([]BaseItemDto, 0)
	for _, c := range courses {
		if courseID != "" && c.ID != courseID {
			continue
		}
		cd, err := h.cat.GetCourseDetail(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		for _, ch := range cd.Chapters {
			out = append(out, mapChapterDTO(ch, cd.CourseSummary, ident.ID, imgMap))
		}
	}
	return out, nil
}

func (h *Handler) queryLessons(ctx context.Context, libraryID, courseID, chapterID, serverID string, imgMap map[string]string, withMediaSources bool, token string) ([]BaseItemDto, error) {
	where := []string{"1=1"}
	var args []any
	if libraryID != "" {
		where = append(where, "c.library_id = ?")
		args = append(args, libraryID)
	}
	if courseID != "" {
		where = append(where, "l.course_id = ?")
		args = append(args, courseID)
	}
	if chapterID != "" {
		where = append(where, "l.chapter_id = ?")
		args = append(args, chapterID)
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT l.id, l.course_id, l.chapter_id, l.media_file_id, l.title, l.overview, l.sort_order, l.cover_path,
		       mf.duration_ms, mf.container, mf.video_codec, mf.audio_codec, mf.width, mf.height, mf.bitrate, mf.thumbnail_path, mf.status,
		       COALESCE(pp.position_ticks, 0), COALESCE(pp.played, 0), COALESCE(pp.play_count, 0), COALESCE(pp.last_played_at, ''),
		       l.created_at, l.updated_at,
		       c.title, c.cover_path, c.updated_at, c.library_id, ch.title, ch.sort_order
		FROM lessons l
		JOIN chapters ch ON ch.id = l.chapter_id
		JOIN courses c ON c.id = l.course_id
		JOIN media_files mf ON mf.id = l.media_file_id
		LEFT JOIN playback_progress pp ON pp.lesson_id = l.id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY c.created_at DESC, ch.sort_order ASC, ch.created_at ASC, l.sort_order ASC, l.created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]BaseItemDto, 0)
	var lessons []*catalog.LessonDetail
	for rows.Next() {
		var item catalog.LessonWithContext
		var playedInt int
		if err := rows.Scan(
			&item.ID, &item.CourseID, &item.ChapterID, &item.MediaFileID, &item.Title, &item.Overview, &item.SortOrder, &item.CoverPath,
			&item.DurationMs, &item.Container, &item.VideoCodec, &item.AudioCodec, &item.Width, &item.Height, &item.Bitrate, &item.ThumbnailPath, &item.MediaStatus,
			&item.PositionTicks, &playedInt, &item.PlayCount, &item.LastPlayedAt,
			&item.CreatedAt, &item.UpdatedAt,
			&item.CourseTitle, &item.CourseCoverPath, &item.CourseUpdatedAt, &item.LibraryID, &item.ChapterTitle, &item.ChapterSortOrder,
		); err != nil {
			return nil, err
		}
		item.Played = playedInt != 0
		dto := mapLessonWithContextDTO(item, serverID, imgMap)
		out = append(out, dto)
		ldCopy := item.LessonDetail
		lessons = append(lessons, &ldCopy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if withMediaSources {
		for i := range out {
			_ = h.attachMediaSources(ctx, &out[i], lessons[i], token)
		}
	}
	return out, nil
}

func (h *Handler) loadTagIDMap(ctx context.Context) (map[string]string, error) {
	tags, err := h.cat.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[t.Name] = t.ID
	}
	return m, nil
}

func (h *Handler) loadFilteredTags(ctx context.Context, rawParentID string) ([]catalog.Tag, error) {
	if strings.TrimSpace(rawParentID) == "" {
		return h.cat.ListTags(ctx)
	}
	parentID := normalizeID(rawParentID)
	rows, err := h.db.QueryContext(ctx, `
		SELECT t.id, t.name, COUNT(ct.course_id) AS course_count
		FROM tags t
		JOIN course_tags ct ON ct.tag_id = t.id
		JOIN courses c ON c.id = ct.course_id
		WHERE c.library_id = ?
		GROUP BY t.id
		HAVING COUNT(ct.course_id) > 0
		ORDER BY t.name ASC`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []catalog.Tag
	for rows.Next() {
		var t catalog.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.CourseCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (h *Handler) loadCourseFallbackImageMap(ctx context.Context) (map[string]string, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT l.course_id, COALESCE(NULLIF(l.cover_path, ''), mf.thumbnail_path) AS img
		FROM lessons l
		JOIN chapters ch ON ch.id = l.chapter_id
		JOIN media_files mf ON mf.id = l.media_file_id
		WHERE l.cover_path != '' OR mf.thumbnail_path != ''
		ORDER BY ch.sort_order ASC, ch.created_at ASC, l.sort_order ASC, l.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]string)
	for rows.Next() {
		var cid, img string
		if err := rows.Scan(&cid, &img); err != nil {
			return nil, err
		}
		if _, exists := res[cid]; !exists && strings.TrimSpace(img) != "" {
			res[cid] = img
		}
	}
	return res, rows.Err()
}

func mapLibraryDTO(lib catalog.Library, serverID string) BaseItemDto {
	imageTags := map[string]string{}
	if lib.CoverPath != "" {
		imageTags["Primary"] = makeImageTag(lib.ID, lib.CoverPath, lib.UpdatedAt)
	}
	cc := lib.CourseCount
	return BaseItemDto{
		Name:                 lib.Name,
		SortName:             lib.Name,
		ServerId:             serverID,
		Id:                   lib.ID,
		Type:                 "CollectionFolder",
		CollectionType:       "tvshows",
		IsFolder:             true,
		ChildCount:           &cc,
		DateCreated:          lib.CreatedAt,
		LocationType:         "FileSystem",
		DisplayPreferencesId: lib.ID,
		ImageTags:            imageTags,
		BackdropImageTags:    []string{},
		UserData: &UserItemDataDto{
			Key: lib.ID,
		},
	}
}

func mapCourseDTO(c catalog.CourseSummary, serverID string, tagMap map[string]string, courseImgMap map[string]string) BaseItemDto {
	sortName := c.SortTitle
	if sortName == "" {
		sortName = c.Title
	}
	tags := make([]string, len(c.Tags))
	copy(tags, c.Tags)
	genres := make([]string, len(c.Tags))
	copy(genres, c.Tags)
	genreItems := make([]NameGuidPair, 0, len(c.Tags))
	for _, t := range c.Tags {
		genreItems = append(genreItems, NameGuidPair{Name: t, Id: tagMap[t]})
	}

	var people []BaseItemPerson
	if c.Instructor != "" {
		people = append(people, BaseItemPerson{
			Name: c.Instructor,
			Role: "Instructor",
			Type: "Director",
		})
	}

	imageTags := map[string]string{}
	if tag := coursePrimaryTag(c, courseImgMap); tag != "" {
		imageTags["Primary"] = tag
	}
	backdrops := []string{}
	if c.BackdropPath != "" {
		backdrops = append(backdrops, makeImageTag(c.ID, c.BackdropPath, c.UpdatedAt))
	}

	cc := c.ChapterCount
	rc := c.LessonCount
	unplayed := c.UnplayedItemCount
	playCount := 0
	if c.Played {
		playCount = 1
	}

	return BaseItemDto{
		Name:               c.Title,
		OriginalTitle:      c.Title,
		SortName:           sortName,
		ForcedSortName:     c.SortTitle,
		Overview:           c.Overview,
		ServerId:           serverID,
		Id:                 c.ID,
		ParentId:           c.LibraryID,
		Type:               "Series",
		IsFolder:           true,
		ChildCount:         &cc,
		RecursiveItemCount: &rc,
		RunTimeTicks:       ticksPtr(c.TotalDurationMs),
		DateCreated:        c.CreatedAt,
		Status:             "Ended",
		DisplayOrder:       "Aired",
		LocationType:       "FileSystem",
		Tags:               tags,
		Genres:             genres,
		GenreItems:         genreItems,
		People:             people,
		ImageTags:          imageTags,
		BackdropImageTags:  backdrops,
		UserData: &UserItemDataDto{
			Played:            c.Played,
			PlayCount:         playCount,
			UnplayedItemCount: &unplayed,
			PlayedPercentage:  c.PlayedPercentage,
			Key:               c.ID,
		},
	}
}

func mapChapterDTO(ch catalog.ChapterDetail, course catalog.CourseSummary, serverID string, courseImgMap map[string]string) BaseItemDto {
	idx := ch.SortOrder
	pidx := ch.SortOrder
	cc := ch.LessonCount
	unplayed := ch.UnplayedItemCount
	playCount := 0
	if ch.Played {
		playCount = 1
	}

	imageTags := map[string]string{}
	if tag := coursePrimaryTag(course, courseImgMap); tag != "" {
		imageTags["Primary"] = tag
	} else {
		for _, l := range ch.Lessons {
			if l.CoverPath != "" || l.ThumbnailPath != "" {
				imageTags["Primary"] = makeImageTag(ch.ID, l.CoverPath+l.ThumbnailPath, ch.UpdatedAt)
				break
			}
		}
	}

	return BaseItemDto{
		Name:              ch.Title,
		SortName:          fmt.Sprintf("%04d - %s", ch.SortOrder, ch.Title),
		Overview:          ch.Overview,
		ServerId:          serverID,
		Id:                ch.ID,
		ParentId:          ch.CourseID,
		SeriesId:          ch.CourseID,
		SeriesName:        course.Title,
		Type:              "Season",
		IsFolder:          true,
		IndexNumber:       &idx,
		ParentIndexNumber: &pidx,
		ChildCount:        &cc,
		DateCreated:       ch.CreatedAt,
		LocationType:      "FileSystem",
		ImageTags:         imageTags,
		BackdropImageTags: []string{},
		UserData: &UserItemDataDto{
			Played:            ch.Played,
			PlayCount:         playCount,
			UnplayedItemCount: &unplayed,
			PlayedPercentage:  ch.PlayedPercentage,
			Key:               ch.ID,
		},
	}
}

func mapLessonDTO(l catalog.LessonDetail, course catalog.CourseSummary, chapterTitle string, chapterOrder int, serverID string, courseImgMap map[string]string) BaseItemDto {
	idx := l.SortOrder
	pidx := chapterOrder
	rt := l.DurationMs * ticksPerMillisecond

	imageTags := map[string]string{}
	if l.CoverPath != "" || l.ThumbnailPath != "" {
		imageTags["Primary"] = makeImageTag(l.ID, l.CoverPath+l.ThumbnailPath, l.UpdatedAt)
	}

	var lastPlayedPtr *string
	if l.LastPlayedAt != "" {
		lp := l.LastPlayedAt
		lastPlayedPtr = &lp
	}

	pct := 0.0
	if l.Played {
		pct = 100.0
	} else if l.DurationMs > 0 && l.PositionTicks > 0 {
		pct = float64(l.PositionTicks) * 100.0 / float64(l.DurationMs*ticksPerMillisecond)
		if pct > 100.0 {
			pct = 100.0
		}
	}

	return BaseItemDto{
		Name:                  l.Title,
		SortName:              fmt.Sprintf("%04d-%04d - %s", chapterOrder, l.SortOrder, l.Title),
		Overview:              l.Overview,
		ServerId:              serverID,
		Id:                    l.ID,
		ParentId:              l.ChapterID,
		SeriesId:              l.CourseID,
		SeriesName:            course.Title,
		SeasonId:              l.ChapterID,
		SeasonName:            chapterTitle,
		Type:                  "Episode",
		MediaType:             "Video",
		IsFolder:              false,
		IndexNumber:           &idx,
		ParentIndexNumber:     &pidx,
		RunTimeTicks:          &rt,
		Container:             l.Container,
		Width:                 l.Width,
		Height:                l.Height,
		HasSubtitles:          len(l.Subtitles) > 0,
		DateCreated:           l.CreatedAt,
		LocationType:          "FileSystem",
		ImageTags:             imageTags,
		BackdropImageTags:     []string{},
		SeriesPrimaryImageTag: coursePrimaryTag(course, courseImgMap),
		UserData: &UserItemDataDto{
			PlaybackPositionTicks: l.PositionTicks,
			PlayCount:             l.PlayCount,
			Played:                l.Played,
			LastPlayedDate:        lastPlayedPtr,
			PlayedPercentage:      pct,
			Key:                   l.ID,
		},
	}
}

func mapLessonWithContextDTO(l catalog.LessonWithContext, serverID string, courseImgMap map[string]string) BaseItemDto {
	courseSummary := catalog.CourseSummary{
		ID:        l.CourseID,
		Title:     l.CourseTitle,
		CoverPath: l.CourseCoverPath,
		UpdatedAt: l.CourseUpdatedAt,
	}
	return mapLessonDTO(l.LessonDetail, courseSummary, l.ChapterTitle, l.ChapterSortOrder, serverID, courseImgMap)
}

func coursePrimaryTag(c catalog.CourseSummary, courseImgMap map[string]string) string {
	if c.CoverPath != "" {
		return makeImageTag(c.ID, c.CoverPath, c.UpdatedAt)
	}
	if fallback, ok := courseImgMap[c.ID]; ok && fallback != "" {
		return makeImageTag(c.ID, fallback, c.UpdatedAt)
	}
	return ""
}

func makeImageTag(id, path, updatedAt string) string {
	sum := sha256.Sum256([]byte(id + "|" + path + "|" + updatedAt))
	return hex.EncodeToString(sum[:8])
}

func ticksPtr(ms int64) *int64 {
	v := ms * ticksPerMillisecond
	return &v
}

func parseCSVList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseTypeSet(raw string) map[string]bool {
	set := make(map[string]bool)
	for _, item := range parseCSVList(raw) {
		set[strings.ToLower(item)] = true
	}
	return set
}

func resolveTagIDsToNames(rawIDs []string, tagMap map[string]string) []string {
	idToLowerName := make(map[string]string, len(tagMap))
	for name, tid := range tagMap {
		idToLowerName[tid] = strings.ToLower(name)
	}
	out := make([]string, 0, len(rawIDs))
	for _, raw := range rawIDs {
		norm := raw
		if n, ok := id.Normalize(raw); ok {
			norm = n
		}
		if name, ok := idToLowerName[norm]; ok {
			out = append(out, name)
		} else {
			out = append(out, "__unknown_tag_id__:"+norm)
		}
	}
	return out
}

func parseMultiFilter(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	sep := "|"
	if !strings.Contains(raw, "|") && strings.Contains(raw, ",") {
		sep = ","
	}
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, strings.ToLower(t))
		}
	}
	return out
}

func filterItems(items []BaseItemDto, tagsFilter, genresFilter []string, searchTerm string, filterPlayed *bool, filterResumable bool) []BaseItemDto {
	if len(tagsFilter) == 0 && len(genresFilter) == 0 && searchTerm == "" && filterPlayed == nil && !filterResumable {
		return items
	}
	searchLower := strings.ToLower(searchTerm)
	out := make([]BaseItemDto, 0, len(items))
	for _, item := range items {
		if len(tagsFilter) > 0 && !itemMatchesAnyTag(item.Tags, tagsFilter) {
			continue
		}
		if len(genresFilter) > 0 && !itemMatchesAnyTag(item.Genres, genresFilter) {
			continue
		}
		if searchLower != "" {
			matched := strings.Contains(strings.ToLower(item.Name), searchLower) ||
				strings.Contains(strings.ToLower(item.Overview), searchLower) ||
				strings.Contains(strings.ToLower(item.SeriesName), searchLower)
			if !matched {
				for _, t := range item.Tags {
					if strings.Contains(strings.ToLower(t), searchLower) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		if filterPlayed != nil {
			played := item.UserData != nil && item.UserData.Played
			if played != *filterPlayed {
				continue
			}
		}
		if filterResumable {
			resumable := item.UserData != nil && item.UserData.PlaybackPositionTicks > 0 && !item.UserData.Played
			if !resumable {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func itemMatchesAnyTag(itemTags []string, wanted []string) bool {
	for _, w := range wanted {
		for _, t := range itemTags {
			if strings.ToLower(t) == w {
				return true
			}
		}
	}
	return false
}

func sortItems(items []BaseItemDto, sortBy, sortOrder string) {
	if len(items) <= 1 {
		return
	}
	primary := ""
	if sortBy != "" {
		primary = strings.ToLower(strings.TrimSpace(strings.Split(sortBy, ",")[0]))
	}
	if primary == "" {
		return
	}
	if primary == "random" {
		rand.Shuffle(len(items), func(i, j int) {
			items[i], items[j] = items[j], items[i]
		})
		return
	}

	desc := strings.EqualFold(strings.TrimSpace(sortOrder), "Descending")
	cmpLess := func(a, b BaseItemDto) bool {
		switch primary {
		case "name":
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case "sortname", "seriessortname":
			sa := a.SortName
			if sa == "" {
				sa = a.Name
			}
			sb := b.SortName
			if sb == "" {
				sb = b.Name
			}
			return strings.ToLower(sa) < strings.ToLower(sb)
		case "datecreated", "datelastcontentadded":
			return a.DateCreated < b.DateCreated
		case "dateplayed":
			la, lb := "", ""
			if a.UserData != nil && a.UserData.LastPlayedDate != nil {
				la = *a.UserData.LastPlayedDate
			}
			if b.UserData != nil && b.UserData.LastPlayedDate != nil {
				lb = *b.UserData.LastPlayedDate
			}
			return la < lb
		case "indexnumber":
			pa, pb := 0, 0
			ia, ib := 0, 0
			if a.ParentIndexNumber != nil {
				pa = *a.ParentIndexNumber
			}
			if b.ParentIndexNumber != nil {
				pb = *b.ParentIndexNumber
			}
			if a.IndexNumber != nil {
				ia = *a.IndexNumber
			}
			if b.IndexNumber != nil {
				ib = *b.IndexNumber
			}
			if pa != pb {
				return pa < pb
			}
			return ia < ib
		default:
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if desc {
			return cmpLess(items[j], items[i])
		}
		return cmpLess(items[i], items[j])
	})
}

func paginateItems(items []BaseItemDto, startIndex, limit int) []BaseItemDto {
	if items == nil {
		return []BaseItemDto{}
	}
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex >= len(items) {
		return []BaseItemDto{}
	}
	sliced := items[startIndex:]
	if limit > 0 && limit < len(sliced) {
		sliced = sliced[:limit]
	}
	return sliced
}
