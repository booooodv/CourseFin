package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"coursefin/internal/auth"
	"coursefin/internal/db"
	"coursefin/internal/id"
)

func TestSetupLoginMultiDeviceAndRevoke(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer conn.Close()

	svc := auth.NewService(conn)
	ctx := context.Background()

	needs, err := svc.NeedsSetup(ctx)
	if err != nil {
		t.Fatalf("NeedsSetup failed: %v", err)
	}
	if !needs {
		t.Fatalf("expected NeedsSetup=true on fresh db")
	}

	if _, err := svc.GetAdminUser(ctx); !errors.Is(err, auth.ErrNotSetup) {
		t.Fatalf("expected ErrNotSetup before setup, got %v", err)
	}

	user, err := svc.SetupAdmin(ctx, "admin", "pass1234")
	if err != nil {
		t.Fatalf("SetupAdmin failed: %v", err)
	}
	if user.Username != "admin" {
		t.Fatalf("expected username admin, got %q", user.Username)
	}
	if _, ok := id.Normalize(user.ID); !ok {
		t.Fatalf("expected canonical 32-char hex user ID, got %q", user.ID)
	}

	needs, err = svc.NeedsSetup(ctx)
	if err != nil {
		t.Fatalf("NeedsSetup after setup failed: %v", err)
	}
	if needs {
		t.Fatalf("expected NeedsSetup=false after setup")
	}

	// Second setup must fail with ErrAlreadySetup.
	if _, err := svc.SetupAdmin(ctx, "admin2", "pass5678"); !errors.Is(err, auth.ErrAlreadySetup) {
		t.Fatalf("expected ErrAlreadySetup on second SetupAdmin, got %v", err)
	}

	// EnsureEnvAdmin must not overwrite existing admin.
	if err := svc.EnsureEnvAdmin(ctx, "other", "otherpass"); err != nil {
		t.Fatalf("EnsureEnvAdmin on already setup db failed: %v", err)
	}

	// Wrong password must fail.
	if _, _, err := svc.Login(ctx, "admin", "wrong", auth.DeviceInfo{DeviceID: "dev1"}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for wrong password, got %v", err)
	}

	// Login from two different devices.
	dev1 := auth.DeviceInfo{
		DeviceID:      "dev1",
		DeviceName:    "iPad Pro",
		ClientName:    "Infuse",
		ClientVersion: "7.7",
	}
	sess1, u1, err := svc.Login(ctx, "admin", "pass1234", dev1)
	if err != nil {
		t.Fatalf("Login dev1 failed: %v", err)
	}
	if sess1.Token == "" || u1.ID != user.ID {
		t.Fatalf("unexpected login result: sess=%+v user=%+v", sess1, u1)
	}
	if sess1.DeviceID != "dev1" || sess1.DeviceName != "iPad Pro" || sess1.ClientName != "Infuse" || sess1.ClientVersion != "7.7" {
		t.Fatalf("expected device metadata stored, got %+v", sess1)
	}
	if sess1.CreatedAt == "" || sess1.LastActiveAt == "" {
		t.Fatalf("expected CreatedAt and LastActiveAt populated, got %+v", sess1)
	}

	dev2 := auth.DeviceInfo{
		DeviceID:      "dev2",
		DeviceName:    "Chrome",
		ClientName:    "CourseFin Web",
		ClientVersion: "1.0",
	}
	sess2, _, err := svc.Login(ctx, "admin", "pass1234", dev2)
	if err != nil {
		t.Fatalf("Login dev2 failed: %v", err)
	}
	if sess2.Token == sess1.Token {
		t.Fatalf("expected distinct tokens for two devices")
	}

	// Authenticate succeeds for both tokens.
	if gotSess, gotUser, err := svc.Authenticate(ctx, sess1.Token); err != nil || gotSess.DeviceID != "dev1" || gotUser.ID != user.ID {
		t.Fatalf("Authenticate tok1 failed: sess=%+v user=%+v err=%v", gotSess, gotUser, err)
	}
	if gotSess, gotUser, err := svc.Authenticate(ctx, sess2.Token); err != nil || gotSess.DeviceID != "dev2" || gotUser.ID != user.ID {
		t.Fatalf("Authenticate tok2 failed: sess=%+v user=%+v err=%v", gotSess, gotUser, err)
	}

	// Re-login from dev1 replaces only dev1's token without invalidating dev2.
	sess1b, _, err := svc.Login(ctx, "admin", "pass1234", dev1)
	if err != nil {
		t.Fatalf("re-login dev1 failed: %v", err)
	}
	if sess1b.Token == sess1.Token {
		t.Fatalf("expected new token on re-login")
	}
	if _, _, err := svc.Authenticate(ctx, sess1.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected old dev1 token to be invalidated, got %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, sess1b.Token); err != nil {
		t.Fatalf("expected new dev1 token to authenticate, got %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, sess2.Token); err != nil {
		t.Fatalf("expected dev2 token to remain valid after dev1 re-login, got %v", err)
	}

	// ListSessions returns 2 active sessions.
	sessions, err := svc.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// RevokeSession invalidates only targeted token.
	if err := svc.RevokeSession(ctx, sess1b.Token); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, sess1b.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected revoked token to fail auth, got %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, sess2.Token); err != nil {
		t.Fatalf("expected dev2 token to remain valid after revoking dev1, got %v", err)
	}

	// ChangePassword updates password hash cleanly.
	if err := svc.ChangePassword(ctx, "wrongpass", "newpass5678"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for wrong old password, got %v", err)
	}
	if err := svc.ChangePassword(ctx, "pass1234", "newpass5678"); err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "pass1234", dev1); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected old password to fail after ChangePassword, got %v", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "newpass5678", dev1); err != nil {
		t.Fatalf("expected new password to succeed after ChangePassword, got %v", err)
	}

	adminUser, err := svc.GetAdminUser(ctx)
	if err != nil || adminUser.ID != user.ID {
		t.Fatalf("GetAdminUser failed: user=%+v err=%v", adminUser, err)
	}
}

