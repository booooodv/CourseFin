package cover_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"coursefin/internal/ai"
	"coursefin/internal/catalog"
	"coursefin/internal/cover"
	"coursefin/internal/db"
)

func makePNG(t *testing.T, r uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: r, G: 100, B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

type fakeImageGen struct {
	mu      sync.Mutex
	prompts []string
	fn      func(ctx context.Context, req ai.ImageGenerateRequest) (*ai.GeneratedImage, error)
}

func (f *fakeImageGen) GenerateImage(ctx context.Context, req ai.ImageGenerateRequest) (*ai.GeneratedImage, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, req.Prompt)
	f.mu.Unlock()
	return f.fn(ctx, req)
}

func (f *fakeImageGen) TestConnection(ctx context.Context) error {
	return nil
}

func setupCoverTestEnv(t *testing.T) (string, *catalog.Service, *catalog.Library, *catalog.Library) {
	t.Helper()
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	cat := catalog.NewService(conn, dataDir)
	libs, err := cat.ListLibraries(context.Background())
	if err != nil || len(libs) == 0 {
		t.Fatalf("ListLibraries: %v", err)
	}
	lib2, err := cat.CreateLibrary(context.Background(), "设计艺术")
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	return dataDir, cat, &libs[0], lib2
}

func TestResolveEffectiveImageSettings_AntiLeakAndDefaults(t *testing.T) {
	t.Run("inherits primary openai config only when base_url is blank", func(t *testing.T) {
		eff := cover.ResolveEffectiveImageSettings(map[string]string{
			"ai_base_url":            "https://api.openai.com/v1",
			"ai_api_key":             "sk-main",
			"ai_image_model":         "gpt-image-1",
			"ai_cover_default_style": "dark_tech",
		})
		if !eff.Configured {
			t.Fatal("expected configured=true")
		}
		if eff.Image.Protocol != ai.ImageProtocolOpenAIImages {
			t.Fatalf("protocol=%q, want openai_images", eff.Image.Protocol)
		}
		if eff.Image.BaseURL != "https://api.openai.com/v1" || eff.Image.APIKey != "sk-main" {
			t.Fatalf("unexpected inherited URL/key: %+v", eff.Image)
		}
		if eff.DefaultStyleID != "dark_tech" {
			t.Fatalf("defaultStyle=%q, want dark_tech", eff.DefaultStyleID)
		}
	})

	t.Run("does NOT leak ai_api_key to custom ai_image_base_url", func(t *testing.T) {
		eff := cover.ResolveEffectiveImageSettings(map[string]string{
			"ai_base_url":       "https://api.openai.com/v1",
			"ai_api_key":        "sk-main-secret",
			"ai_image_protocol": "openai_chat",
			"ai_image_base_url": "https://openrouter.ai/api/v1",
			"ai_image_model":    "google/gemini-2.5-flash-image",
		})
		if eff.Image.APIKey != "" {
			t.Fatalf("expected empty API key for custom base URL, got %q", eff.Image.APIKey)
		}
		if !eff.Configured {
			t.Fatal("expected configured=true")
		}
	})

	t.Run("does NOT leak ai_api_key to gemini protocol", func(t *testing.T) {
		eff := cover.ResolveEffectiveImageSettings(map[string]string{
			"ai_base_url":       "https://api.openai.com/v1",
			"ai_api_key":        "sk-main-secret",
			"ai_image_protocol": "gemini",
			"ai_image_model":    "gemini-2.5-flash-image",
		})
		if eff.Image.APIKey != "" {
			t.Fatalf("expected empty API key for gemini without ai_image_api_key, got %q", eff.Image.APIKey)
		}
		if eff.Configured {
			t.Fatal("expected configured=false when official gemini has no ai_image_api_key")
		}
		if eff.Image.BaseURL != ai.DefaultGeminiBaseURL {
			t.Fatalf("expected default gemini URL, got %q", eff.Image.BaseURL)
		}
	})

	t.Run("unknown default style falls back to editorial_modern", func(t *testing.T) {
		eff := cover.ResolveEffectiveImageSettings(map[string]string{
			"ai_cover_default_style": "nonexistent",
		})
		if eff.DefaultStyleID != cover.DefaultStyleID {
			t.Fatalf("got %q, want %q", eff.DefaultStyleID, cover.DefaultStyleID)
		}
	})
}

