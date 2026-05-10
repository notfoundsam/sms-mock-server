# Dockerfile used by `docker compose up --build` for local development.
# Compiles the binary from source and runs it on distroless/static.
#
# For release images, see Dockerfile.release — it expects a pre-built binary
# in the build context and is what goreleaser uses.

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sms-mock-server ./app

# Pre-create /data in the builder so the final stage can copy it in with
# `nonroot` ownership. Distroless has no shell, so we can't `mkdir` or
# `chown` there — the directory has to come from somewhere that does.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY --from=builder /out/sms-mock-server /app/sms-mock-server
# /data is owned by `nonroot` (UID 65532). When a Docker volume mounts
# here for the first time, Docker copies these permissions onto the
# fresh volume — so `SMS_MOCK_DB_PATH=/data/...` works without any
# host-side chown dance.
COPY --from=builder --chown=nonroot:nonroot /out/data /data

EXPOSE 8080

ENV LOG_LEVEL=INFO

USER nonroot:nonroot

ENTRYPOINT ["/app/sms-mock-server"]
