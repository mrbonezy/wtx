//go:build legacy
// +build legacy

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/manifoldco/promptui"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "wtx error:", err)
		os.Exit(exitCode(err))
	}
}

func run(args []string) error {
	debug, subcommand, err := parseArgs(args)
	if err != nil {
		return err
	}
	setDebug(debug)
	if subcommand == "setup" {
		return runSetup()
	}
	if subcommand == "version" {
		fmt.Fprintln(os.Stdout, versionString())
		return nil
	}
	if subcommand == "check" {
		return checkForUpdates()
	}
	if subcommand == "update" {
		return runUpdate()
	}

	updateCh := startUpdateCheck()
	repoInfo, ok := detectRepoInfo()
	if !ok {
		if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
			setITermGrayTab()
		}
		maybePrintUpdate(updateCh)
		return nil
	}

	selection, err := selectWorktree(repoInfo)
	if err != nil {
		return err
	}

	if selection.Kind == selectionNoWorktree {
		setTitle(selection.Task)
		if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
			setITermGrayTab()
		}
		maybePrintUpdate(updateCh)
		return nil
	}

	config, err := loadConfig()
	if err != nil {
		return err
	}

	lock := selection.Lock
	title := fmt.Sprintf("[%s][%s]", repoInfo.Name, lock.Branch)
	lock.Title = title
	lock.ITermBlue = os.Getenv("TERM_PROGRAM") == "iTerm.app"

	lock.StartToucher()
	defer lock.StopToucher()
	defer lock.Release()

	fmt.Fprintln(os.Stdout, "using worktree at", lock.WorktreePath)
	maybePrintUpdate(updateCh)

	setTitle(title)
	if lock.ITermBlue {
		setITermBlueTab()
	}

	agentCmd := strings.TrimSpace(config.AgentCommand)
	if agentCmd == "" {
		agentCmd = defaultAgentCommand
	}

	if agentCmd == "cd" {
		shellPath := strings.TrimSpace(os.Getenv("SHELL"))
		if shellPath == "" {
			shellPath = "/bin/sh"
		}
		cmd := exec.Command(shellPath)
		cmd.Dir = lock.WorktreePath
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command("/bin/sh", "-c", agentCmd)
	cmd.Dir = lock.WorktreePath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func usageError() error {
	fmt.Fprintln(os.Stderr, "usage: wtx [-d] [setup|version|check|update]")
	return errors.New("invalid arguments")
}

var debugFlag bool
var debugWriter io.Writer
var debugOnce sync.Once

func setDebug(enabled bool) {
	debugFlag = enabled
}

func debugEnabled() bool {
	return debugFlag
}

func debugLogf(format string, args ...any) {
	if !debugEnabled() {
		return
	}
	debugOnce.Do(func() {
		path := strings.TrimSpace(os.Getenv("WTX_DEBUG_LOG"))
		if path == "" {
			path = "/tmp/wtx-debug.log"
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			debugWriter = os.Stderr
			return
		}
		debugWriter = file
	})
	if debugWriter == nil {
		debugWriter = os.Stderr
	}
	fmt.Fprintf(debugWriter, format+"\n", args...)
}

func parseArgs(args []string) (bool, string, error) {
	if len(args) <= 1 {
		return false, "", nil
	}
	debug := false
	subcommand := ""
	for _, arg := range args[1:] {
		switch arg {
		case "-d", "--debug":
			debug = true
		case "setup":
			if subcommand != "" {
				return false, "", usageError()
			}
			subcommand = "setup"
		case "version", "check", "update":
			if subcommand != "" {
				return false, "", usageError()
			}
			subcommand = arg
		default:
			return false, "", usageError()
		}
	}
	return debug, subcommand, nil
}

func setTitle(title string) {
	osc0 := "\x1b]0;" + title + "\x07"
	osc2 := "\x1b]2;" + title + "\x07"
	fmt.Fprint(os.Stdout, osc0, osc2)
}

func setITermBlueTab() {
	fmt.Fprint(os.Stdout,
		"\x1b]1337;SetTabColor=rgb:00/00/ff\x07",
		"\x1b]6;1;bg;red;brightness;0\x07",
		"\x1b]6;1;bg;green;brightness;0\x07",
		"\x1b]6;1;bg;blue;brightness;255\x07",
	)
}

func setITermGrayTab() {
	fmt.Fprint(os.Stdout,
		"\x1b]1337;SetTabColor=rgb:66/66/66\x07",
		"\x1b]6;1;bg;red;brightness;102\x07",
		"\x1b]6;1;bg;green;brightness;102\x07",
		"\x1b]6;1;bg;blue;brightness;102\x07",
	)
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

type repoInfo struct {
	Name   string
	Branch string
	Path   string
}

type worktreeInfo struct {
	Path   string
	Branch string
}

type worktreeOption struct {
	Info      worktreeInfo
	Available bool
	Exists    bool
}

type selectionKind int

const (
	selectionWorktree selectionKind = iota
	selectionNoWorktree
)

type selectionResult struct {
	Kind selectionKind
	Lock *worktreeLock
	Task string
}

type menuItemKind int

const (
	menuWorktree menuItemKind = iota
	menuAddWorktree
	menuNoWorktree
	menuDivider
)

type menuItem struct {
	Kind       menuItemKind
	Label      string
	Worktree   worktreeInfo
	LastUsed   string
	LastUsedAt time.Time
	Available  bool
	IsWorktree bool
	Status     string
	Path       string
	LockID     string
	Exists     bool
	PRLabel    string
	PRCI       string
	PRApproved string
	PRTitle    string
	PRURL      string
}

type actionItem struct {
	Kind     string
	Label    string
	Disabled bool
}

type menuItemView struct {
	Label       string
	Branch      string
	LastUsed    string
	Path        string
	Available   bool
	IsWorktree  bool
	Status      string
	BranchPad   int
	LastUsedPad int
	Kind        menuItemKind
	LockID      string
	PRLabel     string
	PRCI        string
	PRApproved  string
	PRDetails   string
	PRPad       int
}

type worktreeLock struct {
	Path         string
	WorktreePath string
	RepoRoot     string
	Branch       string
	OwnerID      string
	PID          int
	Title        string
	ITermBlue    bool
	stopCh       chan struct{}
	stopOnce     sync.Once
	lostOnce     sync.Once
}

type prFetchStatus int

const (
	prFetchPending prFetchStatus = iota
	prFetchReady
	prFetchMissingGh
	prFetchUnauthed
	prFetchError
)

type prFetchResult struct {
	Status prFetchStatus
	PRs    map[string]prInfo
	Err    error
}

type prInfo struct {
	Number         int
	Title          string
	URL            string
	ReviewDecision string
	CIStatus       ciStatus
}

type ciStatus int

const (
	ciStatusNA ciStatus = iota
	ciStatusSuccess
	ciStatusFailure
)

type ghPR struct {
	Number            int       `json:"number"`
	Title             string    `json:"title"`
	URL               string    `json:"url"`
	HeadRefName       string    `json:"headRefName"`
	ReviewDecision    string    `json:"reviewDecision"`
	StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
}

type ghCheck struct {
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
}

func detectRepoInfo() (repoInfo, bool) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return repoInfo{}, false
	}

	cwd, err := os.Getwd()
	if err != nil {
		return repoInfo{}, false
	}

	topLevel, err := gitOutputInDir(cwd, gitPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return repoInfo{}, false
	}

	branch, err := gitOutputInDir(cwd, gitPath, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || branch == "" {
		branch, err = gitOutputInDir(cwd, gitPath, "rev-parse", "--short", "HEAD")
		if err != nil || branch == "" {
			branch = "detached"
		}
	}

	name := filepath.Base(topLevel)
	if name == "" || name == string(filepath.Separator) {
		name = "repo"
	}

	return repoInfo{Name: name, Branch: branch, Path: topLevel}, true
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

func startPRFetch(repoRoot string) <-chan prFetchResult {
	ch := make(chan prFetchResult, 1)
	go func() {
		debugLogf("wtx: starting gh PR fetch in %s", repoRoot)
		ghPath, err := exec.LookPath("gh")
		if err != nil {
			debugLogf("wtx: gh not found in PATH")
			ch <- prFetchResult{Status: prFetchMissingGh}
			return
		}
		prs, output, err := fetchPRs(repoRoot, ghPath)
		if err != nil {
			if isGhAuthError(output) {
				debugLogf("wtx: gh not authenticated: %s", strings.TrimSpace(output))
				ch <- prFetchResult{Status: prFetchUnauthed}
				return
			}
			if output != "" {
				err = fmt.Errorf("%w: %s", err, strings.TrimSpace(output))
			}
			debugLogf("wtx: gh PR fetch error: %v", err)
			ch <- prFetchResult{Status: prFetchError, Err: err}
			return
		}
		debugLogf("wtx: gh PR fetch success: %d PRs", len(prs))
		ch <- prFetchResult{Status: prFetchReady, PRs: prs}
	}()
	return ch
}

func fetchPRs(repoRoot string, ghPath string) (map[string]prInfo, string, error) {
	cmd := exec.Command(ghPath, "pr", "list", "--state", "open", "--json", "number,title,url,headRefName,reviewDecision,statusCheckRollup", "--limit", "200")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, string(output), err
	}
	var prs []ghPR
	if err := json.Unmarshal(output, &prs); err != nil {
		return nil, "", err
	}
	result := make(map[string]prInfo, len(prs))
	for _, pr := range prs {
		info := prInfo{
			Number:         pr.Number,
			Title:          pr.Title,
			URL:            pr.URL,
			ReviewDecision: pr.ReviewDecision,
			CIStatus:       computeCIStatus(pr.StatusCheckRollup),
		}
		result[normalizeBranchName(pr.HeadRefName)] = info
		result[pr.HeadRefName] = info
	}
	return result, "", nil
}

func computeCIStatus(checks []ghCheck) ciStatus {
	if len(checks) == 0 {
		return ciStatusNA
	}
	pending := false
	for _, check := range checks {
		conclusion := strings.ToUpper(strings.TrimSpace(check.Conclusion))
		status := strings.ToUpper(strings.TrimSpace(check.Status))
		if conclusion == "" {
			if status != "" && status != "COMPLETED" {
				pending = true
				continue
			}
			pending = true
			continue
		}
		switch conclusion {
		case "SUCCESS", "SKIPPED", "NEUTRAL":
			continue
		default:
			return ciStatusFailure
		}
	}
	if pending {
		return ciStatusNA
	}
	return ciStatusSuccess
}

func ciEmoji(status ciStatus) string {
	switch status {
	case ciStatusSuccess:
		return "✅"
	case ciStatusFailure:
		return "❌"
	default:
		return "N/A"
	}
}

func approvedEmoji(reviewDecision string) string {
	if strings.EqualFold(strings.TrimSpace(reviewDecision), "approved") {
		return "✅"
	}
	return "⬜"
}

func formatLink(label string, url string) string {
	if url == "" {
		return label
	}
	return "\x1b]8;;" + url + "\x07" + label + "\x1b]8;;\x07"
}

func formatPRInfo(branch string, prState prFetchResult) (string, string, string, string, string) {
	switch prState.Status {
	case prFetchPending:
		return "PR: ...", "CI: N/A", "Approved: ⬜", "Title: ...", ""
	case prFetchMissingGh:
		return "PR: N/A", "CI: N/A", "Approved: ⬜", "", ""
	case prFetchUnauthed:
		return "PR: N/A", "CI: N/A", "Approved: ⬜", "", ""
	case prFetchError:
		return "PR: N/A", "CI: N/A", "Approved: ⬜", "", ""
	case prFetchReady:
		normBranch := normalizeBranchName(branch)
		if pr, ok := prState.PRs[branch]; ok {
			title := truncateText(pr.Title, 40)
			return fmt.Sprintf("PR: #%d", pr.Number),
				"CI: "+ciEmoji(pr.CIStatus),
				"Approved: "+approvedEmoji(pr.ReviewDecision),
				"Title: "+title,
				pr.URL
		}
		if pr, ok := prState.PRs[normBranch]; ok {
			title := truncateText(pr.Title, 40)
			return fmt.Sprintf("PR: #%d", pr.Number),
				"CI: "+ciEmoji(pr.CIStatus),
				"Approved: "+approvedEmoji(pr.ReviewDecision),
				"Title: "+title,
				pr.URL
		}
		if debugEnabled() {
			debugLogf("wtx: no PR match for branch: %q normalized: %q", branch, normBranch)
		}
		return "PR: no", "CI: N/A", "Approved: ⬜", "", ""
	default:
		return "PR: N/A", "CI: N/A", "Approved: ⬜", "", ""
	}
}

func normalizeBranchName(name string) string {
	trimmed := strings.TrimSpace(name)
	trimmed = strings.TrimPrefix(trimmed, "refs/heads/")
	trimmed = strings.TrimPrefix(trimmed, "refs/remotes/")
	trimmed = strings.TrimPrefix(trimmed, "origin/")
	return trimmed
}

func isGhAuthError(output string) bool {
	msg := strings.ToLower(output)
	return strings.Contains(msg, "not logged into any github hosts") ||
		strings.Contains(msg, "to authenticate") ||
		strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "authorize") ||
		strings.Contains(msg, "oauth")
}

