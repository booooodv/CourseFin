package storage_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"coursefin/internal/storage"
)

func TestLocalFSWalkStatOpenAndDirectURL(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Create directory structure and sample files.
	if err := os.MkdirAll(filepath.Join(root, "course1", "sub"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content1 := []byte("0123456789abcdef-video-content")
	content2 := []byte("deep-video-content")
	contentSub := []byte("1\n00:00:00,000 --> 00:00:01,000\nhello\n")

	if err := os.WriteFile(filepath.Join(root, "course1", "01_intro.mp4"), content1, 0o644); err != nil {
		t.Fatalf("write file 1 failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "course1", "sub", "02_deep.mkv"), content2, 0o644); err != nil {
		t.Fatalf("write file 2 failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "root.srt"), contentSub, 0o644); err != nil {
		t.Fatalf("write root.srt failed: %v", err)
	}

	fs, err := storage.NewLocalFS(root)
	if err != nil {
		t.Fatalf("NewLocalFS failed: %v", err)
	}

	// Verify Walk from root returns clean forward-slash relative paths.
	entries, err := fs.Walk(ctx, "")
	if err != nil {
		t.Fatalf("Walk(\"\") failed: %v", err)
	}

	gotPaths := make(map[string]storage.FileEntry, len(entries))
	for _, e := range entries {
		gotPaths[e.RelPath] = e
	}

	expectedFiles := map[string]int64{
		"course1/01_intro.mp4":     int64(len(content1)),
		"course1/sub/02_deep.mkv":  int64(len(content2)),
		"root.srt":                 int64(len(contentSub)),
	}
	for rel, wantSize := range expectedFiles {
		entry, ok := gotPaths[rel]
		if !ok {
			t.Fatalf("Walk missing entry %q; got keys: %v", rel, gotPaths)
		}
		if entry.IsDir {
			t.Fatalf("expected %q to not be a directory", rel)
		}
		if entry.Size != wantSize {
			t.Fatalf("entry %q size = %d, want %d", rel, entry.Size, wantSize)
		}
		if entry.ModTime.IsZero() {
			t.Fatalf("entry %q has zero ModTime", rel)
		}
	}

	// Verify Walk scoped to a subdirectory still returns paths relative to storage root.
	subEntries, err := fs.Walk(ctx, "course1/sub")
	if err != nil {
		t.Fatalf("Walk(\"course1/sub\") failed: %v", err)
	}
	if len(subEntries) != 1 || subEntries[0].RelPath != "course1/sub/02_deep.mkv" {
		t.Fatalf("unexpected subEntries: %+v", subEntries)
	}

	// Verify Stat.
	info, err := fs.Stat(ctx, "course1/01_intro.mp4")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.RelPath != "course1/01_intro.mp4" || info.Size != int64(len(content1)) || info.IsDir {
		t.Fatalf("unexpected Stat result: %+v", info)
	}

	// Verify Open and Seek.
	f, err := fs.Open(ctx, "course1/01_intro.mp4")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	if _, err := f.Seek(10, io.SeekStart); err != nil {
		t.Fatalf("Seek failed: %v", err)
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(buf) != string(content1[10:]) {
		t.Fatalf("seeked read = %q, want %q", string(buf), string(content1[10:]))
	}

	// Verify DirectURL returns ("", false, nil).
	url, ok, err := fs.DirectURL(ctx, "course1/01_intro.mp4")
	if err != nil {
		t.Fatalf("DirectURL failed: %v", err)
	}
	if ok || url != "" {
		t.Fatalf("DirectURL = (%q, %v), want (\"\", false)", url, ok)
	}

	// Verify Registry resolves local driver and rejects unknown driver.
	reg := storage.NewRegistry()
	prov, err := reg.ProviderFor("local", root, "{}")
	if err != nil {
		t.Fatalf("Registry.ProviderFor(local) failed: %v", err)
	}
	if _, err := prov.Stat(ctx, "root.srt"); err != nil {
		t.Fatalf("Registry provider Stat failed: %v", err)
	}
	if _, err := reg.ProviderFor("s3", root, "{}"); err == nil {
		t.Fatalf("expected error for unsupported driver s3")
	}
}

func TestLocalFSRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ctx := context.Background()

	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub failed: %v", err)
	}
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("top-secret"), 0o644); err != nil {
		t.Fatalf("write outside file failed: %v", err)
	}

	// Create symlinks inside root pointing outside root.
	if err := os.Symlink(outsideFile, filepath.Join(root, "escape_file.txt")); err != nil {
		t.Fatalf("symlink file failed: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape_dir")); err != nil {
		t.Fatalf("symlink dir failed: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "missing.txt"), filepath.Join(root, "dangling_escape.txt")); err != nil {
		t.Fatalf("dangling symlink failed: %v", err)
	}

	fs, err := storage.NewLocalFS(root)
	if err != nil {
		t.Fatalf("NewLocalFS failed: %v", err)
	}

	badPaths := []string{
		"../secret.txt",
		"sub/../../secret.txt",
		"/etc/passwd",
		"escape_file.txt",
		"escape_dir",
		"escape_dir/secret.txt",
		"dangling_escape.txt",
	}

	for _, p := range badPaths {
		if _, err := fs.Stat(ctx, p); !errors.Is(err, storage.ErrPathOutsideRoot) {
			t.Errorf("Stat(%q) error = %v, want ErrPathOutsideRoot", p, err)
		}
		if rc, err := fs.Open(ctx, p); !errors.Is(err, storage.ErrPathOutsideRoot) {
			if rc != nil {
				rc.Close()
			}
			t.Errorf("Open(%q) error = %v, want ErrPathOutsideRoot", p, err)
		}
		if _, _, err := fs.DirectURL(ctx, p); !errors.Is(err, storage.ErrPathOutsideRoot) {
			t.Errorf("DirectURL(%q) error = %v, want ErrPathOutsideRoot", p, err)
		}
		if _, err := fs.Walk(ctx, p); !errors.Is(err, storage.ErrPathOutsideRoot) {
			t.Errorf("Walk(%q) error = %v, want ErrPathOutsideRoot", p, err)
		}
	}
}
