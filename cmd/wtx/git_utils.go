package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

var errGitNotInstalled = errors.New("git not installed")
var errNotInGitRepository = errors.New("not in a git repository")

func gitPath() (string, error) {
	return exec.LookPath("git")
}

func requireGitPath() (string, error) {
	path, err := gitPath()
	if err != nil {
		return "", errGitNotInstalled
	}
	return path, nil
}

func repoRootForDir(dir string, gitBin string) (string, error) {
	_ = gitBin
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", errNotInGitRepository
		}
		dir = wd
	}
	current, err := filepath.Abs(dir)
	if err != nil {
		return "", errNotInGitRepository
	}
	for {
		dotGit := filepath.Join(current, ".git")
		if _, err := os.Stat(dotGit); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", errNotInGitRepository
}

func requireGitContext(dir string) (string, string, error) {
	repoRoot, err := repoRootForDir(dir, "git")
	if err != nil {
		return "", "", err
	}
	return "git", repoRoot, nil
}
