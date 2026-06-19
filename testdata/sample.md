---
title: RAG Architecture Notes
author: Test Author
---

# RAG Architecture

Retrieval-Augmented Generation combines retrieval with generation.

## Hybrid Retrieval

The system fuses vector similarity and keyword search via Reciprocal Rank Fusion.
Vector search captures semantic meaning; keyword search captures exact terms.

## Chunking

Documents are split into overlapping chunks before embedding. Default size is five
hundred and twelve tokens with fifty tokens of overlap.
