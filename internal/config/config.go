package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Port         string   `json:"port"`
	FileTypes    []string `json:"file_types,omitempty"`
	Recursive    bool     `json:"recursive"`
	Debug        bool     `json:"debug"`
	BridgeURL    string   `json:"bridge_url,omitempty"`
	BridgeAPIKey string   `json:"bridge_api_key,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		Port:      "9081",
		FileTypes: []string{".csv", ".xlsx", ".xls", ".json", ".xml", ".parquet"},
		Recursive: true,
		Debug:     false,
	}
}

func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".nodefy"
	}
	return filepath.Join(home, ".nodefy")
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "agent.json")
}

func LogPath() string {
	return filepath.Join(ConfigDir(), "agent.log")
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = ConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return DefaultConfig(), nil
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return DefaultConfig(), nil
	}

	return config, nil
}

func (c *Config) Save(path string) error {
	if path == "" {
		path = ConfigPath()
	}

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
