# Contributing

## CI lanes

Every pull request runs `.github/workflows/ci.yml`, a thin caller over the
org reusable (`eshu-hq/.github` `go-collector-ci.yml` v2, pinned by peeled
commit SHA). A `changes` job selects lanes by path; `ci-complete` is the only
required check and passes when every needed lane succeeds and the rest were
legitimately skipped.

| PR changes | Lanes that run |
|---|---|
| Go sources, `go.mod`, `go.sum` | `lint`, `test`, `race` (+ coverage), `boundary`, `gosec` |
| `go.mod` / `go.sum` / `.golangci.yml` | above plus `govulncheck` |
| `**/testdata/**`, `**/*.json` fixtures | `test` only |
| `**/*.sh` | `shell` only |
| `.github/workflows/**` | `actionlint` plus the Go lanes |
| Docs only (`*.md`, `LICENSE`, `CODEOWNERS`) | no lanes |

The `boundary` lane proves the vacuous image case (no `Dockerfile` or `cmd/`
exists; it fails the day either appears, until a real image lane replaces
it). `actionlint` runs only on workflow changes.

Pushes to `main` run every lane. A weekly schedule runs the `govulncheck` and
`gosec` sweeps; findings open or update an issue.

## Local checks

`make ci` runs the `lint`, `test`, and `race` lane commands. The lint lane is
`go vet`, `golangci-lint run` with the committed `.golangci.yml` (org set:
govet, staticcheck, ineffassign, unused, errcheck; gofumpt formatting —
golangci-lint v2 folds gosimple's S-checks into staticcheck, proven by an
S1002 probe),
`go mod tidy` drift, and the 500-line cap on `.go` files (plain `wc -l`, no
allowlist). Coverage must stay at or above `COVERAGE_FLOOR=90` percent of
statements; the floor only rises.
