package cover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"coursefin/internal/ai"
	"coursefin/internal/catalog"
	"coursefin/internal/id"
)

const defaultCoverHTTP = 240 * time.Second

var (
	ErrNotFound            = errors.New("not found")
	ErrImageNotConfigured  = errors.New("ai image provider is not configured")
	ErrInvalidInput        = errors.New("invalid input")
	ErrBatchAlreadyRunning = errors.New("batch cover generation is already running")
	ErrUpstream            = errors.New("upstream image generation failed")
)

// ConfigLoader loads effective AI image settings for cover generation.
type ConfigLoader func(ctx context.Context) (EffectiveImageSettings, error)

// GeneratorFactory builds an ai.ImageGenerator from effective image configuration.
type GeneratorFactory func(cfg ai.ImageConfig) ai.ImageGenerator

// Options configures a cover.Service instance.
type Options struct {
	DB               *sql.DB
	DataDir          string
	Catalog          *catalog.Service
	LifecycleCtx     context.Context
	HTTPClient       *http.Client
	ConfigLoader     ConfigLoader
	GeneratorFactory GeneratorFactory
}

// GenerateCoverInput specifies optional overrides when generating a single-course AI cover.
type GenerateCoverInput struct {
	StyleID      string  `json:"style_id"`
	Title        string  `json:"title"`
	Subtitle     *string `json:"subtitle"`
	Badge        *string `json:"badge"`
	IncludeBadge *bool   `json:"include_badge"`
	ExtraPrompt  string  `json:"extra_prompt"`
}

// GeneratedCourseCover returns the newly created active cover and refreshed course detail.
type GeneratedCourseCover struct {
	Cover  *catalog.CourseCover  `json:"cover"`
	Course *catalog.CourseDetail `json:"course"`
}

// BatchCoverInput configures a batch cover generation run across a library or all courses.
type BatchCoverInput struct {
	LibraryID    string `json:"library_id,omitempty"`
	Mode         string `json:"mode,omitempty"` // "missing_only" | "all"
	StyleID      string `json:"style_id,omitempty"`
	IncludeBadge *bool  `json:"include_badge,omitempty"`
	ExtraPrompt  string `json:"extra_prompt,omitempty"`
}

// BatchCourseError captures an individual course failure during batch cover generation.
type BatchCourseError struct {
	CourseID    string `json:"course_id"`
	CourseTitle string `json:"course_title"`
	Error       string `json:"error"`
}

// BatchCoverStatus represents the live or most recent batch cover generation job.
type BatchCoverStatus struct {
	Running            bool               `json:"running"`
	Mode               string             `json:"mode"`
	LibraryID          string             `json:"library_id,omitempty"`
	StyleID            string             `json:"style_id"`
	State              string             `json:"state"` // "idle" | "running" | "completed" | "cancelled"
	Total              int                `json:"total"`
	Completed          int                `json:"completed"`
	Skipped            int                `json:"skipped"`
	Failed             int                `json:"failed"`
	CurrentCourseID    string             `json:"current_course_id"`
	CurrentCourseTitle string             `json:"current_course_title"`
	Errors             []BatchCourseError `json:"errors"`
	StartedAt          string             `json:"started_at,omitempty"`
	FinishedAt         string             `json:"finished_at,omitempty"`
}

type batchCourseTarget struct {
	id    string
	title string
}

// Service manages style templates, single-course AI cover generation (append & activate),
// and serialized background batch cover generation.
type Service struct {
	db               *sql.DB
	dataDir          string
	catalog          *catalog.Service
	lifecycleCtx     context.Context
	httpClient       *http.Client
	defaultClient    *http.Client
	configLoader     ConfigLoader
	generatorFactory GeneratorFactory

	mu              sync.Mutex
	wg              sync.WaitGroup
	batch           BatchCoverStatus
	batchJobID      string
	batchCancel     context.CancelFunc
	cancelRequested bool
}

