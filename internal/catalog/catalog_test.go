package catalog_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"coursefin/internal/catalog"
	"coursefin/internal/db"
	"coursefin/internal/id"
)

func setupTestCatalog(t *testing.T) (*catalog.Service, *sql.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	svc := catalog.NewService(conn, dataDir)
	return svc, conn, dataDir
}

func seedMediaFile(t *testing.T, conn *sql.DB, sourceID, relPath string, durationMs int64) string {
	t.Helper()
	mfID := id.New()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(`
		INSERT INTO media_files (
			id, source_id, relative_path, file_name, file_size, mod_time, status,
			duration_ms, container, video_codec, audio_codec, width, height, bitrate,
			streams_json, thumbnail_path, probed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, 1024, ?, 'active', ?, 'mp4', 'h264', 'aac', 1920, 1080, 1000000, '[]', ?, ?, ?, ?)`,
		mfID, sourceID, relPath, filepath.Base(relPath), now, durationMs, mfID+".jpg", now, now, now,
	)
	if err != nil {
		t.Fatalf("seed media_file %s: %v", relPath, err)
	}
	return mfID
}

func TestNaturalCompareMixedChineseAndDecimal(t *testing.T) {
	cases := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "chinese_digits_arabic",
			input: []string{"第10讲", "第2讲", "第1讲"},
			want:  []string{"第1讲", "第2讲", "第10讲"},
		},
		{
			name:  "chinese_numerals",
			input: []string{"第十章", "第二章", "第一章", "第十二章", "第二十章"},
			want:  []string{"第一章", "第二章", "第十章", "第十二章", "第二十章"},
		},
		{
			name:  "dotted_sections",
			input: []string{"1.10 进阶", "1.2 基础", "1.1 导论"},
			want:  []string{"1.1 导论", "1.2 基础", "1.10 进阶"},
		},
		{
			name:  "zero_padded_prefixes",
			input: []string{"10_end", "02_setup", "01_intro"},
			want:  []string{"01_intro", "02_setup", "10_end"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := append([]string(nil), tc.input...)
			sort.Slice(got, func(i, j int) bool {
				return catalog.NaturalCompare(got[i], got[j]) < 0
			})
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}

	if catalog.NaturalCompare("第1讲", "第1讲") != 0 {
		t.Fatalf("expected equal strings to compare 0")
	}
	if catalog.NaturalCompare("第2讲", "第10讲") >= 0 {
		t.Fatalf("expected 第2讲 < 第10讲")
	}
	if catalog.NaturalCompare("第2讲", "第十讲") >= 0 {
		t.Fatalf("expected 第2讲 < 第十讲")
	}
	if catalog.NaturalCompare("第二十章", "第一百零一章") >= 0 {
		t.Fatalf("expected 第二十章 < 第一百零一章")
	}
}

