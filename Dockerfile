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

FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY --from=builder /out/sms-mock-server /app/sms-mock-server

EXPOSE 8080

ENV LOG_LEVEL=INFO

USER nonroot:nonroot

ENTRYPOINT ["/app/sms-mock-server"]
