package scanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"coursefin/internal/id"
	"coursefin/internal/storage"
)

const defaultProbeWorkers = 4

var (
	ErrScanInProgress = errors.New("scan already in progress for source")
	ErrSourceNotFound = errors.New("storage source not found")
)

// ScanReport summarizes the outcome of a single source scan.
type ScanReport struct {
	SourceID       string    `json:"source_id"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	TotalVideos    int       `json:"total_videos"`
	AddedVideos    int       `json:"added_videos"`
	UpdatedVideos  int       `json:"updated_videos"`
	SkippedVideos  int       `json:"skipped_videos"`
	MissingVideos  int       `json:"missing_videos"`
	RestoredVideos int       `json:"restored_videos"`
	ProbedVideos   int       `json:"probed_videos"`
	TotalSubtitles int       `json:"total_subtitles"`
	Errors         []string  `json:"errors,omitempty"`
}

// ScanState captures the live and most recent scan status for a storage source.
type ScanState struct {
	SourceID   string      `json:"source_id"`
	Running    bool        `json:"running"`
	StartedAt  *time.Time  `json:"started_at,omitempty"`
	LastReport *ScanReport `json:"last_report,omitempty"`
	LastError  string      `json:"last_error,omitempty"`
}

// Service performs incremental media discovery, probing, thumbnail extraction,
// and sidecar subtitle matching across configured storage sources.
type Service struct {
	db       *sql.DB
	dataDir  string
	registry *storage.Registry
	prober   Prober
	workers  int

	mu      sync.Mutex
	wg      sync.WaitGroup
	running map[string]bool
	states  map[string]ScanState
}

// NewService constructs a scanner Service.
func NewService(db *sql.DB, dataDir string, registry *storage.Registry, prober Prober) *Service {
	if registry == nil {
		registry = storage.NewRegistry()
	}
	return &Service{
		db:       db,
		dataDir:  dataDir,
		registry: registry,
		prober:   prober,
		workers:  defaultProbeWorkers,
		running:  make(map[string]bool),
		states:   make(map[string]ScanState),
	}
}

// Status returns the current scan state for sourceID.
func (s *Service) Status(sourceID string) ScanState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.states[sourceID]; ok {
		return st
	}
	return ScanState{SourceID: sourceID}
}

// Wait blocks until all background scans started via StartScanAsync have finished.
func (s *Service) Wait() {
	s.wg.Wait()
}

// StartPeriodicLoop runs ScanAllEnabled on the schedule returned by intervalFn
// until ctx is cancelled.
func (s *Service) StartPeriodicLoop(ctx context.Context, intervalFn func() time.Duration) {
	for {
		interval := time.Minute
		if intervalFn != nil {
			if d := intervalFn(); d > 0 {
				interval = d
			}
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.ScanAllEnabled(ctx)
		}
	}
}

// ScanAllEnabled scans every enabled storage source sequentially.
func (s *Service) ScanAllEnabled(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM storage_sources WHERE enabled = 1 ORDER BY created_at ASC`)
	if err != nil {
		return
	}
	var sourceIDs []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err == nil {
			sourceIDs = append(sourceIDs, sid)
		}
	}
	rows.Close()

	for _, sid := range sourceIDs {
		if ctx.Err() != nil {
			return
		}
		_, _ = s.ScanSource(ctx, sid)
	}
}

type sourceRecord struct {
	id         string
	driver     string
	rootPath   string
	configJSON string
}

type existingMediaRecord struct {
	id            string
	relativePath  string
	fileSize      int64
	modTime       string
	status        string
	thumbnailPath string
	probedAt      sql.NullString
}

type existingSubtitleRecord struct {
	id           string
	relativePath string
	status       string
}

type probeJob struct {
	id            string
	isNew         bool
	wasMissing    bool
	entry         storage.FileEntry
	modTimeStr    string
	prevThumbPath string
}

type probeJobResult struct {
	job       probeJob
	probe     *ProbeResult
	thumbPath string
	errMsg    string
}

func (s *Service) beginScan(sourceID string, startedAt time.Time) (ScanState, ScanState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[sourceID] {
		return ScanState{}, ScanState{}, ErrScanInProgress
	}
	s.running[sourceID] = true
	prevState := s.states[sourceID]
	st := ScanState{
		SourceID:   sourceID,
		Running:    true,
		StartedAt:  &startedAt,
		LastReport: prevState.LastReport,
	}
	s.states[sourceID] = st
	return st, prevState, nil
}