func TestLibrariesTagsFiltersIndexedDirsAndSetPlayed(t *testing.T) {
	ctx := context.Background()
	svc, conn, _ := setupTestCatalog(t)

	// Libraries CRUD + conflict guard
	lib2, err := svc.CreateLibrary(ctx, "Design")
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	lib2, err = svc.UpdateLibrary(ctx, lib2.ID, "UI Design")
	if err != nil {
		t.Fatalf("UpdateLibrary: %v", err)
	}
	if lib2.Name != "UI Design" {
		t.Fatalf("lib2.Name = %q, want UI Design", lib2.Name)
	}

	mediaRoot := t.TempDir()
	src, err := svc.CreateSource(ctx, "NAS", "local", mediaRoot)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	mf1 := seedMediaFile(t, conn, src.ID, "01_Go/01_Intro/a.mp4", 50000)
	mf2 := seedMediaFile(t, conn, src.ID, "01_Go/02_Types/b.mp4", 60000)
	_ = seedMediaFile(t, conn, src.ID, "02_Rust/01.mp4", 70000)

	// ListIndexedDirs at root and nested prefix
	rootDirs, err := svc.ListIndexedDirs(ctx, src.ID, "")
	if err != nil {
		t.Fatalf("ListIndexedDirs root: %v", err)
	}
	if len(rootDirs) != 2 || rootDirs[0].Name != "01_Go" || rootDirs[0].FileCount != 2 || !rootDirs[0].HasSubdirs {
		t.Fatalf("unexpected rootDirs: %+v", rootDirs)
	}
	subDirs, err := svc.ListIndexedDirs(ctx, src.ID, "01_Go")
	if err != nil {
		t.Fatalf("ListIndexedDirs 01_Go: %v", err)
	}
	if len(subDirs) != 2 || subDirs[0].Name != "01_Intro" || subDirs[1].Name != "02_Types" {
		t.Fatalf("unexpected subDirs: %+v", subDirs)
	}

	c, err := svc.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: lib2.ID,
		Title:     "Figma Mastery",
		Tags:      []string{"Design", "Figma"},
	})
	if err != nil {
		t.Fatalf("CreateCourse: %v", err)
	}

	// Deleting non-empty library must return ErrConflict
	if err := svc.DeleteLibrary(ctx, lib2.ID); err == nil {
		t.Fatalf("expected error deleting non-empty library")
	}

	l1, err := svc.AddLesson(ctx, c.Chapters[0].ID, mf1, "Intro")
	if err != nil {
		t.Fatalf("AddLesson mf1: %v", err)
	}
	if _, err := svc.AddLesson(ctx, c.Chapters[0].ID, mf2, "Types"); err != nil {
		t.Fatalf("AddLesson mf2: %v", err)
	}

	// Deleting source referenced by a lesson must fail cleanly
	if err := svc.DeleteSource(ctx, src.ID); err == nil {
		t.Fatalf("expected error deleting source with active lessons")
	}

	// Tag & search filtering
	if err := svc.SetCourseTags(ctx, c.ID, []string{"Design", "UI"}); err != nil {
		t.Fatalf("SetCourseTags: %v", err)
	}
	tags, err := svc.ListTags(ctx)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 active tags, got %+v", tags)
	}

	filtered, err := svc.ListCourses(ctx, catalog.CourseFilter{LibraryID: lib2.ID, Tag: "UI", Search: "Figma"})
	if err != nil {
		t.Fatalf("ListCourses filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != c.ID {
		t.Fatalf("unexpected filtered courses: %+v", filtered)
	}

	// SetPlayed true/false & GetLatestCourses
	prog, err := svc.SetPlayed(ctx, l1.ID, true)
	if err != nil {
		t.Fatalf("SetPlayed true: %v", err)
	}
	if !prog.Played || prog.PlayCount != 1 {
		t.Fatalf("unexpected progress after SetPlayed(true): %+v", prog)
	}
	nextUp, err := svc.GetNextUpLessons(ctx, 10)
	if err != nil {
		t.Fatalf("GetNextUpLessons: %v", err)
	}
	if len(nextUp) != 1 || nextUp[0].Title != "Types" {
		t.Fatalf("expected Types in NextUp, got %+v", nextUp)
	}

	prog, err = svc.SetPlayed(ctx, l1.ID, false)
	if err != nil {
		t.Fatalf("SetPlayed false: %v", err)
	}
	if prog.Played || prog.PlayCount != 0 {
		t.Fatalf("unexpected progress after SetPlayed(false): %+v", prog)
	}

	latest, err := svc.GetLatestCourses(ctx, lib2.ID, 5)
	if err != nil {
		t.Fatalf("GetLatestCourses: %v", err)
	}
	if len(latest) != 1 || latest[0].ID != c.ID {
		t.Fatalf("unexpected latest courses: %+v", latest)
	}
}

