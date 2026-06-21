# Embedding Model Migration

When the configured embedding model changes, go-rag can migrate the index so
every chunk is comparable to new queries again. Migration counts how many stored
embeddings were produced by a model different from the current one and re-embeds
exactly those chunks, leaving the rest untouched. Because document identity is
separate from embedding identity, re-embedding never creates duplicate documents.

The migration command is the safe path to switch models: it reports how many
chunks are stale and how many errors occurred, and it can be re-run until the
index is fully on the new model. This is also when retrieval quality should be
re-measured, since a different embedding space can quietly help or hurt recall
in ways that only an evaluation harness can surface.
