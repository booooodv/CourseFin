package updater

import (
	"fmt"
	"strconv"
	"strings"
)

// SemVer represents a parsed Semantic Versioning 2.0.0 version.
type SemVer struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease []string
	Build      string
}

// NormalizeVersion trims whitespace and strips an optional leading v/V prefix
// when followed by a version digit.
func NormalizeVersion(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) > 1 && (s[0] == 'v' || s[0] == 'V') && s[1] >= '0' && s[1] <= '9' {
		return s[1:]
	}
	return s
}

// ParseVersion parses a SemVer 2.0.0 version string (with optional leading v).
func ParseVersion(raw string) (SemVer, error) {
	s := NormalizeVersion(raw)
	if s == "" {
		return SemVer{}, fmt.Errorf("empty version string")
	}

	var build string
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		build = s[idx+1:]
		if build == "" {
			return SemVer{}, fmt.Errorf("empty build metadata in %q", raw)
		}
		for _, part := range strings.Split(build, ".") {
			if !isValidIdent(part) {
				return SemVer{}, fmt.Errorf("invalid build metadata identifier %q in %q", part, raw)
			}
		}
		s = s[:idx]
	}

	var preRelease []string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre := s[idx+1:]
		if pre == "" {
			return SemVer{}, fmt.Errorf("empty prerelease in %q", raw)
		}
		parts := strings.Split(pre, ".")
		for _, part := range parts {
			if !isValidIdent(part) {
				return SemVer{}, fmt.Errorf("invalid prerelease identifier %q in %q", part, raw)
			}
			if isASCIIAllDigits(part) && len(part) > 1 && part[0] == '0' {
				return SemVer{}, fmt.Errorf("leading zero in numeric prerelease %q in %q", part, raw)
			}
		}
		preRelease = parts
		s = s[:idx]
	}

	core := strings.Split(s, ".")
	if len(core) != 3 {
		return SemVer{}, fmt.Errorf("expected MAJOR.MINOR.PATCH in %q", raw)
	}

	major, err := parseCoreNumber(core[0], raw)
	if err != nil {
		return SemVer{}, err
	}
	minor, err := parseCoreNumber(core[1], raw)
	if err != nil {
		return SemVer{}, err
	}
	patch, err := parseCoreNumber(core[2], raw)
	if err != nil {
		return SemVer{}, err
	}

	return SemVer{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		PreRelease: preRelease,
		Build:      build,
	}, nil
}

// IsValidSemVer reports whether raw is a valid SemVer 2.0.0 string.
func IsValidSemVer(raw string) bool {
	_, err := ParseVersion(raw)
	return err == nil
}

// CompareVersion parses and compares two SemVer strings, returning -1, 0, or 1.
func CompareVersion(a, b string) (int, error) {
	va, err := ParseVersion(a)
	if err != nil {
		return 0, fmt.Errorf("parse version %q: %w", a, err)
	}
	vb, err := ParseVersion(b)
	if err != nil {
		return 0, fmt.Errorf("parse version %q: %w", b, err)
	}
	return va.Compare(vb), nil
}

// Compare compares v against other according to SemVer 2.0.0 precedence rules.
func (v SemVer) Compare(other SemVer) int {
	if v.Major != other.Major {
		return cmpInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return cmpInt(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return cmpInt(v.Patch, other.Patch)
	}

	vHasPre := len(v.PreRelease) > 0
	oHasPre := len(other.PreRelease) > 0
	if !vHasPre && !oHasPre {
		return 0
	}
	if !vHasPre {
		return 1
	}
	if !oHasPre {
		return -1
	}

	n := min(len(v.PreRelease), len(other.PreRelease))
	for i := 0; i < n; i++ {
		if c := comparePreIdentifier(v.PreRelease[i], other.PreRelease[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(v.PreRelease), len(other.PreRelease))
}

func parseCoreNumber(part, raw string) (int, error) {
	if !isASCIIAllDigits(part) {
		return 0, fmt.Errorf("non-numeric core component %q in %q", part, raw)
	}
	if len(part) > 1 && part[0] == '0' {
		return 0, fmt.Errorf("leading zero in core component %q in %q", part, raw)
	}
	v, err := strconv.Atoi(part)
	if err != nil {
		return 0, fmt.Errorf("parse core component %q in %q: %w", part, raw, err)
	}
	return v, nil
}

func isValidIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-') {
			return false
		}
	}
	return true
}

func isASCIIAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func comparePreIdentifier(a, b string) int {
	aNum := isASCIIAllDigits(a)
	bNum := isASCIIAllDigits(b)
	switch {
	case aNum && bNum:
		if len(a) != len(b) {
			return cmpInt(len(a), len(b))
		}
		return strings.Compare(a, b)
	case aNum && !bNum:
		return -1
	case !aNum && bNum:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
