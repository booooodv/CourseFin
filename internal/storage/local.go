package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// LocalFS implements StorageProvider over a local directory root with strict
// path traversal and symlink escape protection.
type LocalFS struct {
	rootPath string
}

// NewLocalFS validates rootPath, resolves any symlinks in rootPath itself,
// and returns a read-only LocalFS storage provider.
func NewLocalFS(rootPath string) (*LocalFS, error) {
	if strings.TrimSpace(rootPath) == "" {
		return nil, fmt.Errorf("rootPath must not be empty")
	}

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("resolve rootPath: %w", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("eval symlinks on rootPath: %w", err)
	}

	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("stat rootPath: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("rootPath is not a directory: %s", resolvedRoot)
	}

	return &LocalFS{rootPath: resolvedRoot}, nil
}

// Walk recursively lists files and directories under rootRelPath. Returned
// RelPath values are clean forward-slash paths relative to the LocalFS root.
func (l *LocalFS) Walk(ctx context.Context, rootRelPath string) ([]FileEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	startAbs, err := l.resolvePath(rootRelPath)
	if err != nil {
		return nil, err
	}

	startInfo, err := os.Stat(startAbs)
	if err != nil {
		return nil, err
	}
	if !startInfo.IsDir() {
		return nil, fmt.Errorf("walk target is not a directory: %s", rootRelPath)
	}

	var entries []FileEntry
	err = filepath.WalkDir(startAbs, func(curPath string, d fs.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if walkErr != nil {
			return walkErr
		}

		if curPath == startAbs {
			return nil
		}

		relOS, err := filepath.Rel(l.rootPath, curPath)
		if err != nil {
			return ErrPathOutsideRoot
		}

		relSlash := path.Clean(filepath.ToSlash(relOS))

		// Verify symlinks do not escape rootPath; skip escaping symlinks during Walk.
		if d.Type()&fs.ModeSymlink != 0 {
			if _, err := l.resolvePath(relSlash); err != nil {
				if errors.Is(err, ErrPathOutsideRoot) || errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			info, err := os.Stat(curPath)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			entries = append(entries, FileEntry{
				RelPath: relSlash,
				Name:    d.Name(),
				IsDir:   info.IsDir(),
				Size:    info.Size(),
				ModTime: info.ModTime().UTC(),
			})
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		entries = append(entries, FileEntry{
			RelPath: relSlash,
			Name:    d.Name(),
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RelPath < entries[j].RelPath
	})
	return entries, nil
}

// Stat returns metadata for a relative path inside the storage root.
func (l *LocalFS) Stat(ctx context.Context, relPath string) (FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return FileInfo{}, err
	}

	absPath, err := l.resolvePath(relPath)
	if err != nil {
		return FileInfo{}, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return FileInfo{}, err
	}

	relOS, err := filepath.Rel(l.rootPath, absPath)
	if err != nil {
		return FileInfo{}, ErrPathOutsideRoot
	}
	cleanRel := path.Clean(filepath.ToSlash(relOS))
	if cleanRel == "." {
		cleanRel = ""
	}

	return FileInfo{
		RelPath: cleanRel,
		Name:    info.Name(),
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
	}, nil
}

// Open opens a file inside the storage root for read-only seeking access.
func (l *LocalFS) Open(ctx context.Context, relPath string) (io.ReadSeekCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	absPath, err := l.resolvePath(relPath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("cannot open directory: %s", relPath)
	}

	return os.Open(absPath)
}

// DirectURL always returns ("", false, nil) for LocalFS after validating path safety.
func (l *LocalFS) DirectURL(ctx context.Context, relPath string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if _, err := l.resolvePath(relPath); err != nil {
		return "", false, err
	}
	return "", false, nil
}

// AbsPath validates relPath against traversal/symlink escape and returns the
// resolved absolute local filesystem path.
func (l *LocalFS) AbsPath(relPath string) (string, error) {
	return l.resolvePath(relPath)
}

func (l *LocalFS) resolvePath(relPath string) (string, error) {
	trimmed := strings.TrimSpace(relPath)
	if trimmed == "" || trimmed == "." {
		return l.rootPath, nil
	}

	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "\\") {
		return "", ErrPathOutsideRoot
	}

	osRel := filepath.FromSlash(trimmed)
	cleaned := filepath.Clean(osRel)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideRoot
	}

	joined := filepath.Join(l.rootPath, cleaned)
	if !isWithinRoot(l.rootPath, joined) {
		return "", ErrPathOutsideRoot
	}

	resolved, err := filepath.EvalSymlinks(joined)
	if err == nil {
		if !isWithinRoot(l.rootPath, resolved) {
			return "", ErrPathOutsideRoot
		}
		return resolved, nil
	}

	if errors.Is(err, fs.ErrNotExist) {
		if escapeErr := l.checkMissingPathEscape(joined); escapeErr != nil {
			return "", escapeErr
		}
	}

	return "", err
}

func (l *LocalFS) checkMissingPathEscape(joined string) error {
	// Walk from joined upward to rootPath, checking if any existing symlink or
	// ancestor directory resolves outside rootPath.
	cur := joined
	for {
		if linfo, lerr := os.Lstat(cur); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
			target, rerr := os.Readlink(cur)
			if rerr == nil {
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(cur), target)
				}
				target = filepath.Clean(target)
				if !isWithinRoot(l.rootPath, target) {
					return ErrPathOutsideRoot
				}
				if resTarget, serr := filepath.EvalSymlinks(target); serr == nil && !isWithinRoot(l.rootPath, resTarget) {
					return ErrPathOutsideRoot
				}
			}
		}

		if res, rerr := filepath.EvalSymlinks(cur); rerr == nil {
			if !isWithinRoot(l.rootPath, res) {
				return ErrPathOutsideRoot
			}
			return nil
		}

		if cur == l.rootPath {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return nil
}

func isWithinRoot(root, candidate string) bool {
	if root == candidate {
		return true
	}
	if root == string(filepath.Separator) {
		return strings.HasPrefix(candidate, root)
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}
