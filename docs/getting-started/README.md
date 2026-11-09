# Getting started

Hi! In about a minute you can watch this collector turn
Google Cloud asset data from Cloud Asset Inventory (CAI), or a fixture file into facts for Eshu. It only collects and
returns evidence; the Eshu host validates the result and stores it.
Nothing is stored (or changed) here.

## Run it once

```bash
go run ./cmd/collector --input testdata/complete.json
```

That collects one claim from the checked-in fixture: no Google Cloud account and no live call. Fixture and unit tests never call Google Cloud either.

## What just happened?

The host runs one *claim* (one bounded request to collect one scope)
per process invocation: one `collector-sdk/v1alpha1` request on
stdin, one result full of typed *facts* on stdout. Eshu can then
answer which Google Cloud resources Eshu has seen and how they connect. The picture and the step-by-step flow live
in [Diagrams](../diagrams/README.md) and
[Architecture](../architecture/README.md).

## Where next?

- Every knob: [Configuration](../reference/configuration.md)
- What comes out: [Facts](../reference/facts.md)
- Day-2 work: [Guides](../guides/README.md)
- New words and boundaries: [Concepts](../concepts/README.md)
