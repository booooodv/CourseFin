package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"coursefin/internal/catalog"
	"coursefin/internal/media"
)

func (h *Handler) handleItemImage(w http.ResponseWriter, r *http.Request) {
	itemID := normalizeID(pathParam(r, "itemId"))
	imageType := pathParam(r, "imageType")

	maxWidth := queryInt(r, "maxWidth", 0)
	if maxWidth == 0 {
		maxWidth = queryInt(r, "width", 0)
	}
	if maxWidth == 0 {
		maxWidth = queryInt(r, "fillWidth", 0)
	}

	maxHeight := queryInt(r, "maxHeight", 0)
	if maxHeight == 0 {
		maxHeight = queryInt(r, "height", 0)
	}
	if maxHeight == 0 {
		maxHeight = queryInt(r, "fillHeight", 0)
	}

	h.media.ServeImage(w, r, itemID, imageType, maxWidth, maxHeight)
}

func (h *Handler) handlePlaybackInfo(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	ctx := r.Context()
	itemID := normalizeID(pathParam(r, "itemId"))

	lesson, err := h.cat.GetLesson(ctx, itemID)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	mf, err := h.cat.GetMediaFile(ctx, lesson.MediaFileID)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "media file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	streams, err := h.media.ResolveStreams(ctx, mf.ID)
	if err != nil {
		streams = []media.StreamDescriptor{}
	}

	var req PlaybackInfoRequest
	if r.Method == http.MethodPost && r.Body != nil {
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid playback info payload")
			return
		}
	}
	mergePlaybackInfoQuery(r, &req)

	resp := EvaluatePlaybackInfo(lesson, mf, streams, req, ac.Token)
	writeJSON(w, http.StatusOK, resp)
}

func mergePlaybackInfoQuery(r *http.Request, req *PlaybackInfoRequest) {
	if req.UserId == "" {
		req.UserId = queryParam(r, "UserId")
	}
	if req.MediaSourceId == "" {
		req.MediaSourceId = queryParam(r, "MediaSourceId")
	}
	if req.MaxStreamingBitrate == nil {
		req.MaxStreamingBitrate = queryInt64(r, "MaxStreamingBitrate")
	}
	if req.StartTimeTicks == nil {
		req.StartTimeTicks = queryInt64(r, "StartTimeTicks")
	}
	if req.EnableDirectPlay == nil {
		req.EnableDirectPlay = queryBool(r, "EnableDirectPlay")
	}
	if req.EnableDirectStream == nil {
		req.EnableDirectStream = queryBool(r, "EnableDirectStream")
	}
	if req.EnableTranscoding == nil {
		req.EnableTranscoding = queryBool(r, "EnableTranscoding")
	}
	if req.AudioStreamIndex == nil {
		if raw := queryParam(r, "AudioStreamIndex"); raw != "" {
			if idx, err := strconv.Atoi(raw); err == nil {
				req.AudioStreamIndex = &idx
			}
		}
	}
	if req.SubtitleStreamIndex == nil {
		if raw := queryParam(r, "SubtitleStreamIndex"); raw != "" {
			if idx, err := strconv.Atoi(raw); err == nil {
				req.SubtitleStreamIndex = &idx
			}
		}
	}
}

func (h *Handler) handleVideoStream(w http.ResponseWriter, r *http.Request) {
	targetID := normalizeID(queryParam(r, "MediaSourceId"))
	if targetID == "" {
		targetID = normalizeID(pathParam(r, "itemId"))
	}
	h.media.ServeDirectPlay(w, r, targetID)
}

func (h *Handler) handleMasterPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := normalizeID(pathParam(r, "itemId"))
	sessionID := queryParam(r, "PlaySessionId")
	h.media.ServeMasterPlaylist(w, r, itemID, sessionID)
}

func (h *Handler) handleMediaPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := normalizeID(pathParam(r, "itemId"))
	sessionID := queryParam(r, "PlaySessionId")
	h.media.ServeMediaPlaylist(w, r, itemID, sessionID)
}

