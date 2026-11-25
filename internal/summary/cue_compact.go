package summary

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"coursefin/internal/ai"
)

const (
	// DefaultCompactWindowSec is the default time window for merging adjacent subtitle cues.
	DefaultCompactWindowSec = 15.0
	// DefaultCompactMaxRunes is the default character cap per compacted timestamped line.
	DefaultCompactMaxRunes = 200
	// DefaultSinglePassMaxRunes is the maximum compacted transcript length handled in one LLM call.
	DefaultSinglePassMaxRunes = 24000
	// DefaultMapChunkMaxRunes is the target maximum transcript length per Map-phase chunk.
	DefaultMapChunkMaxRunes = 18000
)

var (
	htmlVTTTagRe = regexp.MustCompile(`<[^>]+>`)
	assTagRe     = regexp.MustCompile(`\{\\[^}]*\}`)
)

// CompactedLine represents one deduplicated, tag-stripped subtitle segment anchored at StartSec.
type CompactedLine struct {
	StartSec  float64
	EndSec    float64
	Text      string
	Formatted string
}

// String returns the formatted timestamped line (e.g. "[05:30] text").
func (l CompactedLine) String() string {
	if l.Formatted != "" {
		return l.Formatted
	}
	return fmt.Sprintf("[%s] %s", FormatTimestamp(l.StartSec), l.Text)
}

// FormatTimestamp formats non-negative seconds as MM:SS, or H:MM:SS when sec >= 3600.
func FormatTimestamp(sec float64) string {
	if math.IsNaN(sec) || math.IsInf(sec, 0) || sec < 0 {
		sec = 0
	}
	totalSec := int(sec)
	if totalSec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", totalSec/3600, (totalSec%3600)/60, totalSec%60)
	}
	return fmt.Sprintf("%02d:%02d", totalSec/60, totalSec%60)
}

func cleanSubtitleLines(raw string) []string {
	norm := strings.ReplaceAll(raw, "\r\n", "\n")
	norm = strings.ReplaceAll(norm, "\r", "\n")
	rawLines := strings.Split(norm, "\n")

	out := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		stripped := assTagRe.ReplaceAllString(line, "")
		stripped = htmlVTTTagRe.ReplaceAllString(stripped, "")
		stripped = html.UnescapeString(stripped)
		collapsed := strings.Join(strings.Fields(stripped), " ")
		if collapsed != "" {
			out = append(out, collapsed)
		}
	}
	return out
}

type cleanedCueItem struct {
	startSec float64
	endSec   float64
	text     string
}

// CompactCues strips HTML/VTT/ASS markup, collapses consecutive duplicate rolling lines,
// and groups cues into compact timestamped lines bounded by windowSec and maxWindowRunes.
func CompactCues(cues []ai.TranscriptCue, windowSec float64, maxWindowRunes int) []CompactedLine {
	if windowSec <= 0 {
		windowSec = DefaultCompactWindowSec
	}
	if maxWindowRunes <= 0 {
		maxWindowRunes = DefaultCompactMaxRunes
	}

	items := make([]cleanedCueItem, 0, len(cues))
	var lastFullCue string
	var lastSubLine string

	for _, cue := range cues {
		subLines := cleanSubtitleLines(cue.Text)
		if len(subLines) == 0 {
			continue
		}
		fullCue := strings.Join(subLines, " ")
		if fullCue == lastFullCue {
			continue
		}
		lastFullCue = fullCue

		kept := make([]string, 0, len(subLines))
		for _, sub := range subLines {
			if sub == lastSubLine {
				continue
			}
			kept = append(kept, sub)
			lastSubLine = sub
		}
		if len(kept) == 0 {
			continue
		}

		start := cue.StartSec
		if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 {
			start = 0
		}
		end := cue.EndSec
		if math.IsNaN(end) || math.IsInf(end, 0) || end < start {
			end = start
		}

		items = append(items, cleanedCueItem{
			startSec: start,
			endSec:   end,
			text:     strings.Join(kept, " "),
		})
	}

	if len(items) == 0 {
		return nil
	}

	out := make([]CompactedLine, 0, len(items))
	var winStart, winEnd float64
	var winParts []string
	winRunes := 0

	flush := func() {
		if len(winParts) == 0 {
			return
		}
		text := strings.Join(winParts, " ")
		out = append(out, CompactedLine{
			StartSec:  winStart,
			EndSec:    winEnd,
			Text:      text,
			Formatted: fmt.Sprintf("[%s] %s", FormatTimestamp(winStart), text),
		})
		winParts = nil
		winRunes = 0
	}

	for _, item := range items {
		itemRunes := utf8.RuneCountInString(item.text)
		if len(winParts) > 0 {
			elapsed := item.startSec - winStart
			if elapsed >= windowSec || elapsed < 0 || winRunes+1+itemRunes > maxWindowRunes {
				flush()
			}
		}
		if len(winParts) == 0 {
			winStart = item.startSec
			winEnd = item.endSec
		} else if item.endSec > winEnd {
			winEnd = item.endSec
		}
		if winRunes > 0 {
			winRunes++
		}
		winRunes += itemRunes
		winParts = append(winParts, item.text)
	}
	flush()

	return out
}

