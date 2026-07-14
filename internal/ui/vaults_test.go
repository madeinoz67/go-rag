package ui

// vaults_test.go (spec 051) proves the Vaults management surface end-to-end at
// the UI transport: list + active marker (US1), create (US2), switch (US3),
// rename (US4), clear + delete with the default guard (US5), and the guard +
// single-default invariants (US6). Hermetic: a local fake embedder; vaults are
// created/managed through the same engine the daemon serves.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
)

// createVaultViaEngine registers a vault directly (test setup for non-create tests).
func createVaultViaEngine(t *testing.T, eng interface {
	CreateVault(context.Context, string) error
}, name string) {
	t.Helper()
	if err := eng.CreateVault(context.Background(), name); err != nil {
		t.Fatalf("CreateVault(%s): %v", name, err)
	}
}

// vaultNamesFromList decodes GET /api/vaults → the names it carries.
func vaultNamesFromList(t *testing.T, srvURL, tok, vault string) []string {
	t.Helper()
	q := ""
	if vault != "" {
		q = "?vault=" + vault
	}
	resp := bearerGet(t, srvURL+"/api/vaults"+q, tok)
	defer resp.Body.Close()
	var list vaultsListDTO
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode vaults: %v", err)
	}
	out := make([]string, len(list.Vaults))
	for i, v := range list.Vaults {
		out[i] = v.Name
	}
	return out
}

// --- US1: list + active marker (T010) ---

func TestUIVaults_List_ActiveMarker(t *testing.T) {
	eng := newWriteTestEngine(t)
	createVaultViaEngine(t, eng, "default")
	createVaultViaEngine(t, eng, "archive")
	srvURL, tok := authedDocServer(t, eng)

	// Default active vault = "default" (no ?vault= → vaultFromRequest default).
	resp := bearerGet(t, srvURL+"/api/vaults", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status %d, want 200", resp.StatusCode)
	}
	var list vaultsListDTO
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Vaults) != 2 {
		t.Fatalf("want 2 vaults, got %d (%v)", len(list.Vaults), list.Vaults)
	}
	// active = "default"; only the default row is marked active.
	if list.Active != "default" {
		t.Errorf("active: got %q, want default", list.Active)
	}
	for _, v := range list.Vaults {
		if v.Active != (v.Name == "default") {
			t.Errorf("vault %q active=%v, want %v", v.Name, v.Active, v.Name == "default")
		}
	}

	// ?vault=archive → archive is the active marker.
	resp2 := bearerGet(t, srvURL+"/api/vaults?vault=archive", tok)
	defer resp2.Body.Close()
	var list2 vaultsListDTO
	json.NewDecoder(resp2.Body).Decode(&list2)
	if list2.Active != "archive" {
		t.Errorf("?vault=archive active: got %q, want archive", list2.Active)
	}
	for _, v := range list2.Vaults {
		if v.Active != (v.Name == "archive") {
			t.Errorf("vault %q active=%v (archive should be the marker)", v.Name, v.Active)
		}
	}
}

func TestUIVaults_List_Parity(t *testing.T) {
	eng := newWriteTestEngine(t)
	createVaultViaEngine(t, eng, "default")
	createVaultViaEngine(t, eng, "archive")
	srvURL, tok := authedDocServer(t, eng)

	got := vaultNamesFromList(t, srvURL, tok, "")
	want, err := eng.ListVaults("")
	if err != nil {
		t.Fatalf("engine ListVaults: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("UI %d vaults != engine %d", len(got), len(want))
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w.Name] = true
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("UI vault %q not in engine list", g)
		}
	}
}

// --- US2: create (T012) ---

