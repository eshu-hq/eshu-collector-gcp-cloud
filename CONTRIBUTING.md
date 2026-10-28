# Contributing

## CI lanes

Every pull request runs `.github/workflows/ci.yml`. A `changes` job selects
lanes by path; `ci-complete` is the only required check and passes when every
needed lane succeeds and the rest were legitimately skipped.

| PR changes | Lanes that run |
|---|---|
| Go sources, `go.mod`, `go.sum` | `lint`, `test`, `race` (+ coverage), `image` proof, `gosec` |
| `go.mod` / `go.sum` / `.golangci.yml` | above plus `govulncheck` |
| `**/testdata/**`, `**/*.json` fixtures | `test` only |
| `**/*.sh` | `shell` only |
| `.github/workflows/**` | `actionlint` plus the Go lanes |
| Docs only (`*.md`, `LICENSE`, `CODEOWNERS`) | no lanes |

Pushes to `main` run every lane. A weekly schedule runs the `govulncheck` and
`gosec` sweeps; findings open or update an issue.

## Local checks

`make ci` runs the `lint`, `test`, and `race` lane commands. The lint lane is
`go vet`, `golangci-lint run` with the committed `.golangci.yml` (org set:
govet, staticcheck, ineffassign, unused, errcheck; gofumpt formatting),
`go mod tidy` drift, and the 500-line cap on `.go` files (plain `wc -l`, no
allowlist). Coverage must stay at or above `COVERAGE_FLOOR=90` percent of
statements; the floor only rises.