func TestStylesAndBuildPrompt(t *testing.T) {
	styles := cover.ListStyles()
	if len(styles) != 6 {
		t.Fatalf("expected 6 styles, got %d", len(styles))
	}
	st, ok := cover.GetStyle("blueprint_grid")
	if !ok || st.PreviewImageURL != "/web/cover-styles/blueprint_grid.jpg" {
		t.Fatalf("unexpected blueprint_grid style: ok=%v %+v", ok, st)
	}
	prompt := cover.BuildPrompt(st, cover.PromptInput{
		Title:         "操作系统内核剖析",
		Subtitle:      "讲师 · 林工",
		Badge:         "18 节 · 4 章",
		Overview:      "深入进程调度与内存管理",
		Tags:          []string{"OS", "C"},
		ChapterTitles: []string{"引导与中断", "虚拟内存"},
		ExtraPrompt:   "加入芯片电路线稿",
	})
	for _, want := range []string{"操作系统内核剖析", "讲师 · 林工", "18 节 · 4 章", "加入芯片电路线稿", "Minimalist Blueprint"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPrompt_NoEditOrReferenceTriggers(t *testing.T) {
	forbiddenPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bpreserve\b`),
		regexp.MustCompile(`(?i)\bomit\b`),
		regexp.MustCompile(`(?i)\breference\b`),
		regexp.MustCompile(`(?i)\bedit\b`),
		regexp.MustCompile(`(?i)\bmodify\b`),
		regexp.MustCompile(`(?i)\breplace\b`),
		regexp.MustCompile(`(?i)\bremove\b`),
		regexp.MustCompile(`(?i)\boriginal\b`),
		regexp.MustCompile(`(?i)on the image`),
		regexp.MustCompile(`(?i)from scratch`),
		regexp.MustCompile(`(?i)inspired by`),
	}
	bannedSpecMarkers := []string{
		"Style:",
		"Subject:",
		"Exact Text",
		"Right-Side Visual Theme",
		"Visual Quality:",
	}

	cases := []struct {
		name  string
		input cover.PromptInput
		empty bool
	}{
		{
			name: "full metadata",
			input: cover.PromptInput{
				Title:         "ICT 2022 交易带教课（中文系列）",
				Subtitle:      "讲师 · ICT",
				Badge:         "24 节 · 4 章",
				Overview:      "包含参考图片 screenshot reference 不应进入提示词",
				Tags:          []string{"交易", "价格行为"},
				ChapterTitles: []string{"参考截图 chapter reference"},
				ExtraPrompt:   "简约金融线条",
			},
			empty: false,
		},
		{
			name: "minimal metadata without badge or subtitle",
			input: cover.PromptInput{
				Title: "Fabio",
			},
			empty: true,
		},
	}

	for _, st := range cover.ListStyles() {
		for _, tc := range cases {
			for _, minimal := range []bool{false, true} {
				mode := "standard"
				if minimal {
					mode = "minimal"
				}
				t.Run(st.ID+"/"+tc.name+"/"+mode, func(t *testing.T) {
					var prompt string
					if minimal {
						prompt = cover.BuildMinimalPrompt(st, tc.input)
					} else {
						prompt = cover.BuildPrompt(st, tc.input)
					}
					if !strings.HasPrefix(prompt, "A flat horizontal 16:10 ") {
						t.Fatalf("prompt does not start with flat horizontal 16:10 visual description:\n%s", prompt)
					}
					if strings.Contains(prompt, "\n") {
						t.Fatalf("expected single-paragraph prompt without newlines:\n%s", prompt)
					}
					for _, marker := range bannedSpecMarkers {
						if strings.Contains(prompt, marker) {
							t.Fatalf("prompt unexpectedly contains spec-sheet marker %q:\n%s", marker, prompt)
						}
					}
					for _, re := range forbiddenPatterns {
						if re.MatchString(prompt) {
							t.Fatalf("prompt matched forbidden trigger %q:\n%s", re.String(), prompt)
						}
					}
					for _, leaked := range []string{"参考图片", "screenshot", "参考截图"} {
						if strings.Contains(prompt, leaked) {
							t.Fatalf("prompt leaked raw overview/chapter text %q:\n%s", leaked, prompt)
						}
					}
					if tc.empty {
						for _, banned := range []string{"pill badge", "outlined tag", "subtitle text"} {
							if strings.Contains(prompt, banned) {
								t.Fatalf("empty badge/subtitle prompt unexpectedly contains %q:\n%s", banned, prompt)
							}
						}
					}
					if minimal && !tc.empty {
						for _, extraOnly := range []string{"简约金融线条", "价格行为"} {
							if strings.Contains(prompt, extraOnly) {
								t.Fatalf("minimal prompt unexpectedly contains %q:\n%s", extraOnly, prompt)
							}
						}
					}
				})
			}
		}
	}
}

func TestService_GenerateCourseCover_AppendsAndPreservesHistory(t *testing.T) {
	dataDir, cat, lib1, _ := setupCoverTestEnv(t)
	ctx := context.Background()

	courseA, err := cat.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID:  lib1.ID,
		Title:      "Go 语言并发编程实战",
		Instructor: "陈默",
		Overview:   "系统讲解 goroutine 与 channel",
		Tags:       []string{"Go", "并发"},
	})
	if err != nil {
		t.Fatalf("CreateCourse A: %v", err)
	}

	// Pre-populate a legacy .jpg cover for courseA; generating AI covers must preserve it
	oldCoverName := courseA.ID + "_cover.jpg"
	oldCoverPath := filepath.Join(dataDir, "covers", oldCoverName)
	if err := os.WriteFile(oldCoverPath, []byte("old-jpg"), 0o644); err != nil {
		t.Fatalf("write old cover: %v", err)
	}
	if _, err := cat.UpdateCourse(ctx, courseA.ID, catalog.UpdateCourseInput{CoverPath: &oldCoverName}); err != nil {
		t.Fatalf("UpdateCourse old cover: %v", err)
	}

	pngData1 := makePNG(t, 42)
	pngData2 := makePNG(t, 128)
	callNum := 0
	gen := &fakeImageGen{
		fn: func(ctx context.Context, req ai.ImageGenerateRequest) (*ai.GeneratedImage, error) {
			callNum++
			data := pngData1
			if callNum > 1 {
				data = pngData2
			}
			return &ai.GeneratedImage{
				Data:        data,
				ContentType: "image/png",
				Ext:         ".png",
				Width:       1536,
				Height:      1024,
			}, nil
		},
	}

	svc := cover.NewService(cover.Options{
		DataDir: dataDir,
		Catalog: cat,
		ConfigLoader: func(ctx context.Context) (cover.EffectiveImageSettings, error) {
			return cover.EffectiveImageSettings{
				Configured:     true,
				DefaultStyleID: "editorial_modern",
				Image:          ai.ImageConfig{Protocol: ai.ImageProtocolOpenAIImages, BaseURL: "https://api.example.com/v1", Model: "gpt-image-1"},
			}, nil
		},
		GeneratorFactory: func(cfg ai.ImageConfig) ai.ImageGenerator { return gen },
	})
	t.Cleanup(svc.Wait)

	emptySub := ""
	incBadge := false
	res1, err := svc.GenerateCourseCover(ctx, courseA.ID, cover.GenerateCoverInput{
		StyleID:      "dark_tech",
		Title:        "Go 并发实战 Pro",
		Subtitle:     &emptySub,
		IncludeBadge: &incBadge,
	})
	if err != nil {
		t.Fatalf("GenerateCourseCover 1: %v", err)
	}
	if res1.Cover.StyleID != "dark_tech" || res1.Cover.Source != catalog.CoverSourceAI || !res1.Cover.IsActive {
		t.Fatalf("unexpected first generated cover: %+v", res1.Cover)
	}
	// Verify prompt respected empty subtitle and disabled badge
	if strings.Contains(res1.Cover.Prompt, "讲师 · 陈默") || strings.Contains(res1.Cover.Prompt, "0 节") {
		t.Fatalf("expected omitted subtitle and badge in prompt:\n%s", res1.Cover.Prompt)
	}
	if res1.Course.CoverPath != res1.Cover.FilePath || len(res1.Course.Covers) != 2 {
		t.Fatalf("expected 2 covers with new active cover, got path=%q len=%d", res1.Course.CoverPath, len(res1.Course.Covers))
	}
	// Legacy cover must still exist on disk
	if _, err := os.Stat(oldCoverPath); err != nil {
		t.Fatalf("expected legacy cover preserved on disk: %v", err)
	}

	// Generate a second AI cover
	res2, err := svc.GenerateCourseCover(ctx, courseA.ID, cover.GenerateCoverInput{
		StyleID: "clay_3d",
		Title:   "Go 并发实战 V2",
	})
	if err != nil {
		t.Fatalf("GenerateCourseCover 2: %v", err)
	}
	if res2.Course.CoverPath != res2.Cover.FilePath || len(res2.Course.Covers) != 3 {
		t.Fatalf("expected 3 covers after second generation, got path=%q len=%d", res2.Course.CoverPath, len(res2.Course.Covers))
	}
	if res2.Course.Covers[0].ID != res2.Cover.ID || !res2.Course.Covers[0].IsActive {
		t.Fatalf("expected res2 active and first: %+v", res2.Course.Covers)
	}
	if res2.Course.Covers[1].ID != res1.Cover.ID || res2.Course.Covers[1].IsActive {
		t.Fatalf("expected res1 preserved second and inactive: %+v", res2.Course.Covers)
	}
	if res2.Course.Covers[2].FilePath != oldCoverName {
		t.Fatalf("expected legacy cover third: %+v", res2.Course.Covers[2])
	}
	for _, cov := range res2.Course.Covers {
		if _, err := os.Stat(filepath.Join(dataDir, "covers", cov.FilePath)); err != nil {
			t.Fatalf("expected cover file %q on disk: %v", cov.FilePath, err)
		}
	}
}

func TestService_BatchGenerateAndCancel(t *testing.T) {
	dataDir, cat, lib1, lib2 := setupCoverTestEnv(t)
	ctx := context.Background()

	cWithCover, _ := cat.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: lib1.ID, Title: "已有封面课"})
	existingName := cWithCover.ID + "_cover.jpg"
	_ = os.WriteFile(filepath.Join(dataDir, "covers", existingName), []byte("keep-me"), 0o644)
	_, _ = cat.UpdateCourse(ctx, cWithCover.ID, catalog.UpdateCourseInput{CoverPath: &existingName})

	cMissingOK, _ := cat.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: lib1.ID, Title: "待补齐课程一"})
	cMissingFail, _ := cat.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: lib1.ID, Title: "触发失败课程"})
	cOtherLib, _ := cat.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: lib2.ID, Title: "其他库课程"})

	pngData := makePNG(t, 88)
	gen := &fakeImageGen{
		fn: func(ctx context.Context, req ai.ImageGenerateRequest) (*ai.GeneratedImage, error) {
			if strings.Contains(req.Prompt, "触发失败课程") {
				return nil, errors.New("simulated upstream error")
			}
			return &ai.GeneratedImage{
				Data:        pngData,
				ContentType: "image/png",
				Ext:         ".png",
				Width:       1536,
				Height:      1024,
			}, nil
		},
	}

	svc := cover.NewService(cover.Options{
		DataDir: dataDir,
		Catalog: cat,
		ConfigLoader: func(ctx context.Context) (cover.EffectiveImageSettings, error) {
			return cover.EffectiveImageSettings{
				Configured:     true,
				DefaultStyleID: "editorial_modern",
				Image:          ai.ImageConfig{Protocol: ai.ImageProtocolOpenAIImages, BaseURL: "https://api.example.com/v1", Model: "gpt-image-1"},
			}, nil
		},
		GeneratorFactory: func(cfg ai.ImageConfig) ai.ImageGenerator { return gen },
	})
	t.Cleanup(svc.Wait)

	// Start batch scoped to lib1 in missing_only mode
	st, err := svc.StartBatch(ctx, cover.BatchCoverInput{
		LibraryID: lib1.ID,
		Mode:      "missing_only",
		StyleID:   "clay_3d",
	})
	if err != nil {
		t.Fatalf("StartBatch: %v", err)
	}
	if !st.Running || st.Total != 3 || st.Skipped != 1 {
		t.Fatalf("unexpected initial batch status: %+v", st)
	}

	svc.Wait()

	final := svc.GetBatchStatus()
	if final.Running || final.State != "completed" || final.Completed != 1 || final.Failed != 1 || final.Skipped != 1 {
		t.Fatalf("unexpected final batch status: %+v", final)
	}
	if len(final.Errors) != 1 || final.Errors[0].CourseID != cMissingFail.ID {
		t.Fatalf("unexpected batch errors: %+v", final.Errors)
	}

	gotKeep, _ := cat.GetCourseDetail(ctx, cWithCover.ID)
	if gotKeep.CoverPath != existingName {
		t.Fatalf("existing cover was overwritten: %q", gotKeep.CoverPath)
	}
	gotGen, _ := cat.GetCourseDetail(ctx, cMissingOK.ID)
	if !strings.HasPrefix(gotGen.CoverPath, cMissingOK.ID+"_cover_") || !strings.HasSuffix(gotGen.CoverPath, ".png") {
		t.Fatalf("expected generated cover for cMissingOK, got %q", gotGen.CoverPath)
	}
	if len(gotGen.Covers) != 1 || gotGen.Covers[0].Source != catalog.CoverSourceAIBatch {
		t.Fatalf("expected 1 ai_batch cover for cMissingOK, got %+v", gotGen.Covers)
	}
	gotOther, _ := cat.GetCourseDetail(ctx, cOtherLib.ID)
	if gotOther.CoverPath != "" {
		t.Fatalf("expected lib2 course untouched, got %q", gotOther.CoverPath)
	}

	// Batch mode "all" on cWithCover (via lib1, ignoring failure on cMissingFail) must append new cover & preserve existing
	if _, err := svc.StartBatch(ctx, cover.BatchCoverInput{
		LibraryID: lib1.ID,
		Mode:      "all",
		StyleID:   "dark_tech",
	}); err != nil {
		t.Fatalf("StartBatch mode=all: %v", err)
	}
	svc.Wait()

	afterAllKeep, _ := cat.GetCourseDetail(ctx, cWithCover.ID)
	if len(afterAllKeep.Covers) != 2 || afterAllKeep.CoverPath == existingName {
		t.Fatalf("expected mode=all to append & activate new cover while preserving legacy: path=%q covers=%+v", afterAllKeep.CoverPath, afterAllKeep.Covers)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "covers", existingName)); err != nil {
		t.Fatalf("expected existing cover file preserved after mode=all: %v", err)
	}

	// Test 409 conflict & cancellation
	started := make(chan struct{})
	release := make(chan struct{})
	blockingGen := &fakeImageGen{
		fn: func(ctx context.Context, req ai.ImageGenerateRequest) (*ai.GeneratedImage, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return &ai.GeneratedImage{Data: pngData, ContentType: "image/png", Ext: ".png", Width: 16, Height: 10}, nil
			}
		},
	}
	svcBlock := cover.NewService(cover.Options{
		DataDir: dataDir,
		Catalog: cat,
		ConfigLoader: func(ctx context.Context) (cover.EffectiveImageSettings, error) {
			return cover.EffectiveImageSettings{
				Configured:     true,
				DefaultStyleID: "editorial_modern",
				Image:          ai.ImageConfig{Protocol: ai.ImageProtocolOpenAIImages, BaseURL: "https://api.example.com/v1", Model: "gpt-image-1"},
			}, nil
		},
		GeneratorFactory: func(cfg ai.ImageConfig) ai.ImageGenerator { return blockingGen },
	})
	t.Cleanup(func() {
		close(release)
		svcBlock.Wait()
	})

	if _, err := svcBlock.StartBatch(ctx, cover.BatchCoverInput{LibraryID: lib2.ID, Mode: "all"}); err != nil {
		t.Fatalf("StartBatch blocking: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocking generator")
	}

	if _, err := svcBlock.StartBatch(ctx, cover.BatchCoverInput{LibraryID: lib2.ID, Mode: "all"}); !errors.Is(err, cover.ErrBatchAlreadyRunning) {
		t.Fatalf("expected ErrBatchAlreadyRunning, got %v", err)
	}

	cancelled := svcBlock.CancelBatch()
	if cancelled.Running || cancelled.State != "cancelled" {
		t.Fatalf("expected cancelled status, got %+v", cancelled)
	}
	svcBlock.Wait()
}

func TestService_ReferenceImageRefusalFallsBackToMinimalPrompt(t *testing.T) {
	dataDir, cat, lib1, lib2 := setupCoverTestEnv(t)
	ctx := context.Background()

	cSingle, err := cat.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID:  lib1.ID,
		Title:      "ICT 2022 交易带教课（中文系列）",
		Instructor: "ICT",
		Tags:       []string{"价格行为"},
	})
	if err != nil {
		t.Fatalf("CreateCourse cSingle: %v", err)
	}
	cBatch, err := cat.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: lib1.ID,
		Title:     "Fabio",
		Tags:      []string{"OrderFlow"},
	})
	if err != nil {
		t.Fatalf("CreateCourse cBatch: %v", err)
	}
	cOtherErr, err := cat.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: lib2.ID,
		Title:     "NonRefErrorCourse",
		Tags:      []string{"TagX"},
	})
	if err != nil {
		t.Fatalf("CreateCourse cOtherErr: %v", err)
	}

	pngData := makePNG(t, 64)
	refErr := errors.New("ai: image upstream returned HTTP 400: aixoras.com: 400 Bad Request: The prompt refers to a reference image, but the request did not include one. Attach the image (for example via /v1/images/edits) or remove the reference, then try again.")
	gen := &fakeImageGen{
		fn: func(ctx context.Context, req ai.ImageGenerateRequest) (*ai.GeneratedImage, error) {
			if strings.Contains(req.Prompt, "简约金融图表") || strings.Contains(req.Prompt, "OrderFlow") {
				return nil, refErr
			}
			return &ai.GeneratedImage{
				Data:        pngData,
				ContentType: "image/png",
				Ext:         ".png",
				Width:       1536,
				Height:      1024,
			}, nil
		},
	}

	svc := cover.NewService(cover.Options{
		DataDir: dataDir,
		Catalog: cat,
		ConfigLoader: func(ctx context.Context) (cover.EffectiveImageSettings, error) {
			return cover.EffectiveImageSettings{
				Configured:     true,
				DefaultStyleID: "editorial_modern",
				Image:          ai.ImageConfig{Protocol: ai.ImageProtocolOpenAIImages, BaseURL: "https://api.example.com/v1", Model: "gpt-image-2.6-sunburst"},
			}, nil
		},
		GeneratorFactory: func(cfg ai.ImageConfig) ai.ImageGenerator { return gen },
	})
	t.Cleanup(svc.Wait)

	// 1. Single-course cover generation falls back once to BuildMinimalPrompt
	singleRes, err := svc.GenerateCourseCover(ctx, cSingle.ID, cover.GenerateCoverInput{
		StyleID:     "editorial_modern",
		ExtraPrompt: "简约金融图表",
	})
	if err != nil {
		t.Fatalf("GenerateCourseCover fallback failed: %v", err)
	}
	gen.mu.Lock()
	singlePrompts := append([]string(nil), gen.prompts...)
	gen.prompts = nil
	gen.mu.Unlock()
	if len(singlePrompts) != 2 {
		t.Fatalf("expected 2 attempts for single course, got %d: %v", len(singlePrompts), singlePrompts)
	}
	if !strings.Contains(singlePrompts[0], "简约金融图表") || strings.Contains(singlePrompts[1], "简约金融图表") {
		t.Fatalf("unexpected single fallback prompts: %+v", singlePrompts)
	}
	if singleRes.Cover.Prompt != singlePrompts[1] {
		t.Fatalf("expected saved cover prompt to match minimal fallback prompt")
	}

	// 2. Batch generation (cSingle already has cover so skipped; cBatch triggers fallback)
	if _, err := svc.StartBatch(ctx, cover.BatchCoverInput{
		LibraryID: lib1.ID,
		Mode:      "missing_only",
		StyleID:   "editorial_modern",
	}); err != nil {
		t.Fatalf("StartBatch: %v", err)
	}
	svc.Wait()

	st := svc.GetBatchStatus()
	if st.Running || st.State != "completed" || st.Completed != 1 || st.Failed != 0 || st.Skipped != 1 {
		t.Fatalf("unexpected batch status after fallback: %+v", st)
	}
	gen.mu.Lock()
	batchPrompts := append([]string(nil), gen.prompts...)
	gen.mu.Unlock()
	if len(batchPrompts) != 2 {
		t.Fatalf("expected 2 attempts for batch course, got %d: %v", len(batchPrompts), batchPrompts)
	}
	if !strings.Contains(batchPrompts[0], "OrderFlow") || strings.Contains(batchPrompts[1], "OrderFlow") {
		t.Fatalf("unexpected batch fallback prompts: %+v", batchPrompts)
	}
	batchCourse, err := cat.GetCourseDetail(ctx, cBatch.ID)
	if err != nil || len(batchCourse.Covers) != 1 || batchCourse.Covers[0].Prompt != batchPrompts[1] {
		t.Fatalf("expected batch saved cover prompt to match minimal fallback prompt, course=%+v err=%v", batchCourse, err)
	}

	// 3. Non-reference error fails immediately without retrying
	errGen := &fakeImageGen{
		fn: func(ctx context.Context, req ai.ImageGenerateRequest) (*ai.GeneratedImage, error) {
			return nil, errors.New("simulated 500 error")
		},
	}
	errSvc := cover.NewService(cover.Options{
		DataDir: dataDir,
		Catalog: cat,
		ConfigLoader: func(ctx context.Context) (cover.EffectiveImageSettings, error) {
			return cover.EffectiveImageSettings{
				Configured:     true,
				DefaultStyleID: "editorial_modern",
				Image:          ai.ImageConfig{Protocol: ai.ImageProtocolOpenAIImages, BaseURL: "https://api.example.com/v1", Model: "gpt-image-2.6-sunburst"},
			}, nil
		},
		GeneratorFactory: func(cfg ai.ImageConfig) ai.ImageGenerator { return errGen },
	})
	t.Cleanup(errSvc.Wait)

	if _, err := errSvc.GenerateCourseCover(ctx, cOtherErr.ID, cover.GenerateCoverInput{StyleID: "editorial_modern"}); err == nil {
		t.Fatal("expected non-reference error to fail")
	}
	errGen.mu.Lock()
	errCalls := len(errGen.prompts)
	errGen.mu.Unlock()
	if errCalls != 1 {
		t.Fatalf("expected 1 attempt for non-reference error, got %d", errCalls)
	}
}
