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
	ErrLessonJobConflict   = errors.New("lesson has conflicting active job")
	ErrInvalidInput        = errors.New("invalid input")

	errNoTranscriptReady = errors.New("no transcript available for lesson")
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
	AI                ai.Config
	ASR               ai.ASRConfig
	ASRSegmentSeconds int
	AIConfigured      bool
	ASRConfigured     bool
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

	asrSegSec := DefaultAudioSegmentSeconds
	if raw := strings.TrimSpace(settings["ai_asr_segment_seconds"]); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= MinAudioSegmentSeconds && v <= MaxAudioSegmentSeconds {
			asrSegSec = v
		}
	}

	asrTimeoutSec := ai.DefaultASRTimeoutSeconds
	if raw := strings.TrimSpace(settings["ai_asr_timeout_seconds"]); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= ai.MinASRTimeoutSeconds && v <= ai.MaxASRTimeoutSeconds {
			asrTimeoutSec = v
		}
	}

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
			BaseURL:        effectiveASRBaseURL,
			APIKey:         effectiveASRAPIKey,
			Model:          asrModel,
			Language:       asrLanguage,
			TimeoutSeconds: asrTimeoutSec,
		},
		ASRSegmentSeconds: asrSegSec,
		AIConfigured:      aiConfigured,
		ASRConfigured:     asrConfigured,
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
	HasSubtitle             bool   `json:"has_subtitle"`
	HasCachedTranscript     bool   `json:"has_cached_transcript"`
	TranscriptSource        string `json:"transcript_source"`
	PartialTranscriptChunks int    `json:"partial_transcript_chunks"`
	PartialTranscriptTotal  int    `json:"partial_transcript_total"`
	AIConfigured            bool   `json:"ai_configured"`
	ASRConfigured           bool   `json:"asr_configured"`
	HasFFmpeg               bool   `json:"has_ffmpeg"`
}

// LessonJobAction identifies whether a lesson job runs transcription only, summarization only, or both.
type LessonJobAction string

const (
	ActionTranscribe             LessonJobAction = "transcribe"
	ActionSummarize              LessonJobAction = "summarize"
	ActionTranscribeAndSummarize LessonJobAction = "transcribe_and_summarize"
)

// LessonJobStatus describes the live or most recent background summary/transcription job for one lesson.
type LessonJobStatus struct {
	LessonID   string          `json:"lesson_id"`
	CourseID   string          `json:"course_id"`
	Action     LessonJobAction `json:"action,omitempty"`
	Running    bool            `json:"running"`
	Stage      JobStage        `json:"stage"`
	Message    string          `json:"message"`
	ChunkIndex int             `json:"chunk_index"`
	ChunkTotal int             `json:"chunk_total"`
	SourceType string          `json:"source_type"`
	Error      string          `json:"error,omitempty"`
	ErrorCode  string          `json:"error_code,omitempty"`
	StartedAt  string          `json:"started_at"`
	FinishedAt string          `json:"finished_at,omitempty"`
}

// BatchLessonError captures an individual lesson failure inside a course batch run.
type BatchLessonError struct {
	LessonID    string `json:"lesson_id"`
	LessonTitle string `json:"lesson_title"`
	Error       string `json:"error"`
	ErrorCode   string `json:"error_code,omitempty"`
}

// BatchKind distinguishes batch transcription jobs from batch summary jobs.
type BatchKind string

const (
	BatchKindTranscribe BatchKind = "transcribe"
	BatchKindSummarize  BatchKind = "summarize"
)

// CourseBatchStatus describes the live or most recent batch transcription or summary job for a course.
type CourseBatchStatus struct {
	CourseID            string             `json:"course_id"`
	Kind                BatchKind          `json:"kind,omitempty"`
	ChapterID           string             `json:"chapter_id,omitempty"`
	ChapterTitle        string             `json:"chapter_title,omitempty"`
	Running             bool               `json:"running"`
	Total               int                `json:"total"`
	Completed           int                `json:"completed"`
	Skipped             int                `json:"skipped"`
	SkippedAlreadyDone  int                `json:"skipped_already_done"`
	SkippedNoTranscript int                `json:"skipped_no_transcript"`
	Failed              int                `json:"failed"`
	CurrentLessonID     string             `json:"current_lesson_id"`
	CurrentLessonTitle  string             `json:"current_lesson_title"`
	CurrentStage        JobStage           `json:"current_stage"`
	CurrentMessage      string             `json:"current_message,omitempty"`
	ChunkIndex          int                `json:"chunk_index"`
	ChunkTotal          int                `json:"chunk_total"`
	Errors              []BatchLessonError `json:"errors"`
	StartedAt           string             `json:"started_at"`
	FinishedAt          string             `json:"finished_at,omitempty"`
}

// LessonSummaryState aggregates saved summary, active/recent job status, and lesson capabilities.
type LessonSummaryState struct {
	LessonID     string             `json:"lesson_id"`
	Summary      *LessonSummary     `json:"summary"`
	Job          *LessonJobStatus   `json:"job"`
	Capabilities LessonCapabilities `json:"capabilities"`
}

// LessonPipelineStatus summarizes transcript readiness, chunk checkpoints, and summary status for one lesson.
type LessonPipelineStatus struct {
	LessonID            string `json:"lesson_id"`
	ChapterID           string `json:"chapter_id"`
	HasSubtitle         bool   `json:"has_subtitle"`
	HasCachedTranscript bool   `json:"has_cached_transcript"`
	TranscriptStatus    string `json:"transcript_status"`
	TranscriptReady     bool   `json:"transcript_ready"`
	TranscribedChunks   int    `json:"transcribed_chunks,omitempty"`
	TotalChunks         int    `json:"total_chunks,omitempty"`
	AISubtitleUpdatedAt string `json:"ai_subtitle_updated_at,omitempty"`
	HasSummary          bool   `json:"has_summary"`
}

// CoursePipelineCounts aggregates lesson-level transcript and summary counts for a course.
type CoursePipelineCounts struct {
	TotalLessons       int `json:"total_lessons"`
	SubtitleLessons    int `json:"subtitle_lessons"`
	TranscribedLessons int `json:"transcribed_lessons"`
	TranscriptReady    int `json:"transcript_ready"`
	PartialLessons     int `json:"partial_lessons"`
	MissingTranscript  int `json:"missing_transcript"`
	SummarizedLessons  int `json:"summarized_lessons"`
	ReadyToSummarize   int `json:"ready_to_summarize"`
}

// CourseSummariesStatus returns the per-lesson pipeline statuses, aggregate counts, and batch job states for a course.
type CourseSummariesStatus struct {
	CourseID            string                 `json:"course_id"`
	SummaryLessonIDs    []string               `json:"summary_lesson_ids"`
	TranscriptLessonIDs []string               `json:"transcript_lesson_ids"`
	Lessons             []LessonPipelineStatus `json:"lessons"`
	Counts              CoursePipelineCounts   `json:"counts"`
	BatchJob            *CourseBatchStatus     `json:"batch_job"`
	TranscribeBatchJob  *CourseBatchStatus     `json:"transcribe_batch_job"`
	SummaryBatchJob     *CourseBatchStatus     `json:"summary_batch_job"`
}

