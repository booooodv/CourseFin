package jellyfin

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"coursefin/internal/catalog"
	"coursefin/internal/id"
	"coursefin/internal/media"
)

const ticksPerMillisecond int64 = 10000

// EvaluatePlaybackInfo negotiates DirectPlay vs HLS remuxing and builds MediaSources
// with aligned MediaStream indices and subtitle DeliveryUrls.
func EvaluatePlaybackInfo(lesson *catalog.LessonDetail, mediaFile *catalog.MediaFile, streams []media.StreamDescriptor, req PlaybackInfoRequest, token string) PlaybackInfoResponse {
	playSessionID := id.New()

	var lessonID, mfID, title, container, videoCodec, audioCodec string
	var durationMs, fileSize, bitrate int64
	var width, height int

	if lesson != nil {
		lessonID = lesson.ID
		mfID = lesson.MediaFileID
		title = lesson.Title
		container = lesson.Container
		videoCodec = lesson.VideoCodec
		audioCodec = lesson.AudioCodec
		durationMs = lesson.DurationMs
		width = lesson.Width
		height = lesson.Height
		bitrate = lesson.Bitrate
	}
	if mediaFile != nil {
		if mediaFile.ID != "" {
			mfID = mediaFile.ID
		}
		if lessonID == "" {
			lessonID = mediaFile.ID
		}
		if title == "" {
			title = mediaFile.FileName
		}
		if mediaFile.Container != "" {
			container = mediaFile.Container
		}
		if container == "" {
			container = strings.TrimPrefix(path.Ext(mediaFile.FileName), ".")
			if container == "" {
				container = strings.TrimPrefix(path.Ext(mediaFile.RelativePath), ".")
			}
		}
		if mediaFile.VideoCodec != "" {
			videoCodec = mediaFile.VideoCodec
		}
		if mediaFile.AudioCodec != "" {
			audioCodec = mediaFile.AudioCodec
		}
		if mediaFile.DurationMs > 0 {
			durationMs = mediaFile.DurationMs
		}
		if mediaFile.FileSize > 0 {
			fileSize = mediaFile.FileSize
		}
		if mediaFile.Width > 0 {
			width = mediaFile.Width
		}
		if mediaFile.Height > 0 {
			height = mediaFile.Height
		}
		if mediaFile.Bitrate > 0 {
			bitrate = mediaFile.Bitrate
		}
	}

	ext := normalizeContainer(container)
	if ext == "" {
		ext = "mp4"
	}
	if title == "" {
		title = lessonID
	}

	supportsDirectPlay := canDirectPlay(req, ext, videoCodec, audioCodec)
	supportsTranscoding := false

	if !supportsDirectPlay && canTranscodeHLS(req, videoCodec) {
		supportsTranscoding = true
	}

	ms := MediaSourceInfo{
		Protocol:              "File",
		Id:                    mfID,
		Type:                  "Default",
		Container:             ext,
		Size:                  fileSize,
		Name:                  title,
		IsRemote:              false,
		RunTimeTicks:          durationMs * ticksPerMillisecond,
		ReadAtNativeFramerate: false,
		SupportsDirectPlay:    supportsDirectPlay,
		SupportsDirectStream:  supportsDirectPlay,
		SupportsTranscoding:   supportsTranscoding,
		SupportsProbing:       true,
		Bitrate:               bitrate,
	}

	if supportsDirectPlay {
		ms.DirectStreamUrl = buildDirectStreamURL(lessonID, ext, mfID, token)
	} else if supportsTranscoding {
		ms.TranscodingUrl = buildTranscodingURL(lessonID, playSessionID, mfID, token)
		ms.TranscodingSubProtocol = "hls"
		ms.TranscodingContainer = "ts"
	}

	mediaStreams, defAudio, defSub := buildMediaStreams(lessonID, mfID, streams, videoCodec, audioCodec, width, height, bitrate, supportsDirectPlay, supportsTranscoding, req.DeviceProfile, token)
	ms.MediaStreams = mediaStreams
	ms.DefaultAudioStreamIndex = defAudio
	ms.DefaultSubtitleStreamIndex = defSub

	if req.AudioStreamIndex != nil {
		idx := *req.AudioStreamIndex
		ms.DefaultAudioStreamIndex = &idx
	}
	if req.SubtitleStreamIndex != nil {
		idx := *req.SubtitleStreamIndex
		ms.DefaultSubtitleStreamIndex = &idx
	}

	resp := PlaybackInfoResponse{
		MediaSources:  []MediaSourceInfo{ms},
		PlaySessionId: playSessionID,
	}
	if !supportsDirectPlay && !supportsTranscoding {
		resp.ErrorCode = "NotAllowed"
	}
	return resp
}

