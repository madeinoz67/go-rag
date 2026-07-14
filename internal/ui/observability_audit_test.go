package ui

// observability_audit_test.go (spec 054) proves the audit-log browser (US2):
// the projection is hash-only (no query plaintext — the load-bearing privacy
// claim, FR-003), newest-first, and bounded; the handler filters by type and
// agrees with Engine.AuditRead (parity), and renders healthy states for an
// empty/disabled log.
//
// Audit is PROCESS-WIDE: Engine.AuditRead ignores the vault arg and reads a
// single log at the unified-store root (spec 052). These tests therefore seed
// the log directly via writeBridgeAudit (the established helper) rather than
// relying on engine.Add to emit audit — mirroring bridgeops_test.go.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
)

// TestToAuditEventDTOs_HashOnlyNoPlaintext: a query event projects to a DTO
// carrying the hash and NEVER the raw query. The source audit.Event has no
// plaintext field by construction; this test pins that the projection does not
// introduce one (FR-003).
func TestToAuditEventDTOs_HashOnlyNoPlaintext(t *testing.T) {
	const query = "super-secret-query-with-PII"
	events := []audit.Event{
		audit.QueryEvent(query, "hybrid", 5, 3, nil),
		audit.IngestEvent("add", "/path/doc.txt", 2, 0, 0, nil),
		audit.AuthFailEvent("rest", "bad token"),
	}
	dtos := toAuditEventDTOs(events)
	if len(dtos) != 3 {
		t.Fatalf("got %d DTOs, want 3", len(dtos))
	}
	// newest-first: the last event (auth-fail) is first.
	if dtos[0].Type != audit.TypeAuthFail {
		t.Errorf("first DTO type = %q, want auth-fail (newest-first)", dtos[0].Type)
	}
	// the raw query string must not appear on ANY projected row.
	for _, d := range dtos {
		b, _ := json.Marshal(d)
		if strings.Contains(string(b), query) {
			t.Errorf("plaintext query leaked into DTO: %s", b)
		}
		if d.Type == audit.TypeQuery {
			if d.QueryHash == "" {
				t.Error("query DTO must carry a non-empty query_hash")
			}
			if d.Mode != "hybrid" || d.K != 5 || d.Hits != 3 {
				t.Errorf("query DTO fields = %+v, want mode=hybrid k=5 hits=3", d)
			}
		}
	}
}

// TestToAuditEventDTOs_EmptyIsNilSafe: empty input yields a non-nil slice so
// JSON encodes `[]`, never `null`.
func TestToAuditEventDTOs_EmptyIsNilSafe(t *testing.T) {
	dtos := toAuditEventDTOs(nil)
	if dtos == nil || len(dtos) != 0 {
		t.Errorf("empty projection = %v, want non-nil empty slice", dtos)
	}
}

// TestObservabilityAudit_FilterByTypeAndParity: with a seeded log, ?type=ingest
// returns only ingest rows, ?type=query only query rows, and the UI response
// matches Engine.AuditRead for the same options (parity, FR-009/SC-003).
func TestObservabilityAudit_FilterByTypeAndParity(t *testing.T) {
	eng := newWriteTestEngine(t)
	now := time.Now().UTC()
	writeBridgeAudit(t, eng,
		audit.Event{TS: now, Type: audit.TypeIngest, Op: "add", Path: "/d/a.txt", New: 2, Status: "ok"},
		audit.Event{TS: now, Type: audit.TypeQuery, QueryHash: audit.QueryHash("secret"), Mode: "hybrid", K: 5, Hits: 3, Status: "ok"},
		audit.Event{TS: now, Type: audit.TypeAuthFail, Transport: "rest", Detail: "bad token"},
	)
	srvURL, tok := authedDocServer(t, eng)

	// type=ingest → exactly the one ingest row.
	resp := bearerGet(t, srvURL+"/api/observability/audit?type=ingest", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit ingest: status %d, want 200", resp.StatusCode)
	}
	var page auditPageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].Type != audit.TypeIngest {
		t.Fatalf("type=ingest → got %+v, want 1 ingest event", page.Events)
	}

	// type=query → exactly the one query row (hash only, no plaintext).
	resp2 := bearerGet(t, srvURL+"/api/observability/audit?type=query", tok)
	defer resp2.Body.Close()
	var page2 auditPageResponse
	_ = json.NewDecoder(resp2.Body).Decode(&page2)
	if len(page2.Events) != 1 || page2.Events[0].QueryHash == "" {
		t.Fatalf("type=query → got %+v, want 1 hashed query event", page2.Events)
	}

	// parity: UI event count == Engine.AuditRead for type=ingest.
	direct, err := eng.AuditRead("default", audit.ReadOptions{Type: audit.TypeIngest})
	if err != nil {
		t.Fatalf("Engine.AuditRead: %v", err)
	}
	if len(direct) != len(page.Events) {
		t.Errorf("parity: UI=%d events, engine=%d", len(page.Events), len(direct))
	}
}

// TestObservabilityAudit_DefaultReturnsAll: no type filter → every event type
// appears, newest-first.
func TestObservabilityAudit_DefaultReturnsAll(t *testing.T) {
	eng := newWriteTestEngine(t)
	now := time.Now().UTC()
	writeBridgeAudit(t, eng,
		audit.Event{TS: now, Type: audit.TypeIngest, Op: "add", Path: "/d/a.txt", New: 1, Status: "ok"},
		audit.Event{TS: now.Add(time.Second), Type: audit.TypeQuery, QueryHash: "h", Mode: "keyword", K: 3, Hits: 1, Status: "ok"},
	)
	srvURL, tok := authedDocServer(t, eng)
	resp := bearerGet(t, srvURL+"/api/observability/audit", tok)
	defer resp.Body.Close()
	var page auditPageResponse
	_ = json.NewDecoder(resp.Body).Decode(&page)
	if len(page.Events) != 2 {
		t.Fatalf("no filter → got %d events, want 2", len(page.Events))
	}
	// newest-first: the query (later TS) is first.
	if page.Events[0].Type != audit.TypeQuery {
		t.Errorf("newest-first: first = %q, want query", page.Events[0].Type)
	}
}

// TestObservabilityAudit_InvalidType400: an unknown type is rejected.
func TestObservabilityAudit_InvalidType400(t *testing.T) {
	eng := newWriteTestEngine(t)
	srvURL, tok := authedDocServer(t, eng)
	resp := bearerGet(t, srvURL+"/api/observability/audit?type=bogus", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid type: status %d, want 400", resp.StatusCode)
	}
}

// TestObservabilityAudit_EmptyHealthy: a fresh vault with no audit log returns
// a healthy empty page (events:[], not an error).
func TestObservabilityAudit_EmptyHealthy(t *testing.T) {
	eng := newWriteTestEngine(t)
	srvURL, tok := authedDocServer(t, eng)
	resp := bearerGet(t, srvURL+"/api/observability/audit", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit empty: status %d, want 200", resp.StatusCode)
	}
	var page auditPageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Events == nil {
		t.Error("empty audit must encode events:[] not null")
	}
}

// TestObservabilityAudit_401Unguarded: no Bearer → 401 (US4 / FR-006).
func TestObservabilityAudit_401Unguarded(t *testing.T) {
	eng := newWriteTestEngine(t)
	srvURL, _ := authedDocServer(t, eng)
	resp := bearerGet(t, srvURL+"/api/observability/audit", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("audit without bearer: status %d, want 401", resp.StatusCode)
	}
}
