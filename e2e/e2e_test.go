package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type runResult struct {
	out string
	err error
}

type testRepo struct {
	root      string
	worktree  string
	managedWT string
}

type testConfig struct {
	AgentCommand          string `json:"agent_command"`
	NewBranchBaseRef      string `json:"new_branch_base_ref,omitempty"`
	NewBranchFetchFirst   *bool  `json:"new_branch_fetch_first,omitempty"`
	IDECommand            string `json:"ide_command,omitempty"`
	MainScreenBranchLimit int    `json:"main_screen_branch_limit,omitempty"`
}

func wtxBin(t *testing.T) string {
	t.Helper()
	bin := strings.TrimSpace(os.Getenv("WTX_E2E_BIN"))
	if bin == "" {
		t.Skip("WTX_E2E_BIN not set; run via make e2e")
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("resolve bin path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("wtx binary not found at %s (set WTX_E2E_BIN): %v", abs, err)
	}
	return abs
}

func runWTX(t *testing.T, dir string, env map[string]string, args ...string) runResult {
	t.Helper()
	cmd := exec.Command(wtxBin(t), args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append([]string{}, os.Environ()...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return runResult{out: string(out), err: err}
}

func runCmd(t *testing.T, dir string, env map[string]string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = append([]string{}, os.Environ()...)
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %v failed: %v\n%s", name, args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func writeConfig(t *testing.T, home string, agent string) {
	t.Helper()
	if strings.TrimSpace(agent) == "" {
		agent = "true"
	}
	fetch := false
	cfg := testConfig{
		AgentCommand:          agent,
		NewBranchBaseRef:      "main",
		NewBranchFetchFirst:   &fetch,
		IDECommand:            "true",
		MainScreenBranchLimit: 10,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	data = append(data, '\n')
	cfgPath := filepath.Join(home, ".wtx", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func setupRepoWithManagedWorktree(t *testing.T) testRepo {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runCmd(t, repoRoot, nil, "git", "init")
	runCmd(t, repoRoot, nil, "git", "checkout", "-B", "main")
	runCmd(t, repoRoot, nil, "git", "config", "user.email", "e2e@example.test")
	runCmd(t, repoRoot, nil, "git", "config", "user.name", "WTX E2E")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("root\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	runCmd(t, repoRoot, nil, "git", "add", "README.md")
	runCmd(t, repoRoot, nil, "git", "commit", "-m", "init")
	runCmd(t, repoRoot, nil, "git", "branch", "feature/existing")

	managedRoot := filepath.Join(filepath.Dir(repoRoot), filepath.Base(repoRoot)+".wt")
	managedWorktree := filepath.Join(managedRoot, "wt.1")
	if err := os.MkdirAll(managedRoot, 0o755); err != nil {
		t.Fatalf("mkdir managed root: %v", err)
	}
	runCmd(t, repoRoot, nil, "git", "worktree", "add", "-b", "slot/one", managedWorktree, "main")

	return testRepo{root: repoRoot, worktree: managedRoot, managedWT: managedWorktree}
}

func setupSingleWorktreeRepo(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runCmd(t, repoRoot, nil, "git", "init")
	runCmd(t, repoRoot, nil, "git", "checkout", "-B", "main")
	runCmd(t, repoRoot, nil, "git", "config", "user.email", "e2e@example.test")
	runCmd(t, repoRoot, nil, "git", "config", "user.name", "WTX E2E")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("root\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	runCmd(t, repoRoot, nil, "git", "add", "README.md")
	runCmd(t, repoRoot, nil, "git", "commit", "-m", "init")
	runCmd(t, repoRoot, nil, "git", "branch", "feature/existing")
	return repoRoot
}

func toolPathDir(t *testing.T, includeGit bool) string {
	t.Helper()
	dir := t.TempDir()
	if includeGit {
		gitPath, err := exec.LookPath("git")
		if err != nil {
			t.Fatalf("git not found: %v", err)
		}
		if err := os.Symlink(gitPath, filepath.Join(dir, "git")); err != nil {
			t.Fatalf("symlink git: %v", err)
		}
	}
	return dir
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func testEnv(home string) map[string]string {
	return map[string]string{
		"HOME":              home,
		"WTX_DISABLE_TMUX":  "1",
		"WTX_DISABLE_ITERM": "1",
	}
}

func assertContains(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q\n--- got ---\n%s", want, got)
	}
}

func currentBranch(t *testing.T, path string) string {
	t.Helper()
	return runCmd(t, path, nil, "git", "rev-parse", "--abbrev-ref", "HEAD")
}

func TestCompletionInstallAndStatusHermetic(t *testing.T) {
	home := t.TempDir()
	env := testEnv(home)
	workDir := t.TempDir()

	before := runWTX(t, workDir, env, "completion", "status")
	if before.err != nil {
		t.Fatalf("completion status before install failed: %v\n%s", before.err, before.out)
	}
	assertContains(t, before.out, "installed: false")
	assertContains(t, before.out, "enabled: false")

	installed := runWTX(t, workDir, env, "completion", "install")
	if installed.err != nil {
		t.Fatalf("completion install failed: %v\n%s", installed.err, installed.out)
	}

	after := runWTX(t, workDir, env, "completion", "status")
	if after.err != nil {
		t.Fatalf("completion status after install failed: %v\n%s", after.err, after.out)
	}
	assertContains(t, after.out, "installed: true")
	assertContains(t, after.out, "enabled: true")

	second := runWTX(t, workDir, env, "completion", "install")
	if second.err != nil {
		t.Fatalf("second completion install failed: %v\n%s", second.err, second.out)
	}

	zshrc := filepath.Join(home, ".zshrc")
	data, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatalf("read zshrc: %v", err)
	}
	content := string(data)
	if strings.Count(content, "# >>> wtx completion >>>") != 1 {
		t.Fatalf("expected one completion block, got %d\n%s", strings.Count(content, "# >>> wtx completion >>>"), content)
	}
	if strings.Count(content, "# <<< wtx completion <<<") != 1 {
		t.Fatalf("expected one completion block end, got %d\n%s", strings.Count(content, "# <<< wtx completion <<<"), content)
	}
	if _, err := os.Stat(filepath.Join(home, ".wtx", "completions", "_wtx")); err != nil {
		t.Fatalf("completion script missing: %v", err)
	}
}

func TestCheckoutExistingBranchNonInteractive(t *testing.T) {
	repo := setupRepoWithManagedWorktree(t)
	home := t.TempDir()
	writeConfig(t, home, "true")
	env := testEnv(home)

	result := runWTX(t, repo.root, env, "co", "feature/existing")
	if result.err != nil {
		t.Fatalf("checkout existing failed: %v\n%s", result.err, result.out)
	}

	rootBranch := currentBranch(t, repo.root)
	slotBranch := currentBranch(t, repo.managedWT)
	if rootBranch != "feature/existing" && slotBranch != "feature/existing" {
		t.Fatalf("expected feature/existing to be checked out in a worktree, got root=%q slot=%q", rootBranch, slotBranch)
	}
}

func TestCheckoutNewBranchNonInteractive(t *testing.T) {
	repo := setupRepoWithManagedWorktree(t)
	home := t.TempDir()
	writeConfig(t, home, "true")
	env := testEnv(home)

	result := runWTX(t, repo.root, env, "checkout", "-b", "feature/new", "--from", "main", "--no-fetch")
	if result.err != nil {
		t.Fatalf("checkout new failed: %v\n%s", result.err, result.out)
	}

	rootBranch := currentBranch(t, repo.root)
	slotBranch := currentBranch(t, repo.managedWT)
	if rootBranch != "feature/new" && slotBranch != "feature/new" {
		t.Fatalf("expected feature/new to be checked out in a worktree, got root=%q slot=%q", rootBranch, slotBranch)
	}
	runCmd(t, repo.root, nil, "git", "show-ref", "--verify", "refs/heads/feature/new")
}

func TestCheckoutLocksWorktreeDuringActiveRun(t *testing.T) {
	repoRoot := setupSingleWorktreeRepo(t)
	home := t.TempDir()
	writeConfig(t, home, "sleep 3")
	envA := testEnv(home)
	envA["WTX_OWNER_ID"] = "owner-a"
	envB := testEnv(home)
	envB["WTX_OWNER_ID"] = "owner-b"

	cmd := exec.Command(wtxBin(t), "co", "feature/existing")
	cmd.Dir = repoRoot
	cmd.Env = append([]string{}, os.Environ()...)
	for k, v := range envA {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var firstOut bytes.Buffer
	cmd.Stdout = &firstOut
	cmd.Stderr = &firstOut
	if err := cmd.Start(); err != nil {
		t.Fatalf("start first checkout: %v", err)
	}

	lockedObserved := false
	locksDir := filepath.Join(home, ".wtx", "locks")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(locksDir, "*.lock"))
		if len(matches) > 0 {
			result := runWTX(t, repoRoot, envB, "co", "feature/existing")
			if result.err != nil && strings.Contains(result.out, "worktree locked") {
				lockedObserved = true
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !lockedObserved {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("expected lock contention while first checkout is active\nfirst output:\n%s", firstOut.String())
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("first checkout failed: %v\n%s", err, firstOut.String())
	}

	after := runWTX(t, repoRoot, envB, "co", "feature/existing")
	if after.err != nil {
		t.Fatalf("checkout after lock release failed: %v\n%s", after.err, after.out)
	}
}

func TestCheckoutValidationErrorNonInteractive(t *testing.T) {
	repo := setupRepoWithManagedWorktree(t)
	home := t.TempDir()
	writeConfig(t, home, "true")
	env := testEnv(home)

	result := runWTX(t, repo.root, env, "checkout", "-b", "feature/bad", "--from", "does/not/exist", "--no-fetch")
	if result.err == nil {
		t.Fatalf("expected validation failure, got success\n%s", result.out)
	}
	assertContains(t, result.out, "no remotes are configured")
}

func TestTmuxStatusWithFakeGH(t *testing.T) {
	repo := setupRepoWithManagedWorktree(t)
	home := t.TempDir()
	fakeBin := toolPathDir(t, true)
	ghLog := filepath.Join(t.TempDir(), "gh.log")
	ghScript := `#!/bin/sh
set -eu
printf "%s\n" "$*" >> "$WTX_FAKE_GH_LOG"
if [ "${1:-}" = "pr" ] && [ "${2:-}" = "view" ]; then
  cat <<'JSON'
{"number":123,"url":"https://example.test/pr/123","headRefName":"feature/existing","baseRefName":"main","title":"fake","isDraft":false,"state":"OPEN","mergeStateStatus":"CLEAN","updatedAt":"2026-01-01T00:00:00Z","mergedAt":"","reviewDecision":"APPROVED","statusCheckRollup":[]}
JSON
  exit 0
fi
printf "unsupported gh args: %s\n" "$*" >&2
exit 1
`
	writeExecutable(t, filepath.Join(fakeBin, "gh"), ghScript)
	env := testEnv(home)
	env["PATH"] = fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	env["WTX_FAKE_GH_LOG"] = ghLog

	result := runWTX(t, repo.root, env, "tmux-status", "--worktree", repo.managedWT)
	if result.err != nil {
		t.Fatalf("tmux-status with fake gh failed: %v\n%s", result.err, result.out)
	}
	assertContains(t, result.out, "PR #123")

	logData, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatalf("read fake gh log: %v", err)
	}
	assertContains(t, string(logData), "pr view")
}

func TestTmuxStatusWithoutGHFallsBack(t *testing.T) {
	repo := setupRepoWithManagedWorktree(t)
	home := t.TempDir()
	fakeBin := toolPathDir(t, true)
	env := testEnv(home)
	env["PATH"] = fakeBin

	result := runWTX(t, repo.root, env, "tmux-status", "--worktree", repo.managedWT)
	if result.err != nil {
		t.Fatalf("tmux-status without gh failed: %v\n%s", result.err, result.out)
	}
	assertContains(t, result.out, "PR - | CI - | GH - | Review -")
}

func TestDisableTmuxSkipsTmuxBinaryUsage(t *testing.T) {
	repo := setupRepoWithManagedWorktree(t)
	home := t.TempDir()
	writeConfig(t, home, "true")
	fakeBin := toolPathDir(t, true)
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxScript := fmt.Sprintf(`#!/bin/sh
set -eu
printf "%%s\n" "$*" >> %q
exit 0
`, logPath)
	writeExecutable(t, filepath.Join(fakeBin, "tmux"), tmuxScript)
	env := testEnv(home)
	env["PATH"] = fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	env["WTX_DISABLE_TMUX"] = "1"

	result := runWTX(t, repo.root, env, "co", "feature/existing")
	if result.err != nil {
		t.Fatalf("checkout with tmux disabled failed: %v\n%s", result.err, result.out)
	}

	if data, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("expected no tmux invocations, got:\n%s", string(data))
	}
}

func TestDisableITermSuppressesITermEscapes(t *testing.T) {
	repo := setupRepoWithManagedWorktree(t)
	home := t.TempDir()
	writeConfig(t, home, "true")
	env := testEnv(home)
	env["TERM_PROGRAM"] = "iTerm.app"
	env["WTX_DISABLE_ITERM"] = "1"

	result := runWTX(t, repo.root, env, "co", "feature/existing")
	if result.err != nil {
		t.Fatalf("checkout with iTerm disabled failed: %v\n%s", result.err, result.out)
	}
	if strings.Contains(result.out, "1337;SetTabColor") {
		t.Fatalf("expected iTerm escapes to be disabled, got output:\n%s", result.out)
	}
}

func TestTestModeBypassesInteractiveUI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive bypass e2e is unix-oriented")
	}
	home := t.TempDir()
	env := testEnv(home)
	env["WTX_TEST_MODE"] = "1"

	result := runWTX(t, t.TempDir(), env)
	if result.err != nil {
		t.Fatalf("test mode root command failed: %v\n%s", result.err, result.out)
	}
	assertContains(t, result.out, "interactive UI bypassed")
}
