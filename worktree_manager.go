package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type WorktreeManager struct {
	cwd     string
	lockMgr *LockManager
	mu      sync.Mutex
	byRepo  map[string]repoBaseRefState
}

const maxRecentBranches = 15

type repoBaseRefState struct {
	Remote  string
	BaseRef string
	Warming bool
}

func NewWorktreeManager(cwd string, lockMgr *LockManager) *WorktreeManager {
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	return &WorktreeManager{
		cwd:     cwd,
		lockMgr: lockMgr,
		byRepo:  make(map[string]repoBaseRefState),
	}
}

func (m *WorktreeManager) ListForStatusBase() WorktreeStatus {
	status := WorktreeStatus{}
	status.CWD = m.cwd
	gitPath, err := gitPath()
	if err != nil {
		status.GitInstalled = false
		return status
	}
	status.GitInstalled = true

	repoRoot, err := repoRootForDir(m.cwd, gitPath)
	if err != nil {
		status.InRepo = false
		return status
	}
	status.InRepo = true
	status.RepoRoot = repoRoot
	status.BaseRef = m.ResolveBaseRefForNewBranch()

	worktrees, malformed, err := listWorktrees(repoRoot, gitPath)
	if err != nil {
		status.Err = err
		return status
	}
	status.Worktrees = worktrees
	status.Malformed = malformed

	return status
}

func (m *WorktreeManager) ResolveBaseRefForNewBranch() string {
	gitPath, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return "main"
	}
	if cached := m.cachedBaseRef(repoRoot); cached != "" {
		return cached
	}
	remote := m.cachedRemote(repoRoot)
	if remote == "" {
		remote = preferredRemoteName(repoRoot, gitPath)
		m.setCachedRemote(repoRoot, remote)
	}
	fallbackBranch := fallbackBaseBranchNoRemote(repoRoot, gitPath)
	if strings.TrimSpace(remote) == "" {
		return fallbackBranch
	}
	fallback := remote + "/" + fallbackBranch
	m.ensureBaseRefWarm(repoRoot, remote, fallback)
	return fallback
}

func (m *WorktreeManager) CreateWorktree(branch string, baseRef string) (WorktreeInfo, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return WorktreeInfo{}, errors.New("branch name required")
	}
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		baseRef = "HEAD"
	}

	gitPath, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return WorktreeInfo{}, err
	}
	layoutRoot := worktreeLayoutRoot(repoRoot, gitPath)

	target, err := nextWorktreePath(layoutRoot)
	if err != nil {
		return WorktreeInfo{}, err
	}
	lock, err := m.lockMgr.Acquire(repoRoot, target)
	if err != nil {
		return WorktreeInfo{}, err
	}
	defer lock.Release()

	baseRef = baseRefForWorktreeAdd(repoRoot, gitPath, baseRef)
	if err := runCommandInDir(layoutRoot, gitPath, "worktree", "add", "-b", branch, target, baseRef); err != nil {
		return WorktreeInfo{}, err
	}

	return WorktreeInfo{Path: target, Branch: branch}, nil
}

func (m *WorktreeManager) CreateWorktreeFromBranch(branch string) (WorktreeInfo, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return WorktreeInfo{}, errors.New("branch name required")
	}

	gitPath, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return WorktreeInfo{}, err
	}
	layoutRoot := worktreeLayoutRoot(repoRoot, gitPath)

	target, err := nextWorktreePath(layoutRoot)
	if err != nil {
		return WorktreeInfo{}, err
	}
	lock, err := m.lockMgr.Acquire(repoRoot, target)
	if err != nil {
		return WorktreeInfo{}, err
	}
	defer lock.Release()

	if err := runCommandInDir(layoutRoot, gitPath, "worktree", "add", target, branch); err != nil {
		return WorktreeInfo{}, err
	}

	return WorktreeInfo{Path: target, Branch: branch}, nil
}

