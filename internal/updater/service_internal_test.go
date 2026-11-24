package updater

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
)

func TestDefaultHTTPClientCheckRedirectStripsAuth(t *testing.T) {
	svc := NewService(Options{})
	checkRedirect := svc.httpClient.CheckRedirect
	if checkRedirect == nil {
		t.Fatalf("expected default httpClient to configure CheckRedirect")
	}

	makeReq := func(rawURL string) *http.Request {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatalf("NewRequest(%q): %v", rawURL, err)
		}
		req.Header.Set("Authorization", "Bearer secret-tok")
		return req
	}

	// Same host, scheme downgrade (https -> http) must strip Authorization.
	downgraded := makeReq("http://api.github.com/repos/a/b")
	viaHTTPS := []*http.Request{makeReq("https://api.github.com/repos/a/b")}
	if err := checkRedirect(downgraded, viaHTTPS); err != nil {
		t.Fatalf("checkRedirect returned unexpected error: %v", err)
	}
	if got := downgraded.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected Authorization stripped on https->http downgrade, got %q", got)
	}

	// Cross-host redirect must strip Authorization.
	crossHost := makeReq("https://objects.githubusercontent.com/asset")
	if err := checkRedirect(crossHost, viaHTTPS); err != nil {
		t.Fatalf("checkRedirect returned unexpected error: %v", err)
	}
	if got := crossHost.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected Authorization stripped on cross-host redirect, got %q", got)
	}

	// Same host and same https scheme preserves Authorization.
	sameOrigin := makeReq("https://api.github.com/repos/a/c")
	if err := checkRedirect(sameOrigin, viaHTTPS); err != nil {
		t.Fatalf("checkRedirect returned unexpected error: %v", err)
	}
	if got := sameOrigin.Header.Get("Authorization"); got != "Bearer secret-tok" {
		t.Fatalf("expected Authorization preserved on same-host https redirect, got %q", got)
	}
}

func TestStatusDoesNotHoldMutexAcrossEligibilityCheck(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "coursefin")
	var svc *Service
	var couldAcquireWriteLock bool

	svc = NewService(Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ExecutablePath: func() (string, error) {
			if svc.mu.TryLock() {
				couldAcquireWriteLock = true
				svc.mu.Unlock()
			}
			return binPath, nil
		},
	})

	_ = svc.Status(context.Background())
	if !couldAcquireWriteLock {
		t.Fatalf("expected Service.mu not to be held while probing target executable eligibility in Status()")
	}
}
