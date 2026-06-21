package cli

import (
	"strings"
	"testing"
)

// TestServeBootGate_RejectsExternalWithoutOptIn exercises the serve boot gate
// end-to-end via cobra: an external bind address with no --bind-external must be
// refused before any listener opens or DB is touched (spec 007 FR-001/003,
// SC-002). The gate runs ahead of openDB, so this test binds nothing and opens
// no Pebble store.
func TestServeBootGate_RejectsExternalWithoutOptIn(t *testing.T) {
	cmd := newServeCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--mcp-addr", "0.0.0.0:17878", "--grpc-addr", "192.168.1.9:17880"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("serve must refuse external bind without --bind-external")
	}
	for _, want := range []string{"0.0.0.0:17878", "192.168.1.9:17880", "--bind-external"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must mention %q, got: %v", want, err)
		}
	}
}

// TestServeBootGate_OptInFlagRegistered confirms the opt-in flag exists and
// defaults off — so the contract is discoverable in --help and the default is
// fail-closed (FR-004/007).
func TestServeBootGate_OptInFlagRegistered(t *testing.T) {
	cmd := newServeCmd()
	b, err := cmd.Flags().GetBool("bind-external")
	if err != nil {
		t.Fatalf("bind-external flag not registered on serve: %v", err)
	}
	if b {
		t.Error("bind-external must default to false (fail-closed)")
	}
}
