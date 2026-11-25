package media

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"coursefin/internal/storage"
)

const maxSubtitleBytes = 32 << 20 // 32 MiB safety cap

var (
	srtCueNumRe    = regexp.MustCompile(`^\d+$`)
	srtTimestampRe = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{3})`)

	errBitmapSubtitle         = errors.New("bitmap subtitle cannot be extracted as text")
	errUnsupportedSubtitleFmt = errors.New("unsupported subtitle target format")
	errLocalAccessRequired    = errors.New("local filesystem access required")
	errMediaLookup            = errors.New("media lookup failed")
)

// StreamDescriptor describes an embedded or external media stream with its
// deterministic Jellyfin stream index.
type StreamDescriptor struct {
	Index         int
	Type          string // "video", "audio", "subtitle"
	Codec         string
	Language      string
	Title         string
	IsDefault     bool
	IsForced      bool
	IsExternal    bool
	IsAIGenerated bool
	SubtitleID    string
	SourceID      string
	RelativePath  string
	Format        string
	Width         int
	Height        int
	Bitrate       int64
	Channels      int
	SampleRate    int
}

// LocalMediaInfo describes local filesystem metadata and audio stream presence for a media file.
type LocalMediaInfo struct {
	MediaFileID string
	AbsPath     string
	DurationMs  int64
	HasAudio    bool
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
// is_default DESC, language ASC, id ASC, and finally any cached ASR transcript
// track stored in media_transcripts.
func (s *Service) ResolveStreams(ctx context.Context, mediaFileID string) ([]StreamDescriptor, error) {
	rec, err := s.resolveMediaRecord(ctx, mediaFileID, false)
	if err != nil {
		return nil, err
	}
	return s.BuildStreamDescriptors(ctx, rec.MediaFileID, rec.StreamsJSON)
}

func parseEmbeddedStreams(mediaFileID, streamsJSON string) ([]ffprobeStreamEntry, error) {
	trimmed := strings.TrimSpace(streamsJSON)
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var embedded []ffprobeStreamEntry
	if err := json.Unmarshal([]byte(trimmed), &embedded); err != nil {
		return nil, fmt.Errorf("parse streams_json for %s: %w", mediaFileID, err)
	}
	return embedded, nil
}

// BuildStreamDescriptors builds deterministic stream descriptors for a media file
// from its probed streams_json, active external subtitle_files rows, and any
// cached ASR transcript in media_transcripts.
func (s *Service) BuildStreamDescriptors(ctx context.Context, mediaFileID, streamsJSON string) ([]StreamDescriptor, error) {
	mediaFileID = normalizeOrRaw(mediaFileID)

	embedded, err := parseEmbeddedStreams(mediaFileID, streamsJSON)
	if err != nil {
		return nil, err
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

	// Append cached ASR transcript strictly after embedded streams and physical sidecars
	// so existing Web/Jellyfin subtitle stream indices never shift.
	var aiLang string
	err = s.db.QueryRowContext(ctx, `
		SELECT language
		FROM media_transcripts
		WHERE media_file_id = ? AND source = 'asr' AND content_vtt != ''`, mediaFileID,
	).Scan(&aiLang)
	if err == nil {
		aiLang = strings.TrimSpace(aiLang)
		if aiLang == "" {
			aiLang = "und"
		}
		descriptors = append(descriptors, StreamDescriptor{
			Index:         nextIndex,
			Type:          "subtitle",
			Codec:         "vtt",
			Language:      aiLang,
			Title:         "AI 转写字幕",
			IsDefault:     false,
			IsExternal:    true,
			IsAIGenerated: true,
			Format:        "vtt",
		})
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query media_transcripts for %s: %w", mediaFileID, err)
	}

	return descriptors, nil
}

// ServeSubtitle serves an AI-generated transcript, an external sidecar subtitle
// (converting SRT -> VTT on the fly when requested), or extracts an embedded
// text subtitle via FFmpeg.
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
	switch {
	case target.IsAIGenerated:
		s.serveAIGeneratedSubtitle(w, r, rec.MediaFileID, fmtNorm)
	case target.IsExternal:
		s.serveExternalSubtitle(w, r, target, fmtNorm)
	default:
		s.serveEmbeddedSubtitle(w, r, rec, target, fmtNorm)
	}
}

func (s *Service) serveAIGeneratedSubtitle(w http.ResponseWriter, r *http.Request, mediaFileID, targetFormat string) {
	var contentVTT string
	err := s.db.QueryRowContext(r.Context(), `
		SELECT content_vtt
		FROM media_transcripts
		WHERE media_file_id = ? AND source = 'asr' AND content_vtt != ''`, mediaFileID,
	).Scan(&contentVTT)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "subtitle stream not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.serveDecodedSubtitleText(w, r, ensureWebVTTHeader(contentVTT), "vtt", targetFormat)
}

func (s *Service) readExternalSubtitleUTF8(ctx context.Context, sub *StreamDescriptor) (string, string, error) {
	var driver, rootPath, configJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT driver, root_path, config_json FROM storage_sources WHERE id = ?`, sub.SourceID,
	).Scan(&driver, &rootPath, &configJSON)
	if err != nil {
		return "", "", err
	}

	provider, err := s.registry.ProviderFor(driver, rootPath, configJSON)
	if err != nil {
		return "", "", fmt.Errorf("storage provider error: %w", err)
	}

	rc, err := provider.Open(ctx, sub.RelativePath)
	if err != nil {
		return "", "", err
	}
	defer rc.Close()

	rawBytes, err := io.ReadAll(io.LimitReader(rc, maxSubtitleBytes))
	if err != nil {
		return "", "", fmt.Errorf("read subtitle: %w", err)
	}

	return DecodeSubtitleToUTF8(rawBytes), normalizeSubtitleFormat(sub.Format), nil
}

