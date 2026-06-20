# Quickstart: Document Vaults

**Phase**: 1 | **Date**: 2026-06-20

End-to-end validation for the vault MVP (US1: create + use a vault).

## Prerequisites

- Go-rag binary built (`make build`)
- Ollama running with at least one embedding model

## Steps

### 1. Create two vaults with different models
```bash
./bin/go-rag vault create cyber-notes --embedding_model mxbai-embed-large
./bin/go-rag vault create personal --embedding_model nomic-embed-text
```
**Expected**: Both vaults created successfully with their respective configs.

### 2. List vaults
```bash
./bin/go-rag vault list
```
**Expected**: Both vaults appear with 0 docs, their embedding models, "stopped" daemon status.

### 3. Ingest into cyber-notes only
```bash
echo "honeypot deception in cybersecurity" > /tmp/cyber.txt
./bin/go-rag --vault cyber-notes add /tmp/cyber.txt
echo "my grandmother's recipe for honey cake" > /tmp/recipe.txt
./bin/go-rag --vault personal add /tmp/recipe.txt
```
**Expected**: Each vault shows 1 document.

### 4. Verify isolation — query each vault
```bash
./bin/go-rag --vault cyber-notes query "honeypot"
./bin/go-rag --vault personal query "honeypot"
```
**Expected**: cyber-notes returns the cybersecurity doc; personal returns the recipe (or no results if the embedding doesn't match). **Critically: neither vault returns the other's documents.**

### 5. Dashboard shows active vault
```bash
./bin/go-rag --vault cyber-notes
```
**Expected**: Dashboard panel shows "Vault: cyber-notes" with the vault's stats.

### 6. Backward compat — no vault flag
```bash
./bin/go-rag init
./bin/go-rag status
```
**Expected**: Works exactly as before (uses the default `./.go-rag` path).

## Success = Acceptance

This quickstart passes iff spec acceptance scenarios US1.1–US1.4 hold: create works, add works, query returns vault-scoped results, and cross-vault isolation is verified.
