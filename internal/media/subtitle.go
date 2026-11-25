package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"coursefin/internal/storage"
)

const maxSubtitleBytes = 32 << 20 // 32 MiB safety cap

var (
	srtCueNumRe    = regexp.MustCompile(`^\d+$`)
	srtTimestampRe = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{3})`)
)

// StreamDescriptor describes an embedded or external media stream with its
// deterministic Jellyfin stream index.
type StreamDescriptor struct {
	Index        int
	Type         string // "video", "audio", "subtitle"
	Codec        string
	Language     string
	Title        string
	IsDefault    bool
	IsForced     bool
	IsExternal   bool
	SubtitleID   string
	SourceID     string
	RelativePath string
	Format       string
	Width        int
	Height       int
	Bitrate      int64
	Channels     int
	SampleRate   int
}

type ffprobeStreamEntry struct {
	Index       int    `json:"index"`
	CodecType   string `json:"codec_type"`
	CodecName   string `json:"codec_name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	BitRate     string `json:"bit_rate"`
	Channels    int    `json:"channels"`
	SampleRate  string `json:"sample_rate"`
	Disposition struct {
		Default int `json:"default"`
		Forced  int `json:"forced"`
	} `json:"disposition"`
	Tags map[string]string `json:"tags"`
}

// ResolveStreams returns all embedded streams (preserving ffprobe stream index)
// followed sequentially by active external sidecar subtitle_files ordered by
// is_default DESC, language ASC, id ASC.
func (s *Service) ResolveStreams(ctx context.Context, mediaFileID string) ([]StreamDescriptor, error) {
	rec, err := s.resolveMediaRecord(ctx, mediaFileID, false)
	if err != nil {
		return nil, err
	}
	return s.BuildStreamDescriptors(ctx, rec.MediaFileID, rec.StreamsJSON)
}

