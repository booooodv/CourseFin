package scanner

import (
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
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

var subtitleDirNames = map[string]bool{
	"subs":      true,
	"subtitles": true,
	"subtitle":  true,
	"sub":       true,
	"字幕":        true,
	"中文字幕":      true,
	"英文字幕":      true,
	"srt":       true,
	"vtt":       true,
	"ass":       true,
}

var langAliases = map[string]struct {
	code  string
	title string
}{
	"zh":          {code: "zh", title: "中文"},
	"chs":         {code: "zh", title: "简体中文"},
	"cht":         {code: "zh", title: "繁體中文"},
	"zh-cn":       {code: "zh", title: "简体中文"},
	"zh_cn":       {code: "zh", title: "简体中文"},
	"zh-tw":       {code: "zh", title: "繁體中文"},
	"zh_tw":       {code: "zh", title: "繁體中文"},
	"zh-hk":       {code: "zh", title: "繁體中文"},
	"zh_hk":       {code: "zh", title: "繁體中文"},
	"zh-sg":       {code: "zh", title: "简体中文"},
	"zh_sg":       {code: "zh", title: "简体中文"},
	"zh-hans":     {code: "zh", title: "简体中文"},
	"zh_hans":     {code: "zh", title: "简体中文"},
	"zh-hant":     {code: "zh", title: "繁體中文"},
	"zh_hant":     {code: "zh", title: "繁體中文"},
	"zh-chs":      {code: "zh", title: "简体中文"},
	"zh-cht":      {code: "zh", title: "繁體中文"},
	"ai-zh":       {code: "zh", title: "中文"},
	"chi":         {code: "zh", title: "中文"},
	"zho":         {code: "zh", title: "中文"},
	"cn":          {code: "zh", title: "中文"},
	"sc":          {code: "zh", title: "简体中文"},
	"tc":          {code: "zh", title: "繁體中文"},
	"chinese":     {code: "zh", title: "中文"},
	"simplified":  {code: "zh", title: "简体中文"},
	"traditional": {code: "zh", title: "繁體中文"},
	"简体":          {code: "zh", title: "简体中文"},
	"繁体":          {code: "zh", title: "繁體中文"},
	"简体中文":        {code: "zh", title: "简体中文"},
	"繁体中文":        {code: "zh", title: "繁體中文"},
	"繁體中文":        {code: "zh", title: "繁體中文"},
	"中文":          {code: "zh", title: "中文"},
	"中文字幕":        {code: "zh", title: "中文字幕"},
	"中文(简体)":      {code: "zh", title: "简体中文"},
	"中文(繁体)":      {code: "zh", title: "繁體中文"},
	"中文（简体）":      {code: "zh", title: "简体中文"},
	"中文（繁体）":      {code: "zh", title: "繁體中文"},
	"中英":          {code: "zh", title: "中英双语"},
	"中英双语":        {code: "zh", title: "中英双语"},
	"双语":          {code: "zh", title: "双语"},
	"双语字幕":        {code: "zh", title: "双语字幕"},
	"bilingual":   {code: "zh", title: "中英双语"},
	"chs&eng":     {code: "zh", title: "中英双语"},
	"cht&eng":     {code: "zh", title: "繁英双语"},
	"zh&en":       {code: "zh", title: "中英双语"},
	"zh-en":       {code: "zh", title: "中英双语"},
	"chs_eng":     {code: "zh", title: "中英双语"},
	"cht_eng":     {code: "zh", title: "繁英双语"},

	"en":      {code: "en", title: "English"},
	"eng":     {code: "en", title: "English"},
	"en-us":   {code: "en", title: "English"},
	"en_us":   {code: "en", title: "English"},
	"en-gb":   {code: "en", title: "English"},
	"en_gb":   {code: "en", title: "English"},
	"en-au":   {code: "en", title: "English"},
	"en-ca":   {code: "en", title: "English"},
	"en-orig": {code: "en", title: "English"},
	"ai-en":   {code: "en", title: "English"},
	"english": {code: "en", title: "English"},
	"英文":      {code: "en", title: "English"},
	"英语":      {code: "en", title: "English"},
	"英文字幕":    {code: "en", title: "英文字幕"},

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
	"default":    true,
	"forced":     true,
	"sdh":        true,
	"cc":         true,
	"hi":         true,
	"auto":       true,
	"orig":       true,
	"ai":         true,
	"asr":        true,
	"official":   true,
	"translated": true,
	"machine":    true,
}

var trailingQualityTagRe = regexp.MustCompile(`(?i)[\s._-]*(?:[\[\(【（]\s*(?:2160p|1440p|1080p|720p|480p|360p|4k|8k|hd|fhd|uhd|x264|x265|h\.?264|h\.?265|hevc|av1|vp9|aac|web-?dl|webrip|bluray|bdrip|remux)\s*[\]\)】）]|(?:[._-])(?:2160p|1440p|1080p|720p|480p|360p|4k|8k|x264|x265|h264|h265|hevc|av1|vp9|web-?dl|webrip|bluray))$`)

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

// IsSubtitleDirName reports whether a directory basename is a recognized sidecar subtitle folder.
func IsSubtitleDirName(name string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(norm.NFC.String(name)))
	return subtitleDirNames[cleaned]
}

