package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	AgentCommand        string `json:"agent_command"`
	NewBranchBaseRef    string `json:"new_branch_base_ref,omitempty"`
	NewBranchFetchFirst *bool  `json:"new_branch_fetch_first,omitempty"`
}

const defaultAgentCommand = "claude"

func LoadConfig() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.AgentCommand = strings.TrimSpace(cfg.AgentCommand)
	if cfg.AgentCommand == "" {
		cfg.AgentCommand = defaultAgentCommand
	}
	cfg.NewBranchBaseRef = strings.TrimSpace(cfg.NewBranchBaseRef)
	return cfg, nil
}

func ConfigExists() (bool, error) {
	path, err := configPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func SaveConfig(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func configPath() (string, error) {
	home := os.Getenv("HOME")
	if strings.TrimSpace(home) == "" {
		return "", errors.New("HOME not set")
	}
	return filepath.Join(home, ".wtx", "config.json"), nil
}