// BuildStreamDescriptors builds deterministic stream descriptors for a media file
// from its probed streams_json and active external subtitle_files rows.
func (s *Service) BuildStreamDescriptors(ctx context.Context, mediaFileID, streamsJSON string) ([]StreamDescriptor, error) {
	mediaFileID = normalizeOrRaw(mediaFileID)

	var embedded []ffprobeStreamEntry
	if strings.TrimSpace(streamsJSON) != "" && strings.TrimSpace(streamsJSON) != "[]" {
		if err := json.Unmarshal([]byte(streamsJSON), &embedded); err != nil {
			return nil, fmt.Errorf("parse streams_json for %s: %w", mediaFileID, err)
		}
	}

	descriptors := make([]StreamDescriptor, 0, len(embedded))
	nextIndex := 0
	for _, st := range embedded {
		if st.Index >= nextIndex {
			nextIndex = st.Index + 1
		}
		var br int64
		if st.BitRate != "" {
			br, _ = strconv.ParseInt(strings.TrimSpace(st.BitRate), 10, 64)
		}
		var sr int
		if st.SampleRate != "" {
			sr, _ = strconv.Atoi(strings.TrimSpace(st.SampleRate))
		}
		lang := tagCI(st.Tags, "language")
		title := tagCI(st.Tags, "title")
		descriptors = append(descriptors, StreamDescriptor{
			Index:      st.Index,
			Type:       strings.ToLower(strings.TrimSpace(st.CodecType)),
			Codec:      strings.ToLower(strings.TrimSpace(st.CodecName)),
			Language:   lang,
			Title:      title,
			IsDefault:  st.Disposition.Default != 0,
			IsForced:   st.Disposition.Forced != 0,
			Width:      st.Width,
			Height:     st.Height,
			Bitrate:    br,
			Channels:   st.Channels,
			SampleRate: sr,
		})
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_id, relative_path, language, title, format, is_default
		FROM subtitle_files
		WHERE media_file_id = ? AND status = 'active'
		ORDER BY is_default DESC, language ASC, id ASC`, mediaFileID)
	if err != nil {
		return nil, fmt.Errorf("query subtitle_files for %s: %w", mediaFileID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var sd StreamDescriptor
		var isDef int
		if err := rows.Scan(&sd.SubtitleID, &sd.SourceID, &sd.RelativePath, &sd.Language, &sd.Title, &sd.Format, &isDef); err != nil {
			return nil, fmt.Errorf("scan subtitle_file: %w", err)
		}
		sd.Index = nextIndex
		nextIndex++
		sd.Type = "subtitle"
		sd.Format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(sd.Format), "."))
		if sd.Format == "" {
			sd.Format = strings.ToLower(strings.TrimPrefix(path.Ext(sd.RelativePath), "."))
		}
		sd.Codec = sd.Format
		sd.IsDefault = isDef != 0
		sd.IsExternal = true
		descriptors = append(descriptors, sd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return descriptors, nil
}

// ServeSubtitle serves either an external sidecar subtitle (converting SRT -> VTT
// on the fly when requested) or extracts an embedded text subtitle via FFmpeg.
func (s *Service) ServeSubtitle(w http.ResponseWriter, r *http.Request, mediaFileID string, streamIndex int, targetFormat string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rec, err := s.resolveMediaRecord(r.Context(), mediaFileID, false)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	streams, err := s.BuildStreamDescriptors(r.Context(), rec.MediaFileID, rec.StreamsJSON)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var target *StreamDescriptor
	for i := range streams {
		if streams[i].Index == streamIndex && streams[i].Type == "subtitle" {
			target = &streams[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "subtitle stream not found", http.StatusNotFound)
		return
	}

	fmtNorm := normalizeSubtitleFormat(targetFormat)
	if target.IsExternal {
		s.serveExternalSubtitle(w, r, target, fmtNorm)
		return
	}
	s.serveEmbeddedSubtitle(w, r, rec, target, fmtNorm)
}

func (s *Service) serveExternalSubtitle(w http.ResponseWriter, r *http.Request, sub *StreamDescriptor, targetFormat string) {
	var driver, rootPath, configJSON string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT driver, root_path, config_json FROM storage_sources WHERE id = ?`, sub.SourceID,
	).Scan(&driver, &rootPath, &configJSON)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	provider, err := s.registry.ProviderFor(driver, rootPath, configJSON)
	if err != nil {
		http.Error(w, fmt.Sprintf("storage provider error: %v", err), http.StatusInternalServerError)
		return
	}

	rc, err := provider.Open(r.Context(), sub.RelativePath)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	defer rc.Close()

	rawBytes, err := io.ReadAll(io.LimitReader(rc, maxSubtitleBytes))
	if err != nil {
		http.Error(w, fmt.Sprintf("read subtitle: %v", err), http.StatusInternalServerError)
		return
	}

	srcFmt := normalizeSubtitleFormat(sub.Format)
	if targetFormat == "" {
		targetFormat = srcFmt
	}

	decodedText := DecodeSubtitleToUTF8(rawBytes)
	utf8Bytes := []byte(decodedText)

	var out []byte
	switch {
	case targetFormat == srcFmt:
		if targetFormat == "vtt" {
			out = []byte(ensureWebVTTHeader(decodedText))
		} else {
			out = utf8Bytes
		}
	case targetFormat == "vtt" && srcFmt == "srt":
		out = []byte(ConvertSRTToVTT(decodedText))
	case targetFormat == "vtt" && (srcFmt == "ass" || srcFmt == "ssa"):
		converted, convErr := s.convertSubtitleBytesWithFFmpeg(r.Context(), utf8Bytes, srcFmt, "webvtt")
		if convErr != nil {
			http.Error(w, convErr.Error(), http.StatusInternalServerError)
			return
		}
		out = converted
	default:
		ffmpegFmt, ok := ffmpegSubtitleFormat(targetFormat)
		if !ok {
			http.Error(w, fmt.Sprintf("unsupported subtitle target format %q", targetFormat), http.StatusUnsupportedMediaType)
			return
		}
		converted, convErr := s.convertSubtitleBytesWithFFmpeg(r.Context(), utf8Bytes, srcFmt, ffmpegFmt)
		if convErr != nil {
			http.Error(w, convErr.Error(), http.StatusInternalServerError)
			return
		}
		out = converted
	}

	writeSubtitleResponse(w, r, targetFormat, out)
}

func ensureWebVTTHeader(text string) string {
	clean := strings.TrimPrefix(text, "\xEF\xBB\xBF")
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	trimmed := strings.TrimLeft(clean, " \t\n")
	if strings.HasPrefix(strings.ToUpper(trimmed), "WEBVTT") {
		return trimmed
	}
	return "WEBVTT\n\n" + trimmed
}

