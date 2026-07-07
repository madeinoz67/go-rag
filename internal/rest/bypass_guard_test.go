package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
)

// bypass_guard_test.go is the regression guard for the spec 045 loopback bypass.
// It exists because the bypass's safety rests on a cross-module invariant —
// "any init'd vault has an admin, so storesEmpty() is false, so the bypass never
// fires on a running daemon" — that has NO compile-time enforcement. A previous
// refactor (re-keying storesEmpty from "no admin AND no key" to "no key")
// silently re-armed full-admin bypass on every running vault; the morning's unit
// test was edited alongside that change and so did not catch it.
//
// This test lives OUTSIDE the auth package on purpose. A change to
// internal/auth/bypass.go's storesEmpty must NOT also edit this file to match;
// if this test fails after such a change, the bypass has been re-armed on
// initialized vaults and the security posture has regressed — re-run the bypass
// decision panel before relaxing it. The bypass grants ModeAdmin, and loopback
// cannot identify the principal (a reverse proxy on the same host, the
// operator's browser via fetch('localhost'), or local malware are all loopback
// peers), so firing on an init'd vault = admin to non-operator actors.

// TestBypassGuard_BareVaultBypasses_InitializedVaultDoesNot pins BOTH halves:
//
//  1. BARE vault (no admin, no key) + loopback + no Bearer → 200 (the bypass is
//     the intended "local just works" safety valve for a pre-init vault).
//  2. INITIALIZED vault (admin present) + loopback + no Bearer → 401 (the bypass
//     MUST NOT fire — this is the load-bearing security invariant).
func TestBypassGuard_BareVaultBypasses_InitializedVaultDoesNot(t *testing.T) {
	// (1) Bare vault — bypass fires on loopback.
	bareEng := newEngineWithCorpus(t, "bare vault bypass safety valve")
	bareSrv := httptest.NewServer(New(bareEng, "").Handler())
	defer bareSrv.Close()
	resp, err := http.Get(bareSrv.URL + "/v1/status")
	if err != nil {
		t.Fatalf("bare vault GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bare vault + loopback + no bearer: want 200 (bypass), got %d — the bare-vault safety valve is broken", resp.StatusCode)
	}

	// (2) Initialized vault — an admin exists (as every init'd vault has). The
	// bypass MUST NOT fire: loopback + no bearer → 401.
	initEng := newEngineWithCorpus(t, "initialized vault must reject loopback no-bearer")
	if _, err := auth.CreateAdmin(auth.NewStore(initEng.DB()), auth.DefaultAdminUsername, "pw"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	initSrv := httptest.NewServer(New(initEng, "").Handler())
	defer initSrv.Close()
	resp2, err := http.Get(initSrv.URL + "/v1/status")
	if err != nil {
		t.Fatalf("init vault GET: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		// If this fails: the loopback bypass is firing on an admin-bearing
		// (i.e. every running) vault. That grants admin to any loopback peer —
		// a same-host reverse proxy, the operator's browser, or local malware.
		// Do NOT just flip the expected status; re-derive the security argument
		// (see bypass.go::storesEmpty + this file's package comment).
		t.Fatalf("initialized vault (admin present) + loopback + no bearer: want 401 (bypass must NOT fire), got %d — admin bypass re-armed on a running vault", resp2.StatusCode)
	}
}
