package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultHTTPTimeout   = 60 * time.Second
	maxResponseBodyBytes = 4 << 20 // 4 MiB
	maxErrorSnippetRunes = 300
	organizeSystemPrompt = `You are an expert course metadata organizer for CourseFin.
Given a course's current title, directory hint, chapters, lesson filenames/titles, and optional subtitle excerpts, produce a clean, well-structured course metadata proposal.

Respond with STRICTLY valid JSON only (no markdown code fences, no commentary) matching this exact schema:
{
  "title": "cleaned course title",
  "overview": "concise course summary",
  "tags": ["3 to 8 normalized topic tags"],
  "chapters": [
    {
      "title": "chapter title",
      "lessons": [
        {
          "lesson_id": "exact lesson_id from input",
          "title": "cleaned lesson title"
        }
      ]
    }
  ]
}

Rules:
1. Preserve the primary language of the course material (e.g. Chinese or English).
2. Clean up messy numbering, resolution tags, or noisy filename artifacts while keeping lessons logically ordered.
3. Group lessons into clear, logical chapters.
4. CRITICAL: Every lesson_id from the input MUST appear EXACTLY ONCE across chapters[].lessons[], preserved verbatim. Never omit, duplicate, or invent a lesson_id.`
)

// OpenAICompatibleProvider implements Provider against any OpenAI-compatible /chat/completions endpoint.
type OpenAICompatibleProvider struct {
	cfg        Config
	endpoint   string
	httpClient *http.Client
}

// NewOpenAICompatibleProvider creates an OpenAI-compatible AI provider.
func NewOpenAICompatibleProvider(cfg Config, httpClient *http.Client) *OpenAICompatibleProvider {
	if cfg.MaxSubtitleChars <= 0 {
		cfg.MaxSubtitleChars = DefaultMaxSubtitleChars
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}

	return &OpenAICompatibleProvider{
		cfg:        cfg,
		endpoint:   normalizeChatCompletionsURL(cfg.BaseURL),
		httpClient: httpClient,
	}
}

func normalizeChatCompletionsURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	if u == "" {
		return ""
	}
	if strings.HasSuffix(u, "/chat/completions") {
		return u
	}
	return u + "/chat/completions"
}

type openAIChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ChatCompletion executes a generic chat completion call against the configured endpoint.
func (p *OpenAICompatibleProvider) ChatCompletion(ctx context.Context, req ChatRequest) (string, error) {
	if p.endpoint == "" {
		return "", errors.New("ai: base URL is not configured")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(p.cfg.Model)
	}
	if model == "" {
		return "", errors.New("ai: model is not configured")
	}
	if len(req.Messages) == 0 {
		return "", errors.New("ai: chat messages must not be empty")
	}

	payload, err := json.Marshal(openAIChatRequest{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("ai: marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("ai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if apiKey := strings.TrimSpace(p.cfg.APIKey); apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ai: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if err != nil {
		return "", fmt.Errorf("ai: read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ai: upstream returned HTTP %d: %s", resp.StatusCode, formatUpstreamError(body))
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("ai: decode response JSON: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", fmt.Errorf("ai: upstream error: %s", strings.TrimSpace(parsed.Error.Message))
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("ai: response contained no choices")
	}
	content := parsed.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", errors.New("ai: response content is empty")
	}
	return content, nil
}

// TestConnection verifies endpoint reachability, authentication, and model availability.
func (p *OpenAICompatibleProvider) TestConnection(ctx context.Context) error {
	_, err := p.ChatCompletion(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "Reply with ok."},
		},
		MaxTokens: 16,
	})
	return err
}

