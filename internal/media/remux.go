package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"coursefin/internal/id"
	"coursefin/internal/storage"
)

const (
	// SegmentDurationSec is the fixed HLS VOD segment duration in seconds.
	SegmentDurationSec = 6

	remuxIdleTimeout  = 5 * time.Minute
	remuxReapInterval = 30 * time.Second
	remuxWaitTimeout  = 30 * time.Second
)

type remuxSession struct {
	key          string
	lessonID     string
	sessionID    string
	rawSessionID string
	dir          string

	mu           sync.Mutex
	closed       bool
	lastAccess   time.Time
	hasActiveCmd bool
	startSegment int
	generation   uint64
	cmdCancel    context.CancelFunc
	cmdDone      chan struct{}
	stoppingDone chan struct{}
	cmdErr       error
	cmdStderr    string
}

func (rs *remuxSession) stopAndRemove() {
	rs.mu.Lock()
	rs.closed = true
	rs.generation++
	cancel := rs.cmdCancel
	done := rs.cmdDone
	stopping := rs.stoppingDone
	rs.cmdCancel = nil
	rs.cmdDone = nil
	rs.hasActiveCmd = false
	rs.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	if stopping != nil {
		<-stopping
	}
	_ = os.RemoveAll(rs.dir)
}

func (s *Service) reapIdleSessionsLoop() {
	defer s.reaperWG.Done()
	ticker := time.NewTicker(remuxReapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.reapExpiredSessions()
		}
	}
}

func (s *Service) reapExpiredSessions() {
	now := time.Now()
	var expired []*remuxSession

	s.mu.Lock()
	for k, sess := range s.remuxSessions {
		sess.mu.Lock()
		idle := now.Sub(sess.lastAccess) > remuxIdleTimeout
		sess.mu.Unlock()
		if idle {
			expired = append(expired, sess)
			delete(s.remuxSessions, k)
		}
	}
	s.mu.Unlock()

	for _, sess := range expired {
		sess.stopAndRemove()
	}
}

// StopRemuxSession kills any active FFmpeg process associated with playSessionID
// and removes its cache directory under {dataDir}/cache/remux.
func (s *Service) StopRemuxSession(playSessionID string) {
	raw := strings.TrimSpace(playSessionID)
	safe := sanitizeSessionToken(raw)
	if raw == "" && safe == "" {
		return
	}

	var matched []*remuxSession
	s.mu.Lock()
	for k, sess := range s.remuxSessions {
		if (raw != "" && sess.rawSessionID == raw) || (safe != "" && sess.sessionID == safe) {
			matched = append(matched, sess)
			delete(s.remuxSessions, k)
		}
	}
	s.mu.Unlock()

	for _, sess := range matched {
		sess.stopAndRemove()
	}
}

// ServeMasterPlaylist serves a master HLS playlist pointing to main.m3u8 with
// PlaySessionId and api_key query parameters propagated.
func (s *Service) ServeMasterPlaylist(w http.ResponseWriter, r *http.Request, lessonID, sessionID string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rec, err := s.resolveMediaRecord(r.Context(), lessonID, true)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	transcodeVideo := isTranscodeVideoRequested(r)
	if transcodeVideo && !s.HasFFmpeg() {
		http.Error(w, "ffmpeg is not available for video transcoding", http.StatusServiceUnavailable)
		return
	}
	if !transcodeVideo && rec.VideoCodec != "" && !s.CanRemuxVideoCodec(rec.VideoCodec) {
		http.Error(w, "video codec cannot be remuxed for HLS playback", http.StatusUnsupportedMediaType)
		return
	}

	effectiveSession := resolveEffectiveSessionID(r, sessionID)
	if effectiveSession == "" {
		effectiveSession = id.New()
	}
	qs := propagateHLSQuery(r, effectiveSession)

	bandwidth := rec.Bitrate
	if bandwidth <= 0 {
		bandwidth = 4000000
	}

	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d", bandwidth))
	if rec.Width > 0 && rec.Height > 0 {
		sb.WriteString(fmt.Sprintf(",RESOLUTION=%dx%d", rec.Width, rec.Height))
	}
	sb.WriteByte('\n')
	sb.WriteString("main.m3u8")
	if qs != "" {
		sb.WriteByte('?')
		sb.WriteString(qs)
	}
	sb.WriteByte('\n')

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(sb.String()))
	}
}

