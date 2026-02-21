package main

import (
	"strings"
	"testing"
)

func TestCheckoutRejectsOverrideFlagsWithoutCreate(t *testing.T) {
	cmd := newRootCommand([]string{"wtx", "checkout", "foo", "--from", "main"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "require -b") {
		t.Fatalf("expected -b requirement message, got %q", msg)
	}
	if !strings.Contains(msg, "Usage:") {
		t.Fatalf("expected usage output in error, got %q", msg)
	}
}

func TestCheckoutRejectsConflictingFetchFlags(t *testing.T) {
	cmd := newRootCommand([]string{"wtx", "checkout", "-b", "foo", "--fetch", "--no-fetch"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cannot be used together") {
		t.Fatalf("expected conflicting flag message, got %q", msg)
	}
}