// FormatCompactedLines renders compacted lines separated by newlines.
func FormatCompactedLines(lines []CompactedLine) string {
	if len(lines) == 0 {
		return ""
	}
	parts := make([]string, len(lines))
	for i, ln := range lines {
		parts[i] = ln.String()
	}
	return strings.Join(parts, "\n")
}

// SplitCompactedLines splits compacted lines on line boundaries into chunks of at most
// maxChunkRunes runes without dropping or truncating any cue.
func SplitCompactedLines(lines []CompactedLine, maxChunkRunes int) []string {
	if len(lines) == 0 {
		return nil
	}
	if maxChunkRunes <= 0 {
		maxChunkRunes = DefaultMapChunkMaxRunes
	}

	var chunks []string
	var cur []string
	curRunes := 0

	flush := func() {
		if len(cur) == 0 {
			return
		}
		chunks = append(chunks, strings.Join(cur, "\n"))
		cur = nil
		curRunes = 0
	}

	for _, ln := range lines {
		formatted := ln.String()
		lineRunes := utf8.RuneCountInString(formatted)
		if len(cur) > 0 && curRunes+1+lineRunes > maxChunkRunes {
			flush()
		}
		if len(cur) > 0 {
			curRunes++
		}
		curRunes += lineRunes
		cur = append(cur, formatted)
	}
	flush()

	return chunks
}

// SplitPlainText splits untimed transcript text into chunks of at most maxChunkRunes runes,
// preferring line boundaries and falling back to rune-safe sentence/space splits for long lines.
func SplitPlainText(text string, maxChunkRunes int) []string {
	normalized := normalizePlainText(text)
	if normalized == "" {
		return nil
	}
	if maxChunkRunes <= 0 {
		maxChunkRunes = DefaultMapChunkMaxRunes
	}

	lines := strings.Split(normalized, "\n")
	var chunks []string
	var cur []string
	curRunes := 0

	flush := func() {
		if len(cur) == 0 {
			return
		}
		chunks = append(chunks, strings.Join(cur, "\n"))
		cur = nil
		curRunes = 0
	}

	for _, line := range lines {
		lineRunes := utf8.RuneCountInString(line)
		if lineRunes > maxChunkRunes {
			flush()
			chunks = append(chunks, splitLongRuneLine(line, maxChunkRunes)...)
			continue
		}
		if len(cur) > 0 && curRunes+1+lineRunes > maxChunkRunes {
			flush()
		}
		if len(cur) > 0 {
			curRunes++
		}
		curRunes += lineRunes
		cur = append(cur, line)
	}
	flush()

	return chunks
}

func splitLongRuneLine(line string, maxRunes int) []string {
	runes := []rune(line)
	var out []string
	for len(runes) > 0 {
		if len(runes) <= maxRunes {
			if seg := strings.TrimSpace(string(runes)); seg != "" {
				out = append(out, seg)
			}
			break
		}

		cut := maxRunes
		minSearch := maxRunes / 2
		for i := maxRunes - 1; i >= minSearch; i-- {
			if isPreferredBreakRune(runes[i]) {
				cut = i + 1
				break
			}
		}

		seg := strings.TrimSpace(string(runes[:cut]))
		if seg != "" {
			out = append(out, seg)
		}
		runes = runes[cut:]
	}
	return out
}

func isPreferredBreakRune(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case '。', '！', '？', '；', '，', '.', '!', '?', ';', ',':
		return true
	default:
		return false
	}
}

func normalizePlainText(raw string) string {
	clean := strings.TrimPrefix(raw, "\xEF\xBB\xBF")
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")

	rawLines := strings.Split(clean, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, ln := range rawLines {
		trimmed := strings.TrimSpace(ln)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}

// DetectManualTranscript inspects manually pasted text to determine whether it contains
// valid timed SRT/VTT cues. Timed input returns canonical WebVTT and hasTimestamps=true;
// untimed input returns normalized plain text and hasTimestamps=false.
func DetectManualTranscript(raw string) (vtt string, plain string, hasTimestamps bool) {
	clean := strings.TrimPrefix(raw, "\xEF\xBB\xBF")
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	trimmed := strings.TrimSpace(clean)
	if trimmed == "" {
		return "", "", false
	}

	if strings.Contains(trimmed, "-->") {
		cues := ai.ParseSubtitleCues(trimmed)
		if len(cues) > 0 {
			vttText := ai.FormatCuesAsWebVTT(cues)
			plainParts := make([]string, 0, len(cues))
			for _, cue := range cues {
				if sub := cleanSubtitleLines(cue.Text); len(sub) > 0 {
					plainParts = append(plainParts, strings.Join(sub, " "))
				}
			}
			return vttText, strings.Join(plainParts, "\n"), true
		}
	}

	return "", normalizePlainText(trimmed), false
}
