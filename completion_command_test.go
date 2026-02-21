package main

import (
	"strings"
	"testing"
)

func TestUpsertCompletionBlock_AppendsWhenMissing(t *testing.T) {
	content := "export PATH=\"$HOME/bin:$PATH\"\n"
	block := strings.Join([]string{zshCompletionBlockStart, "line", zshCompletionBlockEnd, ""}, "\n")

	got := upsertCompletionBlock(content, block)
	if !strings.Contains(got, zshCompletionBlockStart) || !strings.Contains(got, zshCompletionBlockEnd) {
		t.Fatalf("expected completion block to be appended, got %q", got)
	}
}

func TestUpsertCompletionBlock_ReplacesExisting(t *testing.T) {
	content := strings.Join([]string{
		"a",
		zshCompletionBlockStart,
		"old",
		zshCompletionBlockEnd,
		"b",
	}, "\n")
	block := strings.Join([]string{zshCompletionBlockStart, "new", zshCompletionBlockEnd, ""}, "\n")

	got := upsertCompletionBlock(content, block)
	if strings.Contains(got, "old") {
		t.Fatalf("expected old block content to be replaced, got %q", got)
	}
	if !strings.Contains(got, "new") {
		t.Fatalf("expected new block content, got %q", got)
	}
}