// ServeMediaPlaylist serves a synthetic fixed-6s VOD HLS playlist covering the
// entire media duration with #EXT-X-ENDLIST and propagated auth/session query params.
func (s *Service) ServeMediaPlaylist(w http.ResponseWriter, r *http.Request, lessonID, sessionID string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rec, err := s.resolveMediaRecord(r.Context(), lessonID, true)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	transcodeVideo := isTranscodeVideoRequested(r)
	if transcodeVideo && !s.HasFFmpeg() {
		http.Error(w, "ffmpeg is not available for video transcoding", http.StatusServiceUnavailable)
		return
	}
	if !transcodeVideo && rec.VideoCodec != "" && !s.CanRemuxVideoCodec(rec.VideoCodec) {
		http.Error(w, "video codec cannot be remuxed for HLS playback", http.StatusUnsupportedMediaType)
		return
	}

	effectiveSession := resolveEffectiveSessionID(r, sessionID)
	if effectiveSession == "" {
		effectiveSession = id.New()
	}
	qs := propagateHLSQuery(r, effectiveSession)
	durations := buildSegmentDurations(rec.DurationMs)

	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", SegmentDurationSec))
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	sb.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	for idx, dur := range durations {
		sb.WriteString(fmt.Sprintf("#EXTINF:%.6f,\n", dur))
		sb.WriteString(fmt.Sprintf("hls1/main/%d.ts", idx))
		if qs != "" {
			sb.WriteByte('?')
			sb.WriteString(qs)
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("#EXT-X-ENDLIST\n")

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(sb.String()))
	}
}

// ServeHLSSegment serves segment segmentIndex (.ts), reusing an active FFmpeg
// remuxer within the lookahead window or restarting FFmpeg directly at -ss <segmentIndex*6>.
func (s *Service) ServeHLSSegment(w http.ResponseWriter, r *http.Request, lessonID, sessionID string, segmentIndex int) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if segmentIndex < 0 {
		http.Error(w, "invalid segment index", http.StatusBadRequest)
		return
	}

	rec, err := s.resolveMediaRecord(r.Context(), lessonID, true)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	transcodeVideo := isTranscodeVideoRequested(r)
	if transcodeVideo && !s.HasFFmpeg() {
		http.Error(w, "ffmpeg is not available for video transcoding", http.StatusServiceUnavailable)
		return
	}
	if !transcodeVideo && rec.VideoCodec != "" && !s.CanRemuxVideoCodec(rec.VideoCodec) {
		http.Error(w, "video codec cannot be remuxed for HLS playback", http.StatusUnsupportedMediaType)
		return
	}
	if rec.DurationMs > 0 {
		durations := buildSegmentDurations(rec.DurationMs)
		if segmentIndex >= len(durations) {
			http.Error(w, "segment index out of range", http.StatusNotFound)
			return
		}
	}

	provider, err := s.registry.ProviderFor(rec.Driver, rec.RootPath, rec.ConfigJSON)
	if err != nil {
		http.Error(w, fmt.Sprintf("storage provider error: %v", err), http.StatusInternalServerError)
		return
	}
	localResolver, ok := provider.(storage.LocalPathResolver)
	if !ok {
		http.Error(w, "HLS remux requires local filesystem access", http.StatusNotImplemented)
		return
	}
	absPath, err := localResolver.AbsPath(rec.RelativePath)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	normLessonID := normalizeOrRaw(lessonID)
	rawSession := strings.TrimSpace(sessionID)
	if rawSession == "" {
		rawSession = queryParamCI(r, "PlaySessionId")
	}
	safeSession := sanitizeSessionToken(rawSession)
	if safeSession == "" {
		safeSession = "default"
	}

	sess, err := s.getOrCreateRemuxSession(normLessonID, safeSession, rawSession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	segPath, statusCode, err := s.ensureSegmentReady(r.Context(), sess, absPath, rec.AudioCodec, segmentIndex, transcodeVideo)
	if err != nil {
		if statusCode == 0 {
			statusCode = http.StatusInternalServerError
		}
		http.Error(w, err.Error(), statusCode)
		return
	}

	f, err := os.Open(segPath)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeLookupError(w, err)
		return
	}

	sess.mu.Lock()
	sess.lastAccess = time.Now()
	sess.mu.Unlock()

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, fmt.Sprintf("%d.ts", segmentIndex), info.ModTime(), f)
}

func (s *Service) getOrCreateRemuxSession(lessonID, safeSession, rawSession string) (*remuxSession, error) {
	safeLesson := sanitizeSessionToken(lessonID)
	if safeLesson == "" {
		safeLesson = "lesson"
	}
	key := safeLesson + "_" + safeSession
	sessDir := filepath.Join(s.dataDir, "cache", "remux", key)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errors.New("media service is closed")
	}
	if existing, ok := s.remuxSessions[key]; ok {
		existing.mu.Lock()
		isClosed := existing.closed
		if !isClosed {
			existing.lastAccess = time.Now()
		}
		existing.mu.Unlock()
		if !isClosed {
			return existing, nil
		}
	}

	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return nil, fmt.Errorf("create remux session dir: %w", err)
	}

	sess := &remuxSession{
		key:          key,
		lessonID:     safeLesson,
		sessionID:    safeSession,
		rawSessionID: rawSession,
		dir:          sessDir,
		lastAccess:   time.Now(),
	}
	s.remuxSessions[key] = sess
	return sess, nil
}

