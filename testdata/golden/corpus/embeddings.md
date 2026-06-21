# Embeddings and Ollama

go-rag turns text into dense vectors using a local Ollama embedding model. The
embedder is an interface with a single provider today: it posts batches of text
to the Ollama /api/embed endpoint and receives back one fixed-length float vector
per input. The provider retries transient failures with exponential backoff and
fails fast on permanent errors, so a flaky model server degrades gracefully
rather than corrupting the index.

Vectors are stored alongside the chunk they describe, tagged with the model name
and dimensionality that produced them. That provenance is what makes model
migration safe: the system can tell which chunks were embedded under an old model
and re-embed only those. Swapping the embedding model is therefore a deliberate,
scoped operation rather than a silent change that leaves half the index
incomparable to new queries.
