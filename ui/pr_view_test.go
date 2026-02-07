package ui

import "testing"

func TestFormatUnresolvedLabel(t *testing.T) {
	if got := formatUnresolvedLabel(3, true); got != "3" {
		t.Fatalf("expected unresolved 3, got %q", got)
	}
	if got := formatUnresolvedLabel(-2, true); got != "0" {
		t.Fatalf("expected unresolved floor 0, got %q", got)
	}
	if got := formatUnresolvedLabel(2, false); got != "-" {
		t.Fatalf("expected unknown as '-', got %q", got)
	}
}

func TestFormatCommentsLabel_KnownAndUnknown(t *testing.T) {
	if got := formatCommentsLabel(1, 3, true); got != "(1/3)" {
		t.Fatalf("expected (1/3), got %q", got)
	}
	if got := formatCommentsLabel(1, 3, false); got != "-" {
		t.Fatalf("expected '-' when unknown, got %q", got)
	}
}
