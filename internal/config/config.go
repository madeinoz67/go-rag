// Package config loads and saves the persisted go-rag configuration
// (PRD §5.7), stored as JSON in .go-rag/config.json.
package config

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