// StripVideoQualityTags strips common trailing resolution/codec tags (e.g. "[1080p]", ".720p")
// from a video filename stem for fallback subtitle matching.
func StripVideoQualityTags(stem string) string {
	cur := strings.TrimSpace(norm.NFC.String(stem))
	for {
		next := strings.TrimSpace(trailingQualityTagRe.ReplaceAllString(cur, ""))
		if next == "" || next == cur {
			break
		}
		cur = next
	}
	return cur
}

// isoLangCodes enumerates valid ISO 639-1 (2-letter) and common ISO 639-2 (3-letter)
// language codes mapped to canonical codes, avoiding false-positive matches on arbitrary
// 2–3 letter English words like "go", "web", "for", "new", or "api".
var isoLangCodes = map[string]string{
	// ISO 639-1
	"af": "af", "ar": "ar", "az": "az", "be": "be", "bg": "bg", "bn": "bn",
	"bs": "bs", "ca": "ca", "cs": "cs", "cy": "cy", "da": "da", "de": "de",
	"el": "el", "en": "en", "eo": "eo", "es": "es", "et": "et", "eu": "eu",
	"fa": "fa", "fi": "fi", "fr": "fr", "ga": "ga", "gl": "gl", "gu": "gu",
	"he": "he", "hi": "hi", "hr": "hr", "hu": "hu", "hy": "hy", "id": "id",
	"is": "is", "it": "it", "ja": "ja", "ka": "ka", "kk": "kk", "km": "km",
	"kn": "kn", "ko": "ko", "la": "la", "lo": "lo", "lt": "lt", "lv": "lv",
	"mk": "mk", "ml": "ml", "mn": "mn", "mr": "mr", "ms": "ms", "my": "my",
	"nb": "nb", "ne": "ne", "nl": "nl", "nn": "nn", "no": "no", "pa": "pa",
	"pl": "pl", "pt": "pt", "ro": "ro", "ru": "ru", "si": "si", "sk": "sk",
	"sl": "sl", "sq": "sq", "sr": "sr", "sv": "sv", "sw": "sw", "ta": "ta",
	"te": "te", "th": "th", "tl": "tl", "tr": "tr", "uk": "uk", "ur": "ur",
	"uz": "uz", "vi": "vi", "zh": "zh",
	// Common ISO 639-2/B/T & BCP 47 3-letter codes
	"ara": "ar", "bul": "bg", "cat": "ca", "ces": "cs", "cze": "cs",
	"chi": "zh", "zho": "zh", "dan": "da", "deu": "de", "ger": "de",
	"dut": "nl", "nld": "nl", "ell": "el", "gre": "el", "eng": "en",
	"est": "et", "fas": "fa", "per": "fa", "fil": "fil", "fin": "fi",
	"fra": "fr", "fre": "fr", "heb": "he", "hin": "hi", "hrv": "hr",
	"hun": "hu", "ind": "id", "isl": "is", "ice": "is", "ita": "it",
	"jpn": "ja", "kor": "ko", "lav": "lv", "lit": "lt", "may": "ms",
	"msa": "ms", "nor": "no", "pol": "pl", "por": "pt", "ron": "ro",
	"rum": "ro", "rus": "ru", "slk": "sk", "slo": "sk", "slv": "sl",
	"spa": "es", "srp": "sr", "swe": "sv", "tam": "ta", "tel": "te",
	"tha": "th", "tur": "tr", "ukr": "uk", "und": "und", "urd": "ur",
	"vie": "vi", "yue": "zh",
}

