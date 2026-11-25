package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"coursefin/internal/id"
	"coursefin/internal/storage"
)

// Service provides DirectPlay HTTP Range streaming, seekable on-demand VOD HLS
// remuxing, subtitle delivery/conversion, and resized image caching.
type Service struct {
	db         *sql.DB
	dataDir    string
	registry   *storage.Registry
	ffmpegBin  string
	hasFFmpeg  bool
	ffprobeBin string
	hasFFprobe bool

	mu            sync.Mutex
	remuxSessions map[string]*remuxSession
	closed        bool
	stopCh        chan struct{}
	closeOnce     sync.Once
	reaperWG      sync.WaitGroup
}

// NewService constructs a media Service and starts the background remux session reaper.
func NewService(db *sql.DB, dataDir string, registry *storage.Registry, ffmpegBin string) *Service {
	if registry == nil {
		registry = storage.NewRegistry()
	}
	resolved := resolveBinaryPath(ffmpegBin, "ffmpeg")
	var ffprobeResolved string
	if strings.ContainsRune(resolved, filepath.Separator) {
		cand := filepath.Join(filepath.Dir(resolved), "ffprobe")
		if isExecutableBinary(cand) {
			ffprobeResolved = cand
		}
	}
	if ffprobeResolved == "" {
		ffprobeResolved = resolveBinaryPath("", "ffprobe")
	}
	s := &Service{
		db:            db,
		dataDir:       dataDir,
		registry:      registry,
		ffmpegBin:     resolved,
		hasFFmpeg:     isExecutableBinary(resolved),
		ffprobeBin:    ffprobeResolved,
		hasFFprobe:    isExecutableBinary(ffprobeResolved),
		remuxSessions: make(map[string]*remuxSession),
		stopCh:        make(chan struct{}),
	}
	s.reaperWG.Add(1)
	go s.reapIdleSessionsLoop()
	return s
}

// HasFFmpeg reports whether a usable FFmpeg binary is available for remuxing/transcoding.
func (s *Service) HasFFmpeg() bool {
	return s != nil && s.hasFFmpeg
}

// FFmpegBin returns the resolved FFmpeg binary path.
func (s *Service) FFmpegBin() string {
	if s == nil {
		return ""
	}
	return s.ffmpegBin
}

// FFprobeBin returns the resolved FFprobe binary path.
func (s *Service) FFprobeBin() string {
	if s == nil {
		return ""
	}
	return s.ffprobeBin
}

// HasFFprobe reports whether a usable FFprobe binary is available.
func (s *Service) HasFFprobe() bool {
	return s != nil && s.hasFFprobe
}

func isExecutableBinary(bin string) bool {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return false
	}
	if strings.ContainsRune(bin, filepath.Separator) {
		info, err := os.Stat(bin)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// Close stops the remux reaper, terminates all active FFmpeg processes, and cleans
// up temporary files under {dataDir}/cache/remux.
func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		sessions := make([]*remuxSession, 0, len(s.remuxSessions))
		for k, sess := range s.remuxSessions {
			sessions = append(sessions, sess)
			delete(s.remuxSessions, k)
		}
		s.mu.Unlock()

		close(s.stopCh)
		s.reaperWG.Wait()

		for _, sess := range sessions {
			sess.stopAndRemove()
		}

		if s.dataDir != "" {
			remuxRoot := filepath.Join(s.dataDir, "cache", "remux")
			if entries, err := os.ReadDir(remuxRoot); err == nil {
				for _, entry := range entries {
					_ = os.RemoveAll(filepath.Join(remuxRoot, entry.Name()))
				}
			}
		}
	})
	return nil
}

// CanRemuxVideoCodec reports whether the video bitstream can be stream-copied
// into HLS without CPU video re-encoding for modern web/Jellyfin players.
func CanRemuxVideoCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "avc1", "hevc", "h265", "av1", "vp9":
		return true
	default:
		return false
	}
}

// CanRemuxVideoCodec reports whether the video bitstream can be stream-copied
// into HLS without CPU video re-encoding for modern web/Jellyfin players.
func (s *Service) CanRemuxVideoCodec(codec string) bool {
	return CanRemuxVideoCodec(codec)
}

// CanWebRemuxVideoCodec reports whether the video bitstream can be stream-copied
// into MPEG-TS HLS segments that hls.js and browsers can reliably demux.
func CanWebRemuxVideoCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "avc1":
		return true
	default:
		return false
	}
}

// CanWebRemuxVideoCodec reports whether the video bitstream can be stream-copied
// into MPEG-TS HLS segments that hls.js and browsers can reliably demux.
func (s *Service) CanWebRemuxVideoCodec(codec string) bool {
	return CanWebRemuxVideoCodec(codec)
}

// NeedsAudioTranscode reports whether the audio codec must be transcoded to AAC
// during MPEG-TS HLS remuxing for browser compatibility.
func NeedsAudioTranscode(codec string) bool {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch c {
	case "", "aac", "mp3":
		return false
	default:
		return true
	}
}

// NeedsAudioTranscode reports whether the audio codec must be transcoded to AAC
// during MPEG-TS HLS remuxing for browser compatibility.
func (s *Service) NeedsAudioTranscode(codec string) bool {
	return NeedsAudioTranscode(codec)
}

