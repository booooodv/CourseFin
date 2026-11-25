package ai

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultASRHTTPTimeout = 300 * time.Second
	maxAudioUploadBytes   = 32 << 20 // 32 MiB
	silentWAVSampleRate   = 16000
	silentWAVChannels     = 1
	silentWAVBitsSample   = 16
)

// ASRConfig configures an OpenAI-compatible speech-to-text (/audio/transcriptions) endpoint.
type ASRConfig struct {
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	Language string `json:"language"`
}

// TranscriptCue represents a single timed segment of transcribed speech or subtitle text.
type TranscriptCue struct {
	StartSec float64 `json:"start_sec"`
	EndSec   float64 `json:"end_sec"`
	Text     string  `json:"text"`
}

// TranscribeResult contains the detected language, full transcript text, and optional timed cues.
type TranscribeResult struct {
	Language string          `json:"language"`
	Text     string          `json:"text"`
	Cues     []TranscriptCue `json:"cues"`
}

// OpenAICompatibleTranscriber calls any OpenAI-compatible /audio/transcriptions endpoint.
type OpenAICompatibleTranscriber struct {
	cfg        ASRConfig
	endpoint   string
	httpClient *http.Client
}

// NewOpenAICompatibleTranscriber creates an OpenAI-compatible ASR transcriber.
// When httpClient is nil, it defaults to a 300-second timeout for long audio chunks.
func NewOpenAICompatibleTranscriber(cfg ASRConfig, httpClient *http.Client) *OpenAICompatibleTranscriber {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultASRHTTPTimeout}
	}
	return &OpenAICompatibleTranscriber{
		cfg:        cfg,
		endpoint:   normalizeTranscriptionsURL(cfg.BaseURL),
		httpClient: httpClient,
	}
}

func normalizeTranscriptionsURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	if u == "" {
		return ""
	}
	if strings.HasSuffix(u, "/audio/transcriptions") {
		return u
	}
	return u + "/audio/transcriptions"
}

// Transcribe uploads audio bytes to /audio/transcriptions, attempting verbose_json first
// and falling back on HTTP 400/422 to srt and then json.
func (t *OpenAICompatibleTranscriber) Transcribe(ctx context.Context, fileName string, audio io.Reader) (*TranscribeResult, error) {
	if t.endpoint == "" {
		return nil, errors.New("ai: asr base URL is not configured")
	}
	if strings.TrimSpace(t.cfg.Model) == "" {
		return nil, errors.New("ai: asr model is not configured")
	}
	if audio == nil {
		return nil, errors.New("ai: audio reader is nil")
	}

	// Buffer once so 400/422 fallback retries can replay the exact audio payload without reopening the reader.
	audioBytes, err := io.ReadAll(io.LimitReader(audio, maxAudioUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ai: read audio payload: %w", err)
	}
	if len(audioBytes) == 0 {
		return nil, errors.New("ai: audio payload is empty")
	}
	if len(audioBytes) > maxAudioUploadBytes {
		return nil, fmt.Errorf("ai: audio payload exceeds maximum size of %d bytes", maxAudioUploadBytes)
	}

	safeName := filepath.Base(strings.TrimSpace(fileName))
	if safeName == "" || safeName == "." || safeName == "/" || safeName == string(filepath.Separator) {
		safeName = "audio.mp3"
	}

	formats := []string{"verbose_json", "srt", "json"}
	for i, format := range formats {
		statusCode, body, reqErr := t.doMultipartTranscription(ctx, safeName, audioBytes, format)
		if reqErr != nil {
			return nil, reqErr
		}
		if statusCode < 200 || statusCode >= 300 {
			if (statusCode == http.StatusBadRequest || statusCode == http.StatusUnprocessableEntity) && i < len(formats)-1 {
				continue
			}
			return nil, fmt.Errorf("ai: asr upstream returned HTTP %d: %s", statusCode, formatUpstreamError(body))
		}
		return parseASRResponse(format, body, t.cfg.Language)
	}

	return nil, errors.New("ai: asr transcription exhausted fallback formats")
}

// TestConnection verifies ASR endpoint reachability, authentication, and model availability
// by submitting a short in-memory silent WAV clip with response_format=json.
func (t *OpenAICompatibleTranscriber) TestConnection(ctx context.Context) error {
	if t.endpoint == "" {
		return errors.New("ai: asr base URL is not configured")
	}
	if strings.TrimSpace(t.cfg.Model) == "" {
		return errors.New("ai: asr model is not configured")
	}

	wav := GenerateSilentWAV(500)
	statusCode, body, err := t.doMultipartTranscription(ctx, "silence.wav", wav, "json")
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("ai: asr upstream returned HTTP %d: %s", statusCode, formatUpstreamError(body))
	}

	var errEnv struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errEnv) == nil && errEnv.Error != nil && strings.TrimSpace(errEnv.Error.Message) != "" {
		return fmt.Errorf("ai: asr upstream error: %s", strings.TrimSpace(errEnv.Error.Message))
	}
	return nil
}

