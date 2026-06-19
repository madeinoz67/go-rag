package storage

import "testing"

// BenchmarkSyncWrite measures the durable (Sync) write latency that underpins the
// <10ms write-ACK budget (Principle IV / PRD §10.1).
func BenchmarkSyncWrite(b *testing.B) {
	db, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	key := []byte("bench-doc")
	val := make([]byte, 256)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.SetWithPrefix(PrefixDocument, key, val); err != nil {
			b.Fatal(err)
		}
	}
}
