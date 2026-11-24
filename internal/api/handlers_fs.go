package api

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"coursefin/internal/catalog"
)

type fsDirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type fsRootShortcut struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type fsBrowseResponse struct {
	CurrentPath string           `json:"current_path"`
	ParentPath  string           `json:"parent_path"`
	Entries     []fsDirEntry     `json:"entries"`
	Roots       []fsRootShortcut `json:"roots"`
}

func (h *handler) handleBrowseFSDirs(w http.ResponseWriter, r *http.Request) {
	rawPath := strings.TrimSpace(r.URL.Query().Get("path"))
	showHiddenRaw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("show_hidden")))
	showHidden := showHiddenRaw == "true" || showHiddenRaw == "1"

	roots := availableFSRoots()

	var targetPath string
	if rawPath == "" {
		targetPath = defaultBrowsePath(roots)
	} else {
		if !filepath.IsAbs(rawPath) {
			writeError(w, http.StatusBadRequest, "目录路径必须是绝对路径")
			return
		}
		targetPath = filepath.Clean(rawPath)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			writeError(w, http.StatusNotFound, "目录不存在")
		case errors.Is(err, fs.ErrPermission):
			writeError(w, http.StatusForbidden, "无权访问该目录")
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if !info.IsDir() {
		writeError(w, http.StatusBadRequest, "目标路径不是目录")
		return
	}

	dirEntries, err := os.ReadDir(targetPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			writeError(w, http.StatusForbidden, "无权读取该目录")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entries := make([]fsDirEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		name := entry.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !isDirEntryDirectory(targetPath, entry) {
			continue
		}
		entries = append(entries, fsDirEntry{
			Name: name,
			Path: filepath.Join(targetPath, name),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return catalog.NaturalCompare(entries[i].Name, entries[j].Name) < 0
	})

	parentPath := filepath.Dir(targetPath)
	if parentPath == targetPath {
		parentPath = ""
	}

	writeJSON(w, http.StatusOK, fsBrowseResponse{
		CurrentPath: targetPath,
		ParentPath:  parentPath,
		Entries:     entries,
		Roots:       roots,
	})
}

func isDirEntryDirectory(parentDir string, entry fs.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&fs.ModeSymlink != 0 {
		info, err := os.Stat(filepath.Join(parentDir, entry.Name()))
		return err == nil && info.IsDir()
	}
	return false
}

func availableFSRoots() []fsRootShortcut {
	var candidates []fsRootShortcut

	if runtime.GOOS != "windows" {
		candidates = append(candidates, fsRootShortcut{
			Label: "媒体目录 (/media)",
			Path:  "/media",
		})
	}

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, fsRootShortcut{
			Label: "主目录",
			Path:  filepath.Clean(home),
		})
	}

	if runtime.GOOS == "windows" {
		for ch := 'A'; ch <= 'Z'; ch++ {
			drive := string(ch) + `:\`
			candidates = append(candidates, fsRootShortcut{
				Label: drive,
				Path:  drive,
			})
		}
	} else {
		candidates = append(candidates, fsRootShortcut{
			Label: "根目录 (/)",
			Path:  "/",
		})
	}

	seen := make(map[string]bool, len(candidates))
	roots := make([]fsRootShortcut, 0, len(candidates))
	for _, cand := range candidates {
		clean := filepath.Clean(cand.Path)
		if seen[clean] {
			continue
		}
		info, err := os.Stat(clean)
		if err != nil || !info.IsDir() {
			continue
		}
		f, err := os.Open(clean)
		if err != nil {
			continue
		}
		_ = f.Close()
		seen[clean] = true
		roots = append(roots, fsRootShortcut{
			Label: cand.Label,
			Path:  clean,
		})
	}
	return roots
}

func defaultBrowsePath(roots []fsRootShortcut) string {
	if len(roots) > 0 {
		return roots[0].Path
	}
	if cwd, err := os.Getwd(); err == nil && filepath.IsAbs(cwd) {
		return filepath.Clean(cwd)
	}
	return string(filepath.Separator)
}