func (t *OpenAICompatibleTranscriber) doMultipartTranscription(ctx context.Context, fileName string, audioBytes []byte, responseFormat string) (int, []byte, error) {
	var bodyBuf bytes.Buffer
	writer := multipart.NewWriter(&bodyBuf)

	if err := writer.WriteField("model", strings.TrimSpace(t.cfg.Model)); err != nil {
		return 0, nil, fmt.Errorf("ai: write model field: %w", err)
	}
	if err := writer.WriteField("response_format", responseFormat); err != nil {
		return 0, nil, fmt.Errorf("ai: write response_format field: %w", err)
	}
	if lang := strings.TrimSpace(t.cfg.Language); lang != "" {
		if err := writer.WriteField("language", lang); err != nil {
			return 0, nil, fmt.Errorf("ai: write language field: %w", err)
		}
	}

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return 0, nil, fmt.Errorf("ai: create multipart file: %w", err)
	}
	if _, err := part.Write(audioBytes); err != nil {
		return 0, nil, fmt.Errorf("ai: write multipart audio: %w", err)
	}
	if err := writer.Close(); err != nil {
		return 0, nil, fmt.Errorf("ai: close multipart body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(bodyBuf.Bytes()))
	if err != nil {
		return 0, nil, fmt.Errorf("ai: create asr request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if apiKey := strings.TrimSpace(t.cfg.APIKey); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("ai: asr request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("ai: read asr response body: %w", err)
	}
	return resp.StatusCode, body, nil
}

type asrVerboseJSONResponse struct {
	Language string `json:"language"`
	Text     string `json:"text"`
	Segments []struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Text  string  `json:"text"`
	} `json:"segments"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func parseASRResponse(format string, body []byte, fallbackLang string) (*TranscribeResult, error) {
	defaultLang := strings.TrimSpace(fallbackLang)

	switch format {
	case "verbose_json":
		var parsed asrVerboseJSONResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			// Some proxies ignore response_format=verbose_json and return raw SRT text with HTTP 200.
			raw := string(body)
			if strings.Contains(raw, "-->") {
				cues := ParseSubtitleCues(raw)
				return &TranscribeResult{
					Language: defaultLang,
					Text:     joinCueText(cues, raw),
					Cues:     cues,
				}, nil
			}
			return nil, fmt.Errorf("ai: decode asr verbose_json response: %w", err)
		}
		if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
			return nil, fmt.Errorf("ai: asr upstream error: %s", strings.TrimSpace(parsed.Error.Message))
		}

		cues := make([]TranscriptCue, 0, len(parsed.Segments))
		for _, seg := range parsed.Segments {
			txt := strings.TrimSpace(seg.Text)
			if txt == "" {
				continue
			}
			start := seg.Start
			if start < 0 || math.IsNaN(start) || math.IsInf(start, 0) {
				start = 0
			}
			end := seg.End
			if math.IsNaN(end) || math.IsInf(end, 0) || end < start {
				end = start
			}
			cues = append(cues, TranscriptCue{
				StartSec: start,
				EndSec:   end,
				Text:     txt,
			})
		}

		lang := strings.TrimSpace(parsed.Language)
		if lang == "" {
			lang = defaultLang
		}
		text := strings.TrimSpace(parsed.Text)
		if text == "" && len(cues) > 0 {
			text = joinCueText(cues, "")
		}
		return &TranscribeResult{
			Language: lang,
			Text:     text,
			Cues:     cues,
		}, nil

	case "srt":
		raw := strings.TrimSpace(string(body))
		if strings.HasPrefix(raw, "{") {
			var parsed asrVerboseJSONResponse
			if err := json.Unmarshal(body, &parsed); err == nil {
				if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
					return nil, fmt.Errorf("ai: asr upstream error: %s", strings.TrimSpace(parsed.Error.Message))
				}
				lang := strings.TrimSpace(parsed.Language)
				if lang == "" {
					lang = defaultLang
				}
				cues := ParseSubtitleCues(parsed.Text)
				text := strings.TrimSpace(parsed.Text)
				if len(cues) > 0 {
					text = joinCueText(cues, text)
				}
				return &TranscribeResult{
					Language: lang,
					Text:     text,
					Cues:     cues,
				}, nil
			}
		}

		cues := ParseSubtitleCues(string(body))
		return &TranscribeResult{
			Language: defaultLang,
			Text:     joinCueText(cues, raw),
			Cues:     cues,
		}, nil

	case "json":
		var parsed asrVerboseJSONResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("ai: decode asr json response: %w", err)
		}
		if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
			return nil, fmt.Errorf("ai: asr upstream error: %s", strings.TrimSpace(parsed.Error.Message))
		}
		lang := strings.TrimSpace(parsed.Language)
		if lang == "" {
			lang = defaultLang
		}
		return &TranscribeResult{
			Language: lang,
			Text:     strings.TrimSpace(parsed.Text),
		}, nil

	default:
		return nil, fmt.Errorf("ai: unsupported asr response format %q", format)
	}
}

func joinCueText(cues []TranscriptCue, fallback string) string {
	if len(cues) == 0 {
		return strings.TrimSpace(fallback)
	}
	parts := make([]string, 0, len(cues))
	for _, c := range cues {
		if t := strings.TrimSpace(c.Text); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// GenerateSilentWAV generates a standard 44-byte PCM WAV header (16kHz, mono, 16-bit LE)
// followed by zeroed PCM sample bytes for the given duration in milliseconds.
func GenerateSilentWAV(durationMs int) []byte {
	if durationMs < 0 {
		durationMs = 0
	}
	blockAlign := silentWAVChannels * (silentWAVBitsSample / 8)
	byteRate := silentWAVSampleRate * blockAlign
	numSamples := (silentWAVSampleRate * durationMs) / 1000
	dataSize := numSamples * blockAlign

	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // PCM subchunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM format
	binary.LittleEndian.PutUint16(buf[22:24], uint16(silentWAVChannels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(silentWAVSampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], uint16(silentWAVBitsSample))
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))
	return buf
}

// ParseSubtitleCues parses WebVTT or SRT text into timed TranscriptCue entries,
// ignoring WEBVTT headers, SRT sequence numbers, NOTE/STYLE/REGION blocks, and cue settings.
func ParseSubtitleCues(text string) []TranscriptCue {
	clean := strings.TrimPrefix(text, "\xEF\xBB\xBF")
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")

	lines := strings.Split(clean, "\n")
	var blocks [][]string
	var cur []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}

	var cues []TranscriptCue
	for _, blk := range blocks {
		if len(blk) == 0 {
			continue
		}
		firstTrimmed := strings.TrimSpace(blk[0])
		if isIgnoredSubtitleBlock(firstTrimmed) {
			continue
		}

		timeIdx := -1
		var startSec, endSec float64
		for i, line := range blk {
			if s, e, ok := parseCueTimingLine(line); ok {
				timeIdx = i
				startSec = s
				endSec = e
				break
			}
		}
		if timeIdx < 0 || timeIdx+1 >= len(blk) {
			continue
		}

		payload := strings.TrimSpace(strings.Join(blk[timeIdx+1:], "\n"))
		if payload == "" {
			continue
		}

		cues = append(cues, TranscriptCue{
			StartSec: startSec,
			EndSec:   endSec,
			Text:     payload,
		})
	}
	return cues
}

func isIgnoredSubtitleBlock(firstLine string) bool {
	if firstLine == "" {
		return true
	}
	fields := strings.Fields(firstLine)
	if len(fields) == 0 {
		return true
	}
	switch fields[0] {
	case "NOTE", "STYLE", "REGION":
		return true
	}
	return false
}

func parseCueTimingLine(line string) (float64, float64, bool) {
	idx := strings.Index(line, "-->")
	if idx < 0 {
		return 0, 0, false
	}
	left := strings.TrimSpace(line[:idx])
	rightFields := strings.Fields(line[idx+3:])
	if len(rightFields) == 0 {
		return 0, 0, false
	}

	startSec, ok := parseSubtitleTimestamp(left)
	if !ok {
		return 0, 0, false
	}
	endSec, ok := parseSubtitleTimestamp(rightFields[0])
	if !ok {
		return 0, 0, false
	}
	if endSec < startSec {
		endSec = startSec
	}
	return startSec, endSec, true
}

func parseSubtitleTimestamp(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}

	var fracSec float64
	if sepIdx := strings.IndexAny(s, ".,"); sepIdx >= 0 {
		fracStr := s[sepIdx+1:]
		s = s[:sepIdx]
		if fracStr == "" || !isASCIIDigits(fracStr) {
			return 0, false
		}
		f, err := strconv.ParseFloat("0."+fracStr, 64)
		if err != nil {
			return 0, false
		}
		fracSec = f
	}

	parts := strings.Split(s, ":")
	var hours, mins, secs int
	switch len(parts) {
	case 2:
		if !isASCIIDigits(parts[0]) || !isASCIIDigits(parts[1]) {
			return 0, false
		}
		m, err1 := strconv.Atoi(parts[0])
		sec, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || m < 0 || sec < 0 || sec >= 60 {
			return 0, false
		}
		mins, secs = m, sec
	case 3:
		if !isASCIIDigits(parts[0]) || !isASCIIDigits(parts[1]) || !isASCIIDigits(parts[2]) {
			return 0, false
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		sec, err3 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || err3 != nil || h < 0 || m < 0 || m >= 60 || sec < 0 || sec >= 60 {
			return 0, false
		}
		hours, mins, secs = h, m, sec
	default:
		return 0, false
	}

	return float64(hours*3600+mins*60+secs) + fracSec, true
}

func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FormatCuesAsWebVTT serializes transcript cues as standard UTF-8 WebVTT text.
func FormatCuesAsWebVTT(cues []TranscriptCue) string {
	var out strings.Builder
	out.WriteString("WEBVTT\n\n")

	written := 0
	for _, cue := range cues {
		text := strings.ReplaceAll(cue.Text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		// Prevent internal blank lines inside a cue from splitting it into separate blocks.
		for strings.Contains(text, "\n\n") {
			text = strings.ReplaceAll(text, "\n\n", "\n")
		}

		start := cue.StartSec
		if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 {
			start = 0
		}
		end := cue.EndSec
		if math.IsNaN(end) || math.IsInf(end, 0) || end < start {
			end = start
		}

		if written > 0 {
			out.WriteByte('\n')
		}
		written++

		out.WriteString(formatWebVTTTimestamp(start))
		out.WriteString(" --> ")
		out.WriteString(formatWebVTTTimestamp(end))
		out.WriteByte('\n')
		out.WriteString(text)
		out.WriteByte('\n')
	}

	return out.String()
}

func formatWebVTTTimestamp(sec float64) string {
	if math.IsNaN(sec) || math.IsInf(sec, 0) || sec < 0 {
		sec = 0
	}
	totalMs := int64(math.Round(sec * 1000))
	ms := totalMs % 1000
	totalSec := totalMs / 1000
	s := totalSec % 60
	totalMin := totalSec / 60
	m := totalMin % 60
	h := totalMin / 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}