func (s *Service) convertUTF8SubtitleToVTT(ctx context.Context, decodedText, srcFmt string) (string, error) {
	switch srcFmt {
	case "vtt":
		return ensureWebVTTHeader(decodedText), nil
	case "srt":
		return ConvertSRTToVTT(decodedText), nil
	default:
		converted, err := s.convertSubtitleBytesWithFFmpeg(ctx, []byte(decodedText), srcFmt, "webvtt")
		if err != nil {
			return "", err
		}
		return ensureWebVTTHeader(string(converted)), nil
	}
}

func (s *Service) readExternalSubtitleAsVTT(ctx context.Context, sub *StreamDescriptor) (string, error) {
	decodedText, srcFmt, err := s.readExternalSubtitleUTF8(ctx, sub)
	if err != nil {
		return "", err
	}
	return s.convertUTF8SubtitleToVTT(ctx, decodedText, srcFmt)
}

func (s *Service) serveDecodedSubtitleText(w http.ResponseWriter, r *http.Request, decodedText, srcFmt, targetFormat string) {
	if targetFormat == "" {
		targetFormat = srcFmt
	}

	var out []byte
	switch {
	case targetFormat == "vtt":
		vtt, err := s.convertUTF8SubtitleToVTT(r.Context(), decodedText, srcFmt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = []byte(vtt)
	case targetFormat == srcFmt:
		out = []byte(decodedText)
	default:
		ffmpegFmt, ok := ffmpegSubtitleFormat(targetFormat)
		if !ok {
			http.Error(w, fmt.Sprintf("unsupported subtitle target format %q", targetFormat), http.StatusUnsupportedMediaType)
			return
		}
		converted, convErr := s.convertSubtitleBytesWithFFmpeg(r.Context(), []byte(decodedText), srcFmt, ffmpegFmt)
		if convErr != nil {
			http.Error(w, convErr.Error(), http.StatusInternalServerError)
			return
		}
		out = converted
	}

	writeSubtitleResponse(w, r, targetFormat, out)
}

func (s *Service) serveExternalSubtitle(w http.ResponseWriter, r *http.Request, sub *StreamDescriptor, targetFormat string) {
	decodedText, srcFmt, err := s.readExternalSubtitleUTF8(r.Context(), sub)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	s.serveDecodedSubtitleText(w, r, decodedText, srcFmt, targetFormat)
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

func (s *Service) extractEmbeddedSubtitle(ctx context.Context, rec *mediaRecord, sub *StreamDescriptor, targetFormat string) ([]byte, error) {
	if isBitmapSubtitleCodec(sub.Codec) {
		return nil, fmt.Errorf("%w: %s", errBitmapSubtitle, sub.Codec)
	}
	if targetFormat == "" {
		targetFormat = "vtt"
	}
	ffmpegFmt, ok := ffmpegSubtitleFormat(targetFormat)
	if !ok {
		return nil, fmt.Errorf("%w: %q", errUnsupportedSubtitleFmt, targetFormat)
	}

	provider, err := s.registry.ProviderFor(rec.Driver, rec.RootPath, rec.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("storage provider error: %w", err)
	}
	localResolver, ok := provider.(storage.LocalPathResolver)
	if !ok {
		return nil, errLocalAccessRequired
	}
	absPath, err := localResolver.AbsPath(rec.RelativePath)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errMediaLookup, err)
	}

	cmd := exec.CommandContext(
		ctx,
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
		return nil, fmt.Errorf("extract subtitle stream %d: %v (%s)", sub.Index, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (s *Service) extractEmbeddedSubtitleAsVTT(ctx context.Context, rec *mediaRecord, sub *StreamDescriptor) (string, error) {
	raw, err := s.extractEmbeddedSubtitle(ctx, rec, sub, "vtt")
	if err != nil {
		return "", err
	}
	return ensureWebVTTHeader(string(raw)), nil
}

func (s *Service) serveEmbeddedSubtitle(w http.ResponseWriter, r *http.Request, rec *mediaRecord, sub *StreamDescriptor, targetFormat string) {
	if targetFormat == "" {
		targetFormat = "vtt"
	}
	out, err := s.extractEmbeddedSubtitle(r.Context(), rec, sub, targetFormat)
	if err != nil {
		switch {
		case errors.Is(err, errBitmapSubtitle):
			http.Error(w, fmt.Sprintf("bitmap subtitle codec %q cannot be extracted as text", sub.Codec), http.StatusUnsupportedMediaType)
		case errors.Is(err, errUnsupportedSubtitleFmt):
			http.Error(w, fmt.Sprintf("unsupported subtitle target format %q", targetFormat), http.StatusUnsupportedMediaType)
		case errors.Is(err, errLocalAccessRequired):
			http.Error(w, "embedded subtitle extraction requires local filesystem access", http.StatusNotImplemented)
		case errors.Is(err, errMediaLookup):
			writeLookupError(w, err)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	writeSubtitleResponse(w, r, targetFormat, out)
}

// ExtractBestSubtitleVTT selects and extracts the best physical text subtitle
// (active external sidecar first, then non-bitmap embedded stream; within each
// tier: default -> Chinese -> first) as UTF-8 WebVTT.
// AI-generated tracks from media_transcripts are intentionally skipped so callers
// can manage cached transcript priority explicitly.
func (s *Service) ExtractBestSubtitleVTT(ctx context.Context, mediaFileID string) (vttText, sourceType, sourceLabel string, found bool, err error) {
	rec, err := s.resolveMediaRecord(ctx, mediaFileID, false)
	if err != nil {
		return "", "", "", false, err
	}
	streams, err := s.BuildStreamDescriptors(ctx, rec.MediaFileID, rec.StreamsJSON)
	if err != nil {
		return "", "", "", false, err
	}

	var externalSubs []StreamDescriptor
	var embeddedSubs []StreamDescriptor
	for _, st := range streams {
		if !strings.EqualFold(st.Type, "subtitle") || st.IsAIGenerated {
			continue
		}
		if st.IsExternal {
			externalSubs = append(externalSubs, st)
			continue
		}
		if !isBitmapSubtitleCodec(st.Codec) {
			embeddedSubs = append(embeddedSubs, st)
		}
	}

	sortSubtitlesByPriority(externalSubs)
	sortSubtitlesByPriority(embeddedSubs)

	var firstErr error
	for i := range externalSubs {
		sub := &externalSubs[i]
		vtt, readErr := s.readExternalSubtitleAsVTT(ctx, sub)
		if readErr != nil {
			if firstErr == nil {
				firstErr = readErr
			}
			continue
		}
		if !hasWebVTTContent(vtt) {
			continue
		}
		return vtt, "external_subtitle", formatSubtitleSourceLabel("外挂字幕", sub.Language), true, nil
	}

	for i := range embeddedSubs {
		sub := &embeddedSubs[i]
		vtt, extErr := s.extractEmbeddedSubtitleAsVTT(ctx, rec, sub)
		if extErr != nil {
			if firstErr == nil {
				firstErr = extErr
			}
			continue
		}
		if !hasWebVTTContent(vtt) {
			continue
		}
		return vtt, "embedded_subtitle", formatSubtitleSourceLabel("内嵌字幕", sub.Language), true, nil
	}

	if firstErr != nil {
		return "", "", "", false, firstErr
	}
	return "", "", "", false, nil
}

// ResolveLocalMediaInfo resolves the local filesystem path, duration, and audio
// stream presence for a media file (or lesson ID fallback).
func (s *Service) ResolveLocalMediaInfo(ctx context.Context, mediaFileID string) (*LocalMediaInfo, error) {
	rec, err := s.resolveMediaRecord(ctx, mediaFileID, false)
	if err != nil {
		return nil, err
	}

	provider, err := s.registry.ProviderFor(rec.Driver, rec.RootPath, rec.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("storage provider error: %w", err)
	}
	localResolver, ok := provider.(storage.LocalPathResolver)
	if !ok {
		return nil, errLocalAccessRequired
	}
	absPath, err := localResolver.AbsPath(rec.RelativePath)
	if err != nil {
		return nil, err
	}

	embedded, err := parseEmbeddedStreams(rec.MediaFileID, rec.StreamsJSON)
	if err != nil {
		return nil, err
	}
	hasAudio := len(embedded) == 0
	for _, st := range embedded {
		if strings.EqualFold(strings.TrimSpace(st.CodecType), "audio") {
			hasAudio = true
			break
		}
	}

	return &LocalMediaInfo{
		MediaFileID: rec.MediaFileID,
		AbsPath:     absPath,
		DurationMs:  rec.DurationMs,
		HasAudio:    hasAudio,
	}, nil
}

func sortSubtitlesByPriority(subs []StreamDescriptor) {
	sort.SliceStable(subs, func(i, j int) bool {
		if subs[i].IsDefault != subs[j].IsDefault {
			return subs[i].IsDefault
		}
		zhI := isChineseSubtitle(subs[i])
		zhJ := isChineseSubtitle(subs[j])
		if zhI != zhJ {
			return zhI
		}
		return false
	})
}

func isChineseSubtitle(st StreamDescriptor) bool {
	lang := strings.ToLower(strings.TrimSpace(st.Language))
	lang = strings.ReplaceAll(lang, "_", "-")
	switch lang {
	case "zh", "chi", "zho", "chs", "cht", "yue", "cmn", "cn", "chinese":
		return true
	}
	if strings.HasPrefix(lang, "zh-") || strings.HasPrefix(lang, "chi-") || strings.HasPrefix(lang, "zho-") {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(st.Title))
	for _, kw := range []string{"中文", "简体", "繁体", "繁體", "双语", "chinese"} {
		if strings.Contains(title, kw) {
			return true
		}
	}
	return false
}

func formatSubtitleSourceLabel(prefix, language string) string {
	lang := strings.TrimSpace(language)
	if lang == "" {
		return prefix
	}
	return fmt.Sprintf("%s (%s)", prefix, lang)
}

func hasWebVTTContent(vtt string) bool {
	trimmed := strings.TrimSpace(vtt)
	if trimmed == "" {
		return false
	}
	if len(trimmed) >= 6 && strings.EqualFold(trimmed[:6], "WEBVTT") {
		return strings.TrimSpace(trimmed[6:]) != ""
	}
	return true
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
