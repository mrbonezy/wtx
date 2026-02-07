package main

import "testing"

func TestChooseFallbackBaseNoRemote_PrefersMainWhenPresent(t *testing.T) {
	got := chooseFallbackBaseNoRemote(true, "feature/test")
	if got != "main" {
		t.Fatalf("expected main, got %q", got)
	}
}

func TestChooseFallbackBaseNoRemote_UsesCurrentWhenMainMissing(t *testing.T) {
	got := chooseFallbackBaseNoRemote(false, "feature/test")
	if got != "feature/test" {
		t.Fatalf("expected current branch, got %q", got)
	}
}

func TestChooseFallbackBaseNoRemote_FallsBackToMainOnDetached(t *testing.T) {
	got := chooseFallbackBaseNoRemote(false, "detached")
	if got != "main" {
		t.Fatalf("expected main fallback, got %q", got)
	}
}
