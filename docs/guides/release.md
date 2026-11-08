# Release

```bash
make ci
```

`make ci` runs `make lint`, `make test`, and `make race` (see
[Makefile](../../Makefile)): vet, lint and format checks, module
drift, the 500-line file cap, the full test suite, and the race
suite. See [CONTRIBUTING.md](../../CONTRIBUTING.md) for CI lanes and
the coverage floor, which may only rise.

Releasing is maintainer-run and digest-by-digest; nothing here
deploys:

1. Tag the protected `main` head with exactly `v` plus the
   `metadata.version` in [manifest.yaml](../../manifest.yaml).
   Any other tag fails before anything builds or pushes.
2. Push the tag; the release workflow builds, signs, and publishes
   the image with SBOM and provenance attestations and prints the
   digest-pinned reference in the job summary.
3. Record that digest in the manifest with `scripts/pin-digest.sh`,
   then seed activations from the published digest as shown above.

Before tagging, run the local gate from
[Contributing](../contributing/README.md) and re-read
[manifest.yaml](../../manifest.yaml): the manifest version, the
git tag, and the pinned image digest must agree.