// NormalizeSubtitleLang maps common language tags (e.g. zh, chs, cht, zh-cn, en, ja)
// to canonical ISO language codes.
func NormalizeSubtitleLang(tag string) (string, bool) {
	normTag := strings.ToLower(strings.TrimSpace(norm.NFC.String(tag)))
	if normTag == "" {
		return "", false
	}
	if entry, ok := langAliases[normTag]; ok {
		return entry.code, true
	}
	if code, ok := isoLangCodes[normTag]; ok {
		return code, true
	}
	return "", false
}

// MatchSubtitle checks whether subRelPath is a matching sidecar subtitle for videoRelPath
// in the same directory or an immediate recognized subtitle child directory.
func MatchSubtitle(videoRelPath, subRelPath string) (SubtitleMatch, bool) {
	videoDir := norm.NFC.String(path.Dir(videoRelPath))
	subDir := norm.NFC.String(path.Dir(subRelPath))

	sameDir := videoDir == subDir
	inSubChildDir := !sameDir && path.Dir(subDir) == videoDir && IsSubtitleDirName(path.Base(subDir))
	if !sameDir && !inSubChildDir {
		return SubtitleMatch{}, false
	}

	return matchSubtitleByStem(videoRelPath, subRelPath, true)
}

func matchSubtitleByStem(videoRelPath, subRelPath string, allowQualityStrip bool) (SubtitleMatch, bool) {
	videoExt := path.Ext(videoRelPath)
	if !IsVideoExt(videoExt) {
		return SubtitleMatch{}, false
	}
	subExt := path.Ext(subRelPath)
	if !IsSubtitleExt(subExt) {
		return SubtitleMatch{}, false
	}

	videoBase := norm.NFC.String(path.Base(videoRelPath))
	videoStem := strings.TrimSuffix(videoBase, norm.NFC.String(videoExt))
	subBase := norm.NFC.String(path.Base(subRelPath))
	subStem := strings.TrimSuffix(subBase, norm.NFC.String(subExt))
	format := strings.TrimPrefix(strings.ToLower(subExt), ".")

	if m, ok := matchStemPair(videoStem, subStem, subRelPath, format); ok {
		return m, true
	}

	if allowQualityStrip {
		cleanVideoStem := StripVideoQualityTags(videoStem)
		if cleanVideoStem != "" && !strings.EqualFold(cleanVideoStem, videoStem) {
			if m, ok := matchStemPair(cleanVideoStem, subStem, subRelPath, format); ok {
				return m, true
			}
		}
	}

	return SubtitleMatch{}, false
}