func truncateText(input string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(input) <= max {
		return input
	}
	if max <= 3 {
		return input[:max]
	}
	return input[:max-3] + "..."
}

func joinNonEmpty(sep string, parts []string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, sep)
}

func selectWorktree(info repoInfo) (*selectionResult, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}

	printBanner()
	setTitle(fmt.Sprintf("[%s]", info.Name))
	baseRef := defaultBaseRef(info.Path, gitPath)
	prFetchCh := startPRFetch(info.Path)
	prState := prFetchResult{Status: prFetchPending}
	prNoticePrinted := false
	prReadyPrinted := false

	for {
		worktrees, err := listWorktrees(info.Path, gitPath)
		if err != nil {
			return nil, err
		}

		options := make([]worktreeOption, 0, len(worktrees))
		for _, wt := range worktrees {
			exists, err := worktreePathExists(wt.Path)
			if err != nil {
				return nil, err
			}
			available := false
			if exists {
				available, err = isWorktreeAvailable(info.Path, wt.Path)
				if err != nil {
					return nil, err
				}
			}
			options = append(options, worktreeOption{Info: wt, Available: available, Exists: exists})
		}

		if prFetchCh != nil {
			select {
			case prState = <-prFetchCh:
				prFetchCh = nil
			default:
			}
		}
		if prState.Status == prFetchPending && prFetchCh != nil {
			select {
			case prState = <-prFetchCh:
				prFetchCh = nil
			case <-time.After(150 * time.Millisecond):
			}
		}
		if prState.Status == prFetchMissingGh && !prNoticePrinted {
			fmt.Fprintln(os.Stderr, "wtx: gh not installed; PR/CI/Approved info unavailable")
			prNoticePrinted = true
		}
		if prState.Status == prFetchUnauthed && !prNoticePrinted {
			fmt.Fprintln(os.Stderr, "wtx: gh not authenticated; PR/CI/Approved info unavailable")
			prNoticePrinted = true
		}
		if prState.Status == prFetchReady && debugEnabled() && !prReadyPrinted {
			debugLogf("wtx: gh PR fetch ok; open PRs: %d", len(prState.PRs))
			prReadyPrinted = true
		}
		if prState.Status == prFetchError && debugEnabled() && prState.Err != nil {
			debugLogf("wtx: gh PR fetch failed: %v", prState.Err)
		}

		menuItems, err := buildMenuItems(info.Path, options, prState)
		if err != nil {
			return nil, err
		}
		chosen, err := promptSelectMenu(info.Name, menuItems)
		if err != nil {
			return nil, err
		}
		if prFetchCh != nil && prState.Status == prFetchPending {
			select {
			case prState = <-prFetchCh:
				prFetchCh = nil
				continue
			default:
			}
		}
		switch chosen.Kind {
		case menuAddWorktree:
			newPath, newBranch, err := createWorktree(info.Path, gitPath)
			if err != nil {
				return nil, err
			}
			lock, err := lockWorktree(info.Path, worktreeInfo{Path: newPath, Branch: newBranch})
			if err != nil {
				return nil, err
			}
			return &selectionResult{Kind: selectionWorktree, Lock: lock}, nil
		case menuNoWorktree:
			task, err := promptTaskTitle()
			if err != nil {
				return nil, err
			}
			return &selectionResult{Kind: selectionNoWorktree, Task: task}, nil
		case menuDivider:
			continue
		case menuWorktree:
			canDelete, deleteReason := deleteEligibility(info.Path, worktrees, chosen.Worktree)
			lockInfo, lockFound, lockErr := worktreeLockInfo(info.Path, chosen.Worktree.Path)
			if lockErr != nil {
				return nil, lockErr
			}
			action, err := promptWorktreeAction(chosen.Worktree, baseRef, canDelete, deleteReason, chosen.Available, chosen.Exists, lockInfo, lockFound)
			if err != nil {
				return nil, err
			}
			if action == "back" {
				continue
			}
			if action == "open_shell" {
				title := fmt.Sprintf("[%s][%s]", info.Name, chosen.Worktree.Branch)
				setTitle(title)
				if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
					setITermGrayTab()
				}
				if err := runShellInDir(chosen.Worktree.Path); err != nil {
					return nil, err
				}
				return &selectionResult{Kind: selectionNoWorktree, Task: ""}, nil
			}
			if action == "force_unlock" {
				lockPath, err := worktreeLockPath(info.Path, chosen.Worktree.Path)
				if err != nil {
					return nil, err
				}
				pidLabel := "unknown"
				if lockFound && lockInfo.PID > 0 {
					pidLabel = fmt.Sprintf("%d", lockInfo.PID)
				}
				ok, err := promptYesNo(fmt.Sprintf("Force unlock? wtx lives in pid %s [y/N]: ", pidLabel))
				if err != nil {
					return nil, err
				}
				if ok {
					if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
						return nil, err
					}
				}
				continue
			}
			if action == "delete" {
				ok, err := promptYesNo("Delete worktree? [y/N]: ")
				if err != nil {
					return nil, err
				}
				if ok {
					if err := deleteWorktree(info.Path, gitPath, chosen.Worktree.Path, false); err != nil {
						return nil, err
					}
				}
				continue
			}
			if action == "remove_missing" {
				ok, err := promptYesNo("Remove worktree (manually removed)? [y/N]: ")
				if err != nil {
					return nil, err
				}
				if ok {
					if err := deleteWorktree(info.Path, gitPath, chosen.Worktree.Path, true); err != nil {
						return nil, err
					}
				}
				continue
			}
			if action == "use_new_branch" {
				if err := gitFetch(info.Path, gitPath); err != nil {
					return nil, err
				}
				refAfterFetch := defaultBaseRef(info.Path, gitPath)
				branch, err := promptBranchName(refAfterFetch)
				if err != nil {
					return nil, err
				}
				lock, err := lockWorktree(info.Path, chosen.Worktree)
				if err != nil {
					continue
				}
				if err := checkoutNewBranch(chosen.Worktree.Path, gitPath, branch, refAfterFetch); err != nil {
					lock.Release()
					continue
				}
				lock.Branch = branch
				return &selectionResult{Kind: selectionWorktree, Lock: lock}, nil
			}
			if action == "use_existing_branch" {
				branches, err := listLocalBranches(info.Path, gitPath)
				if err != nil {
					return nil, err
				}
				branch, err := promptExistingBranch(branches)
				if err != nil {
					return nil, err
				}
				lock, err := lockWorktree(info.Path, chosen.Worktree)
				if err != nil {
					continue
				}
				if err := checkoutExistingBranch(chosen.Worktree.Path, gitPath, branch); err != nil {
					lock.Release()
					continue
				}
				lock.Branch = branch
				return &selectionResult{Kind: selectionWorktree, Lock: lock}, nil
			}
		}

		lock, err := lockWorktree(info.Path, chosen.Worktree)
		if err != nil {
			continue
		}
		return &selectionResult{Kind: selectionWorktree, Lock: lock}, nil
	}
}

