# go-rag

A single-binary local RAG (Retrieval-Augmented Generation) database — ingest,
index, and query the documents on your filesystem with zero external dependencies
beyond a local [Ollama](https://ollama.com) instance for embeddings.

!!! info "Specification"
    The authoritative product spec lives in
    [`PRD_RAG_Database.md`](https://github.com/madeinoz67/go-rag/blob/main/PRD_RAG_Database.md).
    This documentation site covers usage and design; the PRD covers behavior.

## Why

A local RAG database should be as frictionless as `git init; git add; git commit` —
no Docker, no API keys, no cloud services. Install the binary, run `go-rag init`,
and you have a working RAG system.

## Quickstart

```bash
make build            # build the static binary into ./bin
./bin/go-rag --help   # list commands
```

## Commands

| Command | Description |
|---------|-------------|
| `init` | Initialize a new RAG database |
| `add <path>` | Add files or directories |
| `scan [--watch]` | Scan for changes |
| `query "<text>"` | Hybrid semantic + keyword search |
| `status` | Database statistics and health |
| `config` | View or change configuration |