func (m *WorktreeManager) ListLocalBranchesByRecentUse() ([]string, error) {
	gitPath, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return nil, err
	}

	output, err := commandOutputInDir(repoRoot, gitPath, "reflog", "show", "--format=%gs")
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	branches := make([]string, 0, maxRecentBranches)
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, "checkout: ") {
			continue
		}
		idx := strings.LastIndex(line, " to ")
		if idx == -1 {
			continue
		}
		name := strings.TrimSpace(line[idx+4:])
		if name == "" || strings.HasPrefix(name, "origin/") || seen[name] {
			continue
		}
		seen[name] = true
		branches = append(branches, name)
		if len(branches) >= maxRecentBranches {
			break
		}
	}
	return branches, nil
}

func (m *WorktreeManager) DeleteWorktree(path string, force bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("worktree path required")
	}

	gitPath, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return err
	}
	if err := ensureManagedWorktreePath(repoRoot, path); err != nil {
		return err
	}

	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	lock, err := m.lockMgr.Acquire(repoRoot, path)
	if err != nil {
		return err
	}
	defer lock.Release()

	if err := runCommandInDir(repoRoot, gitPath, args...); err != nil {
		return err
	}
	return nil
}

func commandErrorWithOutput(err error, out []byte) error {
	msg := strings.TrimSpace(string(out))
	if msg != "" {
		return errors.New(msg)
	}
	return err
}

func commandOutputInDir(dir string, path string, args ...string) ([]byte, error) {
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, commandErrorWithOutput(err, out)
	}
	return out, nil
}

func runCommandInDir(dir string, path string, args ...string) error {
	_, err := commandOutputInDir(dir, path, args...)
	return err
}

func (m *WorktreeManager) CanDeleteWorktree(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("worktree path required")
	}
	_, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return err
	}
	return ensureManagedWorktreePath(repoRoot, path)
}

func (m *WorktreeManager) CheckoutExistingBranch(worktreePath string, branch string) error {
	worktreePath = strings.TrimSpace(worktreePath)
	branch = strings.TrimSpace(branch)
	if worktreePath == "" {
		return errors.New("worktree path required")
	}
	if branch == "" {
		return errors.New("branch name required")
	}
	gitPath, err := requireGitPath()
	if err != nil {
		return err
	}
	return runCommandInDir(worktreePath, gitPath, "checkout", branch)
}

func (m *WorktreeManager) CheckoutNewBranch(worktreePath string, branch string, baseRef string, doFetch bool) error {
	worktreePath = strings.TrimSpace(worktreePath)
	branch = strings.TrimSpace(branch)
	baseRef = strings.TrimSpace(baseRef)
	if worktreePath == "" {
		return errors.New("worktree path required")
	}
	if branch == "" {
		return errors.New("branch name required")
	}
	gitPath, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return err
	}
	if doFetch {
		if err := m.FetchRepo(); err != nil {
			return err
		}
	}
	if localBranchExists(repoRoot, gitPath, branch) {
		cmd := exec.Command(gitPath, "checkout", branch)
		cmd.Dir = worktreePath
		return cmd.Run()
	}
	if baseRef == "" {
		baseRef = "HEAD"
	}
	cmd := exec.Command(gitPath, "checkout", "-b", branch, baseRef)
	cmd.Dir = worktreePath
	return cmd.Run()
}

func (m *WorktreeManager) FetchRepo() error {
	gitPath, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return err
	}
	cmd := exec.Command(gitPath, "fetch")
	cmd.Dir = repoRoot
	return cmd.Run()
}

func (m *WorktreeManager) AcquireWorktreeLock(worktreePath string) (*WorktreeLock, error) {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return nil, errors.New("worktree path required")
	}
	_, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return nil, err
	}
	return m.lockMgr.Acquire(repoRoot, worktreePath)
}

func (m *WorktreeManager) UnlockWorktree(worktreePath string) error {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return errors.New("worktree path required")
	}
	_, repoRoot, err := requireGitContext(m.cwd)
	if err != nil {
		return err
	}
	return m.lockMgr.ForceUnlock(repoRoot, worktreePath)
}