// NewService constructs a cover Service.
func NewService(opts Options) *Service {
	lctx := opts.LifecycleCtx
	if lctx == nil {
		lctx = context.Background()
	}
	return &Service{
		db:               opts.DB,
		dataDir:          opts.DataDir,
		catalog:          opts.Catalog,
		lifecycleCtx:     lctx,
		httpClient:       opts.HTTPClient,
		defaultClient:    &http.Client{Timeout: defaultCoverHTTP},
		configLoader:     opts.ConfigLoader,
		generatorFactory: opts.GeneratorFactory,
		batch: BatchCoverStatus{
			State:  "idle",
			Errors: []BatchCourseError{},
		},
	}
}

// Wait blocks until any active batch cover generation goroutine has exited.
func (s *Service) Wait() {
	s.wg.Wait()
}

func (s *Service) imageClient() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return s.defaultClient
}

func (s *Service) newGenerator(cfg ai.ImageConfig) ai.ImageGenerator {
	if s.generatorFactory != nil {
		return s.generatorFactory(cfg)
	}
	return ai.NewHTTPImageGenerator(cfg, s.imageClient())
}

// LoadSettings loads effective AI image settings and default style ID from DB or custom ConfigLoader.
func (s *Service) LoadSettings(ctx context.Context) (EffectiveImageSettings, error) {
	if s.configLoader != nil {
		return s.configLoader(ctx)
	}
	if s.db == nil {
		return EffectiveImageSettings{DefaultStyleID: DefaultStyleID}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return EffectiveImageSettings{}, fmt.Errorf("load app_settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return EffectiveImageSettings{}, fmt.Errorf("scan app_settings: %w", err)
		}
		settings[k] = v
	}
	if err := rows.Err(); err != nil {
		return EffectiveImageSettings{}, err
	}
	return ResolveEffectiveImageSettings(settings), nil
}

// GenerateCourseCover generates an AI cover, persists it as a new associated cover, and activates it.
func (s *Service) GenerateCourseCover(ctx context.Context, courseID string, input GenerateCoverInput) (*GeneratedCourseCover, error) {
	courseID = normalizeOrRaw(courseID)
	if courseID == "" {
		return nil, ErrInvalidInput
	}

	course, err := s.catalog.GetCourseDetail(ctx, courseID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	cfg, err := s.LoadSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Configured {
		return nil, ErrImageNotConfigured
	}

	styleID := strings.TrimSpace(input.StyleID)
	if styleID == "" {
		styleID = cfg.DefaultStyleID
	}
	style, ok := GetStyle(styleID)
	if !ok {
		return nil, fmt.Errorf("%w: unknown cover style %q", ErrInvalidInput, styleID)
	}

	prompt := buildCoursePrompt(course, style, input)
	gen := s.newGenerator(cfg.Image)
	img, err := gen.GenerateImage(ctx, ai.ImageGenerateRequest{
		Prompt: prompt,
		Size:   cfg.Image.Size,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}

	created, updated, err := s.catalog.AddCourseCover(ctx, course.ID, catalog.AddCourseCoverInput{
		Ext:       img.Ext,
		Data:      img.Data,
		Source:    catalog.CoverSourceAI,
		StyleID:   style.ID,
		StyleName: style.Name,
		Prompt:    prompt,
		Width:     img.Width,
		Height:    img.Height,
	})
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, ErrNotFound
		}
		if errors.Is(err, catalog.ErrInvalidInput) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return nil, err
	}

	return &GeneratedCourseCover{
		Cover:  created,
		Course: updated,
	}, nil
}

// GetBatchStatus returns a snapshot of the current or most recent batch cover generation job.
func (s *Service) GetBatchStatus() BatchCoverStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneBatchStatus(s.batch)
}

