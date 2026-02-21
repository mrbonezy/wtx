package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestParseReleaseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantOK  bool
		wantMaj int
		wantMin int
		wantPat int
	}{
		{name: "valid", input: "v1.2.3", wantOK: true, wantMaj: 1, wantMin: 2, wantPat: 3},
		{name: "invalid no v", input: "1.2.3", wantOK: false},
		{name: "invalid prerelease", input: "v1.2.3-rc1", wantOK: false},
		{name: "invalid partial", input: "v1.2", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseReleaseVersion(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("expected ok=%v, got %v", tc.wantOK, ok)
			}
			if !ok {
				return
			}
			if got.Major != tc.wantMaj || got.Minor != tc.wantMin || got.Patch != tc.wantPat {
				t.Fatalf("unexpected parsed version: %#v", got)
			}
		})
	}
}

func TestLatestVersionFromLSRemoteOutput(t *testing.T) {
	output := "" +
		"abc refs/tags/v1.2.3\n" +
		"abc refs/tags/v2.0.0\n" +
		"abc refs/tags/v1.10.0\n" +
		"abc refs/tags/v2.0.0-rc1\n"

	got, ok := latestVersionFromLSRemoteOutput(output)
	if !ok {
		t.Fatalf("expected to find a version")
	}
	if got != "v2.0.0" {
		t.Fatalf("expected v2.0.0, got %q", got)
	}
}

func TestIsUpdateAvailable(t *testing.T) {
	if !isUpdateAvailable("v1.2.3", "v1.2.4") {
		t.Fatalf("expected update to be available")
	}
	if isUpdateAvailable("v1.2.4", "v1.2.4") {
		t.Fatalf("expected same version to be up-to-date")
	}
	if isUpdateAvailable("dev", "v1.2.4") {
		t.Fatalf("expected non-release current version to skip update prompt")
	}
}

func TestIsUpdateAvailableForInstall(t *testing.T) {
	if !isUpdateAvailableForInstall("v1.2.3", "v1.2.4") {
		t.Fatalf("expected release-to-release update to be available")
	}
	if !isUpdateAvailableForInstall("dev", "v1.2.4") {
		t.Fatalf("expected dev build to be install-updatable to release")
	}
	if !isUpdateAvailableForInstall("v0.0.0-20240202-abcdef", "v1.2.4") {
		t.Fatalf("expected pseudo-version build to be install-updatable to release")
	}
	if isUpdateAvailableForInstall("dev", "dev") {
		t.Fatalf("expected non-release latest to remain not updatable")
	}
}

func TestShouldRunInvocationUpdateCheck(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "interactive", args: []string{"wtx"}, want: false},
		{name: "normal command", args: []string{"wtx", "config"}, want: true},
		{name: "completion", args: []string{"wtx", "completion"}, want: false},
		{name: "internal helper", args: []string{"wtx", "tmux-status"}, want: false},
		{name: "update command", args: []string{"wtx", "update"}, want: false},
		{name: "version long flag", args: []string{"wtx", "--version"}, want: false},
		{name: "version short flag", args: []string{"wtx", "-v"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRunInvocationUpdateCheck(tc.args)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestShouldRetryInstallForSumDB(t *testing.T) {
	if !shouldRetryInstallForSumDB("verifying module: checksum mismatch in sumdb") {
		t.Fatalf("expected sumdb output to trigger retry")
	}
	if shouldRetryInstallForSumDB("build failed: package not found") {
		t.Fatalf("unexpected retry trigger")
	}
}

func TestShouldCheckForUpdates(t *testing.T) {
	now := time.Unix(1_000, 0)
	if !shouldCheckForUpdates(0, now, 24*time.Hour) {
		t.Fatalf("expected first run to check")
	}
	if shouldCheckForUpdates(now.Unix()-60, now, 24*time.Hour) {
		t.Fatalf("expected recent check to be throttled")
	}
	if !shouldCheckForUpdates(now.Unix()-int64(25*time.Hour/time.Second), now, 24*time.Hour) {
		t.Fatalf("expected stale check to run")
	}
}

func TestCheckForUpdatesWithThrottle_FailedResolveWithoutCacheDoesNotThrottle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldResolve := resolveLatestVersionFn
	resolveLatestVersionFn = func(context.Context) (string, error) {
		return "", errors.New("network down")
	}
	t.Cleanup(func() {
		resolveLatestVersionFn = oldResolve
	})

	if _, err := checkForUpdatesWithThrottle(context.Background(), "v0.0.10", 24*time.Hour); err == nil {
		t.Fatalf("expected resolver failure")
	}

	statePath, err := updateStatePath()
	if err != nil {
		t.Fatalf("state path: %v", err)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no state file after failed resolve without cache, got: %v", err)
	}
}

func TestCheckForUpdatesWithThrottle_UsesCacheOnResolveFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := writeUpdateState(updateState{LastSeenVersion: "v0.0.11"}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	oldResolve := resolveLatestVersionFn
	resolveLatestVersionFn = func(context.Context) (string, error) {
		return "", errors.New("network down")
	}
	t.Cleanup(func() {
		resolveLatestVersionFn = oldResolve
	})

	result, err := checkForUpdatesWithThrottle(context.Background(), "v0.0.10", 0)
	if err != nil {
		t.Fatalf("unexpected error with cache: %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatalf("expected cached latest version to surface update availability")
	}
}

func TestCheckForUpdatesWithThrottle_DevBuildUsesInstallAvailability(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := writeUpdateState(updateState{
		LastCheckedUnix: time.Now().Unix(),
		LastSeenVersion: "v0.0.11",
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	result, err := checkForUpdatesWithThrottle(context.Background(), "dev", 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatalf("expected dev build to be update-available when cached release exists")
	}
}
