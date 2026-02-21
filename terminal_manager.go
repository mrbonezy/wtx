package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	tabTitleMu   sync.Mutex
	lastTabTitle string
)

func setITermWTXTab() {
	setITermTab("wtx")
}

func setITermWTXBranchTab(branch string) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		setITermWTXTab()
		return
	}
	setITermTab("wtx - " + branch)
}

func setITermTab(title string) {
	inTmux := strings.TrimSpace(os.Getenv("TMUX")) != ""
	if !inTmux && strings.TrimSpace(os.Getenv("TERM_PROGRAM")) != "iTerm.app" {
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "wtx"
	}
	if shouldSkipTabTitleUpdate(title) {
		return
	}
	// Outside tmux we control title directly; inside tmux title is managed by tmux.
	if !inTmux {
		writeTerminalEscape("\x1b]0;" + title + "\x07")
		writeTerminalEscape("\x1b]1;" + title + "\x07")
		writeTerminalEscape("\x1b]2;" + title + "\x07")
	}
	writeTerminalEscape("\x1b]1337;SetTabColor=rgb:3d/2a/5c\x07")
	writeTerminalEscape("\x1b]6;1;bg;red;brightness;61\x07")
	writeTerminalEscape("\x1b]6;1;bg;green;brightness;42\x07")
	writeTerminalEscape("\x1b]6;1;bg;blue;brightness;92\x07")
}

func resetITermTabColor() {
	inTmux := strings.TrimSpace(os.Getenv("TMUX")) != ""
	if !inTmux && strings.TrimSpace(os.Getenv("TERM_PROGRAM")) != "iTerm.app" {
		return
	}
	// Clear iTerm custom tab color and let defaults apply.
	writeTerminalEscape("\x1b]1337;SetTabColor=\x07")
}

func writeTerminalEscape(seq string) {
	if strings.TrimSpace(seq) == "" {
		return
	}
	// When inside tmux, wrap OSC sequences so iTerm receives them.
	if strings.TrimSpace(os.Getenv("TMUX")) != "" {
		escaped := strings.ReplaceAll(seq, "\x1b", "\x1b\x1b")
		fmt.Fprint(os.Stdout, "\x1bPtmux;", escaped, "\x1b\\")
		return
	}
	fmt.Fprint(os.Stdout, seq)
}

func shouldSkipTabTitleUpdate(title string) bool {
	tabTitleMu.Lock()
	defer tabTitleMu.Unlock()
	if title == lastTabTitle {
		return true
	}
	lastTabTitle = title
	return false
}
