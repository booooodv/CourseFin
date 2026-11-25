package summary

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"coursefin/internal/ai"
	"coursefin/internal/catalog"
	"coursefin/internal/id"
	"coursefin/internal/media"
)

const defaultLLMTimeout = 180 * time.Second

var (
	ErrNotFound            = errors.New("not found")
	ErrAINotConfigured     = errors.New("ai provider is not configured")
	ErrASRNotConfigured    = errors.New("asr provider is not configured")
	ErrBatchAlreadyRunning = errors.New("course batch summary already running")
	ErrInvalidInput        = errors.New("invalid input")
)

// JobStage identifies the current execution phase of a lesson or course summary task.
type JobStage string

const (
	StageQueued          JobStage = "queued"
	StageReadingSubtitle JobStage = "reading_subtitle"
	StageExtractingAudio JobStage = "extracting_audio"
	StageTranscribing    JobStage = "transcribing"
	StageSummarizing     JobStage = "summarizing"
	StageCompleted       JobStage = "completed"
	StageFailed          JobStage = "failed"
	StageCancelled       JobStage = "cancelled"
)

// AIConfigSnapshot contains resolved effective LLM and ASR settings plus readiness flags.
type AIConfigSnapshot struct {
	AI            ai.Config
	ASR           ai.ASRConfig
	AIConfigured  bool
	ASRConfigured bool
}

// ConfigLoader loads effective AI and ASR configuration for summary execution.
type ConfigLoader func(ctx context.Context) (AIConfigSnapshot, error)

// ResolveEffectiveSettings resolves effective AI and ASR configurations from raw app_settings
// values while ensuring the main ai_api_key is never forwarded when a custom ai_asr_base_url is set.
func ResolveEffectiveSettings(settings map[string]string) AIConfigSnapshot {
	aiBaseURL := strings.TrimSpace(settings["ai_base_url"])
	aiAPIKey := strings.TrimSpace(settings["ai_api_key"])
	aiModel := strings.TrimSpace(settings["ai_model"])

	maxSubChars := ai.DefaultMaxSubtitleChars
	if raw := strings.TrimSpace(settings["ai_max_subtitle_chars"]); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			maxSubChars = v
		}
	}

	asrBaseURLRaw := strings.TrimSpace(settings["ai_asr_base_url"])
	asrAPIKeyRaw := strings.TrimSpace(settings["ai_asr_api_key"])
	asrModel := strings.TrimSpace(settings["ai_asr_model"])
	asrLanguage := strings.TrimSpace(settings["ai_asr_language"])

	effectiveASRBaseURL := asrBaseURLRaw
	if effectiveASRBaseURL == "" {
		effectiveASRBaseURL = aiBaseURL
	}

	effectiveASRAPIKey := asrAPIKeyRaw
	if effectiveASRAPIKey == "" && asrBaseURLRaw == "" {
		effectiveASRAPIKey = aiAPIKey
	}

	aiConfigured := aiBaseURL != "" && aiModel != ""
	asrConfigured := effectiveASRBaseURL != "" && asrModel != ""

	return AIConfigSnapshot{
		AI: ai.Config{
			BaseURL:          aiBaseURL,
			APIKey:           aiAPIKey,
			Model:            aiModel,
			MaxSubtitleChars: maxSubChars,
		},
		ASR: ai.ASRConfig{
			BaseURL:  effectiveASRBaseURL,
			APIKey:   effectiveASRAPIKey,
			Model:    asrModel,
			Language: asrLanguage,
		},
		AIConfigured:  aiConfigured,
		ASRConfigured: asrConfigured,
	}
}

// LessonSummary represents a persisted structured lesson AI summary.
type LessonSummary struct {
	LessonID      string               `json:"lesson_id"`
	MediaFileID   string               `json:"media_file_id"`
	SourceType    string               `json:"source_type"`
	SourceLabel   string               `json:"source_label"`
	HasTimestamps bool                 `json:"has_timestamps"`
	Model         string               `json:"model"`
	Overview      string               `json:"overview"`
	Outline       []ai.OutlineItem     `json:"outline"`
	Timeline      []ai.TimelineSection `json:"timeline"`
	CreatedAt     string               `json:"created_at"`
	UpdatedAt     string               `json:"updated_at"`
}

// LessonCapabilities describes available transcript sources and configured AI/ASR/FFmpeg capabilities.
type LessonCapabilities struct {
	HasSubtitle         bool   `json:"has_subtitle"`
	HasCachedTranscript bool   `json:"has_cached_transcript"`
	TranscriptSource    string `json:"transcript_source"`
	AIConfigured        bool   `json:"ai_configured"`
	ASRConfigured       bool   `json:"asr_configured"`
	HasFFmpeg           bool   `json:"has_ffmpeg"`
}

// LessonJobStatus describes the live or most recent background summary job for one lesson.
type LessonJobStatus struct {
	LessonID   string   `json:"lesson_id"`
	CourseID   string   `json:"course_id"`
	Running    bool     `json:"running"`
	Stage      JobStage `json:"stage"`
	Message    string   `json:"message"`
	ChunkIndex int      `json:"chunk_index"`
	ChunkTotal int      `json:"chunk_total"`
	SourceType string   `json:"source_type"`
	Error      string   `json:"error,omitempty"`
	ErrorCode  string   `json:"error_code,omitempty"`
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at,omitempty"`
}

// BatchLessonError captures an individual lesson failure inside a course batch run.
type BatchLessonError struct {
	LessonID    string `json:"lesson_id"`
	LessonTitle string `json:"lesson_title"`
	Error       string `json:"error"`
	ErrorCode   string `json:"error_code,omitempty"`
}

// CourseBatchStatus describes the live or most recent batch summary job for a course.
type CourseBatchStatus struct {
	CourseID           string             `json:"course_id"`
	Running            bool               `json:"running"`
	Total              int                `json:"total"`
	Completed          int                `json:"completed"`
	Skipped            int                `json:"skipped"`
	Failed             int                `json:"failed"`
	CurrentLessonID    string             `json:"current_lesson_id"`
	CurrentLessonTitle string             `json:"current_lesson_title"`
	CurrentStage       JobStage           `json:"current_stage"`
	ChunkIndex         int                `json:"chunk_index"`
	ChunkTotal         int                `json:"chunk_total"`
	Errors             []BatchLessonError `json:"errors"`
	StartedAt          string             `json:"started_at"`
	FinishedAt         string             `json:"finished_at,omitempty"`
}

// LessonSummaryState aggregates saved summary, active/recent job status, and lesson capabilities.
type LessonSummaryState struct {
	LessonID     string             `json:"lesson_id"`
	Summary      *LessonSummary     `json:"summary"`
	Job          *LessonJobStatus   `json:"job"`
	Capabilities LessonCapabilities `json:"capabilities"`
}

