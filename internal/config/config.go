package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const configDir = ".essh"
const configFile = "config.json"

// Config represents the ~/.essh/config.json file.
type Config struct {
	StoragePath string `json:"storage_path"`
	KeyfilePath string `json:"keyfile_path,omitempty"`
}

// Dir returns the path to ~/.essh/.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	return filepath.Join(home, configDir), nil
}

// Path returns the full path to ~/.essh/config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFile), nil
}

// ExpandPath expands a leading ~ to the user's home directory.
func ExpandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// CollapsePath replaces the user's home directory prefix with ~.
func CollapsePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// Load reads the config from ~/.essh/config.json.
// Paths containing ~ are expanded to absolute paths.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg.StoragePath = ExpandPath(cfg.StoragePath)
	cfg.KeyfilePath = ExpandPath(cfg.KeyfilePath)
	return &cfg, nil
}

// Save writes the config to ~/.essh/config.json.
func Save(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	p := filepath.Join(dir, configFile)
	return os.WriteFile(p, data, 0600)
}
