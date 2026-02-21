package main

import "testing"

func TestResolveCheckoutFetchPreference_UsesConfigWhenNotExplicit(t *testing.T) {
	f := false
	cfg := Config{NewBranchFetchFirst: &f}
	if got := resolveCheckoutFetchPreference(nil, cfg); got {
		t.Fatalf("expected false from config, got true")
	}
}

func TestResolveCheckoutFetchPreference_ExplicitOverridesConfig(t *testing.T) {
	f := false
	cfg := Config{NewBranchFetchFirst: &f}
	explicit := true
	if got := resolveCheckoutFetchPreference(&explicit, cfg); !got {
		t.Fatalf("expected explicit true override, got false")
	}
}

func TestResolveCheckoutFetchPreference_DefaultsTrue(t *testing.T) {
	if got := resolveCheckoutFetchPreference(nil, Config{}); !got {
		t.Fatalf("expected default true, got false")
	}
}

func TestExplicitFetchPreference_ValidatesMutualExclusion(t *testing.T) {
	if _, err := explicitFetchPreference(true, true); err == nil {
		t.Fatalf("expected error when both flags are set")
	}
}

func TestFormatRunCommandMessage_NoQuotes(t *testing.T) {
	got := formatRunCommandMessage("claude --dangerously-skip-permissions")
	want := "Running claude --dangerously-skip-permissions"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
