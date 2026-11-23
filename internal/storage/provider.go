package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var (
	ErrPathOutsideRoot   = errors.New("path escapes storage root")
	ErrUnsupportedDriver = errors.New("unsupported storage driver")
)

// FileEntry represents a file or directory discovered during a storage walk.
type FileEntry struct {
	RelPath string
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// FileInfo describes metadata for a single file or directory in storage.
type FileInfo struct {
	RelPath string
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// StorageProvider abstracts physical media storage backends (LocalFS in v1,
// extensible to WebDAV, Alist/OpenList, S3, etc.).
type StorageProvider interface {
	Walk(ctx context.Context, rootRelPath string) ([]FileEntry, error)
	Stat(ctx context.Context, relPath string) (FileInfo, error)
	Open(ctx context.Context, relPath string) (io.ReadSeekCloser, error)
	DirectURL(ctx context.Context, relPath string) (string, bool, error)
}

// LocalPathResolver is an optional capability implemented by local filesystem
// providers to expose a validated absolute path for tools such as ffmpeg/ffprobe.
type LocalPathResolver interface {
	AbsPath(relPath string) (string, error)
}

// DriverFactory constructs a StorageProvider for a registered driver name.
type DriverFactory func(rootPath, configJSON string) (StorageProvider, error)

// Registry maps storage driver identifiers to StorageProvider factories.
type Registry struct {
	mu      sync.RWMutex
	drivers map[string]DriverFactory
}

// NewRegistry returns a Registry pre-populated with built-in drivers ("local").
func NewRegistry() *Registry {
	r := &Registry{
		drivers: make(map[string]DriverFactory),
	}
	r.Register("local", func(rootPath, configJSON string) (StorageProvider, error) {
		return NewLocalFS(rootPath)
	})
	return r
}

// Register adds or replaces a storage driver factory.
func (r *Registry) Register(driver string, factory DriverFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drivers == nil {
		r.drivers = make(map[string]DriverFactory)
	}
	r.drivers[strings.ToLower(strings.TrimSpace(driver))] = factory
}

// ProviderFor instantiates a StorageProvider for the given driver and rootPath.
func (r *Registry) ProviderFor(driver, rootPath, configJSON string) (StorageProvider, error) {
	key := strings.ToLower(strings.TrimSpace(driver))
	if key == "" {
		key = "local"
	}

	r.mu.RLock()
	factory := r.drivers[key]
	r.mu.RUnlock()

	if factory == nil && key == "local" {
		return NewLocalFS(rootPath)
	}
	if factory == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, driver)
	}
	return factory(rootPath, configJSON)
}
