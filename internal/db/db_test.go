package db_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coursefin/internal/db"
	"coursefin/internal/id"
)

func TestOpenCreatesSchemaAndSeedsDefaults(t *testing.T) {
	dataDir := t.TempDir()

	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer conn.Close()

	// Verify data subdirectories are created.
	for _, sub := range []string{"covers", "thumbs", "cache/images", "cache/remux"} {
		info, err := os.Stat(filepath.Join(dataDir, sub))
		if err != nil || !info.IsDir() {
			t.Fatalf("expected directory %q to exist: err=%v", sub, err)
		}
	}

	// Verify WAL mode is enabled.
	var journalMode string
	if err := conn.QueryRow(`PRAGMA journal_mode;`).Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode failed: %v", err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("expected WAL journal_mode, got %q", journalMode)
	}

	// Verify foreign keys are enabled.
	var fk int
	if err := conn.QueryRow(`PRAGMA foreign_keys;`).Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys failed: %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}

	// Verify all required tables exist.
	requiredTables := []string{
		"storage_sources",
		"media_files",
		"subtitle_files",
		"libraries",
		"courses",
		"chapters",
		"lessons",
		"tags",
		"course_tags",
		"playback_progress",
		"users",
		"sessions",
		"app_settings",
	}
	for _, tbl := range requiredTables {
		var name string
		err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err != nil {
			t.Fatalf("expected table %q to exist: %v", tbl, err)
		}
	}

	// Verify server_id is a valid 32-char hex string.
	var serverID string
	if err := conn.QueryRow(`SELECT value FROM app_settings WHERE key='server_id'`).Scan(&serverID); err != nil {
		t.Fatalf("expected server_id in app_settings: %v", err)
	}
	normID, ok := id.Normalize(serverID)
	if !ok || normID != serverID {
		t.Fatalf("expected canonical 32-char hex server_id, got %q", serverID)
	}

	// Verify server_name default.
	var serverName string
	if err := conn.QueryRow(`SELECT value FROM app_settings WHERE key='server_name'`).Scan(&serverName); err != nil {
		t.Fatalf("expected server_name in app_settings: %v", err)
	}
	if serverName != "CourseFin" {
		t.Fatalf("expected server_name CourseFin, got %q", serverName)
	}

	// Verify default library "我的课程" exists.
	var libID, libName string
	if err := conn.QueryRow(`SELECT id, name FROM libraries WHERE name='我的课程'`).Scan(&libID, &libName); err != nil {
		t.Fatalf("expected default library '我的课程': %v", err)
	}
	if _, ok := id.Normalize(libID); !ok {
		t.Fatalf("expected valid hex library id, got %q", libID)
	}

	// Verify foreign key constraint enforcement.
	_, err = conn.Exec(`INSERT INTO courses (id, library_id, title, created_at, updated_at) VALUES ('00000000000000000000000000000001', 'nonexistent', 'Test', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err == nil {
		t.Fatalf("expected foreign key violation inserting course with missing library_id")
	}

	// Close and reopen to verify server_id and default library persist.
	if err := conn.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	conn2, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen db.Open failed: %v", err)
	}
	defer conn2.Close()

	var serverID2 string
	if err := conn2.QueryRow(`SELECT value FROM app_settings WHERE key='server_id'`).Scan(&serverID2); err != nil {
		t.Fatalf("query server_id after reopen failed: %v", err)
	}
	if serverID2 != serverID {
		t.Fatalf("expected server_id to persist across reopen: got %q, want %q", serverID2, serverID)
	}

	var libCount int
	if err := conn2.QueryRow(`SELECT COUNT(*) FROM libraries`).Scan(&libCount); err != nil {
		t.Fatalf("query libraries count failed: %v", err)
	}
	if libCount != 1 {
		t.Fatalf("expected 1 library after reopen, got %d", libCount)
	}
}

func TestMigration002AddsManuallyBoundColumn(t *testing.T) {
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer conn.Close()

	rows, err := conn.Query(`PRAGMA table_info(subtitle_files)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(subtitle_files): %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		if name == "manually_bound" {
			found = true
			if notNull != 1 {
				t.Errorf("manually_bound notnull = %d, want 1", notNull)
			}
			if dfltStr, ok := dflt.(string); !ok || dfltStr != "0" {
				t.Errorf("manually_bound default = %#v, want \"0\"", dflt)
			}
		}
	}
	if !found {
		t.Fatalf("expected manually_bound column in subtitle_files")
	}
}

