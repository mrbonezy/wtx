//go:build local_e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalE2ECheckoutNewBranchWithFetchOnIsolatedRepo(t *testing.T) {
	if strings.TrimSpace(os.Getenv("WTX_LOCAL_E2E")) != "1" {
		t.Skip("set WTX_LOCAL_E2E=1 to run local-only e2e tests")
	}

	root := t.TempDir()
	originBare := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	runCmd(t, root, nil, "git", "init", "--bare", originBare)
	runCmd(t, root, nil, "git", "init", seed)
	runCmd(t, seed, nil, "git", "checkout", "-B", "main")
	runCmd(t, seed, nil, "git", "config", "user.email", "local-e2e@example.test")
	runCmd(t, seed, nil, "git", "config", "user.name", "WTX Local E2E")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	runCmd(t, seed, nil, "git", "add", "README.md")
	runCmd(t, seed, nil, "git", "commit", "-m", "init main")
	runCmd(t, seed, nil, "git", "remote", "add", "origin", originBare)
	runCmd(t, seed, nil, "git", "push", "-u", "origin", "main")

	runCmd(t, root, nil, "git", "clone", originBare, clone)
	runCmd(t, clone, nil, "git", "config", "user.email", "local-e2e@example.test")
	runCmd(t, clone, nil, "git", "config", "user.name", "WTX Local E2E")

	managedRoot := clone + ".wt"
	managedWorktree := filepath.Join(managedRoot, "wt.1")
	if err := os.MkdirAll(managedRoot, 0o755); err != nil {
		t.Fatalf("mkdir managed root: %v", err)
	}
	runCmd(t, clone, nil, "git", "worktree", "add", "-b", "slot/one", managedWorktree, "main")

	home := t.TempDir()
	writeConfig(t, home, "true")
	env := testEnv(home)
	branch := "feature/local-e2e"
	result := runWTX(t, clone, env, "checkout", "-b", branch, "--from", "origin/main", "--fetch")
	if result.err != nil {
		t.Fatalf("local e2e checkout failed: %v\n%s", result.err, result.out)
	}

	rootBranch := currentBranch(t, clone)
	slotBranch := currentBranch(t, managedWorktree)
	if rootBranch != branch && slotBranch != branch {
		t.Fatalf("expected %q to be checked out in a worktree, got root=%q slot=%q", branch, rootBranch, slotBranch)
	}
	runCmd(t, clone, nil, "git", "show-ref", "--verify", "refs/heads/"+branch)
}
