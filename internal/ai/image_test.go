package ai_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"coursefin/internal/ai"
)

func makeTestPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 120, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestValidateImageProtocol(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", ai.ImageProtocolOpenAIImages, false},
		{"  ", ai.ImageProtocolOpenAIImages, false},
		{"openai_images", ai.ImageProtocolOpenAIImages, false},
		{"OPENAI_CHAT", ai.ImageProtocolOpenAIChat, false},
		{"gemini", ai.ImageProtocolGemini, false},
		{"midjourney", "", true},
	} {
		got, err := ai.ValidateImageProtocol(tc.in)
		if (err != nil) != tc.wantErr {
			t.Fatalf("ValidateImageProtocol(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
		}
		if !tc.wantErr && got != tc.want {
			t.Fatalf("ValidateImageProtocol(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHTTPImageGenerator_OpenAIImages_B64AndSizeFallback(t *testing.T) {
	pngBytes := makeTestPNGBytes(t, 24, 16)
	var calls atomic.Int32
	var seenSizes []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-img" {
			t.Fatalf("missing auth header: %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Size string `json:"size"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seenSizes = append(seenSizes, body.Size)

		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unsupported size 1536x1024"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"b64_json": base64.StdEncoding.EncodeToString(pngBytes)},
			},
		})
	}))
	defer srv.Close()

	gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
		Protocol: ai.ImageProtocolOpenAIImages,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "sk-img",
		Model:    "gpt-image-1",
	}, srv.Client())

	got, err := gen.GenerateImage(context.Background(), ai.ImageGenerateRequest{Prompt: "Go course cover"})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if len(seenSizes) != 2 || seenSizes[0] != "1536x1024" || seenSizes[1] != "1792x1024" {
		t.Fatalf("unexpected size fallback sequence: %v", seenSizes)
	}
	if got.Ext != ".png" || got.Width != 24 || got.Height != 16 {
		t.Fatalf("unexpected image metadata: %+v", got)
	}
}

func TestHTTPImageGenerator_OpenAIImages_URLDownload(t *testing.T) {
	pngBytes := makeTestPNGBytes(t, 16, 10)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{
					{"url": srv.URL + "/cdn/generated.png"},
				},
			})
		case "/cdn/generated.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
		Protocol: ai.ImageProtocolOpenAIImages,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "sk-url",
		Model:    "flux-pro",
		Size:     "1024x1024",
	}, srv.Client())

	got, err := gen.GenerateImage(context.Background(), ai.ImageGenerateRequest{Prompt: "Rust course"})
	if err != nil {
		t.Fatalf("GenerateImage URL: %v", err)
	}
	if got.Ext != ".png" || got.Width != 16 || got.Height != 10 {
		t.Fatalf("unexpected image: %+v", got)
	}
}

