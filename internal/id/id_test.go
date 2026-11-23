package id_test

import (
	"testing"

	"coursefin/internal/id"
)

func TestNew(t *testing.T) {
	a := id.New()
	b := id.New()
	if len(a) != 32 {
		t.Fatalf("expected 32-char id, got %q (len=%d)", a, len(a))
	}
	if a == b {
		t.Fatalf("expected unique ids, got %q twice", a)
	}
	norm, ok := id.Normalize(a)
	if !ok || norm != a {
		t.Fatalf("expected id.New() %q to be canonical lowercase hex, got (%q, %v)", a, norm, ok)
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"a1b2c3d4e5f60718293a4b5c6d7e8f90", "a1b2c3d4e5f60718293a4b5c6d7e8f90", true},
		{"A1B2C3D4E5F60718293A4B5C6D7E8F90", "a1b2c3d4e5f60718293a4b5c6d7e8f90", true},
		{"a1b2c3d4-e5f6-0718-293a-4b5c6d7e8f90", "a1b2c3d4e5f60718293a4b5c6d7e8f90", true},
		{"A1B2C3D4-E5F6-0718-293A-4B5C6D7E8F90", "a1b2c3d4e5f60718293a4b5c6d7e8f90", true},
		{"", "", false},
		{"not-a-uuid", "", false},
		{"a1b2c3d4e5f60718293a4b5c6d7e8f9", "", false},
		{"a1b2c3d4e5f60718293a4b5c6d7e8f900", "", false},
		{"g1b2c3d4e5f60718293a4b5c6d7e8f90", "", false},
		{"a1b2c3d4_e5f6-0718-293a-4b5c6d7e8f90", "", false},
		{"{a1b2c3d4-e5f6-0718-293a-4b5c6d7e8f90}", "", false},
	}

	for _, tc := range cases {
		got, ok := id.Normalize(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Normalize(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