// GenerateLessonOptions configures single-lesson summary generation.
type GenerateLessonOptions struct {
	Force             bool   `json:"force"`
	ForceRetranscribe bool   `json:"force_retranscribe"`
	ManualTranscript  string `json:"manual_transcript"`
}

// TranscribeLessonOptions configures single-lesson speech-to-text transcription.
type TranscribeLessonOptions struct {
	ForceRetranscribe bool `json:"force_retranscribe"`
}

// CourseBatchOptions configures course-wide or chapter-scoped batch transcription/summarization.
type CourseBatchOptions struct {
	Mode      string `json:"mode"`
	ChapterID string `json:"chapter_id"`
}

// Options configures a summary Service instance.
type Options struct {
	DB                  *sql.DB
	DataDir             string
	Catalog             *catalog.Service
	Media               *media.Service
	LifecycleCtx        context.Context
	HTTPClient          *http.Client
	ConfigLoader        ConfigLoader
	AudioExtractor      AudioExtractor
	AudioSegmentSeconds int
	SinglePassMaxRunes  int
	MapChunkMaxRunes    int
	CompactWindowSec    float64
	CompactMaxRunes     int
}

type lessonJobEntry struct {
	status          LessonJobStatus
	action          LessonJobAction
	queued          bool
	queueKind       BatchKind
	ownerIsBatch    bool
	cancelRequested bool
	cancel          context.CancelFunc
}

type singleTranscribeRequest struct {
	lessonID          string
	courseID          string
	mediaFileID       string
	forceRetranscribe bool
	thenSummarize     bool
	summaryOpts       GenerateLessonOptions
}

type singleSummaryRequest struct {
	lessonID               string
	courseID               string
	mediaFileID            string
	preferCachedTranscript bool
	opts                   GenerateLessonOptions
}

type batchLessonItem struct {
	lessonID          string
	lessonTitle       string
	mediaFileID       string
	forceRetranscribe bool
}

type batchEntry struct {
	kind               BatchKind
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
// and dual independent serial priority + batch job queues.
type Service struct {
	db                 *sql.DB
	dataDir            string
	catalog            *catalog.Service
	media              *media.Service
	lifecycleCtx       context.Context
	httpClient         *http.Client
	defaultLLMClient   *http.Client
	configLoader       ConfigLoader
	audioExtractor      AudioExtractor
	hasCustomExtractor  bool
	hasCustomSegmentSec bool
	audioSegmentSec     int
	singlePassMaxRunes int
	mapChunkMaxRunes   int
	compactWindowSec   float64
	compactMaxRunes    int

	mu   sync.Mutex
	cond *sync.Cond
	wg   sync.WaitGroup

	transcribeWorkerActive   bool
	singleTranscribeQueue    []*singleTranscribeRequest
	batchTranscribeQueue     []*batchEntry
	courseTranscribeBatches  map[string]*batchEntry
	activeTranscribeLessonID string
	activeTranscribeMediaID  string

	summaryWorkerActive   bool
	singleSummaryQueue    []*singleSummaryRequest
	batchSummaryQueue     []*batchEntry
	courseSummaryBatches  map[string]*batchEntry
	activeSummaryLessonID string
	activeSummaryMediaID  string

	lessonJobs map[string]*lessonJobEntry
}

// NewService constructs a summary Service. Background workers are started lazily on first enqueue.
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

	hasCustomSegmentSec := opts.AudioSegmentSeconds > 0
	segSec := opts.AudioSegmentSeconds
	if segSec <= 0 {
		segSec = DefaultAudioSegmentSeconds
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
			segSec,
		)
	}

	s := &Service{
		db:                      opts.DB,
		dataDir:                 opts.DataDir,
		catalog:                 opts.Catalog,
		media:                   opts.Media,
		lifecycleCtx:            lctx,
		httpClient:              opts.HTTPClient,
		defaultLLMClient:        &http.Client{Timeout: defaultLLMTimeout},
		configLoader:            opts.ConfigLoader,
		audioExtractor:          extractor,
		hasCustomExtractor:      hasCustomExtractor,
		hasCustomSegmentSec:     hasCustomSegmentSec,
		audioSegmentSec:         segSec,
		singlePassMaxRunes:      singlePassMax,
		mapChunkMaxRunes:        mapChunkMax,
		compactWindowSec:        compactWin,
		compactMaxRunes:         compactMax,
		courseTranscribeBatches: make(map[string]*batchEntry),
		courseSummaryBatches:    make(map[string]*batchEntry),
		lessonJobs:              make(map[string]*lessonJobEntry),
	}
	s.cond = sync.NewCond(&s.mu)
	context.AfterFunc(lctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	return s
}

func (s *Service) effectiveAudioSegmentSec(cfg AIConfigSnapshot) int {
	if s.hasCustomSegmentSec && s.audioSegmentSec > 0 {
		return s.audioSegmentSec
	}
	if cfg.ASRSegmentSeconds >= MinAudioSegmentSeconds && cfg.ASRSegmentSeconds <= MaxAudioSegmentSeconds {
		return cfg.ASRSegmentSeconds
	}
	if s.audioSegmentSec > 0 {
		return s.audioSegmentSec
	}
	return DefaultAudioSegmentSeconds
}

