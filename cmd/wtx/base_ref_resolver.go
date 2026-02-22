package main

import "strings"

func resolveNewBranchBaseRef(configBaseRef string, statusBaseRef string) string {
	base := strings.TrimSpace(configBaseRef)
	if base != "" {
		return base
	}
	base = strings.TrimSpace(statusBaseRef)
	if base != "" {
		return base
	}
	return "origin/main"
}