// StartScanAsync validates sourceID, atomically marks the scan as running, and
// launches the scan in a tracked background goroutine bound to ctx.
func (s *Service) StartScanAsync(ctx context.Context, sourceID string) (ScanState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ScanState{}, err
	}
	src, err := s.loadSource(ctx, sourceID)
	if err != nil {
		return ScanState{}, err
	}

	startedAt := time.Now().UTC()
	st, prevState, err := s.beginScan(sourceID, startedAt)
	if err != nil {
		return ScanState{}, err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_, _ = s.runScanLocked(ctx, sourceID, src, startedAt, prevState)
	}()
	return st, nil
}

// ScanSource performs an incremental scan of a single storage source.
func (s *Service) ScanSource(ctx context.Context, sourceID string) (*ScanReport, error) {
	startedAt := time.Now().UTC()
	_, prevState, err := s.beginScan(sourceID, startedAt)
	if err != nil {
		return nil, err
	}
	return s.runScanLocked(ctx, sourceID, nil, startedAt, prevState)
}

func (s *Service) runScanLocked(ctx context.Context, sourceID string, preloadedSrc *sourceRecord, startedAt time.Time, prevState ScanState) (*ScanReport, error) {
	var report *ScanReport
	var scanErr error
	defer func() {
		s.mu.Lock()
		delete(s.running, sourceID)
		st := ScanState{
			SourceID:   sourceID,
			Running:    false,
			StartedAt:  &startedAt,
			LastReport: prevState.LastReport,
		}
		if report != nil {
			st.LastReport = report
		}
		if scanErr != nil {
			st.LastError = scanErr.Error()
		}
		s.states[sourceID] = st
		s.mu.Unlock()
	}()

	src := preloadedSrc
	if src == nil {
		var err error
		src, err = s.loadSource(ctx, sourceID)
		if err != nil {
			scanErr = err
			return nil, err
		}
	}

	provider, err := s.registry.ProviderFor(src.driver, src.rootPath, src.configJSON)
	if err != nil {
		scanErr = err
		return nil, err
	}

	entries, err := provider.Walk(ctx, "")
	if err != nil {
		scanErr = fmt.Errorf("walk source %s: %w", sourceID, err)
		return nil, scanErr
	}

	var videoEntries []storage.FileEntry
	var subEntries []storage.FileEntry
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		ext := path.Ext(entry.Name)
		if IsVideoExt(ext) {
			videoEntries = append(videoEntries, entry)
		} else if IsSubtitleExt(ext) {
			subEntries = append(subEntries, entry)
		}
	}

	existingMedia, err := s.loadExistingMedia(ctx, sourceID)
	if err != nil {
		scanErr = err
		return nil, err
	}
	existingSubs, err := s.loadExistingSubtitles(ctx, sourceID)
	if err != nil {
		scanErr = err
		return nil, err
	}

	rep := &ScanReport{
		SourceID:       sourceID,
		StartedAt:      startedAt,
		TotalVideos:    len(videoEntries),
		TotalSubtitles: len(subEntries),
	}

	seenVideos := make(map[string]bool, len(videoEntries))
	videoIDByRel := make(map[string]string, len(videoEntries))
	var jobs []probeJob
	var restoreIDs []string

	for _, entry := range videoEntries {
		seenVideos[entry.RelPath] = true
		modStr := entry.ModTime.UTC().Format(time.RFC3339Nano)

		ex, exists := existingMedia[entry.RelPath]
		if !exists {
			mediaID := id.New()
			videoIDByRel[entry.RelPath] = mediaID
			rep.AddedVideos++
			jobs = append(jobs, probeJob{
				id:         mediaID,
				isNew:      true,
				entry:      entry,
				modTimeStr: modStr,
			})
			continue
		}

		videoIDByRel[entry.RelPath] = ex.id
		unchanged := ex.fileSize == entry.Size &&
			ex.modTime == modStr &&
			ex.probedAt.Valid &&
			ex.probedAt.String != ""

		if unchanged {
			rep.SkippedVideos++
			if ex.status != "active" {
				restoreIDs = append(restoreIDs, ex.id)
				rep.RestoredVideos++
			}
			continue
		}

		wasMissing := ex.status != "active"
		if wasMissing {
			rep.RestoredVideos++
		} else {
			rep.UpdatedVideos++
		}

		jobs = append(jobs, probeJob{
			id:            ex.id,
			isNew:         false,
			wasMissing:    wasMissing,
			entry:         entry,
			modTimeStr:    modStr,
			prevThumbPath: ex.thumbnailPath,
		})
	}

	var missingVideoIDs []string
	for rel, ex := range existingMedia {
		if !seenVideos[rel] && ex.status != "missing" {
			missingVideoIDs = append(missingVideoIDs, ex.id)
			rep.MissingVideos++
		}
	}

	results := s.runProbeWorkers(ctx, src, provider, jobs)
	for _, r := range results {
		if r.probe != nil {
			rep.ProbedVideos++
		}
		if r.errMsg != "" {
			rep.Errors = append(rep.Errors, r.errMsg)
		}
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		scanErr = ctxErr
		return nil, ctxErr
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	if err := s.persistScanResults(ctx, sourceID, nowStr, results, restoreIDs, missingVideoIDs, videoEntries, videoIDByRel, subEntries, existingSubs); err != nil {
		scanErr = err
		return nil, err
	}

	rep.FinishedAt = time.Now().UTC()
	report = rep
	return rep, nil
}