func matchStemPair(videoStem, subStem, subRelPath, format string) (SubtitleMatch, bool) {
	if strings.EqualFold(subStem, videoStem) {
		return SubtitleMatch{
			RelPath:  subRelPath,
			Language: "und",
			Format:   format,
		}, true
	}

	rest, ok := trimFoldPrefix(subStem, videoStem)
	if !ok || rest == "" {
		return SubtitleMatch{}, false
	}

	delim, restAfterDelim, ok := splitLeadingSubtitleDelimiter(rest)
	if !ok {
		return SubtitleMatch{}, false
	}

	lang, title, isDefault, ok := parseSubtitleSuffix(restAfterDelim, delim)
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

func splitLeadingSubtitleDelimiter(rest string) (delim rune, after string, ok bool) {
	if rest == "" {
		return 0, "", false
	}
	var first rune
	var width int
	for i, r := range rest {
		if i == 0 {
			first = r
			width = len(string(r))
			break
		}
	}
	switch first {
	case '.', '_', '-', ' ':
		return first, strings.TrimSpace(rest[width:]), true
	case '[', '(', '【', '（':
		return first, strings.TrimSpace(rest), true
	default:
		return 0, "", false
	}
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

func parseSubtitleSuffix(rawSuffix string, delim rune) (lang, title string, isDefault, ok bool) {
	cleaned := cleanSuffixWrapper(rawSuffix)
	if cleaned == "" {
		return "", "", false, false
	}

	// Guard against matching episode numbers like 01_2.srt or 01-02.srt to 01.mp4.
	if isPureDigitsOrNumericRange(cleaned) {
		return "", "", false, false
	}

	lang = "und"
	lowerFull := strings.ToLower(cleaned)

	// Direct whole-suffix alias check (handles e.g. "简体中文", "中英双语", "en-orig", "ai-zh", "中文(简体)").
	if alias, exists := langAliases[lowerFull]; exists {
		return alias.code, alias.title, false, true
	}

	tokens := splitSuffixTokens(cleaned)
	if len(tokens) == 0 {
		return "", "", false, false
	}

	// Scan for 2-token compound aliases (e.g. zh-hans, zh-cn, en-orig, ai-zh) and single tokens.
	for i := 0; i < len(tokens); i++ {
		tok := strings.ToLower(strings.TrimSpace(tokens[i]))
		if tok == "" {
			continue
		}
		if i+1 < len(tokens) {
			nextTok := strings.ToLower(strings.TrimSpace(tokens[i+1]))
			for _, joiner := range []string{"-", "_", "&"} {
				compound := tok + joiner + nextTok
				if alias, exists := langAliases[compound]; exists {
					lang = alias.code
					if title == "" {
						title = alias.title
					}
					i++
					goto nextToken
				}
			}
		}
		if subtitleModifiers[tok] {
			if tok == "default" {
				isDefault = true
			}
			continue
		}
		if alias, exists := langAliases[tok]; exists {
			lang = alias.code
			if title == "" {
				title = alias.title
			}
			continue
		}
		if normCode, isLang := NormalizeSubtitleLang(tok); isLang {
			if lang == "und" {
				lang = normCode
			}
			if title == "" {
				title = normCode
			}
			continue
		}
	nextToken:
	}

	// Preserve readable suffix as title for custom descriptors (e.g. "机翻校对") instead of discarding.
	if title == "" && !allModifierTokens(tokens) {
		title = cleaned
	}
	_ = delim
	return lang, title, isDefault, true
}

func cleanSuffixWrapper(s string) string {
	cur := strings.TrimSpace(s)
	cur = strings.TrimLeft(cur, "._- ")
	cur = strings.TrimRight(cur, "._- ")
	for {
		trimmed := cur
		if len(trimmed) >= 2 && ((strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) ||
			(strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")"))) {
			cur = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			continue
		}
		if strings.HasPrefix(trimmed, "【") && strings.HasSuffix(trimmed, "】") {
			cur = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "【"), "】"))
			continue
		}
		if strings.HasPrefix(trimmed, "（") && strings.HasSuffix(trimmed, "）") {
			cur = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "（"), "）"))
			continue
		}
		break
	}
	return strings.TrimSpace(cur)
}

func splitSuffixTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '.', '_', '-', ' ', '\t', '[', ']', '(', ')', '【', '】', '（', '）', '/':
			return true
		default:
			return false
		}
	})
}

func allModifierTokens(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, t := range tokens {
		if !subtitleModifiers[strings.ToLower(strings.TrimSpace(t))] {
			return false
		}
	}
	return true
}

func isPureDigitsOrNumericRange(s string) bool {
	hasDigit := false
	for _, r := range s {
		if unicode.IsDigit(r) {
			hasDigit = true
			continue
		}
		if r == '-' || r == '_' || r == '.' || r == ' ' {
			continue
		}
		return false
	}
	return hasDigit
}

// trimFoldPrefix checks whether s starts with prefix under Unicode case-folding
// on rune boundaries (avoiding mid-rune byte slicing when folded runes have different UTF-8 widths).
func trimFoldPrefix(s, prefix string) (string, bool) {
	si, pi := 0, 0
	for pi < len(prefix) {
		if si >= len(s) {
			return "", false
		}
		sr, sw := utf8.DecodeRuneInString(s[si:])
		pr, pw := utf8.DecodeRuneInString(prefix[pi:])
		if sr != pr && !runesEqualFold(sr, pr) {
			return "", false
		}
		si += sw
		pi += pw
	}
	return s[si:], true
}

func runesEqualFold(a, b rune) bool {
	if a == b {
		return true
	}
	if a < utf8.RuneSelf && b < utf8.RuneSelf {
		if 'A' <= a && a <= 'Z' {
			a += 'a' - 'A'
		}
		if 'A' <= b && b <= 'Z' {
			b += 'a' - 'A'
		}
		return a == b
	}
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}
