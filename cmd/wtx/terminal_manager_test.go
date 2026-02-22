package main

import "testing"

func TestShouldSkipTabTitleUpdate_DedupesSameTitle(t *testing.T) {
	tabTitleMu.Lock()
	lastTabTitle = ""
	tabTitleMu.Unlock()

	if skip := shouldSkipTabTitleUpdate("wtx - feature"); skip {
		t.Fatalf("first update should not be skipped")
	}
	if skip := shouldSkipTabTitleUpdate("wtx - feature"); !skip {
		t.Fatalf("second identical update should be skipped")
	}
}

func TestShouldSkipTabTitleUpdate_AllowsDifferentTitle(t *testing.T) {
	tabTitleMu.Lock()
	lastTabTitle = "wtx - one"
	tabTitleMu.Unlock()

	if skip := shouldSkipTabTitleUpdate("wtx - two"); skip {
		t.Fatalf("different title should not be skipped")
	}
}
