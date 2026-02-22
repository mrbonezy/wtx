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
	zshAliasBlockStart      = "# >>> wtx aliases >>>"
	zshAliasBlockEnd        = "# <<< wtx aliases <<<"
)

type zshCompletionStatus struct {
	Installed      bool
	Enabled        bool
	AliasesEnabled bool
	ScriptPath     string
	ZshrcPath      string
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
			fmt.Printf("zsh aliases enabled: %t\n", status.AliasesEnabled)
			if !status.Installed || !status.Enabled {
				fmt.Println("Install with: wtx completion install")
			}
			if !status.AliasesEnabled {
				fmt.Println("Optional aliases: wtx completion aliases install")
			}
			return nil
		},
	}

	cmd.AddCommand(
		newCompletionZshCommand(),
		newCompletionInstallCommand(),
		newCompletionAliasesCommand(),
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
	var aliases bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install zsh completion",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := installZshCompletion(cmd.Root(), aliases)
			if err != nil {
				return err
			}
			fmt.Printf("Installed completion script: %s\n", status.ScriptPath)
			fmt.Printf("Updated zsh config: %s\n", status.ZshrcPath)
			if aliases {
				fmt.Println("Installed aliases: wco, wpr")
			} else {
				fmt.Println("Aliases unchanged (opt-in via: wtx completion install --aliases)")
			}
			fmt.Println("Restart shell or run: exec zsh")
			return nil
		},
	}
	cmd.Flags().BoolVar(&aliases, "aliases", false, "Also install managed aliases (wco, wpr)")
	return cmd
}

func newCompletionAliasesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aliases",
		Short: "Manage optional zsh aliases for wtx",
	}
	cmd.AddCommand(
		newCompletionAliasesInstallCommand(),
		newCompletionAliasesRemoveCommand(),
	)
	return cmd
}

func newCompletionAliasesInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install managed zsh aliases (wco, wpr)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			status, err := installZshAliases()
			if err != nil {
				return err
			}
			fmt.Printf("Updated zsh config: %s\n", status.ZshrcPath)
			fmt.Println("Installed aliases: wco, wpr")
			fmt.Println("Restart shell or run: exec zsh")
			return nil
		},
	}
}

func newCompletionAliasesRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Remove managed zsh aliases (keeps completion)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			status, err := removeZshAliases()
			if err != nil {
				return err
			}
			fmt.Printf("Updated zsh config: %s\n", status.ZshrcPath)
			fmt.Println("Removed managed aliases: wco, wpr")
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
			fmt.Printf("aliases_enabled: %t\n", status.AliasesEnabled)
			fmt.Printf("script: %s\n", status.ScriptPath)
			fmt.Printf("zshrc: %s\n", status.ZshrcPath)
			if !status.Installed || !status.Enabled {
				fmt.Println("Install with: wtx completion install")
			}
			if !status.AliasesEnabled {
				fmt.Println("Optional aliases: wtx completion aliases install")
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
		status.AliasesEnabled = strings.Contains(content, zshAliasBlockStart) && strings.Contains(content, zshAliasBlockEnd)
	}
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return zshCompletionStatus{}, err
	}
	return status, nil
}

func installZshCompletion(root *cobra.Command, withAliases bool) (zshCompletionStatus, error) {
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
	if withAliases {
		updated = upsertAliasBlock(updated, zshAliasesBlock())
	}
	if err := os.WriteFile(status.ZshrcPath, []byte(updated), 0o644); err != nil {
		return zshCompletionStatus{}, err
	}

	return detectZshCompletionStatus()
}

func installZshAliases() (zshCompletionStatus, error) {
	status, err := detectZshCompletionStatus()
	if err != nil {
		return zshCompletionStatus{}, err
	}
	current := ""
	if data, err := os.ReadFile(status.ZshrcPath); err == nil {
		current = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return zshCompletionStatus{}, err
	}
	updated := upsertAliasBlock(current, zshAliasesBlock())
	if err := os.WriteFile(status.ZshrcPath, []byte(updated), 0o644); err != nil {
		return zshCompletionStatus{}, err
	}
	return detectZshCompletionStatus()
}

func removeZshAliases() (zshCompletionStatus, error) {
	status, err := detectZshCompletionStatus()
	if err != nil {
		return zshCompletionStatus{}, err
	}
	current := ""
	if data, err := os.ReadFile(status.ZshrcPath); err == nil {
		current = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return zshCompletionStatus{}, err
	}
	updated := removeManagedBlock(current, zshAliasBlockStart, zshAliasBlockEnd)
	if err := os.WriteFile(status.ZshrcPath, []byte(updated), 0o644); err != nil {
		return zshCompletionStatus{}, err
	}
	return detectZshCompletionStatus()
}

func upsertCompletionBlock(content string, block string) string {
	return upsertManagedBlock(content, block, zshCompletionBlockStart, zshCompletionBlockEnd)
}

func upsertAliasBlock(content string, block string) string {
	return upsertManagedBlock(content, block, zshAliasBlockStart, zshAliasBlockEnd)
}

func zshAliasesBlock() string {
	return strings.Join([]string{
		zshAliasBlockStart,
		"alias wco='wtx co'",
		"alias wpr='wtx pr'",
		"compdef _wtx wco",
		"compdef _wtx wpr",
		zshAliasBlockEnd,
		"",
	}, "\n")
}

func upsertManagedBlock(content string, block string, startMarker string, endMarker string) string {
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start >= 0 && end >= start {
		end += len(endMarker)
		replaced := content[:start] + block + content[end:]
		return strings.TrimRight(replaced, "\n") + "\n"
	}
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return block
	}
	return content + "\n\n" + block
}

func removeManagedBlock(content string, startMarker string, endMarker string) string {
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start < 0 || end < start {
		return strings.TrimRight(content, "\n") + "\n"
	}
	end += len(endMarker)
	trimLeft := strings.TrimRight(content[:start], "\n")
	trimRight := strings.TrimLeft(content[end:], "\n")
	if trimLeft == "" && trimRight == "" {
		return ""
	}
	if trimLeft == "" {
		return trimRight + "\n"
	}
	if trimRight == "" {
		return trimLeft + "\n"
	}
	return trimLeft + "\n\n" + trimRight + "\n"
}