func listWorktrees(repoRoot string, gitPath string) ([]WorktreeInfo, []string, error) {
	output, err := commandOutputInDir(repoRoot, gitPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, nil, err
	}
	return parseWorktrees(string(output))
}

func parseWorktrees(output string) ([]WorktreeInfo, []string, error) {
	var worktrees []WorktreeInfo
	var malformed []string
	var current *WorktreeInfo

	lines := strings.Split(output, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "worktree":
			if len(fields) < 2 {
				malformed = append(malformed, line)
				current = nil
				continue
			}
			worktrees = append(worktrees, WorktreeInfo{Path: strings.Join(fields[1:], " ")})
			current = &worktrees[len(worktrees)-1]
		case "branch":
			if current == nil {
				malformed = append(malformed, line)
				continue
			}
			current.Branch = shortBranch(strings.Join(fields[1:], " "))
		case "detached":
			if current == nil {
				malformed = append(malformed, line)
				continue
			}
			if current.Branch == "" {
				current.Branch = "detached"
			}
		default:
			if current == nil {
				malformed = append(malformed, line)
			}
		}
	}

	for i := range worktrees {
		if worktrees[i].Branch == "" {
			worktrees[i].Branch = "detached"
		}
	}
	return worktrees, malformed, nil
}

func shortBranch(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "refs/heads/")
	value = strings.TrimPrefix(value, "refs/remotes/")
	value = strings.TrimPrefix(value, "origin/")
	if value == "" {
		return "detached"
	}
	return value
}

func gitOutputInDir(dir string, path string, args ...string) (string, error) {
	output, err := commandOutputInDir(dir, path, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func gitRunInDir(dir string, path string, args ...string) error {
	return runCommandInDir(dir, path, args...)
}

func fallbackBaseBranchNoRemote(repoRoot string, gitPath string) string {
	mainExists := localBranchExists(repoRoot, gitPath, "main")
	current, err := gitOutputInDir(repoRoot, gitPath, "branch", "--show-current")
	if err == nil {
		return chooseFallbackBaseNoRemote(mainExists, current)
	}
	return chooseFallbackBaseNoRemote(mainExists, "")
}

func chooseFallbackBaseNoRemote(mainExists bool, currentBranch string) string {
	if mainExists {
		return "main"
	}
	currentBranch = strings.TrimSpace(currentBranch)
	if currentBranch != "" && currentBranch != "detached" {
		return currentBranch
	}
	return "main"
}

func localBranchExists(repoRoot string, gitPath string, branch string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false
	}
	_, err := gitOutputInDir(repoRoot, gitPath, "show-ref", "--verify", "refs/heads/"+branch)
	return err == nil
}

func baseRefForWorktreeAdd(repoRoot string, gitPath string, baseRef string) string {
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" || baseRef == "HEAD" {
		return "HEAD"
	}
	remote := preferredRemoteName(repoRoot, gitPath)
	if remoteRef, ok := asRemoteRef(repoRoot, gitPath, remote, baseRef); ok {
		return remoteRef
	}
	branch := shortBranch(baseRef)
	if strings.TrimSpace(branch) != "" && branch != "detached" {
		if localBranchExists(repoRoot, gitPath, branch) {
			return branch
		}
		if remoteRef, ok := asRemoteRef(repoRoot, gitPath, remote, branch); ok {
			return remoteRef
		}
	}
	if remoteRef, ok := asRemoteRef(repoRoot, gitPath, remote, baseRef); ok {
		return remoteRef
	}
	return baseRef
}

func defaultBaseRefFromGitHub(repoRoot string) (string, error) {
	owner, name, err := resolveGitHubRepo(repoRoot)
	if err != nil {
		return "", err
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return "", err
	}
	out, err := commandOutputInDir(repoRoot, ghPath, "repo", "view", owner+"/"+name, "--json", "defaultBranchRef", "--jq", ".defaultBranchRef.name")
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" {
		return "", errors.New("github default branch not found")
	}
	return ref, nil
}

