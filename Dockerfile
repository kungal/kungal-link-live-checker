# Single static Go binary on distroless. Build context = repo root.
# Mirrors kun-galgame-infra/docker/go.Dockerfile conventions (CGO off, trimmed,
# distroless/static:nonroot, `healthcheck` subcommand for HEALTHCHECK).
ARG GO_VERSION=1.25

# ---- build ----
FROM golang:${GO_VERSION}-trixie AS build
WORKDIR /src
# go.mod first → module layer cached until it changes. This service has zero
# external deps, so there is no go.sum and `go mod download` is a no-op.
COPY go.mod ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/app ./cmd/server

# ---- run ----
# distroless/static: ~2MB, no shell, nonroot. Bundles ca-certificates, which the
# checker needs for outbound HTTPS to the netdisk APIs.
FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.source="https://github.com/kungal/kungal-link-live-checker"
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 6734
# distroless has no curl/wget — the binary probes its own /healthz.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
        CMD ["/app", "healthcheck"]
ENTRYPOINT ["/app"]