// CourseSummariesStatus returns the summarized lesson IDs and batch job status for a course.
type CourseSummariesStatus struct {
	CourseID         string             `json:"course_id"`
	SummaryLessonIDs []string           `json:"summary_lesson_ids"`
	BatchJob         *CourseBatchStatus `json:"batch_job"`
}

// GenerateLessonOptions configures single-lesson summary generation.
type GenerateLessonOptions struct {
	Force             bool   `json:"force"`
	ForceRetranscribe bool   `json:"force_retranscribe"`
	ManualTranscript  string `json:"manual_transcript"`
}

// Options configures a summary Service instance.
type Options struct {
	DB                 *sql.DB
	DataDir            string
	Catalog            *catalog.Service
	Media              *media.Service
	LifecycleCtx       context.Context
	HTTPClient         *http.Client
	ConfigLoader       ConfigLoader
	AudioExtractor     AudioExtractor
	SinglePassMaxRunes int
	MapChunkMaxRunes   int
	CompactWindowSec   float64
	CompactMaxRunes    int
}

type lessonJobEntry struct {
	status          LessonJobStatus
	queued          bool
	cancelRequested bool
	cancel          context.CancelFunc
}

type singleJobRequest struct {
	lessonID string
	courseID string
	opts     GenerateLessonOptions
}

type batchLessonItem struct {
	lessonID    string
	lessonTitle string
}

type batchEntry struct {
	status             CourseBatchStatus
	mode               string
	pending            []batchLessonItem
	cancelRequested    bool
	activeLessonID     string
	activeLessonCancel context.CancelFunc
}

type pipelineError struct {
	code string
	err  error
}

func (e *pipelineError) Error() string {
	return e.err.Error()
}

func (e *pipelineError) Unwrap() error {
	return e.err
}

// Service manages background lesson AI summarization, audio extraction/ASR transcription,
// and serialized priority + batch job queues.
type Service struct {
	db                 *sql.DB
	dataDir            string
	catalog            *catalog.Service
	media              *media.Service
	lifecycleCtx       context.Context
	httpClient         *http.Client
	defaultLLMClient   *http.Client
	configLoader       ConfigLoader
	audioExtractor     AudioExtractor
	hasCustomExtractor bool
	singlePassMaxRunes int
	mapChunkMaxRunes   int
	compactWindowSec   float64
	compactMaxRunes    int

	mu            sync.Mutex
	workerActive  bool
	wg            sync.WaitGroup
	singleQueue   []*singleJobRequest
	batchQueue    []*batchEntry
	lessonJobs    map[string]*lessonJobEntry
	courseBatches map[string]*batchEntry
}

// NewService constructs a summary Service. The background worker is started lazily on first enqueue.
func NewService(opts Options) *Service {
	lctx := opts.LifecycleCtx
	if lctx == nil {
		lctx = context.Background()
	}

	singlePassMax := opts.SinglePassMaxRunes
	if singlePassMax <= 0 {
		singlePassMax = DefaultSinglePassMaxRunes
	}
	mapChunkMax := opts.MapChunkMaxRunes
	if mapChunkMax <= 0 {
		mapChunkMax = DefaultMapChunkMaxRunes
	}
	compactWin := opts.CompactWindowSec
	if compactWin <= 0 {
		compactWin = DefaultCompactWindowSec
	}
	compactMax := opts.CompactMaxRunes
	if compactMax <= 0 {
		compactMax = DefaultCompactMaxRunes
	}

	var extractor AudioExtractor
	hasCustomExtractor := opts.AudioExtractor != nil
	if hasCustomExtractor {
		extractor = opts.AudioExtractor
	} else if opts.Media != nil {
		extractor = NewFFmpegAudioExtractor(
			opts.DataDir,
			opts.Media.FFmpegBin(),
			opts.Media.FFprobeBin(),
			DefaultAudioSegmentSeconds,
		)
	}

	s := &Service{
		db:                 opts.DB,
		dataDir:            opts.DataDir,
		catalog:            opts.Catalog,
		media:              opts.Media,
		lifecycleCtx:       lctx,
		httpClient:         opts.HTTPClient,
		defaultLLMClient:   &http.Client{Timeout: defaultLLMTimeout},
		configLoader:       opts.ConfigLoader,
		audioExtractor:     extractor,
		hasCustomExtractor: hasCustomExtractor,
		singlePassMaxRunes: singlePassMax,
		mapChunkMaxRunes:   mapChunkMax,
		compactWindowSec:   compactWin,
		compactMaxRunes:    compactMax,
		lessonJobs:         make(map[string]*lessonJobEntry),
		courseBatches:      make(map[string]*batchEntry),
	}
	return s
}

// Wait blocks until any active background worker goroutine has exited.
// Returns immediately when no work is active.
func (s *Service) Wait() {
	s.wg.Wait()
}

func (s *Service) llmHTTPClient() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return s.defaultLLMClient
}

func (s *Service) asrHTTPClient() *http.Client {
	return s.httpClient
}

func (s *Service) loadConfig(ctx context.Context) (AIConfigSnapshot, error) {
	if s.configLoader != nil {
		return s.configLoader(ctx)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return AIConfigSnapshot{}, fmt.Errorf("load app_settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return AIConfigSnapshot{}, fmt.Errorf("scan app_settings: %w", err)
		}
		settings[k] = v
	}
	if err := rows.Err(); err != nil {
		return AIConfigSnapshot{}, err
	}
	return ResolveEffectiveSettings(settings), nil
}

// GetLessonSummaryState returns the saved summary, current/last job status, and capabilities for a lesson.
func (s *Service) GetLessonSummaryState(ctx context.Context, lessonID string) (*LessonSummaryState, error) {
	lessonID = normalizeOrRaw(lessonID)
	if lessonID == "" {
		return nil, ErrInvalidInput
	}

	lesson, err := s.catalog.GetLesson(ctx, lessonID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	savedSummary, err := s.loadLessonSummary(ctx, lesson.ID)
	if err != nil {
		return nil, err
	}

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}

	caps, err := s.computeLessonCapabilities(ctx, lesson, cfg)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	var jobCopy *LessonJobStatus
	if entry, ok := s.lessonJobs[lesson.ID]; ok {
		cp := entry.status
		jobCopy = &cp
	}
	s.mu.Unlock()

	return &LessonSummaryState{
		LessonID:     lesson.ID,
		Summary:      savedSummary,
		Job:          jobCopy,
		Capabilities: caps,
	}, nil
}

