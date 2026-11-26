package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

const (
	ImageProtocolOpenAIImages = "openai_images"
	ImageProtocolOpenAIChat   = "openai_chat"
	ImageProtocolGemini       = "gemini"

	DefaultGeminiBaseURL    = "https://generativelanguage.googleapis.com/v1beta"
	defaultImageHTTPTimeout = 240 * time.Second
	MaxGeneratedImageBytes  = 20 << 20 // 20 MiB
	maxImageRedirects       = 3
)

var (
	dataURIImageRegex  = regexp.MustCompile(`data:image/(?:png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=_\-\r\n]+)`)
	markdownImageRegex = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^\s)]+)\)`)
)

// ImageConfig configures an AI image generation provider endpoint.
type ImageConfig struct {
	Protocol string `json:"protocol"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	Size     string `json:"size"`
}

// ImageGenerateRequest describes a single image generation call.
type ImageGenerateRequest struct {
	Prompt      string `json:"prompt"`
	Size        string `json:"size,omitempty"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
}

// GeneratedImage holds decoded image bytes and validated metadata.
type GeneratedImage struct {
	Data        []byte
	ContentType string
	Ext         string
	Width       int
	Height      int
}

// ImageGenerator defines CourseFin's pluggable AI image generation interface.
type ImageGenerator interface {
	GenerateImage(ctx context.Context, req ImageGenerateRequest) (*GeneratedImage, error)
	TestConnection(ctx context.Context) error
}

// NormalizeImageProtocol normalizes an image protocol string, defaulting empty values to openai_images.
func NormalizeImageProtocol(raw string) string {
	norm := strings.ToLower(strings.TrimSpace(raw))
	if norm == "" {
		return ImageProtocolOpenAIImages
	}
	return norm
}

// ValidateImageProtocol checks whether raw is empty or one of the supported image protocols.
func ValidateImageProtocol(raw string) (string, error) {
	norm := NormalizeImageProtocol(raw)
	switch norm {
	case ImageProtocolOpenAIImages, ImageProtocolOpenAIChat, ImageProtocolGemini:
		return norm, nil
	default:
		return "", fmt.Errorf("ai: unsupported image protocol %q", raw)
	}
}

// HTTPImageGenerator implements ImageGenerator over OpenAI Images, OpenAI Chat, and Gemini APIs.
type HTTPImageGenerator struct {
	cfg        ImageConfig
	httpClient *http.Client
}

// NewHTTPImageGenerator constructs a multi-protocol HTTP image generator.
func NewHTTPImageGenerator(cfg ImageConfig, httpClient *http.Client) *HTTPImageGenerator {
	cfg.Protocol = NormalizeImageProtocol(cfg.Protocol)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Size = strings.TrimSpace(cfg.Size)

	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultImageHTTPTimeout}
	}
	return &HTTPImageGenerator{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

// TestConnection verifies endpoint reachability, authentication, and model image output.
func (g *HTTPImageGenerator) TestConnection(ctx context.Context) error {
	req := ImageGenerateRequest{
		Prompt: "Create a brand-new standalone minimal solid cobalt blue geometric square icon on a light gray studio background, with no text.",
		Size:   g.cfg.Size,
	}
	_, err := g.GenerateImage(ctx, req)
	return err
}

// GenerateImage invokes the configured protocol and returns a validated image payload.
func (g *HTTPImageGenerator) GenerateImage(ctx context.Context, req ImageGenerateRequest) (*GeneratedImage, error) {
	protocol, err := ValidateImageProtocol(g.cfg.Protocol)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(g.cfg.Model) == "" {
		return nil, errors.New("ai: image model is not configured")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("ai: image prompt must not be empty")
	}

	switch protocol {
	case ImageProtocolOpenAIImages:
		return g.generateOpenAIImages(ctx, req)
	case ImageProtocolOpenAIChat:
		return g.generateOpenAIChat(ctx, req)
	case ImageProtocolGemini:
		return g.generateGemini(ctx, req)
	default:
		return nil, fmt.Errorf("ai: unsupported image protocol %q", protocol)
	}
}

func (g *HTTPImageGenerator) generateOpenAIImages(ctx context.Context, req ImageGenerateRequest) (*GeneratedImage, error) {
	endpoint := normalizeImagesGenerationsURL(g.cfg.BaseURL)
	if endpoint == "" {
		return nil, errors.New("ai: image base URL is not configured")
	}

	sizePref := strings.TrimSpace(req.Size)
	if sizePref == "" {
		sizePref = g.cfg.Size
	}
	sizesToTry := []string{sizePref}
	if sizePref == "" || strings.EqualFold(sizePref, "auto") {
		sizesToTry = []string{"1536x1024", "1792x1024", "1024x1024", ""}
	}

	type openAIImageGenReq struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		N      int    `json:"n"`
		Size   string `json:"size,omitempty"`
	}

	var statusCode int
	var respBody []byte
	for idx, sz := range sizesToTry {
		payload, err := json.Marshal(openAIImageGenReq{
			Model:  g.cfg.Model,
			Prompt: req.Prompt,
			N:      1,
			Size:   sz,
		})
		if err != nil {
			return nil, fmt.Errorf("ai: marshal image request: %w", err)
		}

		headers := map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		}
		if g.cfg.APIKey != "" {
			headers["Authorization"] = "Bearer " + g.cfg.APIKey
		}

		statusCode, respBody, err = g.doPostJSON(ctx, endpoint, headers, payload)
		if err != nil {
			if sz != "" {
				return nil, fmt.Errorf("%w (model=%s, size=%s)", err, g.cfg.Model, sz)
			}
			return nil, fmt.Errorf("%w (model=%s)", err, g.cfg.Model)
		}
		if statusCode >= 200 && statusCode < 300 {
			break
		}
		if (statusCode == http.StatusBadRequest || statusCode == http.StatusUnprocessableEntity) && idx < len(sizesToTry)-1 && isLikelySizeError(respBody, sz) {
			continue
		}
		return nil, fmt.Errorf("ai: image upstream returned HTTP %d: %s", statusCode, formatUpstreamError(respBody))
	}

	var parsed struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("ai: decode image response JSON: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return nil, fmt.Errorf("ai: image upstream error: %s", strings.TrimSpace(parsed.Error.Message))
	}
	if len(parsed.Data) == 0 {
		return nil, errors.New("ai: image response contained no data items")
	}

	item := parsed.Data[0]
	if b64 := strings.TrimSpace(item.B64JSON); b64 != "" {
		raw, err := decodeBase64Image(b64)
		if err != nil {
			return nil, err
		}
		return decodeAndValidateImage(raw)
	}
	if imgURL := strings.TrimSpace(item.URL); imgURL != "" {
		raw, err := g.downloadImageURL(ctx, imgURL)
		if err != nil {
			return nil, err
		}
		return decodeAndValidateImage(raw)
	}
	return nil, errors.New("ai: image response contained neither b64_json nor url")
}

func (g *HTTPImageGenerator) generateOpenAIChat(ctx context.Context, req ImageGenerateRequest) (*GeneratedImage, error) {
	endpoint := normalizeChatCompletionsURL(g.cfg.BaseURL)
	if endpoint == "" {
		return nil, errors.New("ai: image base URL is not configured")
	}

	type chatImageReq struct {
		Model      string        `json:"model"`
		Messages   []ChatMessage `json:"messages"`
		Modalities []string      `json:"modalities,omitempty"`
	}

	attempts := [][]string{{"image", "text"}, nil}
	var respBody []byte
	for idx, modalities := range attempts {
		payload, err := json.Marshal(chatImageReq{
			Model: g.cfg.Model,
			Messages: []ChatMessage{
				{Role: "user", Content: req.Prompt},
			},
			Modalities: modalities,
		})
		if err != nil {
			return nil, fmt.Errorf("ai: marshal chat image request: %w", err)
		}

		headers := map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		}
		if g.cfg.APIKey != "" {
			headers["Authorization"] = "Bearer " + g.cfg.APIKey
		}

		statusCode, body, reqErr := g.doPostJSON(ctx, endpoint, headers, payload)
		if reqErr != nil {
			return nil, fmt.Errorf("%w (model=%s)", reqErr, g.cfg.Model)
		}
		if statusCode >= 200 && statusCode < 300 {
			respBody = body
			break
		}
		if (statusCode == http.StatusBadRequest || statusCode == http.StatusUnprocessableEntity) && idx < len(attempts)-1 {
			continue
		}
		return nil, fmt.Errorf("ai: image upstream returned HTTP %d: %s", statusCode, formatUpstreamError(body))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
				Images  []struct {
					URL      string `json:"url"`
					B64JSON  string `json:"b64_json"`
					ImageURL *struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"images"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("ai: decode chat image response JSON: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return nil, fmt.Errorf("ai: image upstream error: %s", strings.TrimSpace(parsed.Error.Message))
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("ai: chat image response contained no choices")
	}

	msg := parsed.Choices[0].Message
	for _, img := range msg.Images {
		if b64 := strings.TrimSpace(img.B64JSON); b64 != "" {
			raw, err := decodeBase64Image(b64)
			if err != nil {
				return nil, err
			}
			return decodeAndValidateImage(raw)
		}
		candURL := strings.TrimSpace(img.URL)
		if candURL == "" && img.ImageURL != nil {
			candURL = strings.TrimSpace(img.ImageURL.URL)
		}
		if candURL != "" {
			raw, err := g.resolveDataOrHTTPImage(ctx, candURL)
			if err != nil {
				return nil, err
			}
			return decodeAndValidateImage(raw)
		}
	}

	if len(msg.Content) > 0 && string(msg.Content) != "null" {
		rawTrimmed := bytes.TrimSpace(msg.Content)
		if len(rawTrimmed) > 0 && rawTrimmed[0] == '[' {
			var parts []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				URL      string `json:"url"`
				B64JSON  string `json:"b64_json"`
				Data     string `json:"data"`
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url"`
			}
			if err := json.Unmarshal(rawTrimmed, &parts); err == nil {
				for _, part := range parts {
					if b64 := firstNonEmptyImageString(part.B64JSON, part.Data); b64 != "" {
						raw, derr := decodeBase64Image(b64)
						if derr != nil {
							return nil, derr
						}
						return decodeAndValidateImage(raw)
					}
					candURL := strings.TrimSpace(part.URL)
					if candURL == "" && part.ImageURL != nil {
						candURL = strings.TrimSpace(part.ImageURL.URL)
					}
					if candURL != "" {
						raw, rerr := g.resolveDataOrHTTPImage(ctx, candURL)
						if rerr != nil {
							return nil, rerr
						}
						return decodeAndValidateImage(raw)
					}
					if part.Text != "" {
						if img, ok, err := g.extractImageFromText(ctx, part.Text); err != nil {
							return nil, err
						} else if ok {
							return img, nil
						}
					}
				}
			}
		} else {
			var text string
			if err := json.Unmarshal(rawTrimmed, &text); err == nil {
				if img, ok, err := g.extractImageFromText(ctx, text); err != nil {
					return nil, err
				} else if ok {
					return img, nil
				}
			}
		}
	}

	return nil, errors.New("ai: chat image response did not contain any image payload")
}

