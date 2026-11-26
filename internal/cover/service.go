package cover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"coursefin/internal/ai"
	"coursefin/internal/catalog"
	"coursefin/internal/id"
)

const (
	previewTTL        = 30 * time.Minute
	maxCachedPreviews = 50
	defaultCoverHTTP  = 120 * time.Second
)

var (
	ErrNotFound            = errors.New("not found")
	ErrImageNotConfigured  = errors.New("ai image provider is not configured")
	ErrInvalidInput        = errors.New("invalid input")
	ErrBatchAlreadyRunning = errors.New("batch cover generation is already running")
	ErrPreviewNotFound     = errors.New("cover preview not found or expired")
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

// GeneratePreviewInput specifies optional overrides when generating a single-course cover preview.
type GeneratePreviewInput struct {
	StyleID      string  `json:"style_id"`
	Title        string  `json:"title"`
	Subtitle     *string `json:"subtitle"`
	Badge        *string `json:"badge"`
	IncludeBadge *bool   `json:"include_badge"`
	ExtraPrompt  string  `json:"extra_prompt"`
}

// CoverPreview describes a temporary generated cover image ready for review before applying.
type CoverPreview struct {
	PreviewID  string `json:"preview_id"`
	CourseID   string `json:"course_id"`
	StyleID    string `json:"style_id"`
	StyleName  string `json:"style_name"`
	PreviewURL string `json:"preview_url"`
	Prompt     string `json:"prompt"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	CreatedAt  string `json:"created_at"`
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

type previewEntry struct {
	previewID   string
	courseID    string
	filePath    string
	contentType string
	ext         string
	createdAt   time.Time
}

type batchCourseTarget struct {
	id    string
	title string
}

// Service manages style templates, single-course AI cover previews/application,
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
	previews        map[string]previewEntry
	batch           BatchCoverStatus
	batchJobID      string
	batchCancel     context.CancelFunc
	cancelRequested bool
}

// NewService constructs a cover Service and clears stale preview files from previous runs.
func NewService(opts Options) *Service {
	lctx := opts.LifecycleCtx
	if lctx == nil {
		lctx = context.Background()
	}
	s := &Service{
		db:               opts.DB,
		dataDir:          opts.DataDir,
		catalog:          opts.Catalog,
		lifecycleCtx:     lctx,
		httpClient:       opts.HTTPClient,
		defaultClient:    &http.Client{Timeout: defaultCoverHTTP},
		configLoader:     opts.ConfigLoader,
		generatorFactory: opts.GeneratorFactory,
		previews:         make(map[string]previewEntry),
		batch: BatchCoverStatus{
			State:  "idle",
			Errors: []BatchCourseError{},
		},
	}
	s.cleanPreviewCacheDir()
	return s
}

// Wait blocks until any active batch cover generation goroutine has exited.
func (s *Service) Wait() {
	s.wg.Wait()
}

func (s *Service) cleanPreviewCacheDir() {
	if s.dataDir == "" {
		return
	}
	dir := filepath.Join(s.dataDir, "cache", "cover_previews")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
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

// GenerateCoursePreview generates a temporary AI cover preview without overwriting the course's current cover.
func (s *Service) GenerateCoursePreview(ctx context.Context, courseID string, input GeneratePreviewInput) (*CoverPreview, error) {
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

	previewDir := filepath.Join(s.dataDir, "cache", "cover_previews")
	if err := os.MkdirAll(previewDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cover preview dir: %w", err)
	}

	previewID := id.New()
	filePath := filepath.Join(previewDir, previewID+img.Ext)
	if err := writeFileAtomically(previewDir, filePath, img.Data); err != nil {
		return nil, fmt.Errorf("save cover preview: %w", err)
	}

	now := time.Now().UTC()
	s.mu.Lock()
	s.previews[previewID] = previewEntry{
		previewID:   previewID,
		courseID:    course.ID,
		filePath:    filePath,
		contentType: img.ContentType,
		ext:         img.Ext,
		createdAt:   now,
	}
	s.prunePreviewsLocked(now)
	s.mu.Unlock()

	return &CoverPreview{
		PreviewID:  previewID,
		CourseID:   course.ID,
		StyleID:    style.ID,
		StyleName:  style.Name,
		PreviewURL: "/api/v1/cover/previews/" + previewID,
		Prompt:     prompt,
		Width:      img.Width,
		Height:     img.Height,
		CreatedAt:  now.Format(time.RFC3339Nano),
	}, nil
}

// GetPreviewFile returns the validated on-disk path and MIME type of an active preview.
func (s *Service) GetPreviewFile(previewID string) (string, string, error) {
	normID, ok := id.Normalize(strings.TrimSpace(previewID))
	if !ok {
		return "", "", ErrPreviewNotFound
	}

	now := time.Now().UTC()
	s.mu.Lock()
	s.prunePreviewsLocked(now)
	entry, exists := s.previews[normID]
	s.mu.Unlock()

	if !exists {
		return "", "", ErrPreviewNotFound
	}
	info, err := os.Stat(entry.filePath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return "", "", ErrPreviewNotFound
	}
	return entry.filePath, entry.contentType, nil
}

// ApplyCoursePreview promotes a generated preview to the course's persisted cover image.
func (s *Service) ApplyCoursePreview(ctx context.Context, courseID, previewID string) (*catalog.CourseDetail, error) {
	courseID = normalizeOrRaw(courseID)
	normPreviewID, ok := id.Normalize(strings.TrimSpace(previewID))
	if courseID == "" || !ok {
		return nil, ErrPreviewNotFound
	}

	now := time.Now().UTC()
	s.mu.Lock()
	s.prunePreviewsLocked(now)
	entry, exists := s.previews[normPreviewID]
	s.mu.Unlock()

	if !exists {
		return nil, ErrPreviewNotFound
	}
	if entry.courseID != courseID {
		return nil, fmt.Errorf("%w: preview does not belong to course", ErrInvalidInput)
	}

	raw, err := os.ReadFile(entry.filePath)
	if err != nil {
		return nil, ErrPreviewNotFound
	}

	updated, err := s.saveCourseCoverBytes(ctx, courseID, entry.ext, raw)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if cur, ok := s.previews[normPreviewID]; ok {
		delete(s.previews, normPreviewID)
		_ = os.Remove(cur.filePath)
	}
	s.mu.Unlock()

	return updated, nil
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

		prompt := buildCoursePrompt(course, style, GeneratePreviewInput{
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

		if _, err := s.saveCourseCoverBytes(ctx, course.ID, img.Ext, img.Data); err != nil {
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

func (s *Service) saveCourseCoverBytes(ctx context.Context, courseID, ext string, raw []byte) (*catalog.CourseDetail, error) {
	course, err := s.catalog.GetCourseDetail(ctx, courseID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	coversDir := filepath.Join(s.dataDir, "covers")
	if err := os.MkdirAll(coversDir, 0o755); err != nil {
		return nil, fmt.Errorf("create covers dir: %w", err)
	}

	destName := course.ID + "_cover" + ext
	destPath := filepath.Join(coversDir, destName)
	if err := writeFileAtomically(coversDir, destPath, raw); err != nil {
		return nil, fmt.Errorf("write course cover: %w", err)
	}

	updated, err := s.catalog.UpdateCourse(ctx, course.ID, catalog.UpdateCourseInput{
		CoverPath: &destName,
	})
	if err != nil {
		if course.CoverPath == "" || filepath.Base(course.CoverPath) != destName {
			_ = os.Remove(destPath)
		}
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if course.CoverPath != "" && filepath.Base(course.CoverPath) != destName {
		_ = os.Remove(filepath.Join(coversDir, filepath.Base(course.CoverPath)))
	}
	return updated, nil
}

func (s *Service) prunePreviewsLocked(now time.Time) {
	for pid, entry := range s.previews {
		if now.Sub(entry.createdAt) > previewTTL {
			delete(s.previews, pid)
			_ = os.Remove(entry.filePath)
		}
	}
	if len(s.previews) <= maxCachedPreviews {
		return
	}
	all := make([]previewEntry, 0, len(s.previews))
	for _, entry := range s.previews {
		all = append(all, entry)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].createdAt.Before(all[j].createdAt)
	})
	excess := len(all) - maxCachedPreviews
	for i := 0; i < excess; i++ {
		delete(s.previews, all[i].previewID)
		_ = os.Remove(all[i].filePath)
	}
}

func buildCoursePrompt(course *catalog.CourseDetail, style StyleTemplate, input GeneratePreviewInput) string {
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

func writeFileAtomically(dir, destPath string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".cover-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
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
