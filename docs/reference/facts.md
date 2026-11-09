# Facts

You'll get what's in the scope, how resources relate, and who can
access them, which answers which Google Cloud resources Eshu has
seen and how they connect.

The table below mirrors `spec.emittedFacts` in
[manifest.yaml](../../manifest.yaml); update this table in the same
change as the manifest. That file declares 10 fact kinds with
`reported` confidence (the source reported it; Eshu didn't measure
it directly). `gcp_cloud_resource` is at schema `1.1.0`; the other
nine are at `1.0.0`.

| Fact kind | What you get |
|---|---|
| `gcp_cloud_resource` | One Google Cloud resource seen in the scope |
| `gcp_cloud_relationship` | One link between two Google Cloud resources |
| `gcp_tag_observation` | Tags or labels seen on one resource |
| `gcp_iam_policy_observation` | An IAM policy (who can do what) seen on a resource |
| `gcp_dns_record` | One DNS record seen for a resource |
| `gcp_image_reference` | A container or machine image a resource points at |
| `gcp_iam_principal` | One IAM identity (a who) seen in the scope |
| `gcp_iam_trust_policy` | Who is allowed to act as one IAM identity |
| `gcp_iam_permission_policy` | What one IAM identity is allowed to do |
| `gcp_collection_warning` | Part of the scope could not be read fully |

New to the words *fact* and *claim*? [Concepts](../concepts/README.md) defines them in plain language.
