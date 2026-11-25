package media

import (
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// DecodeSubtitleToUTF8 normalizes subtitle file bytes (UTF-8 with/without BOM,
// UTF-16 LE/BE with BOM, or GB18030/GBK) into clean UTF-8 text without BOM.
func DecodeSubtitleToUTF8(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}

	// UTF-8 BOM
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return string(raw[3:])
	}

	// UTF-16 LE BOM
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		if decoded, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(raw); err == nil {
			return string(decoded)
		}
	}

	// UTF-16 BE BOM
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		if decoded, err := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder().Bytes(raw); err == nil {
			return string(decoded)
		}
	}

	// Valid UTF-8
	if utf8.Valid(raw) {
		return string(raw)
	}

	// Fallback to GB18030 (strict superset of GBK/GB2312) for legacy Chinese subtitles.
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw); err == nil {
		return string(decoded)
	}

	return string(raw)
}
