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

# Create /data owned by the nonroot user (UID 65532) BEFORE the VOLUME declaration.
# A Docker named volume mounts root-owned by default; but on FIRST mount Docker
# copies the image's content+ownership at the mount path into the volume — so an
# image-owned /data (65532) makes the named volume writable by nonroot without any
# chown/root/busybox init step (spec 033, FR-005 nonroot preserved).
WORKDIR /data

# Documents the three transports (spec 003). EXPOSE is informational; actual
# host reachability is governed by compose `ports:` (loopback by default).
EXPOSE 7878 7879 7880

# The Pebble vault. Declared so a bare `docker run` (without compose) gets an
# anonymous volume rather than writes landing in the ephemeral container layer;
# compose overrides this with the named go-rag-data volume. Single-writer.
VOLUME /data

# Binary at /usr/local/bin/go-rag (a PATH dir in distroless) so the bare name
# `go-rag` resolves for `docker compose exec`, the HEALTHCHECK, and `docker run`.
COPY --from=builder /out/go-rag /usr/local/bin/go-rag

# ENTRYPOINT is the binary (bare name resolves via PATH); CMD runs the FOREGROUND
# daemon (spec 033). `serve` (not `start`, which detaches and would exit PID 1) is
# correct for a container. The 0.0.0.0 addrs + --bind-external are mandatory: the
# daemon refuses non-loopback binds without --bind-external (spec 007), and the
# loopback defaults would be unreachable through a host port mapping. Override
# CMD to run another subcommand, e.g. `docker run ... ghcr.io/.../go-rag version`.
ENTRYPOINT ["go-rag"]
CMD ["serve", "--db-path", "/data", "--mcp-addr", "0.0.0.0:7878", "--rest-addr", "0.0.0.0:7879", "--grpc-addr", "0.0.0.0:7880", "--bind-external"]

# Exec-array HEALTHCHECK — the ONLY form that works on distroless/static (no
# /bin/sh, no curl). `go-rag health` (bare name via PATH) probes
# 127.0.0.1:7878/mcp/health over loopback INSIDE the container (reachable
# regardless of --bind-external or the host port mapping) and exits 0 on HTTP 200.
# start-period covers cold-vault boot + model-bundle init; do not lower below
# ~10s or cold start flaps unhealthy.
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 \
  CMD ["go-rag", "health"]

# USER nonroot (UID 65532) is inherited from the distroless base image — no change.
