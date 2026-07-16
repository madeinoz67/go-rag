package ui

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/storage/migrate"
	"github.com/madeinoz67/go-rag/internal/upgrade"
)

// systemStatusDTO — spec 056 Slice 1. Read-only projection of daemon identity +
// transport posture + storage schema. No egress (FR-002/003/006). On-disk schema
// is always == ExpectedVersion for a RUNNING daemon: a newer store is refused at
// open and an older one migrated up (the refuse-newer invariant, spec 034), so a
// single version field is truthful and needs no pebble read.
type systemStatusDTO struct {
	Version       string         `json:"version"`
	PID           int            `json:"pid"`
	UptimeSeconds int            `json:"uptime_seconds"`
	Schema        schemaDTO      `json:"schema"`
	Transports    []transportDTO `json:"transports"`
	BindWarning   string         `json:"bind_warning"`
}

type schemaDTO struct {
	Version      int  `json:"version"`
	UnifiedStore bool `json:"unified_store"`
}

type transportDTO struct {
	Kind     string `json:"kind"`
	Address  string `json:"address"`
	Loopback bool   `json:"loopback"`
	State    string `json:"state"`
}

// updateCheckDTO — spec 056 US3. Operator-initiated (POST); the one sanctioned
// egress (mirrors `go-rag upgrade`). latest="unknown" when the release source is
// unreachable (graceful, FR-008).
type updateCheckDTO struct {
	Current        string `json:"current"`
	Latest         string `json:"latest"`
	NewerAvailable bool   `json:"newer_available"`
	CheckedAt      string `json:"checked_at"`
}

// handleSystem — GET /api/settings/system. Read-only; no egress.
func (s *Server) handleSystem(w http.ResponseWriter, _ *http.Request) {
	addrs, _ := daemon.ReadAddrs(s.eng.Config().DBPath)
	writeJSON(w, http.StatusOK, toSystemDTO(addrs, s.version, s.startedAt))
}

func toSystemDTO(addrs daemon.Addrs, version string, startedAt time.Time) systemStatusDTO {
	transports := []transportDTO{
		mkTransport("mcp", addrs.MCPAddr),
		mkTransport("rest", addrs.RESTAddr),
		mkTransport("grpc", addrs.GRPCAddr),
		mkTransport("ui", addrs.UIAddr),
	}
	return systemStatusDTO{
		Version:       version,
		PID:           os.Getpid(),
		UptimeSeconds: int(time.Since(startedAt).Seconds()),
		Schema: schemaDTO{
			Version:      int(migrate.ExpectedVersion),
			UnifiedStore: true, // spec 052: one daemon serves all vaults
		},
		Transports:  transports,
		BindWarning: bindWarning(transports),
	}
}

func mkTransport(kind, addr string) transportDTO {
	if addr == "" {
		return transportDTO{Kind: kind, State: "disabled"}
	}
	return transportDTO{Kind: kind, Address: addr, Loopback: daemon.IsLoopbackBind(addr), State: "listening"}
}

// bindWarning lists any enabled non-loopback transports (security posture, spec 007).
func bindWarning(ts []transportDTO) string {
	var offenders []string
	for _, t := range ts {
		if t.State == "listening" && !t.Loopback {
			offenders = append(offenders, t.Kind)
		}
	}
	if len(offenders) == 0 {
		return ""
	}
	return "non-loopback bind: " + strings.Join(offenders, ", ")
}

// handleUpdateCheck — POST /api/settings/updates/check. Operator-initiated egress
// (spec 034; mirrors `go-rag upgrade`). Never auto-fires (SC-003). Offline or
// unreachable ⇒ latest="unknown", never an error (FR-008).
func (s *Server) handleUpdateCheck(w http.ResponseWriter, _ *http.Request) {
	current := s.version
	latest, err := upgrade.LatestVersion(current)
	if err != nil {
		latest = "unknown"
	}
	writeJSON(w, http.StatusOK, updateCheckDTO{
		Current:        current,
		Latest:         latest,
		NewerAvailable: upgrade.NewerVersionAvailable(current, latest),
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
	})
}
