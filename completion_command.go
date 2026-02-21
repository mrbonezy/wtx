package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	zshCompletionBlockStart = "# >>> wtx completion >>>"
	zshCompletionBlockEnd   = "# <<< wtx completion <<<"
)

type zshCompletionStatus struct {
	Installed  bool
	Enabled    bool
	ScriptPath string
	ZshrcPath  string
}

func newCompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Manage shell completion",
		RunE: func(_ *cobra.Command, _ []string) error {
			status, err := detectZshCompletionStatus()
			if err != nil {
				return err
			}
			fmt.Printf("zsh completion installed: %t\n", status.Installed)
			fmt.Printf("zsh completion enabled: %t\n", status.Enabled)
			if !status.Installed || !status.Enabled {
				fmt.Println("Install with: wtx completion install")
			}
			return nil
		},
	}

	cmd.AddCommand(
		newCompletionZshCommand(),
		newCompletionInstallCommand(),
		newCompletionStatusCommand(),
	)
	return cmd
}

func newCompletionZshCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "zsh",
		Short: "Generate zsh completion script",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Root().GenZshCompletion(os.Stdout)
		},
	}
}

func newCompletionInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install zsh completion",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := installZshCompletion(cmd.Root())
			if err != nil {
				return err
			}
			fmt.Printf("Installed completion script: %s\n", status.ScriptPath)
			fmt.Printf("Updated zsh config: %s\n", status.ZshrcPath)
			fmt.Println("Restart shell or run: exec zsh")
			return nil
		},
	}
}

func newCompletionStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show zsh completion install status",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			status, err := detectZshCompletionStatus()
			if err != nil {
				return err
			}
			fmt.Printf("installed: %t\n", status.Installed)
			fmt.Printf("enabled: %t\n", status.Enabled)
			fmt.Printf("script: %s\n", status.ScriptPath)
			fmt.Printf("zshrc: %s\n", status.ZshrcPath)
			if !status.Installed || !status.Enabled {
				fmt.Println("Install with: wtx completion install")
			}
			return nil
		},
	}
}

func detectZshCompletionStatus() (zshCompletionStatus, error) {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return zshCompletionStatus{}, errors.New("HOME not set")
	}
	scriptPath := filepath.Join(home, ".wtx", "completions", "_wtx")
	zshrcPath := filepath.Join(home, ".zshrc")

	status := zshCompletionStatus{
		ScriptPath: scriptPath,
		ZshrcPath:  zshrcPath,
	}

	if info, err := os.Stat(scriptPath); err == nil && info.Size() > 0 {
		status.Installed = true
	}
	data, err := os.ReadFile(zshrcPath)
	if err == nil {
		content := string(data)
		status.Enabled = strings.Contains(content, zshCompletionBlockStart) && strings.Contains(content, zshCompletionBlockEnd)
	}
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return zshCompletionStatus{}, err
	}
	return status, nil
}

func installZshCompletion(root *cobra.Command) (zshCompletionStatus, error) {
	status, err := detectZshCompletionStatus()
	if err != nil {
		return zshCompletionStatus{}, err
	}

	if err := os.MkdirAll(filepath.Dir(status.ScriptPath), 0o755); err != nil {
		return zshCompletionStatus{}, err
	}

	var buf bytes.Buffer
	if err := root.GenZshCompletion(&buf); err != nil {
		return zshCompletionStatus{}, err
	}
	if err := os.WriteFile(status.ScriptPath, buf.Bytes(), 0o644); err != nil {
		return zshCompletionStatus{}, err
	}

	block := strings.Join([]string{
		zshCompletionBlockStart,
		"fpath+=(\"$HOME/.wtx/completions\")",
		"autoload -Uz compinit",
		"compinit",
		zshCompletionBlockEnd,
		"",
	}, "\n")

	current := ""
	if data, err := os.ReadFile(status.ZshrcPath); err == nil {
		current = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return zshCompletionStatus{}, err
	}

	updated := upsertCompletionBlock(current, block)
	if err := os.WriteFile(status.ZshrcPath, []byte(updated), 0o644); err != nil {
		return zshCompletionStatus{}, err
	}

	return detectZshCompletionStatus()
}

func upsertCompletionBlock(content string, block string) string {
	start := strings.Index(content, zshCompletionBlockStart)
	end := strings.Index(content, zshCompletionBlockEnd)
	if start >= 0 && end >= start {
		end += len(zshCompletionBlockEnd)
		replaced := content[:start] + block + content[end:]
		return strings.TrimRight(replaced, "\n") + "\n"
	}
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return block
	}
	return content + "\n\n" + block
}