func (s *Service) serveEmbeddedSubtitle(w http.ResponseWriter, r *http.Request, rec *mediaRecord, sub *StreamDescriptor, targetFormat string) {
	if isBitmapSubtitleCodec(sub.Codec) {
		http.Error(w, fmt.Sprintf("bitmap subtitle codec %q cannot be extracted as text", sub.Codec), http.StatusUnsupportedMediaType)
		return
	}
	if targetFormat == "" {
		targetFormat = "vtt"
	}
	ffmpegFmt, ok := ffmpegSubtitleFormat(targetFormat)
	if !ok {
		http.Error(w, fmt.Sprintf("unsupported subtitle target format %q", targetFormat), http.StatusUnsupportedMediaType)
		return
	}

	provider, err := s.registry.ProviderFor(rec.Driver, rec.RootPath, rec.ConfigJSON)
	if err != nil {
		http.Error(w, fmt.Sprintf("storage provider error: %v", err), http.StatusInternalServerError)
		return
	}
	localResolver, ok := provider.(storage.LocalPathResolver)
	if !ok {
		http.Error(w, "embedded subtitle extraction requires local filesystem access", http.StatusNotImplemented)
		return
	}
	absPath, err := localResolver.AbsPath(rec.RelativePath)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	cmd := exec.CommandContext(
		r.Context(),
		s.ffmpegBin,
		"-hide_banner",
		"-loglevel", "error",
		"-i", absPath,
		"-map", fmt.Sprintf("0:%d", sub.Index),
		"-f", ffmpegFmt,
		"pipe:1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("extract subtitle stream %d: %v (%s)", sub.Index, err, strings.TrimSpace(stderr.String())), http.StatusInternalServerError)
		return
	}

	writeSubtitleResponse(w, r, targetFormat, stdout.Bytes())
}

func (s *Service) convertSubtitleBytesWithFFmpeg(ctx context.Context, raw []byte, srcFmt, dstFFmpegFmt string) ([]byte, error) {
	inFmt, ok := ffmpegSubtitleFormat(srcFmt)
	if !ok {
		return nil, fmt.Errorf("unsupported subtitle source format %q", srcFmt)
	}
	cmd := exec.CommandContext(
		ctx,
		s.ffmpegBin,
		"-hide_banner",
		"-loglevel", "error",
		"-f", inFmt,
		"-i", "pipe:0",
		"-f", dstFFmpegFmt,
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("convert subtitle %s -> %s: %w (%s)", srcFmt, dstFFmpegFmt, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func writeSubtitleResponse(w http.ResponseWriter, r *http.Request, format string, body []byte) {
	w.Header().Set("Content-Type", subtitleContentType(format))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// ConvertSRTToVTT converts SubRip (.srt) text into WebVTT (.vtt) format,
// stripping UTF-8 BOM, normalizing CRLF line endings, converting comma cue
// timestamps to dots, and preserving multi-line Chinese/English cue payloads.
func ConvertSRTToVTT(srt string) string {
	clean := strings.TrimPrefix(srt, "\xEF\xBB\xBF")
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

	var out strings.Builder
	out.WriteString("WEBVTT\n\n")

	written := 0
	for _, blk := range blocks {
		if len(blk) == 0 {
			continue
		}
		timeIdx := -1
		for i, line := range blk {
			if strings.Contains(line, "-->") {
				timeIdx = i
				break
			}
		}
		if timeIdx < 0 {
			continue
		}
		// Skip pure numeric SRT sequence number line right before the timestamp.
		startIdx := timeIdx
		if timeIdx == 1 && srtCueNumRe.MatchString(strings.TrimSpace(blk[0])) {
			startIdx = 1
		} else if timeIdx > 0 {
			startIdx = 0
		}

		if written > 0 {
			out.WriteByte('\n')
		}
		written++

		for i := startIdx; i < len(blk); i++ {
			line := blk[i]
			if i == timeIdx {
				line = convertSRTTimestampLine(line)
			}
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}

	return out.String()
}

func convertSRTTimestampLine(line string) string {
	return srtTimestampRe.ReplaceAllStringFunc(line, func(m string) string {
		parts := srtTimestampRe.FindStringSubmatch(m)
		if len(parts) != 5 {
			return m
		}
		hh := parts[1]
		if len(hh) == 1 {
			hh = "0" + hh
		}
		return hh + ":" + parts[2] + ":" + parts[3] + "." + parts[4]
	})
}

func normalizeSubtitleFormat(raw string) string {
	f := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "."))
	switch f {
	case "webvtt":
		return "vtt"
	case "subrip":
		return "srt"
	default:
		return f
	}
}

func ffmpegSubtitleFormat(norm string) (string, bool) {
	switch norm {
	case "vtt":
		return "webvtt", true
	case "srt":
		return "srt", true
	case "ass", "ssa":
		return "ass", true
	default:
		return "", false
	}
}

func subtitleContentType(norm string) string {
	switch norm {
	case "vtt":
		return "text/vtt; charset=utf-8"
	case "srt":
		return "application/x-subrip; charset=utf-8"
	case "ass", "ssa":
		return "text/x-ssa; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

func isBitmapSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "dvd_subtitle", "hdmv_pgs_subtitle", "pgssub", "dvb_subtitle", "xsub", "dvdsub":
		return true
	default:
		return false
	}
}

func tagCI(tags map[string]string, key string) string {
	if len(tags) == 0 {
		return ""
	}
	if v, ok := tags[key]; ok {
		return v
	}
	for k, v := range tags {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