func canDirectPlay(req PlaybackInfoRequest, container, videoCodec, audioCodec string) bool {
	if req.EnableDirectPlay != nil && !*req.EnableDirectPlay {
		return false
	}

	// Clients like Infuse / VidHub / SenPlayer omit DeviceProfile or send empty DirectPlayProfiles.
	if req.DeviceProfile == nil || len(req.DeviceProfile.DirectPlayProfiles) == 0 {
		return true
	}

	for _, prof := range req.DeviceProfile.DirectPlayProfiles {
		if prof.Type != "" && !strings.EqualFold(prof.Type, "Video") {
			continue
		}
		if !matchContainerList(prof.Container, container) {
			continue
		}
		if videoCodec != "" && !matchCodecList(prof.VideoCodec, videoCodec, normalizeVideoCodec) {
			continue
		}
		if audioCodec != "" && !matchCodecList(prof.AudioCodec, audioCodec, normalizeAudioCodec) {
			continue
		}
		return true
	}
	return false
}

func canTranscodeHLS(req PlaybackInfoRequest, videoCodec string) bool {
	if req.EnableTranscoding != nil && !*req.EnableTranscoding {
		return false
	}
	if videoCodec != "" && !media.CanRemuxVideoCodec(videoCodec) {
		return false
	}
	if req.DeviceProfile == nil || len(req.DeviceProfile.TranscodingProfiles) == 0 {
		return true
	}

	for _, prof := range req.DeviceProfile.TranscodingProfiles {
		if prof.Type != "" && !strings.EqualFold(prof.Type, "Video") {
			continue
		}
		if prof.Protocol != "" && !strings.EqualFold(prof.Protocol, "hls") {
			continue
		}
		if videoCodec != "" && !matchCodecList(prof.VideoCodec, videoCodec, normalizeVideoCodec) {
			continue
		}
		return true
	}
	return false
}

func buildDirectStreamURL(lessonID, ext, mediaFileID, token string) string {
	q := url.Values{}
	q.Set("Static", "true")
	if mediaFileID != "" {
		q.Set("MediaSourceId", mediaFileID)
	}
	if token != "" {
		q.Set("api_key", token)
	}
	return fmt.Sprintf("/Videos/%s/stream.%s?%s", lessonID, ext, q.Encode())
}

func buildTranscodingURL(lessonID, playSessionID, mediaFileID, token string) string {
	q := url.Values{}
	q.Set("PlaySessionId", playSessionID)
	if mediaFileID != "" {
		q.Set("MediaSourceId", mediaFileID)
	}
	if token != "" {
		q.Set("api_key", token)
	}
	return fmt.Sprintf("/Videos/%s/master.m3u8?%s", lessonID, q.Encode())
}

func buildSubtitleDeliveryURL(lessonID, mediaFileID string, streamIdx int, format, token string) string {
	u := fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/Stream.%s", lessonID, mediaFileID, streamIdx, format)
	if token != "" {
		u += "?api_key=" + url.QueryEscape(token)
	}
	return u
}

func buildMediaStreams(
	lessonID, mediaFileID string,
	streams []media.StreamDescriptor,
	videoCodec, audioCodec string,
	width, height int,
	bitrate int64,
	supportsDirectPlay, supportsTranscoding bool,
	profile *DeviceProfile,
	token string,
) ([]MediaStream, *int, *int) {
	out := make([]MediaStream, 0, len(streams)+2)
	var defAudio *int
	var defSub *int
	hasVideo := false
	hasAudio := false
	maxIdx := -1

	for _, sd := range streams {
		if sd.Index > maxIdx {
			maxIdx = sd.Index
		}
		switch strings.ToLower(sd.Type) {
		case "video":
			hasVideo = true
			display := sd.Title
			if display == "" {
				display = formatVideoDisplayTitle(sd.Width, sd.Height, sd.Codec)
			}
			out = append(out, MediaStream{
				Codec:        sd.Codec,
				Language:     sd.Language,
				Title:        sd.Title,
				DisplayTitle: display,
				Index:        sd.Index,
				IsDefault:    sd.IsDefault,
				IsForced:     sd.IsForced,
				IsExternal:   false,
				Type:         "Video",
				Width:        sd.Width,
				Height:       sd.Height,
				BitRate:      sd.Bitrate,
			})
		case "audio":
			hasAudio = true
			if defAudio == nil || sd.IsDefault {
				idx := sd.Index
				defAudio = &idx
			}
			display := sd.Title
			if display == "" {
				display = formatAudioDisplayTitle(sd.Language, sd.Codec)
			}
			out = append(out, MediaStream{
				Codec:        sd.Codec,
				Language:     sd.Language,
				Title:        sd.Title,
				DisplayTitle: display,
				Index:        sd.Index,
				IsDefault:    sd.IsDefault,
				IsForced:     sd.IsForced,
				IsExternal:   false,
				Type:         "Audio",
				Channels:     sd.Channels,
				SampleRate:   sd.SampleRate,
				BitRate:      sd.Bitrate,
			})
		case "subtitle":
			isText := !isBitmapSubtitle(sd.Codec)
			if sd.IsDefault && defSub == nil {
				idx := sd.Index
				defSub = &idx
			}
			display := sd.Title
			if display == "" {
				if sd.Language != "" {
					display = sd.Language
				} else {
					display = "Subtitle"
				}
			}

			ms := MediaStream{
				Codec:                  sd.Codec,
				Language:               sd.Language,
				Title:                  sd.Title,
				DisplayTitle:           display,
				Index:                  sd.Index,
				IsDefault:              sd.IsDefault,
				IsForced:               sd.IsForced,
				IsExternal:             sd.IsExternal,
				Type:                   "Subtitle",
				IsTextSubtitleStream:   isText,
				SupportsExternalStream: isText,
			}

			if sd.IsExternal {
				targetFmt := chooseExternalSubtitleFormat(sd.Format, profile)
				ms.DeliveryMethod = "External"
				ms.DeliveryUrl = buildSubtitleDeliveryURL(lessonID, mediaFileID, sd.Index, targetFmt, token)
			} else if !supportsDirectPlay && supportsTranscoding {
				if isText {
					ms.DeliveryMethod = "External"
					ms.DeliveryUrl = buildSubtitleDeliveryURL(lessonID, mediaFileID, sd.Index, "vtt", token)
				} else {
					ms.DeliveryMethod = "Drop"
				}
			} else {
				ms.DeliveryMethod = "Embed"
			}

			out = append(out, ms)
		}
	}

	if !hasVideo && videoCodec != "" {
		idx := maxIdx + 1
		maxIdx = idx
		out = append(out, MediaStream{
			Codec:        strings.ToLower(videoCodec),
			DisplayTitle: formatVideoDisplayTitle(width, height, videoCodec),
			Index:        idx,
			IsDefault:    true,
			Type:         "Video",
			Width:        width,
			Height:       height,
			BitRate:      bitrate,
		})
	}
	if !hasAudio && audioCodec != "" {
		idx := maxIdx + 1
		maxIdx = idx
		defAudio = &idx
		out = append(out, MediaStream{
			Codec:        strings.ToLower(audioCodec),
			DisplayTitle: formatAudioDisplayTitle("", audioCodec),
			Index:        idx,
			IsDefault:    true,
			Type:         "Audio",
		})
	}

	return out, defAudio, defSub
}