// StartLessonSummary validates configuration and enqueues a single-lesson summary job
// onto the high-priority single-lesson queue. Re-triggering an already running/queued lesson
// returns its current job snapshot idempotently.
func (s *Service) StartLessonSummary(ctx context.Context, lessonID string, opts GenerateLessonOptions) (*LessonJobStatus, error) {
	lessonID = normalizeOrRaw(lessonID)
	if lessonID == "" {
		return nil, ErrInvalidInput
	}

	lesson, err := s.catalog.GetLesson(ctx, lessonID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	s.mu.Lock()
	if entry, ok := s.lessonJobs[lesson.ID]; ok && entry.status.Running {
		cp := entry.status
		s.mu.Unlock()
		return &cp, nil
	}
	s.mu.Unlock()

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.AIConfigured {
		return nil, ErrAINotConfigured
	}

	manual := strings.TrimSpace(opts.ManualTranscript)
	if manual == "" {
		if opts.ForceRetranscribe {
			if !cfg.ASRConfigured {
				return nil, ErrASRNotConfigured
			}
		} else {
			caps, capErr := s.computeLessonCapabilities(ctx, lesson, cfg)
			if capErr != nil {
				return nil, capErr
			}
			if !caps.HasSubtitle && !caps.HasCachedTranscript && !cfg.ASRConfigured {
				return nil, ErrASRNotConfigured
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.lifecycleCtx.Err(); err != nil {
		return nil, err
	}
	if entry, ok := s.lessonJobs[lesson.ID]; ok && entry.status.Running {
		cp := entry.status
		return &cp, nil
	}

	now := nowUTC()
	entry := &lessonJobEntry{
		status: LessonJobStatus{
			LessonID:  lesson.ID,
			CourseID:  lesson.CourseID,
			Running:   true,
			Stage:     StageQueued,
			Message:   "等待生成 AI 总结...",
			StartedAt: now,
		},
		queued: true,
	}
	s.lessonJobs[lesson.ID] = entry
	s.singleQueue = append(s.singleQueue, &singleJobRequest{
		lessonID: lesson.ID,
		courseID: lesson.CourseID,
		opts:     opts,
	})
	s.ensureWorkerLocked()

	cp := entry.status
	return &cp, nil
}

// CancelLessonSummary cancels a queued or active single-lesson summary job.
func (s *Service) CancelLessonSummary(lessonID string) (*LessonJobStatus, error) {
	lessonID = normalizeOrRaw(lessonID)
	if lessonID == "" {
		return nil, ErrInvalidInput
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.lessonJobs[lessonID]
	if !ok {
		return &LessonJobStatus{
			LessonID: lessonID,
			Running:  false,
			Stage:    StageCancelled,
			Message:  "已取消",
		}, nil
	}

	if !entry.status.Running {
		cp := entry.status
		return &cp, nil
	}

	entry.cancelRequested = true
	if entry.queued {
		s.removeSingleQueuedLocked(lessonID)
		entry.queued = false
		entry.status.Running = false
		entry.status.Stage = StageCancelled
		entry.status.Message = "已取消"
		entry.status.FinishedAt = nowUTC()
	} else if entry.cancel != nil {
		entry.status.Message = "正在取消..."
		entry.cancel()
	}

	cp := entry.status
	return &cp, nil
}

// DeleteLessonSummary removes a lesson's saved AI summary and optionally deletes its cached transcript.
func (s *Service) DeleteLessonSummary(ctx context.Context, lessonID string, deleteTranscript bool) error {
	lessonID = normalizeOrRaw(lessonID)
	if lessonID == "" {
		return ErrInvalidInput
	}

	lesson, err := s.catalog.GetLesson(ctx, lessonID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete summary tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM lesson_summaries WHERE lesson_id = ?`, lesson.ID); err != nil {
		return fmt.Errorf("delete lesson_summaries: %w", err)
	}
	if deleteTranscript && lesson.MediaFileID != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_transcripts WHERE media_file_id = ?`, lesson.MediaFileID); err != nil {
			return fmt.Errorf("delete media_transcripts: %w", err)
		}
	}
	return tx.Commit()
}

// GetCourseSummaryStatus returns summarized lesson IDs and the latest/active batch job for courseID.
func (s *Service) GetCourseSummaryStatus(ctx context.Context, courseID string) (*CourseSummariesStatus, error) {
	courseID = normalizeOrRaw(courseID)
	if courseID == "" {
		return nil, ErrInvalidInput
	}

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM courses WHERE id = ?`, courseID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ls.lesson_id
		FROM lesson_summaries ls
		JOIN lessons l ON l.id = ls.lesson_id
		WHERE l.course_id = ?
		ORDER BY l.sort_order ASC, ls.lesson_id ASC`, courseID)
	if err != nil {
		return nil, fmt.Errorf("query course summaries: %w", err)
	}
	defer rows.Close()

	lessonIDs := []string{}
	for rows.Next() {
		var lid string
		if err := rows.Scan(&lid); err != nil {
			return nil, fmt.Errorf("scan course summary lesson_id: %w", err)
		}
		lessonIDs = append(lessonIDs, lid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	var batchCopy *CourseBatchStatus
	if batch, ok := s.courseBatches[courseID]; ok {
		cp := cloneBatchStatus(batch.status)
		batchCopy = &cp
	}
	s.mu.Unlock()

	return &CourseSummariesStatus{
		CourseID:         courseID,
		SummaryLessonIDs: lessonIDs,
		BatchJob:         batchCopy,
	}, nil
}

// StartCourseBatch validates AI configuration and queues all target lessons in courseID for serial summarization.
// mode may be "" / "missing_only" (default) or "all".
func (s *Service) StartCourseBatch(ctx context.Context, courseID string, mode string) (*CourseBatchStatus, error) {
	courseID = normalizeOrRaw(courseID)
	if courseID == "" {
		return nil, ErrInvalidInput
	}

	modeNorm := strings.ToLower(strings.TrimSpace(mode))
	if modeNorm == "" {
		modeNorm = "missing_only"
	}
	if modeNorm != "missing_only" && modeNorm != "all" {
		return nil, fmt.Errorf("%w: unsupported batch mode %q", ErrInvalidInput, mode)
	}

	course, err := s.catalog.GetCourseDetail(ctx, courseID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.AIConfigured {
		return nil, ErrAINotConfigured
	}

	s.mu.Lock()
	if existing, ok := s.courseBatches[course.ID]; ok && existing.status.Running {
		cp := cloneBatchStatus(existing.status)
		s.mu.Unlock()
		return &cp, ErrBatchAlreadyRunning
	}
	s.mu.Unlock()

	var allLessons []batchLessonItem
	for _, ch := range course.Chapters {
		for _, l := range ch.Lessons {
			allLessons = append(allLessons, batchLessonItem{
				lessonID:    l.ID,
				lessonTitle: l.Title,
			})
		}
	}

	existingSet := make(map[string]bool)
	if modeNorm == "missing_only" && len(allLessons) > 0 {
		rows, err := s.db.QueryContext(ctx, `
			SELECT ls.lesson_id
			FROM lesson_summaries ls
			JOIN lessons l ON l.id = ls.lesson_id
			WHERE l.course_id = ?`, course.ID)
		if err != nil {
			return nil, fmt.Errorf("query existing course summaries: %w", err)
		}
		for rows.Next() {
			var lid string
			if err := rows.Scan(&lid); err != nil {
				rows.Close()
				return nil, err
			}
			existingSet[lid] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	var pending []batchLessonItem
	skipped := 0
	for _, item := range allLessons {
		if modeNorm == "missing_only" && existingSet[item.lessonID] {
			skipped++
			continue
		}
		pending = append(pending, item)
	}

	now := nowUTC()
	status := CourseBatchStatus{
		CourseID:  course.ID,
		Running:   len(pending) > 0,
		Total:     len(allLessons),
		Skipped:   skipped,
		Errors:    []BatchLessonError{},
		StartedAt: now,
	}
	if len(pending) == 0 {
		status.CurrentStage = StageCompleted
		status.FinishedAt = now
	} else {
		status.CurrentStage = StageQueued
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.courseBatches[course.ID]; ok && existing.status.Running {
		cp := cloneBatchStatus(existing.status)
		return &cp, ErrBatchAlreadyRunning
	}

	if len(pending) > 0 {
		if err := s.lifecycleCtx.Err(); err != nil {
			return nil, err
		}
	}

	entry := &batchEntry{
		status:  status,
		mode:    modeNorm,
		pending: pending,
	}
	s.courseBatches[course.ID] = entry

	if len(pending) > 0 {
		s.batchQueue = append(s.batchQueue, entry)
		s.ensureWorkerLocked()
	}

	cp := cloneBatchStatus(entry.status)
	return &cp, nil
}

// CancelCourseBatch clears any remaining queued lessons in courseID's batch and cancels its active lesson if running.
func (s *Service) CancelCourseBatch(courseID string) (*CourseBatchStatus, error) {
	courseID = normalizeOrRaw(courseID)
	if courseID == "" {
		return nil, ErrInvalidInput
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	batch, ok := s.courseBatches[courseID]
	if !ok {
		return &CourseBatchStatus{
			CourseID:     courseID,
			Running:      false,
			CurrentStage: StageCancelled,
			Errors:       []BatchLessonError{},
		}, nil
	}

	if !batch.status.Running {
		cp := cloneBatchStatus(batch.status)
		return &cp, nil
	}

	batch.cancelRequested = true
	batch.pending = nil
	s.removeBatchFromQueueLocked(batch)

	if batch.activeLessonCancel != nil {
		if entry, ok := s.lessonJobs[batch.activeLessonID]; ok {
			entry.cancelRequested = true
		}
		batch.activeLessonCancel()
	} else {
		finishBatchLocked(batch, StageCancelled)
	}

	cp := cloneBatchStatus(batch.status)
	return &cp, nil
}

func (s *Service) ensureWorkerLocked() {
	if s.workerActive {
		return
	}
	s.workerActive = true
	s.wg.Add(1)
	go s.runWorker()
}

type nextTaskKind int

const (
	taskNone nextTaskKind = iota
	taskSingle
	taskBatchItem
)

func (s *Service) runWorker() {
	defer s.wg.Done()

	for {
		s.mu.Lock()
		if s.lifecycleCtx.Err() != nil {
			s.cancelQueuedOnShutdownLocked()
			s.workerActive = false
			s.mu.Unlock()
			return
		}

		kind, singleReq, batch, item := s.popNextTaskLocked()
		if kind == taskNone {
			s.workerActive = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		switch kind {
		case taskSingle:
			s.executeSingleTask(singleReq)
		case taskBatchItem:
			s.executeBatchItem(batch, item)
		}
	}
}

func (s *Service) popNextTaskLocked() (nextTaskKind, *singleJobRequest, *batchEntry, batchLessonItem) {
	if len(s.singleQueue) > 0 {
		req := s.singleQueue[0]
		s.singleQueue = s.singleQueue[1:]
		return taskSingle, req, nil, batchLessonItem{}
	}

	for len(s.batchQueue) > 0 {
		batch := s.batchQueue[0]
		if batch.cancelRequested || len(batch.pending) == 0 {
			s.batchQueue = s.batchQueue[1:]
			continue
		}
		item := batch.pending[0]
		batch.pending = batch.pending[1:]
		if len(batch.pending) == 0 {
			s.batchQueue = s.batchQueue[1:]
		}
		return taskBatchItem, nil, batch, item
	}

	return taskNone, nil, nil, batchLessonItem{}
}

func (s *Service) removeSingleQueuedLocked(lessonID string) {
	kept := s.singleQueue[:0]
	for _, req := range s.singleQueue {
		if req.lessonID == lessonID {
			continue
		}
		kept = append(kept, req)
	}
	s.singleQueue = kept
}

func (s *Service) removeBatchFromQueueLocked(target *batchEntry) {
	kept := s.batchQueue[:0]
	for _, b := range s.batchQueue {
		if b == target {
			continue
		}
		kept = append(kept, b)
	}
	s.batchQueue = kept
}

func (s *Service) cancelQueuedOnShutdownLocked() {
	now := nowUTC()
	for _, req := range s.singleQueue {
		if entry, ok := s.lessonJobs[req.lessonID]; ok && entry.status.Running {
			entry.queued = false
			entry.status.Running = false
			entry.status.Stage = StageCancelled
			entry.status.Message = "已取消"
			entry.status.FinishedAt = now
		}
	}
	s.singleQueue = nil

	for _, batch := range s.batchQueue {
		batch.cancelRequested = true
		if batch.status.Running {
			finishBatchLocked(batch, StageCancelled)
		} else {
			batch.pending = nil
		}
	}
	s.batchQueue = nil
}

func clearBatchActiveFieldsLocked(batch *batchEntry, stage JobStage) {
	batch.status.CurrentStage = stage
	batch.status.CurrentLessonID = ""
	batch.status.CurrentLessonTitle = ""
	batch.status.ChunkIndex = 0
	batch.status.ChunkTotal = 0
}

func finishBatchLocked(batch *batchEntry, stage JobStage) {
	batch.pending = nil
	batch.status.Running = false
	clearBatchActiveFieldsLocked(batch, stage)
	batch.status.FinishedAt = nowUTC()
}

func (s *Service) executeSingleTask(req *singleJobRequest) {
	jobCtx, jobCancel := context.WithCancel(s.lifecycleCtx)
	defer jobCancel()

	s.mu.Lock()
	entry, ok := s.lessonJobs[req.lessonID]
	if !ok {
		entry = &lessonJobEntry{
			status: LessonJobStatus{
				LessonID:  req.lessonID,
				CourseID:  req.courseID,
				StartedAt: nowUTC(),
			},
		}
		s.lessonJobs[req.lessonID] = entry
	}
	if entry.cancelRequested || s.lifecycleCtx.Err() != nil {
		entry.queued = false
		entry.status.Running = false
		entry.status.Stage = StageCancelled
		entry.status.Message = "已取消"
		entry.status.FinishedAt = nowUTC()
		s.mu.Unlock()
		return
	}
	entry.queued = false
	entry.cancel = jobCancel
	entry.status.Stage = StageReadingSubtitle
	entry.status.Message = "正在读取字幕..."
	s.mu.Unlock()

	sourceType, err := s.runLessonSummary(jobCtx, req.lessonID, req.opts, func(stage JobStage, msg string, chunkIdx, chunkTotal int, srcType string) {
		s.mu.Lock()
		if e, exists := s.lessonJobs[req.lessonID]; exists {
			e.status.Stage = stage
			e.status.Message = msg
			e.status.ChunkIndex = chunkIdx
			e.status.ChunkTotal = chunkTotal
			if srcType != "" {
				e.status.SourceType = srcType
			}
		}
		s.mu.Unlock()
	})

	s.mu.Lock()
	defer s.mu.Unlock()
	if e, exists := s.lessonJobs[req.lessonID]; exists {
		e.cancel = nil
		s.finalizeLessonJobLocked(e, sourceType, err)
	}
}

func (s *Service) executeBatchItem(batch *batchEntry, item batchLessonItem) {
	s.mu.Lock()
	if batch.cancelRequested || s.lifecycleCtx.Err() != nil {
		if batch.status.Running {
			finishBatchLocked(batch, StageCancelled)
		}
		s.mu.Unlock()
		return
	}
	mode := batch.mode
	s.mu.Unlock()

	// If a priority single-lesson job summarized this lesson while it was waiting in missing_only mode, skip it.
	if mode == "missing_only" {
		var exists int
		err := s.db.QueryRowContext(s.lifecycleCtx, `SELECT 1 FROM lesson_summaries WHERE lesson_id = ?`, item.lessonID).Scan(&exists)
		if err == nil {
			s.mu.Lock()
			if batch.cancelRequested || s.lifecycleCtx.Err() != nil {
				if batch.status.Running {
					finishBatchLocked(batch, StageCancelled)
				}
				s.mu.Unlock()
				return
			}
			batch.status.Skipped++
			if len(batch.pending) == 0 {
				finishBatchLocked(batch, StageCompleted)
			} else {
				clearBatchActiveFieldsLocked(batch, StageQueued)
			}
			s.mu.Unlock()
			return
		}
	}

	jobCtx, jobCancel := context.WithCancel(s.lifecycleCtx)
	defer jobCancel()

	s.mu.Lock()
	if batch.cancelRequested || s.lifecycleCtx.Err() != nil {
		if batch.status.Running {
			finishBatchLocked(batch, StageCancelled)
		}
		s.mu.Unlock()
		return
	}

	now := nowUTC()
	entry := &lessonJobEntry{
		status: LessonJobStatus{
			LessonID:  item.lessonID,
			CourseID:  batch.status.CourseID,
			Running:   true,
			Stage:     StageReadingSubtitle,
			Message:   "正在读取字幕...",
			StartedAt: now,
		},
		cancel: jobCancel,
	}
	s.lessonJobs[item.lessonID] = entry

	batch.activeLessonID = item.lessonID
	batch.activeLessonCancel = jobCancel
	batch.status.CurrentLessonID = item.lessonID
	batch.status.CurrentLessonTitle = item.lessonTitle
	batch.status.CurrentStage = StageReadingSubtitle
	batch.status.ChunkIndex = 0
	batch.status.ChunkTotal = 0
	s.mu.Unlock()

	sourceType, err := s.runLessonSummary(jobCtx, item.lessonID, GenerateLessonOptions{}, func(stage JobStage, msg string, chunkIdx, chunkTotal int, srcType string) {
		s.mu.Lock()
		if e, exists := s.lessonJobs[item.lessonID]; exists {
			e.status.Stage = stage
			e.status.Message = msg
			e.status.ChunkIndex = chunkIdx
			e.status.ChunkTotal = chunkTotal
			if srcType != "" {
				e.status.SourceType = srcType
			}
		}
		batch.status.CurrentStage = stage
		batch.status.ChunkIndex = chunkIdx
		batch.status.ChunkTotal = chunkTotal
		s.mu.Unlock()
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	if e, exists := s.lessonJobs[item.lessonID]; exists {
		e.cancel = nil
		s.finalizeLessonJobLocked(e, sourceType, err)
	}

	batch.activeLessonID = ""
	batch.activeLessonCancel = nil

	if batch.cancelRequested || s.lifecycleCtx.Err() != nil {
		if batch.status.Running {
			finishBatchLocked(batch, StageCancelled)
		}
		return
	}

	if err == nil {
		batch.status.Completed++
	} else {
		batch.status.Failed++
		errCode, errMsg := classifyJobError(err)
		if errors.Is(err, context.Canceled) {
			errCode = "cancelled"
			errMsg = "任务已取消"
		}
		batch.status.Errors = append(batch.status.Errors, BatchLessonError{
			LessonID:    item.lessonID,
			LessonTitle: item.lessonTitle,
			Error:       errMsg,
			ErrorCode:   errCode,
		})
	}

	if len(batch.pending) == 0 {
		finishBatchLocked(batch, StageCompleted)
	} else {
		clearBatchActiveFieldsLocked(batch, StageQueued)
	}
}

func (s *Service) finalizeLessonJobLocked(entry *lessonJobEntry, sourceType string, err error) {
	now := nowUTC()
	entry.status.Running = false
	entry.status.FinishedAt = now
	if sourceType != "" {
		entry.status.SourceType = sourceType
	}

	if err == nil {
		entry.status.Stage = StageCompleted
		entry.status.Message = "AI 总结生成完成"
		entry.status.Error = ""
		entry.status.ErrorCode = ""
		return
	}

	if entry.cancelRequested || errors.Is(err, context.Canceled) || s.lifecycleCtx.Err() != nil {
		entry.status.Stage = StageCancelled
		entry.status.Message = "已取消"
		entry.status.Error = ""
		entry.status.ErrorCode = ""
		return
	}

	code, msg := classifyJobError(err)
	entry.status.Stage = StageFailed
	entry.status.Message = "生成失败"
	entry.status.Error = msg
	entry.status.ErrorCode = code
}

func classifyJobError(err error) (code string, msg string) {
	if err == nil {
		return "", ""
	}
	var pe *pipelineError
	if errors.As(err, &pe) {
		return pe.code, pe.err.Error()
	}
	switch {
	case errors.Is(err, ErrAINotConfigured):
		return "ai_not_configured", err.Error()
	case errors.Is(err, ErrASRNotConfigured):
		return "asr_not_configured", err.Error()
	case errors.Is(err, ErrNotFound):
		return "not_found", err.Error()
	case errors.Is(err, ErrInvalidInput):
		return "invalid_input", err.Error()
	default:
		return "internal_error", err.Error()
	}
}

type progressFunc func(stage JobStage, msg string, chunkIdx, chunkTotal int, sourceType string)

type resolvedTranscript struct {
	sourceType    string
	sourceLabel   string
	hasTimestamps bool
	vttText       string
	plainText     string
}

func (s *Service) runLessonSummary(
	ctx context.Context,
	lessonID string,
	opts GenerateLessonOptions,
	report progressFunc,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	lesson, err := s.catalog.GetLesson(ctx, lessonID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return "", &pipelineError{code: "not_found", err: ErrNotFound}
		}
		return "", err
	}

	courseTitle, chapterTitle := s.loadLessonHierarchyTitles(ctx, lesson.ID)

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return "", err
	}
	if !cfg.AIConfigured {
		return "", &pipelineError{code: "ai_not_configured", err: ErrAINotConfigured}
	}

	durationSec := int(lesson.DurationMs / 1000)

	report(StageReadingSubtitle, "正在读取字幕...", 0, 0, "")

	src, err := s.resolveLessonTranscript(ctx, lesson.MediaFileID, opts, cfg, report)
	if err != nil {
		return src.sourceType, err
	}

	if err := ctx.Err(); err != nil {
		return src.sourceType, err
	}

	summaryOut, err := s.summarizeResolvedTranscript(ctx, cfg, courseTitle, chapterTitle, lesson.Title, durationSec, src, report)
	if err != nil {
		return src.sourceType, err
	}

	// Guard against saving if cancelled right before persistence; existing summary stays untouched.
	if err := ctx.Err(); err != nil {
		return src.sourceType, err
	}

	if err := s.upsertLessonSummary(ctx, lesson.ID, lesson.MediaFileID, src.sourceType, src.sourceLabel, src.hasTimestamps, cfg.AI.Model, summaryOut); err != nil {
		return src.sourceType, err
	}

	return src.sourceType, nil
}

func (s *Service) resolveLessonTranscript(
	ctx context.Context,
	mediaFileID string,
	opts GenerateLessonOptions,
	cfg AIConfigSnapshot,
	report progressFunc,
) (resolvedTranscript, error) {
	manual := strings.TrimSpace(opts.ManualTranscript)
	if manual != "" {
		vtt, plain, hasTS := DetectManualTranscript(manual)
		if strings.TrimSpace(plain) == "" {
			return resolvedTranscript{}, &pipelineError{code: "invalid_input", err: fmt.Errorf("%w: manual_transcript is empty", ErrInvalidInput)}
		}
		if err := s.upsertMediaTranscript(ctx, mediaFileID, "manual", "und", "", vtt, plain); err != nil {
			return resolvedTranscript{}, err
		}
		return resolvedTranscript{
			sourceType:    "manual",
			sourceLabel:   "手动文稿",
			hasTimestamps: hasTS,
			vttText:       vtt,
			plainText:     plain,
		}, nil
	}

	if !opts.ForceRetranscribe {
		if s.media != nil {
			vtt, st, sl, found, subErr := s.media.ExtractBestSubtitleVTT(ctx, mediaFileID)
			if subErr != nil {
				if ctx.Err() != nil {
					return resolvedTranscript{}, ctx.Err()
				}
				// If physical subtitle extraction failed (e.g. embedded subtitle without ffmpeg),
				// still check cached transcript before failing.
				if res, ok, cErr := s.resolveCachedTranscript(ctx, mediaFileID); cErr == nil && ok {
					return res, nil
				}
				return resolvedTranscript{}, &pipelineError{code: "subtitle_extract_failed", err: subErr}
			}
			if found {
				return resolvedTranscript{
					sourceType:    st,
					sourceLabel:   sl,
					hasTimestamps: true,
					vttText:       vtt,
				}, nil
			}
		}

		if res, ok, cacheErr := s.resolveCachedTranscript(ctx, mediaFileID); cacheErr != nil {
			return resolvedTranscript{}, cacheErr
		} else if ok {
			return res, nil
		}
	}

	vtt, lang, asrErr := s.runASRPipeline(ctx, mediaFileID, cfg, report)
	if asrErr != nil {
		return resolvedTranscript{sourceType: "asr"}, asrErr
	}
	return resolvedTranscript{
		sourceType:    "asr",
		sourceLabel:   formatASRSourceLabel(lang),
		hasTimestamps: true,
		vttText:       vtt,
	}, nil
}

func (s *Service) resolveCachedTranscript(ctx context.Context, mediaFileID string) (resolvedTranscript, bool, error) {
	cached, found, err := s.loadCachedTranscript(ctx, mediaFileID)
	if err != nil || !found {
		return resolvedTranscript{}, false, err
	}
	res := resolvedTranscript{}
	if strings.TrimSpace(cached.contentVTT) != "" {
		res.vttText = cached.contentVTT
		res.hasTimestamps = true
	} else {
		res.plainText = cached.plainText
		res.hasTimestamps = false
	}
	if cached.source == "manual" {
		res.sourceType = "manual"
		res.sourceLabel = "手动文稿"
	} else {
		res.sourceType = "asr"
		res.sourceLabel = formatASRSourceLabel(cached.language)
	}
	return res, true, nil
}

func (s *Service) summarizeResolvedTranscript(
	ctx context.Context,
	cfg AIConfigSnapshot,
	courseTitle, chapterTitle, lessonTitle string,
	durationSec int,
	src resolvedTranscript,
	report progressFunc,
) (*ai.LessonSummaryOutput, error) {
	var (
		fullText   string
		chunkTexts []string
	)

	if src.hasTimestamps {
		cues := ai.ParseSubtitleCues(src.vttText)
		compacted := CompactCues(cues, s.compactWindowSec, s.compactMaxRunes)
		fullText = FormatCompactedLines(compacted)
		if strings.TrimSpace(fullText) == "" {
			return nil, &pipelineError{code: "empty_transcript", err: errors.New("subtitle/transcript contains no usable text")}
		}
		if utf8.RuneCountInString(fullText) > s.singlePassMaxRunes {
			chunkTexts = SplitCompactedLines(compacted, s.mapChunkMaxRunes)
		}
	} else {
		fullText = normalizePlainText(src.plainText)
		if strings.TrimSpace(fullText) == "" {
			return nil, &pipelineError{code: "empty_transcript", err: errors.New("transcript contains no usable text")}
		}
		if utf8.RuneCountInString(fullText) > s.singlePassMaxRunes {
			chunkTexts = SplitPlainText(fullText, s.mapChunkMaxRunes)
		}
	}

	provider := ai.NewOpenAICompatibleProvider(cfg.AI, s.llmHTTPClient())

	if len(chunkTexts) <= 1 {
		transcriptForSingle := fullText
		if len(chunkTexts) == 1 {
			transcriptForSingle = chunkTexts[0]
		}
		report(StageSummarizing, "正在生成 AI 总结...", 0, 0, src.sourceType)
		out, sumErr := provider.SummarizeLesson(ctx, ai.LessonSummaryInput{
			CourseTitle:   courseTitle,
			ChapterTitle:  chapterTitle,
			LessonTitle:   lessonTitle,
			DurationSec:   durationSec,
			HasTimestamps: src.hasTimestamps,
			Transcript:    transcriptForSingle,
		})
		if sumErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &pipelineError{code: "ai_failed", err: sumErr}
		}
		return out, nil
	}

	totalSteps := len(chunkTexts) + 1
	var mergedTimeline []ai.TimelineSection
	for i, chunk := range chunkTexts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report(StageSummarizing, fmt.Sprintf("正在分段总结（%d/%d）...", i+1, totalSteps), i+1, totalSteps, src.sourceType)
		sections, mapErr := provider.SummarizeLessonChunk(ctx, ai.LessonChunkInput{
			CourseTitle:   courseTitle,
			ChapterTitle:  chapterTitle,
			LessonTitle:   lessonTitle,
			ChunkIndex:    i + 1,
			ChunkTotal:    len(chunkTexts),
			DurationSec:   durationSec,
			HasTimestamps: src.hasTimestamps,
			Transcript:    chunk,
		})
		if mapErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &pipelineError{code: "ai_failed", err: mapErr}
		}
		mergedTimeline = append(mergedTimeline, sections...)
	}

	var normErr error
	mergedTimeline, normErr = ai.ValidateAndNormalizeTimelineChunk(mergedTimeline, durationSec, src.hasTimestamps)
	if normErr != nil {
		return nil, &pipelineError{code: "ai_failed", err: normErr}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report(StageSummarizing, fmt.Sprintf("正在汇总课程大纲（%d/%d）...", totalSteps, totalSteps), totalSteps, totalSteps, src.sourceType)
	overview, outline, redErr := provider.SynthesizeLessonOutline(ctx, ai.LessonOutlineSynthInput{
		CourseTitle:   courseTitle,
		ChapterTitle:  chapterTitle,
		LessonTitle:   lessonTitle,
		DurationSec:   durationSec,
		HasTimestamps: src.hasTimestamps,
		Timeline:      mergedTimeline,
	})
	if redErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &pipelineError{code: "ai_failed", err: redErr}
	}

	candidate := &ai.LessonSummaryOutput{
		Overview: overview,
		Outline:  outline,
		Timeline: mergedTimeline,
	}
	if valErr := ai.ValidateAndNormalizeSummary(candidate, durationSec, src.hasTimestamps); valErr != nil {
		return nil, &pipelineError{code: "ai_failed", err: valErr}
	}
	return candidate, nil
}

func (s *Service) runASRPipeline(ctx context.Context, mediaFileID string, cfg AIConfigSnapshot, report progressFunc) (vttText string, language string, err error) {
	if !cfg.ASRConfigured {
		return "", "", &pipelineError{code: "asr_not_configured", err: ErrASRNotConfigured}
	}
	if s.audioExtractor == nil || (!s.hasCustomExtractor && !s.media.HasFFmpeg()) {
		return "", "", &pipelineError{code: "ffmpeg_unavailable", err: errors.New("ffmpeg is unavailable for audio extraction")}
	}
	if s.media == nil {
		return "", "", &pipelineError{code: "media_unavailable", err: errors.New("media service is unavailable")}
	}

	info, err := s.media.ResolveLocalMediaInfo(ctx, mediaFileID)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", &pipelineError{code: "media_unavailable", err: err}
	}
	if !info.HasAudio {
		return "", "", &pipelineError{code: "no_audio_track", err: errors.New("media file has no audio track")}
	}

	report(StageExtractingAudio, "正在提取视频音频...", 0, 0, "asr")

	jobID := id.New()
	chunks, cleanup, err := s.audioExtractor.ExtractChunks(ctx, info.AbsPath, jobID)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", &pipelineError{code: "audio_extract_failed", err: err}
	}
	if len(chunks) == 0 {
		return "", "", &pipelineError{code: "audio_extract_failed", err: errors.New("no audio chunks extracted")}
	}

	transcriber := ai.NewOpenAICompatibleTranscriber(cfg.ASR, s.asrHTTPClient())

	var (
		allCues      []ai.TranscriptCue
		detectedLang string
	)
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		report(StageTranscribing, fmt.Sprintf("正在语音转写（分片 %d/%d）...", i+1, len(chunks)), i+1, len(chunks), "asr")

		res, txErr := transcribeChunkFile(ctx, transcriber, chunk.Path)
		if txErr != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return "", "", &pipelineError{code: "asr_failed", err: txErr}
		}

		if detectedLang == "" && strings.TrimSpace(res.Language) != "" {
			detectedLang = strings.TrimSpace(res.Language)
		}

		if len(res.Cues) > 0 {
			for _, cue := range res.Cues {
				allCues = append(allCues, ai.TranscriptCue{
					StartSec: chunk.OffsetSec + cue.StartSec,
					EndSec:   chunk.OffsetSec + cue.EndSec,
					Text:     cue.Text,
				})
			}
		} else if trimmed := strings.TrimSpace(res.Text); trimmed != "" {
			dur := chunk.DurationSec
			if dur <= 0 {
				dur = float64(DefaultAudioSegmentSeconds)
			}
			allCues = append(allCues, ai.TranscriptCue{
				StartSec: chunk.OffsetSec,
				EndSec:   chunk.OffsetSec + dur,
				Text:     trimmed,
			})
		}
	}

	if len(allCues) == 0 {
		return "", "", &pipelineError{code: "empty_transcript", err: errors.New("asr returned empty transcript")}
	}

	if detectedLang == "" {
		detectedLang = strings.TrimSpace(cfg.ASR.Language)
	}
	if detectedLang == "" {
		detectedLang = "und"
	}

	vtt := ai.FormatCuesAsWebVTT(allCues)
	plainParts := make([]string, 0, len(allCues))
	for _, c := range allCues {
		if t := strings.TrimSpace(c.Text); t != "" {
			plainParts = append(plainParts, t)
		}
	}
	plain := strings.Join(plainParts, "\n")

	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if err := s.upsertMediaTranscript(ctx, mediaFileID, "asr", detectedLang, cfg.ASR.Model, vtt, plain); err != nil {
		return "", "", err
	}

	return vtt, detectedLang, nil
}

func transcribeChunkFile(ctx context.Context, transcriber *ai.OpenAICompatibleTranscriber, chunkPath string) (*ai.TranscribeResult, error) {
	f, err := os.Open(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("open audio chunk %s: %w", chunkPath, err)
	}
	defer f.Close()
	return transcriber.Transcribe(ctx, filepath.Base(chunkPath), f)
}

type cachedTranscriptRow struct {
	source     string
	language   string
	contentVTT string
	plainText  string
}

func (s *Service) loadCachedTranscript(ctx context.Context, mediaFileID string) (*cachedTranscriptRow, bool, error) {
	var row cachedTranscriptRow
	err := s.db.QueryRowContext(ctx, `
		SELECT source, language, content_vtt, plain_text
		FROM media_transcripts
		WHERE media_file_id = ?`, mediaFileID,
	).Scan(&row.source, &row.language, &row.contentVTT, &row.plainText)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("query media_transcripts: %w", err)
	}
	if strings.TrimSpace(row.contentVTT) == "" && strings.TrimSpace(row.plainText) == "" {
		return nil, false, nil
	}
	return &row, true, nil
}

func (s *Service) upsertMediaTranscript(ctx context.Context, mediaFileID, source, language, model, contentVTT, plainText string) error {
	if strings.TrimSpace(language) == "" {
		language = "und"
	}
	now := nowUTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(media_file_id) DO UPDATE SET
			source = excluded.source,
			language = excluded.language,
			model = excluded.model,
			content_vtt = excluded.content_vtt,
			plain_text = excluded.plain_text,
			updated_at = excluded.updated_at`,
		mediaFileID, source, language, model, contentVTT, plainText, now, now,
	)
	if err != nil {
		return fmt.Errorf("upsert media_transcripts: %w", err)
	}
	return nil
}

func (s *Service) upsertLessonSummary(
	ctx context.Context,
	lessonID, mediaFileID, sourceType, sourceLabel string,
	hasTimestamps bool,
	model string,
	out *ai.LessonSummaryOutput,
) error {
	outlineJSON, err := json.Marshal(out.Outline)
	if err != nil {
		return fmt.Errorf("marshal outline_json: %w", err)
	}
	timelineJSON, err := json.Marshal(out.Timeline)
	if err != nil {
		return fmt.Errorf("marshal timeline_json: %w", err)
	}
	hasTSInt := 0
	if hasTimestamps {
		hasTSInt = 1
	}
	now := nowUTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO lesson_summaries (
			lesson_id, media_file_id, source_type, source_label, has_timestamps,
			model, overview, outline_json, timeline_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(lesson_id) DO UPDATE SET
			media_file_id = excluded.media_file_id,
			source_type = excluded.source_type,
			source_label = excluded.source_label,
			has_timestamps = excluded.has_timestamps,
			model = excluded.model,
			overview = excluded.overview,
			outline_json = excluded.outline_json,
			timeline_json = excluded.timeline_json,
			updated_at = excluded.updated_at`,
		lessonID, mediaFileID, sourceType, sourceLabel, hasTSInt,
		model, out.Overview, string(outlineJSON), string(timelineJSON), now, now,
	)
	if err != nil {
		return fmt.Errorf("upsert lesson_summaries: %w", err)
	}
	return nil
}

func (s *Service) loadLessonSummary(ctx context.Context, lessonID string) (*LessonSummary, error) {
	var (
		row          LessonSummary
		hasTSInt     int
		outlineJSON  string
		timelineJSON string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT lesson_id, media_file_id, source_type, source_label, has_timestamps,
		       model, overview, outline_json, timeline_json, created_at, updated_at
		FROM lesson_summaries
		WHERE lesson_id = ?`, lessonID,
	).Scan(
		&row.LessonID, &row.MediaFileID, &row.SourceType, &row.SourceLabel, &hasTSInt,
		&row.Model, &row.Overview, &outlineJSON, &timelineJSON, &row.CreatedAt, &row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query lesson_summaries: %w", err)
	}
	row.HasTimestamps = hasTSInt != 0
	row.Outline = []ai.OutlineItem{}
	row.Timeline = []ai.TimelineSection{}

	if strings.TrimSpace(outlineJSON) != "" {
		if err := json.Unmarshal([]byte(outlineJSON), &row.Outline); err != nil {
			return nil, fmt.Errorf("parse outline_json: %w", err)
		}
		if row.Outline == nil {
			row.Outline = []ai.OutlineItem{}
		}
	}
	if strings.TrimSpace(timelineJSON) != "" {
		if err := json.Unmarshal([]byte(timelineJSON), &row.Timeline); err != nil {
			return nil, fmt.Errorf("parse timeline_json: %w", err)
		}
		if row.Timeline == nil {
			row.Timeline = []ai.TimelineSection{}
		}
	}
	return &row, nil
}

func (s *Service) computeLessonCapabilities(ctx context.Context, lesson *catalog.LessonDetail, cfg AIConfigSnapshot) (LessonCapabilities, error) {
	caps := LessonCapabilities{
		AIConfigured:  cfg.AIConfigured,
		ASRConfigured: cfg.ASRConfigured,
		HasFFmpeg:     s.media != nil && s.media.HasFFmpeg(),
	}

	if s.media != nil && lesson.MediaFileID != "" {
		streams, err := s.media.ResolveStreams(ctx, lesson.MediaFileID)
		if err == nil {
			for _, st := range streams {
				if !strings.EqualFold(st.Type, "subtitle") || st.IsAIGenerated {
					continue
				}
				if st.IsExternal || !isBitmapCodec(st.Codec) {
					caps.HasSubtitle = true
					break
				}
			}
		}
	}

	if lesson.MediaFileID != "" {
		cached, found, err := s.loadCachedTranscript(ctx, lesson.MediaFileID)
		if err != nil {
			return LessonCapabilities{}, err
		}
		if found {
			caps.HasCachedTranscript = true
			caps.TranscriptSource = cached.source
		}
	}

	return caps, nil
}

func (s *Service) loadLessonHierarchyTitles(ctx context.Context, lessonID string) (courseTitle, chapterTitle string) {
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(c.title, ''), COALESCE(ch.title, '')
		FROM lessons l
		LEFT JOIN courses c ON c.id = l.course_id
		LEFT JOIN chapters ch ON ch.id = l.chapter_id
		WHERE l.id = ?`, lessonID,
	).Scan(&courseTitle, &chapterTitle)
	return courseTitle, chapterTitle
}

func formatASRSourceLabel(language string) string {
	lang := strings.TrimSpace(language)
	if lang == "" || strings.EqualFold(lang, "und") {
		return "AI 语音转写"
	}
	return fmt.Sprintf("AI 语音转写 (%s)", lang)
}

func isBitmapCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "dvd_subtitle", "hdmv_pgs_subtitle", "pgssub", "dvb_subtitle", "xsub", "dvdsub":
		return true
	default:
		return false
	}
}

func cloneBatchStatus(st CourseBatchStatus) CourseBatchStatus {
	cp := st
	if st.Errors != nil {
		cp.Errors = make([]BatchLessonError, len(st.Errors))
		copy(cp.Errors, st.Errors)
	} else {
		cp.Errors = []BatchLessonError{}
	}
	return cp
}

func normalizeOrRaw(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if norm, ok := id.Normalize(trimmed); ok {
		return norm
	}
	return trimmed
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
