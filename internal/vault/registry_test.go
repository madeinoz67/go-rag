package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
)

func TestValidateName(t *testing.T) {
	valid := []string{"default", "cyber-notes", "vault-1", "a", "123"}
	invalid := []string{"", "UPPER", "with space", "under_score", "d.ot", string(make([]byte, 65))}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", n)
		}
	}
}

func TestCreateExistsList(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GO_RAG_VAULT_ROOT", root)

	if err := Create("alpha", config.Default()); err != nil {
		t.Fatal(err)
	}
	if !Exists("alpha") {
		t.Fatal("Exists should report true after Create")
	}
	if Exists("beta") {
		t.Fatal("Exists should report false for non-existent vault")
	}
	// Duplicate create should error
	if err := Create("alpha", config.Default()); err == nil {
		t.Fatal("Create should error on duplicate")
	}
	// List
	names := List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("List = %v, want [alpha]", names)
	}
	// Verify config.json written
	if _, err := os.Stat(filepath.Join(root, "alpha", "config.json")); err != nil {
		t.Fatalf("config.json not created: %v", err)
	}
	// Verify data dir created
	if _, err := os.Stat(filepath.Join(root, "alpha", "data")); err != nil {
		t.Fatalf("data/ not created: %v", err)
	}
}

func TestDeleteClear(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GO_RAG_VAULT_ROOT", root)

	Create("test", config.Default())

	// Clear removes data but keeps config
	if err := Clear("test"); err != nil {
		t.Fatal(err)
	}
	if !Exists("test") {
		t.Fatal("vault should still exist after Clear (config preserved)")
	}
	if _, err := os.Stat(filepath.Join(root, "test", "data")); !os.IsNotExist(err) {
		t.Fatal("data/ should be gone after Clear")
	}

	// Delete removes everything
	if err := Delete("test"); err != nil {
		t.Fatal(err)
	}
	if Exists("test") {
		t.Fatal("vault should not exist after Delete")
	}

	// Delete on non-existent vault errors
	if err := Delete("nope"); err == nil {
		t.Fatal("Delete should error on non-existent vault")
	}
}
