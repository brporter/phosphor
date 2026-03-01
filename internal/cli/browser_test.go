package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrowserLogin_Success(t *testing.T) {
	origOpen := openBrowserFn
	defer func() { openBrowserFn = origOpen }()
	openBrowserFn = func(url string) {} // no-op

	pollCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/auth/login"):
			json.NewEncoder(w).Encode(loginStartResponse{SessionID: "test-session", AuthURL: "http://example.com/auth"})
		case strings.HasSuffix(r.URL.Path, "/api/auth/poll"):
			pollCount++
			if pollCount >= 1 {
				json.NewEncoder(w).Encode(pollResponse{Status: "complete", IDToken: "test-id-token"})
			} else {
				json.NewEncoder(w).Encode(pollResponse{Status: "pending"})
			}
		}
	}))
	defer srv.Close()

	relayURL := strings.Replace(srv.URL, "http://", "ws://", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	token, err := BrowserLogin(ctx, relayURL, "test")
	if err != nil {
		t.Fatal(err)
	}
	if token != "test-id-token" {
		t.Errorf("got token %q, want %q", token, "test-id-token")
	}
}

func TestBrowserLogin_RelayError(t *testing.T) {
	origOpen := openBrowserFn
	defer func() { openBrowserFn = origOpen }()
	openBrowserFn = func(url string) {}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/auth/login") {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	relayURL := strings.Replace(srv.URL, "http://", "ws://", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := BrowserLogin(ctx, relayURL, "test")
	if err == nil {
		t.Fatal("expected error for relay 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain status code %q, got: %v", "500", err)
	}
}

func TestBrowserLogin_ContextCanceled(t *testing.T) {
	origOpen := openBrowserFn
	defer func() { openBrowserFn = origOpen }()
	openBrowserFn = func(url string) {}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/auth/login"):
			json.NewEncoder(w).Encode(loginStartResponse{SessionID: "s1", AuthURL: "http://test/auth"})
		case strings.HasSuffix(r.URL.Path, "/api/auth/poll"):
			json.NewEncoder(w).Encode(pollResponse{Status: "pending"})
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay to let the login request complete but abort during polling.
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()

	relayURL := strings.Replace(srv.URL, "http://", "ws://", 1)
	_, err := BrowserLogin(ctx, relayURL, "test")
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}
