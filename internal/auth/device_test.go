package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestDeviceCode_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("unexpected Content-Type: %s", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if got := r.FormValue("client_id"); got != "test-client-id" {
			t.Errorf("expected client_id=test-client-id, got %s", got)
		}
		if got := r.FormValue("scope"); got != "openid profile email" {
			t.Errorf("expected scope='openid profile email', got %s", got)
		}
		resp := DeviceCodeResponse{
			DeviceCode:      "device-code-abc",
			UserCode:        "USER-CODE",
			VerificationURI: "https://example.com/activate",
			ExpiresIn:       1800,
			Interval:        10,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	result, err := RequestDeviceCode(context.Background(), server.URL, "test-client-id", []string{"openid", "profile", "email"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DeviceCode != "device-code-abc" {
		t.Errorf("expected DeviceCode=device-code-abc, got %s", result.DeviceCode)
	}
	if result.UserCode != "USER-CODE" {
		t.Errorf("expected UserCode=USER-CODE, got %s", result.UserCode)
	}
	if result.VerificationURI != "https://example.com/activate" {
		t.Errorf("expected VerificationURI=https://example.com/activate, got %s", result.VerificationURI)
	}
	if result.ExpiresIn != 1800 {
		t.Errorf("expected ExpiresIn=1800, got %d", result.ExpiresIn)
	}
	if result.Interval != 10 {
		t.Errorf("expected Interval=10, got %d", result.Interval)
	}
}

func TestRequestDeviceCode_IntervalDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeviceCodeResponse{
			DeviceCode:      "dc",
			UserCode:        "UC",
			VerificationURI: "https://example.com/activate",
			ExpiresIn:       900,
			Interval:        0,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	result, err := RequestDeviceCode(context.Background(), server.URL, "client-id", []string{"openid"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Interval != 5 {
		t.Errorf("expected default Interval=5 when server returns 0, got %d", result.Interval)
	}
}

func TestRequestDeviceCode_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	_, err := RequestDeviceCode(context.Background(), server.URL, "client-id", []string{"openid"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain status code 500, got: %v", err)
	}
}

func TestRequestDeviceCode_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-valid-json{{{"))
	}))
	defer server.Close()

	_, err := RequestDeviceCode(context.Background(), server.URL, "client-id", []string{"openid"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected error to contain 'decode', got: %v", err)
	}
}

func TestPollForToken_ImmediateSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeviceTokenResponse{
			AccessToken:  "access-token-xyz",
			IDToken:      "id-token-xyz",
			RefreshToken: "refresh-token-xyz",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	result, err := PollForToken(context.Background(), server.URL, "client-id", "device-code-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AccessToken != "access-token-xyz" {
		t.Errorf("expected AccessToken=access-token-xyz, got %s", result.AccessToken)
	}
	if result.IDToken != "id-token-xyz" {
		t.Errorf("expected IDToken=id-token-xyz, got %s", result.IDToken)
	}
	if result.RefreshToken != "refresh-token-xyz" {
		t.Errorf("expected RefreshToken=refresh-token-xyz, got %s", result.RefreshToken)
	}
	if result.TokenType != "Bearer" {
		t.Errorf("expected TokenType=Bearer, got %s", result.TokenType)
	}
	if result.ExpiresIn != 3600 {
		t.Errorf("expected ExpiresIn=3600, got %d", result.ExpiresIn)
	}
}

func TestPollForToken_ExpiredToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeviceTokenResponse{
			Error:     "expired_token",
			ErrorDesc: "The device code has expired.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	_, err := PollForToken(context.Background(), server.URL, "client-id", "device-code-abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected error to contain 'expired', got: %v", err)
	}
}

func TestPollForToken_AccessDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeviceTokenResponse{
			Error:     "access_denied",
			ErrorDesc: "The user denied the authorization request.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	_, err := PollForToken(context.Background(), server.URL, "client-id", "device-code-abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Errorf("expected error to contain 'denied', got: %v", err)
	}
}

func TestPollForToken_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeviceTokenResponse{
			AccessToken: "should-not-get-this",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := PollForToken(ctx, server.URL, "client-id", "device-code-abc")
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
	if err != ctx.Err() {
		t.Errorf("expected ctx.Err()=%v, got %v", ctx.Err(), err)
	}
}