func TestMigration003CreatesSummaryTablesAndCascades(t *testing.T) {
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	// Verify cache/asr directory exists
	info, err := os.Stat(filepath.Join(dataDir, "cache", "asr"))
	if err != nil || !info.IsDir() {
		t.Fatalf("expected cache/asr directory to exist: err=%v", err)
	}

	// Verify both tables exist
	for _, tbl := range []string{"media_transcripts", "lesson_summaries"} {
		var name string
		if err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil {
			t.Fatalf("expected table %q: %v", tbl, err)
		}
	}

	// Seed source -> media_file -> course -> chapter -> lesson, then insert transcript & summary.
	// Deleting lesson must cascade-delete lesson_summaries while keeping media_transcripts;
	// deleting media_file must cascade-delete media_transcripts.
	now := "2026-11-25T00:00:00Z"
	var libID string
	if err := conn.QueryRow(`SELECT id FROM libraries LIMIT 1`).Scan(&libID); err != nil {
		t.Fatalf("query default library: %v", err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	mustExec(`INSERT INTO storage_sources (id, name, driver, root_path, created_at, updated_at) VALUES ('s1','src','local','/tmp',?,?)`, now, now)
	mustExec(`INSERT INTO media_files (id, source_id, relative_path, file_name, created_at, updated_at) VALUES ('m1','s1','a.mp4','a.mp4',?,?)`, now, now)
	mustExec(`INSERT INTO courses (id, library_id, title, created_at, updated_at) VALUES ('c1',?,'Course',?,?)`, libID, now, now)
	mustExec(`INSERT INTO chapters (id, course_id, title, sort_order, created_at, updated_at) VALUES ('ch1','c1','Ch1',1,?,?)`, now, now)
	mustExec(`INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, sort_order, created_at, updated_at) VALUES ('l1','c1','ch1','m1','L1',1,?,?)`, now, now)
	mustExec(`INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at) VALUES ('m1','asr','zh','whisper-1','WEBVTT','',?,?)`, now, now)
	mustExec(`INSERT INTO lesson_summaries (lesson_id, media_file_id, source_type, source_label, has_timestamps, model, overview, outline_json, timeline_json, created_at, updated_at) VALUES ('l1','m1','asr','AI 语音转写',1,'gpt-4o-mini','ov','[]','[]',?,?)`, now, now)

	mustExec(`DELETE FROM lessons WHERE id = 'l1'`)
	var sumCount, trCount int
	_ = conn.QueryRow(`SELECT COUNT(*) FROM lesson_summaries WHERE lesson_id = 'l1'`).Scan(&sumCount)
	_ = conn.QueryRow(`SELECT COUNT(*) FROM media_transcripts WHERE media_file_id = 'm1'`).Scan(&trCount)
	if sumCount != 0 || trCount != 1 {
		t.Fatalf("after deleting lesson: summaries=%d want 0, transcripts=%d want 1", sumCount, trCount)
	}

	mustExec(`DELETE FROM media_files WHERE id = 'm1'`)
	_ = conn.QueryRow(`SELECT COUNT(*) FROM media_transcripts WHERE media_file_id = 'm1'`).Scan(&trCount)
	if trCount != 0 {
		t.Fatalf("after deleting media_file: transcripts=%d want 0", trCount)
	}
}