func (s *Service) runProbeWorkers(ctx context.Context, src *sourceRecord, provider storage.StorageProvider, jobs []probeJob) []probeJobResult {
	if len(jobs) == 0 {
		return nil
	}

	workerCount := s.workers
	if workerCount < 1 {
		workerCount = 1
	}
	if len(jobs) < workerCount {
		workerCount = len(jobs)
	}

	jobCh := make(chan int, len(jobs))
	results := make([]probeJobResult, len(jobs))
	for i := range jobs {
		jobCh <- i
	}
	close(jobCh)

	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobCh {
				if ctx.Err() != nil {
					return
				}
				results[idx] = s.processProbeJob(ctx, src, provider, jobs[idx])
			}
		}()
	}
	wg.Wait()
	return results
}

func (s *Service) processProbeJob(ctx context.Context, src *sourceRecord, provider storage.StorageProvider, job probeJob) probeJobResult {
	res := probeJobResult{
		job:       job,
		thumbPath: job.prevThumbPath,
	}

	if s.prober == nil {
		return res
	}

	var absPath string
	var err error
	if resolver, ok := provider.(storage.LocalPathResolver); ok {
		absPath, err = resolver.AbsPath(job.entry.RelPath)
		if err != nil {
			res.errMsg = fmt.Sprintf("resolve %s: %v", job.entry.RelPath, err)
			return res
		}
	} else {
		absPath = filepath.Join(src.rootPath, filepath.FromSlash(job.entry.RelPath))
	}

	probeRes, err := s.prober.Probe(ctx, absPath)
	if err != nil {
		res.errMsg = fmt.Sprintf("probe %s: %v", job.entry.RelPath, err)
		return res
	}
	res.probe = probeRes

	seekSec := calculateThumbnailSeek(probeRes.DurationMs)
	thumbName := job.id + ".jpg"
	thumbOutPath := filepath.Join(s.dataDir, "thumbs", thumbName)
	if err := s.prober.ExtractThumbnail(ctx, absPath, seekSec, thumbOutPath); err != nil {
		res.errMsg = fmt.Sprintf("thumbnail %s: %v", job.entry.RelPath, err)
	} else {
		res.thumbPath = thumbName
	}

	return res
}

func calculateThumbnailSeek(durationMs int64) float64 {
	durationSec := float64(durationMs) / 1000.0
	if durationSec < 5.0 {
		return 0
	}
	seek := durationSec * 0.10
	if seek < 5.0 {
		return 5.0
	}
	if seek > 60.0 {
		return 60.0
	}
	return seek
}

