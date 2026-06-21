# Hybrid Retrieval

A query in go-rag is answered by hybrid retrieval: a lexical leg and a semantic
leg run in parallel and their rankings are fused. The lexical leg is a BM25 full
-text index that scores chunks by term frequency and inverse document frequency,
so it excels at exact keyword and acronym matches. The semantic leg compares the
query vector against stored chunk vectors by cosine similarity, so it catches
paraphrase and synonym where the words differ but the meaning aligns.

The two ranked lists are merged with reciprocal rank fusion, a rank-based formula
that rewards items appearing near the top of either list. An optional cross
-encoder reranker then re-scores a pool of the fused candidates for higher
precision before the final top-k are returned. Three modes expose the legs
separately or together: hybrid (the default), semantic, and keyword.
