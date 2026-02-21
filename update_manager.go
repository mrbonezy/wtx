package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	updateRepoModule       = "github.com/mrbonezy/wtx"
	updateRepoGitURL       = "https://github.com/mrbonezy/wtx.git"
	defaultUpdateInterval  = 24 * time.Hour
	startupUpdateTimeout   = 3 * time.Second
	resolveUpdateTimeout   = 8 * time.Second
	installUpdateTimeout   = 2 * time.Minute
	updateStateFileName    = "update-state.json"
	wtxUpdateCommandFormat = "wtx %s -> %s available. Run: wtx update"
)

var releaseVersionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

type parsedVersion struct {
	Major int
	Minor int
	Patch int
}

type updateState struct {
	LastCheckedUnix int64  `json:"last_checked_unix"`
	LastSeenVersion string `json:"last_seen_version,omitempty"`
}

type updateCheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
}

func runUpdateCommand(checkOnly bool, quiet bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), resolveUpdateTimeout)
	defer cancel()

	latest, err := resolveLatestVersion(ctx)
	if err != nil {
		return err
	}
	cur := currentVersion()

	result := updateCheckResult{
		CurrentVersion:  cur,
		LatestVersion:   latest,
		UpdateAvailable: isUpdateAvailableForInstall(cur, latest),
	}

	if checkOnly {
		printUpdateCheckResult(result, quiet)
		return nil
	}

	if !result.UpdateAvailable {
		if quiet {
			fmt.Println("up_to_date")
			return nil
		}
		fmt.Printf("wtx is up to date (%s)\n", result.CurrentVersion)
		return nil
	}

	if !quiet {
		fmt.Printf("Updating wtx to %s...\n", result.LatestVersion)
	}

	installCtx, installCancel := context.WithTimeout(context.Background(), installUpdateTimeout)
	defer installCancel()
	if err := installVersion(installCtx, result.LatestVersion); err != nil {
		return err
	}

	if quiet {
		fmt.Println(result.LatestVersion)
		return nil
	}
	fmt.Printf("Updated wtx to %s\n", result.LatestVersion)
	return nil
}

func printUpdateCheckResult(result updateCheckResult, quiet bool) {
	if quiet {
		if result.UpdateAvailable {
			fmt.Println(result.LatestVersion)
			return
		}
		fmt.Println("up_to_date")
		return
	}

	if result.UpdateAvailable {
		fmt.Printf("Update available: wtx %s -> %s\n", result.CurrentVersion, result.LatestVersion)
		return
	}
	fmt.Printf("wtx is up to date (%s)\n", result.CurrentVersion)
}

func maybeStartInvocationUpdateCheck(args []string) {
	if !shouldRunInvocationUpdateCheck(args) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), startupUpdateTimeout)
		defer cancel()

		result, err := checkForUpdatesWithThrottle(ctx, currentVersion(), defaultUpdateInterval)
		if err != nil || !result.UpdateAvailable {
			return
		}
		fmt.Fprintf(os.Stderr, wtxUpdateCommandFormat+"\n", result.CurrentVersion, result.LatestVersion)
	}()
}

func shouldRunInvocationUpdateCheck(args []string) bool {
	if len(args) <= 1 {
		return false
	}
	name := strings.TrimSpace(args[1])
	if name == "" {
		return true
	}
	switch name {
	case "tmux-status", "tmux-title", "tmux-agent-start", "tmux-agent-exit", "completion", "__complete", "__completeNoDesc", "update":
		return false
	default:
		return true
	}
}

func checkForUpdatesWithThrottle(ctx context.Context, currentVersion string, interval time.Duration) (updateCheckResult, error) {
	currentVersion = strings.TrimSpace(currentVersion)
	state, _ := readUpdateState()
	now := time.Now()
	cachedLatest := strings.TrimSpace(state.LastSeenVersion)
	if !shouldCheckForUpdates(state.LastCheckedUnix, now, interval) {
		return updateCheckResult{
			CurrentVersion:  currentVersion,
			LatestVersion:   cachedLatest,
			UpdateAvailable: isUpdateAvailable(currentVersion, cachedLatest),
		}, nil
	}

	latest, err := resolveLatestVersion(ctx)
	state.LastCheckedUnix = now.Unix()
	if err == nil {
		state.LastSeenVersion = latest
		cachedLatest = latest
	}
	_ = writeUpdateState(state)
	if err != nil {
		if strings.TrimSpace(cachedLatest) != "" {
			return updateCheckResult{
				CurrentVersion:  currentVersion,
				LatestVersion:   cachedLatest,
				UpdateAvailable: isUpdateAvailable(currentVersion, cachedLatest),
			}, nil
		}
		return updateCheckResult{}, err
	}
	return updateCheckResult{
		CurrentVersion:  currentVersion,
		LatestVersion:   latest,
		UpdateAvailable: isUpdateAvailable(currentVersion, latest),
	}, nil
}

func shouldCheckForUpdates(lastCheckedUnix int64, now time.Time, interval time.Duration) bool {
	if lastCheckedUnix <= 0 {
		return true
	}
	lastChecked := time.Unix(lastCheckedUnix, 0)
	if now.Before(lastChecked) {
		return true
	}
	return now.Sub(lastChecked) >= interval
}

