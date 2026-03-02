package cmd

import "testing"

func TestParseBoolArg(t *testing.T) {
	if !parseBoolArg([]string{"--worktree", "/tmp/wt.1", "--force-unlock"}, "--force-unlock") {
		t.Fatalf("expected --force-unlock to be detected")
	}
	if parseBoolArg([]string{"--worktree", "/tmp/wt.1"}, "--force-unlock") {
		t.Fatalf("did not expect --force-unlock when flag is absent")
	}
}

func TestShouldStartIsolatedTmuxSession(t *testing.T) {
	tests := []struct {
		name          string
		current       string
		sessionParent string
		want          bool
	}{
		{
			name:          "same terminal does not isolate",
			current:       "Ghostty",
			sessionParent: "ghostty",
			want:          false,
		},
		{
			name:          "different terminal isolates",
			current:       "Apple_Terminal",
			sessionParent: "Ghostty",
			want:          true,
		},
		{
			name:          "missing session metadata does not isolate",
			current:       "Apple_Terminal",
			sessionParent: "",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStartIsolatedTmuxSession(tt.current, tt.sessionParent); got != tt.want {
				t.Fatalf("shouldStartIsolatedTmuxSession(%q, %q)=%v, want %v", tt.current, tt.sessionParent, got, tt.want)
			}
		})
	}
}

func TestWTXPaneStyleOptions(t *testing.T) {
	options := wtxPaneStyleOptions()
	if len(options) == 0 {
		t.Fatalf("expected pane style options")
	}

	valuesByKey := make(map[string]string, len(options))
	for _, option := range options {
		valuesByKey[option.key] = option.value
	}

	expected := map[string]string{
		"pane-border-style":        "fg=#1e1530",
		"pane-active-border-style": "fg=#6a4b9c",
		"pane-border-lines":        "heavy",
		"pane-border-status":       "off",
		"pane-border-format":       "#{?#{&&:#{pane_active},#{>:#{window_panes},1}},#[bold fg=#1e1530 bg=#6a4b9c] ACTIVE #[default],}",
	}

	for key, want := range expected {
		got, ok := valuesByKey[key]
		if !ok {
			t.Fatalf("expected option %q to be present", key)
		}
		if got != want {
			t.Fatalf("expected option %q value %q, got %q", key, want, got)
		}
	}
}

func TestShouldDisableTmuxInputEnhancements(t *testing.T) {
	tests := []struct {
		name string
		term string
		want bool
	}{
		{name: "iterm", term: "iTerm.app", want: true},
		{name: "ghostty", term: "ghostty", want: true},
		{name: "ghostty version suffix", term: "ghostty2", want: true},
		{name: "apple terminal", term: "Apple_Terminal", want: false},
		{name: "unknown", term: "xterm-256color", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldDisableTmuxInputEnhancements(tt.term); got != tt.want {
				t.Fatalf("shouldDisableTmuxInputEnhancements(%q)=%v, want %v", tt.term, got, tt.want)
			}
		})
	}
}