func listWorktrees(repoRoot string, gitPath string) ([]worktreeInfo, error) {
	cmd := exec.Command(gitPath, "worktree", "list", "--porcelain")
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var worktrees []worktreeInfo
	var current *worktreeInfo

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "worktree":
			worktrees = append(worktrees, worktreeInfo{Path: strings.Join(fields[1:], " ")})
			current = &worktrees[len(worktrees)-1]
		case "branch":
			if current != nil {
				current.Branch = shortBranch(strings.Join(fields[1:], " "))
			}
		case "detached":
			if current != nil && current.Branch == "" {
				current.Branch = "detached"
			}
		}
	}

	for i := range worktrees {
		if worktrees[i].Branch == "" {
			worktrees[i].Branch = "detached"
		}
	}

	return worktrees, nil
}

func shortBranch(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return ref
}

func shortRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "refs/remotes/")
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return ref
}

func promptYesNo(prompt string) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stdout, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

func worktreeLastUsed(repoRoot string, worktreePath string) (time.Time, string, error) {
	lockPath, err := worktreeLockPath(repoRoot, worktreePath)
	if err != nil {
		return time.Time{}, "", err
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return time.Time{}, "", err
		}
		lastUsedPath, pathErr := worktreeLastUsedPath(repoRoot, worktreePath)
		if pathErr != nil {
			return time.Time{}, "", pathErr
		}
		info, err = os.Stat(lastUsedPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return time.Time{}, "never", nil
			}
			return time.Time{}, "", err
		}
	}
	lastUsedAt := info.ModTime()
	return lastUsedAt, humanizeDuration(time.Since(lastUsedAt)), nil
}

