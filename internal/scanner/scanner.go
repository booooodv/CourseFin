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

	"golang.org/x/text/unicode/norm"
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
	ProbedVideos     int       `json:"probed_videos"`
	TotalSubtitles   int       `json:"total_subtitles"`
	MatchedSubtitles int       `json:"matched_subtitles"`
	Errors           []string  `json:"errors,omitempty"`
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
	id            string
	relativePath  string
	mediaFileID   sql.NullString
	manuallyBound bool
	status        string
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
	matchedSubs, err := s.persistScanResults(ctx, sourceID, nowStr, results, restoreIDs, missingVideoIDs, videoEntries, videoIDByRel, subEntries, existingSubs)
	if err != nil {
		scanErr = err
		return nil, err
	}
	rep.MatchedSubtitles = matchedSubs

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
) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin scan tx: %w", err)
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
				return 0, fmt.Errorf("insert media_file %s: %w", job.entry.RelPath, err)
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
				return 0, fmt.Errorf("update media_file %s: %w", job.entry.RelPath, err)
			}
		}
	}

	for _, idToRestore := range restoreIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE media_files SET status = 'active', updated_at = ? WHERE id = ?`, nowStr, idToRestore); err != nil {
			return 0, fmt.Errorf("restore media_file %s: %w", idToRestore, err)
		}
	}

	for _, idMissing := range missingVideoIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE media_files SET status = 'missing', updated_at = ? WHERE id = ?`, nowStr, idMissing); err != nil {
			return 0, fmt.Errorf("mark missing media_file %s: %w", idMissing, err)
		}
	}

	matchedSubs, err := syncSubtitlesTx(ctx, tx, sourceID, videoEntries, videoIDByRel, subEntries, existingSubs)
	if err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE storage_sources SET last_scanned_at = ?, updated_at = ? WHERE id = ?`, nowStr, nowStr, sourceID); err != nil {
		return 0, fmt.Errorf("update storage_sources.last_scanned_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit scan tx: %w", err)
	}
	return matchedSubs, nil
}

type videoSubtitleIndex struct {
	byDir          map[string][]string
	dirsByParent   map[string][]string
	videoIDByRel   map[string]string
}

func buildVideoSubtitleIndex(videoRels []string, videoIDByRel map[string]string) *videoSubtitleIndex {
	idx := &videoSubtitleIndex{
		byDir:        make(map[string][]string),
		dirsByParent: make(map[string][]string),
		videoIDByRel: videoIDByRel,
	}
	seenDir := make(map[string]bool)
	for _, rel := range videoRels {
		dir := norm.NFC.String(path.Dir(rel))
		idx.byDir[dir] = append(idx.byDir[dir], rel)
		if !seenDir[dir] {
			seenDir[dir] = true
			parent := norm.NFC.String(path.Dir(dir))
			idx.dirsByParent[parent] = append(idx.dirsByParent[parent], dir)
		}
	}
	// Prefer longer stems first so e.g. 01_intro.2.mp4 beats 01_intro.mp4 when matching 01_intro.2.srt.
	for dir := range idx.byDir {
		sort.Slice(idx.byDir[dir], func(i, j int) bool {
			li := len(strings.TrimSuffix(norm.NFC.String(path.Base(idx.byDir[dir][i])), path.Ext(idx.byDir[dir][i])))
			lj := len(strings.TrimSuffix(norm.NFC.String(path.Base(idx.byDir[dir][j])), path.Ext(idx.byDir[dir][j])))
			if li != lj {
				return li > lj
			}
			return idx.byDir[dir][i] < idx.byDir[dir][j]
		})
	}
	for parent := range idx.dirsByParent {
		sort.Strings(idx.dirsByParent[parent])
	}
	return idx
}

func (idx *videoSubtitleIndex) match(subRel string) (videoID string, match SubtitleMatch, found bool) {
	subDir := norm.NFC.String(path.Dir(subRel))

	// 1. Same directory candidates.
	if vid, m, ok := idx.matchInCandidates(idx.byDir[subDir], subRel, false); ok {
		return vid, m, true
	}

	// 2. If subtitle resides inside a recognized subtitle folder (Subs/, Subtitles/, 字幕/, etc.):
	if subDir != "." && IsSubtitleDirName(path.Base(subDir)) {
		parentDir := norm.NFC.String(path.Dir(subDir))
		// 2a. Parent directory candidates (e.g. Course/Ch1/Subs/01.srt -> Course/Ch1/01.mp4).
		if vid, m, ok := idx.matchInCandidates(idx.byDir[parentDir], subRel, false); ok {
			return vid, m, true
		}

		// 2b. Sibling directories under the same parent (e.g. Course/Subtitles/01.srt -> Course/Videos/01.mp4).
		// Require a unique match across siblings to prevent false cross-chapter binding.
		var siblingCandidates []string
		for _, sibDir := range idx.dirsByParent[parentDir] {
			if sibDir == subDir {
				continue
			}
			siblingCandidates = append(siblingCandidates, idx.byDir[sibDir]...)
		}
		if len(siblingCandidates) > 0 {
			if vid, m, ok := idx.matchInCandidates(siblingCandidates, subRel, true); ok {
				return vid, m, true
			}
		}
	}

	return "", SubtitleMatch{}, false
}

func (idx *videoSubtitleIndex) matchInCandidates(candidates []string, subRel string, requireUnique bool) (string, SubtitleMatch, bool) {
	if len(candidates) == 0 {
		return "", SubtitleMatch{}, false
	}

	subExt := path.Ext(subRel)
	subStem := strings.TrimSuffix(norm.NFC.String(path.Base(subRel)), norm.NFC.String(subExt))

	// Pass 1: exact stem match.
	var exactHits []struct {
		vid string
		m   SubtitleMatch
	}
	for _, vidRel := range candidates {
		vidExt := path.Ext(vidRel)
		vidStem := strings.TrimSuffix(norm.NFC.String(path.Base(vidRel)), norm.NFC.String(vidExt))
		if strings.EqualFold(subStem, vidStem) {
			if m, ok := matchSubtitleByStem(vidRel, subRel, false); ok {
				if !requireUnique {
					return idx.videoIDByRel[vidRel], m, true
				}
				exactHits = append(exactHits, struct {
					vid string
					m   SubtitleMatch
				}{vid: idx.videoIDByRel[vidRel], m: m})
			}
		}
	}
	if len(exactHits) == 1 {
		return exactHits[0].vid, exactHits[0].m, true
	}
	if len(exactHits) > 1 {
		return "", SubtitleMatch{}, false
	}

	// Pass 2: stem + separator + language/descriptor match.
	var prefixHits []struct {
		vid string
		m   SubtitleMatch
	}
	for _, vidRel := range candidates {
		if m, ok := matchSubtitleByStem(vidRel, subRel, false); ok {
			if !requireUnique {
				return idx.videoIDByRel[vidRel], m, true
			}
			prefixHits = append(prefixHits, struct {
				vid string
				m   SubtitleMatch
			}{vid: idx.videoIDByRel[vidRel], m: m})
		}
	}
	if len(prefixHits) == 1 {
		return prefixHits[0].vid, prefixHits[0].m, true
	}
	if len(prefixHits) > 1 {
		return "", SubtitleMatch{}, false
	}

	// Pass 3: quality-tag-stripped stem fallback (always require unique candidate match).
	var strippedHits []struct {
		vid string
		m   SubtitleMatch
	}
	for _, vidRel := range candidates {
		if m, ok := matchSubtitleByStem(vidRel, subRel, true); ok {
			strippedHits = append(strippedHits, struct {
				vid string
				m   SubtitleMatch
			}{vid: idx.videoIDByRel[vidRel], m: m})
		}
	}
	if len(strippedHits) == 1 {
		return strippedHits[0].vid, strippedHits[0].m, true
	}

	return "", SubtitleMatch{}, false
}

func syncSubtitlesTx(
	ctx context.Context,
	tx *sql.Tx,
	sourceID string,
	videoEntries []storage.FileEntry,
	videoIDByRel map[string]string,
	subEntries []storage.FileEntry,
	existingSubs map[string]existingSubtitleRecord,
) (int, error) {
	videoRels := make([]string, 0, len(videoEntries))
	for _, v := range videoEntries {
		videoRels = append(videoRels, v.RelPath)
	}
	vidIdx := buildVideoSubtitleIndex(videoRels, videoIDByRel)

	matchedCount := 0
	seenSubs := make(map[string]bool, len(subEntries))
	for _, sub := range subEntries {
		seenSubs[sub.RelPath] = true

		if ex, exists := existingSubs[sub.RelPath]; exists && ex.manuallyBound {
			if ex.status != "active" {
				if _, err := tx.ExecContext(ctx, `UPDATE subtitle_files SET status = 'active' WHERE id = ?`, ex.id); err != nil {
					return 0, fmt.Errorf("restore manually bound subtitle_file %s: %w", sub.RelPath, err)
				}
			}
			if ex.mediaFileID.Valid && strings.TrimSpace(ex.mediaFileID.String) != "" {
				matchedCount++
			}
			continue
		}

		matchedVideoID := sql.NullString{}
		vid, match, found := vidIdx.match(sub.RelPath)
		if found {
			matchedVideoID = sql.NullString{String: vid, Valid: true}
			matchedCount++
		} else {
			subExt := path.Ext(sub.RelPath)
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
				return 0, fmt.Errorf("update subtitle_file %s: %w", sub.RelPath, err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
				id.New(), sourceID, sub.RelPath, matchedVideoID, match.Language, match.Title, match.Format, isDef,
			); err != nil {
				return 0, fmt.Errorf("insert subtitle_file %s: %w", sub.RelPath, err)
			}
		}
	}

	for rel, ex := range existingSubs {
		if !seenSubs[rel] && ex.status != "missing" {
			if _, err := tx.ExecContext(ctx, `UPDATE subtitle_files SET status = 'missing' WHERE id = ?`, ex.id); err != nil {
				return 0, fmt.Errorf("mark missing subtitle_file %s: %w", rel, err)
			}
		}
	}
	return matchedCount, nil
}

// RematchUnboundSubtitles performs a pure-database pass re-matching active
// subtitle_files where media_file_id IS NULL AND manually_bound = 0 against
// active media_files in the same storage source.
func (s *Service) RematchUnboundSubtitles(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_id, relative_path
		FROM subtitle_files
		WHERE status = 'active' AND media_file_id IS NULL AND manually_bound = 0`)
	if err != nil {
		return 0, fmt.Errorf("query unbound subtitles: %w", err)
	}
	type unboundSub struct {
		id       string
		sourceID string
		relPath  string
	}
	bySource := make(map[string][]unboundSub)
	for rows.Next() {
		var u unboundSub
		if err := rows.Scan(&u.id, &u.sourceID, &u.relPath); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan unbound subtitle: %w", err)
		}
		bySource[u.sourceID] = append(bySource[u.sourceID], u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	if len(bySource) == 0 {
		return 0, nil
	}

	type subUpdate struct {
		subID       string
		mediaFileID string
		language    string
		title       string
		isDefault   int
	}
	var updates []subUpdate

	for sourceID, subs := range bySource {
		mRows, err := s.db.QueryContext(ctx, `
			SELECT id, relative_path
			FROM media_files
			WHERE source_id = ? AND status = 'active'`, sourceID)
		if err != nil {
			return 0, fmt.Errorf("query active media_files for source %s: %w", sourceID, err)
		}
		var videoRels []string
		videoIDByRel := make(map[string]string)
		for mRows.Next() {
			var mid, rel string
			if err := mRows.Scan(&mid, &rel); err != nil {
				mRows.Close()
				return 0, fmt.Errorf("scan media_file: %w", err)
			}
			videoRels = append(videoRels, rel)
			videoIDByRel[rel] = mid
		}
		if err := mRows.Err(); err != nil {
			mRows.Close()
			return 0, err
		}
		mRows.Close()

		if len(videoRels) == 0 {
			continue
		}
		vidIdx := buildVideoSubtitleIndex(videoRels, videoIDByRel)
		for _, sub := range subs {
			if vid, m, ok := vidIdx.match(sub.relPath); ok {
				isDef := 0
				if m.IsDefault {
					isDef = 1
				}
				updates = append(updates, subUpdate{
					subID:       sub.id,
					mediaFileID: vid,
					language:    m.Language,
					title:       m.Title,
					isDefault:   isDef,
				})
			}
		}
	}

	if len(updates) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin rematch tx: %w", err)
	}
	defer tx.Rollback()

	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `
			UPDATE subtitle_files
			SET media_file_id = ?, language = ?, title = ?, is_default = ?
			WHERE id = ? AND media_file_id IS NULL AND manually_bound = 0`,
			u.mediaFileID, u.language, u.title, u.isDefault, u.subID,
		); err != nil {
			return 0, fmt.Errorf("rematch update subtitle %s: %w", u.subID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit rematch tx: %w", err)
	}
	return len(updates), nil
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
		SELECT id, relative_path, media_file_id, manually_bound, status
		FROM subtitle_files
		WHERE source_id = ?`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query existing subtitle_files: %w", err)
	}
	defer rows.Close()

	res := make(map[string]existingSubtitleRecord)
	for rows.Next() {
		var r existingSubtitleRecord
		var manualInt int
		if err := rows.Scan(&r.id, &r.relativePath, &r.mediaFileID, &manualInt, &r.status); err != nil {
			return nil, fmt.Errorf("scan existing subtitle_file: %w", err)
		}
		r.manuallyBound = manualInt != 0
		res[r.relativePath] = r
	}
	return res, rows.Err()
}
