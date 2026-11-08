# Architecture

Short version: the Eshu host hands this collector one bounded scope,
the collector reads it and hands typed facts back, and the host
validates and commits them to Eshu core. This collector stores
nothing itself. All diagrams live in
[Diagrams](../diagrams/README.md); this page is the words.

## Where it sits

Google Cloud (or a fixture file) -> this collector ->
Eshu host process -> Eshu core.

Overview diagram: [Where it sits](../diagrams/README.md#1-where-it-sits).

## One claim, start to finish

the host writes one `collector-sdk/v1alpha1` request to
stdin, the collector reads the claimed scope and builds facts,
then writes one result to stdout. Each host request gets exactly
one result document back; there's no multi-page continuation in
this version.

Sequence diagram:
[One claim, start to finish](../diagrams/README.md#2-one-claim-start-to-finish).

## When something goes wrong

scopes naming different `credential_ref` values return a
terminal result, because one claim uses one ADC identity. A scan
that matches `freshness.previousDigest` returns a fact-free
`unchanged` result. A result over the page budget fails the claim
as terminal with class `page-budget-exceeded`; it's never split
into pages. A scope that can't be scanned fails closed with a
terminal result; no claim reports complete for a scope it didn't
read.

Failure-flow diagram:
[When something goes wrong](../diagrams/README.md#3-when-something-goes-wrong).

## Trust boundary

- It never writes to the Eshu graph, Postgres, or any queue.
  The host validates the result and commits the facts.
- It never performs a Google Cloud write; live transport is GET-only.
- It never stores a secret value in config, facts, logs, or labels.
  Credentials and redaction material resolve by name from the
  process environment.
- It never scans a scope that config didn't list.
