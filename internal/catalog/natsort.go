package catalog

import (
	"strconv"
	"strings"
	"unicode"
)

type natToken struct {
	isNum  bool
	digits string // zero-stripped canonical decimal digits when isNum is true
	text   string // lowercased text when isNum is false
}

// NaturalCompare compares two strings using natural ordering for both ASCII
// numeric runs (e.g. "1.2" < "1.10", "01_intro" < "10_end") and Chinese numerals
// (零一二三四五六七八九十百千, e.g. "第一章" < "第二章" < "第十章").
// Returns -1 if a < b, 0 if a == b, or 1 if a > b.
func NaturalCompare(a, b string) int {
	if a == b {
		return 0
	}

	ta := tokenizeNatural(a)
	tb := tokenizeNatural(b)

	n := len(ta)
	if len(tb) < n {
		n = len(tb)
	}

	for i := 0; i < n; i++ {
		xa, xb := ta[i], tb[i]
		if xa.isNum != xb.isNum {
			if xa.isNum {
				return -1
			}
			return 1
		}
		if xa.isNum {
			if len(xa.digits) != len(xb.digits) {
				if len(xa.digits) < len(xb.digits) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(xa.digits, xb.digits); c != 0 {
				return c
			}
			continue
		}
		if c := strings.Compare(xa.text, xb.text); c != 0 {
			return c
		}
	}

	if len(ta) != len(tb) {
		if len(ta) < len(tb) {
			return -1
		}
		return 1
	}

	return strings.Compare(a, b)
}

func tokenizeNatural(s string) []natToken {
	runes := []rune(s)
	tokens := make([]natToken, 0, 8)

	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r >= '0' && r <= '9':
			j := i + 1
			for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
				j++
			}
			tokens = append(tokens, natToken{
				isNum:  true,
				digits: stripLeadingZeros(string(runes[i:j])),
			})
			i = j

		case isChineseNumeral(r):
			j := i + 1
			for j < len(runes) && isChineseNumeral(runes[j]) {
				j++
			}
			val := parseChineseNumeral(runes[i:j])
			tokens = append(tokens, natToken{
				isNum:  true,
				digits: strconv.FormatUint(val, 10),
			})
			i = j

		default:
			var sb strings.Builder
			for i < len(runes) {
				cur := runes[i]
				if (cur >= '0' && cur <= '9') || isChineseNumeral(cur) {
					break
				}
				sb.WriteRune(unicode.ToLower(cur))
				i++
			}
			tokens = append(tokens, natToken{
				isNum: false,
				text:  sb.String(),
			})
		}
	}

	return tokens
}

func stripLeadingZeros(s string) string {
	trimmed := strings.TrimLeft(s, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

func isChineseNumeral(r rune) bool {
	switch r {
	case '零', '一', '二', '三', '四', '五', '六', '七', '八', '九', '十', '百', '千':
		return true
	default:
		return false
	}
}

func chineseDigitValue(r rune) (uint64, bool) {
	switch r {
	case '零':
		return 0, true
	case '一':
		return 1, true
	case '二':
		return 2, true
	case '三':
		return 3, true
	case '四':
		return 4, true
	case '五':
		return 5, true
	case '六':
		return 6, true
	case '七':
		return 7, true
	case '八':
		return 8, true
	case '九':
		return 9, true
	default:
		return 0, false
	}
}

func chineseUnitValue(r rune) (uint64, bool) {
	switch r {
	case '十':
		return 10, true
	case '百':
		return 100, true
	case '千':
		return 1000, true
	default:
		return 0, false
	}
}

func parseChineseNumeral(runes []rune) uint64 {
	hasUnit := false
	for _, r := range runes {
		if _, ok := chineseUnitValue(r); ok {
			hasUnit = true
			break
		}
	}

	if !hasUnit {
		var val uint64
		for _, r := range runes {
			d, _ := chineseDigitValue(r)
			val = val*10 + d
		}
		return val
	}

	var total uint64
	var cur uint64
	hasCur := false

	for _, r := range runes {
		if d, ok := chineseDigitValue(r); ok {
			cur = d
			hasCur = true
			continue
		}
		if unit, ok := chineseUnitValue(r); ok {
			if !hasCur {
				cur = 1
			}
			total += cur * unit
			cur = 0
			hasCur = false
		}
	}

	return total + cur
}
