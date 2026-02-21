package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const recentBranchCacheLimit = 40

type recentBranchCache struct {
	Branches []string `json:"branches"`
}

func wtxHomeDir() (string, error) {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return "", errors.New("HOME not set")
	}
	return filepath.Join(home, ".wtx"), nil
}

func recentBranchCachePath(repoRoot string) (string, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "", errors.New("repo root required")
	}
	home, err := wtxHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "cache", "recent_branches", hashString(repoRoot)+".json"), nil
}

func readRecentBranches(repoRoot string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	path, err := recentBranchCachePath(repoRoot)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	var cache recentBranchCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	out := make([]string, 0, min(limit, len(cache.Branches)))
	seen := make(map[string]bool, len(cache.Branches))
	for _, raw := range cache.Branches {
		b := strings.TrimSpace(raw)
		if b == "" || seen[b] {
			continue
		}
		seen[b] = true
		out = append(out, b)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func writeRecentBranches(repoRoot string, branches []string) error {
	path, err := recentBranchCachePath(repoRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cache := recentBranchCache{Branches: branches}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func recordRecentBranch(repoRoot string, branch string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	branch = strings.TrimSpace(branch)
	if repoRoot == "" || branch == "" || branch == "detached" {
		return nil
	}
	recent, err := readRecentBranches(repoRoot, recentBranchCacheLimit)
	if err != nil {
		return err
	}
	merged := make([]string, 0, len(recent)+1)
	merged = append(merged, branch)
	for _, b := range recent {
		if b == branch {
			continue
		}
		merged = append(merged, b)
		if len(merged) >= recentBranchCacheLimit {
			break
		}
	}
	return writeRecentBranches(repoRoot, merged)
}

func recordRecentBranchForWorktree(worktreePath string, branch string) {
	branch = strings.TrimSpace(branch)
	if branch == "" || branch == "detached" {
		return
	}
	_, repoRoot, err := requireGitContext(worktreePath)
	if err != nil {
		return
	}
	if err := recordRecentBranch(repoRoot, branch); err != nil {
		fmt.Fprintln(os.Stderr, "wtx warning: failed to update recent branch cache:", err)
	}
}
