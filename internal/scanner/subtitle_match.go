package scanner

import (
	"path"
	"strings"
)

var videoExtensions = map[string]bool{
	".mp4":  true,
	".mkv":  true,
	".webm": true,
	".mov":  true,
	".m4v":  true,
	".avi":  true,
	".flv":  true,
	".ts":   true,
	".wmv":  true,
	".rmvb": true,
}

var subtitleExtensions = map[string]bool{
	".srt": true,
	".vtt": true,
	".ass": true,
	".ssa": true,
}

var langAliases = map[string]struct {
	code  string
	title string
}{
	"zh":      {code: "zh", title: "中文"},
	"chs":     {code: "zh", title: "简体中文"},
	"cht":     {code: "zh", title: "繁體中文"},
	"zh-cn":   {code: "zh", title: "简体中文"},
	"zh_cn":   {code: "zh", title: "简体中文"},
	"zh-tw":   {code: "zh", title: "繁體中文"},
	"zh_tw":   {code: "zh", title: "繁體中文"},
	"zh-hk":   {code: "zh", title: "繁體中文"},
	"zh_hk":   {code: "zh", title: "繁體中文"},
	"zh-hans": {code: "zh", title: "简体中文"},
	"zh-hant": {code: "zh", title: "繁體中文"},
	"chi":     {code: "zh", title: "中文"},
	"zho":     {code: "zh", title: "中文"},
	"cn":      {code: "zh", title: "中文"},
	"sc":      {code: "zh", title: "简体中文"},
	"tc":      {code: "zh", title: "繁體中文"},
	"简体":      {code: "zh", title: "简体中文"},
	"繁体":      {code: "zh", title: "繁體中文"},
	"中文":      {code: "zh", title: "中文"},

	"en":      {code: "en", title: "English"},
	"eng":     {code: "en", title: "English"},
	"en-us":   {code: "en", title: "English"},
	"en_us":   {code: "en", title: "English"},
	"en-gb":   {code: "en", title: "English"},
	"en_gb":   {code: "en", title: "English"},
	"english": {code: "en", title: "English"},
	"英文":      {code: "en", title: "English"},
	"英语":      {code: "en", title: "English"},

	"ja":       {code: "ja", title: "日本語"},
	"jp":       {code: "ja", title: "日本語"},
	"jpn":      {code: "ja", title: "日本語"},
	"japanese": {code: "ja", title: "日本語"},
	"日语":       {code: "ja", title: "日本語"},
	"日文":       {code: "ja", title: "日本語"},

	"ko":     {code: "ko", title: "한국어"},
	"kor":    {code: "ko", title: "한국어"},
	"korean": {code: "ko", title: "한국어"},
	"韩语":     {code: "ko", title: "한국어"},
	"韩文":     {code: "ko", title: "한국어"},

	"fr":  {code: "fr", title: "Français"},
	"fre": {code: "fr", title: "Français"},
	"fra": {code: "fr", title: "Français"},
	"de":  {code: "de", title: "Deutsch"},
	"ger": {code: "de", title: "Deutsch"},
	"deu": {code: "de", title: "Deutsch"},
	"es":  {code: "es", title: "Español"},
	"spa": {code: "es", title: "Español"},
	"pt":  {code: "pt", title: "Português"},
	"por": {code: "pt", title: "Português"},
	"ru":  {code: "ru", title: "Русский"},
	"rus": {code: "ru", title: "Русский"},
	"it":  {code: "it", title: "Italiano"},
	"ita": {code: "it", title: "Italiano"},
	"ar":  {code: "ar", title: "العربية"},
	"ara": {code: "ar", title: "العربية"},
	"vi":  {code: "vi", title: "Tiếng Việt"},
	"vie": {code: "vi", title: "Tiếng Việt"},
	"th":  {code: "th", title: "ไทย"},
	"tha": {code: "th", title: "ไทย"},
	"id":  {code: "id", title: "Bahasa Indonesia"},
	"ind": {code: "id", title: "Bahasa Indonesia"},
	"und": {code: "und", title: ""},
}

var subtitleModifiers = map[string]bool{
	"default": true,
	"forced":  true,
	"sdh":     true,
	"cc":      true,
	"hi":      true,
}