func TestCourseDecouplingAndMultiCourseReuse(t *testing.T) {
	ctx := context.Background()
	svc, conn, _ := setupTestCatalog(t)

	mediaRoot := t.TempDir()
	src, err := svc.CreateSource(ctx, "Courses", "local", mediaRoot)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	mf1 := seedMediaFile(t, conn, src.ID, "shared/01_intro.mp4", 60000)
	mf2 := seedMediaFile(t, conn, src.ID, "shared/02_core.mp4", 90000)

	libs, err := svc.ListLibraries(ctx)
	if err != nil || len(libs) == 0 {
		t.Fatalf("ListLibraries: %v (len=%d)", err, len(libs))
	}
	libID := libs[0].ID

	c1, err := svc.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: libID,
		Title:     "Course A",
		Tags:      []string{"Go", "Backend"},
	})
	if err != nil {
		t.Fatalf("CreateCourse A: %v", err)
	}
	if len(c1.Chapters) != 1 || c1.Chapters[0].Title != "正片" || c1.Chapters[0].SortOrder != 1 {
		t.Fatalf("expected default chapter 正片 with sort_order=1, got %+v", c1.Chapters)
	}

	c2, err := svc.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: libID,
		Title:     "Course B",
	})
	if err != nil {
		t.Fatalf("CreateCourse B: %v", err)
	}

	ch1A := c1.Chapters[0].ID
	ch2A, err := svc.CreateChapter(ctx, c1.ID, "进阶")
	if err != nil {
		t.Fatalf("CreateChapter: %v", err)
	}

	l1A, err := svc.AddLesson(ctx, ch1A, mf1, "Intro in A")
	if err != nil {
		t.Fatalf("AddLesson l1A: %v", err)
	}
	l2A, err := svc.AddLesson(ctx, ch1A, mf2, "Core in A")
	if err != nil {
		t.Fatalf("AddLesson l2A: %v", err)
	}

	// Reorder lessons atomically across chapters (move l2A into ch2A, keep l1A in ch1A).
	if err := svc.ReorderLessons(ctx, c1.ID, []catalog.LessonPlacement{
		{LessonID: l2A.ID, ChapterID: ch2A.ID, SortOrder: 1},
		{LessonID: l1A.ID, ChapterID: ch1A, SortOrder: 1},
	}); err != nil {
		t.Fatalf("ReorderLessons: %v", err)
	}
	if err := svc.ReorderChapters(ctx, c1.ID, []string{ch2A.ID, ch1A}); err != nil {
		t.Fatalf("ReorderChapters: %v", err)
	}

	c1Detail, err := svc.GetCourseDetail(ctx, c1.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail c1: %v", err)
	}
	if c1Detail.Chapters[0].ID != ch2A.ID || len(c1Detail.Chapters[0].Lessons) != 1 || c1Detail.Chapters[0].Lessons[0].ID != l2A.ID {
		t.Fatalf("unexpected chapter/lesson reorder result: %+v", c1Detail.Chapters)
	}

	// Add the exact same media files into Course B.
	if _, err := svc.AddLesson(ctx, c2.Chapters[0].ID, mf1, "Intro in B"); err != nil {
		t.Fatalf("AddLesson c2 mf1: %v", err)
	}
	if _, err := svc.AddLesson(ctx, c2.Chapters[0].ID, mf2, "Core in B"); err != nil {
		t.Fatalf("AddLesson c2 mf2: %v", err)
	}

	// Delete Course A; Course B and media_files must remain intact.
	if err := svc.DeleteCourse(ctx, c1.ID); err != nil {
		t.Fatalf("DeleteCourse c1: %v", err)
	}

	c2Detail, err := svc.GetCourseDetail(ctx, c2.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail c2 after deleting c1: %v", err)
	}
	if len(c2Detail.Chapters) != 1 || len(c2Detail.Chapters[0].Lessons) != 2 {
		t.Fatalf("expected Course B to keep its 2 lessons, got %+v", c2Detail.Chapters)
	}

	files, err := svc.ListMediaFiles(ctx, src.ID, "", "", "active")
	if err != nil {
		t.Fatalf("ListMediaFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 media_files untouched, got %d", len(files))
	}
}

