# eshu-collector-gcp-cloud

Eshu GCP cloud collector, extracted from the Eshu core repository.

**Pre-release. Holds no producer authority until its reviewed cutover
(eshu-hq/eshu#8856).**

Extraction tracking: [eshu-hq/eshu#8856](https://github.com/eshu-hq/eshu/issues/8856).
The in-tree extraction census, producer-grant draft, and parity proofs live
in the core repository until the cutover rehearsal lands them here.

## Releasing

The release vehicle is digest-by-digest and maintainer-run. Nothing here
deploys: pushing a version tag builds, signs, and publishes the image plus
SBOM and provenance attestations, and a person copies the digest into the
activation afterwards.

1. Land the release on `main`, then tag the protected `main` head:
   `git tag v0.1.0` (the tag must be exactly `v` plus
   `manifest.yaml` `metadata.version`; anything else fails before anything
   builds or pushes).
2. Push the tag. `release.yml` publishes the image and prints the
   digest-pinned reference (`ghcr.io/...@sha256:...`) in the job summary.
3. Pin the published digest in `manifest.yaml` `artifacts`:
   `scripts/pin-digest.sh ghcr.io/eshu-hq/eshu-collector-gcp-cloud:v0.1.0`.
   The checked-in manifest carries the all-zero placeholder until this
   first publish lands it (`manifest_test.go` enforces the placeholder
   only at the initial `0.1.0` version).
4. Seed the component home from the published image digest:
   `collector seed --dest DIR --image-ref ghcr.io/...@sha256:...`
   (or set `COLLECTOR_IMAGE_REF`); the installer stamps the deployed
   reference into the seeded `manifest.yaml` because an image cannot
   embed its own digest.

Operational notes: under the `process` adapter the host starts
`./collector --sdk-stdio`, writes one `collector-sdk/v1alpha1` claim on
stdin, and reads one SDK result from stdout. `--previous-digest` (or the
host `freshness.previousDigest`) short-circuits an unchanged scan to a
fact-free `unchanged` result. The live claim path parses the host
activation scopes and then fails closed with a terminal
`live-transport-unavailable` result: the live CAI transport stays in core
until the live-backend phase, so no claim ever reports complete for a
scope it did not read. Producer grants and producer authority stay in core
until the cutover above; this section covers the image vehicle only.
