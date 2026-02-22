package main

import (
	"runtime/debug"
	"testing"
)

func TestCurrentVersion_PrefersExplicitVersion(t *testing.T) {
	oldVersion := version
	oldReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		version = oldVersion
		readBuildInfo = oldReadBuildInfo
	})

	version = "v9.9.9"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v1.2.3"},
		}, true
	}

	if got := currentVersion(); got != "v9.9.9" {
		t.Fatalf("expected explicit version, got %q", got)
	}
}

func TestCurrentVersion_UsesBuildInfoWhenDev(t *testing.T) {
	oldVersion := version
	oldReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		version = oldVersion
		readBuildInfo = oldReadBuildInfo
	})

	version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v1.2.3"},
		}, true
	}

	if got := currentVersion(); got != "v1.2.3" {
		t.Fatalf("expected build-info version, got %q", got)
	}
}

func TestCurrentVersion_FallsBackToDev(t *testing.T) {
	oldVersion := version
	oldReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		version = oldVersion
		readBuildInfo = oldReadBuildInfo
	})

	version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "(devel)"},
		}, true
	}

	if got := currentVersion(); got != "dev" {
		t.Fatalf("expected dev fallback, got %q", got)
	}
}