func humanizeDuration(d time.Duration) string {
	if d < 10*time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days < 30 {
		return fmt.Sprintf("%dd ago", days)
	}
	months := days / 30
	return fmt.Sprintf("%dmo ago", months)
}

func buildMenuItems(repoRoot string, options []worktreeOption, prState prFetchResult) ([]menuItem, error) {
	items := make([]menuItem, 0, len(options)+2)
	if debugEnabled() {
		debugLogf("wtx: buildMenuItems prState=%v prCount=%d", prState.Status, len(prState.PRs))
	}
	for _, option := range options {
		lastUsedAt, lastUsedLabel, err := worktreeLastUsed(repoRoot, option.Info.Path)
		if err != nil {
			return nil, err
		}
		lockID, err := worktreeID(repoRoot, option.Info.Path)
		if err != nil {
			lockID = ""
		}
		label := option.Info.Branch
		status := "free"
		if !option.Exists {
			status = "manually removed"
		} else if !option.Available {
			status = "in use"
		}
		lastUsedText := "last used: " + lastUsedLabel
		prLabel, prCI, prApproved, prTitle, prURL := formatPRInfo(option.Info.Branch, prState)
		if debugEnabled() {
			debugLogf("wtx: branch=%q prLabel=%q prTitle=%q", option.Info.Branch, prLabel, prTitle)
		}
		items = append(items, menuItem{
			Kind:       menuWorktree,
			Label:      label,
			Worktree:   option.Info,
			LastUsed:   lastUsedText,
			LastUsedAt: lastUsedAt,
			Available:  option.Available && option.Exists,
			IsWorktree: true,
			Status:     status,
			Path:       option.Info.Path,
			LockID:     lockID,
			Exists:     option.Exists,
			PRLabel:    prLabel,
			PRCI:       prCI,
			PRApproved: prApproved,
			PRTitle:    prTitle,
			PRURL:      prURL,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Available != items[j].Available {
			return items[i].Available
		}
		return items[i].LastUsedAt.After(items[j].LastUsedAt)
	})
	items = append(items,
		menuItem{Kind: menuDivider, Label: "──────────", IsWorktree: false},
		menuItem{Kind: menuAddWorktree, Label: "Add worktree...", IsWorktree: false},
		menuItem{Kind: menuNoWorktree, Label: "No worktree (set task title)", IsWorktree: false},
	)
	return items, nil
}

func promptSelectMenu(repoName string, items []menuItem) (menuItem, error) {
	funcMap := template.FuncMap{}
	for key, value := range promptui.FuncMap {
		funcMap[key] = value
	}
	funcMap["dim"] = promptui.Styler(promptui.FGFaint)
	funcMap["dimBold"] = promptui.Styler(promptui.FGFaint, promptui.FGBold)

	branchPad := maxBranchLen(items)
	lastUsedPad := maxLastUsedLen(items)
	list := make([]menuItemView, 0, len(items))
	for _, item := range items {
		prLabel := item.PRLabel
		if item.PRURL != "" {
			prLabel = formatLink(item.PRLabel, item.PRURL)
		}
		prDetails := joinNonEmpty("  ", []string{prLabel, item.PRCI, item.PRApproved, item.PRTitle})
		list = append(list, menuItemView{
			Label:       item.Label,
			Branch:      item.Worktree.Branch,
			LastUsed:    item.LastUsed,
			Path:        item.Path,
			Available:   item.Available,
			IsWorktree:  item.IsWorktree,
			Status:      item.Status,
			BranchPad:   branchPad,
			LastUsedPad: lastUsedPad,
			Kind:        item.Kind,
			LockID:      item.LockID,
			PRLabel:     item.PRLabel,
			PRCI:        item.PRCI,
			PRApproved:  item.PRApproved,
			PRDetails:   prDetails,
		})
	}

	details := "{{ if .IsWorktree }}{{ if .Path }}{{ \"Path: \" | faint }}{{ .Path | faint }}{{ end }}{{ end }}"
	if debugEnabled() {
		details = "{{ if .IsWorktree }}{{ if .Path }}{{ \"Path: \" | faint }}{{ .Path | faint }}{{ end }}{{ if .LockID }}\n{{ \"Lock ID: \" | faint }}{{ .LockID | faint }}{{ end }}{{ end }}"
	}
	templates := &promptui.SelectTemplates{
		Active:   "{{ if .IsWorktree }}{{ if .Available }}{{ cyan \"▸\" }} {{ cyan (bold (printf \"%-*s\" .BranchPad .Label)) }}  {{ printf \"%-7s\" .Status }} {{ .PRDetails }} {{ printf \"%-*s\" .LastUsedPad .LastUsed }}{{ else }}{{ dim \"▸\" }} {{ dimBold (printf \"%-*s\" .BranchPad .Label) }}  {{ dim (printf \"%-7s\" .Status) }} {{ dim .PRDetails }} {{ dim (printf \"%-*s\" .LastUsedPad .LastUsed) }}{{ end }}{{ else }}{{ if eq .Kind 3 }}{{ dim \"▸\" }} {{ dim .Label }}{{ else }}{{ cyan \"▸\" }} {{ cyan .Label }}{{ end }}{{ end }}",
		Inactive: "{{ if .IsWorktree }}{{ if .Available }}  {{ printf \"%-*s\" .BranchPad .Label }}  {{ printf \"%-7s\" .Status }} {{ .PRDetails }} {{ printf \"%-*s\" .LastUsedPad .LastUsed }}{{ else }}  {{ dimBold (printf \"%-*s\" .BranchPad .Label) }}  {{ dim (printf \"%-7s\" .Status) }} {{ dim .PRDetails }} {{ dim (printf \"%-*s\" .LastUsedPad .LastUsed) }}{{ end }}{{ else }}{{ if eq .Kind 3 }}  {{ dim .Label }}{{ else }}  {{ .Label }}{{ end }}{{ end }}",
		Selected: "{{ if .IsWorktree }}{{ if .Available }}{{ cyan \"✔\" }} {{ cyan (bold (printf \"%-*s\" .BranchPad .Label)) }}  {{ printf \"%-7s\" .Status }} {{ .PRDetails }} {{ printf \"%-*s\" .LastUsedPad .LastUsed }}{{ else }}{{ dim \"✔\" }} {{ dimBold (printf \"%-*s\" .BranchPad .Label) }}  {{ dim (printf \"%-7s\" .Status) }} {{ dim .PRDetails }} {{ dim (printf \"%-*s\" .LastUsedPad .LastUsed) }}{{ end }}{{ else }}{{ if eq .Kind 3 }}{{ dim \"✔\" }} {{ dim .Label }}{{ else }}{{ cyan \"✔\" }} {{ cyan .Label }}{{ end }}{{ end }}",
		Details:  details,
		FuncMap:  funcMap,
	}

	cursorPos := firstAvailableIndex(list)
	for {
		selectPrompt := promptui.Select{
			Label:        "Select worktree",
			Items:        list,
			Templates:    templates,
			Size:         min(len(list), 10),
			CursorPos:    cursorPos,
			HideSelected: true,
			Stdout:       promptStdout(),
		}
		index, _, err := selectPrompt.Run()
		if err != nil {
			return menuItem{}, err
		}
		return items[index], nil
	}
}

func promptWorktreeAction(wt worktreeInfo, baseRef string, allowDelete bool, deleteReason string, available bool, exists bool, lockInfo lockPayloadData, lockFound bool) (string, error) {
	displayBase := shortRef(baseRef)
	deleteLabel := "Delete worktree"
	if !allowDelete && deleteReason != "" {
		deleteLabel = fmt.Sprintf("%s (%s)", deleteLabel, deleteReason)
	}
	forceLabel := "Force unlock"
	if !available {
		if lockFound && lockInfo.PID > 0 {
			forceLabel = fmt.Sprintf("Force unlock (PID %d)", lockInfo.PID)
		} else {
			forceLabel = "Force unlock (PID unknown)"
		}
	}
	items := []actionItem{
		{Kind: "use", Label: fmt.Sprintf("Use (%s)", wt.Branch), Disabled: !available},
		{Kind: "use_new_branch", Label: fmt.Sprintf("Checkout new branch from (%s)", displayBase), Disabled: !available},
		{Kind: "use_existing_branch", Label: "Choose an existing branch", Disabled: !available},
		{Kind: "open_shell", Label: "Open shell here (no lock)", Disabled: !exists},
		{Kind: "divider", Label: "──────────", Disabled: true},
		{Kind: "delete", Label: deleteLabel, Disabled: !allowDelete || !available || !exists},
		{Kind: "force_unlock", Label: forceLabel, Disabled: available},
		{Kind: "back", Label: "Back"},
	}
	if !exists {
		items = append(items[:5], append([]actionItem{{Kind: "remove_missing", Label: "Remove worktree (manually removed)", Disabled: false}}, items[5:]...)...)
	}

	funcMap := template.FuncMap{}
	for key, value := range promptui.FuncMap {
		funcMap[key] = value
	}

	templates := &promptui.SelectTemplates{
		Active:   "{{ if .Disabled }}{{ cyan \"▸\" }} {{ faint .Label }}{{ else }}{{ cyan \"▸\" }} {{ cyan .Label }}{{ end }}",
		Inactive: "{{ if .Disabled }}  {{ faint .Label }}{{ else }}  {{ .Label }}{{ end }}",
		Selected: "{{ if .Disabled }}{{ faint .Label }}{{ else }}{{ cyan \"✔\" }} {{ cyan .Label }}{{ end }}",
		FuncMap:  funcMap,
	}

	for {
		selectPrompt := promptui.Select{
			Label:        fmt.Sprintf("Action for %s [%s]", wt.Path, wt.Branch),
			Items:        items,
			Templates:    templates,
			Size:         min(len(items), 6),
			HideSelected: true,
			Stdout:       promptStdout(),
		}
		index, _, err := selectPrompt.Run()
		if err != nil {
			return "", err
		}
		chosen := items[index]
		if chosen.Disabled {
			if chosen.Kind == "divider" {
				continue
			}
			if deleteReason != "" {
				fmt.Fprintln(os.Stdout, deleteReason)
			} else {
				fmt.Fprintln(os.Stdout, "That option is unavailable.")
			}
			continue
		}
		return chosen.Kind, nil
	}
}

func printBanner() {
	const banner = `██     ██ ████████ ██   ██ 
██     ██    ██     ██ ██  
██  █  ██    ██      ███  
██ ███ ██    ██     ██ ██ 
 ███ ███     ██    ██   ██ 
                           

`
	fmt.Fprint(os.Stdout, banner)
}

func promptTaskTitle() (string, error) {
	prompt := promptui.Prompt{
		Label:  "Task title",
		Stdout: promptStdout(),
	}
	value, err := prompt.Run()
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("task title required")
	}
	return value, nil
}

