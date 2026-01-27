package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds the agent configuration
type Config struct {
	Port      string   `json:"port"`                  // WebSocket server port (default: 9081)
	FileTypes []string `json:"file_types,omitempty"`  // File types to watch (e.g., [".csv", ".xlsx"])
	Recursive bool     `json:"recursive"`             // Watch directories recursively
	Debug     bool     `json:"debug"`                 // Enable debug logging
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Port:      "9081",
		FileTypes: []string{".csv", ".xlsx", ".xls", ".json", ".xml", ".parquet"},
		Recursive: true,
		Debug:     false,
	}
}

// ConfigDir returns the nodefy config directory path
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".nodefy"
	}
	return filepath.Join(home, ".nodefy")
}

// ConfigPath returns the default config file path
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "agent.json")
}

// LogPath returns the default log file path
func LogPath() string {
	return filepath.Join(ConfigDir(), "agent.log")
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
		return DefaultConfig(), nil // Return defaults on any read error
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return DefaultConfig(), nil // Return defaults on parse error
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
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
