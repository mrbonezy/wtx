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

func TestUpsertAliasBlock_AppendsAndReplacesExisting(t *testing.T) {
	content := "export PATH=\"$HOME/bin:$PATH\"\n"
	block := zshAliasesBlock()

	got := upsertAliasBlock(content, block)
	if !strings.Contains(got, zshAliasBlockStart) || !strings.Contains(got, zshAliasBlockEnd) {
		t.Fatalf("expected alias block to be appended, got %q", got)
	}
	if !strings.Contains(got, "alias wco='wtx co'") || !strings.Contains(got, "alias wpr='wtx pr'") {
		t.Fatalf("expected both aliases to be present, got %q", got)
	}

	replaced := upsertAliasBlock(got, strings.Join([]string{
		zshAliasBlockStart,
		"alias wco='wtx checkout'",
		"alias wpr='wtx pr'",
		zshAliasBlockEnd,
		"",
	}, "\n"))
	if strings.Contains(replaced, "alias wco='wtx co'") {
		t.Fatalf("expected old alias block content to be replaced, got %q", replaced)
	}
	if !strings.Contains(replaced, "alias wco='wtx checkout'") {
		t.Fatalf("expected new alias block content, got %q", replaced)
	}
	if !strings.Contains(replaced, "alias wpr='wtx pr'") {
		t.Fatalf("expected new wpr alias block content, got %q", replaced)
	}
}

func TestRemoveManagedBlock_RemovesAliasBlockOnly(t *testing.T) {
	content := strings.Join([]string{
		"line-a",
		zshAliasBlockStart,
		"alias wco='wtx co'",
		"alias wpr='wtx pr'",
		zshAliasBlockEnd,
		"line-b",
		"",
	}, "\n")
	got := removeManagedBlock(content, zshAliasBlockStart, zshAliasBlockEnd)
	if strings.Contains(got, zshAliasBlockStart) || strings.Contains(got, zshAliasBlockEnd) {
		t.Fatalf("expected alias block to be removed, got %q", got)
	}
	if !strings.Contains(got, "line-a") || !strings.Contains(got, "line-b") {
		t.Fatalf("expected surrounding content to be preserved, got %q", got)
	}
}
