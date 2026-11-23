package ai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"coursefin/internal/ai"
)

var _ ai.Provider = (*ai.OpenAICompatibleProvider)(nil)

func TestOrganizeCourseParsesValidJSONAndMarkdownFence(t *testing.T) {
	input := ai.CourseOrganizeInput{
		CourseID:       "course-1",
		CurrentTitle:   "00_Go_Course_2026_Raw",
		SourceRootPath: "/courses/go-2026",
		Chapters: []ai.OrganizeChapterInput{
			{
				ChapterID: "ch-1",
				Title:     "第 1 章",
				Lessons: []ai.OrganizeLessonInput{
					{
						LessonID:     "lesson-aaa",
						Title:        "01_intro_final_v2",
						FileName:     "01_intro_final_v2.mp4",
						RelativePath: "01/01_intro_final_v2.mp4",
					},
					{
						LessonID:     "lesson-bbb",
						Title:        "02_setup_go_env",
						FileName:     "02_setup_go_env.mp4",
						RelativePath: "01/02_setup_go_env.mp4",
					},
				},
			},
		},
	}

	proposalJSON := `{
		"title": "Go 语言系统精讲",
		"overview": "从零开始系统学习 Go 语言核心语法与工程实践。",
		"tags": ["Go", "后端开发", "系统编程"],
		"chapters": [
			{
				"title": "第一章：入门与环境搭建",
				"lessons": [
					{"lesson_id": "lesson-aaa", "title": "课程介绍"},
					{"lesson_id": "lesson-bbb", "title": "Go 开发环境搭建"}
				]
			}
		]
	}`

	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "raw json",
			content: proposalJSON,
		},
		{
			name:    "json markdown fence",
			content: "```json\n" + proposalJSON + "\n```",
		},
		{
			name:    "plain markdown fence",
			content: "```\n" + proposalJSON + "\n```",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuth string
			var gotPath string
			var gotBody struct {
				Model    string           `json:"model"`
				Messages []ai.ChatMessage `json:"messages"`
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("expected POST, got %s", r.Method)
				}
				gotAuth = r.Header.Get("Authorization")
				gotPath = r.URL.Path
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := json.Unmarshal(raw, &gotBody); err != nil {
					t.Fatalf("unmarshal request body: %v", err)
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "chatcmpl-123",
					"choices": []map[string]any{
						{
							"index": 0,
							"message": map[string]any{
								"role":    "assistant",
								"content": tc.content,
							},
						},
					},
				})
			}))
			defer srv.Close()

			provider := ai.NewOpenAICompatibleProvider(ai.Config{
				BaseURL: srv.URL + "/v1",
				APIKey:  "sk-test-secret",
				Model:   "gpt-4o-mini",
			}, srv.Client())

			prop, err := provider.OrganizeCourse(context.Background(), input)
			if err != nil {
				t.Fatalf("OrganizeCourse returned error: %v", err)
			}

			if gotAuth != "Bearer sk-test-secret" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sk-test-secret")
			}
			if gotPath != "/v1/chat/completions" {
				t.Errorf("request path = %q, want %q", gotPath, "/v1/chat/completions")
			}
			if gotBody.Model != "gpt-4o-mini" {
				t.Errorf("request model = %q, want %q", gotBody.Model, "gpt-4o-mini")
			}
			if len(gotBody.Messages) < 2 {
				t.Fatalf("expected >= 2 messages (system + user), got %d", len(gotBody.Messages))
			}
			if gotBody.Messages[0].Role != "system" {
				t.Errorf("first message role = %q, want system", gotBody.Messages[0].Role)
			}
			if !strings.Contains(gotBody.Messages[1].Content, "lesson-aaa") || !strings.Contains(gotBody.Messages[1].Content, "lesson-bbb") {
				t.Errorf("user message does not contain input lesson IDs: %s", gotBody.Messages[1].Content)
			}

			if prop.Title != "Go 语言系统精讲" {
				t.Errorf("Title = %q, want %q", prop.Title, "Go 语言系统精讲")
			}
			if prop.Overview != "从零开始系统学习 Go 语言核心语法与工程实践。" {
				t.Errorf("Overview = %q", prop.Overview)
			}
			if len(prop.Tags) != 3 || prop.Tags[0] != "Go" {
				t.Errorf("Tags = %#v", prop.Tags)
			}
			if len(prop.Chapters) != 1 || len(prop.Chapters[0].Lessons) != 2 {
				t.Fatalf("unexpected chapters/lessons: %#v", prop.Chapters)
			}
			if prop.Chapters[0].Lessons[0].LessonID != "lesson-aaa" || prop.Chapters[0].Lessons[0].Title != "课程介绍" {
				t.Errorf("unexpected lesson 0: %#v", prop.Chapters[0].Lessons[0])
			}
			if prop.Chapters[0].Lessons[1].LessonID != "lesson-bbb" || prop.Chapters[0].Lessons[1].Title != "Go 开发环境搭建" {
				t.Errorf("unexpected lesson 1: %#v", prop.Chapters[0].Lessons[1])
			}
		})
	}
}

