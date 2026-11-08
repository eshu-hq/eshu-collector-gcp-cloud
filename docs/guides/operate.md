# Operate

## How it gets run

The host runs one claim per process invocation. It starts
`./collector --sdk-stdio`, writes one `collector-sdk/v1alpha1`
request to stdin, and reads one result from stdout. The host owns
admission, identity, storage, and recovery. This binary never
commits facts itself.

```bash
go run ./cmd/collector --sdk-stdio < request.json > result.json
```

Without `--sdk-stdio`, the binary collects one fixture file claim.
Fixture and unit tests make no live Google Cloud call.

Installing it first? See [Seed and install](seed-and-install.md).

## What to watch

Each run writes one health line to stderr with liveness, last
success time and digest, failure count and reason, retry depth,
and the effective limits. Health text carries no credentials or
payloads. The manifest declares the metric prefix
`eshu_dp_gcp_cloud_`; the stderr health line is the operator
signal proven by the code here.

## Dependencies

| Dependency | Version or contract | Where it's pinned |
|---|---|---|
| `github.com/eshu-hq/eshu-sdk/collector` | `v0.2.0-rc.2` | `go.mod` |
| `github.com/eshu-hq/eshu-sdk/factschema` | `v0.1.0-rc.12` | `go.mod` |
| Host protocol | `collector-sdk/v1alpha1`, `process` adapter | [manifest.yaml](../../manifest.yaml) |
| Eshu core compatibility | `>=0.0.5 <0.2.0` | [manifest.yaml](../../manifest.yaml) |
| Producer identity | `collector-gcp-cloud`, version `0.1.4` | [manifest.yaml](../../manifest.yaml) |
