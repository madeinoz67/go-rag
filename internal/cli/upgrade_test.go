package cli

import "testing"

// TestUpgradeCmdFlagsAndUse confirms the upgrade command is wired with its
// contract flags. Deeper behavioral coverage (version discovery, comparison,
// non-destructive --check) lives in internal/upgrade/*_test.go; the --check path
// is non-destructive by construction (it never calls SelfUpdate).
func TestUpgradeCmdFlagsAndUse(t *testing.T) {
	cmd := newUpgradeCmd("v1.0.0")
	if cmd.Use != "upgrade" {
		t.Errorf("Use = %q, want \"upgrade\"", cmd.Use)
	}
	for _, f := range []string{"check", "yes", "rollback"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("flag --%s not registered on the upgrade command", f)
		}
	}
}