func (h *Handler) handleHLSSegment(w http.ResponseWriter, r *http.Request) {
	itemID := normalizeID(pathParam(r, "itemId"))
	sessionID := queryParam(r, "PlaySessionId")
	segRaw := strings.TrimSpace(pathParam(r, "segment"))
	lower := strings.ToLower(segRaw)
	if !strings.HasSuffix(lower, ".ts") {
		writeError(w, http.StatusBadRequest, "unsupported HLS segment format")
		return
	}
	idxStr := segRaw[:len(segRaw)-len(".ts")]
	segIdx, err := strconv.Atoi(idxStr)
	if err != nil || segIdx < 0 {
		writeError(w, http.StatusBadRequest, "invalid segment index")
		return
	}
	h.media.ServeHLSSegment(w, r, itemID, sessionID, segIdx)
}

func (h *Handler) handleDeleteActiveEncodings(w http.ResponseWriter, r *http.Request) {
	sessionID := queryParam(r, "PlaySessionId")
	h.media.StopRemuxSession(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSubtitleStream(w http.ResponseWriter, r *http.Request) {
	msID := normalizeID(pathParam(r, "mediaSourceId"))
	if msID == "" || msID == "00000000000000000000000000000000" {
		msID = normalizeID(pathParam(r, "itemId"))
	}
	idxStr := pathParam(r, "index")
	streamIdx, err := strconv.Atoi(idxStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subtitle stream index")
		return
	}
	format := pathParam(r, "format")
	h.media.ServeSubtitle(w, r, msID, streamIdx, format)
}

func (h *Handler) handlePlaybackStart(w http.ResponseWriter, r *http.Request) {
	var req PlaybackStartInfo
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid playback start payload")
			return
		}
	}
	itemID := normalizeID(req.ItemId)
	if itemID == "" {
		itemID = normalizeID(queryParam(r, "ItemId"))
	}
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "ItemId is required")
		return
	}
	if _, err := h.cat.GetLesson(r.Context(), itemID); errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.PositionTicks > 0 {
		if _, err := h.cat.ReportProgress(r.Context(), itemID, req.PositionTicks); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handlePlaybackProgress(w http.ResponseWriter, r *http.Request) {
	var req PlaybackProgressInfo
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid playback progress payload")
			return
		}
	}
	itemID := normalizeID(req.ItemId)
	if itemID == "" {
		itemID = normalizeID(queryParam(r, "ItemId"))
	}
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "ItemId is required")
		return
	}

	_, err := h.cat.ReportProgress(r.Context(), itemID, req.PositionTicks)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handlePlaybackStopped(w http.ResponseWriter, r *http.Request) {
	var req PlaybackStopInfo
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid playback stop payload")
			return
		}
	}
	if req.PlaySessionId != "" {
		h.media.StopRemuxSession(req.PlaySessionId)
	}
	itemID := normalizeID(req.ItemId)
	if itemID == "" {
		itemID = normalizeID(queryParam(r, "ItemId"))
	}
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "ItemId is required")
		return
	}

	if req.PositionTicks != nil {
		_, err := h.cat.ReportProgress(r.Context(), itemID, *req.PositionTicks)
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleMarkPlayed(w http.ResponseWriter, r *http.Request) {
	h.setPlayedHandler(w, r, true)
}

func (h *Handler) handleMarkUnplayed(w http.ResponseWriter, r *http.Request) {
	h.setPlayedHandler(w, r, false)
}

func (h *Handler) setPlayedHandler(w http.ResponseWriter, r *http.Request, played bool) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	ctx := r.Context()
	itemID := normalizeID(pathParam(r, "itemId"))
	ud, err := h.setItemPlayed(ctx, itemID, played)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ud)
}

func (h *Handler) handleGetUserData(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	ctx := r.Context()
	itemID := normalizeID(pathParam(r, "itemId"))
	ident := h.getServerIdentity(ctx)
	dto, err := h.loadSingleItemDTO(ctx, itemID, ident.ID, false, ac.Token)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dto.UserData == nil {
		writeJSON(w, http.StatusOK, UserItemDataDto{Key: itemID})
		return
	}
	writeJSON(w, http.StatusOK, dto.UserData)
}