func TestValidateProposalRejectsMissingOrUnknownLessonID(t *testing.T) {
	input := ai.CourseOrganizeInput{
		CurrentTitle: "Test Course",
		Chapters: []ai.OrganizeChapterInput{
			{
				Title: "Ch 1",
				Lessons: []ai.OrganizeLessonInput{
					{LessonID: "id-1", Title: "L1"},
					{LessonID: "id-2", Title: "L2"},
				},
			},
		},
	}

	t.Run("missing lesson_id", func(t *testing.T) {
		prop := &ai.CourseOrganizeProposal{
			Title: "Cleaned Title",
			Chapters: []ai.ProposalChapter{
				{
					Title: "Chapter 1",
					Lessons: []ai.ProposalLesson{
						{LessonID: "id-1", Title: "Lesson 1"},
					},
				},
			},
		}
		err := ai.ValidateProposal(input, prop)
		if err == nil {
			t.Fatal("expected error for missing lesson_id, got nil")
		}
		if !strings.Contains(err.Error(), "id-2") && !strings.Contains(strings.ToLower(err.Error()), "missing") {
			t.Errorf("error %q should describe missing lesson_id", err.Error())
		}
	})

	t.Run("unknown lesson_id", func(t *testing.T) {
		prop := &ai.CourseOrganizeProposal{
			Title: "Cleaned Title",
			Chapters: []ai.ProposalChapter{
				{
					Title: "Chapter 1",
					Lessons: []ai.ProposalLesson{
						{LessonID: "id-1", Title: "Lesson 1"},
						{LessonID: "id-2", Title: "Lesson 2"},
						{LessonID: "id-fabricated", Title: "Fake Lesson"},
					},
				},
			},
		}
		err := ai.ValidateProposal(input, prop)
		if err == nil {
			t.Fatal("expected error for unknown lesson_id, got nil")
		}
		if !strings.Contains(err.Error(), "id-fabricated") && !strings.Contains(strings.ToLower(err.Error()), "unknown") {
			t.Errorf("error %q should describe unknown lesson_id", err.Error())
		}
	})

	t.Run("duplicate lesson_id", func(t *testing.T) {
		prop := &ai.CourseOrganizeProposal{
			Title: "Cleaned Title",
			Chapters: []ai.ProposalChapter{
				{
					Title: "Chapter 1",
					Lessons: []ai.ProposalLesson{
						{LessonID: "id-1", Title: "Lesson 1"},
						{LessonID: "id-1", Title: "Lesson 1 Again"},
						{LessonID: "id-2", Title: "Lesson 2"},
					},
				},
			},
		}
		err := ai.ValidateProposal(input, prop)
		if err == nil {
			t.Fatal("expected error for duplicate lesson_id, got nil")
		}
		if !strings.Contains(err.Error(), "id-1") && !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			t.Errorf("error %q should describe duplicate lesson_id", err.Error())
		}
	})

	t.Run("empty title or chapters or lesson title", func(t *testing.T) {
		badProposals := []*ai.CourseOrganizeProposal{
			nil,
			{
				Title: "   ",
				Chapters: []ai.ProposalChapter{
					{Title: "Ch", Lessons: []ai.ProposalLesson{{LessonID: "id-1", Title: "L1"}, {LessonID: "id-2", Title: "L2"}}},
				},
			},
			{
				Title:    "Title",
				Chapters: nil,
			},
			{
				Title: "Title",
				Chapters: []ai.ProposalChapter{
					{Title: "  ", Lessons: []ai.ProposalLesson{{LessonID: "id-1", Title: "L1"}, {LessonID: "id-2", Title: "L2"}}},
				},
			},
			{
				Title: "Title",
				Chapters: []ai.ProposalChapter{
					{Title: "Ch", Lessons: []ai.ProposalLesson{{LessonID: "id-1", Title: ""}, {LessonID: "id-2", Title: "L2"}}},
				},
			},
		}
		for i, prop := range badProposals {
			if err := ai.ValidateProposal(input, prop); err == nil {
				t.Errorf("case %d: expected error, got nil", i)
			}
		}
	})

	t.Run("valid proposal across multiple chapters", func(t *testing.T) {
		prop := &ai.CourseOrganizeProposal{
			Title: "Cleaned Title",
			Chapters: []ai.ProposalChapter{
				{Title: "Part A", Lessons: []ai.ProposalLesson{{LessonID: "id-2", Title: "Lesson Two"}}},
				{Title: "Part B", Lessons: []ai.ProposalLesson{{LessonID: "id-1", Title: "Lesson One"}}},
			},
		}
		if err := ai.ValidateProposal(input, prop); err != nil {
			t.Fatalf("expected valid proposal, got error: %v", err)
		}
	})
}

