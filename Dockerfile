# Standalone GCP Cloud collector image: pure-Go binary plus manifest and
# fixture, built reproducibly with -trimpath. Module path is github.com/eshu-hq/eshu-collector-gcp-cloud; keep the
# COPY roots when this directory becomes a new repository root.
FROM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# GKE nodes are linux/amd64; pin GOARCH so builds on any host (e.g. arm64
# dev boxes) produce a runnable binary.
RUN CGO_ENABLED=0 GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/collector ./cmd/collector

# Base digests: both resolved 2026-11-02 with `crane digest` against the
# registry (Go base index digest, multi-arch; the builder selects amd64).
# Re-pin with scripts/pin-digest.sh flow when bumping either base, and keep
# the Go base in sync with go.mod.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/collector /usr/local/bin/collector
COPY manifest.yaml /etc/collector/manifest.yaml
COPY testdata/complete.json /var/lib/collector/complete.json
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/collector", "--sdk-stdio"]
