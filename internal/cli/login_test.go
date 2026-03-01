package cli

import (
	"context"
	"strings"
	"testing"
)

func TestLogin_InvalidProvider(t *testing.T) {
	ctx := context.Background()
	err := Login(ctx, "invalid", "ws://localhost", false)
	if err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("expected error to contain %q, got: %v", "unknown provider", err)
	}
}

func TestLoginDeviceCode_Unsupported(t *testing.T) {
	ctx := context.Background()
	// apple is a valid provider name but device code flow is not supported for it
	err := Login(ctx, "apple", "ws://localhost", true)
	if err == nil {
		t.Fatal("expected error for unsupported device code provider, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected error to contain %q, got: %v", "not supported", err)
	}
}

func TestLoginDeviceCode_MissingClientID(t *testing.T) {
	t.Setenv("PHOSPHOR_MICROSOFT_CLIENT_ID", "")

	ctx := context.Background()
	err := Login(ctx, "microsoft", "ws://localhost", true)
	if err == nil {
		t.Fatal("expected error for missing client ID, got nil")
	}
	if !strings.Contains(err.Error(), "no client ID") {
		t.Errorf("expected error to contain %q, got: %v", "no client ID", err)
	}
}
