package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	lessonSummarySystemPrompt = `You are an expert educational content analyst and note-taking assistant for CourseFin.
Given a course lesson's metadata, duration, timestamp availability (has_timestamps), and transcript, produce a two-part structured lesson summary suitable for previewing the lesson and copying into study notes.

Respond with STRICTLY valid JSON only (no markdown code fences, no extra commentary) matching this exact schema:
{
  "overview": "1~2 句话概括整节课核心内容与学习目标",
  "outline": [
    {
      "title": "模块标题",
      "summary": "1~2 句话说明本模块核心内容",
      "start_sec": 0
    }
  ],
  "timeline": [
    {
      "start_sec": 0,
      "end_sec": 315,
      "title": "段落主题",
      "summary": "该时间段内讲解内容概括",
      "key_points": ["核心知识点 1", "关键命令或步骤 2"]
    }
  ]
}

Rules:
1. Language: Write all prose, titles, summaries, and key_points in clear, concise Chinese (中文), while preserving English technical terms, function/symbol names, CLI commands, flags, and code identifiers verbatim.
2. Outline: Extract 3 to 8 logical modules that cover the whole lesson concisely so a learner can scan it in 30 seconds.
3. Timeline: Segment the entire lesson strictly in chronological order from beginning to end without skipping or truncating later sections. Each section must include a concise title, summary, and actionable key_points for notes.
4. Timestamps:
   - When has_timestamps is true: set start_sec and end_sec to non-negative integer seconds within [0, duration_sec] (when duration_sec > 0), with end_sec >= start_sec and timeline sections ordered chronologically.
   - When has_timestamps is false: set ALL "start_sec" and "end_sec" fields in both outline and timeline to null, and divide the content sequentially in the order it appears in the transcript.`

	lessonSummaryChunkSystemPrompt = `You are an expert educational content analyst and note-taking assistant for CourseFin.
You are processing ONE sequential chunk (chunk_index of chunk_total) of a long lesson transcript. Produce chronological timeline sections covering EVERY topic discussed in this chunk.

Respond with STRICTLY valid JSON only (no markdown code fences, no extra commentary) matching this exact schema:
{
  "timeline": [
    {
      "start_sec": 0,
      "end_sec": 315,
      "title": "段落主题",
      "summary": "该时间段内讲解内容概括",
      "key_points": ["核心知识点 1", "关键命令或步骤 2"]
    }
  ]
}

Rules:
1. Language: Write all prose, titles, summaries, and key_points in clear, concise Chinese (中文), while preserving English technical terms, function/symbol names, CLI commands, flags, and code identifiers verbatim.
2. Completeness: Cover the entire chunk chronologically from start to end without omitting later topics. Each section needs a clear title, summary, and concise key_points.
3. Timestamps:
   - When has_timestamps is true: set start_sec and end_sec to non-negative integer seconds within [0, duration_sec] (when duration_sec > 0), with end_sec >= start_sec.
   - When has_timestamps is false: set ALL "start_sec" and "end_sec" fields to null and order sections sequentially by appearance.`

	lessonOutlineSynthSystemPrompt = `You are an expert educational content analyst and note-taking assistant for CourseFin.
Given the complete merged chronological timeline sections of a long lesson, synthesize a concise lesson overview and a 3 to 8 item high-level outline covering the entire lesson.

Respond with STRICTLY valid JSON only (no markdown code fences, no extra commentary) matching this exact schema:
{
  "overview": "1~2 句话概括整节课核心内容与学习目标",
  "outline": [
    {
      "title": "模块标题",
      "summary": "1~2 句话说明本模块核心内容",
      "start_sec": 0
    }
  ]
}

Rules:
1. Language: Write all prose, titles, and summaries in clear, concise Chinese (中文), while preserving English technical terms, function/symbol names, CLI commands, flags, and code identifiers verbatim.
2. Outline: Distill 3 to 8 logical modules covering the entire lesson cleanly so a learner can scan it in 30 seconds.
3. Timestamps:
   - When has_timestamps is true: set each outline item's start_sec to the corresponding section's integer start_sec within [0, duration_sec] (when duration_sec > 0).
   - When has_timestamps is false: set ALL "start_sec" fields to null.`
)

// OutlineItem represents a high-level module in a lesson's AI summary outline.
type OutlineItem struct {
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	StartSec *int   `json:"start_sec"`
}

