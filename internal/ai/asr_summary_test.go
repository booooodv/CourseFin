package ai_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"coursefin/internal/ai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newMockHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func makeHTTPResponse(status int, contentType, body string) *http.Response {
	hdr := make(http.Header)
	if contentType != "" {
		hdr.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     hdr,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type parsedMultipart struct {
	Fields   map[string]string
	FileName string
	FileData []byte
}

func parseMultipartRequest(t *testing.T, r *http.Request) parsedMultipart {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type %q: %v", r.Header.Get("Content-Type"), err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("Content-Type = %q, want multipart/*", mediaType)
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	out := parsedMultipart{Fields: make(map[string]string)}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read multipart part: %v", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read multipart part %q body: %v", part.FormName(), err)
		}
		if part.FormName() == "file" {
			out.FileName = part.FileName()
			out.FileData = data
		} else {
			out.Fields[part.FormName()] = string(data)
		}
	}
	return out
}

func intPtr(v int) *int {
	return &v
}

func TestTranscriberVerboseJSONAndFallback(t *testing.T) {
	audioPayload := []byte("fake-mp3-audio-bytes-1234567890")

	t.Run("verbose_json success with auth and language", func(t *testing.T) {
		var gotMethod, gotPath, gotAuth string
		var gotMP parsedMultipart

		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			gotMP = parseMultipartRequest(t, r)
			return makeHTTPResponse(http.StatusOK, "application/json", `{
				"language": "zh",
				"text": "你好世界",
				"segments": [
					{"start": 0.0, "end": 3.5, "text": "你好"},
					{"start": 3.5, "end": 7.2, "text": "世界"}
				]
			}`), nil
		})

		tr := ai.NewOpenAICompatibleTranscriber(ai.ASRConfig{
			BaseURL:  "https://api.example.com/v1",
			APIKey:   "sk-asr-secret",
			Model:    "whisper-1",
			Language: "zh",
		}, client)

		res, err := tr.Transcribe(context.Background(), "chunk_000.mp3", bytes.NewReader(audioPayload))
		if err != nil {
			t.Fatalf("Transcribe error: %v", err)
		}
		if gotMethod != http.MethodPost {
			t.Errorf("method = %q, want POST", gotMethod)
		}
		if gotPath != "/v1/audio/transcriptions" {
			t.Errorf("path = %q, want /v1/audio/transcriptions", gotPath)
		}
		if gotAuth != "Bearer sk-asr-secret" {
			t.Errorf("Authorization = %q, want Bearer sk-asr-secret", gotAuth)
		}
		if gotMP.Fields["model"] != "whisper-1" {
			t.Errorf("model = %q, want whisper-1", gotMP.Fields["model"])
		}
		if gotMP.Fields["response_format"] != "verbose_json" {
			t.Errorf("response_format = %q, want verbose_json", gotMP.Fields["response_format"])
		}
		if gotMP.Fields["language"] != "zh" {
			t.Errorf("language = %q, want zh", gotMP.Fields["language"])
		}
		if gotMP.FileName != "chunk_000.mp3" {
			t.Errorf("file name = %q, want chunk_000.mp3", gotMP.FileName)
		}
		if !bytes.Equal(gotMP.FileData, audioPayload) {
			t.Errorf("uploaded file bytes mismatch")
		}
		if res.Language != "zh" {
			t.Errorf("res.Language = %q, want zh", res.Language)
		}
		if res.Text != "你好世界" {
			t.Errorf("res.Text = %q, want 你好世界", res.Text)
		}
		if len(res.Cues) != 2 {
			t.Fatalf("len(res.Cues) = %d, want 2", len(res.Cues))
		}
		if res.Cues[0].StartSec != 0.0 || res.Cues[0].EndSec != 3.5 || res.Cues[0].Text != "你好" {
			t.Errorf("cue[0] = %+v", res.Cues[0])
		}
		if res.Cues[1].StartSec != 3.5 || res.Cues[1].EndSec != 7.2 || res.Cues[1].Text != "世界" {
			t.Errorf("cue[1] = %+v", res.Cues[1])
		}
	})

	t.Run("empty api key omits authorization header and empty language omits field", func(t *testing.T) {
		var hasAuth bool
		var gotPath string
		var gotMP parsedMultipart

		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			_, hasAuth = r.Header["Authorization"]
			gotPath = r.URL.Path
			gotMP = parseMultipartRequest(t, r)
			return makeHTTPResponse(http.StatusOK, "application/json", `{"text":"auto"}`), nil
		})

		tr := ai.NewOpenAICompatibleTranscriber(ai.ASRConfig{
			BaseURL: "http://whisper.local:8000/v1/audio/transcriptions/",
			Model:   "faster-whisper-large-v3",
		}, client)

		res, err := tr.Transcribe(context.Background(), "audio.mp3", bytes.NewReader(audioPayload))
		if err != nil {
			t.Fatalf("Transcribe error: %v", err)
		}
		if hasAuth {
			t.Errorf("expected no Authorization header when APIKey is empty")
		}
		if gotPath != "/v1/audio/transcriptions" {
			t.Errorf("path = %q, want /v1/audio/transcriptions", gotPath)
		}
		if _, ok := gotMP.Fields["language"]; ok {
			t.Errorf("expected no language field when Language is empty, got %q", gotMP.Fields["language"])
		}
		if res.Text != "auto" || len(res.Cues) != 0 {
			t.Errorf("unexpected result: %+v", res)
		}
	})

	t.Run("verbose_json 400 falls back to srt and replays exact audio", func(t *testing.T) {
		var formats []string
		var payloads [][]byte

		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			mp := parseMultipartRequest(t, r)
			formats = append(formats, mp.Fields["response_format"])
			payloads = append(payloads, mp.FileData)

			if mp.Fields["response_format"] == "verbose_json" {
				return makeHTTPResponse(http.StatusBadRequest, "application/json", `{"error":{"message":"verbose_json unsupported"}}`), nil
			}
			if mp.Fields["response_format"] == "srt" {
				srt := "1\n00:00:00,000 --> 00:00:02,500\n你好\n\n2\n00:00:02,500 --> 00:00:05,000\n世界\n"
				return makeHTTPResponse(http.StatusOK, "text/plain; charset=utf-8", srt), nil
			}
			return makeHTTPResponse(http.StatusInternalServerError, "text/plain", "unexpected format"), nil
		})

		tr := ai.NewOpenAICompatibleTranscriber(ai.ASRConfig{
			BaseURL: "https://api.example.com/v1",
			Model:   "sensevoice",
		}, client)

		res, err := tr.Transcribe(context.Background(), "chunk.mp3", bytes.NewReader(audioPayload))
		if err != nil {
			t.Fatalf("Transcribe error: %v", err)
		}
		if len(formats) != 2 || formats[0] != "verbose_json" || formats[1] != "srt" {
			t.Fatalf("formats tried = %v, want [verbose_json srt]", formats)
		}
		if !bytes.Equal(payloads[0], audioPayload) || !bytes.Equal(payloads[1], audioPayload) {
			t.Errorf("audio payload was not replayed identically across retry")
		}
		if len(res.Cues) != 2 {
			t.Fatalf("len(res.Cues) = %d, want 2", len(res.Cues))
		}
		if res.Cues[0].StartSec != 0 || res.Cues[0].EndSec != 2.5 || res.Cues[0].Text != "你好" {
			t.Errorf("cue[0] = %+v", res.Cues[0])
		}
		if res.Cues[1].StartSec != 2.5 || res.Cues[1].EndSec != 5.0 || res.Cues[1].Text != "世界" {
			t.Errorf("cue[1] = %+v", res.Cues[1])
		}
	})

	t.Run("verbose_json and srt 422 fall back to json", func(t *testing.T) {
		var formats []string

		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			mp := parseMultipartRequest(t, r)
			formats = append(formats, mp.Fields["response_format"])

			switch mp.Fields["response_format"] {
			case "verbose_json", "srt":
				return makeHTTPResponse(http.StatusUnprocessableEntity, "application/json", `{"error":{"message":"unsupported format"}}`), nil
			case "json":
				return makeHTTPResponse(http.StatusOK, "application/json", `{"text":"plain transcript"}`), nil
			default:
				return makeHTTPResponse(http.StatusInternalServerError, "text/plain", "unexpected"), nil
			}
		})

		tr := ai.NewOpenAICompatibleTranscriber(ai.ASRConfig{
			BaseURL: "https://api.example.com/v1",
			Model:   "gpt-4o-mini-transcribe",
		}, client)

		res, err := tr.Transcribe(context.Background(), "chunk.mp3", bytes.NewReader(audioPayload))
		if err != nil {
			t.Fatalf("Transcribe error: %v", err)
		}
		if len(formats) != 3 || formats[0] != "verbose_json" || formats[1] != "srt" || formats[2] != "json" {
			t.Fatalf("formats tried = %v, want [verbose_json srt json]", formats)
		}
		if res.Text != "plain transcript" {
			t.Errorf("res.Text = %q, want %q", res.Text, "plain transcript")
		}
		if len(res.Cues) != 0 {
			t.Errorf("len(res.Cues) = %d, want 0", len(res.Cues))
		}
	})

	t.Run("non-400/422 errors do not retry", func(t *testing.T) {
		calls := 0
		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			calls++
			return makeHTTPResponse(http.StatusUnauthorized, "application/json", `{"error":{"message":"invalid key"}}`), nil
		})

		tr := ai.NewOpenAICompatibleTranscriber(ai.ASRConfig{
			BaseURL: "https://api.example.com/v1",
			APIKey:  "bad",
			Model:   "whisper-1",
		}, client)

		_, err := tr.Transcribe(context.Background(), "chunk.mp3", bytes.NewReader(audioPayload))
		if err == nil {
			t.Fatal("expected error on 401, got nil")
		}
		if calls != 1 {
			t.Errorf("expected 1 attempt on 401, got %d", calls)
		}
		if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid key") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestTranscriberTestConnectionSendsSilentWAV(t *testing.T) {
	t.Run("sends valid RIFF WAVE header with response_format=json and accepts empty text", func(t *testing.T) {
		var gotMP parsedMultipart
		var gotPath string

		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			gotPath = r.URL.Path
			gotMP = parseMultipartRequest(t, r)
			return makeHTTPResponse(http.StatusOK, "application/json", `{"text":""}`), nil
		})

		tr := ai.NewOpenAICompatibleTranscriber(ai.ASRConfig{
			BaseURL: "https://api.example.com/v1",
			APIKey:  "sk-ok",
			Model:   "whisper-1",
		}, client)

		if err := tr.TestConnection(context.Background()); err != nil {
			t.Fatalf("TestConnection error: %v", err)
		}
		if gotPath != "/v1/audio/transcriptions" {
			t.Errorf("path = %q, want /v1/audio/transcriptions", gotPath)
		}
		if gotMP.Fields["response_format"] != "json" {
			t.Errorf("response_format = %q, want json", gotMP.Fields["response_format"])
		}
		if gotMP.Fields["model"] != "whisper-1" {
			t.Errorf("model = %q, want whisper-1", gotMP.Fields["model"])
		}
		if len(gotMP.FileData) <= 44 {
			t.Fatalf("WAV payload too short: %d bytes", len(gotMP.FileData))
		}
		if string(gotMP.FileData[0:4]) != "RIFF" || string(gotMP.FileData[8:12]) != "WAVE" {
			t.Errorf("invalid WAV header: %q / %q", gotMP.FileData[0:4], gotMP.FileData[8:12])
		}
		sampleRate := binary.LittleEndian.Uint32(gotMP.FileData[24:28])
		channels := binary.LittleEndian.Uint16(gotMP.FileData[22:24])
		bitsPerSample := binary.LittleEndian.Uint16(gotMP.FileData[34:36])
		if sampleRate != 16000 || channels != 1 || bitsPerSample != 16 {
			t.Errorf("WAV format = %dHz %dch %dbit, want 16000Hz 1ch 16bit", sampleRate, channels, bitsPerSample)
		}
	})

	t.Run("GenerateSilentWAV produces exact PCM size", func(t *testing.T) {
		wav := ai.GenerateSilentWAV(500)
		// 500ms at 16kHz * 1ch * 2bytes = 16000 PCM bytes + 44 header = 16044 bytes
		if len(wav) != 16044 {
			t.Fatalf("len(wav) = %d, want 16044", len(wav))
		}
		riffSize := binary.LittleEndian.Uint32(wav[4:8])
		dataSize := binary.LittleEndian.Uint32(wav[40:44])
		if riffSize != 16036 || dataSize != 16000 {
			t.Errorf("sizes = riff:%d data:%d, want 16036 / 16000", riffSize, dataSize)
		}
	})
}

func TestFormatAndParseWebVTTCuesRoundTrip(t *testing.T) {
	inputCues := []ai.TranscriptCue{
		{StartSec: 1.25, EndSec: 4.5, Text: "欢迎来到 Go 课程"},
		{StartSec: 3723.45, EndSec: 3728.0, Text: "第一行内容\n第二行内容"},
	}

	vtt := ai.FormatCuesAsWebVTT(inputCues)
	if !strings.HasPrefix(vtt, "WEBVTT\n\n") {
		t.Fatalf("missing WEBVTT header:\n%s", vtt)
	}
	if !strings.Contains(vtt, "01:02:03.450 --> 01:02:08.000") {
		t.Fatalf("expected formatted >=1h timestamp in VTT:\n%s", vtt)
	}

	// Add header metadata, NOTE blocks, STYLE block, cue IDs, and cue settings to test parser resilience.
	augmentedVTT := "WEBVTT - Course Transcript\nKind: captions\nLanguage: zh\n\n" +
		"NOTE This is a comment block\nwith 00:00:01.000 --> 00:00:02.000 inside\n\n" +
		"STYLE\n::cue { color: white; }\n\n" +
		"cue-1\n00:00:01.250 --> 00:00:04.500 align:start position:10%\n欢迎来到 Go 课程\n\n" +
		"01:02:03.450 --> 01:02:08.000\n第一行内容\n第二行内容\n"

	parsed := ai.ParseSubtitleCues(augmentedVTT)
	if len(parsed) != len(inputCues) {
		t.Fatalf("len(parsed) = %d, want %d; got %+v", len(parsed), len(inputCues), parsed)
	}
	for i := range inputCues {
		if parsed[i].StartSec != inputCues[i].StartSec ||
			parsed[i].EndSec != inputCues[i].EndSec ||
			parsed[i].Text != inputCues[i].Text {
			t.Errorf("cue[%d] = %+v, want %+v", i, parsed[i], inputCues[i])
		}
	}

	// Also verify MM:SS.mmm short timestamp and SRT comma format.
	shortParsed := ai.ParseSubtitleCues("1\n01:02,500 --> 01:05.750\n短格式\n")
	if len(shortParsed) != 1 || shortParsed[0].StartSec != 62.5 || shortParsed[0].EndSec != 65.75 || shortParsed[0].Text != "短格式" {
		t.Errorf("shortParsed = %+v", shortParsed)
	}
}

func TestSummarizeLessonSinglePassAndMapReduce(t *testing.T) {
	t.Run("SummarizeLesson parses fenced JSON and builds structured output", func(t *testing.T) {
		var gotSystem, gotUser string

		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			var body struct {
				Messages    []ai.ChatMessage `json:"messages"`
				Temperature *float64         `json:"temperature"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if len(body.Messages) != 2 {
				t.Fatalf("messages count = %d, want 2", len(body.Messages))
			}
			gotSystem = body.Messages[0].Content
			gotUser = body.Messages[1].Content

			respJSON := "```json\n" + `{
				"overview": "本节课讲解 Go ` + "`context`" + ` 包的核心用法与取消传播机制。",
				"outline": [
					{"title": "Context 基础概念", "summary": "介绍取消信号与截止时间模型。", "start_sec": 0},
					{"title": "WithCancel 实战", "summary": "演示 goroutine 优雅退出。", "start_sec": 120},
					{"title": "常见误区总结", "summary": "避免将业务参数塞入 WithValue。", "start_sec": 240}
				],
				"timeline": [
					{
						"start_sec": 0,
						"end_sec": 120,
						"title": "为什么需要 Context",
						"summary": "梳理请求级取消与超时控制。",
						"key_points": ["context.Background()", "超时传播"]
					},
					{
						"start_sec": 120,
						"end_sec": 240,
						"title": "WithCancel 与 WithTimeout",
						"summary": "结合 select 监听 <-ctx.Done()。",
						"key_points": ["defer cancel()", "防止泄漏"]
					}
				]
			}` + "\n```"

			payload, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": respJSON}},
				},
			})
			return makeHTTPResponse(http.StatusOK, "application/json", string(payload)), nil
		})

		provider := ai.NewOpenAICompatibleProvider(ai.Config{
			BaseURL: "https://llm.example.com/v1",
			APIKey:  "sk-llm",
			Model:   "gpt-4o-mini",
		}, client)

		out, err := provider.SummarizeLesson(context.Background(), ai.LessonSummaryInput{
			CourseTitle:   "Go 进阶",
			ChapterTitle:  "并发编程",
			LessonTitle:   "Context 原理与实践",
			DurationSec:   300,
			HasTimestamps: true,
			Transcript:    "[00:00] 今天来看 context 包",
		})
		if err != nil {
			t.Fatalf("SummarizeLesson error: %v", err)
		}
		if !strings.Contains(gotSystem, "JSON") {
			t.Errorf("system prompt should require JSON: %q", gotSystem)
		}
		if !strings.Contains(gotUser, "Context 原理与实践") || !strings.Contains(gotUser, "[00:00] 今天来看 context 包") {
			t.Errorf("user prompt missing input context: %q", gotUser)
		}
		if len(out.Outline) != 3 || len(out.Timeline) != 2 {
			t.Fatalf("unexpected counts: outline=%d timeline=%d", len(out.Outline), len(out.Timeline))
		}
		if out.Outline[1].StartSec == nil || *out.Outline[1].StartSec != 120 {
			t.Errorf("outline[1].StartSec = %v, want 120", out.Outline[1].StartSec)
		}
		if out.Timeline[0].KeyPoints[0] != "context.Background()" {
			t.Errorf("unexpected key points: %+v", out.Timeline[0].KeyPoints)
		}
	})

	t.Run("SummarizeLessonChunk and SynthesizeLessonOutline for map-reduce", func(t *testing.T) {
		callIdx := 0
		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			callIdx++
			var body struct {
				Messages []ai.ChatMessage `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			userMsg := body.Messages[1].Content

			var content string
			if callIdx == 1 {
				if !strings.Contains(userMsg, `"chunk_index":1`) || !strings.Contains(userMsg, `"chunk_total":2`) {
					t.Fatalf("chunk request missing chunk metadata: %s", userMsg)
				}
				content = `{"timeline":[{"start_sec":0,"end_sec":600,"title":"第一部分","summary":"分块一概要","key_points":["知识点A"]}]}`
			} else {
				if !strings.Contains(userMsg, "第一部分") {
					t.Fatalf("outline synth request missing timeline input: %s", userMsg)
				}
				content = `{"overview":"全节先讲基础再讲实战。","outline":[{"title":"基础篇","summary":"讲解核心模型。","start_sec":0},{"title":"实战篇","summary":"项目案例演练。","start_sec":600}]}`
			}

			payload, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": content}},
				},
			})
			return makeHTTPResponse(http.StatusOK, "application/json", string(payload)), nil
		})

		provider := ai.NewOpenAICompatibleProvider(ai.Config{
			BaseURL: "https://llm.example.com/v1",
			Model:   "gpt-4o-mini",
		}, client)

		sections, err := provider.SummarizeLessonChunk(context.Background(), ai.LessonChunkInput{
			CourseTitle:   "分布式系统",
			ChapterTitle:  "第 1 章",
			LessonTitle:   "共识算法",
			ChunkIndex:    1,
			ChunkTotal:    2,
			DurationSec:   1200,
			HasTimestamps: true,
			Transcript:    "[00:00] 第一部分内容",
		})
		if err != nil {
			t.Fatalf("SummarizeLessonChunk error: %v", err)
		}
		if len(sections) != 1 || sections[0].Title != "第一部分" {
			t.Fatalf("unexpected chunk sections: %+v", sections)
		}

		overview, outline, err := provider.SynthesizeLessonOutline(context.Background(), ai.LessonOutlineSynthInput{
			CourseTitle:   "分布式系统",
			ChapterTitle:  "第 1 章",
			LessonTitle:   "共识算法",
			DurationSec:   1200,
			HasTimestamps: true,
			Timeline:      sections,
		})
		if err != nil {
			t.Fatalf("SynthesizeLessonOutline error: %v", err)
		}
		if overview != "全节先讲基础再讲实战。" {
			t.Errorf("overview = %q", overview)
		}
		if len(outline) != 2 || outline[1].StartSec == nil || *outline[1].StartSec != 600 {
			t.Errorf("unexpected outline: %+v", outline)
		}
	})

	t.Run("SummarizeLesson without timestamps clears LLM-hallucinated timestamps", func(t *testing.T) {
		client := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			content := `{
				"overview": "纯文稿总结",
				"outline": [{"title": "主题一", "summary": "概要", "start_sec": 42}],
				"timeline": [{"start_sec": 10, "end_sec": 50, "title": "片段一", "summary": "内容", "key_points": ["要点"]}]
			}`
			payload, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": content}},
				},
			})
			return makeHTTPResponse(http.StatusOK, "application/json", string(payload)), nil
		})

		provider := ai.NewOpenAICompatibleProvider(ai.Config{
			BaseURL: "https://llm.example.com/v1",
			Model:   "gpt-4o-mini",
		}, client)

		out, err := provider.SummarizeLesson(context.Background(), ai.LessonSummaryInput{
			CourseTitle:   "课程",
			ChapterTitle:  "章节",
			LessonTitle:   "无时间戳课时",
			HasTimestamps: false,
			Transcript:    "这是一段纯文本讲义内容。",
		})
		if err != nil {
			t.Fatalf("SummarizeLesson error: %v", err)
		}
		if out.Outline[0].StartSec != nil {
			t.Errorf("expected outline start_sec nil, got %v", *out.Outline[0].StartSec)
		}
		if out.Timeline[0].StartSec != nil || out.Timeline[0].EndSec != nil {
			t.Errorf("expected timeline timestamps nil, got (%v, %v)", out.Timeline[0].StartSec, out.Timeline[0].EndSec)
		}
	})
}

func TestValidateAndNormalizeSummaryClampsAndSortsTimestamps(t *testing.T) {
	t.Run("clamps negative and over-duration timestamps, fixes inverted end_sec, stably sorts timeline, cleans key_points", func(t *testing.T) {
		out := &ai.LessonSummaryOutput{
			Overview: "  课程概览  ",
			Outline: []ai.OutlineItem{
				{Title: " 模块一 ", Summary: " 简介 ", StartSec: intPtr(-10)},
				{Title: " 模块二 ", Summary: " 结尾 ", StartSec: intPtr(500)},
			},
			Timeline: []ai.TimelineSection{
				{StartSec: intPtr(200), EndSec: intPtr(250), Title: "第三段", Summary: " 概要3 ", KeyPoints: []string{"", "  要点3  ", "   "}},
				{StartSec: intPtr(120), EndSec: intPtr(100), Title: "第二段", Summary: "概要2", KeyPoints: nil},
				{StartSec: intPtr(-5), EndSec: intPtr(999), Title: "第一段", Summary: "概要1", KeyPoints: []string{"  要点1 "}},
				{StartSec: intPtr(10), EndSec: intPtr(50), Title: "一点五段", Summary: "概要1.5", KeyPoints: []string{}},
			},
		}

		if err := ai.ValidateAndNormalizeSummary(out, 300, true); err != nil {
			t.Fatalf("ValidateAndNormalizeSummary error: %v", err)
		}

		if out.Overview != "课程概览" {
			t.Errorf("Overview = %q, want 课程概览", out.Overview)
		}
		if *out.Outline[0].StartSec != 0 || *out.Outline[1].StartSec != 300 {
			t.Errorf("outline start_sec not clamped: %d, %d", *out.Outline[0].StartSec, *out.Outline[1].StartSec)
		}
		if out.Outline[0].Title != "模块一" || out.Outline[0].Summary != "简介" {
			t.Errorf("outline text not trimmed: %+v", out.Outline[0])
		}

		if len(out.Timeline) != 4 {
			t.Fatalf("len(Timeline) = %d, want 4", len(out.Timeline))
		}
		wantOrder := []struct {
			title string
			start int
			end   int
		}{
			{"第一段", 0, 300},
			{"一点五段", 10, 50},
			{"第二段", 120, 120},
			{"第三段", 200, 250},
		}
		for i, want := range wantOrder {
			got := out.Timeline[i]
			if got.Title != want.title {
				t.Errorf("timeline[%d].Title = %q, want %q", i, got.Title, want.title)
			}
			if got.StartSec == nil || *got.StartSec != want.start {
				t.Errorf("timeline[%d].StartSec = %v, want %d", i, got.StartSec, want.start)
			}
			if got.EndSec == nil || *got.EndSec != want.end {
				t.Errorf("timeline[%d].EndSec = %v, want %d", i, got.EndSec, want.end)
			}
		}
		if len(out.Timeline[0].KeyPoints) != 1 || out.Timeline[0].KeyPoints[0] != "要点1" {
			t.Errorf("timeline[0].KeyPoints = %#v", out.Timeline[0].KeyPoints)
		}
		if out.Timeline[2].KeyPoints == nil || len(out.Timeline[2].KeyPoints) != 0 {
			t.Errorf("timeline[2].KeyPoints should be non-nil empty slice, got %#v", out.Timeline[2].KeyPoints)
		}
		if len(out.Timeline[3].KeyPoints) != 1 || out.Timeline[3].KeyPoints[0] != "要点3" {
			t.Errorf("timeline[3].KeyPoints = %#v, want [要点3]", out.Timeline[3].KeyPoints)
		}
	})

	t.Run("hasTimestamps false clears all timestamps to nil and preserves order", func(t *testing.T) {
		out := &ai.LessonSummaryOutput{
			Overview: "无时间戳课程",
			Outline: []ai.OutlineItem{
				{Title: "大纲 A", Summary: "A", StartSec: intPtr(100)},
				{Title: "大纲 B", Summary: "B", StartSec: intPtr(10)},
			},
			Timeline: []ai.TimelineSection{
				{StartSec: intPtr(200), EndSec: intPtr(250), Title: "片段 A", Summary: "A"},
				{StartSec: intPtr(10), EndSec: intPtr(50), Title: "片段 B", Summary: "B"},
			},
		}

		if err := ai.ValidateAndNormalizeSummary(out, 300, false); err != nil {
			t.Fatalf("ValidateAndNormalizeSummary error: %v", err)
		}
		for i, item := range out.Outline {
			if item.StartSec != nil {
				t.Errorf("outline[%d].StartSec = %v, want nil", i, *item.StartSec)
			}
		}
		if out.Timeline[0].Title != "片段 A" || out.Timeline[1].Title != "片段 B" {
			t.Errorf("timeline order not preserved: %+v", out.Timeline)
		}
		for i, sec := range out.Timeline {
			if sec.StartSec != nil || sec.EndSec != nil {
				t.Errorf("timeline[%d] timestamps = (%v, %v), want (nil, nil)", i, sec.StartSec, sec.EndSec)
			}
		}
	})

	t.Run("empty outline or timeline returns error", func(t *testing.T) {
		if err := ai.ValidateAndNormalizeSummary(nil, 300, true); err == nil {
			t.Error("expected error for nil output")
		}
		emptyOutline := &ai.LessonSummaryOutput{
			Overview: "Overview",
			Outline:  nil,
			Timeline: []ai.TimelineSection{{Title: "S1", Summary: "Desc"}},
		}
		if err := ai.ValidateAndNormalizeSummary(emptyOutline, 300, true); err == nil {
			t.Error("expected error for empty outline")
		}
		emptyTimeline := &ai.LessonSummaryOutput{
			Overview: "Overview",
			Outline:  []ai.OutlineItem{{Title: "O1", Summary: "Desc"}},
			Timeline: []ai.TimelineSection{},
		}
		if err := ai.ValidateAndNormalizeSummary(emptyTimeline, 300, true); err == nil {
			t.Error("expected error for empty timeline")
		}
		whitespaceOnlyOutline := &ai.LessonSummaryOutput{
			Overview: "Overview",
			Outline:  []ai.OutlineItem{{Title: "  ", Summary: "\t"}},
			Timeline: []ai.TimelineSection{{Title: "S1", Summary: "Desc"}},
		}
		if err := ai.ValidateAndNormalizeSummary(whitespaceOnlyOutline, 300, true); err == nil {
			t.Error("expected error when outline contains only blank items")
		}
	})

	t.Run("stable sort preserves relative order on equal start_sec and allows unknown duration", func(t *testing.T) {
		sections, err := ai.ValidateAndNormalizeTimelineChunk([]ai.TimelineSection{
			{StartSec: intPtr(50), EndSec: intPtr(80), Title: "First-50", Summary: "A"},
			{StartSec: nil, EndSec: nil, Title: "Nil-Start", Summary: "N"},
			{StartSec: intPtr(-10), EndSec: intPtr(10), Title: "Clamped-0", Summary: "Z"},
			{StartSec: intPtr(50), EndSec: intPtr(60), Title: "Second-50", Summary: "B"},
			{StartSec: intPtr(900), EndSec: intPtr(950), Title: "Unclamped-900", Summary: "U"},
		}, 0, true)
		if err != nil {
			t.Fatalf("ValidateAndNormalizeTimelineChunk error: %v", err)
		}
		gotTitles := make([]string, len(sections))
		for i, s := range sections {
			gotTitles[i] = s.Title
		}
		wantTitles := []string{"Clamped-0", "First-50", "Second-50", "Unclamped-900", "Nil-Start"}
		for i, want := range wantTitles {
			if gotTitles[i] != want {
				t.Errorf("sections[%d].Title = %q, want %q (all=%v)", i, gotTitles[i], want, gotTitles)
			}
		}
		if *sections[0].StartSec != 0 {
			t.Errorf("sections[0].StartSec = %d, want 0", *sections[0].StartSec)
		}
		if *sections[3].StartSec != 900 || *sections[3].EndSec != 950 {
			t.Errorf("sections[3] = (%d, %d), want (900, 950) when durationSec==0", *sections[3].StartSec, *sections[3].EndSec)
		}
	})
}