func (s *Service) ensureSegmentReady(ctx context.Context, sess *remuxSession, absPath, audioCodec string, segmentIndex int, transcodeVideo bool) (string, int, error) {
	segPath := filepath.Join(sess.dir, fmt.Sprintf("%d.ts", segmentIndex))

	var gen uint64
	var done chan struct{}

	sess.mu.Lock()
	for {
		if sess.closed {
			sess.mu.Unlock()
			return "", http.StatusServiceUnavailable, errors.New("remux session stopped")
		}
		sess.lastAccess = time.Now()

		if segFileReady(segPath) {
			sess.mu.Unlock()
			return segPath, http.StatusOK, nil
		}

		if sess.stoppingDone != nil {
			waitStop := sess.stoppingDone
			sess.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", 499, ctx.Err()
			case <-s.stopCh:
				return "", http.StatusServiceUnavailable, errors.New("media service shutting down")
			case <-waitStop:
			}
			sess.mu.Lock()
			continue
		}

		if sess.hasActiveCmd {
			currentProd := activeRunMaxSegment(sess.dir, sess.startSegment)
			if segmentIndex >= sess.startSegment && segmentIndex <= currentProd+3 {
				gen = sess.generation
				done = sess.cmdDone
				sess.mu.Unlock()
				break
			}

			// Seek jump: supersede current generation before unlocking and cancelling so
			// any concurrent waiter on oldDone observes curGen != gen and returns 409 Conflict.
			sess.generation++
			cancel := sess.cmdCancel
			oldDone := sess.cmdDone
			sess.cmdCancel = nil
			sess.cmdDone = nil
			sess.hasActiveCmd = false
			sess.stoppingDone = oldDone
			sess.mu.Unlock()

			if cancel != nil {
				cancel()
			}
			if oldDone != nil {
				<-oldDone
			}

			sess.mu.Lock()
			if sess.stoppingDone == oldDone {
				sess.stoppingDone = nil
			}
			continue
		}

		cleanTempArtifacts(sess.dir)
		if err := os.MkdirAll(sess.dir, 0o755); err != nil {
			sess.mu.Unlock()
			return "", http.StatusInternalServerError, fmt.Errorf("ensure remux dir: %w", err)
		}

		var err error
		gen, done, err = s.launchFFmpegRemuxLocked(sess, absPath, audioCodec, segmentIndex, transcodeVideo)
		if err != nil {
			sess.mu.Unlock()
			return "", http.StatusInternalServerError, err
		}
		sess.mu.Unlock()
		break
	}

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(remuxWaitTimeout)
	defer timer.Stop()

	for {
		if segFileReady(segPath) {
			return segPath, http.StatusOK, nil
		}

		select {
		case <-ctx.Done():
			return "", 499, ctx.Err()
		case <-s.stopCh:
			return "", http.StatusServiceUnavailable, errors.New("media service shutting down")
		case <-done:
			if segFileReady(segPath) {
				return segPath, http.StatusOK, nil
			}
			sess.mu.Lock()
			closed := sess.closed
			curGen := sess.generation
			cmdErr := sess.cmdErr
			cmdStderr := strings.TrimSpace(sess.cmdStderr)
			sess.mu.Unlock()
			if closed {
				return "", http.StatusServiceUnavailable, errors.New("remux session stopped")
			}
			if curGen != gen {
				return "", http.StatusConflict, errors.New("remux session seeked to another position")
			}
			if cmdErr != nil {
				return "", http.StatusInternalServerError, fmt.Errorf("ffmpeg remux failed: %w (%s)", cmdErr, cmdStderr)
			}
			return "", http.StatusNotFound, fmt.Errorf("segment %d not produced by remuxer", segmentIndex)
		case <-timer.C:
			return "", http.StatusGatewayTimeout, fmt.Errorf("timed out waiting for segment %d", segmentIndex)
		case <-ticker.C:
		}
	}
}

