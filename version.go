package main

import (
	"runtime/debug"
	"strings"
)

var version = "dev"

var readBuildInfo = debug.ReadBuildInfo

func currentVersion() string {
	v := strings.TrimSpace(version)
	if v != "" && v != "dev" {
		return v
	}

	buildInfo, ok := readBuildInfo()
	if !ok || buildInfo == nil {
		return "dev"
	}

	mv := strings.TrimSpace(buildInfo.Main.Version)
	if mv == "" || mv == "(devel)" {
		return "dev"
	}
	return mv
}
