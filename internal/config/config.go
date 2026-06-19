// Package config loads and saves the persisted go-rag configuration
// (PRD §5.7), stored as JSON in .go-rag/config.json.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

// Config is the set of configurable options (PRD §5.7 table).
type Config struct {
	OllamaURL       string   `json:"ollama_url"`
	OllamaModel     string   `json:"ollama_model,omitempty"`
	WatchDirs       []string `json:"watch_dirs"`
	ChunkSize       int      `json:"chunk_size"`
	ChunkOverlap    int      `json:"chunk_overlap"`
	DBPath          string   `json:"db_path"`
	FileGlob        string   `json:"file_glob"`
	PollIntervalSec int      `json:"poll_interval_secs"`
}

// Default returns the configuration used by `go-rag init` when no overrides apply.
func Default() Config {
	return Config{
		OllamaURL:       "http://localhost:11434",
		WatchDirs:       []string{"."},
		ChunkSize:       512,
		ChunkOverlap:    50,
		DBPath:          "./.go-rag",
		FileGlob:        "*",
		PollIntervalSec: 60,
	}
}

// Validate returns an error if the config has invalid values.
func (c Config) Validate() error {
	u, err := url.Parse(c.OllamaURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid ollama_url: %q", c.OllamaURL)
	}
	if c.ChunkSize <= 0 {
		return fmt.Errorf("chunk_size must be positive")
	}
	if c.ChunkOverlap < 0 {
		return fmt.Errorf("chunk_overlap must be non-negative")
	}
	if c.PollIntervalSec <= 0 {
		return fmt.Errorf("poll_interval_secs must be positive")
	}
	return nil
}

// Load reads config from a JSON file.
func Load(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse config %q: %w", path, err)
	}
	return c, nil
}

// Save writes config to a JSON file (creating parent dirs).
func Save(path string, c Config) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Get returns the string value of a config key, or ok=false if unknown.
func (c Config) Get(key string) (string, bool) {
	switch key {
	case "ollama_url":
		return c.OllamaURL, true
	case "ollama_model":
		return c.OllamaModel, true
	case "db_path":
		return c.DBPath, true
	case "file_glob":
		return c.FileGlob, true
	case "chunk_size":
		return strconv.Itoa(c.ChunkSize), true
	case "chunk_overlap":
		return strconv.Itoa(c.ChunkOverlap), true
	case "poll_interval_secs":
		return strconv.Itoa(c.PollIntervalSec), true
	}
	return "", false
}

// Set assigns a string value to a config key, returning an error on an unknown
// key or an invalid value (non-numeric / non-positive where required).
func (c *Config) Set(key, val string) error {
	switch key {
	case "ollama_url":
		c.OllamaURL = val
	case "ollama_model":
		c.OllamaModel = val
	case "db_path":
		c.DBPath = val
	case "file_glob":
		c.FileGlob = val
	case "chunk_size":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid chunk_size: %q", val)
		}
		c.ChunkSize = n
	case "chunk_overlap":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid chunk_overlap: %q", val)
		}
		c.ChunkOverlap = n
	case "poll_interval_secs":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid poll_interval_secs: %q", val)
		}
		c.PollIntervalSec = n
	default:
		return fmt.Errorf("unknown config key: %q", key)
	}
	return nil
}
