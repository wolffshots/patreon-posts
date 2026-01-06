package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Campaign represents a saved campaign
type Campaign struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Config holds the application configuration
type Config struct {
	Cookies        string     `json:"cookies"`
	Campaigns      []Campaign `json:"campaigns,omitempty"`
	PublishedAfter string     `json:"published_after,omitempty"` // Filter posts to those published after this date (YYYY-MM-DD)
}

// DefaultConfigPath returns the default config file path
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".patreon-posts.json"), nil
}

// Load reads configuration from file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// Save writes configuration to file
func Save(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}