func TestDirectoryImportPreviewDeepNestingAndIdempotentSync(t *testing.T) {
	ctx := context.Background()
	svc, conn, dataDir := setupTestCatalog(t)

	mediaRoot := t.TempDir()
	courseDir := filepath.Join(mediaRoot, "GoCourse")
	for _, sub := range []string{
		filepath.Join(courseDir, "01_Basics"),
		filepath.Join(courseDir, "02_Core", "Deep"),
		filepath.Join(courseDir, "10_Deploy"),
	} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	coverBytes := []byte("fake-jpeg-cover-bytes")
	if err := os.WriteFile(filepath.Join(courseDir, "cover.jpg"), coverBytes, 0o644); err != nil {
		t.Fatalf("write cover.jpg: %v", err)
	}

	src, err := svc.CreateSource(ctx, "MainNAS", "local", mediaRoot)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	seedMediaFile(t, conn, src.ID, "GoCourse/00_trailer.mp4", 30000)
	seedMediaFile(t, conn, src.ID, "GoCourse/01_Basics/01_hello.mp4", 60000)
	seedMediaFile(t, conn, src.ID, "GoCourse/02_Core/Deep/01_sub.mp4", 90000)
	seedMediaFile(t, conn, src.ID, "GoCourse/10_Deploy/01_docker.mp4", 75000)

	preview, err := svc.PreviewDirectoryImport(ctx, src.ID, "GoCourse")
	if err != nil {
		t.Fatalf("PreviewDirectoryImport: %v", err)
	}

	if preview.CourseTitle != "GoCourse" {
		t.Fatalf("CourseTitle = %q, want GoCourse", preview.CourseTitle)
	}
	if preview.CoverRelPath != "GoCourse/cover.jpg" {
		t.Fatalf("CoverRelPath = %q, want GoCourse/cover.jpg", preview.CoverRelPath)
	}
	if len(preview.Chapters) != 4 {
		t.Fatalf("len(Chapters) = %d, want 4 (%+v)", len(preview.Chapters), preview.Chapters)
	}
	if preview.Chapters[0].Title != "正片" || preview.Chapters[0].SortOrder != 1 {
		t.Fatalf("Chapter 0 = %+v, want 正片 sort_order 1", preview.Chapters[0])
	}
	if preview.Chapters[1].Title != "01_Basics" || preview.Chapters[2].Title != "02_Core" || preview.Chapters[3].Title != "10_Deploy" {
		t.Fatalf("unexpected chapter order: %+v", preview.Chapters)
	}
	if len(preview.Chapters[2].Lessons) != 1 || preview.Chapters[2].Lessons[0].Title != "Deep / 01_sub" {
		t.Fatalf("expected flattened nested lesson title 'Deep / 01_sub', got %+v", preview.Chapters[2].Lessons)
	}

	libs, err := svc.ListLibraries(ctx)
	if err != nil || len(libs) == 0 {
		t.Fatalf("ListLibraries: %v", err)
	}

	var commitChapters []catalog.CommitImportChapter
	for _, ch := range preview.Chapters {
		var ls []catalog.CommitImportLesson
		for _, l := range ch.Lessons {
			ls = append(ls, catalog.CommitImportLesson{
				MediaFileID: l.MediaFileID,
				Title:       l.Title,
			})
		}
		commitChapters = append(commitChapters, catalog.CommitImportChapter{
			Title:   ch.Title,
			Lessons: ls,
		})
	}

	course, err := svc.CommitDirectoryImport(ctx, catalog.CommitImportInput{
		SourceID:     src.ID,
		DirRelPath:   "GoCourse",
		LibraryID:    libs[0].ID,
		Title:        preview.CourseTitle,
		Tags:         []string{"Go", "Backend"},
		CoverRelPath: preview.CoverRelPath,
		Chapters:     commitChapters,
	})
	if err != nil {
		t.Fatalf("CommitDirectoryImport: %v", err)
	}

	expectedCoverName := course.ID + ".jpg"
	if course.CoverPath != expectedCoverName {
		t.Fatalf("course.CoverPath = %q, want %q", course.CoverPath, expectedCoverName)
	}
	copiedCover, err := os.ReadFile(filepath.Join(dataDir, "covers", expectedCoverName))
	if err != nil {
		t.Fatalf("read copied cover: %v", err)
	}
	if string(copiedCover) != string(coverBytes) {
		t.Fatalf("copied cover content mismatch")
	}

	// User manually renames a lesson.
	renamedLessonID := course.Chapters[1].Lessons[0].ID
	if _, err := svc.UpdateLesson(ctx, renamedLessonID, "My Custom Intro Title", "Custom overview", ""); err != nil {
		t.Fatalf("UpdateLesson: %v", err)
	}

	// Add 1 new indexed file under 02_Core.
	newMF := seedMediaFile(t, conn, src.ID, "GoCourse/02_Core/02_interfaces.mp4", 80000)

	newFiles, err := svc.CheckNewFiles(ctx, course.ID)
	if err != nil {
		t.Fatalf("CheckNewFiles: %v", err)
	}
	if len(newFiles) != 1 || newFiles[0].ID != newMF {
		t.Fatalf("expected 1 new file (%s), got %+v", newMF, newFiles)
	}

	if err := svc.AppendNewFiles(ctx, course.ID, []string{newMF}); err != nil {
		t.Fatalf("AppendNewFiles: %v", err)
	}

	updatedCourse, err := svc.GetCourseDetail(ctx, course.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail after append: %v", err)
	}
	if updatedCourse.Chapters[1].Lessons[0].Title != "My Custom Intro Title" {
		t.Fatalf("user-renamed lesson was overwritten: %q", updatedCourse.Chapters[1].Lessons[0].Title)
	}
	if len(updatedCourse.Chapters[2].Lessons) != 2 {
		t.Fatalf("expected 2 lessons in 02_Core after append, got %d", len(updatedCourse.Chapters[2].Lessons))
	}

	// Second CheckNewFiles must return empty.
	newFiles2, err := svc.CheckNewFiles(ctx, course.ID)
	if err != nil {
		t.Fatalf("CheckNewFiles (2nd): %v", err)
	}
	if len(newFiles2) != 0 {
		t.Fatalf("expected 0 new files on second check, got %d", len(newFiles2))
	}
}

