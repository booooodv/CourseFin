package catalog_test

import (
	"context"
	"database/sql"
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
}
