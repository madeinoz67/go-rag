# Local-First Security

go-rag is local-first by construction: it runs as one statically linked binary,
keeps every document and vector on the local disk, and talks only to a loopback
Ollama for embeddings. There is no cloud service, no account, and no telemetry
egress for any core operation. The network surface is the loopback address where
the server binds, which keeps the database off the network by default.

A single-instance guard, built from a PID file and the Pebble file lock, stops a
second process from opening the same vault and racing on writes. When a bearer
token is configured, requests without it are rejected as unauthenticated, so the
default-open development mode is an explicit choice rather than an accident. The
boundary to watch is binding off loopback: that flips several guarantees and
should be deliberate.