// StartBatch validates configuration, resolves matching courses, and runs serial background cover generation.
func (s *Service) StartBatch(ctx context.Context, input BatchCoverInput) (BatchCoverStatus, error) {
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = "missing_only"
	}
	if mode != "missing_only" && mode != "all" {
		return BatchCoverStatus{}, fmt.Errorf("%w: unsupported batch mode %q", ErrInvalidInput, input.Mode)
	}

	libID := strings.TrimSpace(input.LibraryID)
	if libID != "" {
		libID = normalizeOrRaw(libID)
	}

	cfg, err := s.LoadSettings(ctx)
	if err != nil {
		return BatchCoverStatus{}, err
	}
	if !cfg.Configured {
		return BatchCoverStatus{}, ErrImageNotConfigured
	}

	styleID := strings.TrimSpace(input.StyleID)
	if styleID == "" {
		styleID = cfg.DefaultStyleID
	}
	style, ok := GetStyle(styleID)
	if !ok {
		return BatchCoverStatus{}, fmt.Errorf("%w: unknown cover style %q", ErrInvalidInput, styleID)
	}

	s.mu.Lock()
	if s.batch.Running {
		cp := cloneBatchStatus(s.batch)
		s.mu.Unlock()
		return cp, ErrBatchAlreadyRunning
	}
	s.mu.Unlock()

	courses, err := s.catalog.ListCourses(ctx, catalog.CourseFilter{LibraryID: libID})
	if err != nil {
		return BatchCoverStatus{}, err
	}

	var pending []batchCourseTarget
	skipped := 0
	for _, c := range courses {
		if mode == "missing_only" && strings.TrimSpace(c.CoverPath) != "" {
			skipped++
			continue
		}
		pending = append(pending, batchCourseTarget{
			id:    c.ID,
			title: c.Title,
		})
	}

	nowStr := nowUTC()
	status := BatchCoverStatus{
		Running:   len(pending) > 0,
		Mode:      mode,
		LibraryID: libID,
		StyleID:   style.ID,
		Total:     len(courses),
		Skipped:   skipped,
		Errors:    []BatchCourseError{},
		StartedAt: nowStr,
	}
	if len(pending) == 0 {
		status.State = "completed"
		status.FinishedAt = nowStr
	} else {
		status.State = "running"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.batch.Running {
		return cloneBatchStatus(s.batch), ErrBatchAlreadyRunning
	}

	if len(pending) == 0 {
		s.batch = status
		s.batchJobID = ""
		s.batchCancel = nil
		s.cancelRequested = false
		return cloneBatchStatus(s.batch), nil
	}

	if err := s.lifecycleCtx.Err(); err != nil {
		return BatchCoverStatus{}, err
	}

	jobID := id.New()
	batchCtx, cancel := context.WithCancel(s.lifecycleCtx)
	s.batch = status
	s.batchJobID = jobID
	s.batchCancel = cancel
	s.cancelRequested = false

	s.wg.Add(1)
	go s.runBatch(batchCtx, jobID, pending, style, input.IncludeBadge, input.ExtraPrompt, cfg)

	return cloneBatchStatus(s.batch), nil
}

// CancelBatch cancels an active batch cover generation job and returns its updated status snapshot.
func (s *Service) CancelBatch() BatchCoverStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.batch.Running {
		return cloneBatchStatus(s.batch)
	}

	s.cancelRequested = true
	s.batch.Running = false
	s.batch.State = "cancelled"
	s.batch.CurrentCourseID = ""
	s.batch.CurrentCourseTitle = ""
	s.batch.FinishedAt = nowUTC()
	if s.batchCancel != nil {
		s.batchCancel()
	}
	return cloneBatchStatus(s.batch)
}