// ServeDirectPlay serves a media file via storage DirectURL redirect (302) or
// HTTP Range-capable streaming (HEAD, 206 Partial Content, ETag, Accept-Ranges).
func (s *Service) ServeDirectPlay(w http.ResponseWriter, r *http.Request, mediaFileID string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rec, err := s.resolveMediaRecord(r.Context(), mediaFileID, false)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	provider, err := s.registry.ProviderFor(rec.Driver, rec.RootPath, rec.ConfigJSON)
	if err != nil {
		http.Error(w, fmt.Sprintf("storage provider error: %v", err), http.StatusInternalServerError)
		return
	}

	directURL, ok, err := provider.DirectURL(r.Context(), rec.RelativePath)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	if ok && directURL != "" {
		http.Redirect(w, r, directURL, http.StatusFound)
		return
	}

	info, err := provider.Stat(r.Context(), rec.RelativePath)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	if info.IsDir {
		http.Error(w, "media target is a directory", http.StatusNotFound)
		return
	}

	rc, err := provider.Open(r.Context(), rec.RelativePath)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	defer rc.Close()

	modTime := info.ModTime
	if modTime.IsZero() && rec.ModTime != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, rec.ModTime); perr == nil {
			modTime = parsed
		}
	}

	etag := fmt.Sprintf(`"%s-%d-%d"`, rec.MediaFileID, info.Size, modTime.UnixNano())
	w.Header().Set("ETag", etag)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", mimeTypeForMedia(rec.RelativePath, rec.Container))

	name := rec.FileName
	if name == "" {
		name = path.Base(rec.RelativePath)
	}
	http.ServeContent(w, r, name, modTime, rc)
}

type mediaRecord struct {
	MediaFileID   string
	SourceID      string
	Driver        string
	RootPath      string
	ConfigJSON    string
	RelativePath  string
	FileName      string
	FileSize      int64
	ModTime       string
	Status        string
	DurationMs    int64
	Container     string
	VideoCodec    string
	AudioCodec    string
	Width         int
	Height        int
	Bitrate       int64
	StreamsJSON   string
	ThumbnailPath string
}

func (s *Service) resolveMediaRecord(ctx context.Context, rawID string, preferLesson bool) (*mediaRecord, error) {
	targetID := normalizeOrRaw(rawID)
	if targetID == "" {
		return nil, sql.ErrNoRows
	}

	queryByMediaID := func() (*mediaRecord, error) {
		var rec mediaRecord
		err := s.db.QueryRowContext(ctx, `
			SELECT mf.id, mf.source_id, ss.driver, ss.root_path, ss.config_json,
			       mf.relative_path, mf.file_name, mf.file_size, mf.mod_time, mf.status,
			       mf.duration_ms, mf.container, mf.video_codec, mf.audio_codec,
			       mf.width, mf.height, mf.bitrate, mf.streams_json, mf.thumbnail_path
			FROM media_files mf
			JOIN storage_sources ss ON ss.id = mf.source_id
			WHERE mf.id = ?`, targetID,
		).Scan(
			&rec.MediaFileID, &rec.SourceID, &rec.Driver, &rec.RootPath, &rec.ConfigJSON,
			&rec.RelativePath, &rec.FileName, &rec.FileSize, &rec.ModTime, &rec.Status,
			&rec.DurationMs, &rec.Container, &rec.VideoCodec, &rec.AudioCodec,
			&rec.Width, &rec.Height, &rec.Bitrate, &rec.StreamsJSON, &rec.ThumbnailPath,
		)
		if err != nil {
			return nil, err
		}
		return &rec, nil
	}

	queryByLessonID := func() (*mediaRecord, error) {
		var rec mediaRecord
		err := s.db.QueryRowContext(ctx, `
			SELECT mf.id, mf.source_id, ss.driver, ss.root_path, ss.config_json,
			       mf.relative_path, mf.file_name, mf.file_size, mf.mod_time, mf.status,
			       mf.duration_ms, mf.container, mf.video_codec, mf.audio_codec,
			       mf.width, mf.height, mf.bitrate, mf.streams_json, mf.thumbnail_path
			FROM lessons l
			JOIN media_files mf ON mf.id = l.media_file_id
			JOIN storage_sources ss ON ss.id = mf.source_id
			WHERE l.id = ?`, targetID,
		).Scan(
			&rec.MediaFileID, &rec.SourceID, &rec.Driver, &rec.RootPath, &rec.ConfigJSON,
			&rec.RelativePath, &rec.FileName, &rec.FileSize, &rec.ModTime, &rec.Status,
			&rec.DurationMs, &rec.Container, &rec.VideoCodec, &rec.AudioCodec,
			&rec.Width, &rec.Height, &rec.Bitrate, &rec.StreamsJSON, &rec.ThumbnailPath,
		)
		if err != nil {
			return nil, err
		}
		return &rec, nil
	}

	if preferLesson {
		rec, err := queryByLessonID()
		if err == nil {
			return rec, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return queryByMediaID()
	}

	rec, err := queryByMediaID()
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return queryByLessonID()
}

func writeLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrPathOutsideRoot):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, fs.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func normalizeOrRaw(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if norm, ok := id.Normalize(trimmed); ok {
		return norm
	}
	return trimmed
}

func resolveBinaryPath(configured, defaultName string) string {
	cand := strings.TrimSpace(configured)
	if cand == "" {
		cand = defaultName
	}
	if strings.ContainsRune(cand, filepath.Separator) {
		return cand
	}
	if p, err := exec.LookPath(cand); err == nil {
		return p
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		full := filepath.Join(dir, cand)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full
		}
	}
	return cand
}

func mimeTypeForMedia(relPath, container string) string {
	ext := strings.ToLower(path.Ext(relPath))
	if ext == "" && container != "" {
		ext = "." + strings.ToLower(strings.TrimPrefix(container, "."))
	}
	switch ext {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".ts":
		return "video/mp2t"
	case ".flv":
		return "video/x-flv"
	case ".avi":
		return "video/x-msvideo"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".rmvb":
		return "application/vnd.rn-realmedia-vbr"
	}
	if ext != "" {
		if m := mime.TypeByExtension(ext); m != "" {
			return m
		}
	}
	return "application/octet-stream"
}