// TimelineSection represents a chronological segment of a lesson's AI summary.
type TimelineSection struct {
	StartSec  *int     `json:"start_sec"`
	EndSec    *int     `json:"end_sec"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	KeyPoints []string `json:"key_points"`
}

// LessonSummaryInput is the prompt payload for single-pass lesson summarization.
type LessonSummaryInput struct {
	CourseTitle   string `json:"course_title"`
	ChapterTitle  string `json:"chapter_title"`
	LessonTitle   string `json:"lesson_title"`
	DurationSec   int    `json:"duration_sec"`
	HasTimestamps bool   `json:"has_timestamps"`
	Transcript    string `json:"transcript"`
}

// LessonSummaryOutput is the validated two-part structured summary for a lesson.
type LessonSummaryOutput struct {
	Overview string            `json:"overview"`
	Outline  []OutlineItem     `json:"outline"`
	Timeline []TimelineSection `json:"timeline"`
}

// LessonChunkInput is the prompt payload for the Map stage of long-lesson summarization.
type LessonChunkInput struct {
	CourseTitle   string `json:"course_title"`
	ChapterTitle  string `json:"chapter_title"`
	LessonTitle   string `json:"lesson_title"`
	ChunkIndex    int    `json:"chunk_index"`
	ChunkTotal    int    `json:"chunk_total"`
	DurationSec   int    `json:"duration_sec"`
	HasTimestamps bool   `json:"has_timestamps"`
	Transcript    string `json:"transcript"`
}

// LessonOutlineSynthInput is the prompt payload for the Reduce stage synthesizing overview and outline.
type LessonOutlineSynthInput struct {
	CourseTitle   string            `json:"course_title"`
	ChapterTitle  string            `json:"chapter_title"`
	LessonTitle   string            `json:"lesson_title"`
	DurationSec   int               `json:"duration_sec"`
	HasTimestamps bool              `json:"has_timestamps"`
	Timeline      []TimelineSection `json:"timeline"`
}

// SummarizeLesson generates a complete structured lesson summary in a single LLM call.
func (p *OpenAICompatibleProvider) SummarizeLesson(ctx context.Context, input LessonSummaryInput) (*LessonSummaryOutput, error) {
	if strings.TrimSpace(input.Transcript) == "" {
		return nil, errors.New("ai: lesson transcript must not be empty")
	}

	var out LessonSummaryOutput
	if err := p.runJSONPrompt(ctx, lessonSummarySystemPrompt, input, &out); err != nil {
		return nil, fmt.Errorf("ai: summarize lesson: %w", err)
	}
	if err := ValidateAndNormalizeSummary(&out, input.DurationSec, input.HasTimestamps); err != nil {
		return nil, err
	}
	return &out, nil
}

// SummarizeLessonChunk generates chronological timeline sections for one transcript chunk
// during the Map phase of long-lesson summarization.
func (p *OpenAICompatibleProvider) SummarizeLessonChunk(ctx context.Context, input LessonChunkInput) ([]TimelineSection, error) {
	if strings.TrimSpace(input.Transcript) == "" {
		return nil, errors.New("ai: lesson chunk transcript must not be empty")
	}

	var resp struct {
		Timeline []TimelineSection `json:"timeline"`
	}
	if err := p.runJSONPrompt(ctx, lessonSummaryChunkSystemPrompt, input, &resp); err != nil {
		return nil, fmt.Errorf("ai: summarize lesson chunk: %w", err)
	}
	return ValidateAndNormalizeTimelineChunk(resp.Timeline, input.DurationSec, input.HasTimestamps)
}

// SynthesizeLessonOutline distills the lesson overview and high-level outline from merged
// chronological timeline sections during the Reduce phase.
func (p *OpenAICompatibleProvider) SynthesizeLessonOutline(ctx context.Context, input LessonOutlineSynthInput) (string, []OutlineItem, error) {
	if len(input.Timeline) == 0 {
		return "", nil, errors.New("ai: lesson timeline must not be empty")
	}

	var resp struct {
		Overview string        `json:"overview"`
		Outline  []OutlineItem `json:"outline"`
	}
	if err := p.runJSONPrompt(ctx, lessonOutlineSynthSystemPrompt, input, &resp); err != nil {
		return "", nil, fmt.Errorf("ai: synthesize lesson outline: %w", err)
	}
	return ValidateAndNormalizeOutline(resp.Overview, resp.Outline, input.DurationSec, input.HasTimestamps)
}

func (p *OpenAICompatibleProvider) runJSONPrompt(ctx context.Context, systemPrompt string, input any, target any) error {
	userPayload, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal prompt input: %w", err)
	}

	temp := 0.2
	rawContent, err := p.ChatCompletion(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(userPayload)},
		},
		Temperature: &temp,
	})
	if err != nil {
		return err
	}

	cleaned := stripMarkdownJSONFence(rawContent)
	if err := json.Unmarshal([]byte(cleaned), target); err != nil {
		return fmt.Errorf("parse JSON response: %w", err)
	}
	return nil
}

// ValidateAndNormalizeSummary validates and normalizes a complete lesson summary output:
// trimming whitespace, dropping empty key points, requiring non-empty outline and timeline,
// clamping and sorting timestamps when hasTimestamps is true, or clearing all timestamps to nil otherwise.
func ValidateAndNormalizeSummary(out *LessonSummaryOutput, durationSec int, hasTimestamps bool) error {
	if out == nil {
		return errors.New("ai: lesson summary is nil")
	}

	overview, outline, err := ValidateAndNormalizeOutline(out.Overview, out.Outline, durationSec, hasTimestamps)
	if err != nil {
		return err
	}
	timeline, err := ValidateAndNormalizeTimelineChunk(out.Timeline, durationSec, hasTimestamps)
	if err != nil {
		return err
	}

	out.Overview = overview
	out.Outline = outline
	out.Timeline = timeline
	return nil
}

// ValidateAndNormalizeOutline trims and validates the overview and outline items,
// clamping start_sec when hasTimestamps is true or setting it to nil when false.
func ValidateAndNormalizeOutline(overview string, outline []OutlineItem, durationSec int, hasTimestamps bool) (string, []OutlineItem, error) {
	cleanedOverview := strings.TrimSpace(overview)
	cleaned := make([]OutlineItem, 0, len(outline))

	for _, item := range outline {
		title := strings.TrimSpace(item.Title)
		summary := strings.TrimSpace(item.Summary)
		if title == "" && summary == "" {
			continue
		}

		var startPtr *int
		if hasTimestamps && item.StartSec != nil {
			v := clampTimestampSec(*item.StartSec, durationSec)
			startPtr = &v
		}

		cleaned = append(cleaned, OutlineItem{
			Title:    title,
			Summary:  summary,
			StartSec: startPtr,
		})
	}

	if len(cleaned) == 0 {
		return "", nil, errors.New("ai: lesson summary outline must not be empty")
	}
	return cleanedOverview, cleaned, nil
}

// ValidateAndNormalizeTimelineChunk trims and validates timeline sections, removing empty
// key_points, clamping/fixing inverted timestamps and stably sorting chronologically when
// hasTimestamps is true, or clearing all timestamps to nil while preserving order when false.
func ValidateAndNormalizeTimelineChunk(sections []TimelineSection, durationSec int, hasTimestamps bool) ([]TimelineSection, error) {
	cleaned := make([]TimelineSection, 0, len(sections))

	for _, sec := range sections {
		title := strings.TrimSpace(sec.Title)
		summary := strings.TrimSpace(sec.Summary)

		keyPoints := make([]string, 0, len(sec.KeyPoints))
		for _, kp := range sec.KeyPoints {
			if trimmed := strings.TrimSpace(kp); trimmed != "" {
				keyPoints = append(keyPoints, trimmed)
			}
		}

		if title == "" && summary == "" && len(keyPoints) == 0 {
			continue
		}

		var startPtr, endPtr *int
		if hasTimestamps {
			if sec.StartSec != nil {
				v := clampTimestampSec(*sec.StartSec, durationSec)
				startPtr = &v
			}
			if sec.EndSec != nil {
				v := clampTimestampSec(*sec.EndSec, durationSec)
				endPtr = &v
			}
			if startPtr != nil && endPtr != nil && *endPtr < *startPtr {
				v := *startPtr
				endPtr = &v
			}
		}

		cleaned = append(cleaned, TimelineSection{
			StartSec:  startPtr,
			EndSec:    endPtr,
			Title:     title,
			Summary:   summary,
			KeyPoints: keyPoints,
		})
	}

	if len(cleaned) == 0 {
		return nil, errors.New("ai: lesson summary timeline must not be empty")
	}

	if hasTimestamps {
		sort.SliceStable(cleaned, func(i, j int) bool {
			a, b := cleaned[i].StartSec, cleaned[j].StartSec
			switch {
			case a != nil && b != nil:
				return *a < *b
			case a != nil && b == nil:
				return true
			default:
				return false
			}
		})
	}

	return cleaned, nil
}

func clampTimestampSec(v, durationSec int) int {
	if v < 0 {
		v = 0
	}
	if durationSec > 0 && v > durationSec {
		v = durationSec
	}
	return v
}