func resolveLatestVersion(ctx context.Context) (string, error) {
	output, err := runCommand(ctx, "git", []string{"ls-remote", "--tags", "--refs", updateRepoGitURL}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to resolve latest version: %w", err)
	}
	latest, ok := latestVersionFromLSRemoteOutput(output)
	if !ok {
		return "", errors.New("failed to resolve latest version: no semver tags found")
	}
	return latest, nil
}

func installVersion(ctx context.Context, targetVersion string) error {
	targetVersion = strings.TrimSpace(targetVersion)
	if !isReleaseVersion(targetVersion) {
		return fmt.Errorf("invalid target version %q", targetVersion)
	}

	baseEnv := []string{"GOPROXY=direct"}
	installArgs := []string{"install", updateRepoModule + "@" + targetVersion}
	output, err := runCommand(ctx, "go", installArgs, baseEnv)
	if err == nil {
		return nil
	}
	if !shouldRetryInstallForSumDB(output + "\n" + err.Error()) {
		return fmt.Errorf("failed to install %s: %w\n%s", targetVersion, err, trimmedCommandOutput(output))
	}

	fallbackEnv := append(baseEnv, "GONOSUMDB="+updateRepoModule)
	fallbackOut, fallbackErr := runCommand(ctx, "go", installArgs, fallbackEnv)
	if fallbackErr != nil {
		return fmt.Errorf("failed to install %s (retry with GONOSUMDB also failed): %w\n%s", targetVersion, fallbackErr, trimmedCommandOutput(fallbackOut))
	}
	return nil
}

func shouldRetryInstallForSumDB(output string) bool {
	lower := strings.ToLower(strings.TrimSpace(output))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "sumdb") {
		return true
	}
	if strings.Contains(lower, "checksum") {
		return true
	}
	if strings.Contains(lower, "verifying") && strings.Contains(lower, "go.sum") {
		return true
	}
	return false
}

func trimmedCommandOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	const maxLen = 1600
	if len(output) <= maxLen {
		return output
	}
	return output[:maxLen] + "\n..."
}

func latestVersionFromLSRemoteOutput(output string) (string, bool) {
	var bestRaw string
	var best parsedVersion
	found := false
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		ref := strings.TrimSpace(fields[1])
		if !strings.HasPrefix(ref, "refs/tags/") {
			continue
		}
		candidate := strings.TrimPrefix(ref, "refs/tags/")
		parsed, ok := parseReleaseVersion(candidate)
		if !ok {
			continue
		}
		if !found || compareReleaseVersions(parsed, best) > 0 {
			bestRaw = candidate
			best = parsed
			found = true
		}
	}
	return bestRaw, found
}

func isUpdateAvailable(currentVersion string, latestVersion string) bool {
	current, okCurrent := parseReleaseVersion(strings.TrimSpace(currentVersion))
	latest, okLatest := parseReleaseVersion(strings.TrimSpace(latestVersion))
	if !okCurrent || !okLatest {
		return false
	}
	return compareReleaseVersions(latest, current) > 0
}

func isUpdateAvailableForInstall(currentVersion string, latestVersion string) bool {
	currentVersion = strings.TrimSpace(currentVersion)
	latestVersion = strings.TrimSpace(latestVersion)
	if currentVersion == "" || latestVersion == "" {
		return false
	}
	if isUpdateAvailable(currentVersion, latestVersion) {
		return true
	}
	return !isReleaseVersion(currentVersion) && isReleaseVersion(latestVersion)
}

func isReleaseVersion(version string) bool {
	_, ok := parseReleaseVersion(version)
	return ok
}

func parseReleaseVersion(version string) (parsedVersion, bool) {
	match := releaseVersionPattern.FindStringSubmatch(strings.TrimSpace(version))
	if len(match) != 4 {
		return parsedVersion{}, false
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return parsedVersion{}, false
	}
	minor, err := strconv.Atoi(match[2])
	if err != nil {
		return parsedVersion{}, false
	}
	patch, err := strconv.Atoi(match[3])
	if err != nil {
		return parsedVersion{}, false
	}
	return parsedVersion{Major: major, Minor: minor, Patch: patch}, true
}

func compareReleaseVersions(a parsedVersion, b parsedVersion) int {
	if a.Major != b.Major {
		if a.Major > b.Major {
			return 1
		}
		return -1
	}
	if a.Minor != b.Minor {
		if a.Minor > b.Minor {
			return 1
		}
		return -1
	}
	if a.Patch != b.Patch {
		if a.Patch > b.Patch {
			return 1
		}
		return -1
	}
	return 0
}

func runCommand(ctx context.Context, name string, args []string, extraEnv []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return string(output), ctxErr
		}
		return string(output), err
	}
	return string(output), nil
}

func readUpdateState() (updateState, error) {
	path, err := updateStatePath()
	if err != nil {
		return updateState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updateState{}, err
	}
	var state updateState
	if err := json.Unmarshal(data, &state); err != nil {
		return updateState{}, err
	}
	return state, nil
}

func writeUpdateState(state updateState) error {
	path, err := updateStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func updateStatePath() (string, error) {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return "", errors.New("HOME not set")
	}
	return filepath.Join(home, ".wtx", updateStateFileName), nil
}
