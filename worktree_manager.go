package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type WorktreeInfo struct {
	Path               string
	Branch             string
	Available          bool
	PRURL              string
	PRNumber           int
	HasPR              bool
	PRStatus           string
	CIState            PRCIState
	CIDone             int
	CITotal            int
	Approved           bool
	UnresolvedComments int
}

type WorktreeStatus struct {
	GitInstalled bool
	InRepo       bool
	RepoRoot     string
	CWD          string
	BaseRef      string
	Worktrees    []WorktreeInfo
	Orphaned     []WorktreeInfo
	Malformed    []string
	Err          error
}

type WorktreeManager struct {
	cwd     string
	lockMgr *LockManager
	ghMgr   *GHManager
}

func NewWorktreeManager(cwd string, lockMgr *LockManager, ghMgr *GHManager) *WorktreeManager {
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	if lockMgr == nil {
		lockMgr = NewLockManager()
	}
	if ghMgr == nil {
		ghMgr = NewGHManager()
	}
	return &WorktreeManager{cwd: cwd, lockMgr: lockMgr, ghMgr: ghMgr}
}

func (m *WorktreeManager) Status() WorktreeStatus {
	status := WorktreeStatus{}
	status.CWD = m.cwd
	gitPath, err := exec.LookPath("git")
	if err != nil {
		status.GitInstalled = false
		return status
	}
	status.GitInstalled = true

	repoRoot, err := gitOutputInDir(m.cwd, gitPath, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		status.InRepo = false
		return status
	}
	status.InRepo = true
	status.RepoRoot = repoRoot
	status.BaseRef = defaultBaseRef(repoRoot, gitPath)

	worktrees, malformed, err := listWorktrees(repoRoot, gitPath)
	if err != nil {
		status.Err = err
		return status
	}
	status.Worktrees = worktrees
	status.Malformed = malformed

	orphaned := make([]WorktreeInfo, 0)
	for _, wt := range worktrees {
		exists, err := worktreePathExists(wt.Path)
		if err != nil {
			status.Err = err
			return status
		}
		if !exists {
			wt.Available = false
			for i := range status.Worktrees {
				if status.Worktrees[i].Path == wt.Path {
					status.Worktrees[i].Available = false
					break
				}
			}
			orphaned = append(orphaned, wt)
			continue
		}
		available, err := m.lockMgr.IsAvailable(repoRoot, wt.Path)
		if err != nil {
			status.Err = err
			return status
		}
		for i := range status.Worktrees {
			if status.Worktrees[i].Path == wt.Path {
				status.Worktrees[i].Available = available
				break
			}
		}
	}
	status.Orphaned = orphaned

	return status
}

func (m *WorktreeManager) PRDataForStatus(status WorktreeStatus) map[string]PRData {
	data, _ := m.prDataForStatus(status, false)
	return data
}

func (m *WorktreeManager) PRDataForStatusForce(status WorktreeStatus) map[string]PRData {
	data, _ := m.prDataForStatus(status, true)
	return data
}

func (m *WorktreeManager) PRDataForStatusWithError(status WorktreeStatus, force bool) (map[string]PRData, error) {
	return m.prDataForStatus(status, force)
}

func (m *WorktreeManager) prDataForStatus(status WorktreeStatus, force bool) (map[string]PRData, error) {
	if !status.InRepo || strings.TrimSpace(status.RepoRoot) == "" {
		return map[string]PRData{}, nil
	}
	branches := make([]string, 0, len(status.Worktrees))
	for _, wt := range status.Worktrees {
		b := strings.TrimSpace(wt.Branch)
		if b == "" || b == "detached" {
			continue
		}
		branches = append(branches, b)
	}
	if force {
		return m.ghMgr.PRDataByBranchForce(status.RepoRoot, branches)
	}
	return m.ghMgr.PRDataByBranch(status.RepoRoot, branches)
}

