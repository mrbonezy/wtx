package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
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
	if len(args) > 2 {
		return usageError()
	}
	if len(args) == 2 {
		if args[1] != "setup" {
			return usageError()
		}
		return runSetup()
	}

	repoInfo, ok := detectRepoInfo()
	if !ok {
		if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
			setITermGrayTab()
		}
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
		return nil
	}

	config, err := loadConfig()
	if err != nil {
		return err
	}

	lock := selection.Lock
	lock.StartToucher()
	defer lock.StopToucher()
	defer lock.Release()

	fmt.Fprintln(os.Stdout, "using worktree at", lock.WorktreePath)

	title := fmt.Sprintf("[%s][%s]", repoInfo.Name, lock.Branch)
	setTitle(title)
	if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
		setITermBlueTab()
	}

	agentCmd := strings.TrimSpace(config.AgentCommand)
	if agentCmd == "" {
		agentCmd = defaultAgentCommand
	}

	cmd := exec.Command("/bin/sh", "-c", agentCmd)
	cmd.Dir = lock.WorktreePath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func usageError() error {
	fmt.Fprintln(os.Stderr, "usage: wtx [setup]")
	return errors.New("invalid arguments")
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
}

type actionItem struct {
	Kind     string
	Label    string
	Disabled bool
}

type menuItemView struct {
	Label      string
	Branch     string
	LastUsed   string
	Path       string
	Available  bool
	IsWorktree bool
	Status     string
	BranchPad  int
	LastUsedPad int
	Kind       menuItemKind
}