func deleteWorktree(repoRoot string, gitPath string, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func checkoutNewBranch(worktreePath string, gitPath string, branch string, baseRef string) error {
	cmd := exec.Command(gitPath, "checkout", "-b", branch, baseRef)
	cmd.Dir = worktreePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func checkoutExistingBranch(worktreePath string, gitPath string, branch string) error {
	cmd := exec.Command(gitPath, "checkout", branch)
	cmd.Dir = worktreePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func deleteEligibility(repoRoot string, all []worktreeInfo, target worktreeInfo) (bool, string) {
	if len(all) <= 1 {
		return false, "Cannot delete the last remaining worktree"
	}
	if isRepoRoot(repoRoot, target.Path) {
		return false, "Cannot delete original worktree"
	}
	if !isManagedWorktree(repoRoot, target.Path) {
		return false, "Cannot delete original worktree"
	}
	return true, ""
}

func isRepoRoot(repoRoot string, worktreePath string) bool {
	rootReal, err := realPath(repoRoot)
	if err != nil {
		return repoRoot == worktreePath
	}
	worktreeReal, err := realPath(worktreePath)
	if err != nil {
		return repoRoot == worktreePath
	}
	return rootReal == worktreeReal
}

func isManagedWorktree(repoRoot string, worktreePath string) bool {
	base := filepath.Base(repoRoot)
	parent := filepath.Dir(worktreePath)
	if filepath.Base(parent) != base+".wt" {
		return false
	}
	leaf := filepath.Base(worktreePath)
	if !strings.HasPrefix(leaf, "wt.") {
		return false
	}
	suffix := strings.TrimPrefix(leaf, "wt.")
	return suffix != "" && isNumeric(suffix)
}

func isNumeric(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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

func writeLastUsed(repoRoot string, worktreePath string) error {
	lastUsedPath, err := worktreeLastUsedPath(repoRoot, worktreePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lastUsedPath), 0o755); err != nil {
		return err
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	return os.WriteFile(lastUsedPath, []byte(timestamp+"\n"), 0o644)
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstAvailableIndex(items []menuItemView) int {
	for i, item := range items {
		if item.IsWorktree && item.Available {
			return i
		}
	}
	return 0
}

func nextAvailableIndex(items []menuItemView, start int) int {
	for i := start + 1; i < len(items); i++ {
		if items[i].IsWorktree && items[i].Available {
			return i
		}
	}
	for i := 0; i < start; i++ {
		if items[i].IsWorktree && items[i].Available {
			return i
		}
	}
	return 0
}

func maxBranchLen(items []menuItem) int {
	maxLen := 0
	for _, item := range items {
		if !item.IsWorktree {
			continue
		}
		if len(item.Label) > maxLen {
			maxLen = len(item.Label)
		}
	}
	if maxLen < 4 {
		maxLen = 4
	}
	return maxLen
}

func maxLastUsedLen(items []menuItem) int {
	maxLen := 0
	for _, item := range items {
		if !item.IsWorktree {
			continue
		}
		if len(item.LastUsed) > maxLen {
			maxLen = len(item.LastUsed)
		}
	}
	if maxLen < 12 {
		maxLen = 12
	}
	return maxLen
}

func createWorktree(repoRoot string, gitPath string) (string, string, error) {
	baseRef := defaultBaseRef(repoRoot, gitPath)
	branch, err := promptBranchName(baseRef)
	if err != nil {
		return "", "", err
	}
	target, err := nextWorktreePath(repoRoot)
	if err != nil {
		return "", "", err
	}
	cmd := exec.Command(gitPath, "worktree", "add", "-b", branch, target, baseRef)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", err
	}
	return target, branch, nil
}

func promptBranchName(baseRef string) (string, error) {
	label := fmt.Sprintf("New branch name (from %s)", baseRef)
	prompt := promptui.Prompt{
		Label:  label,
		Stdout: promptStdout(),
	}
	value, err := prompt.Run()
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("branch name required")
	}
	return value, nil
}

func promptExistingBranch(branches []string) (string, error) {
	if len(branches) == 0 {
		return "", errors.New("no local branches found")
	}
	items := make([]actionItem, 0, len(branches))
	for _, branch := range branches {
		items = append(items, actionItem{Kind: branch, Label: branch})
	}
	templates := &promptui.SelectTemplates{
		Active:   "{{ cyan \"▸\" }} {{ cyan .Label }}",
		Inactive: "  {{ .Label }}",
		Selected: "{{ cyan \"✔\" }} {{ cyan .Label }}",
	}
	selectPrompt := promptui.Select{
		Label:        "Select branch",
		Items:        items,
		Templates:    templates,
		Size:         min(len(items), 10),
		HideSelected: true,
		Stdout:       promptStdout(),
		Searcher: func(input string, index int) bool {
			branch := strings.ToLower(items[index].Label)
			query := strings.ToLower(strings.TrimSpace(input))
			if query == "" {
				return true
			}
			return strings.Contains(branch, query)
		},
	}
	index, _, err := selectPrompt.Run()
	if err != nil {
		return "", err
	}
	return items[index].Kind, nil
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
	return "HEAD"
}

func gitFetch(repoRoot string, gitPath string) error {
	cmd := exec.Command(gitPath, "fetch")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runShellInDir(dir string) error {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func listLocalBranches(repoRoot string, gitPath string) ([]string, error) {
	output, err := gitOutputInDir(repoRoot, gitPath, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(output, "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
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

func isWorktreeAvailable(repoRoot string, worktreePath string) (bool, error) {
	lockPath, err := worktreeLockPath(repoRoot, worktreePath)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(lockPath)
	if err == nil {
		payload, perr := readLockPayload(lockPath)
		if perr != nil {
			return false, nil
		}
		if payload.PID > 0 && pidAlive(payload.PID) {
			return false, nil
		}
		if time.Since(info.ModTime()) < 10*time.Second {
			return false, nil
		}
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

func lockWorktree(repoRoot string, wt worktreeInfo) (*worktreeLock, error) {
	lockPath, err := worktreeLockPath(repoRoot, wt.Path)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}

	ownerID := buildOwnerID()
	pid := os.Getpid()
	payload, err := lockPayload(repoRoot, wt.Path, ownerID, pid)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		if _, werr := file.Write(payload); werr != nil {
			file.Close()
			_ = os.Remove(lockPath)
			return nil, werr
		}
		_ = file.Close()
		_ = writeLastUsed(repoRoot, wt.Path)
		return newWorktreeLock(lockPath, wt, repoRoot, ownerID, pid), nil
	}

	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}

	info, statErr := os.Stat(lockPath)
	if statErr != nil {
		return nil, statErr
	}
	if time.Since(info.ModTime()) < 10*time.Second {
		return nil, errors.New("worktree locked")
	}

	tmpPath := lockPath + "." + randomToken() + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, lockPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	current, err := readLockPayload(lockPath)
	if err != nil {
		return nil, err
	}
	if current.OwnerID != ownerID || current.PID != pid {
		return nil, errors.New("worktree locked")
	}

	_ = writeLastUsed(repoRoot, wt.Path)
	return newWorktreeLock(lockPath, wt, repoRoot, ownerID, pid), nil
}

func lockPayload(repoRoot string, worktreePath string, ownerID string, pid int) ([]byte, error) {
	data := map[string]any{
		"pid":           pid,
		"owner_id":      ownerID,
		"worktree_path": worktreePath,
		"repo_root":     repoRoot,
		"timestamp":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	return json.Marshal(data)
}

func newWorktreeLock(path string, wt worktreeInfo, repoRoot string, ownerID string, pid int) *worktreeLock {
	return &worktreeLock{
		Path:         path,
		WorktreePath: wt.Path,
		RepoRoot:     repoRoot,
		Branch:       wt.Branch,
		OwnerID:      ownerID,
		PID:          pid,
		stopCh:       make(chan struct{}),
	}
}

func (l *worktreeLock) StartToucher() {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				payload, err := readLockPayload(l.Path)
				if err != nil {
					l.signalLostLock("lock file missing or unreadable")
					return
				}
				if payload.OwnerID != l.OwnerID || payload.PID != l.PID {
					l.signalLostLock("lock ownership lost")
					return
				}
				if l.Title != "" {
					setTitle(l.Title)
					if l.ITermBlue {
						setITermBlueTab()
					}
				}
				now := time.Now()
				_ = os.Chtimes(l.Path, now, now)
			case <-l.stopCh:
				return
			}
		}
	}()
}

func (l *worktreeLock) StopToucher() {
	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
}

func (l *worktreeLock) Release() {
	_ = writeLastUsed(l.RepoRoot, l.WorktreePath)
	_ = os.Remove(l.Path)
}

func (l *worktreeLock) signalLostLock(reason string) {
	l.lostOnce.Do(func() {
		_ = writeLastUsed(l.RepoRoot, l.WorktreePath)
		fmt.Fprintln(os.Stderr, "wtx error: worktree lock lost:", reason)
		os.Exit(1)
	})
}

func worktreeLockPath(repoRoot string, worktreePath string) (string, error) {
	worktreeID, err := worktreeID(repoRoot, worktreePath)
	if err != nil {
		return "", err
	}
	lockDir := filepath.Join(os.Getenv("HOME"), ".wtx", "locks")
	return filepath.Join(lockDir, worktreeID+".lock"), nil
}

func worktreeLockInfo(repoRoot string, worktreePath string) (lockPayloadData, bool, error) {
	lockPath, err := worktreeLockPath(repoRoot, worktreePath)
	if err != nil {
		return lockPayloadData{}, false, err
	}
	payload, err := readLockPayload(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return lockPayloadData{}, false, nil
		}
		return lockPayloadData{}, false, err
	}
	return payload, true, nil
}

func worktreeLastUsedPath(repoRoot string, worktreePath string) (string, error) {
	worktreeID, err := worktreeID(repoRoot, worktreePath)
	if err != nil {
		return "", err
	}
	lastUsedDir := filepath.Join(os.Getenv("HOME"), ".wtx", "last_used")
	return filepath.Join(lastUsedDir, worktreeID+".stamp"), nil
}

func worktreeID(repoRoot string, worktreePath string) (string, error) {
	repoIDRoot := repoRoot
	if gitPath, err := exec.LookPath("git"); err == nil {
		commonDir, err := gitOutputInDir(repoRoot, gitPath, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err == nil && commonDir != "" {
			repoIDRoot = commonDir
		}
	}
	repoRootReal, err := realPath(repoIDRoot)
	if err != nil {
		return "", err
	}
	worktreeReal, err := realPathOrAbs(worktreePath)
	if err != nil {
		return "", err
	}

	repoID := hashString(repoRootReal)
	worktreeID := hashString(repoID + ":" + worktreeReal)
	return worktreeID, nil
}

func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func realPathOrAbs(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return abs, nil
		}
		return "", err
	}
	return real, nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func buildOwnerID() string {
	name := os.Getenv("USER")
	if name == "" {
		if u, err := user.Current(); err == nil {
			name = u.Username
		}
	}
	host, _ := os.Hostname()
	if name == "" && host == "" {
		name = "unknown"
	}
	if host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s@%s:%d:%s", name, host, os.Getpid(), randomToken())
}

func waitForInterrupt() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
}

type lockPayloadData struct {
	OwnerID string `json:"owner_id"`
	PID     int    `json:"pid"`
}

func readLockPayload(path string) (lockPayloadData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lockPayloadData{}, err
	}
	var payload lockPayloadData
	if err := json.Unmarshal(data, &payload); err != nil {
		return lockPayloadData{}, err
	}
	return payload, nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func randomToken() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

const defaultAgentCommand = "claude"

type config struct {
	AgentCommand string `json:"agent_command"`
}

func runSetup() error {
	fmt.Fprintln(os.Stdout, "wtx setup")
	choice, err := promptSetupMenu()
	if err != nil {
		return err
	}
	if choice != "ai_agent" {
		return nil
	}
	current, err := loadConfig()
	if err != nil {
		return err
	}
	cmd, err := promptAgentCommand(current.AgentCommand)
	if err != nil {
		return err
	}
	current.AgentCommand = cmd
	return saveConfig(current)
}

func promptSetupMenu() (string, error) {
	items := []actionItem{
		{Kind: "ai_agent", Label: "AI agent"},
	}
	templates := &promptui.SelectTemplates{
		Active:   "{{ cyan \"▸\" }} {{ cyan .Label }}",
		Inactive: "  {{ .Label }}",
		Selected: "{{ cyan \"✔\" }} {{ cyan .Label }}",
	}
	selectPrompt := promptui.Select{
		Label:        "Setup",
		Items:        items,
		Templates:    templates,
		Size:         min(len(items), 6),
		HideSelected: true,
		Stdout:       promptStdout(),
	}
	index, _, err := selectPrompt.Run()
	if err != nil {
		return "", err
	}
	return items[index].Kind, nil
}

func promptAgentCommand(current string) (string, error) {
	current = strings.TrimSpace(current)
	if current == "" {
		current = defaultAgentCommand
	}
	prompt := promptui.Prompt{
		Label:   "AI agent command",
		Default: current,
		Stdout:  promptStdout(),
	}
	value, err := prompt.Run()
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return current, nil
	}
	return value, nil
}

func loadConfig() (config, error) {
	path, err := configPath()
	if err != nil {
		return config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config{AgentCommand: defaultAgentCommand}, nil
		}
		return config{}, err
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func saveConfig(cfg config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func configPath() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("HOME not set")
	}
	return filepath.Join(home, ".wtx", "config.json"), nil
}

type bellFilterWriter struct {
	w io.Writer
}

func (b *bellFilterWriter) Write(p []byte) (int, error) {
	if bytes.IndexByte(p, '\a') == -1 {
		if _, err := b.w.Write(p); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	filtered := bytes.ReplaceAll(p, []byte{'\a'}, nil)
	if _, err := b.w.Write(filtered); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (b *bellFilterWriter) Close() error {
	return nil
}

func promptStdout() io.WriteCloser {
	return &bellFilterWriter{w: os.Stdout}
}

func checkForUpdates() error {
	current := resolvedVersion()
	if current == "" {
		fmt.Fprintln(os.Stdout, "wtx version: dev (no update check for dev builds)")
		return nil
	}
	latestCommit, err := fetchLatestCommit()
	if err != nil {
		return err
	}
	if latestCommit == "" {
		return errors.New("unable to determine latest release")
	}
	if commitsEqual(current, latestCommit) {
		fmt.Fprintf(os.Stdout, "wtx is up to date (%s)\n", shortVersion(current))
		return nil
	}
	fmt.Fprintf(os.Stdout, "update available: %s -> %s\n", shortVersion(current), shortVersion(latestCommit))
	fmt.Fprintln(os.Stdout, "Run: go install github.com/mrbonezy/wtx@latest")
	return nil
}

type updateStatus struct {
	LatestCommit string
	Err          error
}

func startUpdateCheck() <-chan updateStatus {
	if resolvedVersion() == "" {
		return nil
	}
	ch := make(chan updateStatus, 1)
	go func() {
		commit, err := fetchLatestCommit()
		if err != nil {
			ch <- updateStatus{Err: err}
			return
		}
		if commit == "" {
			ch <- updateStatus{Err: errors.New("missing latest commit")}
			return
		}
		if !commitsEqual(commit, resolvedVersion()) {
			ch <- updateStatus{LatestCommit: commit}
		}
	}()
	return ch
}

func maybePrintUpdate(ch <-chan updateStatus) {
	if ch == nil {
		return
	}
	select {
	case status := <-ch:
		if status.Err != nil {
			if debugEnabled() {
				fmt.Fprintln(os.Stderr, "wtx update check failed:", status.Err)
			}
			return
		}
		if status.LatestCommit == "" || commitsEqual(status.LatestCommit, resolvedVersion()) {
			return
		}
		fmt.Fprintf(os.Stdout, "\nupdate available: %s -> %s\n", shortVersion(resolvedVersion()), shortVersion(status.LatestCommit))
		fmt.Fprintln(os.Stdout, "Run: wtx update")
	default:
	}
}

func fetchLatestCommit() (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/mrbonezy/wtx/commits/main", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wtx")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status from GitHub: %s", resp.Status)
	}
	var payload struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.SHA), nil
}

func versionString() string {
	current := resolvedVersion()
	if current == "" {
		return "wtx dev"
	}
	return fmt.Sprintf("wtx %s", shortVersion(current))
}

func shortVersion(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 7 {
		return value[:7]
	}
	return value
}

func commitsEqual(a string, b string) bool {
	aa := normalizeCommit(a)
	bb := normalizeCommit(b)
	if aa == "" || bb == "" {
		return a == b
	}
	if aa == bb {
		return true
	}
	if strings.HasPrefix(aa, bb) || strings.HasPrefix(bb, aa) {
		return true
	}
	return false
}

func normalizeCommit(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var out strings.Builder
	for _, ch := range value {
		isHex := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
		if !isHex {
			break
		}
		out.WriteRune(ch)
	}
	if out.Len() < 7 {
		return ""
	}
	return out.String()
}

func runUpdate() error {
	var cmd *exec.Cmd
	cmd = exec.Command("go", "install", "github.com/mrbonezy/wtx@latest")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func resolvedVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	rev, _ := buildVCSRevision()
	return rev
}

func buildVCSRevision() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	var rev string
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			rev = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if rev == "" {
		if parsed := parsePseudoVersion(info.Main.Version); parsed != "" {
			return parsed, true
		}
		return "", false
	}
	if modified {
		return rev + "-dirty", true
	}
	return rev, true
}

func parsePseudoVersion(value string) string {
	parts := strings.Split(value, "-")
	if len(parts) < 3 {
		return ""
	}
	last := parts[len(parts)-1]
	if len(last) < 7 {
		return ""
	}
	for _, ch := range last {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ""
		}
	}
	return last
}
