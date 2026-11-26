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

func TestService_GeneratePreviewAndApply(t *testing.T) {
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
	courseB, err := cat.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: lib1.ID,
		Title:     "Rust 所有权精讲",
	})
	if err != nil {
		t.Fatalf("CreateCourse B: %v", err)
	}

	// Pre-populate an old .jpg cover for courseA so we verify preview doesn't overwrite it
	// and apply cleans up the old extension file.
	oldCoverName := courseA.ID + "_cover.jpg"
	oldCoverPath := filepath.Join(dataDir, "covers", oldCoverName)
	if err := os.WriteFile(oldCoverPath, []byte("old-jpg"), 0o644); err != nil {
		t.Fatalf("write old cover: %v", err)
	}
	if _, err := cat.UpdateCourse(ctx, courseA.ID, catalog.UpdateCourseInput{CoverPath: &oldCoverName}); err != nil {
		t.Fatalf("UpdateCourse old cover: %v", err)
	}

	pngData := makePNG(t, 42)
	gen := &fakeImageGen{
		fn: func(ctx context.Context, req ai.ImageGenerateRequest) (*ai.GeneratedImage, error) {
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

	emptySub := ""
	incBadge := false
	preview, err := svc.GenerateCoursePreview(ctx, courseA.ID, cover.GeneratePreviewInput{
		StyleID:      "dark_tech",
		Title:        "Go 并发实战 Pro",
		Subtitle:     &emptySub,
		IncludeBadge: &incBadge,
	})
	if err != nil {
		t.Fatalf("GenerateCoursePreview: %v", err)
	}
	if preview.StyleID != "dark_tech" || !strings.HasSuffix(preview.PreviewURL, preview.PreviewID) {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	// Verify prompt respected empty subtitle and disabled badge
	if strings.Contains(preview.Prompt, "讲师 · 陈默") || strings.Contains(preview.Prompt, "0 节") {
		t.Fatalf("expected omitted subtitle and badge in prompt:\n%s", preview.Prompt)
	}

	// Course A's persisted cover must still be unchanged before apply
	curA, _ := cat.GetCourseDetail(ctx, courseA.ID)
	if curA.CoverPath != oldCoverName {
		t.Fatalf("expected old cover preserved before apply, got %q", curA.CoverPath)
	}

	// Cross-course apply must be rejected
	if _, err := svc.ApplyCoursePreview(ctx, courseB.ID, preview.PreviewID); !errors.Is(err, cover.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on cross-course apply, got %v", err)
	}

	// GetPreviewFile returns valid file
	pPath, pType, err := svc.GetPreviewFile(preview.PreviewID)
	if err != nil || pType != "image/png" {
		t.Fatalf("GetPreviewFile: path=%q type=%q err=%v", pPath, pType, err)
	}

	// Apply to course A
	updated, err := svc.ApplyCoursePreview(ctx, courseA.ID, preview.PreviewID)
	if err != nil {
		t.Fatalf("ApplyCoursePreview: %v", err)
	}
	wantCover := courseA.ID + "_cover.png"
	if updated.CoverPath != wantCover {
		t.Fatalf("updated.CoverPath=%q, want %q", updated.CoverPath, wantCover)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "covers", wantCover)); err != nil {
		t.Fatalf("expected new cover on disk: %v", err)
	}
	if _, err := os.Stat(oldCoverPath); !os.IsNotExist(err) {
		t.Fatalf("expected old .jpg cover to be cleaned up, stat err=%v", err)
	}
	// Preview should be removed after apply
	if _, _, err := svc.GetPreviewFile(preview.PreviewID); !errors.Is(err, cover.ErrPreviewNotFound) {
		t.Fatalf("expected preview cleaned after apply, got %v", err)
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
	if gotGen.CoverPath != cMissingOK.ID+"_cover.png" {
		t.Fatalf("expected generated cover for cMissingOK, got %q", gotGen.CoverPath)
	}
	gotOther, _ := cat.GetCourseDetail(ctx, cOtherLib.ID)
	if gotOther.CoverPath != "" {
		t.Fatalf("expected lib2 course untouched, got %q", gotOther.CoverPath)
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
