# Storage with Pebble

All go-rag state lives in a single embedded Pebble key-value store. Pebble is a
pure-Go log-structured merge tree with a write-ahead log, so every accepted write
is durable on disk after a sync and the database recovers cleanly from a hard
crash. There is exactly one database instance per vault; its keyspace is
partitioned by single-byte prefixes, one per record type: sources, documents,
chunks, embeddings, and several secondary indexes.

A file lock ensures only one process opens a vault for writing at a time, which
prevents the corruption that comes from two writers racing on the same keys.
Because documents and chunks are content-addressed by SHA-256, the store is
idempotent: ingesting the same file twice is a no-op, and the vector index can be
rebuilt from the persisted chunks if it is ever lost.