type worktreeLock struct {
	Path         string
	WorktreePath string
	RepoRoot     string
	Branch       string
	stopCh       chan struct{}
	stopOnce     sync.Once
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

func selectWorktree(info repoInfo) (*selectionResult, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}

	printBanner()
	setTitle(fmt.Sprintf("[%s]", info.Name))
	baseRef := defaultBaseRef(info.Path, gitPath)

	for {
		worktrees, err := listWorktrees(info.Path, gitPath)
		if err != nil {
			return nil, err
		}

		options := make([]worktreeOption, 0, len(worktrees))
		for _, wt := range worktrees {
			available, err := isWorktreeAvailable(info.Path, wt.Path)
			if err != nil {
				return nil, err
			}
			options = append(options, worktreeOption{Info: wt, Available: available})
		}

		menuItems, err := buildMenuItems(info.Path, options)
		if err != nil {
			return nil, err
		}
		chosen, err := promptSelectMenu(info.Name, menuItems)
		if err != nil {
			return nil, err
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
			if !chosen.Available {
				continue
			}
			canDelete, deleteReason := deleteEligibility(info.Path, worktrees, chosen.Worktree)
			action, err := promptWorktreeAction(chosen.Worktree, baseRef, canDelete, deleteReason)
			if err != nil {
				return nil, err
			}
			if action == "back" {
				continue
			}
			if action == "delete" {
				ok, err := promptYesNo("Delete worktree? [y/N]: ")
				if err != nil {
					return nil, err
				}
				if ok {
					if err := deleteWorktree(info.Path, gitPath, chosen.Worktree.Path); err != nil {
						return nil, err
					}
				}
				continue
			}
			if action == "use_new_branch" {
				branch, err := promptBranchName(baseRef)
				if err != nil {
					return nil, err
				}
				lock, err := lockWorktree(info.Path, chosen.Worktree)
				if err != nil {
					continue
				}
				if err := checkoutNewBranch(chosen.Worktree.Path, gitPath, branch, baseRef); err != nil {
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

func buildMenuItems(repoRoot string, options []worktreeOption) ([]menuItem, error) {
	items := make([]menuItem, 0, len(options)+2)
	for _, option := range options {
		lastUsedAt, lastUsedLabel, err := worktreeLastUsed(repoRoot, option.Info.Path)
		if err != nil {
			return nil, err
		}
		label := option.Info.Branch
		status := "free"
		if !option.Available {
			status = "in use"
		}
		lastUsedText := "last used: " + lastUsedLabel
		items = append(items, menuItem{
			Kind:       menuWorktree,
			Label:      label,
			Worktree:   option.Info,
			LastUsed:   lastUsedText,
			LastUsedAt: lastUsedAt,
			Available:  option.Available,
			IsWorktree: true,
			Status:     status,
			Path:       option.Info.Path,
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
		list = append(list, menuItemView{
			Label:      item.Label,
			Branch:     item.Worktree.Branch,
			LastUsed:   item.LastUsed,
			Path:       item.Path,
			Available:  item.Available,
			IsWorktree: item.IsWorktree,
			Status:     item.Status,
			BranchPad:  branchPad,
			LastUsedPad: lastUsedPad,
			Kind:       item.Kind,
		})
	}

	templates := &promptui.SelectTemplates{
		Active:   "{{ if .IsWorktree }}{{ if .Available }}{{ cyan \"▸\" }} {{ cyan (bold (printf \"%-*s\" .BranchPad .Label)) }}  {{ printf \"%-7s\" .Status }} {{ printf \"%-*s\" .LastUsedPad .LastUsed }}{{ else }}{{ dim \"▸\" }} {{ dimBold (printf \"%-*s\" .BranchPad .Label) }}  {{ dim (printf \"%-7s %-*s\" .Status .LastUsedPad .LastUsed) }}{{ end }}{{ else }}{{ if eq .Kind 3 }}{{ dim \"▸\" }} {{ dim .Label }}{{ else }}{{ cyan \"▸\" }} {{ cyan .Label }}{{ end }}{{ end }}",
		Inactive: "{{ if .IsWorktree }}{{ if .Available }}  {{ printf \"%-*s\" .BranchPad .Label }}  {{ printf \"%-7s\" .Status }} {{ printf \"%-*s\" .LastUsedPad .LastUsed }}{{ else }}  {{ dimBold (printf \"%-*s\" .BranchPad .Label) }}  {{ dim (printf \"%-7s %-*s\" .Status .LastUsedPad .LastUsed) }}{{ end }}{{ else }}{{ if eq .Kind 3 }}  {{ dim .Label }}{{ else }}  {{ .Label }}{{ end }}{{ end }}",
		Selected: "{{ if .IsWorktree }}{{ if .Available }}{{ cyan \"✔\" }} {{ cyan (bold (printf \"%-*s\" .BranchPad .Label)) }}  {{ printf \"%-7s\" .Status }} {{ printf \"%-*s\" .LastUsedPad .LastUsed }}{{ else }}{{ dim \"✔\" }} {{ dimBold (printf \"%-*s\" .BranchPad .Label) }}  {{ dim (printf \"%-7s %-*s\" .Status .LastUsedPad .LastUsed) }}{{ end }}{{ else }}{{ if eq .Kind 3 }}{{ dim \"✔\" }} {{ dim .Label }}{{ else }}{{ cyan \"✔\" }} {{ cyan .Label }}{{ end }}{{ end }}",
		Details:  "{{ if .IsWorktree }}{{ if .Path }}{{ \"Path: \" | faint }}{{ .Path | faint }}{{ end }}{{ end }}",
		FuncMap:  funcMap,
	}

	cursorPos := firstAvailableIndex(list)
	for {
		selectPrompt := promptui.Select{
			Label:     "Select worktree",
			Items:     list,
			Templates: templates,
			Size:      min(len(list), 10),
			CursorPos: cursorPos,
			HideSelected: true,
		}
		index, _, err := selectPrompt.Run()
		if err != nil {
			return menuItem{}, err
		}
		if list[index].IsWorktree && !list[index].Available {
			cursorPos = nextAvailableIndex(list, index)
			continue
		}
		return items[index], nil
	}
}

func promptWorktreeAction(wt worktreeInfo, baseRef string, allowDelete bool, deleteReason string) (string, error) {
	displayBase := shortRef(baseRef)
	deleteLabel := "Delete worktree"
	if !allowDelete && deleteReason != "" {
		deleteLabel = fmt.Sprintf("%s (%s)", deleteLabel, deleteReason)
	}
	items := []actionItem{
		{Kind: "use", Label: fmt.Sprintf("Use (%s)", wt.Branch)},
		{Kind: "use_new_branch", Label: fmt.Sprintf("Use and checkout new branch from %s", displayBase)},
		{Kind: "delete", Label: deleteLabel, Disabled: !allowDelete},
		{Kind: "back", Label: "Back"},
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
			Label:     fmt.Sprintf("Action for %s [%s]", wt.Path, wt.Branch),
			Items:     items,
			Templates: templates,
			Size:      min(len(items), 6),
			HideSelected: true,
		}
		index, _, err := selectPrompt.Run()
		if err != nil {
			return "", err
		}
		chosen := items[index]
		if chosen.Disabled {
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
	prompt := promptui.Prompt{Label: "Task title"}
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

func deleteWorktree(repoRoot string, gitPath string, path string) error {
	cmd := exec.Command(gitPath, "worktree", "remove", path)
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
	leaf := filepath.Base(worktreePath)
	prefix := base + ".wt."
	if !strings.HasPrefix(leaf, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(leaf, prefix)
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
	prompt := promptui.Prompt{Label: label}
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

func nextWorktreePath(repoRoot string) (string, error) {
	parent := filepath.Dir(repoRoot)
	base := filepath.Base(repoRoot)
	for i := 1; i < 10000; i++ {
		candidate := filepath.Join(parent, fmt.Sprintf("%s.wt.%d", base, i))
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

	payload, err := lockPayload(repoRoot, wt.Path)
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
		return newWorktreeLock(lockPath, wt, repoRoot), nil
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

	tmpPath := lockPath + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, lockPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	return newWorktreeLock(lockPath, wt, repoRoot), nil
}

func lockPayload(repoRoot string, worktreePath string) ([]byte, error) {
	ownerID := buildOwnerID()
	data := map[string]any{
		"pid":           os.Getpid(),
		"owner_id":      ownerID,
		"worktree_path": worktreePath,
		"repo_root":     repoRoot,
		"timestamp":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	return json.Marshal(data)
}

func newWorktreeLock(path string, wt worktreeInfo, repoRoot string) *worktreeLock {
	return &worktreeLock{
		Path:         path,
		WorktreePath: wt.Path,
		RepoRoot:     repoRoot,
		Branch:       wt.Branch,
		stopCh:       make(chan struct{}),
	}
}

func (l *worktreeLock) StartToucher() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
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

func worktreeLockPath(repoRoot string, worktreePath string) (string, error) {
	worktreeID, err := worktreeID(repoRoot, worktreePath)
	if err != nil {
		return "", err
	}
	lockDir := filepath.Join(os.Getenv("HOME"), ".claudex", "locks")
	return filepath.Join(lockDir, worktreeID+".lock"), nil
}

func worktreeLastUsedPath(repoRoot string, worktreePath string) (string, error) {
	worktreeID, err := worktreeID(repoRoot, worktreePath)
	if err != nil {
		return "", err
	}
	lastUsedDir := filepath.Join(os.Getenv("HOME"), ".claudex", "last_used")
	return filepath.Join(lastUsedDir, worktreeID+".stamp"), nil
}

func worktreeID(repoRoot string, worktreePath string) (string, error) {
	repoRootReal, err := realPath(repoRoot)
	if err != nil {
		return "", err
	}
	worktreeReal, err := realPath(worktreePath)
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
		return "unknown"
	}
	if host == "" {
		return name
	}
	if name == "" {
		return host
	}
	return name + "@" + host
}

func waitForInterrupt() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
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
		Label:       "Setup",
		Items:       items,
		Templates:   templates,
		Size:        min(len(items), 6),
		HideSelected: true,
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