func TestProgressAutoCompleteAt90PercentAndNextUp(t *testing.T) {
	ctx := context.Background()
	svc, conn, _ := setupTestCatalog(t)

	mediaRoot := t.TempDir()
	src, err := svc.CreateSource(ctx, "Disk", "local", mediaRoot)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	// 100s lessons -> total ticks = 100_000 * 10_000 = 1_000_000_000
	mf1 := seedMediaFile(t, conn, src.ID, "c/01.mp4", 100000)
	mf2 := seedMediaFile(t, conn, src.ID, "c/02.mp4", 100000)

	libs, err := svc.ListLibraries(ctx)
	if err != nil || len(libs) == 0 {
		t.Fatalf("ListLibraries: %v", err)
	}

	course, err := svc.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: libs[0].ID,
		Title:     "Progress Course",
	})
	if err != nil {
		t.Fatalf("CreateCourse: %v", err)
	}
	chID := course.Chapters[0].ID
	l1, err := svc.AddLesson(ctx, chID, mf1, "Lesson 1")
	if err != nil {
		t.Fatalf("AddLesson 1: %v", err)
	}
	l2, err := svc.AddLesson(ctx, chID, mf2, "Lesson 2")
	if err != nil {
		t.Fatalf("AddLesson 2: %v", err)
	}

	// Report 50% on Lesson 1 -> appears in Resume, not completed.
	totalTicks := int64(100000) * 10000
	p50, err := svc.ReportProgress(ctx, l1.ID, totalTicks/2)
	if err != nil {
		t.Fatalf("ReportProgress 50%%: %v", err)
	}
	if p50.Played || p50.PositionTicks != totalTicks/2 {
		t.Fatalf("unexpected progress at 50%%: %+v", p50)
	}

	resume, err := svc.GetResumeLessons(ctx, 10)
	if err != nil {
		t.Fatalf("GetResumeLessons: %v", err)
	}
	if len(resume) != 1 || resume[0].ID != l1.ID {
		t.Fatalf("expected Lesson 1 in resume, got %+v", resume)
	}

	// Report 91% on Lesson 1 -> auto-completes, resets position_ticks = 0, disappears from Resume, Lesson 2 enters NextUp.
	p91, err := svc.ReportProgress(ctx, l1.ID, (totalTicks*91)/100)
	if err != nil {
		t.Fatalf("ReportProgress 91%%: %v", err)
	}
	if !p91.Played || p91.PlayCount != 1 || p91.PositionTicks != 0 {
		t.Fatalf("expected auto-completed progress at 91%%, got %+v", p91)
	}

	resumeAfter, err := svc.GetResumeLessons(ctx, 10)
	if err != nil {
		t.Fatalf("GetResumeLessons after 91%%: %v", err)
	}
	if len(resumeAfter) != 0 {
		t.Fatalf("expected empty resume after completion, got %+v", resumeAfter)
	}

	nextUp, err := svc.GetNextUpLessons(ctx, 10)
	if err != nil {
		t.Fatalf("GetNextUpLessons: %v", err)
	}
	if len(nextUp) != 1 || nextUp[0].ID != l2.ID {
		t.Fatalf("expected Lesson 2 in NextUp, got %+v", nextUp)
	}

	detail, err := svc.GetCourseDetail(ctx, course.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail: %v", err)
	}
	if detail.UnplayedItemCount != 1 {
		t.Fatalf("UnplayedItemCount = %d, want 1", detail.UnplayedItemCount)
	}
	if detail.PlayedPercentage != 50.0 {
		t.Fatalf("PlayedPercentage = %v, want 50.0", detail.PlayedPercentage)
	}

	// Repeated heartbeats in the final 10% (92%, 95%, 99%) must NOT inflate play_count.
	for _, pct := range []int64{92, 95, 99} {
		pRepeat, err := svc.ReportProgress(ctx, l1.ID, (totalTicks*pct)/100)
		if err != nil {
			t.Fatalf("ReportProgress %d%%: %v", pct, err)
		}
		if !pRepeat.Played || pRepeat.PositionTicks != 0 || pRepeat.PlayCount != 1 {
			t.Fatalf("at %d%%: expected Played=true PositionTicks=0 PlayCount=1, got %+v", pct, pRepeat)
		}
	}

	// Genuine replay: seek back >0 then complete again increments play_count once to 2.
	if _, err := svc.ReportProgress(ctx, l1.ID, totalTicks/4); err != nil {
		t.Fatalf("ReportProgress replay 25%%: %v", err)
	}
	pReplayDone, err := svc.ReportProgress(ctx, l1.ID, (totalTicks*95)/100)
	if err != nil {
		t.Fatalf("ReportProgress replay 95%%: %v", err)
	}
	if pReplayDone.PlayCount != 2 {
		t.Fatalf("expected PlayCount=2 after genuine replay, got %+v", pReplayDone)
	}
}