func TestHTTPImageGenerator_OpenAIChat_ImagesAndMarkdownFallback(t *testing.T) {
	pngBytes := makeTestPNGBytes(t, 20, 12)
	b64 := base64.StdEncoding.EncodeToString(pngBytes)

	t.Run("message.images with modalities fallback", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/chat/completions" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if calls.Add(1) == 1 {
				if _, ok := body["modalities"]; !ok {
					t.Fatalf("expected modalities on first attempt")
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"unknown field modalities"}}`))
				return
			}
			if _, ok := body["modalities"]; ok {
				t.Fatalf("expected modalities omitted on fallback attempt")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{
							"images": []any{
								map[string]any{
									"image_url": map[string]string{
										"url": "data:image/png;base64," + b64,
									},
								},
							},
						},
					},
				},
			})
		}))
		defer srv.Close()

		gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
			Protocol: ai.ImageProtocolOpenAIChat,
			BaseURL:  srv.URL + "/v1",
			APIKey:   "sk-chat",
			Model:    "gemini-2.5-flash-image",
		}, srv.Client())

		got, err := gen.GenerateImage(context.Background(), ai.ImageGenerateRequest{Prompt: "Cover"})
		if err != nil {
			t.Fatalf("GenerateImage openai_chat images: %v", err)
		}
		if got.Width != 20 || got.Height != 12 {
			t.Fatalf("unexpected dimensions: %+v", got)
		}
	})

	t.Run("markdown data URI inside string content", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{
							"content": "Here is your cover:\n![cover](data:image/png;base64," + b64 + ")",
						},
					},
				},
			})
		}))
		defer srv.Close()

		gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
			Protocol: ai.ImageProtocolOpenAIChat,
			BaseURL:  srv.URL + "/v1",
			Model:    "gpt-4o-image",
		}, srv.Client())

		got, err := gen.GenerateImage(context.Background(), ai.ImageGenerateRequest{Prompt: "Cover"})
		if err != nil {
			t.Fatalf("GenerateImage markdown: %v", err)
		}
		if got.Ext != ".png" || got.Width != 20 {
			t.Fatalf("unexpected image: %+v", got)
		}
	})
}

func TestHTTPImageGenerator_Gemini(t *testing.T) {
	pngBytes := makeTestPNGBytes(t, 18, 12)
	b64 := base64.StdEncoding.EncodeToString(pngBytes)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.5-flash-image:generateContent" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "goog-secret" {
			t.Fatalf("unexpected x-goog-api-key: %q", r.Header.Get("x-goog-api-key"))
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("api key must not be in query string: %q", r.URL.RawQuery)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"imageConfig unsupported"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{
							map[string]any{
								"inlineData": map[string]string{
									"mimeType": "image/png",
									"data":     b64,
								},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
		Protocol: ai.ImageProtocolGemini,
		BaseURL:  srv.URL + "/v1beta",
		APIKey:   "goog-secret",
		Model:    "gemini-2.5-flash-image",
	}, srv.Client())

	got, err := gen.GenerateImage(context.Background(), ai.ImageGenerateRequest{Prompt: "Cover"})
	if err != nil {
		t.Fatalf("GenerateImage gemini: %v", err)
	}
	if got.Width != 18 || got.Height != 12 {
		t.Fatalf("unexpected image: %+v", got)
	}
}

func TestHTTPImageGenerator_RejectsCorruptPayloadAndSurfacesUpstreamError(t *testing.T) {
	t.Run("corrupt image bytes rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{
					{"b64_json": base64.StdEncoding.EncodeToString([]byte("not an image at all"))},
				},
			})
		}))
		defer srv.Close()

		gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
			Protocol: ai.ImageProtocolOpenAIImages,
			BaseURL:  srv.URL + "/v1",
			Model:    "gpt-image-1",
		}, srv.Client())

		if _, err := gen.GenerateImage(context.Background(), ai.ImageGenerateRequest{Prompt: "Cover"}); err == nil {
			t.Fatal("expected error for non-image payload")
		}
	})

	t.Run("HTTP 401 error surfaced", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
		}))
		defer srv.Close()

		gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
			Protocol: ai.ImageProtocolOpenAIImages,
			BaseURL:  srv.URL + "/v1",
			Model:    "gpt-image-1",
		}, srv.Client())

		err := gen.TestConnection(context.Background())
		if err == nil || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "invalid api key") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestHTTPImageGenerator_OpenAIImages_DoesNotRetryNonSize400(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
	}{
		{
			name:    "english reference image error",
			message: "The prompt refers to a reference image, but the request did not include one. Attach the image (for example via /v1/images/edits) or remove the reference, then try again.",
		},
		{
			name:    "chinese missing original image error",
			message: "目前没有可供编辑的原始图片。请上传需要编辑的图片，我就可以按照这份设计要求制作。",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]string{
						"message": tc.message,
					},
				})
			}))
			defer srv.Close()

			gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
				Protocol: ai.ImageProtocolOpenAIImages,
				BaseURL:  srv.URL + "/v1",
				APIKey:   "sk-test",
				Model:    "gpt-image-2.6-sunburst",
			}, srv.Client())

			_, err := gen.GenerateImage(context.Background(), ai.ImageGenerateRequest{Prompt: "Cover"})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("expected exactly 1 HTTP request without blind size fallback, got %d", got)
			}
			if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestHTTPImageGenerator_TestConnection_UsesAutoSizeSequence(t *testing.T) {
	pngBytes := makeTestPNGBytes(t, 24, 16)
	var calls atomic.Int32
	var seenSizes []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Size string `json:"size"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seenSizes = append(seenSizes, body.Size)

		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unsupported size 1536x1024"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"b64_json": base64.StdEncoding.EncodeToString(pngBytes)},
			},
		})
	}))
	defer srv.Close()

	gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
		Protocol: ai.ImageProtocolOpenAIImages,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "sk-test",
		Model:    "gpt-image-2.6-sunburst",
	}, srv.Client())

	if err := gen.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if len(seenSizes) != 2 || seenSizes[0] != "1536x1024" || seenSizes[1] != "1792x1024" {
		t.Fatalf("unexpected size fallback sequence in TestConnection: %v", seenSizes)
	}
}

func TestHTTPImageGenerator_OpenAIImages_TimeoutErrorIncludesDiagnostics(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	client := &http.Client{Timeout: 40 * time.Millisecond}
	gen := ai.NewHTTPImageGenerator(ai.ImageConfig{
		Protocol: ai.ImageProtocolOpenAIImages,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "sk-test",
		Model:    "gpt-image-2.6-sunburst",
		Size:     "1536x1024",
	}, client)

	_, err := gen.GenerateImage(context.Background(), ai.ImageGenerateRequest{Prompt: "Cover"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	for _, want := range []string{
		"ai: image request failed after",
		"model=gpt-image-2.6-sunburst",
		"size=1536x1024",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error %q to contain %q", err.Error(), want)
		}
	}
}
