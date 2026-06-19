// Package watcher implements two-layer change detection (PRD §7):
//
//  1. fsnotify — real-time OS events (<100ms), does not survive restarts.
//  2. Periodic polling — stat() + SHA-256 comparison (every 60s), ground truth.
//
// SHA-256 content addressing is the final arbiter (PRD §7.2): identical content
// = identical hash, no false positives. TODO(later): implement.
package watcher

import "context"

// ChangeDetector runs the two-layer change-detection loop (PRD §7.1). Stub.
type ChangeDetector struct{}

// Watch blocks until ctx is cancelled, processing filesystem + polling events.
// Stub.
func (cd *ChangeDetector) Watch(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