func TestDeleteLibraryWithOptions(t *testing.T) {
	ctx := context.Background()
	svc, conn, dataDir := setupTestCatalog(t)

	// 1. Empty library deletes cleanly with default options
	emptyLib, err := svc.CreateLibrary(ctx, "空库")
	if err != nil {
		t.Fatalf("CreateLibrary empty: %v", err)
	}
	if err := svc.DeleteLibraryWithOptions(ctx, emptyLib.ID, catalog.DeleteLibraryOptions{}); err != nil {
		t.Fatalf("DeleteLibraryWithOptions empty: %v", err)
	}

	// Setup media source, two libraries, and a course with chapter + lesson + cover
	mediaRoot := t.TempDir()
	src, err := svc.CreateSource(ctx, "Disk", "local", mediaRoot)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	mfID := seedMediaFile(t, conn, src.ID, "go/01.mp4", 60000)

	libA, err := svc.CreateLibrary(ctx, "库A")
	if err != nil {
		t.Fatalf("CreateLibrary A: %v", err)
	}
	libB, err := svc.CreateLibrary(ctx, "库B")
	if err != nil {
		t.Fatalf("CreateLibrary B: %v", err)
	}

	course, err := svc.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: libA.ID,
		Title:     "Go 课程",
	})
	if err != nil {
		t.Fatalf("CreateCourse: %v", err)
	}
	ch, err := svc.CreateChapter(ctx, course.ID, "第一章")
	if err != nil {
		t.Fatalf("CreateChapter: %v", err)
	}
	if _, err := svc.AddLesson(ctx, ch.ID, mfID, "第1节"); err != nil {
		t.Fatalf("AddLesson: %v", err)
	}

	// 2. Non-empty library returns ErrConflict with default options
	if err := svc.DeleteLibraryWithOptions(ctx, libA.ID, catalog.DeleteLibraryOptions{}); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("default delete non-empty err = %v, want ErrConflict", err)
	}

	// 3. Conflicting options (MoveToLibraryID + Force) returns ErrInvalidInput
	if err := svc.DeleteLibraryWithOptions(ctx, libA.ID, catalog.DeleteLibraryOptions{
		MoveToLibraryID: libB.ID,
		Force:           true,
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("move+force err = %v, want ErrInvalidInput", err)
	}

	// 4. Self-migration returns ErrInvalidInput; nonexistent target returns ErrNotFound
	if err := svc.DeleteLibraryWithOptions(ctx, libA.ID, catalog.DeleteLibraryOptions{
		MoveToLibraryID: libA.ID,
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("self move err = %v, want ErrInvalidInput", err)
	}
	if err := svc.DeleteLibraryWithOptions(ctx, libA.ID, catalog.DeleteLibraryOptions{
		MoveToLibraryID: "nonexistent-lib",
	}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("missing target move err = %v, want ErrNotFound", err)
	}

	// 5. Valid MoveToLibraryID moves courses & preserves chapters/lessons, deletes libA
	if err := svc.DeleteLibraryWithOptions(ctx, libA.ID, catalog.DeleteLibraryOptions{
		MoveToLibraryID: libB.ID,
	}); err != nil {
		t.Fatalf("move delete libA: %v", err)
	}
	moved, err := svc.GetCourseDetail(ctx, course.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail after move: %v", err)
	}
	if moved.LibraryID != libB.ID {
		t.Fatalf("moved.LibraryID = %q, want %q", moved.LibraryID, libB.ID)
	}
	if moved.ChapterCount != 2 || moved.LessonCount != 1 {
		t.Fatalf("expected chapters/lessons preserved (default + created chapter), got ch=%d l=%d", moved.ChapterCount, moved.LessonCount)
	}

	// 6. Force delete removes library + courses + app-managed covers, leaves media_files intact
	coversDir := filepath.Join(dataDir, "covers")
	if err := os.MkdirAll(coversDir, 0o755); err != nil {
		t.Fatalf("mkdir covers: %v", err)
	}
	coverName := course.ID + "_cover.jpg"
	coverFull := filepath.Join(coversDir, coverName)
	if err := os.WriteFile(coverFull, []byte("fakejpg"), 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}
	if _, err := svc.UpdateCourse(ctx, course.ID, catalog.UpdateCourseInput{CoverPath: &coverName}); err != nil {
		t.Fatalf("UpdateCourse cover: %v", err)
	}

	if err := svc.DeleteLibraryWithOptions(ctx, libB.ID, catalog.DeleteLibraryOptions{Force: true}); err != nil {
		t.Fatalf("force delete libB: %v", err)
	}
	if _, err := svc.GetCourseDetail(ctx, course.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("expected course deleted after force library delete, got %v", err)
	}
	if _, err := os.Stat(coverFull); !os.IsNotExist(err) {
		t.Fatalf("expected cover file removed on force delete, stat err = %v", err)
	}
	if _, err := svc.GetMediaFile(ctx, mfID); err != nil {
		t.Fatalf("expected media_file intact after force library delete, got %v", err)
	}
}

func TestListAndBindSubtitles(t *testing.T) {
	ctx := context.Background()
	svc, conn, _ := setupTestCatalog(t)
	mediaRoot := t.TempDir()

	src, err := svc.CreateSource(ctx, "SubSrc", "local", mediaRoot)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	mfID := seedMediaFile(t, conn, src.ID, "Course/01_intro.mp4", 60000)

	subID := id.New()
	if _, err := conn.Exec(`
		INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
		VALUES (?, ?, 'Course/custom_sub.srt', NULL, 'und', '', 'srt', 0, 'active')`,
		subID, src.ID,
	); err != nil {
		t.Fatalf("insert unmatched subtitle: %v", err)
	}

	course, err := svc.CreateCourse(ctx, catalog.CreateCourseInput{Title: "Sub Course"})
	if err != nil {
		t.Fatalf("CreateCourse: %v", err)
	}
	lesson, err := svc.AddLesson(ctx, course.Chapters[0].ID, mfID, "Intro")
	if err != nil {
		t.Fatalf("AddLesson: %v", err)
	}
	if lesson.SourceID != src.ID {
		t.Fatalf("GetLesson SourceID = %q, want %q", lesson.SourceID, src.ID)
	}
	detail, err := svc.GetCourseDetail(ctx, course.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail: %v", err)
	}
	if detail.Chapters[0].Lessons[0].SourceID != src.ID {
		t.Fatalf("GetCourseDetail lesson SourceID = %q, want %q", detail.Chapters[0].Lessons[0].SourceID, src.ID)
	}

	unmatchedTrue := true
	subs, err := svc.ListSourceSubtitles(ctx, src.ID, "", "", &unmatchedTrue)
	if err != nil {
		t.Fatalf("ListSourceSubtitles: %v", err)
	}
	if len(subs) != 1 || subs[0].ID != subID || subs[0].FileName != "custom_sub.srt" {
		t.Fatalf("unexpected unmatched subs: %+v", subs)
	}

	// Bind subtitle manually
	titleZh := "中文字幕"
	langZh := "zh"
	isDef := true
	bound, err := svc.BindSubtitle(ctx, subID, catalog.BindSubtitleInput{
		MediaFileID: &mfID,
		Title:       &titleZh,
		Language:    &langZh,
		IsDefault:   &isDef,
	})
	if err != nil {
		t.Fatalf("BindSubtitle: %v", err)
	}
	if bound.MediaFileID != mfID || !bound.ManuallyBound || bound.Title != "中文字幕" || bound.Language != "zh" || !bound.IsDefault {
		t.Fatalf("unexpected bound subtitle: %+v", bound)
	}

	lessonAfter, err := svc.GetLesson(ctx, lesson.ID)
	if err != nil {
		t.Fatalf("GetLesson after bind: %v", err)
	}
	if len(lessonAfter.Subtitles) != 1 || lessonAfter.Subtitles[0].ID != subID {
		t.Fatalf("expected lesson to include bound subtitle, got %+v", lessonAfter.Subtitles)
	}

	// Unbind keeps manually_bound = true so scanner will not silently re-attach
	empty := ""
	unbound, err := svc.BindSubtitle(ctx, subID, catalog.BindSubtitleInput{
		MediaFileID: &empty,
	})
	if err != nil {
		t.Fatalf("BindSubtitle unbind: %v", err)
	}
	if unbound.MediaFileID != "" || !unbound.ManuallyBound {
		t.Fatalf("expected unbound with manually_bound=true, got %+v", unbound)
	}

	// ResetAuto returns subtitle to auto-match pool (manually_bound = false)
	reset, err := svc.BindSubtitle(ctx, subID, catalog.BindSubtitleInput{
		ResetAuto: true,
	})
	if err != nil {
		t.Fatalf("BindSubtitle reset_auto: %v", err)
	}
	if reset.MediaFileID != "" || reset.ManuallyBound {
		t.Fatalf("expected reset_auto with manually_bound=false, got %+v", reset)
	}
}

func TestStorageAssignmentCountsAndMultiCourseDedup(t *testing.T) {
	ctx := context.Background()
	svc, conn, _ := setupTestCatalog(t)
	mediaRoot := t.TempDir()

	src, err := svc.CreateSource(ctx, "AssignSrc", "local", mediaRoot)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	mf1 := seedMediaFile(t, conn, src.ID, "DirA/01.mp4", 60000)
	mf2 := seedMediaFile(t, conn, src.ID, "DirA/02.mp4", 60000)
	mf3 := seedMediaFile(t, conn, src.ID, "DirB/03.mp4", 60000)

	c1, err := svc.CreateCourse(ctx, catalog.CreateCourseInput{Title: "Beta Course"})
	if err != nil {
		t.Fatalf("CreateCourse c1: %v", err)
	}
	c2, err := svc.CreateCourse(ctx, catalog.CreateCourseInput{Title: "Alpha Course"})
	if err != nil {
		t.Fatalf("CreateCourse c2: %v", err)
	}
	// Assign mf1 to BOTH courses to test deduplication
	if _, err := svc.AddLesson(ctx, c1.Chapters[0].ID, mf1, "L1"); err != nil {
		t.Fatalf("AddLesson c1: %v", err)
	}
	if _, err := svc.AddLesson(ctx, c2.Chapters[0].ID, mf1, "L1 copy"); err != nil {
		t.Fatalf("AddLesson c2: %v", err)
	}

	gotSrc, err := svc.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if gotSrc.MediaCount != 3 || gotSrc.AssignedCount != 1 || gotSrc.UnassignedCount != 2 {
		t.Fatalf("GetSource counts = media:%d assigned:%d unassigned:%d, want 3/1/2", gotSrc.MediaCount, gotSrc.AssignedCount, gotSrc.UnassignedCount)
	}
	sources, err := svc.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 || sources[0].AssignedCount != 1 || sources[0].UnassignedCount != 2 {
		t.Fatalf("ListSources counts = %+v, want 3/1/2", sources)
	}

	dirs, err := svc.ListIndexedDirs(ctx, src.ID, "")
	if err != nil {
		t.Fatalf("ListIndexedDirs: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %+v", dirs)
	}
	dirA, dirB := dirs[0], dirs[1]
	if dirA.Name != "DirA" || dirA.FileCount != 2 || dirA.AssignedCount != 1 || dirA.UnassignedCount != 1 {
		t.Fatalf("unexpected DirA: %+v", dirA)
	}
	if len(dirA.Courses) != 2 || dirA.Courses[0].Title != "Alpha Course" || dirA.Courses[1].Title != "Beta Course" {
		t.Fatalf("unexpected DirA.Courses: %+v", dirA.Courses)
	}
	if dirB.Name != "DirB" || dirB.FileCount != 1 || dirB.AssignedCount != 0 || dirB.UnassignedCount != 1 || dirB.Courses == nil || len(dirB.Courses) != 0 {
		t.Fatalf("unexpected DirB: %+v", dirB)
	}

	allFiles, err := svc.ListMediaFilesWithAssigned(ctx, src.ID, "", "", "", "")
	if err != nil {
		t.Fatalf("ListMediaFilesWithAssigned all: %v", err)
	}
	if len(allFiles) != 3 {
		t.Fatalf("expected 3 files, got %d", len(allFiles))
	}
	if !allFiles[0].Assigned || len(allFiles[0].Courses) != 2 || allFiles[1].Assigned || allFiles[2].Assigned {
		t.Fatalf("unexpected file assignment flags: %+v", allFiles)
	}

	assignedOnly, err := svc.ListMediaFilesWithAssigned(ctx, src.ID, "", "", "", "true")
	if err != nil {
		t.Fatalf("ListMediaFilesWithAssigned true: %v", err)
	}
	if len(assignedOnly) != 1 || assignedOnly[0].ID != mf1 {
		t.Fatalf("expected only mf1, got %+v", assignedOnly)
	}

	unassignedOnly, err := svc.ListMediaFilesWithAssigned(ctx, src.ID, "", "", "", "false")
	if err != nil {
		t.Fatalf("ListMediaFilesWithAssigned false: %v", err)
	}
	if len(unassignedOnly) != 2 || unassignedOnly[0].ID != mf2 || unassignedOnly[1].ID != mf3 {
		t.Fatalf("expected mf2 and mf3, got %+v", unassignedOnly)
	}
}

func TestBindSubtitleRejectsCrossSourceOrMissingMediaFile(t *testing.T) {
	ctx := context.Background()
	svc, conn, _ := setupTestCatalog(t)

	srcA, err := svc.CreateSource(ctx, "SrcA", "local", t.TempDir())
	if err != nil {
		t.Fatalf("CreateSource A: %v", err)
	}
	srcB, err := svc.CreateSource(ctx, "SrcB", "local", t.TempDir())
	if err != nil {
		t.Fatalf("CreateSource B: %v", err)
	}
	mfA := seedMediaFile(t, conn, srcA.ID, "a.mp4", 60000)
	mfB := seedMediaFile(t, conn, srcB.ID, "b.mp4", 60000)

	// Mark mfA missing
	if _, err := conn.Exec(`UPDATE media_files SET status = 'missing' WHERE id = ?`, mfA); err != nil {
		t.Fatalf("mark mfA missing: %v", err)
	}

	subID := "sub00000000000000000000000000099"
	if _, err := conn.Exec(
		`INSERT INTO subtitle_files (id, source_id, media_file_id, relative_path, language, title, format, is_default, manually_bound, status)
		 VALUES (?, ?, NULL, 'a.srt', 'zh', '中文', 'srt', 0, 0, 'active')`,
		subID, srcA.ID,
	); err != nil {
		t.Fatalf("insert subtitle: %v", err)
	}

	if _, err := svc.BindSubtitle(ctx, subID, catalog.BindSubtitleInput{MediaFileID: &mfB}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for cross-source bind, got %v", err)
	}
	if _, err := svc.BindSubtitle(ctx, subID, catalog.BindSubtitleInput{MediaFileID: &mfA}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for missing media_file bind, got %v", err)
	}
}
