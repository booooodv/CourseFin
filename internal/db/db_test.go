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