// SubtitleMatch describes a sidecar subtitle matched to a video file.
type SubtitleMatch struct {
	RelPath   string
	Language  string
	Title     string
	Format    string
	IsDefault bool
}

// IsVideoExt reports whether ext (with or without leading dot) is a supported video extension.
func IsVideoExt(ext string) bool {
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return videoExtensions[strings.ToLower(ext)]
}

// IsSubtitleExt reports whether ext (with or without leading dot) is a supported subtitle extension.
func IsSubtitleExt(ext string) bool {
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return subtitleExtensions[strings.ToLower(ext)]
}

// NormalizeSubtitleLang maps common language tags (e.g. zh, chs, cht, zh-cn, en, ja)
// to canonical ISO language codes.
func NormalizeSubtitleLang(tag string) (string, bool) {
	norm := strings.ToLower(strings.TrimSpace(tag))
	if norm == "" {
		return "", false
	}
	if entry, ok := langAliases[norm]; ok {
		return entry.code, true
	}
	if len(norm) == 2 && isASCIIAlpha(norm) {
		return norm, true
	}
	if len(norm) == 3 && isASCIIAlpha(norm) {
		return norm, true
	}
	return "", false
}

// MatchSubtitle checks whether subRelPath is a matching sidecar subtitle for videoRelPath
// in the same directory.
func MatchSubtitle(videoRelPath, subRelPath string) (SubtitleMatch, bool) {
	if path.Dir(videoRelPath) != path.Dir(subRelPath) {
		return SubtitleMatch{}, false
	}

	videoExt := path.Ext(videoRelPath)
	if !IsVideoExt(videoExt) {
		return SubtitleMatch{}, false
	}
	subExt := path.Ext(subRelPath)
	if !IsSubtitleExt(subExt) {
		return SubtitleMatch{}, false
	}

	videoBase := path.Base(videoRelPath)
	videoStem := strings.TrimSuffix(videoBase, videoExt)
	subBase := path.Base(subRelPath)
	subStem := strings.TrimSuffix(subBase, subExt)
	format := strings.TrimPrefix(strings.ToLower(subExt), ".")

	if strings.EqualFold(subStem, videoStem) {
		return SubtitleMatch{
			RelPath:  subRelPath,
			Language: "und",
			Format:   format,
		}, true
	}

	if len(subStem) <= len(videoStem)+1 {
		return SubtitleMatch{}, false
	}
	if !strings.EqualFold(subStem[:len(videoStem)], videoStem) || subStem[len(videoStem)] != '.' {
		return SubtitleMatch{}, false
	}

	suffix := subStem[len(videoStem)+1:]
	lang, title, isDefault, ok := parseSubtitleSuffix(suffix)
	if !ok {
		return SubtitleMatch{}, false
	}

	return SubtitleMatch{
		RelPath:   subRelPath,
		Language:  lang,
		Title:     title,
		Format:    format,
		IsDefault: isDefault,
	}, true
}

// MatchSubtitlesForVideo returns all matching sidecar subtitles from candidateRelPaths
// for a single video relative path.
func MatchSubtitlesForVideo(videoRelPath string, candidateRelPaths []string) []SubtitleMatch {
	var matches []SubtitleMatch
	for _, cand := range candidateRelPaths {
		if m, ok := MatchSubtitle(videoRelPath, cand); ok {
			matches = append(matches, m)
		}
	}
	return matches
}

func parseSubtitleSuffix(suffix string) (lang, title string, isDefault, ok bool) {
	parts := strings.Split(suffix, ".")
	lang = "und"
	var matched bool

	for _, raw := range parts {
		lower := strings.ToLower(strings.TrimSpace(raw))
		if lower == "" {
			return "", "", false, false
		}
		if subtitleModifiers[lower] {
			if lower == "default" {
				isDefault = true
			}
			matched = true
			continue
		}
		if alias, exists := langAliases[lower]; exists {
			lang = alias.code
			title = alias.title
			matched = true
			continue
		}
		if norm, isLang := NormalizeSubtitleLang(lower); isLang {
			lang = norm
			if title == "" {
				title = norm
			}
			matched = true
			continue
		}
		return "", "", false, false
	}

	if !matched {
		return "", "", false, false
	}
	return lang, title, isDefault, true
}

func isASCIIAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return len(s) > 0
}
