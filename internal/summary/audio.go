package summary

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"coursefin/internal/id"
)

// DefaultAudioSegmentSeconds is the default 5-minute segment length (~1.8 MB MP3 at 48kbps),
// keeping per-chunk local/CPU Whisper inference fast and checkpoint granularity fine.
const (
	DefaultAudioSegmentSeconds = 300
	MinAudioSegmentSeconds     = 60
	MaxAudioSegmentSeconds     = 1200
)

// AudioChunk describes an extracted audio segment file and its timeline offset within the source media.
type AudioChunk struct {
	Path        string
	OffsetSec   float64
	DurationSec float64
}

// AudioExtractor extracts audio chunks for speech-to-text transcription and provides a cleanup hook
// that removes any temporary files created during extraction.
type AudioExtractor interface {
	ExtractChunks(ctx context.Context, mediaPath string, jobID string) ([]AudioChunk, func(), error)
}

// FFmpegAudioExtractor extracts low-bitrate mono MP3 chunks under {dataDir}/cache/asr/{jobID}.
type FFmpegAudioExtractor struct {
	dataDir    string
	ffmpegBin  string
	ffprobeBin string
	segmentSec int
}

// NewFFmpegAudioExtractor constructs an AudioExtractor backed by ffmpeg and ffprobe.
func NewFFmpegAudioExtractor(dataDir, ffmpegBin, ffprobeBin string, segmentSec int) *FFmpegAudioExtractor {
	if segmentSec <= 0 {
		segmentSec = DefaultAudioSegmentSeconds
	}
	return &FFmpegAudioExtractor{
		dataDir:    dataDir,
		ffmpegBin:  strings.TrimSpace(ffmpegBin),
		ffprobeBin: strings.TrimSpace(ffprobeBin),
		segmentSec: segmentSec,
	}
}

// ExtractChunks extracts 16kHz mono 48kbps MP3 segments from mediaPath into {dataDir}/cache/asr/{jobID}.
// The returned cleanup function removes the job temporary directory and should always be deferred by callers.
func (e *FFmpegAudioExtractor) ExtractChunks(ctx context.Context, mediaPath string, jobID string) ([]AudioChunk, func(), error) {
	if e == nil || e.ffmpegBin == "" {
		return nil, func() {}, errors.New("summary: ffmpeg binary is not configured")
	}
	if strings.TrimSpace(e.dataDir) == "" {
		return nil, func() {}, errors.New("summary: data directory is required for asr temp cache")
	}
	if strings.TrimSpace(mediaPath) == "" {
		return nil, func() {}, errors.New("summary: media path is required")
	}

	safeJobID := sanitizeJobDirName(jobID)
	workDir := filepath.Join(e.dataDir, "cache", "asr", safeJobID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, func() {}, fmt.Errorf("create asr workdir: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(workDir)
	}

	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, cleanup, err
	}

	segSec := e.segmentSec
	if segSec <= 0 {
		segSec = DefaultAudioSegmentSeconds
	}

	outPattern := filepath.Join(workDir, "chunk_%03d.mp3")
	cmd := exec.CommandContext(
		ctx,
		e.ffmpegBin,
		"-hide_banner",
		"-loglevel", "error",
		"-i", mediaPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-b:a", "48k",
		"-f", "segment",
		"-segment_time", strconv.Itoa(segSec),
		"-reset_timestamps", "1",
		outPattern,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		if ctx.Err() != nil {
			return nil, cleanup, ctx.Err()
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return nil, cleanup, fmt.Errorf("ffmpeg extract audio: %w (%s)", err, errMsg)
		}
		return nil, cleanup, fmt.Errorf("ffmpeg extract audio: %w", err)
	}

	entries, err := os.ReadDir(workDir)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("read asr workdir: %w", err)
	}

	var chunkNames []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "chunk_") && strings.HasSuffix(name, ".mp3") {
			chunkNames = append(chunkNames, name)
		}
	}
	sort.Strings(chunkNames)

	if len(chunkNames) == 0 {
		cleanup()
		return nil, cleanup, errors.New("ffmpeg produced no audio chunks")
	}

	chunks := make([]AudioChunk, 0, len(chunkNames))
	offsetSec := 0.0
	for _, name := range chunkNames {
		if err := ctx.Err(); err != nil {
			cleanup()
			return nil, cleanup, err
		}
		fullPath := filepath.Join(workDir, name)
		dur := e.probeDurationSec(ctx, fullPath, float64(segSec))
		if err := ctx.Err(); err != nil {
			cleanup()
			return nil, cleanup, err
		}
		chunks = append(chunks, AudioChunk{
			Path:        fullPath,
			OffsetSec:   offsetSec,
			DurationSec: dur,
		})
		offsetSec += dur
	}

	return chunks, cleanup, nil
}

func (e *FFmpegAudioExtractor) probeDurationSec(ctx context.Context, chunkPath string, fallbackSec float64) float64 {
	if e.ffprobeBin == "" {
		return fallbackSec
	}
	cmd := exec.CommandContext(
		ctx,
		e.ffprobeBin,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		chunkPath,
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fallbackSec
	}
	dur, err := strconv.ParseFloat(strings.TrimSpace(stdout.String()), 64)
	if err != nil || math.IsNaN(dur) || math.IsInf(dur, 0) || dur <= 0 {
		return fallbackSec
	}
	return dur
}

func sanitizeJobDirName(raw string) string {
	base := filepath.Base(strings.TrimSpace(raw))
	if base == "" || base == "." || base == ".." || base == "/" || base == string(filepath.Separator) {
		return id.New()
	}
	return base
}
