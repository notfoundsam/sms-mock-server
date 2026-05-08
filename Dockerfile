# Release image used by goreleaser. The binary is built outside this
# Dockerfile (per-arch) and placed in the build context alongside config.yaml.
#
# For local dev, run the Go binary directly via `make run` — no Docker build
# is needed. To produce a local image for testing, use:
#   goreleaser release --snapshot --clean
FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY sms-mock-server /app/sms-mock-server
COPY --chown=nonroot:nonroot config.yaml /app/config.yaml

EXPOSE 8080

ENV LOG_LEVEL=INFO

USER nonroot:nonroot

ENTRYPOINT ["/app/sms-mock-server"]
CMD ["-config", "/app/config.yaml"]
