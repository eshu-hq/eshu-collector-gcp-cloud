# Seed and install

An init container (or you, by hand) can lay down the component
without a shell script: `seed` writes the collector
binary and [manifest.yaml](../../manifest.yaml) into a component
directory and can stamp the deployed image digest into the seeded
manifest:

```bash
collector seed --dest DIR --image-ref ghcr.io/eshu-hq/eshu-collector-gcp-cloud@sha256:<digest>
```

Without `--image-ref`, `COLLECTOR_IMAGE_REF` supplies the
reference. [manifest.yaml](../../manifest.yaml) pins the Linux amd64
image for version `0.1.4` by digest:
`ghcr.io/eshu-hq/eshu-collector-gcp-cloud@sha256:06cc0b5747226adea1d753df61b097930bb8220e17c838346e6a1b2bec840a38`.
The image entrypoint is `/usr/local/bin/collector --sdk-stdio`
(see [Dockerfile](../../Dockerfile)).

Image references stay digest-pinned (`@sha256:...`). After a
release, record the published digest as described in
[Release](release.md), then seed activations from that digest.
First run after install? [Getting started](../getting-started/README.md)
has the one-minute version.