func (s *Service) persistScanResults(
	ctx context.Context,
	sourceID string,
	nowStr string,
	results []probeJobResult,
	restoreIDs []string,
	missingVideoIDs []string,
	videoEntries []storage.FileEntry,
	videoIDByRel map[string]string,
	subEntries []storage.FileEntry,
	existingSubs map[string]existingSubtitleRecord,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin scan tx: %w", err)
	}
	defer tx.Rollback()

	for _, r := range results {
		job := r.job
		var (
			durationMs  int64
			container   string
			videoCodec  string
			audioCodec  string
			width       int
			height      int
			bitrate     int64
			streamsJSON = "[]"
			probedAt    sql.NullString
		)
		if r.probe != nil {
			durationMs = r.probe.DurationMs
			container = r.probe.Container
			videoCodec = r.probe.VideoCodec
			audioCodec = r.probe.AudioCodec
			width = r.probe.Width
			height = r.probe.Height
			bitrate = r.probe.Bitrate
			if strings.TrimSpace(r.probe.StreamsJSON) != "" {
				streamsJSON = r.probe.StreamsJSON
			}
			probedAt = sql.NullString{String: nowStr, Valid: true}
		}

		if job.isNew {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO media_files (
					id, source_id, relative_path, file_name, file_size, mod_time, status,
					duration_ms, container, video_codec, audio_codec, width, height, bitrate,
					streams_json, thumbnail_path, probed_at, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				job.id, sourceID, job.entry.RelPath, job.entry.Name, job.entry.Size, job.modTimeStr,
				durationMs, container, videoCodec, audioCodec, width, height, bitrate,
				streamsJSON, r.thumbPath, probedAt, nowStr, nowStr,
			); err != nil {
				return fmt.Errorf("insert media_file %s: %w", job.entry.RelPath, err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE media_files
				SET file_name = ?, file_size = ?, mod_time = ?, status = 'active',
				    duration_ms = ?, container = ?, video_codec = ?, audio_codec = ?,
				    width = ?, height = ?, bitrate = ?, streams_json = ?,
				    thumbnail_path = ?, probed_at = ?, updated_at = ?
				WHERE id = ?`,
				job.entry.Name, job.entry.Size, job.modTimeStr,
				durationMs, container, videoCodec, audioCodec,
				width, height, bitrate, streamsJSON,
				r.thumbPath, probedAt, nowStr, job.id,
			); err != nil {
				return fmt.Errorf("update media_file %s: %w", job.entry.RelPath, err)
			}
		}
	}

	for _, idToRestore := range restoreIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE media_files SET status = 'active', updated_at = ? WHERE id = ?`, nowStr, idToRestore); err != nil {
			return fmt.Errorf("restore media_file %s: %w", idToRestore, err)
		}
	}

	for _, idMissing := range missingVideoIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE media_files SET status = 'missing', updated_at = ? WHERE id = ?`, nowStr, idMissing); err != nil {
			return fmt.Errorf("mark missing media_file %s: %w", idMissing, err)
		}
	}

	if err := syncSubtitlesTx(ctx, tx, sourceID, videoEntries, videoIDByRel, subEntries, existingSubs); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE storage_sources SET last_scanned_at = ?, updated_at = ? WHERE id = ?`, nowStr, nowStr, sourceID); err != nil {
		return fmt.Errorf("update storage_sources.last_scanned_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit scan tx: %w", err)
	}
	return nil
}

