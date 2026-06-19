// Package pipeline orchestrates ingestion (PRD §4.4):
//
//	Read -> Split -> Hash -> Dedup -> Store(sync) -> ACK -> [Embed/Index async]
//
// The async-after-ACK write model keeps the write path under 10ms (PRD §10.1);
// all embedding and indexing work happens on background workers after the user
// is acknowledged. TODO(later): implement.
package pipeline

import "context"

// Pipeline runs the ingest pipeline over a path. Stub.
type Pipeline struct{}

// Run ingests the given path. Stub.
func (p *Pipeline) Run(ctx context.Context, path string) error {
	_ = ctx
	_ = path
	return nil
}
