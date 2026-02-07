package main

import (
	"errors"
	"strings"
	"testing"
)

func TestCommandErrorWithOutput_PrefersCommandOutput(t *testing.T) {
	fallback := errors.New("exit status 128")
	err := commandErrorWithOutput(fallback, []byte("fatal: worktree contains unstaged changes\n"))
	if err == nil {
		t.Fatalf("expected an error")
	}
	if !strings.Contains(err.Error(), "unstaged changes") {
		t.Fatalf("expected stderr message, got %q", err.Error())
	}
}

func TestCommandErrorWithOutput_FallsBackToOriginalError(t *testing.T) {
	fallback := errors.New("exit status 128")
	err := commandErrorWithOutput(fallback, []byte("   \n\t"))
	if err == nil {
		t.Fatalf("expected an error")
	}
	if err.Error() != fallback.Error() {
		t.Fatalf("expected fallback error %q, got %q", fallback.Error(), err.Error())
	}
}
