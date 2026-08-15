# =============================================================================
# Multi-stage build for the Go API.
#
# Stage 1 compiles with the full Go toolchain; stage 2 keeps only the resulting
# binary. The final image is a few megabytes instead of ~900MB, and it contains no
# compiler, shell or package manager for an attacker to use.
# =============================================================================

# ---- Stage 1: build ---------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Copy the manifests first and download dependencies as their own layer. Docker
# caches layers by content, so editing a .go file no longer re-downloads every
# module -- only the final COPY and build layers are invalidated.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a statically linked binary with no libc dependency,
# which is what lets it run in a scratch/alpine image.
# -ldflags="-s -w" strips the symbol table and DWARF debug info (smaller binary).
# -trimpath removes local filesystem paths from the build, making it reproducible.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/server \
    ./cmd/server

# ---- Stage 2: runtime -------------------------------------------------------
FROM alpine:3.22 AS runtime

# ca-certificates for outbound TLS (e.g. a managed Postgres over sslmode=require);
# tzdata so time.Local and timestamp formatting resolve real timezones.
RUN apk add --no-cache ca-certificates tzdata

# Run as a non-root user. If the process is ever compromised, it has no write
# access to the image and no privileged operations available to it.
RUN adduser -D -u 10001 appuser

COPY --from=build /out/server /usr/local/bin/server

USER appuser

ENV APP_ENV=production \
    PORT=8080

EXPOSE 8080

# wget comes with busybox in alpine, so the healthcheck needs no extra package.
# The API's /api/health endpoint pings Postgres, so this reports the service
# unhealthy when its database is unreachable -- not merely when it has crashed.
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/api/health || exit 1

ENTRYPOINT ["/usr/local/bin/server"]