func TestTestConnectionReportsUpstreamErrors(t *testing.T) {
	t.Run("200 OK passes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": "ok"}},
				},
			})
		}))
		defer srv.Close()

		p := ai.NewOpenAICompatibleProvider(ai.Config{
			BaseURL: srv.URL,
			APIKey:  "sk-ok",
			Model:   "gpt-4o-mini",
		}, srv.Client())

		if err := p.TestConnection(context.Background()); err != nil {
			t.Fatalf("expected nil error on 200, got: %v", err)
		}
	})

	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"message": "upstream failure",
					},
				})
			}))
			defer srv.Close()

			p := ai.NewOpenAICompatibleProvider(ai.Config{
				BaseURL: srv.URL,
				APIKey:  "sk-bad",
				Model:   "gpt-4o-mini",
			}, srv.Client())

			err := p.TestConnection(context.Background())
			if err == nil {
				t.Fatalf("expected error for status %d, got nil", status)
			}
			wantCode := "401"
			if status == http.StatusInternalServerError {
				wantCode = "500"
			}
			if !strings.Contains(err.Error(), wantCode) {
				t.Errorf("error %q does not include status code %s", err.Error(), wantCode)
			}
		})
	}
}

func TestBaseURLNormalization(t *testing.T) {
	urls := []struct {
		raw      string
		wantPath string
	}{
		{raw: "/v1", wantPath: "/v1/chat/completions"},
		{raw: "/v1/", wantPath: "/v1/chat/completions"},
		{raw: "/v1///", wantPath: "/v1/chat/completions"},
		{raw: "/v1/chat/completions", wantPath: "/v1/chat/completions"},
		{raw: "/v1/chat/completions/", wantPath: "/v1/chat/completions"},
		{raw: "", wantPath: "/chat/completions"},
	}

	for _, tc := range urls {
		t.Run(tc.raw, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []map[string]any{
						{"message": map[string]any{"role": "assistant", "content": "pong"}},
					},
				})
			}))
			defer srv.Close()

			p := ai.NewOpenAICompatibleProvider(ai.Config{
				BaseURL: "  " + srv.URL + tc.raw + "  ",
				Model:   "test-model",
			}, srv.Client())

			if err := p.TestConnection(context.Background()); err != nil {
				t.Fatalf("TestConnection error: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("got path %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

func TestSubtitleExcerptTruncationAndDefaultClient(t *testing.T) {
	// Default httpClient fallback and default MaxSubtitleChars = 4000
	pDefault := ai.NewOpenAICompatibleProvider(ai.Config{
		BaseURL: "http://127.0.0.1:0/v1",
		Model:   "m",
	}, nil)
	if pDefault == nil {
		t.Fatal("expected non-nil provider")
	}

	longZh := strings.Repeat("中", 350) // 350 runes > 200 per-lesson cap
	longEn := strings.Repeat("a", 300)

	var capturedUserPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		capturedUserPrompt = body.Messages[1].Content

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"title":"Course","overview":"Summary","tags":["Go"],"chapters":[{"title":"Ch1","lessons":[{"lesson_id":"l1","title":"L1"},{"lesson_id":"l2","title":"L2"},{"lesson_id":"l3","title":"L3"}]}]}`,
					},
				},
			},
		})
	}))
	defer srv.Close()

	// Budget 250 total: l1 gets 200 runes, l2 gets 50 runes, l3 gets 0 runes.
	p := ai.NewOpenAICompatibleProvider(ai.Config{
		BaseURL:          srv.URL,
		Model:            "qwen-plus",
		MaxSubtitleChars: 250,
	}, &http.Client{Timeout: 5 * time.Second})

	input := ai.CourseOrganizeInput{
		CurrentTitle: "Raw",
		Chapters: []ai.OrganizeChapterInput{
			{
				Title: "Ch1",
				Lessons: []ai.OrganizeLessonInput{
					{LessonID: "l1", Title: "1", SubtitleExcerpt: longZh},
					{LessonID: "l2", Title: "2", SubtitleExcerpt: longEn},
					{LessonID: "l3", Title: "3", SubtitleExcerpt: "should be omitted due to budget"},
				},
			},
		},
	}

	if _, err := p.OrganizeCourse(context.Background(), input); err != nil {
		t.Fatalf("OrganizeCourse: %v", err)
	}

	if !utf8.ValidString(capturedUserPrompt) {
		t.Fatal("user prompt contains invalid UTF-8 after truncation")
	}

	var sentInput ai.CourseOrganizeInput
	if err := json.Unmarshal([]byte(capturedUserPrompt), &sentInput); err != nil {
		t.Fatalf("expected user prompt to be valid JSON of CourseOrganizeInput: %v\nprompt=%s", err, capturedUserPrompt)
	}

	got1 := sentInput.Chapters[0].Lessons[0].SubtitleExcerpt
	got2 := sentInput.Chapters[0].Lessons[1].SubtitleExcerpt
	got3 := sentInput.Chapters[0].Lessons[2].SubtitleExcerpt

	if utf8.RuneCountInString(got1) != 200 {
		t.Errorf("lesson 1 excerpt rune count = %d, want 200", utf8.RuneCountInString(got1))
	}
	if utf8.RuneCountInString(got2) != 50 {
		t.Errorf("lesson 2 excerpt rune count = %d, want 50", utf8.RuneCountInString(got2))
	}
	if got3 != "" {
		t.Errorf("lesson 3 excerpt = %q, want empty when budget exhausted", got3)
	}
	// Original input must remain unmodified.
	if utf8.RuneCountInString(input.Chapters[0].Lessons[0].SubtitleExcerpt) != 350 {
		t.Error("input SubtitleExcerpt was mutated")
	}
}

func TestChatCompletionGenericPrimitive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "override-model" {
			t.Fatalf("expected override-model, got %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "Hello from AI"}},
			},
		})
	}))
	defer srv.Close()

	p := ai.NewOpenAICompatibleProvider(ai.Config{
		BaseURL: srv.URL,
		Model:   "default-model",
	}, srv.Client())

	out, err := p.ChatCompletion(context.Background(), ai.ChatRequest{
		Model:    "override-model",
		Messages: []ai.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if out != "Hello from AI" {
		t.Errorf("ChatCompletion = %q, want %q", out, "Hello from AI")
	}
}

func TestDefaultMaxSubtitleCharsAndErrorCases(t *testing.T) {
	t.Run("default MaxSubtitleChars caps total at 4000", func(t *testing.T) {
		var capturedUserPrompt string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Messages []ai.ChatMessage `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			capturedUserPrompt = body.Messages[1].Content

			// Return valid proposal preserving all 25 lesson IDs.
			var in ai.CourseOrganizeInput
			_ = json.Unmarshal([]byte(capturedUserPrompt), &in)
			var lessons []ai.ProposalLesson
			for _, l := range in.Chapters[0].Lessons {
				lessons = append(lessons, ai.ProposalLesson{LessonID: l.LessonID, Title: l.Title})
			}
			propBytes, _ := json.Marshal(ai.CourseOrganizeProposal{
				Title:    "Organized",
				Overview: "Overview",
				Tags:     []string{"Go"},
				Chapters: []ai.ProposalChapter{{Title: "Ch1", Lessons: lessons}},
			})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": string(propBytes)}},
				},
			})
		}))
		defer srv.Close()

		// 25 lessons * 200 chars = 5000 chars; default budget 4000 allows 20 lessons * 200 chars.
		lessons := make([]ai.OrganizeLessonInput, 25)
		for i := range lessons {
			lessons[i] = ai.OrganizeLessonInput{
				LessonID:        "l-" + string(rune('a'+i)),
				Title:           "Lesson",
				SubtitleExcerpt: strings.Repeat("x", 250),
			}
		}

		p := ai.NewOpenAICompatibleProvider(ai.Config{
			BaseURL: srv.URL,
			Model:   "gpt-4o-mini",
			// MaxSubtitleChars left 0 -> defaults to 4000
		}, nil)

		if _, err := p.OrganizeCourse(context.Background(), ai.CourseOrganizeInput{
			CurrentTitle: "Course",
			Chapters:     []ai.OrganizeChapterInput{{Title: "Ch1", Lessons: lessons}},
		}); err != nil {
			t.Fatalf("OrganizeCourse error: %v", err)
		}

		var sent ai.CourseOrganizeInput
		if err := json.Unmarshal([]byte(capturedUserPrompt), &sent); err != nil {
			t.Fatalf("unmarshal prompt: %v", err)
		}
		totalRunes := 0
		for _, l := range sent.Chapters[0].Lessons {
			n := utf8.RuneCountInString(l.SubtitleExcerpt)
			if n > 200 {
				t.Errorf("lesson %s excerpt runes = %d, want <= 200", l.LessonID, n)
			}
			totalRunes += n
		}
		if totalRunes != 4000 {
			t.Errorf("total subtitle runes = %d, want 4000", totalRunes)
		}
	})

	t.Run("config and upstream validation errors", func(t *testing.T) {
		pNoURL := ai.NewOpenAICompatibleProvider(ai.Config{Model: "m"}, nil)
		if err := pNoURL.TestConnection(context.Background()); err == nil {
			t.Error("expected error when BaseURL is empty")
		}

		pNoModel := ai.NewOpenAICompatibleProvider(ai.Config{BaseURL: "http://localhost/v1"}, nil)
		if err := pNoModel.TestConnection(context.Background()); err == nil {
			t.Error("expected error when Model is empty")
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": `{"title":"T","chapters":[{"title":"C","lessons":[{"lesson_id":"wrong","title":"L"}]}]}`}},
				},
			})
		}))
		defer srv.Close()

		p := ai.NewOpenAICompatibleProvider(ai.Config{BaseURL: srv.URL, Model: "m"}, srv.Client())
		_, err := p.OrganizeCourse(context.Background(), ai.CourseOrganizeInput{
			CurrentTitle: "T",
			Chapters: []ai.OrganizeChapterInput{
				{Title: "C", Lessons: []ai.OrganizeLessonInput{{LessonID: "real-id", Title: "L"}}},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "wrong") {
			t.Errorf("expected ValidateProposal error mentioning wrong ID, got: %v", err)
		}
	})
}