func chooseExternalSubtitleFormat(srcFormat string, profile *DeviceProfile) string {
	src := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(srcFormat), "."))
	if profile != nil && len(profile.SubtitleProfiles) > 0 {
		supportsVTT := false
		supportsSrc := false
		for _, sp := range profile.SubtitleProfiles {
			if sp.Method != "" && !strings.EqualFold(sp.Method, "External") {
				continue
			}
			f := strings.ToLower(strings.TrimSpace(sp.Format))
			if f == "vtt" || f == "webvtt" {
				supportsVTT = true
			}
			if src != "" && f == src {
				supportsSrc = true
			}
		}
		if supportsVTT {
			return "vtt"
		}
		if supportsSrc {
			return src
		}
	}
	return "vtt"
}

func matchContainerList(allowedList, actual string) bool {
	if strings.TrimSpace(allowedList) == "" {
		return true
	}
	normActual := normalizeContainer(actual)
	for _, item := range strings.Split(allowedList, ",") {
		if normalizeContainer(item) == normActual {
			return true
		}
	}
	return false
}

func matchCodecList(allowedList, actual string, normFn func(string) string) bool {
	if strings.TrimSpace(allowedList) == "" {
		return true
	}
	normActual := normFn(actual)
	for _, item := range strings.Split(allowedList, ",") {
		if normFn(item) == normActual {
			return true
		}
	}
	return false
}

func normalizeContainer(c string) string {
	v := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(c), "."))
	switch v {
	case "matroska":
		return "mkv"
	case "mpegts":
		return "ts"
	default:
		return v
	}
}

func normalizeVideoCodec(c string) string {
	v := strings.ToLower(strings.TrimSpace(c))
	switch v {
	case "avc", "avc1":
		return "h264"
	case "h265", "hvc1", "hev1":
		return "hevc"
	case "av01":
		return "av1"
	case "vp09":
		return "vp9"
	default:
		return v
	}
}

func normalizeAudioCodec(c string) string {
	v := strings.ToLower(strings.TrimSpace(c))
	switch v {
	case "mp4a", "mp4a-40-2":
		return "aac"
	case "ac-3":
		return "ac3"
	case "ec-3", "eac-3":
		return "eac3"
	default:
		return v
	}
}

func isBitmapSubtitle(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "dvd_subtitle", "hdmv_pgs_subtitle", "pgssub", "dvb_subtitle", "xsub", "dvdsub":
		return true
	default:
		return false
	}
}

func formatVideoDisplayTitle(w, h int, codec string) string {
	parts := make([]string, 0, 2)
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dp", h))
	} else if w > 0 {
		parts = append(parts, fmt.Sprintf("%dw", w))
	}
	if codec != "" {
		parts = append(parts, strings.ToUpper(codec))
	}
	if len(parts) == 0 {
		return "Video"
	}
	return strings.Join(parts, " ")
}

func formatAudioDisplayTitle(lang, codec string) string {
	parts := make([]string, 0, 2)
	if lang != "" {
		parts = append(parts, lang)
	}
	if codec != "" {
		parts = append(parts, strings.ToUpper(codec))
	}
	if len(parts) == 0 {
		return "Audio"
	}
	return strings.Join(parts, " ")
}
