package ui

// vaults.go (spec 051) is the console's Vaults MANAGEMENT surface. The unified
// store (spec 052) serves every vault from one db; this view lets the operator
// list vaults, create one, switch the active vault live (client-side), and
// rename/clear/delete — all over EXISTING engine methods (ListVaults/CreateVault/
// RenameVault/ClearVault/DeleteVault). Switch carries no route (it is a
// client-side X-Go-Rag-Vault header change); the server confirms a switch only
// implicitly via subsequent reads.
//
// Routes (all spec 045 Bearer-guarded):
//
//	GET    /api/vaults                  → every vault + the active marker (US1)
//	POST   /api/vaults                  → create a vault (US2)
//	POST   /api/vaults/{name}/rename    → rename (US4)
//	POST   /api/vaults/{name}/clear     → empty a vault, keep it (US5)
//	DELETE /api/vaults/{name}           → delete (US5; default refused)
//
// Destructive ops (create/rename/clear/delete) are confirmed client-side; the
// server executes the guarded mutation on receipt.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/madeinoz67/go-rag/internal/engine"
)

// vaultDTO is one vault row: name, document count, and whether it is the
// caller's active vault (the one the shell picker targets).
type vaultDTO struct {
	Name      string `json:"name"`
	Documents int    `json:"documents"`
	Active    bool   `json:"active"`
}

// vaultsListDTO is the GET /api/vaults envelope. `active` names the caller's
// current vault so the UI can mark it without guessing.
type vaultsListDTO struct {
	Vaults []vaultDTO `json:"vaults"`
	Active string     `json:"active"`
}

type createVaultRequest struct {
	Name string `json:"name"`
}

type renameVaultRequest struct {
	NewName string `json:"new_name"`
}

// toVaultsListDTO projects engine entries + the active vault name into the UI
// envelope. The active flag is computed server-side from the caller's vault.
func toVaultsListDTO(entries []engine.VaultEntry, active string) vaultsListDTO {
	out := vaultsListDTO{Active: active, Vaults: make([]vaultDTO, 0, len(entries))}
	for _, e := range entries {
		out.Vaults = append(out.Vaults, vaultDTO{Name: e.Name, Documents: e.Documents, Active: e.Name == active})
	}
	return out
}

// vaultEntryByName re-reads the registry for one vault's {Name, Documents}.
// Used to build the create/rename response DTO (those engine calls return no
// entry; a fresh lookup is cheap — vault counts are small).
func (s *Server) vaultEntryByName(name string) (engine.VaultEntry, bool) {
	entries, err := s.eng.ListVaults("")
	if err != nil {
		return engine.VaultEntry{}, false
	}
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return engine.VaultEntry{}, false
}

// handleVaultsList is the UI projection of Engine.ListVaults (US1):
// GET /api/vaults → every vault with the active marker (the caller's current
// vault per X-Go-Rag-Vault / ?vault=).
func (s *Server) handleVaultsList(w http.ResponseWriter, r *http.Request) {
	entries, err := s.eng.ListVaults("")
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toVaultsListDTO(entries, vaultFromRequest(r)))
}

// handleVaultCreate is the UI projection of Engine.CreateVault (US2):
// POST /api/vaults {name} → register a new empty vault. 201 + vaultDTO;
// 400 invalid (bad name / duplicate).
func (s *Server) handleVaultCreate(w http.ResponseWriter, r *http.Request) {
	var req createVaultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := s.eng.CreateVault(r.Context(), name); err != nil {
		writeEngineErr(w, err) // ErrInvalid (bad name / duplicate) → 400
		return
	}
	active := vaultFromRequest(r)
	entry, ok := s.vaultEntryByName(name)
	if !ok {
		entry = engine.VaultEntry{Name: name}
	}
	writeJSON(w, http.StatusCreated, vaultDTO{Name: entry.Name, Documents: entry.Documents, Active: entry.Name == active})
}

// handleVaultRename is the UI projection of Engine.RenameVault (US4):
// POST /api/vaults/{name}/rename {new_name} → metadata-only rename (data
// identity preserved). 200 + vaultDTO; 400 invalid/collision; 404 unknown.
func (s *Server) handleVaultRename(w http.ResponseWriter, r *http.Request) {
	oldName := r.PathValue("name")
	if strings.TrimSpace(oldName) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	var req renameVaultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	newName := strings.TrimSpace(req.NewName)
	if err := s.eng.RenameVault(r.Context(), oldName, newName); err != nil {
		writeEngineErr(w, err) // ErrInvalid (bad/collision) → 400; ErrNotFound → 404
		return
	}
	active := vaultFromRequest(r)
	entry, ok := s.vaultEntryByName(newName)
	if !ok {
		entry = engine.VaultEntry{Name: newName}
	}
	writeJSON(w, http.StatusOK, vaultDTO{Name: entry.Name, Documents: entry.Documents, Active: entry.Name == active})
}

// handleVaultClear is the UI projection of Engine.ClearVault (US5):
// POST /api/vaults/{name}/clear → empty the vault's contents; it stays
// registered + writable. 204 (idempotent).
func (s *Server) handleVaultClear(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.eng.ClearVault(r.Context(), name); err != nil {
		writeEngineErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVaultDelete is the UI projection of Engine.DeleteVault (US5):
// DELETE /api/vaults/{name} → clear + unregister. 204; 400 if {name} is default
// (the default vault is always present). Idempotent for unknown names.
func (s *Server) handleVaultDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.eng.DeleteVault(r.Context(), name); err != nil {
		writeEngineErr(w, err) // ErrInvalid (default) → 400
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