func (s *Service) resolveExpectedSegmentSec(ctx context.Context) int {
	if s.hasCustomSegmentSec && s.audioSegmentSec > 0 {
		return s.audioSegmentSec
	}
	if cfg, err := s.loadConfig(ctx); err == nil {
		return s.effectiveAudioSegmentSec(cfg)
	}
	if s.audioSegmentSec > 0 {
		return s.audioSegmentSec
	}
	return DefaultAudioSegmentSeconds
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

// StartLessonTranscribe validates ASR configuration (without requiring LLM configuration)
// and enqueues a single-lesson transcription job onto the high-priority transcribe queue.
func (s *Service) StartLessonTranscribe(ctx context.Context, lessonID string, opts TranscribeLessonOptions) (*LessonJobStatus, error) {
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
		if entry.action == ActionTranscribe || entry.action == ActionTranscribeAndSummarize {
			cp := entry.status
			s.mu.Unlock()
			return &cp, nil
		}
		s.mu.Unlock()
		return nil, ErrLessonJobConflict
	}
	s.mu.Unlock()

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.ASRConfigured {
		return nil, ErrASRNotConfigured
	}

	if !opts.ForceRetranscribe {
		caps, capErr := s.computeLessonCapabilities(ctx, lesson, cfg)
		if capErr != nil {
			return nil, capErr
		}
		if caps.HasSubtitle || caps.HasCachedTranscript {
			now := nowUTC()
			return &LessonJobStatus{
				LessonID:   lesson.ID,
				CourseID:   lesson.CourseID,
				Action:     ActionTranscribe,
				Running:    false,
				Stage:      StageCompleted,
				Message:    "已有可用字幕或转写文稿",
				StartedAt:  now,
				FinishedAt: now,
			}, nil
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.lifecycleCtx.Err(); err != nil {
		return nil, err
	}
	if entry, ok := s.lessonJobs[lesson.ID]; ok && entry.status.Running {
		if entry.action == ActionTranscribe || entry.action == ActionTranscribeAndSummarize {
			cp := entry.status
			return &cp, nil
		}
		return nil, ErrLessonJobConflict
	}

	now := nowUTC()
	entry := &lessonJobEntry{
		status: LessonJobStatus{
			LessonID:  lesson.ID,
			CourseID:  lesson.CourseID,
			Action:    ActionTranscribe,
			Running:   true,
			Stage:     StageQueued,
			Message:   "等待语音转文字...",
			StartedAt: now,
		},
		action:    ActionTranscribe,
		queued:    true,
		queueKind: BatchKindTranscribe,
	}
	s.lessonJobs[lesson.ID] = entry
	s.singleTranscribeQueue = append(s.singleTranscribeQueue, &singleTranscribeRequest{
		lessonID:          lesson.ID,
		courseID:          lesson.CourseID,
		mediaFileID:       lesson.MediaFileID,
		forceRetranscribe: opts.ForceRetranscribe,
		thenSummarize:     false,
	})
	s.ensureTranscribeWorkerLocked()

	cp := entry.status
	return &cp, nil
}

// StartLessonSummary validates configuration and enqueues a single-lesson summary job.
// If ASR transcription is required, it starts on the transcribe worker and hands off to the summary worker.
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
		if entry.action == ActionSummarize || entry.action == ActionTranscribeAndSummarize {
			cp := entry.status
			s.mu.Unlock()
			return &cp, nil
		}
		s.mu.Unlock()
		return nil, ErrLessonJobConflict
	}
	s.mu.Unlock()

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.AIConfigured {
		return nil, ErrAINotConfigured
	}

	needsASR := false
	manual := strings.TrimSpace(opts.ManualTranscript)
	if manual == "" {
		if opts.ForceRetranscribe {
			if !cfg.ASRConfigured {
				return nil, ErrASRNotConfigured
			}
			needsASR = true
		} else {
			caps, capErr := s.computeLessonCapabilities(ctx, lesson, cfg)
			if capErr != nil {
				return nil, capErr
			}
			if !caps.HasSubtitle && !caps.HasCachedTranscript {
				if !cfg.ASRConfigured {
					return nil, ErrASRNotConfigured
				}
				needsASR = true
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.lifecycleCtx.Err(); err != nil {
		return nil, err
	}
	if entry, ok := s.lessonJobs[lesson.ID]; ok && entry.status.Running {
		if entry.action == ActionSummarize || entry.action == ActionTranscribeAndSummarize {
			cp := entry.status
			return &cp, nil
		}
		return nil, ErrLessonJobConflict
	}

	now := nowUTC()
	if needsASR {
		entry := &lessonJobEntry{
			status: LessonJobStatus{
				LessonID:  lesson.ID,
				CourseID:  lesson.CourseID,
				Action:    ActionTranscribeAndSummarize,
				Running:   true,
				Stage:     StageQueued,
				Message:   "等待语音转文字...",
				StartedAt: now,
			},
			action:    ActionTranscribeAndSummarize,
			queued:    true,
			queueKind: BatchKindTranscribe,
		}
		s.lessonJobs[lesson.ID] = entry
		s.singleTranscribeQueue = append(s.singleTranscribeQueue, &singleTranscribeRequest{
			lessonID:          lesson.ID,
			courseID:          lesson.CourseID,
			mediaFileID:       lesson.MediaFileID,
			forceRetranscribe: opts.ForceRetranscribe,
			thenSummarize:     true,
			summaryOpts:       opts,
		})
		s.ensureTranscribeWorkerLocked()
		cp := entry.status
		return &cp, nil
	}

	entry := &lessonJobEntry{
		status: LessonJobStatus{
			LessonID:  lesson.ID,
			CourseID:  lesson.CourseID,
			Action:    ActionSummarize,
			Running:   true,
			Stage:     StageQueued,
			Message:   "等待生成 AI 总结...",
			StartedAt: now,
		},
		action:    ActionSummarize,
		queued:    true,
		queueKind: BatchKindSummarize,
	}
	s.lessonJobs[lesson.ID] = entry
	s.singleSummaryQueue = append(s.singleSummaryQueue, &singleSummaryRequest{
		lessonID:    lesson.ID,
		courseID:    lesson.CourseID,
		mediaFileID: lesson.MediaFileID,
		opts:        opts,
	})
	s.ensureSummaryWorkerLocked()

	cp := entry.status
	return &cp, nil
}

// CancelLessonTranscribe cancels a queued or active single-lesson transcription (or combined) job.
func (s *Service) CancelLessonTranscribe(lessonID string) (*LessonJobStatus, error) {
	return s.cancelLessonJob(lessonID)
}

// CancelLessonSummary cancels a queued or active single-lesson summary (or combined) job.
func (s *Service) CancelLessonSummary(lessonID string) (*LessonJobStatus, error) {
	return s.cancelLessonJob(lessonID)
}

func (s *Service) cancelLessonJob(lessonID string) (*LessonJobStatus, error) {
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
		s.removeSingleTranscribeQueuedLocked(lessonID)
		s.removeSingleSummaryQueuedLocked(lessonID)
		entry.queued = false
		entry.status.Running = false
		entry.status.Stage = StageCancelled
		entry.status.Message = "已取消"
		entry.status.FinishedAt = nowUTC()
	} else if entry.cancel != nil {
		entry.status.Message = "正在取消..."
		entry.cancel()
	}
	s.cond.Broadcast()

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
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_transcript_chunks WHERE media_file_id = ?`, lesson.MediaFileID); err != nil {
			return fmt.Errorf("delete media_transcript_chunks: %w", err)
		}
	}
	return tx.Commit()
}

// GetCourseSummaryStatus returns per-lesson pipeline statuses, aggregate counts, and batch job status for courseID.
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

	snap, err := s.computeCourseLessonsPipelineStatus(ctx, courseID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	var transcribeCopy, summaryCopy *CourseBatchStatus
	if batch, ok := s.courseTranscribeBatches[courseID]; ok {
		cp := cloneBatchStatus(batch.status)
		transcribeCopy = &cp
	}
	if batch, ok := s.courseSummaryBatches[courseID]; ok {
		cp := cloneBatchStatus(batch.status)
		summaryCopy = &cp
	}
	s.mu.Unlock()

	return &CourseSummariesStatus{
		CourseID:            courseID,
		SummaryLessonIDs:    snap.summaryLessonIDs,
		TranscriptLessonIDs: snap.transcriptLessonIDs,
		Lessons:             snap.lessons,
		Counts:              snap.counts,
		BatchJob:            summaryCopy,
		TranscribeBatchJob:  transcribeCopy,
		SummaryBatchJob:     summaryCopy,
	}, nil
}

type preparedBatchScope struct {
	courseID     string
	mode         string
	chapterID    string
	chapterTitle string
	snap         *coursePipelineSnapshot
	mediaByID    map[string]string
	items        []LessonPipelineStatus
}

func (s *Service) prepareCourseBatchScope(ctx context.Context, courseID string, opts CourseBatchOptions) (*preparedBatchScope, error) {
	courseID = normalizeOrRaw(courseID)
	if courseID == "" {
		return nil, ErrInvalidInput
	}

	modeNorm := strings.ToLower(strings.TrimSpace(opts.Mode))
	if modeNorm == "" {
		modeNorm = "missing_only"
	}
	if modeNorm != "missing_only" && modeNorm != "all" {
		return nil, fmt.Errorf("%w: unsupported batch mode %q", ErrInvalidInput, opts.Mode)
	}

	chapterID := normalizeOrRaw(opts.ChapterID)

	course, err := s.catalog.GetCourseDetail(ctx, courseID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var chapterTitle string
	mediaByLesson := make(map[string]string)
	foundChapter := chapterID == ""
	for _, ch := range course.Chapters {
		if ch.ID == chapterID {
			foundChapter = true
			chapterTitle = ch.Title
		}
		for _, l := range ch.Lessons {
			mediaByLesson[l.ID] = l.MediaFileID
		}
	}
	if !foundChapter {
		return nil, ErrNotFound
	}

	snap, err := s.computeCourseLessonsPipelineStatus(ctx, course.ID)
	if err != nil {
		return nil, err
	}

	var items []LessonPipelineStatus
	for _, lp := range snap.lessons {
		if chapterID != "" && lp.ChapterID != chapterID {
			continue
		}
		items = append(items, lp)
	}

	return &preparedBatchScope{
		courseID:     course.ID,
		mode:         modeNorm,
		chapterID:    chapterID,
		chapterTitle: chapterTitle,
		snap:         snap,
		mediaByID:    mediaByLesson,
		items:        items,
	}, nil
}

// StartCourseTranscribeBatch validates ASR configuration (without requiring AI LLM configuration)
// and queues target lessons in courseID (optionally scoped to ChapterID) for serial transcription.
func (s *Service) StartCourseTranscribeBatch(ctx context.Context, courseID string, opts CourseBatchOptions) (*CourseBatchStatus, error) {
	scope, err := s.prepareCourseBatchScope(ctx, courseID, opts)
	if err != nil {
		return nil, err
	}

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.ASRConfigured {
		return nil, ErrASRNotConfigured
	}

	s.mu.Lock()
	if existing, ok := s.courseTranscribeBatches[scope.courseID]; ok && existing.status.Running {
		cp := cloneBatchStatus(existing.status)
		s.mu.Unlock()
		return &cp, ErrBatchAlreadyRunning
	}
	s.mu.Unlock()

	var pending []batchLessonItem
	skippedAlreadyDone := 0
	for _, lp := range scope.items {
		if scope.mode == "missing_only" {
			if lp.TranscriptReady {
				skippedAlreadyDone++
				continue
			}
		} else if lp.HasSubtitle {
			// "all" mode re-transcribes cached/missing lessons while skipping physical subtitles.
			skippedAlreadyDone++
			continue
		}
		pending = append(pending, batchLessonItem{
			lessonID:          lp.LessonID,
			lessonTitle:       scope.snap.lessonTitles[lp.LessonID],
			mediaFileID:       scope.mediaByID[lp.LessonID],
			forceRetranscribe: scope.mode == "all",
		})
	}

	now := nowUTC()
	status := CourseBatchStatus{
		CourseID:           scope.courseID,
		Kind:               BatchKindTranscribe,
		ChapterID:          scope.chapterID,
		ChapterTitle:       scope.chapterTitle,
		Running:            len(pending) > 0,
		Total:              len(scope.items),
		Skipped:            skippedAlreadyDone,
		SkippedAlreadyDone: skippedAlreadyDone,
		Errors:             []BatchLessonError{},
		StartedAt:          now,
	}
	if len(pending) == 0 {
		status.CurrentStage = StageCompleted
		status.FinishedAt = now
	} else {
		status.CurrentStage = StageQueued
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.courseTranscribeBatches[scope.courseID]; ok && existing.status.Running {
		cp := cloneBatchStatus(existing.status)
		return &cp, ErrBatchAlreadyRunning
	}
	if len(pending) > 0 {
		if err := s.lifecycleCtx.Err(); err != nil {
			return nil, err
		}
	}

	entry := &batchEntry{
		kind:    BatchKindTranscribe,
		status:  status,
		mode:    scope.mode,
		pending: pending,
	}
	s.courseTranscribeBatches[scope.courseID] = entry
	if len(pending) > 0 {
		s.batchTranscribeQueue = append(s.batchTranscribeQueue, entry)
		s.ensureTranscribeWorkerLocked()
	}

	cp := cloneBatchStatus(entry.status)
	return &cp, nil
}

// StartCourseSummaryBatch validates AI configuration and queues lessons with available transcripts
// in courseID (optionally scoped to ChapterID) for serial summarization without triggering ASR.
func (s *Service) StartCourseSummaryBatch(ctx context.Context, courseID string, opts CourseBatchOptions) (*CourseBatchStatus, error) {
	scope, err := s.prepareCourseBatchScope(ctx, courseID, opts)
	if err != nil {
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
	if existing, ok := s.courseSummaryBatches[scope.courseID]; ok && existing.status.Running {
		cp := cloneBatchStatus(existing.status)
		s.mu.Unlock()
		return &cp, ErrBatchAlreadyRunning
	}
	s.mu.Unlock()

	var pending []batchLessonItem
	skippedAlreadyDone := 0
	skippedNoTranscript := 0
	for _, lp := range scope.items {
		if scope.mode == "missing_only" && lp.HasSummary {
			skippedAlreadyDone++
			continue
		}
		if !lp.TranscriptReady {
			skippedNoTranscript++
			continue
		}
		pending = append(pending, batchLessonItem{
			lessonID:    lp.LessonID,
			lessonTitle: scope.snap.lessonTitles[lp.LessonID],
			mediaFileID: scope.mediaByID[lp.LessonID],
		})
	}

	now := nowUTC()
	status := CourseBatchStatus{
		CourseID:            scope.courseID,
		Kind:                BatchKindSummarize,
		ChapterID:           scope.chapterID,
		ChapterTitle:        scope.chapterTitle,
		Running:             len(pending) > 0,
		Total:               len(scope.items),
		Skipped:             skippedAlreadyDone + skippedNoTranscript,
		SkippedAlreadyDone:  skippedAlreadyDone,
		SkippedNoTranscript: skippedNoTranscript,
		Errors:              []BatchLessonError{},
		StartedAt:           now,
	}
	if len(pending) == 0 {
		status.CurrentStage = StageCompleted
		status.FinishedAt = now
	} else {
		status.CurrentStage = StageQueued
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.courseSummaryBatches[scope.courseID]; ok && existing.status.Running {
		cp := cloneBatchStatus(existing.status)
		return &cp, ErrBatchAlreadyRunning
	}
	if len(pending) > 0 {
		if err := s.lifecycleCtx.Err(); err != nil {
			return nil, err
		}
	}

	entry := &batchEntry{
		kind:    BatchKindSummarize,
		status:  status,
		mode:    scope.mode,
		pending: pending,
	}
	s.courseSummaryBatches[scope.courseID] = entry
	if len(pending) > 0 {
		s.batchSummaryQueue = append(s.batchSummaryQueue, entry)
		s.ensureSummaryWorkerLocked()
	}

	cp := cloneBatchStatus(entry.status)
	return &cp, nil
}

// StartCourseBatch is a backward-compatible wrapper around StartCourseSummaryBatch.
func (s *Service) StartCourseBatch(ctx context.Context, courseID string, mode string) (*CourseBatchStatus, error) {
	return s.StartCourseSummaryBatch(ctx, courseID, CourseBatchOptions{Mode: mode})
}

// CancelCourseTranscribeBatch cancels the active/queued transcription batch for courseID.
func (s *Service) CancelCourseTranscribeBatch(courseID string) (*CourseBatchStatus, error) {
	return s.cancelCourseBatch(courseID, BatchKindTranscribe)
}

// CancelCourseSummaryBatch cancels the active/queued summary batch for courseID.
func (s *Service) CancelCourseSummaryBatch(courseID string) (*CourseBatchStatus, error) {
	return s.cancelCourseBatch(courseID, BatchKindSummarize)
}

// CancelCourseBatch is a backward-compatible wrapper around CancelCourseSummaryBatch.
func (s *Service) CancelCourseBatch(courseID string) (*CourseBatchStatus, error) {
	return s.CancelCourseSummaryBatch(courseID)
}

func (s *Service) cancelCourseBatch(courseID string, kind BatchKind) (*CourseBatchStatus, error) {
	courseID = normalizeOrRaw(courseID)
	if courseID == "" {
		return nil, ErrInvalidInput
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	batches := s.courseSummaryBatches
	queuePtr := &s.batchSummaryQueue
	if kind == BatchKindTranscribe {
		batches = s.courseTranscribeBatches
		queuePtr = &s.batchTranscribeQueue
	}

	batch, ok := batches[courseID]
	if !ok {
		return &CourseBatchStatus{
			CourseID:     courseID,
			Kind:         kind,
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
	removeBatchEntryLocked(queuePtr, batch)

	if batch.activeLessonCancel != nil {
		if entry, ok := s.lessonJobs[batch.activeLessonID]; ok {
			entry.cancelRequested = true
			entry.status.Message = "正在取消..."
		}
		batch.status.CurrentMessage = "正在取消..."
		batch.activeLessonCancel()
	} else {
		finishBatchLocked(batch, StageCancelled)
	}
	s.cond.Broadcast()

	cp := cloneBatchStatus(batch.status)
	return &cp, nil
}

func (s *Service) ensureTranscribeWorkerLocked() {
	if s.transcribeWorkerActive {
		return
	}
	s.transcribeWorkerActive = true
	s.wg.Add(1)
	go s.runTranscribeWorker()
}

func (s *Service) ensureSummaryWorkerLocked() {
	if s.summaryWorkerActive {
		return
	}
	s.summaryWorkerActive = true
	s.wg.Add(1)
	go s.runSummaryWorker()
}

type nextTaskKind int

const (
	taskNone nextTaskKind = iota
	taskSingle
	taskBatchItem
)

func (s *Service) runTranscribeWorker() {
	defer s.wg.Done()

	for {
		s.mu.Lock()
		if s.lifecycleCtx.Err() != nil {
			s.cancelQueuedTranscribeOnShutdownLocked()
			s.transcribeWorkerActive = false
			s.cond.Broadcast()
			s.mu.Unlock()
			return
		}

		kind, singleReq, batch, item := s.popNextTranscribeTaskLocked()
		if kind == taskNone {
			s.transcribeWorkerActive = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		switch kind {
		case taskSingle:
			s.executeSingleTranscribeTask(singleReq)
		case taskBatchItem:
			s.executeTranscribeBatchItem(batch, item)
		}
	}
}

func (s *Service) runSummaryWorker() {
	defer s.wg.Done()

	for {
		s.mu.Lock()
		if s.lifecycleCtx.Err() != nil {
			s.cancelQueuedSummaryOnShutdownLocked()
			s.summaryWorkerActive = false
			s.cond.Broadcast()
			s.mu.Unlock()
			return
		}

		kind, singleReq, batch, item := s.popNextSummaryTaskLocked()
		if kind == taskNone {
			s.summaryWorkerActive = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		switch kind {
		case taskSingle:
			s.executeSingleSummaryTask(singleReq)
		case taskBatchItem:
			s.executeSummaryBatchItem(batch, item)
		}
	}
}

func (s *Service) popNextTranscribeTaskLocked() (nextTaskKind, *singleTranscribeRequest, *batchEntry, batchLessonItem) {
	if len(s.singleTranscribeQueue) > 0 {
		req := s.singleTranscribeQueue[0]
		s.singleTranscribeQueue = s.singleTranscribeQueue[1:]
		return taskSingle, req, nil, batchLessonItem{}
	}
	kind, batch, item := popBatchQueueLocked(&s.batchTranscribeQueue)
	return kind, nil, batch, item
}

func (s *Service) popNextSummaryTaskLocked() (nextTaskKind, *singleSummaryRequest, *batchEntry, batchLessonItem) {
	if len(s.singleSummaryQueue) > 0 {
		req := s.singleSummaryQueue[0]
		s.singleSummaryQueue = s.singleSummaryQueue[1:]
		return taskSingle, req, nil, batchLessonItem{}
	}
	kind, batch, item := popBatchQueueLocked(&s.batchSummaryQueue)
	return kind, nil, batch, item
}

func popBatchQueueLocked(queuePtr *[]*batchEntry) (nextTaskKind, *batchEntry, batchLessonItem) {
	q := *queuePtr
	for len(q) > 0 {
		batch := q[0]
		if batch.cancelRequested || len(batch.pending) == 0 {
			q = q[1:]
			continue
		}
		item := batch.pending[0]
		batch.pending = batch.pending[1:]
		if len(batch.pending) == 0 {
			q = q[1:]
		}
		*queuePtr = q
		return taskBatchItem, batch, item
	}
	*queuePtr = q
	return taskNone, nil, batchLessonItem{}
}

func (s *Service) removeSingleTranscribeQueuedLocked(lessonID string) {
	kept := s.singleTranscribeQueue[:0]
	for _, req := range s.singleTranscribeQueue {
		if req.lessonID == lessonID {
			continue
		}
		kept = append(kept, req)
	}
	s.singleTranscribeQueue = kept
}

func (s *Service) removeSingleSummaryQueuedLocked(lessonID string) {
	kept := s.singleSummaryQueue[:0]
	for _, req := range s.singleSummaryQueue {
		if req.lessonID == lessonID {
			continue
		}
		kept = append(kept, req)
	}
	s.singleSummaryQueue = kept
}

func removeBatchEntryLocked(queuePtr *[]*batchEntry, target *batchEntry) {
	q := *queuePtr
	kept := q[:0]
	for _, b := range q {
		if b == target {
			continue
		}
		kept = append(kept, b)
	}
	*queuePtr = kept
}

func (s *Service) markQueuedLessonCancelledLocked(lessonID, now string) {
	if entry, ok := s.lessonJobs[lessonID]; ok && entry.status.Running {
		entry.queued = false
		entry.status.Running = false
		entry.status.Stage = StageCancelled
		entry.status.Message = "已取消"
		entry.status.FinishedAt = now
	}
}

func cancelBatchQueueOnShutdownLocked(q []*batchEntry) {
	for _, batch := range q {
		batch.cancelRequested = true
		if batch.status.Running {
			finishBatchLocked(batch, StageCancelled)
		} else {
			batch.pending = nil
		}
	}
}

func (s *Service) cancelQueuedTranscribeOnShutdownLocked() {
	now := nowUTC()
	for _, req := range s.singleTranscribeQueue {
		s.markQueuedLessonCancelledLocked(req.lessonID, now)
	}
	s.singleTranscribeQueue = nil
	cancelBatchQueueOnShutdownLocked(s.batchTranscribeQueue)
	s.batchTranscribeQueue = nil
}

func (s *Service) cancelQueuedSummaryOnShutdownLocked() {
	now := nowUTC()
	for _, req := range s.singleSummaryQueue {
		s.markQueuedLessonCancelledLocked(req.lessonID, now)
	}
	s.singleSummaryQueue = nil
	cancelBatchQueueOnShutdownLocked(s.batchSummaryQueue)
	s.batchSummaryQueue = nil
}

func (s *Service) waitForWorkerSlotLocked(kind BatchKind, lessonID, mediaFileID string, ownerIsBatch bool, isCancelled func() bool) bool {
	for {
		if isCancelled() || s.lifecycleCtx.Err() != nil {
			return false
		}
		var conflict bool
		if kind == BatchKindTranscribe {
			conflict = s.activeSummaryLessonID == lessonID || (mediaFileID != "" && s.activeSummaryMediaID == mediaFileID)
		} else {
			conflict = s.activeTranscribeLessonID == lessonID || (mediaFileID != "" && s.activeTranscribeMediaID == mediaFileID)
		}
		// Ensure a batch item never overwrites a live single-lesson job (including during ASR -> LLM handoff).
		if !conflict && ownerIsBatch {
			if existing, ok := s.lessonJobs[lessonID]; ok && existing.status.Running && !existing.ownerIsBatch {
				conflict = true
			}
		}
		if !conflict {
			return true
		}
		s.cond.Wait()
	}
}

func clearBatchActiveFieldsLocked(batch *batchEntry, stage JobStage) {
	batch.status.CurrentStage = stage
	batch.status.CurrentMessage = ""
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

func (s *Service) executeSingleTranscribeTask(req *singleTranscribeRequest) {
	jobCtx, jobCancel := context.WithCancel(s.lifecycleCtx)
	defer jobCancel()

	s.mu.Lock()
	entry, ok := s.lessonJobs[req.lessonID]
	if !ok {
		entry = &lessonJobEntry{
			status: LessonJobStatus{
				LessonID:  req.lessonID,
				CourseID:  req.courseID,
				Action:    ActionTranscribe,
				StartedAt: nowUTC(),
			},
			action: ActionTranscribe,
		}
		s.lessonJobs[req.lessonID] = entry
	}
	if !s.waitForWorkerSlotLocked(BatchKindTranscribe, req.lessonID, req.mediaFileID, false, func() bool { return entry.cancelRequested }) {
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
	entry.status.Stage = StageExtractingAudio
	entry.status.Message = "正在准备语音转写..."
	s.activeTranscribeLessonID = req.lessonID
	s.activeTranscribeMediaID = req.mediaFileID
	s.mu.Unlock()

	sourceType, err := s.runLessonTranscribe(jobCtx, req.lessonID, req.forceRetranscribe, func(stage JobStage, msg string, chunkIdx, chunkTotal int, srcType string) {
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

	s.activeTranscribeLessonID = ""
	s.activeTranscribeMediaID = ""

	e, exists := s.lessonJobs[req.lessonID]
	if exists {
		e.cancel = nil
	}

	if err == nil && req.thenSummarize && exists && !e.cancelRequested && s.lifecycleCtx.Err() == nil {
		// Hand off seamlessly to summaryWorker without clearing Running status.
		e.queued = true
		e.queueKind = BatchKindSummarize
		e.status.Stage = StageQueued
		e.status.Message = "语音转写完成，等待生成 AI 总结..."
		e.status.ChunkIndex = 0
		e.status.ChunkTotal = 0
		if sourceType != "" {
			e.status.SourceType = sourceType
		}
		nextOpts := req.summaryOpts
		nextOpts.ForceRetranscribe = false
		s.singleSummaryQueue = append(s.singleSummaryQueue, &singleSummaryRequest{
			lessonID:               req.lessonID,
			courseID:               req.courseID,
			mediaFileID:            req.mediaFileID,
			preferCachedTranscript: req.forceRetranscribe,
			opts:                   nextOpts,
		})
		s.ensureSummaryWorkerLocked()
		s.cond.Broadcast()
		return
	}

	if exists {
		s.finalizeLessonJobLocked(e, sourceType, err)
	}
	s.cond.Broadcast()
}

func (s *Service) executeSingleSummaryTask(req *singleSummaryRequest) {
	jobCtx, jobCancel := context.WithCancel(s.lifecycleCtx)
	defer jobCancel()

	s.mu.Lock()
	entry, ok := s.lessonJobs[req.lessonID]
	if !ok {
		entry = &lessonJobEntry{
			status: LessonJobStatus{
				LessonID:  req.lessonID,
				CourseID:  req.courseID,
				Action:    ActionSummarize,
				StartedAt: nowUTC(),
			},
			action: ActionSummarize,
		}
		s.lessonJobs[req.lessonID] = entry
	}
	if !s.waitForWorkerSlotLocked(BatchKindSummarize, req.lessonID, req.mediaFileID, false, func() bool { return entry.cancelRequested }) {
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
	s.activeSummaryLessonID = req.lessonID
	s.activeSummaryMediaID = req.mediaFileID
	s.mu.Unlock()

	sourceType, err := s.runLessonSummaryOnly(jobCtx, req.lessonID, req.opts, req.preferCachedTranscript, func(stage JobStage, msg string, chunkIdx, chunkTotal int, srcType string) {
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

	s.activeSummaryLessonID = ""
	s.activeSummaryMediaID = ""

	if e, exists := s.lessonJobs[req.lessonID]; exists {
		e.cancel = nil
		s.finalizeLessonJobLocked(e, sourceType, err)
	}
	s.cond.Broadcast()
}

func (s *Service) executeTranscribeBatchItem(batch *batchEntry, item batchLessonItem) {
	s.mu.Lock()
	if !s.waitForWorkerSlotLocked(BatchKindTranscribe, item.lessonID, item.mediaFileID, true, func() bool { return batch.cancelRequested }) {
		if batch.status.Running {
			finishBatchLocked(batch, StageCancelled)
		}
		s.mu.Unlock()
		return
	}
	mode := batch.mode
	s.mu.Unlock()

	if mode == "missing_only" && s.hasReadyTranscriptOrSubtitle(s.lifecycleCtx, item.mediaFileID) {
		s.mu.Lock()
		if batch.cancelRequested || s.lifecycleCtx.Err() != nil {
			if batch.status.Running {
				finishBatchLocked(batch, StageCancelled)
			}
			s.mu.Unlock()
			return
		}
		batch.status.Skipped++
		batch.status.SkippedAlreadyDone++
		if len(batch.pending) == 0 {
			finishBatchLocked(batch, StageCompleted)
		} else {
			clearBatchActiveFieldsLocked(batch, StageQueued)
		}
		s.mu.Unlock()
		return
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
			Action:    ActionTranscribe,
			Running:   true,
			Stage:     StageExtractingAudio,
			Message:   "正在提取视频音频...",
			StartedAt: now,
		},
		action:       ActionTranscribe,
		ownerIsBatch: true,
		cancel:       jobCancel,
	}
	s.lessonJobs[item.lessonID] = entry

	s.activeTranscribeLessonID = item.lessonID
	s.activeTranscribeMediaID = item.mediaFileID
	batch.activeLessonID = item.lessonID
	batch.activeLessonCancel = jobCancel
	batch.status.CurrentLessonID = item.lessonID
	batch.status.CurrentLessonTitle = item.lessonTitle
	batch.status.CurrentStage = StageExtractingAudio
	batch.status.CurrentMessage = "正在提取视频音频..."
	batch.status.ChunkIndex = 0
	batch.status.ChunkTotal = 0
	s.mu.Unlock()

	sourceType, err := s.runLessonTranscribe(jobCtx, item.lessonID, item.forceRetranscribe, func(stage JobStage, msg string, chunkIdx, chunkTotal int, srcType string) {
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
		batch.status.CurrentMessage = msg
		batch.status.ChunkIndex = chunkIdx
		batch.status.ChunkTotal = chunkTotal
		s.mu.Unlock()
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	s.activeTranscribeLessonID = ""
	s.activeTranscribeMediaID = ""

	if e, exists := s.lessonJobs[item.lessonID]; exists {
		e.cancel = nil
		s.finalizeLessonJobLocked(e, sourceType, err)
	}

	batch.activeLessonID = ""
	batch.activeLessonCancel = nil
	s.cond.Broadcast()

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

func (s *Service) executeSummaryBatchItem(batch *batchEntry, item batchLessonItem) {
	s.mu.Lock()
	if !s.waitForWorkerSlotLocked(BatchKindSummarize, item.lessonID, item.mediaFileID, true, func() bool { return batch.cancelRequested }) {
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
			batch.status.SkippedAlreadyDone++
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
			Action:    ActionSummarize,
			Running:   true,
			Stage:     StageReadingSubtitle,
			Message:   "正在读取字幕...",
			StartedAt: now,
		},
		action:       ActionSummarize,
		ownerIsBatch: true,
		cancel:       jobCancel,
	}
	s.lessonJobs[item.lessonID] = entry

	s.activeSummaryLessonID = item.lessonID
	s.activeSummaryMediaID = item.mediaFileID
	batch.activeLessonID = item.lessonID
	batch.activeLessonCancel = jobCancel
	batch.status.CurrentLessonID = item.lessonID
	batch.status.CurrentLessonTitle = item.lessonTitle
	batch.status.CurrentStage = StageReadingSubtitle
	batch.status.CurrentMessage = "正在读取字幕..."
	batch.status.ChunkIndex = 0
	batch.status.ChunkTotal = 0
	s.mu.Unlock()

	sourceType, err := s.runLessonSummaryOnly(jobCtx, item.lessonID, GenerateLessonOptions{}, false, func(stage JobStage, msg string, chunkIdx, chunkTotal int, srcType string) {
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
		batch.status.CurrentMessage = msg
		batch.status.ChunkIndex = chunkIdx
		batch.status.ChunkTotal = chunkTotal
		s.mu.Unlock()
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	s.activeSummaryLessonID = ""
	s.activeSummaryMediaID = ""

	if e, exists := s.lessonJobs[item.lessonID]; exists {
		e.cancel = nil
		if errors.Is(err, errNoTranscriptReady) {
			e.status.Running = false
			e.status.Stage = StageCancelled
			e.status.Message = "无可用字幕或转写文稿，已跳过"
			e.status.Error = ""
			e.status.ErrorCode = ""
			e.status.FinishedAt = nowUTC()
		} else {
			s.finalizeLessonJobLocked(e, sourceType, err)
		}
	}

	batch.activeLessonID = ""
	batch.activeLessonCancel = nil
	s.cond.Broadcast()

	if batch.cancelRequested || s.lifecycleCtx.Err() != nil {
		if batch.status.Running {
			finishBatchLocked(batch, StageCancelled)
		}
		return
	}

	if err == nil {
		batch.status.Completed++
	} else if errors.Is(err, errNoTranscriptReady) {
		batch.status.Skipped++
		batch.status.SkippedNoTranscript++
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
		if entry.action == ActionTranscribe {
			entry.status.Message = "语音转文字完成"
		} else {
			entry.status.Message = "AI 总结生成完成"
		}
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
	if entry.action == ActionTranscribe {
		entry.status.Message = "转写失败"
	} else {
		entry.status.Message = "生成失败"
	}
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
	case errors.Is(err, ErrLessonJobConflict):
		return "lesson_job_conflict", err.Error()
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

func (s *Service) hasReadyTranscriptOrSubtitle(ctx context.Context, mediaFileID string) bool {
	if mediaFileID == "" {
		return false
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_files WHERE media_file_id = ? AND status = 'active'`, mediaFileID).Scan(&count); err == nil && count > 0 {
		return true
	}
	var streamsJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(streams_json, '[]') FROM media_files WHERE id = ?`, mediaFileID).Scan(&streamsJSON); err == nil && hasEmbeddedTextSubtitleJSON(streamsJSON) {
		return true
	}
	if _, found, err := s.loadCachedTranscript(ctx, mediaFileID); err == nil && found {
		return true
	}
	return false
}

func (s *Service) runLessonTranscribe(
	ctx context.Context,
	lessonID string,
	forceRetranscribe bool,
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

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return "", err
	}
	if !cfg.ASRConfigured {
		return "", &pipelineError{code: "asr_not_configured", err: ErrASRNotConfigured}
	}

	if !forceRetranscribe {
		caps, capErr := s.computeLessonCapabilities(ctx, lesson, cfg)
		if capErr == nil {
			if caps.HasSubtitle {
				return "subtitle", nil
			}
			if caps.HasCachedTranscript {
				return caps.TranscriptSource, nil
			}
		}
	}

	_, _, asrErr := s.runASRPipeline(ctx, lesson.MediaFileID, forceRetranscribe, cfg, report)
	if asrErr != nil {
		return "asr", asrErr
	}
	return "asr", nil
}

func (s *Service) runLessonSummaryOnly(
	ctx context.Context,
	lessonID string,
	opts GenerateLessonOptions,
	preferCached bool,
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

	src, err := s.resolveLessonTranscriptOnly(ctx, lesson.MediaFileID, opts, preferCached)
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

func (s *Service) resolveLessonTranscriptOnly(
	ctx context.Context,
	mediaFileID string,
	opts GenerateLessonOptions,
	preferCached bool,
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

	if preferCached {
		if res, ok, cacheErr := s.resolveCachedTranscript(ctx, mediaFileID); cacheErr != nil {
			return resolvedTranscript{}, cacheErr
		} else if ok {
			return res, nil
		}
	}

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

	if !preferCached {
		if res, ok, cacheErr := s.resolveCachedTranscript(ctx, mediaFileID); cacheErr != nil {
			return resolvedTranscript{}, cacheErr
		} else if ok {
			return res, nil
		}
	}

	return resolvedTranscript{}, errNoTranscriptReady
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

func (s *Service) runASRPipeline(ctx context.Context, mediaFileID string, forceRetranscribe bool, cfg AIConfigSnapshot, report progressFunc) (vttText string, language string, err error) {
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

	if forceRetranscribe {
		if delErr := s.deleteChunkCheckpoints(ctx, mediaFileID); delErr != nil {
			return "", "", delErr
		}
	}

	report(StageExtractingAudio, "正在提取视频音频...", 0, 0, "asr")

	segSec := s.effectiveAudioSegmentSec(cfg)
	extractor := s.audioExtractor
	if !s.hasCustomExtractor && s.media != nil {
		extractor = NewFFmpegAudioExtractor(s.dataDir, s.media.FFmpegBin(), s.media.FFprobeBin(), segSec)
	}

	jobID := id.New()
	chunks, cleanup, err := extractor.ExtractChunks(ctx, info.AbsPath, jobID)
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

	fingerprint := s.computeMediaFingerprint(ctx, mediaFileID, info.AbsPath)
	var savedByIndex map[int]chunkCheckpoint
	if !forceRetranscribe {
		existingCPs, loadErr := s.loadChunkCheckpoints(ctx, mediaFileID)
		if loadErr != nil {
			_ = s.deleteChunkCheckpoints(ctx, mediaFileID)
		} else if validMap, ok := validateChunkCheckpoints(existingCPs, len(chunks), segSec, fingerprint); ok {
			savedByIndex = validMap
		} else if len(existingCPs) > 0 {
			_ = s.deleteChunkCheckpoints(ctx, mediaFileID)
		}
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

		if saved, ok := savedByIndex[i]; ok {
			report(StageTranscribing, fmt.Sprintf("复用已转写分片（%d/%d）...", i+1, len(chunks)), i+1, len(chunks), "asr")
			if detectedLang == "" && strings.TrimSpace(saved.language) != "" {
				detectedLang = strings.TrimSpace(saved.language)
			}
			allCues = append(allCues, saved.cues...)
			continue
		}

		report(StageTranscribing, fmt.Sprintf("正在语音转写（分片 %d/%d）...", i+1, len(chunks)), i+1, len(chunks), "asr")

		res, txErr := transcribeChunkFile(ctx, transcriber, chunk.Path)
		if txErr != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return "", "", &pipelineError{code: "asr_failed", err: txErr}
		}

		chunkLang := strings.TrimSpace(res.Language)
		if detectedLang == "" && chunkLang != "" {
			detectedLang = chunkLang
		}

		var chunkCues []ai.TranscriptCue
		if len(res.Cues) > 0 {
			chunkCues = make([]ai.TranscriptCue, 0, len(res.Cues))
			for _, cue := range res.Cues {
				chunkCues = append(chunkCues, ai.TranscriptCue{
					StartSec: chunk.OffsetSec + cue.StartSec,
					EndSec:   chunk.OffsetSec + cue.EndSec,
					Text:     cue.Text,
				})
			}
		} else if trimmed := strings.TrimSpace(res.Text); trimmed != "" {
			dur := chunk.DurationSec
			if dur <= 0 {
				dur = float64(segSec)
			}
			chunkCues = []ai.TranscriptCue{{
				StartSec: chunk.OffsetSec,
				EndSec:   chunk.OffsetSec + dur,
				Text:     trimmed,
			}}
		}

		if persistErr := s.persistChunkCheckpoint(ctx, chunkCheckpoint{
			mediaFileID:      mediaFileID,
			chunkIndex:       i,
			chunkTotal:       len(chunks),
			segmentSec:       segSec,
			mediaFingerprint: fingerprint,
			offsetSec:        chunk.OffsetSec,
			durationSec:      chunk.DurationSec,
			language:         chunkLang,
			model:            cfg.ASR.Model,
			cues:             chunkCues,
		}); persistErr != nil {
			return "", "", persistErr
		}

		allCues = append(allCues, chunkCues...)
	}

	if len(allCues) == 0 {
		_ = s.deleteChunkCheckpoints(context.WithoutCancel(ctx), mediaFileID)
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
	if err := s.finalizeASRTranscriptTx(ctx, mediaFileID, detectedLang, cfg.ASR.Model, vtt, plain); err != nil {
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
		} else if !caps.HasSubtitle {
			done, total, pErr := s.loadPartialCheckpointProgress(ctx, lesson.MediaFileID, s.effectiveAudioSegmentSec(cfg))
			if pErr != nil {
				return LessonCapabilities{}, pErr
			}
			caps.PartialTranscriptChunks = done
			caps.PartialTranscriptTotal = total
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
