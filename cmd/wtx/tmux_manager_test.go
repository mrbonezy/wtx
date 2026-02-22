package main

import "testing"

func TestParseBoolArg(t *testing.T) {
	if !parseBoolArg([]string{"--worktree", "/tmp/wt.1", "--force-unlock"}, "--force-unlock") {
		t.Fatalf("expected --force-unlock to be detected")
	}
	if parseBoolArg([]string{"--worktree", "/tmp/wt.1"}, "--force-unlock") {
		t.Fatalf("did not expect --force-unlock when flag is absent")
	}
}