func TestEnsureEnvAdminFreshDB(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer conn.Close()

	svc := auth.NewService(conn)
	ctx := context.Background()

	if err := svc.EnsureEnvAdmin(ctx, "envadmin", "envpass"); err != nil {
		t.Fatalf("EnsureEnvAdmin failed: %v", err)
	}
	needs, err := svc.NeedsSetup(ctx)
	if err != nil {
		t.Fatalf("NeedsSetup failed: %v", err)
	}
	if needs {
		t.Fatalf("expected NeedsSetup=false after EnsureEnvAdmin")
	}
	if _, _, err := svc.Login(ctx, "envadmin", "envpass", auth.DeviceInfo{DeviceID: "d1"}); err != nil {
		t.Fatalf("Login with env admin failed: %v", err)
	}
}

func TestParseAuthHeaderVariants(t *testing.T) {
	cases := []struct {
		name       string
		buildReq   func() *http.Request
		wantToken  string
		wantDevice auth.DeviceInfo
	}{
		{
			name: "MediaBrowser full header",
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/Users/Me", nil)
				r.Header.Set("Authorization", `MediaBrowser Client="Infuse", Device="iPad", DeviceId="dev1", Version="7.7", Token="tok1"`)
				return r
			},
			wantToken: "tok1",
			wantDevice: auth.DeviceInfo{
				ClientName:    "Infuse",
				DeviceName:    "iPad",
				DeviceID:      "dev1",
				ClientVersion: "7.7",
			},
		},
		{
			name: "Emby header",
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/Users/Me", nil)
				r.Header.Set("Authorization", `Emby Token="tok2"`)
				return r
			},
			wantToken: "tok2",
		},
		{
			name: "X-Emby-Token header",
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/Users/Me", nil)
				r.Header.Set("X-Emby-Token", "tok3")
				return r
			},
			wantToken: "tok3",
		},
		{
			name: "X-MediaBrowser-Token header",
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/Users/Me", nil)
				r.Header.Set("X-MediaBrowser-Token", "tok4")
				return r
			},
			wantToken: "tok4",
		},
		{
			name: "api_key query param",
			buildReq: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/Items?api_key=tok5", nil)
			},
			wantToken: "tok5",
		},
		{
			name: "Case-insensitive MediaBrowser header",
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/Users/Me", nil)
				r.Header.Set("Authorization", `mediabrowser client="VidHub", device="iPhone", deviceid="d2", version="1.2", token="tok6"`)
				return r
			},
			wantToken: "tok6",
			wantDevice: auth.DeviceInfo{
				ClientName:    "VidHub",
				DeviceName:    "iPhone",
				DeviceID:      "d2",
				ClientVersion: "1.2",
			},
		},
		{
			name: "Device metadata in MediaBrowser + X-Emby-Token fallback",
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/Users/Me", nil)
				r.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Swiftfin", Device="AppleTV", DeviceId="tv1", Version="1.0"`)
				r.Header.Set("X-Emby-Token", "tok7")
				return r
			},
			wantToken: "tok7",
			wantDevice: auth.DeviceInfo{
				ClientName:    "Swiftfin",
				DeviceName:    "AppleTV",
				DeviceID:      "tv1",
				ClientVersion: "1.0",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok, dev := auth.ParseAuthHeader(tc.buildReq())
			if tok != tc.wantToken {
				t.Fatalf("token = %q, want %q", tok, tc.wantToken)
			}
			if dev != tc.wantDevice {
				t.Fatalf("device = %+v, want %+v", dev, tc.wantDevice)
			}
		})
	}
}
