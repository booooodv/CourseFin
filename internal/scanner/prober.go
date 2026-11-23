package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProbeResult holds normalized media metadata extracted by probing a video file.
type ProbeResult struct {
	DurationMs  int64  `json:"duration_ms"`
	Container   string `json:"container"`
	VideoCodec  string `json:"video_codec"`
	AudioCodec  string `json:"audio_codec"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bitrate     int64  `json:"bitrate"`
	StreamsJSON string `json:"streams_json"`
}

// Prober extracts media stream metadata and frame thumbnails from video files.
type Prober interface {
	Probe(ctx context.Context, absPath string) (*ProbeResult, error)
	ExtractThumbnail(ctx context.Context, absPath string, seekSec float64, outPath string) error
}

// FFmpegProber implements Prober using ffprobe and ffmpeg CLI binaries.
type FFmpegProber struct {
	ffprobeBin string
	ffmpegBin  string
}

// NewFFmpegProber returns an FFmpegProber configured with ffprobe and ffmpeg binary paths.
// Empty paths fall back to PATH lookup and common Homebrew/system locations.
func NewFFmpegProber(ffprobeBin, ffmpegBin string) *FFmpegProber {
	return &FFmpegProber{
		ffprobeBin: resolveBinaryPath(ffprobeBin, "ffprobe"),
		ffmpegBin:  resolveBinaryPath(ffmpegBin, "ffmpeg"),
	}
}

func resolveBinaryPath(configured, defaultName string) string {
	cand := strings.TrimSpace(configured)
	if cand == "" {
		cand = defaultName
	}
	if strings.ContainsRune(cand, filepath.Separator) {
		return cand
	}
	if p, err := exec.LookPath(cand); err == nil {
		return p
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		full := filepath.Join(dir, cand)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full
		}
	}
	return cand
}

type ffprobeOutput struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams json.RawMessage `json:"streams"`
}

type ffprobeStream struct {
	Index       int    `json:"index"`
	CodecType   string `json:"codec_type"`
	CodecName   string `json:"codec_name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Duration    string `json:"duration"`
	Disposition struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

// Probe runs ffprobe on absPath and returns normalized stream and container metadata.
func (p *FFmpegProber) Probe(ctx context.Context, absPath string) (*ProbeResult, error) {
	cmd := exec.CommandContext(
		ctx,
		p.ffprobeBin,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		absPath,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w (stderr: %s)", absPath, err, strings.TrimSpace(stderr.String()))
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("parse ffprobe json for %s: %w", absPath, err)
	}

	var streams []ffprobeStream
	if len(parsed.Streams) > 0 {
		if err := json.Unmarshal(parsed.Streams, &streams); err != nil {
			return nil, fmt.Errorf("parse ffprobe streams for %s: %w", absPath, err)
		}
	}

	res := &ProbeResult{
		Container:   detectContainer(absPath, parsed.Format.FormatName),
		StreamsJSON: "[]",
	}

	if len(parsed.Streams) > 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, parsed.Streams); err == nil && compact.Len() > 0 {
			res.StreamsJSON = compact.String()
		}
	}

	if durSec, err := strconv.ParseFloat(strings.TrimSpace(parsed.Format.Duration), 64); err == nil && durSec > 0 {
		res.DurationMs = int64(math.Round(durSec * 1000))
	}
	if br, err := strconv.ParseInt(strings.TrimSpace(parsed.Format.BitRate), 10, 64); err == nil && br > 0 {
		res.Bitrate = br
	}

	var fallbackVideo *ffprobeStream
	for i := range streams {
		st := &streams[i]
		switch strings.ToLower(st.CodecType) {
		case "video":
			if st.Disposition.AttachedPic == 0 && res.VideoCodec == "" {
				res.VideoCodec = strings.ToLower(st.CodecName)
				res.Width = st.Width
				res.Height = st.Height
			} else if fallbackVideo == nil {
				fallbackVideo = st
			}
			if res.DurationMs == 0 {
				if durSec, err := strconv.ParseFloat(strings.TrimSpace(st.Duration), 64); err == nil && durSec > 0 {
					res.DurationMs = int64(math.Round(durSec * 1000))
				}
			}
		case "audio":
			if res.AudioCodec == "" {
				res.AudioCodec = strings.ToLower(st.CodecName)
			}
		}
	}

	if res.VideoCodec == "" && fallbackVideo != nil {
		res.VideoCodec = strings.ToLower(fallbackVideo.CodecName)
		res.Width = fallbackVideo.Width
		res.Height = fallbackVideo.Height
	}

	return res, nil
}

// ExtractThumbnail captures a single scaled JPEG frame from absPath at seekSec.
func (p *FFmpegProber) ExtractThumbnail(ctx context.Context, absPath string, seekSec float64, outPath string) error {
	if seekSec < 0 {
		seekSec = 0
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create thumbnail dir: %w", err)
	}

	err := p.runThumbnailFFmpeg(ctx, absPath, seekSec, outPath)
	if err != nil && seekSec > 0 {
		// Retry from start in case keyframe seek overshot short media.
		if retryErr := p.runThumbnailFFmpeg(ctx, absPath, 0, outPath); retryErr == nil {
			return nil
		}
	}
	return err
}

func (p *FFmpegProber) runThumbnailFFmpeg(ctx context.Context, absPath string, seekSec float64, outPath string) error {
	seekArg := fmt.Sprintf("%.3f", seekSec)
	cmd := exec.CommandContext(
		ctx,
		p.ffmpegBin,
		"-ss", seekArg,
		"-i", absPath,
		"-frames:v", "1",
		"-vf", "scale='min(640,iw)':-2",
		"-q:v", "4",
		"-y",
		outPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg thumbnail %s: %w (stderr: %s)", absPath, err, strings.TrimSpace(stderr.String()))
	}

	info, err := os.Stat(outPath)
	if err != nil {
		return fmt.Errorf("thumbnail output missing %s: %w", outPath, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("thumbnail output is empty: %s", outPath)
	}
	return nil
}

func detectContainer(absPath, formatName string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(absPath)), ".")
	if ext != "" {
		return ext
	}
	parts := strings.Split(strings.ToLower(formatName), ",")
	if len(parts) > 0 && parts[0] != "" {
		if parts[0] == "matroska" {
			return "mkv"
		}
		return strings.TrimSpace(parts[0])
	}
	return ""
}
