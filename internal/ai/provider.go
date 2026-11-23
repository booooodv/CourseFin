package ai

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	// DefaultMaxSubtitleChars is the default total subtitle character budget across a course prompt.
	DefaultMaxSubtitleChars = 4000
	// DefaultPerLessonSubtitleChars is the maximum subtitle characters sent per lesson.
	DefaultPerLessonSubtitleChars = 200
)

// Config configures an AI provider endpoint.
type Config struct {
	BaseURL          string `json:"base_url"`
	APIKey           string `json:"api_key"`
	Model            string `json:"model"`
	MaxSubtitleChars int    `json:"max_subtitle_chars"`
}

// ChatMessage represents a single role/content message in a chat completion request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a generic chat completion request.
type ChatRequest struct {
	Model       string        `json:"model,omitempty"`
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// OrganizeLessonInput describes an existing lesson passed to AI course organization.
type OrganizeLessonInput struct {
	LessonID        string `json:"lesson_id"`
	Title           string `json:"title"`
	FileName        string `json:"file_name,omitempty"`
	RelativePath    string `json:"relative_path,omitempty"`
	SubtitleExcerpt string `json:"subtitle_excerpt,omitempty"`
}

// OrganizeChapterInput describes an existing chapter and its lessons passed to AI course organization.
type OrganizeChapterInput struct {
	ChapterID string                `json:"chapter_id,omitempty"`
	Title     string                `json:"title"`
	Lessons   []OrganizeLessonInput `json:"lessons"`
}

// CourseOrganizeInput is the read-only snapshot sent to the AI provider to generate a metadata proposal.
type CourseOrganizeInput struct {
	CourseID       string                 `json:"course_id,omitempty"`
	CurrentTitle   string                 `json:"current_title"`
	SourceRootPath string                 `json:"source_root_path,omitempty"`
	Overview       string                 `json:"overview,omitempty"`
	Chapters       []OrganizeChapterInput `json:"chapters"`
}

// ProposalLesson is a proposed lesson entry referencing an existing lesson_id verbatim.
type ProposalLesson struct {
	LessonID string `json:"lesson_id"`
	Title    string `json:"title"`
}

// ProposalChapter is a proposed chapter grouping and its ordered lessons.
type ProposalChapter struct {
	Title   string           `json:"title"`
	Lessons []ProposalLesson `json:"lessons"`
}

// CourseOrganizeProposal is the preview-only metadata proposal returned by an AI provider.
type CourseOrganizeProposal struct {
	Title    string            `json:"title"`
	Overview string            `json:"overview"`
	Tags     []string          `json:"tags"`
	Chapters []ProposalChapter `json:"chapters"`
}

// Provider defines CourseFin's pluggable AI provider interface.
type Provider interface {
	TestConnection(ctx context.Context) error
	OrganizeCourse(ctx context.Context, input CourseOrganizeInput) (*CourseOrganizeProposal, error)
	ChatCompletion(ctx context.Context, req ChatRequest) (string, error)
}

// ValidateProposal ensures a proposal has non-empty course/chapter/lesson titles
// and accounts for every input lesson_id exactly once without unknown or duplicate IDs.
func ValidateProposal(input CourseOrganizeInput, prop *CourseOrganizeProposal) error {
	if prop == nil {
		return errors.New("ai: proposal is nil")
	}
	if strings.TrimSpace(prop.Title) == "" {
		return errors.New("ai: proposal title must not be empty")
	}
	if len(prop.Chapters) == 0 {
		return errors.New("ai: proposal chapters must not be empty")
	}

	expected := make(map[string]struct{})
	for _, ch := range input.Chapters {
		for _, l := range ch.Lessons {
			if l.LessonID == "" {
				return errors.New("ai: input contains empty lesson_id")
			}
			if _, dup := expected[l.LessonID]; dup {
				return fmt.Errorf("ai: input contains duplicate lesson_id %q", l.LessonID)
			}
			expected[l.LessonID] = struct{}{}
		}
	}

	seen := make(map[string]struct{}, len(expected))
	for ci, ch := range prop.Chapters {
		if strings.TrimSpace(ch.Title) == "" {
			return fmt.Errorf("ai: proposal chapter %d title must not be empty", ci+1)
		}
		if len(ch.Lessons) == 0 {
			return fmt.Errorf("ai: proposal chapter %d must contain at least one lesson", ci+1)
		}
		for li, l := range ch.Lessons {
			if l.LessonID == "" {
				return fmt.Errorf("ai: proposal chapter %d lesson %d has empty lesson_id", ci+1, li+1)
			}
			if strings.TrimSpace(l.Title) == "" {
				return fmt.Errorf("ai: proposal lesson %q title must not be empty", l.LessonID)
			}
			if _, ok := expected[l.LessonID]; !ok {
				return fmt.Errorf("ai: proposal contains unknown lesson_id %q", l.LessonID)
			}
			if _, dup := seen[l.LessonID]; dup {
				return fmt.Errorf("ai: proposal contains duplicate lesson_id %q", l.LessonID)
			}
			seen[l.LessonID] = struct{}{}
		}
	}

	var missing []string
	for id := range expected {
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("ai: proposal is missing lesson_id(s): %s", strings.Join(missing, ", "))
	}

	return nil
}
