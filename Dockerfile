# syntax=docker/dockerfile:1
# Multi-stage build — go-rag is a static, CGO-free binary (PRD §9.5).

# ---- build stage ----
FROM golang:1.26-alpine AS builder
WORKDIR /src

# Cache deps: copy manifests first (go.sum may be absent on first build).
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/go-rag ./cmd/go-rag

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/go-rag /go-rag
ENTRYPOINT ["/go-rag"]