func (g *HTTPImageGenerator) generateGemini(ctx context.Context, req ImageGenerateRequest) (*GeneratedImage, error) {
	base := g.cfg.BaseURL
	if base == "" {
		base = DefaultGeminiBaseURL
	}
	endpoint := normalizeGeminiGenerateContentURL(base, g.cfg.Model)
	if endpoint == "" {
		return nil, errors.New("ai: gemini endpoint URL is invalid")
	}

	aspect := resolveGeminiAspectRatio(req.AspectRatio, req.Size, g.cfg.Size)

	type geminiImageConfig struct {
		AspectRatio string `json:"aspectRatio,omitempty"`
	}
	type geminiGenConfig struct {
		ResponseModalities []string           `json:"responseModalities"`
		ImageConfig        *geminiImageConfig `json:"imageConfig,omitempty"`
	}
	type geminiPart struct {
		Text string `json:"text,omitempty"`
	}
	type geminiContent struct {
		Parts []geminiPart `json:"parts"`
	}
	type geminiReq struct {
		Contents         []geminiContent `json:"contents"`
		GenerationConfig geminiGenConfig `json:"generationConfig"`
	}

	configs := []geminiGenConfig{
		{
			ResponseModalities: []string{"IMAGE"},
			ImageConfig:        &geminiImageConfig{AspectRatio: aspect},
		},
		{
			ResponseModalities: []string{"TEXT", "IMAGE"},
		},
	}

	var respBody []byte
	for idx, genCfg := range configs {
		payload, err := json.Marshal(geminiReq{
			Contents: []geminiContent{
				{Parts: []geminiPart{{Text: req.Prompt}}},
			},
			GenerationConfig: genCfg,
		})
		if err != nil {
			return nil, fmt.Errorf("ai: marshal gemini image request: %w", err)
		}

		headers := map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		}
		if g.cfg.APIKey != "" {
			headers["x-goog-api-key"] = g.cfg.APIKey
		}

		statusCode, body, reqErr := g.doPostJSON(ctx, endpoint, headers, payload)
		if reqErr != nil {
			return nil, fmt.Errorf("%w (model=%s)", reqErr, g.cfg.Model)
		}
		if statusCode >= 200 && statusCode < 300 {
			respBody = body
			break
		}
		if (statusCode == http.StatusBadRequest || statusCode == http.StatusUnprocessableEntity) && idx < len(configs)-1 {
			continue
		}
		return nil, fmt.Errorf("ai: gemini image upstream returned HTTP %d: %s", statusCode, formatUpstreamError(body))
	}

	type geminiInlineData struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineDataCamel *geminiInlineData `json:"inlineData"`
					InlineDataSnake *geminiInlineData `json:"inline_data"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("ai: decode gemini image response JSON: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return nil, fmt.Errorf("ai: gemini image upstream error: %s", strings.TrimSpace(parsed.Error.Message))
	}

	for _, cand := range parsed.Candidates {
		for _, part := range cand.Content.Parts {
			inline := part.InlineDataCamel
			if inline == nil {
				inline = part.InlineDataSnake
			}
			if inline != nil && strings.TrimSpace(inline.Data) != "" {
				raw, err := decodeBase64Image(inline.Data)
				if err != nil {
					return nil, err
				}
				return decodeAndValidateImage(raw)
			}
		}
	}

	return nil, errors.New("ai: gemini response contained no inline image data")
}

func (g *HTTPImageGenerator) doPostJSON(ctx context.Context, endpoint string, headers map[string]string, payload []byte) (int, []byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("ai: create image request: %w", err)
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	started := time.Now()
	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("ai: image request failed after %s: %w", time.Since(started).Round(time.Millisecond), err)
	}
	defer resp.Body.Close()

	// Allow base64 image JSON responses up to 32 MiB to comfortably hold 20 MiB raw images.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("ai: read image response body: %w", err)
	}
	return resp.StatusCode, body, nil
}

func isLikelySizeError(body []byte, triedSize string) bool {
	trimmedSize := strings.TrimSpace(triedSize)
	if trimmedSize == "" {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(formatUpstreamError(body)))
	if msg == "" || msg == "empty response body" {
		return true
	}
	if strings.Contains(msg, strings.ToLower(trimmedSize)) {
		return true
	}
	for _, kw := range []string{
		"size",
		"dimension",
		"resolution",
		"aspect",
		"width",
		"height",
		"pixel",
		"must be one of",
		"supported values",
		"allowed values",
		"尺寸",
		"分辨率",
		"宽高",
		"画幅",
		"比例",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

func (g *HTTPImageGenerator) extractImageFromText(ctx context.Context, text string) (*GeneratedImage, bool, error) {
	if m := dataURIImageRegex.FindStringSubmatch(text); len(m) >= 2 {
		raw, err := decodeBase64Image(m[1])
		if err != nil {
			return nil, true, err
		}
		img, err := decodeAndValidateImage(raw)
		return img, true, err
	}
	if m := markdownImageRegex.FindStringSubmatch(text); len(m) >= 2 {
		raw, err := g.downloadImageURL(ctx, m[1])
		if err != nil {
			return nil, true, err
		}
		img, err := decodeAndValidateImage(raw)
		return img, true, err
	}
	return nil, false, nil
}

func (g *HTTPImageGenerator) resolveDataOrHTTPImage(ctx context.Context, rawURL string) ([]byte, error) {
	trimmed := strings.TrimSpace(rawURL)
	if strings.HasPrefix(strings.ToLower(trimmed), "data:image/") {
		return decodeBase64Image(trimmed)
	}
	return g.downloadImageURL(ctx, trimmed)
}

func (g *HTTPImageGenerator) downloadImageURL(ctx context.Context, rawURL string) ([]byte, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("ai: invalid image download URL: %w", err)
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return nil, fmt.Errorf("ai: unsupported image download URL scheme %q", parsedURL.Scheme)
	}

	dlClient := &http.Client{
		Timeout:   g.httpClient.Timeout,
		Transport: g.httpClient.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxImageRedirects {
				return errors.New("ai: image download exceeded maximum redirects")
			}
			if req.URL == nil || (!strings.EqualFold(req.URL.Scheme, "http") && !strings.EqualFold(req.URL.Scheme, "https")) {
				return errors.New("ai: image redirect targeted unsupported scheme")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("ai: create image download request: %w", err)
	}
	resp, err := dlClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: download generated image failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai: image download returned HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxGeneratedImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ai: read downloaded image: %w", err)
	}
	if len(raw) > MaxGeneratedImageBytes {
		return nil, fmt.Errorf("ai: generated image exceeds maximum size of %d bytes", MaxGeneratedImageBytes)
	}
	return raw, nil
}

func decodeBase64Image(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(s), "data:") {
		idx := strings.Index(s, ";base64,")
		if idx < 0 {
			return nil, errors.New("ai: unsupported data URI encoding (expected base64)")
		}
		s = s[idx+len(";base64,"):]
	}
	s = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return nil, errors.New("ai: empty base64 image payload")
	}

	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(s); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return decoded, nil
	}
	return nil, errors.New("ai: decode base64 image payload failed")
}

func decodeAndValidateImage(raw []byte) (*GeneratedImage, error) {
	if len(raw) == 0 {
		return nil, errors.New("ai: generated image payload is empty")
	}
	if len(raw) > MaxGeneratedImageBytes {
		return nil, fmt.Errorf("ai: generated image exceeds maximum size of %d bytes", MaxGeneratedImageBytes)
	}

	ct := http.DetectContentType(raw)
	var ext string
	switch ct {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		return nil, fmt.Errorf("ai: upstream did not return a valid image (%s)", ct)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("ai: generated image failed integrity decode: %v", err)
	}

	return &GeneratedImage{
		Data:        raw,
		ContentType: ct,
		Ext:         ext,
		Width:       cfg.Width,
		Height:      cfg.Height,
	}, nil
}

func normalizeImagesGenerationsURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	if u == "" {
		return ""
	}
	if strings.HasSuffix(u, "/images/generations") {
		return u
	}
	return u + "/images/generations"
}

func normalizeGeminiGenerateContentURL(baseURL, model string) string {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u == "" {
		return ""
	}
	if strings.Contains(u, ":generateContent") {
		return u
	}
	cleanModel := strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if cleanModel == "" {
		return ""
	}
	return u + "/models/" + url.PathEscape(cleanModel) + ":generateContent"
}

func resolveGeminiAspectRatio(reqRatio, reqSize, cfgSize string) string {
	if r := strings.TrimSpace(reqRatio); r != "" {
		return r
	}
	sz := strings.TrimSpace(reqSize)
	if sz == "" || strings.EqualFold(sz, "auto") {
		sz = strings.TrimSpace(cfgSize)
	}
	switch strings.ToLower(sz) {
	case "", "auto":
		return "3:2"
	case "1:1", "3:2", "4:3", "16:9", "9:16":
		return sz
	case "1792x1024", "1920x1080", "1280x720":
		return "16:9"
	case "1536x1024", "1200x800":
		return "3:2"
	case "1024x1024", "512x512":
		return "1:1"
	}
	if parts := strings.Split(strings.ToLower(sz), "x"); len(parts) == 2 {
		w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 == nil && err2 == nil && w > 0 && h > 0 {
			ratio := float64(w) / float64(h)
			switch {
			case ratio >= 1.65:
				return "16:9"
			case ratio >= 1.2:
				return "3:2"
			case ratio <= 0.8:
				return "3:4"
			default:
				return "1:1"
			}
		}
	}
	return "3:2"
}

func firstNonEmptyImageString(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}
