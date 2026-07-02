package grpc

import (
	"testing"

	"github.com/madeinoz67/go-rag/internal/events"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// TestToEventProto_ReingestedCarriesDeltas (spec 043 / BL-010, T012): the gRPC
// projection of a RE_INGESTED event carries the chunk_deltas field with the
// correct change types + chunk IDs. Validates toEventProto + toChunkDeltasPB +
// toChangeTypePB.
func TestToEventProto_ReingestedCarriesDeltas(t *testing.T) {
	ev := events.DocumentEvent{
		Type:       events.EventReingested,
		DocumentID: "doc-1",
		Deltas: []events.ChunkDelta{
			{Change: events.ChangeUnchanged, NewChunkID: "n1", PrevChunkID: "o1"},
			{Change: events.ChangeAdded, NewChunkID: "n2"},
			{Change: events.ChangeRemoved, PrevChunkID: "o3"},
		},
	}
	pb := toEventProto(ev)

	if pb.GetType() != goragpb.DocumentEventType_RE_INGESTED {
		t.Fatalf("type = %v, want RE_INGESTED", pb.GetType())
	}
	if len(pb.GetChunkDeltas()) != 3 {
		t.Fatalf("chunk_deltas len = %d, want 3", len(pb.GetChunkDeltas()))
	}

	// UNCHANGED: both IDs populated.
	d0 := pb.GetChunkDeltas()[0]
	if d0.GetChangeType() != goragpb.ChunkDelta_UNCHANGED {
		t.Errorf("delta[0] type = %v, want UNCHANGED", d0.GetChangeType())
	}
	if d0.GetChunkId() != "n1" || d0.GetPrevChunkId() != "o1" {
		t.Errorf("delta[0] ids = %q/%q, want n1/o1", d0.GetChunkId(), d0.GetPrevChunkId())
	}

	// ADDED: only new ID.
	d1 := pb.GetChunkDeltas()[1]
	if d1.GetChangeType() != goragpb.ChunkDelta_ADDED {
		t.Errorf("delta[1] type = %v, want ADDED", d1.GetChangeType())
	}
	if d1.GetChunkId() != "n2" {
		t.Errorf("delta[1] chunk_id = %q, want n2", d1.GetChunkId())
	}

	// REMOVED: only prev ID.
	d2 := pb.GetChunkDeltas()[2]
	if d2.GetChangeType() != goragpb.ChunkDelta_REMOVED {
		t.Errorf("delta[2] type = %v, want REMOVED", d2.GetChangeType())
	}
	if d2.GetPrevChunkId() != "o3" {
		t.Errorf("delta[2] prev_chunk_id = %q, want o3", d2.GetPrevChunkId())
	}
}

// TestToEventProto_NonReingestedHasNoDeltas: INGESTED/EMBEDDED/DELETED events
// must NOT carry chunk_deltas (the field is RE_INGESTED-only).
func TestToEventProto_NonReingestedHasNoDeltas(t *testing.T) {
	for _, typ := range []events.DocumentEventType{events.EventIngested, events.EventEmbedded, events.EventDeleted} {
		pb := toEventProto(events.DocumentEvent{Type: typ, DocumentID: "d"})
		if len(pb.GetChunkDeltas()) != 0 {
			t.Errorf("type %v: chunk_deltas len = %d, want 0 (RE_INGESTED-only field)", typ, len(pb.GetChunkDeltas()))
		}
	}
}