func (s *Service) runBatch(
	ctx context.Context,
	jobID string,
	pending []batchCourseTarget,
	style StyleTemplate,
	includeBadge *bool,
	extraPrompt string,
	cfg EffectiveImageSettings,
) {
	defer s.wg.Done()

	gen := s.newGenerator(cfg.Image)

	for _, target := range pending {
		if ctx.Err() != nil || s.isJobCancelled(jobID) {
			s.finishBatchCancelled(jobID)
			return
		}

		s.mu.Lock()
		if s.batchJobID != jobID || s.cancelRequested {
			s.mu.Unlock()
			return
		}
		s.batch.CurrentCourseID = target.id
		s.batch.CurrentCourseTitle = target.title
		s.mu.Unlock()

		course, err := s.catalog.GetCourseDetail(ctx, target.id)
		if err != nil {
			if ctx.Err() != nil || s.isJobCancelled(jobID) {
				s.finishBatchCancelled(jobID)
				return
			}
			s.recordBatchFailure(jobID, target, err)
			continue
		}

		prompt := buildCoursePrompt(course, style, GenerateCoverInput{
			StyleID:      style.ID,
			Title:        course.Title,
			IncludeBadge: includeBadge,
			ExtraPrompt:  extraPrompt,
		})

		img, err := gen.GenerateImage(ctx, ai.ImageGenerateRequest{
			Prompt: prompt,
			Size:   cfg.Image.Size,
		})
		if err != nil {
			if ctx.Err() != nil || s.isJobCancelled(jobID) {
				s.finishBatchCancelled(jobID)
				return
			}
			s.recordBatchFailure(jobID, target, err)
			continue
		}

		if ctx.Err() != nil || s.isJobCancelled(jobID) {
			s.finishBatchCancelled(jobID)
			return
		}

		if _, _, err := s.catalog.AddCourseCover(ctx, course.ID, catalog.AddCourseCoverInput{
			Ext:       img.Ext,
			Data:      img.Data,
			Source:    catalog.CoverSourceAIBatch,
			StyleID:   style.ID,
			StyleName: style.Name,
			Prompt:    prompt,
			Width:     img.Width,
			Height:    img.Height,
		}); err != nil {
			if ctx.Err() != nil || s.isJobCancelled(jobID) {
				s.finishBatchCancelled(jobID)
				return
			}
			s.recordBatchFailure(jobID, target, err)
			continue
		}

		s.mu.Lock()
		if s.batchJobID == jobID && !s.cancelRequested {
			s.batch.Completed++
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batchJobID == jobID && !s.cancelRequested {
		s.batch.Running = false
		if ctx.Err() != nil {
			s.batch.State = "cancelled"
		} else {
			s.batch.State = "completed"
		}
		s.batch.CurrentCourseID = ""
		s.batch.CurrentCourseTitle = ""
		s.batch.FinishedAt = nowUTC()
	}
}

func (s *Service) isJobCancelled(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.batchJobID != jobID || s.cancelRequested
}

func (s *Service) finishBatchCancelled(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batchJobID != jobID {
		return
	}
	s.batch.Running = false
	s.batch.State = "cancelled"
	s.batch.CurrentCourseID = ""
	s.batch.CurrentCourseTitle = ""
	if s.batch.FinishedAt == "" {
		s.batch.FinishedAt = nowUTC()
	}
}

func (s *Service) recordBatchFailure(jobID string, target batchCourseTarget, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batchJobID != jobID || s.cancelRequested {
		return
	}
	s.batch.Failed++
	s.batch.Errors = append(s.batch.Errors, BatchCourseError{
		CourseID:    target.id,
		CourseTitle: target.title,
		Error:       err.Error(),
	})
}

func buildCoursePrompt(course *catalog.CourseDetail, style StyleTemplate, input GenerateCoverInput) string {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = strings.TrimSpace(course.Title)
	}

	subtitle := ""
	if input.Subtitle != nil {
		subtitle = strings.TrimSpace(*input.Subtitle)
	} else if inst := strings.TrimSpace(course.Instructor); inst != "" {
		subtitle = "讲师 · " + inst
	}

	wantBadge := true
	if input.IncludeBadge != nil {
		wantBadge = *input.IncludeBadge
	}
	badge := ""
	if wantBadge {
		if input.Badge != nil {
			badge = strings.TrimSpace(*input.Badge)
		} else if course.LessonCount > 0 || course.ChapterCount > 0 {
			badge = fmt.Sprintf("%d 节 · %d 章", course.LessonCount, course.ChapterCount)
		}
	}

	var chapters []string
	for _, ch := range course.Chapters {
		if t := strings.TrimSpace(ch.Title); t != "" {
			chapters = append(chapters, t)
		}
	}

	return BuildPrompt(style, PromptInput{
		Title:         title,
		Subtitle:      subtitle,
		Badge:         badge,
		Overview:      course.Overview,
		Tags:          course.Tags,
		ChapterTitles: chapters,
		ExtraPrompt:   input.ExtraPrompt,
	})
}

func cloneBatchStatus(st BatchCoverStatus) BatchCoverStatus {
	cp := st
	if st.Errors != nil {
		cp.Errors = append([]BatchCourseError(nil), st.Errors...)
	} else {
		cp.Errors = []BatchCourseError{}
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
