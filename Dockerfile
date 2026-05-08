# Multi-stage build for the SMS Mock Server.
# Produces a static, distroless image (~10-15 MB target).

# Stage 1: build the static binary
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Pre-fetch deps for caching: copy go.mod/go.sum first.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source. .dockerignore excludes tests/, docs/, etc.
COPY . .

# Build flags:
#   CGO_ENABLED=0 → fully static, no glibc dependency, runs on distroless/static
#   -ldflags -s -w → strip symbol & debug info (smaller binary)
#   -ldflags -X .../httpapi.version=$VERSION → embed git tag/commit if provided
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w -X github.com/notfoundsam/sms-mock-server/app/httpapi.version=${VERSION}" \
      -o /out/sms-mock-server \
      ./app

# Stage 2: distroless static runtime image.
# distroless/static contains glibc-free CA certs + tzdata + nonroot user — nothing else.
FROM gcr.io/distroless/static:nonroot

WORKDIR /app

# Binary
COPY --from=builder /out/sms-mock-server /app/sms-mock-server

# Default config (overridable via volume mount in docker-compose).
COPY --chown=nonroot:nonroot config.yaml /app/config.yaml

# Data directory (SQLite DB) — persisted via volume.
# distroless/static doesn't have `mkdir`; the dir comes from the base image's
# nonroot home or is created on first DB open.

EXPOSE 8080

ENV LOG_LEVEL=INFO

USER nonroot:nonroot

ENTRYPOINT ["/app/sms-mock-server"]
CMD ["-config", "/app/config.yaml"]
