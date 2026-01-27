package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the agent configuration
type Config struct {
	OrchestratorURL string   `json:"orchestrator_url"`
	SessionKey      string   `json:"session_key"`
	WatchPaths      []string `json:"watch_paths"`
	FileTypes       []string `json:"file_types,omitempty"` // e.g., [".csv", ".xlsx", ".json"]
	Recursive       bool     `json:"recursive"`
	ReconnectDelay  int      `json:"reconnect_delay_seconds"` // seconds
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		OrchestratorURL: "ws://localhost:9080/ws/agent",
		SessionKey:      "",
		WatchPaths:      []string{},
		FileTypes:       []string{".csv", ".xlsx", ".xls", ".json", ".xml", ".parquet"},
		Recursive:       true,
		ReconnectDelay:  5,
	}
}

// ConfigPath returns the default config file path
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".nodefy-agent.json"
	}
	return filepath.Join(home, ".nodefy", "agent.json")
}

// Load loads configuration from file
func Load(path string) (*Config, error) {
	if path == "" {
		path = ConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return config, nil
}

// Save saves configuration to file
func (c *Config) Save(path string) error {
	if path == "" {
		path = ConfigPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.OrchestratorURL == "" {
		return fmt.Errorf("orchestrator_url is required")
	}
	if c.SessionKey == "" {
		return fmt.Errorf("session_key is required")
	}
	// watch_paths is optional - frontend can add paths dynamically
	return nil
}
