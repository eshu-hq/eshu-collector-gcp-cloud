# Concepts

The Eshu host gives this collector one bounded Google Cloud scope.
The collector lists assets for that scope, turns them into small
typed facts, and hands the facts back. The host validates the
result and commits it to Eshu core. Nothing gets stored here.

Quick glossary. A *fact* is one small, typed piece of evidence.
A *claim* is one bounded request to collect one scope. The *host*
is the Eshu process that starts this collector, sends it a claim,
and stores the result. CAI means Cloud Asset Inventory, Google
Cloud's asset listing service.

## When to use it

Use it when Eshu needs evidence about Google Cloud resources under
one organization, folder, or project, including their
relationships, tags, DNS records, image references, and IAM
policies and identities.

## When not to use it

- Don't use it as a live Google Cloud browser or reporting tool.
  It returns facts to the Eshu host; it has no user interface.
- Don't mix credentials in one activation.
  Scopes that name different `credential_ref` values fail closed.
- Don't use it for other clouds or for Google Cloud data-plane
  contents (object data, secrets, or record payloads).

## What you get

The full fact table lives in [Facts](../reference/facts.md), which
mirrors `spec.emittedFacts` in [manifest.yaml](../../manifest.yaml).

## Boundaries: what this never does

- It never writes to the Eshu graph, Postgres, or any queue.
  The host validates the result and commits the facts.
- It never performs a Google Cloud write; live transport is GET-only.
- It never stores a secret value in config, facts, logs, or labels.
  Credentials and redaction material resolve by name from the
  process environment.
- It never scans a scope that config didn't list.

For the flow behind these boundaries, see
[Architecture](../architecture/README.md) and
[Diagrams](../diagrams/README.md).
