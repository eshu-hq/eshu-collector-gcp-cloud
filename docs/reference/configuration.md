# Configuration

There's no `config.example.*` file in this repo. The host request
`config` object is the configuration. A request with `source.input`
takes the fixture-file path; a request without it must carry the
live fields below.

| Name | Required | Default | What it does |
|---|---|---|---|
| `collector_instance_id` | Yes for live | None | Instance that owns scope policy |
| `scopes` | Yes for live, at least one | None | Bounded Google Cloud scopes this instance may scan |
| `scopes[].parent_scope_kind` | Yes | None | `organization`, `folder`, or `project` |
| `scopes[].parent_scope_id` | Yes | None | Organization number, folder number, or project ID |
| `scopes[].credential_ref` | Yes | None | Name of the read-only credential; never a secret value |
| `scopes[].asset_type_family` | No | `mixed` | Asset family shard for the scope |
| `scopes[].content_family` | No | `resource` | CAI content family shard |
| `scopes[].location_bucket` | No | `global` | Location shard |
| `source.input` | No | Flag default `testdata/complete.json` | Fixture file to read instead of Google Cloud |
| `source.sourceURI` | No | `https://example.invalid/source/gcp-cloud` | Credential-free source address recorded on facts |
| `source.redactionKeyEnv` | Yes for live | Must be `ESHU_GCP_REDACTION_KEY` | Name of the env var holding redaction key material |
| `freshness.previousDigest` | No | Empty | Prior digest; an unchanged scan returns a fact-free `unchanged` result |
| `limits.maxRecordsPerClaim` | No | `5000` | Maximum facts in one claim |
| `limits.maxPayloadBytes` | No | `65536` | Maximum bytes in one fact payload |
| `limits.claimTimeoutSeconds` | No | `300` | Maximum wall-clock seconds for one claim |
| `limits.maxPageBytes` | No | `4128768` (4 MiB minus 64 KiB headroom) | Maximum bytes in the one result document |

Local flags mirror these: `--input`, `--source-uri`,
`--parent-scope-kind`, `--parent-scope-id`, `--previous-digest`,
`--max-records`, `--max-payload-bytes`, `--claim-timeout-seconds`,
and `--max-page-bytes`. Zero selects the default; negative values
fail.

Want to see it run without a Google Cloud account? Point it at the
checked-in fixture:

```bash
go run ./cmd/collector --input testdata/complete.json
```

## Credentials and least privilege

Config names a credential with `credential_ref`; it never holds a
secret value. Live reads use Application Default Credentials (ADC,
Google Cloud's standard credential lookup) and only GET
`assets.list` calls, plus opt-in read-only tag listing calls when a
scope enables them in code. The OAuth scope requested in code is
`https://www.googleapis.com/auth/cloud-platform`, which
`assets.list` accepts. The collector itself only reads; it never
calls a write API.

This repo doesn't name a specific IAM role, so grant the named
credential only the read access the claimed scope needs and stop
there.

Redaction key material comes only from the `ESHU_GCP_REDACTION_KEY`
environment variable; config names that variable, never its value.
One claim uses one ADC identity, so scopes naming different
`credential_ref` values return a terminal result.

## Claim budget

One claim is bounded by the limits above (defaults: 5,000 records,
65,536 payload bytes per fact, 300 seconds, 4,128,768 result
bytes). One input page document is capped at 32 MiB. Go over the
page budget and the claim fails as terminal with class
`page-budget-exceeded` rather than splitting into pages. If a
scope keeps blowing the budget, shard it by asset family, content
family, or location bucket.

---

Looking for what comes out the other side? See [Facts](facts.md).
Stuck on a value? [Troubleshoot](../guides/troubleshoot.md) covers
the failures people hit most.
