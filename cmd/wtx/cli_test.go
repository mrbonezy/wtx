package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunVersionFlag(t *testing.T) {
	oldResolve := resolveLatestVersionFn
	resolveLatestVersionFn = func(context.Context) (string, error) {
		return "v9.9.9", nil
	}
	t.Cleanup(func() {
		resolveLatestVersionFn = oldResolve
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	if err := run([]string{"wtx", "--version"}); err != nil {
		t.Fatalf("run --version: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	got := strings.TrimSpace(out.String())
	want := currentVersion()
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRunVersionFlagAlias(t *testing.T) {
	oldResolve := resolveLatestVersionFn
	resolveLatestVersionFn = func(context.Context) (string, error) {
		return "v9.9.9", nil
	}
	t.Cleanup(func() {
		resolveLatestVersionFn = oldResolve
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	if err := run([]string{"wtx", "-v"}); err != nil {
		t.Fatalf("run -v: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	got := strings.TrimSpace(out.String())
	want := currentVersion()
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRunVersionFlagChecksLatest(t *testing.T) {
	called := false
	oldResolve := resolveLatestVersionFn
	resolveLatestVersionFn = func(context.Context) (string, error) {
		called = true
		return "v9.9.9", nil
	}
	t.Cleanup(func() {
		resolveLatestVersionFn = oldResolve
	})

	if err := run([]string{"wtx", "--version"}); err != nil {
		t.Fatalf("run --version: %v", err)
	}
	if !called {
		t.Fatalf("expected --version to check latest version")
	}
}

func TestPromptAndMaybeInstallVersionUpdate_NoSkipsInstall(t *testing.T) {
	called := false
	oldInstall := installVersionFn
	installVersionFn = func(context.Context, string) error {
		called = true
		return nil
	}
	t.Cleanup(func() {
		installVersionFn = oldInstall
	})

	var out bytes.Buffer
	err := promptAndMaybeInstallVersionUpdate(strings.NewReader("n\n"), &out, updateCheckResult{
		CurrentVersion:  "v1.0.0",
		LatestVersion:   "v1.1.0",
		UpdateAvailable: true,
	})
	if err != nil {
		t.Fatalf("promptAndMaybeInstallVersionUpdate no: %v", err)
	}
	if called {
		t.Fatalf("expected install not to be called")
	}
	if !strings.Contains(out.String(), "Skipped update.") {
		t.Fatalf("expected skipped message, got %q", out.String())
	}
}

func TestPromptAndMaybeInstallVersionUpdate_YesInstalls(t *testing.T) {
	called := false
	var installed string
	oldInstall := installVersionFn
	installVersionFn = func(_ context.Context, target string) error {
		called = true
		installed = target
		return nil
	}
	t.Cleanup(func() {
		installVersionFn = oldInstall
	})

	var out bytes.Buffer
	err := promptAndMaybeInstallVersionUpdate(strings.NewReader("yes\n"), &out, updateCheckResult{
		CurrentVersion:  "v1.0.0",
		LatestVersion:   "v1.1.0",
		UpdateAvailable: true,
	})
	if err != nil {
		t.Fatalf("promptAndMaybeInstallVersionUpdate yes: %v", err)
	}
	if !called {
		t.Fatalf("expected install to be called")
	}
	if installed != "v1.1.0" {
		t.Fatalf("expected target v1.1.0, got %q", installed)
	}
	if !strings.Contains(out.String(), "Updated wtx to v1.1.0") {
		t.Fatalf("expected updated message, got %q", out.String())
	}
}