func TestUIVaults_Create(t *testing.T) {
	eng := newWriteTestEngine(t)
	srvURL, tok := authedDocServer(t, eng)

	// Valid create → 201 + appears in the list.
	resp := bearerRequest(t, http.MethodPost, srvURL+"/api/vaults", tok, createVaultRequest{Name: "archive"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d, want 201", resp.StatusCode)
	}
	var v vaultDTO
	json.NewDecoder(resp.Body).Decode(&v)
	if v.Name != "archive" {
		t.Errorf("create response name: got %q, want archive", v.Name)
	}
	names := vaultNamesFromList(t, srvURL, tok, "")
	found := false
	for _, n := range names {
		if n == "archive" {
			found = true
		}
	}
	if !found {
		t.Errorf("archive not listed after create: %v", names)
	}

	// Duplicate → 400.
	dup := bearerRequest(t, http.MethodPost, srvURL+"/api/vaults", tok, createVaultRequest{Name: "archive"})
	dup.Body.Close()
	if dup.StatusCode != http.StatusBadRequest {
		t.Errorf("duplicate create: got %d, want 400", dup.StatusCode)
	}
	// Invalid names → 400.
	for _, bad := range []string{"Bad Name", "UPPER", "under_score"} {
		b := bearerRequest(t, http.MethodPost, srvURL+"/api/vaults", tok, createVaultRequest{Name: bad})
		b.Body.Close()
		if b.StatusCode != http.StatusBadRequest {
			t.Errorf("create %q: got %d, want 400", bad, b.StatusCode)
		}
	}
}

// --- US3: switch (T014) ---

func TestUIVaults_Switch_ActiveFollowsVault(t *testing.T) {
	eng := newWriteTestEngine(t)
	createVaultViaEngine(t, eng, "a")
	createVaultViaEngine(t, eng, "b")
	srvURL, tok := authedDocServer(t, eng)

	// Switch is client-side (the header/?vault=); the server reflects it via the
	// active marker. ?vault=b → b is active, a is not.
	for _, active := range []string{"a", "b"} {
		resp := bearerGet(t, srvURL+"/api/vaults?vault="+active, tok)
		var list vaultsListDTO
		json.NewDecoder(resp.Body).Decode(&list)
		resp.Body.Close()
		if list.Active != active {
			t.Errorf("?vault=%s: active got %q", active, list.Active)
		}
		for _, v := range list.Vaults {
			if v.Active != (v.Name == active) {
				t.Errorf("vault %q active=%v, want %v", v.Name, v.Active, v.Name == active)
			}
		}
	}
}

// --- US4: rename (T016) ---

func TestUIVaults_Rename(t *testing.T) {
	eng := newWriteTestEngine(t)
	createVaultViaEngine(t, eng, "scratch")
	createVaultViaEngine(t, eng, "other")
	srvURL, tok := authedDocServer(t, eng)

	// Rename → 200 + new name lists, old gone.
	resp := bearerRequest(t, http.MethodPost, srvURL+"/api/vaults/scratch/rename", tok, renameVaultRequest{NewName: "drafts"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rename: status %d, want 200", resp.StatusCode)
	}
	names := vaultNamesFromList(t, srvURL, tok, "")
	hasDrafts, hasScratch := false, false
	for _, n := range names {
		if n == "drafts" {
			hasDrafts = true
		}
		if n == "scratch" {
			hasScratch = true
		}
	}
	if !hasDrafts || hasScratch {
		t.Errorf("rename result: drafts=%v scratch=%v, want drafts=true scratch=false", hasDrafts, hasScratch)
	}

	// Collision → 400.
	col := bearerRequest(t, http.MethodPost, srvURL+"/api/vaults/drafts/rename", tok, renameVaultRequest{NewName: "other"})
	col.Body.Close()
	if col.StatusCode != http.StatusBadRequest {
		t.Errorf("rename collision: got %d, want 400", col.StatusCode)
	}
	// Unknown source → 404.
	nf := bearerRequest(t, http.MethodPost, srvURL+"/api/vaults/nope/rename", tok, renameVaultRequest{NewName: "x"})
	nf.Body.Close()
	if nf.StatusCode != http.StatusNotFound {
		t.Errorf("rename unknown: got %d, want 404", nf.StatusCode)
	}
}

// --- US5: clear + delete (T018) ---

func TestUIVaults_ClearDelete(t *testing.T) {
	eng := newWriteTestEngine(t)
	createVaultViaEngine(t, eng, "default")
	createVaultViaEngine(t, eng, "temp")
	srvURL, tok := authedDocServer(t, eng)

	// Delete non-default → 204, gone from the list.
	del := bearerRequest(t, http.MethodDelete, srvURL+"/api/vaults/temp", tok, nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete temp: got %d, want 204", del.StatusCode)
	}
	for _, n := range vaultNamesFromList(t, srvURL, tok, "") {
		if n == "temp" {
			t.Error("temp still listed after delete")
		}
	}

	// Delete default → 400 (guarded).
	dd := bearerRequest(t, http.MethodDelete, srvURL+"/api/vaults/default", tok, nil)
	dd.Body.Close()
	if dd.StatusCode != http.StatusBadRequest {
		t.Errorf("delete default: got %d, want 400", dd.StatusCode)
	}

	// Clear is exercised on a fresh vault (count stays 0; the vault stays listed).
	createVaultViaEngine(t, eng, "scratch")
	clr := bearerRequest(t, http.MethodPost, srvURL+"/api/vaults/scratch/clear", tok, nil)
	clr.Body.Close()
	if clr.StatusCode != http.StatusNoContent {
		t.Errorf("clear: got %d, want 204", clr.StatusCode)
	}
	stillListed := false
	for _, n := range vaultNamesFromList(t, srvURL, tok, "") {
		if n == "scratch" {
			stillListed = true
		}
	}
	if !stillListed {
		t.Error("scratch not listed after clear (should stay registered)")
	}
}

// --- US6: guard + invariants (T019) ---

func TestUIVaults_Guard(t *testing.T) {
	eng := newWriteTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/vaults"},
		{http.MethodPost, "/api/vaults"},
		{http.MethodPost, "/api/vaults/x/rename"},
		{http.MethodPost, "/api/vaults/x/clear"},
		{http.MethodDelete, "/api/vaults/x"},
	}
	for _, c := range cases {
		resp := bearerRequest(t, c.method, srv.URL+c.path, "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without bearer: got %d, want 401", c.method, c.path, resp.StatusCode)
		}
	}
}
