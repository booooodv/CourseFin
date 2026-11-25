package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"coursefin/internal/id"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open initializes the data directory layout, opens the SQLite database with
// WAL mode and foreign key constraints enabled, applies embedded migrations,
// and seeds persistent server defaults.
func Open(dataDir string) (*sql.DB, error) {
	if err := ensureDataDirs(dataDir); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "coursefin.db")
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(4)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if err := runMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	if err := seedDefaults(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("seed defaults: %w", err)
	}

	return conn, nil
}

func ensureDataDirs(dataDir string) error {
	dirs := []string{
		dataDir,
		filepath.Join(dataDir, "covers"),
		filepath.Join(dataDir, "thumbs"),
		filepath.Join(dataDir, "cache", "images"),
		filepath.Join(dataDir, "cache", "remux"),
		filepath.Join(dataDir, "cache", "asr"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

func runMigrations(conn *sql.DB) error {
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		sqlBytes, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := conn.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}

		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("execute migration %s: %w", name, err)
		}

		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, name, now); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}

func seedDefaults(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO app_settings (key, value, updated_at) VALUES ('server_id', ?, ?)`,
		id.New(), now,
	); err != nil {
		return fmt.Errorf("seed server_id: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO app_settings (key, value, updated_at) VALUES ('server_name', 'CourseFin', ?)`,
		now,
	); err != nil {
		return fmt.Errorf("seed server_name: %w", err)
	}

	var libCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM libraries`).Scan(&libCount); err != nil {
		return fmt.Errorf("count libraries: %w", err)
	}
	if libCount == 0 {
		if _, err := tx.Exec(
			`INSERT INTO libraries (id, name, cover_path, sort_order, created_at, updated_at) VALUES (?, '我的课程', '', 1, ?, ?)`,
			id.New(), now, now,
		); err != nil {
			return fmt.Errorf("seed default library: %w", err)
		}
	}

	return tx.Commit()
}