func syncSubtitlesTx(
	ctx context.Context,
	tx *sql.Tx,
	sourceID string,
	videoEntries []storage.FileEntry,
	videoIDByRel map[string]string,
	subEntries []storage.FileEntry,
	existingSubs map[string]existingSubtitleRecord,
) error {
	videosByDir := make(map[string][]string)
	for _, v := range videoEntries {
		dir := path.Dir(v.RelPath)
		videosByDir[dir] = append(videosByDir[dir], v.RelPath)
	}
	// Prefer longer stems first so e.g. 01_intro.2.mp4 beats 01_intro.mp4 when matching 01_intro.2.srt.
	for dir := range videosByDir {
		sort.Slice(videosByDir[dir], func(i, j int) bool {
			li := len(strings.TrimSuffix(path.Base(videosByDir[dir][i]), path.Ext(videosByDir[dir][i])))
			lj := len(strings.TrimSuffix(path.Base(videosByDir[dir][j]), path.Ext(videosByDir[dir][j])))
			if li != lj {
				return li > lj
			}
			return videosByDir[dir][i] < videosByDir[dir][j]
		})
	}

	seenSubs := make(map[string]bool, len(subEntries))
	for _, sub := range subEntries {
		seenSubs[sub.RelPath] = true
		dir := path.Dir(sub.RelPath)
		candidates := videosByDir[dir]

		matchedVideoID := sql.NullString{}
		var match SubtitleMatch
		var found bool

		// First pass: exact stem match.
		subExt := path.Ext(sub.RelPath)
		subStem := strings.TrimSuffix(path.Base(sub.RelPath), subExt)
		for _, vidRel := range candidates {
			vidExt := path.Ext(vidRel)
			vidStem := strings.TrimSuffix(path.Base(vidRel), vidExt)
			if strings.EqualFold(subStem, vidStem) {
				if m, ok := MatchSubtitle(vidRel, sub.RelPath); ok {
					match = m
					matchedVideoID = sql.NullString{String: videoIDByRel[vidRel], Valid: true}
					found = true
					break
				}
			}
		}

		// Second pass: stem + language/modifier match.
		if !found {
			for _, vidRel := range candidates {
				if m, ok := MatchSubtitle(vidRel, sub.RelPath); ok {
					match = m
					matchedVideoID = sql.NullString{String: videoIDByRel[vidRel], Valid: true}
					found = true
					break
				}
			}
		}

		if !found {
			match = SubtitleMatch{
				RelPath:  sub.RelPath,
				Language: "und",
				Format:   strings.TrimPrefix(strings.ToLower(subExt), "."),
			}
		}

		isDef := 0
		if match.IsDefault {
			isDef = 1
		}

		if ex, exists := existingSubs[sub.RelPath]; exists {
			if _, err := tx.ExecContext(ctx, `
				UPDATE subtitle_files
				SET media_file_id = ?, language = ?, title = ?, format = ?, is_default = ?, status = 'active'
				WHERE id = ?`,
				matchedVideoID, match.Language, match.Title, match.Format, isDef, ex.id,
			); err != nil {
				return fmt.Errorf("update subtitle_file %s: %w", sub.RelPath, err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
				id.New(), sourceID, sub.RelPath, matchedVideoID, match.Language, match.Title, match.Format, isDef,
			); err != nil {
				return fmt.Errorf("insert subtitle_file %s: %w", sub.RelPath, err)
			}
		}
	}

	for rel, ex := range existingSubs {
		if !seenSubs[rel] && ex.status != "missing" {
			if _, err := tx.ExecContext(ctx, `UPDATE subtitle_files SET status = 'missing' WHERE id = ?`, ex.id); err != nil {
				return fmt.Errorf("mark missing subtitle_file %s: %w", rel, err)
			}
		}
	}
	return nil
}

func (s *Service) loadSource(ctx context.Context, sourceID string) (*sourceRecord, error) {
	var rec sourceRecord
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, driver, root_path, config_json FROM storage_sources WHERE id = ?`,
		sourceID,
	).Scan(&rec.id, &rec.driver, &rec.rootPath, &rec.configJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load storage_source %s: %w", sourceID, err)
	}
	return &rec, nil
}

func (s *Service) loadExistingMedia(ctx context.Context, sourceID string) (map[string]existingMediaRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, relative_path, file_size, mod_time, status, thumbnail_path, probed_at
		FROM media_files
		WHERE source_id = ?`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query existing media_files: %w", err)
	}
	defer rows.Close()

	res := make(map[string]existingMediaRecord)
	for rows.Next() {
		var r existingMediaRecord
		if err := rows.Scan(&r.id, &r.relativePath, &r.fileSize, &r.modTime, &r.status, &r.thumbnailPath, &r.probedAt); err != nil {
			return nil, fmt.Errorf("scan existing media_file: %w", err)
		}
		res[r.relativePath] = r
	}
	return res, rows.Err()
}

func (s *Service) loadExistingSubtitles(ctx context.Context, sourceID string) (map[string]existingSubtitleRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, relative_path, status
		FROM subtitle_files
		WHERE source_id = ?`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query existing subtitle_files: %w", err)
	}
	defer rows.Close()

	res := make(map[string]existingSubtitleRecord)
	for rows.Next() {
		var r existingSubtitleRecord
		if err := rows.Scan(&r.id, &r.relativePath, &r.status); err != nil {
			return nil, fmt.Errorf("scan existing subtitle_file: %w", err)
		}
		res[r.relativePath] = r
	}
	return res, rows.Err()
}