func (h *Handler) handlePostUserData(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	var req UpdateUserItemDataDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user data payload")
		return
	}

	ctx := r.Context()
	itemID := normalizeID(pathParam(r, "itemId"))

	if req.Played != nil {
		if _, err := h.setItemPlayed(ctx, itemID, *req.Played); errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !*req.Played && req.PlaybackPositionTicks != nil && *req.PlaybackPositionTicks > 0 {
			_, _ = h.cat.ReportProgress(ctx, itemID, *req.PlaybackPositionTicks)
		}
	} else if req.PlaybackPositionTicks != nil {
		if _, err := h.cat.ReportProgress(ctx, itemID, *req.PlaybackPositionTicks); errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	ident := h.getServerIdentity(ctx)
	dto, err := h.loadSingleItemDTO(ctx, itemID, ident.ID, false, ac.Token)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dto.UserData == nil {
		writeJSON(w, http.StatusOK, UserItemDataDto{Key: itemID})
		return
	}
	writeJSON(w, http.StatusOK, dto.UserData)
}

func (h *Handler) setItemPlayed(ctx context.Context, itemID string, played bool) (UserItemDataDto, error) {
	prog, err := h.cat.SetPlayed(ctx, itemID, played)
	if err == nil {
		var lp *string
		if prog.LastPlayedAt != "" {
			s := prog.LastPlayedAt
			lp = &s
		}
		pct := 0.0
		if prog.Played {
			pct = 100.0
		}
		return UserItemDataDto{
			PlaybackPositionTicks: prog.PositionTicks,
			PlayCount:             prog.PlayCount,
			Played:                prog.Played,
			LastPlayedDate:        lp,
			PlayedPercentage:      pct,
			Key:                   prog.LessonID,
		}, nil
	}
	if !errors.Is(err, catalog.ErrNotFound) {
		return UserItemDataDto{}, err
	}

	kind, kerr := h.detectItemKind(ctx, itemID)
	if kerr != nil {
		return UserItemDataDto{}, kerr
	}
	switch kind {
	case "chapter":
		ch, cerr := h.cat.GetChapter(ctx, itemID)
		if cerr != nil {
			return UserItemDataDto{}, cerr
		}
		for _, l := range ch.Lessons {
			if _, serr := h.cat.SetPlayed(ctx, l.ID, played); serr != nil {
				return UserItemDataDto{}, serr
			}
		}
		updated, cerr := h.cat.GetChapter(ctx, itemID)
		if cerr != nil {
			return UserItemDataDto{}, cerr
		}
		unplayed := updated.UnplayedItemCount
		pc := 0
		if updated.Played {
			pc = 1
		}
		return UserItemDataDto{
			Played:            updated.Played,
			PlayCount:         pc,
			UnplayedItemCount: &unplayed,
			PlayedPercentage:  updated.PlayedPercentage,
			Key:               updated.ID,
		}, nil
	case "course":
		cd, cerr := h.cat.GetCourseDetail(ctx, itemID)
		if cerr != nil {
			return UserItemDataDto{}, cerr
		}
		for _, ch := range cd.Chapters {
			for _, l := range ch.Lessons {
				if _, serr := h.cat.SetPlayed(ctx, l.ID, played); serr != nil {
					return UserItemDataDto{}, serr
				}
			}
		}
		updated, cerr := h.cat.GetCourseDetail(ctx, itemID)
		if cerr != nil {
			return UserItemDataDto{}, cerr
		}
		unplayed := updated.UnplayedItemCount
		pc := 0
		if updated.Played {
			pc = 1
		}
		return UserItemDataDto{
			Played:            updated.Played,
			PlayCount:         pc,
			UnplayedItemCount: &unplayed,
			PlayedPercentage:  updated.PlayedPercentage,
			Key:               updated.ID,
		}, nil
	default:
		return UserItemDataDto{}, catalog.ErrNotFound
	}
}
