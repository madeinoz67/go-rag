# Data Model — Settings: System & Transports (Slice 1, spec 056)

> No new persisted entity. Slice 1 is a read-only projection of daemon/process/
> transport/schema state + one operator-initiated update-check. Defines the two
> transfer objects the new endpoints return and the existing symbols reused.

## Source symbols (read-only reuse)

| Source | Location | Provides |
|---|---|---|
| `daemon.Status(dbPath)` / `ReadPID` / `ReadAddrs` | internal/daemon/lifecycle.go, pid.go | PID + bind addresses |
| `daemon.IsLoopbackBind` / `NonLoopbackBinds` / `ExternalBindWarning` | internal/daemon/bind.go | loopback/external posture (spec 007) |
| `migrate.readVersion(db)` / `migrate.ExpectedVersion` | internal/storage/migrate/migrate.go | on-disk + expected schema version |
| `eng.Config()` (MCPAddr/RESTAddr/gRPCAddr/UIAddr) | internal/config | configured transport addresses |
| `upgrade.LatestVersion` / `NewerVersionAvailable` | internal/upgrade/release.go, semver.go | update availability (operator-initiated) |
| UI Server `version` field + `startedAt` | internal/ui (NEW, UI-layer only) | binary version + uptime |

## Transfer object: `systemStatusDTO` — `GET /api/settings/system`

```
systemStatusDTO {
  version:        string     // plumbed binary version (main.version)
  pid:            int        // daemon PID (pidfile)
  uptime_seconds: int        // now - startedAt (serve-process age)
  schema: {
    on_disk:     int         // migrate.readVersion(db)
    expected:    int         // migrate.ExpectedVersion (binary)
    unified_store: bool      // spec 052 single-store posture (fixed true)
  }
  transports: [             // one entry per transport
    { kind: "mcp"|"rest"|"grpc"|"ui", address: string, loopback: bool, state: "listening"|"disabled" }
  ]
  bind_warning: string       // daemon.ExternalBindWarning ("" when all loopback)
}
```

## Transfer object: `updateCheckDTO` — `POST /api/settings/updates/check`

Operator-initiated (egress). Returns:

```
updateCheckDTO {
  current:          string   // the plumbed binary version
  latest:           string   // upgrade.LatestVersion(); "unknown" if unreachable
  newer_available:  bool     // upgrade.NewerVersionAvailable(current, latest)
  checked_at:       string   // RFC3339 timestamp of the check
}
```

## Validation rules (from spec)

- All `GET /api/settings/system` values reflect the running daemon (FR-006) — no egress.
- `POST /api/settings/updates/check` runs ONLY on explicit operator action (FR-004 / SC-003) — never on view load.
- Graceful degradation: unreachable release source ⇒ `latest="unknown"`, `newer_available=false` (FR-008); never an error.
- Read-only except the explicit check action (FR-005); neither endpoint mutates config or storage.