func asRemoteRef(repoRoot string, gitPath string, remote string, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", false
	}
	if ref == "" {
		return "", false
	}
	if strings.HasPrefix(ref, remote+"/") {
		return ref, true
	}
	remoteRef := remote + "/" + ref
	if _, err := gitOutputInDir(repoRoot, gitPath, "show-ref", "--verify", "refs/remotes/"+remoteRef); err == nil {
		return remoteRef, true
	}
	return "", false
}

func preferredRemoteName(repoRoot string, gitPath string) string {
	remotes, err := gitOutputInDir(repoRoot, gitPath, "remote")
	if err != nil {
		return ""
	}
	list := strings.Split(strings.TrimSpace(remotes), "\n")
	for _, remote := range list {
		if trimmed := strings.TrimSpace(remote); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (m *WorktreeManager) cachedBaseRef(repoRoot string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.TrimSpace(m.byRepo[repoRoot].BaseRef)
}

func (m *WorktreeManager) cachedRemote(repoRoot string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.TrimSpace(m.byRepo[repoRoot].Remote)
}

func (m *WorktreeManager) setCachedRemote(repoRoot string, remote string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.byRepo[repoRoot]
	entry.Remote = strings.TrimSpace(remote)
	m.byRepo[repoRoot] = entry
}

func (m *WorktreeManager) ensureBaseRefWarm(repoRoot string, remote string, fallback string) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return
	}
	m.mu.Lock()
	entry := m.byRepo[repoRoot]
	if strings.TrimSpace(entry.BaseRef) != "" || entry.Warming {
		m.mu.Unlock()
		return
	}
	entry.Warming = true
	entry.Remote = strings.TrimSpace(remote)
	m.byRepo[repoRoot] = entry
	m.mu.Unlock()

	go func() {
		resolved := strings.TrimSpace(fallback)
		if strings.TrimSpace(entry.Remote) != "" {
			if ghRef, err := defaultBaseRefFromGitHub(repoRoot); err == nil {
				ghRef = shortBranch(ghRef)
				if ghRef != "" && ghRef != "detached" {
					resolved = entry.Remote + "/" + ghRef
				}
			}
		}
		if strings.TrimSpace(resolved) == "" {
			resolved = "main"
		}
		m.mu.Lock()
		final := m.byRepo[repoRoot]
		final.Remote = entry.Remote
		final.BaseRef = resolved
		final.Warming = false
		m.byRepo[repoRoot] = final
		m.mu.Unlock()
	}()
}

func worktreePathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func nextWorktreePath(repoRoot string) (string, error) {
	worktreeRoot := managedWorktreeRoot(repoRoot)
	for i := 1; i < 100; i++ {
		candidate := filepath.Join(worktreeRoot, fmt.Sprintf("wt.%d", i))
		_, statErr := os.Stat(candidate)
		if errors.Is(statErr, os.ErrNotExist) {
			return candidate, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
	}
	return "", errors.New("no available worktree path")
}

func worktreeLayoutRoot(repoRoot string, gitPath string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" || strings.TrimSpace(gitPath) == "" {
		return repoRoot
	}
	commonDir, err := gitOutputInDir(repoRoot, gitPath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return repoRoot
	}
	commonDir = strings.TrimSpace(commonDir)
	if strings.EqualFold(filepath.Base(commonDir), ".git") {
		return filepath.Dir(commonDir)
	}
	return repoRoot
}

func ensureManagedWorktreePath(repoRoot string, worktreePath string) error {
	managedRoot := managedWorktreeRoot(repoRoot)
	managedRootReal, err := realPathOrAbs(managedRoot)
	if err != nil {
		return err
	}
	worktreeReal, err := realPathOrAbs(worktreePath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(managedRootReal, worktreeReal)
	if err != nil {
		return err
	}
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("cannot delete worktree outside %s", managedRoot)
	}
	return nil
}

func managedWorktreeRoot(repoRoot string) string {
	base := filepath.Base(repoRoot)
	parent := filepath.Dir(repoRoot)
	return filepath.Join(parent, base+".wt")
}
