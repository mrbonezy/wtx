package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const tmuxStatusGHTTL = 10 * time.Second
const tmuxStatusGHStaleMaxAge = 2 * time.Minute
const defaultGHSummary = "PR - | CI - | GH - | Review -"

type ghStatusCacheEntry struct {
	FetchedAtUnix int64  `json:"fetched_at_unix"`
	Summary       string `json:"summary"`
}

func runTmuxStatus(args []string) error {
	worktreePath := parseWorktreeArg(args)
	fmt.Print(buildTmuxStatusLine(worktreePath))
	return nil
}

func runTmuxTitle(args []string) error {
	worktreePath := parseWorktreeArg(args)
	fmt.Print(buildTmuxTitle(worktreePath))
	return nil
}

func parseWorktreeArg(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--worktree" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
	}
	return ""
}

func buildTmuxStatusLine(worktreePath string) string {
	label := "WTX"
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return label
	}
	branch := currentBranchInWorktree(worktreePath)
	if branch != "" {
		label += "  " + branch
	}
	label += "  " + worktreePath
	label += "  " + ghSummaryForBranchCached(worktreePath, branch)
	if agent := strings.TrimSpace(tmuxAgentSummary(worktreePath)); agent != "" {
		label += "  " + agent
	}
	return label
}

func buildTmuxTitle(worktreePath string) string {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return "wtx"
	}
	branch := currentBranchInWorktree(worktreePath)
	if branch == "" {
		return "wtx"
	}
	return "wtx - " + branch
}

func currentBranchInWorktree(worktreePath string) string {
	branch, err := gitOutputInDir(worktreePath, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch = strings.TrimSpace(branch)
	if branch == "" || strings.EqualFold(branch, "HEAD") || strings.EqualFold(branch, "detached") {
		return ""
	}
	return branch
}

func ghSummaryForBranchCached(worktreePath string, branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return defaultGHSummary
	}
	repoRoot, err := repoRootForDir(worktreePath, "")
	if err != nil {
		return defaultGHSummary
	}
	if summary, ok := readCachedGHSummary(repoRoot, branch); ok {
		return summary
	}
	summary, reliable := ghSummaryForRepoBranch(repoRoot, branch)
	if reliable {
		_ = writeCachedGHSummary(repoRoot, branch, summary)
		return summary
	}
	if summary, ok := readCachedGHSummaryAllowStale(repoRoot, branch); ok {
		return summary
	}
	return summary
}

func ghSummaryForRepoBranch(repoRoot string, branch string) (string, bool) {
	data, err := NewGHManager().PRDataByBranch(repoRoot, []string{branch})
	if err != nil {
		return defaultGHSummary, false
	}
	pr, ok := data[branch]
	if !ok {
		return defaultGHSummary, true
	}
	return "PR " + prLabelWithURL(pr) + " | CI " + ciLabel(pr) + " | GH " + ghAPIStatusLabel(pr) + " | Review " + reviewLabel(pr), true
}

func readCachedGHSummary(repoRoot string, branch string) (string, bool) {
	return readCachedGHSummaryWithTTL(repoRoot, branch, tmuxStatusGHTTL)
}

func readCachedGHSummaryAllowStale(repoRoot string, branch string) (string, bool) {
	return readCachedGHSummaryWithTTL(repoRoot, branch, tmuxStatusGHStaleMaxAge)
}

func readCachedGHSummaryWithTTL(repoRoot string, branch string, ttl time.Duration) (string, bool) {
	path, err := ghStatusCachePath(repoRoot, branch)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var entry ghStatusCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return "", false
	}
	if strings.TrimSpace(entry.Summary) == "" || entry.FetchedAtUnix <= 0 {
		return "", false
	}
	if ttl > 0 && time.Since(time.Unix(entry.FetchedAtUnix, 0)) > ttl {
		return "", false
	}
	return entry.Summary, true
}

func writeCachedGHSummary(repoRoot string, branch string, summary string) error {
	path, err := ghStatusCachePath(repoRoot, branch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := ghStatusCacheEntry{
		FetchedAtUnix: time.Now().Unix(),
		Summary:       summary,
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ghStatusCachePath(repoRoot string, branch string) (string, error) {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return "", fmt.Errorf("HOME not set")
	}
	key := hashString(strings.TrimSpace(repoRoot) + "|" + strings.TrimSpace(branch))
	return filepath.Join(home, ".wtx", "status-cache", key+".json"), nil
}

func prLabel(pr PRData) string {
	if pr.Number <= 0 {
		return "-"
	}
	return fmt.Sprintf("#%d", pr.Number)
}

func prLabelWithURL(pr PRData) string {
	return prLabel(pr)
}

func ciLabel(pr PRData) string {
	if pr.CITotal == 0 {
		return "-"
	}
	switch pr.CIState {
	case PRCISuccess:
		return fmt.Sprintf("ok %d/%d", pr.CICompleted, pr.CITotal)
	case PRCIFail:
		return fmt.Sprintf("fail %d/%d", pr.CICompleted, pr.CITotal)
	case PRCIInProgress:
		return fmt.Sprintf("run %d/%d", pr.CICompleted, pr.CITotal)
	default:
		return "-"
	}
}

func reviewLabel(pr PRData) string {
	required, requiredKnown := ensureRequiredAtLeastApproved(
		pr.ReviewApproved,
		pr.ReviewKnown,
		pr.ReviewRequired,
		pr.ReviewRequired > 0,
	)
	pr.ReviewRequired = required
	if pr.ReviewRequired > 0 {
		return fmt.Sprintf("%d/%d u:%d", pr.ReviewApproved, pr.ReviewRequired, pr.UnresolvedComments)
	}
	if pr.ReviewKnown || requiredKnown {
		return fmt.Sprintf("%d/%d u:%d", pr.ReviewApproved, pr.ReviewApproved, pr.UnresolvedComments)
	}
	prefix := "pending"
	if pr.Approved {
		prefix = "approved"
	}
	return fmt.Sprintf("%s u:%d", prefix, pr.UnresolvedComments)
}

func ghAPIStatusLabel(pr PRData) string {
	status := strings.TrimSpace(strings.ToLower(pr.Status))
	switch status {
	case "conflict":
		return "conflict"
	case "awaiting-ci":
		return "waiting for checks"
	case "awaiting-review":
		return "awaiting approval"
	case "can-merge":
		return "mergeable"
	case "awaiting-comments":
		return "awaiting comments"
	case "draft":
		return "draft"
	case "open":
		return "open"
	case "closed":
		return "closed"
	case "merged":
		return "merged"
	default:
		return "-"
	}
}
