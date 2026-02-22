package main

import (
	"errors"
	"testing"
)

func TestFormatInteractiveUpdateHint_WithUpdateAvailable(t *testing.T) {
	got, isErr := formatInteractiveUpdateHint("v1.0.0", updateCheckResult{
		CurrentVersion:  "v1.0.0",
		LatestVersion:   "v1.1.0",
		UpdateAvailable: true,
	}, nil)
	want := "wtx v1.0.0 -> v1.1.0 available. Run: wtx update"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if isErr {
		t.Fatalf("expected non-error hint")
	}
}

func TestFormatInteractiveUpdateHint_WithoutUpdate(t *testing.T) {
	got, isErr := formatInteractiveUpdateHint("v1.0.0", updateCheckResult{
		CurrentVersion:  "v1.0.0",
		LatestVersion:   "v1.0.0",
		UpdateAvailable: false,
	}, nil)
	want := "wtx v1.0.0"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if isErr {
		t.Fatalf("expected non-error hint")
	}
}

func TestFormatInteractiveUpdateHint_OnCheckError(t *testing.T) {
	got, isErr := formatInteractiveUpdateHint("v1.0.0", updateCheckResult{}, errors.New("boom"))
	want := "wtx update check failed: boom"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if !isErr {
		t.Fatalf("expected error hint")
	}
}

func TestFormatInteractiveUpdateHint_OnResolveFallbackError(t *testing.T) {
	got, isErr := formatInteractiveUpdateHint("v1.0.0", updateCheckResult{
		ResolveError: "network down",
	}, nil)
	want := "wtx update check failed: network down"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if !isErr {
		t.Fatalf("expected error hint")
	}
}
