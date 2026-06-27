package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_SetValidPersists(t *testing.T) {
	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("embedding-provider", "ollama")
	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatal(err)
	}

	setCmd := newConfigSetCmd()
	if err := setCmd.RunE(setCmd, []string{"ollama_url", "http://example.com:11434"}); err != nil {
		t.Fatalf("set valid url: %v", err)
	}

	out := captureStdout(t, func() {
		getCmd := newConfigGetCmd()
		_ = getCmd.RunE(getCmd, []string{"ollama_url"})
	})
	if !strings.Contains(out, "http://example.com:11434") {
		t.Errorf("set value must persist via get: %q", out)
	}
}

func TestConfig_SetInvalidRetainsPrevious(t *testing.T) {
	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("embedding-provider", "ollama")
	_ = initCmd.RunE(initCmd, nil) // default ollama_url = http://localhost:11434

	setCmd := newConfigSetCmd()
	if err := setCmd.RunE(setCmd, []string{"ollama_url", "not a url"}); err == nil {
		t.Fatal("setting an invalid URL must error")
	}

	out := captureStdout(t, func() {
		getCmd := newConfigGetCmd()
		_ = getCmd.RunE(getCmd, []string{"ollama_url"})
	})
	if strings.Contains(out, "not a url") {
		t.Errorf("invalid value must not persist; previous should be retained: %q", out)
	}
	if !strings.Contains(out, "http://localhost:11434") {
		t.Errorf("previous value should be retained: %q", out)
	}
}

func TestConfig_GetUnknownKey(t *testing.T) {
	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("embedding-provider", "ollama")
	_ = initCmd.RunE(initCmd, nil)

	getCmd := newConfigGetCmd()
	if err := getCmd.RunE(getCmd, []string{"no_such_key"}); err == nil {
		t.Fatal("get on unknown key must error")
	}
}
