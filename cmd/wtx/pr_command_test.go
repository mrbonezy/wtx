package main

import (
	"strings"
	"testing"
)

func TestPROnlyAcceptsNumericPositiveNumber(t *testing.T) {
	cmd := newRootCommand([]string{"wtx", "pr", "abc"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid pull request number") {
		t.Fatalf("expected invalid PR number message, got %q", msg)
	}
	if !strings.Contains(msg, "Usage:") {
		t.Fatalf("expected usage output in error, got %q", msg)
	}
}

func TestPRRequiresOneArgument(t *testing.T) {
	cmd := newRootCommand([]string{"wtx", "pr"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing pull request number") {
		t.Fatalf("expected missing argument message, got %q", msg)
	}
}
