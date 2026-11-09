# Troubleshoot

Start with the symptom you have. Most of these end at a config
value, so keep [Configuration](../reference/configuration.md) open
in another tab.

- Terminal result about credentials? Your scopes name different
  `credential_ref` values. One claim uses one ADC identity, so
  split the activation by credential.
- `page-budget-exceeded` failures? The result document outgrew
  `maxPageBytes`. Shard the scope before raising the limit.
- Empty result you didn't expect? Check whether
  `freshness.previousDigest` matched. An unchanged scan returns a
  fact-free `unchanged` result on purpose.
- Scope reports terminal instead of partial? That's by design.
  A scope that can't be scanned fails closed, and no claim reports
  complete for a scope it didn't read.

## Still stuck?

Re-read the failure flow in
[Diagrams](../diagrams/README.md#3-when-something-goes-wrong) and
check which branch your run took. If the result looks complete but
wrong, compare it against [Facts](../reference/facts.md) before
suspecting the source.