// OrganizeCourse requests a structured metadata organization proposal without mutating any course state.
func (p *OpenAICompatibleProvider) OrganizeCourse(ctx context.Context, input CourseOrganizeInput) (*CourseOrganizeProposal, error) {
	sanitized := applySubtitleBudget(input, p.cfg.MaxSubtitleChars, DefaultPerLessonSubtitleChars)

	userPayload, err := json.Marshal(sanitized)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal organize input: %w", err)
	}

	temp := 0.2
	rawContent, err := p.ChatCompletion(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: organizeSystemPrompt},
			{Role: "user", Content: string(userPayload)},
		},
		Temperature: &temp,
	})
	if err != nil {
		return nil, err
	}

	cleaned := stripMarkdownJSONFence(rawContent)
	var prop CourseOrganizeProposal
	if err := json.Unmarshal([]byte(cleaned), &prop); err != nil {
		return nil, fmt.Errorf("ai: parse proposal JSON: %w", err)
	}

	normalizeProposalText(&prop)
	if err := ValidateProposal(input, &prop); err != nil {
		return nil, err
	}

	return &prop, nil
}

func formatUpstreamError(body []byte) string {
	var errEnv struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errEnv); err == nil && errEnv.Error != nil && strings.TrimSpace(errEnv.Error.Message) != "" {
		return strings.TrimSpace(errEnv.Error.Message)
	}
	snippet := strings.TrimSpace(string(body))
	if snippet == "" {
		return "empty response body"
	}
	truncated, _ := truncateRunes(snippet, maxErrorSnippetRunes)
	return truncated
}

func applySubtitleBudget(input CourseOrganizeInput, totalLimit, perLessonLimit int) CourseOrganizeInput {
	out := CourseOrganizeInput{
		CourseID:       input.CourseID,
		CurrentTitle:   input.CurrentTitle,
		SourceRootPath: input.SourceRootPath,
		Overview:       input.Overview,
		Chapters:       make([]OrganizeChapterInput, len(input.Chapters)),
	}

	remaining := totalLimit
	for ci, ch := range input.Chapters {
		chCopy := OrganizeChapterInput{
			ChapterID: ch.ChapterID,
			Title:     ch.Title,
			Lessons:   make([]OrganizeLessonInput, len(ch.Lessons)),
		}
		for li, l := range ch.Lessons {
			lCopy := l
			excerpt := strings.TrimSpace(l.SubtitleExcerpt)
			if excerpt == "" || remaining <= 0 {
				lCopy.SubtitleExcerpt = ""
			} else {
				limit := perLessonLimit
				if remaining < limit {
					limit = remaining
				}
				var used int
				lCopy.SubtitleExcerpt, used = truncateRunes(excerpt, limit)
				remaining -= used
			}
			chCopy.Lessons[li] = lCopy
		}
		out.Chapters[ci] = chCopy
	}
	return out
}

func truncateRunes(s string, maxRunes int) (string, int) {
	if maxRunes <= 0 || s == "" {
		return "", 0
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s, utf8.RuneCountInString(s)
	}
	count := 0
	for idx := range s {
		if count == maxRunes {
			return s[:idx], maxRunes
		}
		count++
	}
	return s, count
}

func stripMarkdownJSONFence(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "{") {
		return s
	}
	if start := strings.Index(s, "```"); start != -1 {
		afterOpen := s[start+3:]
		if nl := strings.IndexByte(afterOpen, '\n'); nl != -1 {
			body := afterOpen[nl+1:]
			if end := strings.LastIndex(body, "```"); end != -1 {
				return strings.TrimSpace(body[:end])
			}
		}
	}
	if first := strings.IndexByte(s, '{'); first != -1 {
		if last := strings.LastIndexByte(s, '}'); last > first {
			return strings.TrimSpace(s[first : last+1])
		}
	}
	return s
}

func normalizeProposalText(prop *CourseOrganizeProposal) {
	prop.Title = strings.TrimSpace(prop.Title)
	prop.Overview = strings.TrimSpace(prop.Overview)

	cleanTags := make([]string, 0, len(prop.Tags))
	for _, tag := range prop.Tags {
		if t := strings.TrimSpace(tag); t != "" {
			cleanTags = append(cleanTags, t)
		}
	}
	prop.Tags = cleanTags

	for ci := range prop.Chapters {
		prop.Chapters[ci].Title = strings.TrimSpace(prop.Chapters[ci].Title)
		for li := range prop.Chapters[ci].Lessons {
			prop.Chapters[ci].Lessons[li].Title = strings.TrimSpace(prop.Chapters[ci].Lessons[li].Title)
		}
	}
}