func (m *WorktreeManager) CreateWorktree(branch string) (WorktreeInfo, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return WorktreeInfo{}, errors.New("branch name required")
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return WorktreeInfo{}, errors.New("git not installed")
	}

	repoRoot, err := gitOutputInDir(m.cwd, gitPath, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return WorktreeInfo{}, errors.New("not in a git repository")
	}

	target, err := nextWorktreePath(repoRoot)
	if err != nil {
		return WorktreeInfo{}, err
	}
	lock, err := m.lockMgr.Acquire(repoRoot, target)
	if err != nil {
		return WorktreeInfo{}, err
	}
	defer lock.Release()

	cmd := exec.Command(gitPath, "worktree", "add", "-b", branch, target, "HEAD")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		return WorktreeInfo{}, err
	}

	return WorktreeInfo{Path: target, Branch: branch}, nil
}

func (m *WorktreeManager) CreateWorktreeFromBranch(branch string) (WorktreeInfo, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return WorktreeInfo{}, errors.New("branch name required")
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return WorktreeInfo{}, errors.New("git not installed")
	}

	repoRoot, err := gitOutputInDir(m.cwd, gitPath, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return WorktreeInfo{}, errors.New("not in a git repository")
	}

	target, err := nextWorktreePath(repoRoot)
	if err != nil {
		return WorktreeInfo{}, err
	}
	lock, err := m.lockMgr.Acquire(repoRoot, target)
	if err != nil {
		return WorktreeInfo{}, err
	}
	defer lock.Release()

	cmd := exec.Command(gitPath, "worktree", "add", target, branch)
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		return WorktreeInfo{}, err
	}

	return WorktreeInfo{Path: target, Branch: branch}, nil
}

func (m *WorktreeManager) ListLocalBranchesByRecentUse() ([]string, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git not installed")
	}

	repoRoot, err := gitOutputInDir(m.cwd, gitPath, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return nil, errors.New("not in a git repository")
	}

	cmd := exec.Command(gitPath, "for-each-ref", "--sort=-committerdate", "--format=%(refname:short)", "refs/heads")
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	branches := make([]string, 0, len(lines))
	for _, raw := range lines {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}

func (m *WorktreeManager) DeleteWorktree(path string, force bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("worktree path required")
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return errors.New("git not installed")
	}

	repoRoot, err := gitOutputInDir(m.cwd, gitPath, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return errors.New("not in a git repository")
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

	cmd := exec.Command(gitPath, args...)
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
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
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return errors.New("git not installed")
	}
	cmd := exec.Command(gitPath, "checkout", branch)
	cmd.Dir = worktreePath
	return cmd.Run()
}

func (m *WorktreeManager) AcquireWorktreeLock(worktreePath string) (*WorktreeLock, error) {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return nil, errors.New("worktree path required")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git not installed")
	}
	repoRoot, err := gitOutputInDir(m.cwd, gitPath, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return nil, errors.New("not in a git repository")
	}
	return m.lockMgr.Acquire(repoRoot, worktreePath)
}

func (m *WorktreeManager) UnlockWorktree(worktreePath string) error {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return errors.New("worktree path required")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return errors.New("git not installed")
	}
	repoRoot, err := gitOutputInDir(m.cwd, gitPath, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return errors.New("not in a git repository")
	}
	return m.lockMgr.ForceUnlock(repoRoot, worktreePath)
}

func listWorktrees(repoRoot string, gitPath string) ([]WorktreeInfo, []string, error) {
	cmd := exec.Command(gitPath, "worktree", "list", "--porcelain")
	cmd.Dir = repoRoot
	output, err := cmd.Output()
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
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func defaultBaseRef(repoRoot string, gitPath string) string {
	ref, err := gitOutputInDir(repoRoot, gitPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err == nil && ref != "" {
		return ref
	}
	ref, err = gitOutputInDir(repoRoot, gitPath, "symbolic-ref", "--short", "HEAD")
	if err == nil && ref != "" {
		return ref
	}
	return "main"
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
	base := filepath.Base(repoRoot)
	parent := filepath.Dir(repoRoot)
	worktreeRoot := filepath.Join(parent, base+".wt")
	for i := 1; i < 10000; i++ {
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