func (s *Service) launchFFmpegRemuxLocked(sess *remuxSession, absPath, audioCodec string, startSegment int, transcodeVideo bool) (uint64, chan struct{}, error) {
	startSec := startSegment * SegmentDurationSec

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-ss", strconv.Itoa(startSec),
		"-i", absPath,
		"-map", "0:v:0",
		"-map", "0:a:0?",
	}
	if transcodeVideo {
		args = append(args,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "23",
			"-pix_fmt", "yuv420p",
			"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", SegmentDurationSec),
			"-sc_threshold", "0",
		)
	} else {
		args = append(args, "-c:v", "copy")
	}
	if s.NeedsAudioTranscode(audioCodec) {
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "192k")
	} else {
		args = append(args, "-c:a", "copy")
	}
	args = append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(SegmentDurationSec),
		"-start_number", strconv.Itoa(startSegment),
		"-hls_segment_filename", filepath.Join(sess.dir, "%d.ts"),
		"-hls_playlist_type", "event",
		"-hls_flags", "temp_file",
		"-output_ts_offset", strconv.Itoa(startSec),
		"-hls_segment_options", "mpegts_copyts=1",
		"-y",
		filepath.Join(sess.dir, "live.m3u8"),
	)

	cmdCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cmdCtx, s.ffmpegBin, args...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return 0, nil, fmt.Errorf("start ffmpeg remux: %w", err)
	}

	sess.generation++
	gen := sess.generation
	done := make(chan struct{})
	sess.hasActiveCmd = true
	sess.startSegment = startSegment
	sess.cmdCancel = cancel
	sess.cmdDone = done
	sess.cmdErr = nil
	sess.cmdStderr = ""

	go func(g uint64, c *exec.Cmd, ch chan struct{}, buf *bytes.Buffer) {
		err := c.Wait()
		sess.mu.Lock()
		if sess.generation == g {
			sess.hasActiveCmd = false
			sess.cmdCancel = nil
			sess.cmdDone = nil
			sess.cmdErr = err
			sess.cmdStderr = buf.String()
		}
		sess.mu.Unlock()
		cancel()
		close(ch)
	}(gen, cmd, done, &stderrBuf)

	return gen, done, nil
}

func segFileReady(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// activeRunMaxSegment returns the highest segment index >= startSegment
// produced or currently being written by the active FFmpeg run, ignoring stale
// finalized *.ts files from prior seek positions.
func activeRunMaxSegment(dir string, startSegment int) int {
	highest := startSegment

	if raw, err := os.ReadFile(filepath.Join(dir, "live.m3u8")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			base := filepath.Base(line)
			if !strings.HasSuffix(base, ".ts") {
				continue
			}
			if n, err := strconv.Atoi(strings.TrimSuffix(base, ".ts")); err == nil && n >= startSegment && n > highest {
				highest = n
			}
		}
	}

	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".ts.tmp") {
				continue
			}
			if n, err := strconv.Atoi(strings.TrimSuffix(name, ".ts.tmp")); err == nil && n >= startSegment && n > highest {
				highest = n
			}
		}
	}

	return highest
}

func cleanTempArtifacts(dir string) {
	_ = os.Remove(filepath.Join(dir, "live.m3u8"))
	_ = os.Remove(filepath.Join(dir, "live.m3u8.tmp"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".tmp") {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func buildSegmentDurations(durationMs int64) []float64 {
	if durationMs <= 0 {
		return []float64{float64(SegmentDurationSec)}
	}
	segMs := int64(SegmentDurationSec) * 1000
	var out []float64
	for offset := int64(0); offset < durationMs; offset += segMs {
		rem := durationMs - offset
		if rem <= 50 && len(out) > 0 {
			break
		}
		if rem >= segMs {
			out = append(out, float64(SegmentDurationSec))
		} else {
			out = append(out, float64(rem)/1000.0)
		}
	}
	if len(out) == 0 {
		out = append(out, float64(durationMs)/1000.0)
	}
	return out
}

func resolveEffectiveSessionID(r *http.Request, sessionID string) string {
	if s := strings.TrimSpace(sessionID); s != "" {
		return s
	}
	if r != nil {
		return strings.TrimSpace(queryParamCI(r, "PlaySessionId"))
	}
	return ""
}

func isTranscodeVideoRequested(r *http.Request) bool {
	raw := strings.ToLower(strings.TrimSpace(queryParamCI(r, "TranscodeVideo")))
	return raw == "1" || raw == "true"
}

func propagateHLSQuery(r *http.Request, sessionID string) string {
	vals := url.Values{}
	if sessionID != "" {
		vals.Set("PlaySessionId", sessionID)
	}
	if r != nil {
		if apiKey := strings.TrimSpace(queryParamCI(r, "api_key")); apiKey != "" {
			vals.Set("api_key", apiKey)
		}
		if msID := strings.TrimSpace(queryParamCI(r, "MediaSourceId")); msID != "" {
			vals.Set("MediaSourceId", msID)
		}
		if isTranscodeVideoRequested(r) {
			vals.Set("TranscodeVideo", "1")
		}
	}
	return vals.Encode()
}

func queryParamCI(r *http.Request, key string) string {
	if r == nil || r.URL == nil {
		return ""
	}
	q := r.URL.Query()
	if v := q.Get(key); v != "" {
		return v
	}
	for k, vs := range q {
		if strings.EqualFold(k, key) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

func sanitizeSessionToken(raw string) string {
	var sb strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}
