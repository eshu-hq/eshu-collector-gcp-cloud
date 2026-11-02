// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var digestPattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)

// placeholderDigestPattern matches the all-zero first-publish placeholder.
// No image has been pushed yet, so the manifest cannot pin a real pushed
// digest; the tag workflow prints it and scripts/pin-digest.sh records it.
var placeholderDigestPattern = regexp.MustCompile(`@sha256:0{64}$`)

type manifestFact struct {
	Kind             string   `yaml:"kind"`
	PayloadSchemaRef string   `yaml:"payloadSchemaRef"`
	SchemaVersions   []string `yaml:"schemaVersions"`
	SourceConfidence []string `yaml:"sourceConfidence"`
}

type manifestFile struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		ID      string `yaml:"id"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
	Spec struct {
		CollectorKinds []string `yaml:"collectorKinds"`
		Runtime        struct {
			SDKProtocol string `yaml:"sdkProtocol"`
			Adapter     string `yaml:"adapter"`
		} `yaml:"runtime"`
		Artifacts []struct {
			Platform string `yaml:"platform"`
			Image    string `yaml:"image"`
		} `yaml:"artifacts"`
		EmittedFacts []manifestFact `yaml:"emittedFacts"`
		Telemetry    struct {
			MetricsPrefix string `yaml:"metricsPrefix"`
		} `yaml:"telemetry"`
	} `yaml:"spec"`
}

func loadManifest(t *testing.T) manifestFile {
	t.Helper()
	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var manifest manifestFile
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// gcpEmittedFacts is the emission contract: every gcp_* kind the gcpruntime
// Source can emit, with the schema version pinned in gcpcloud/kinds.go.
// gcp_cloud_resource rides at 1.1.0; every other family rides at 1.0.0.
// Confidence is reported: the envelope builders record facts returned by
// the GCP APIs as reported.
var gcpEmittedFacts = []manifestFact{
	{Kind: "gcp_cloud_resource", SchemaVersions: []string{"1.1.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_cloud_relationship", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_tag_observation", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_iam_policy_observation", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_dns_record", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_image_reference", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_iam_principal", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_iam_trust_policy", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_iam_permission_policy", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
	{Kind: "gcp_collection_warning", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"reported"}},
}

// TestManifestReleaseProvenance proves releases are reproducible and the
// component manifest is valid: every artifact is digest-pinned, the manifest
// names this collector's kind and wire protocol, and every emitted fact
// family is versioned with declared confidence.
func TestManifestReleaseProvenance(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	if strings.TrimSpace(manifest.APIVersion) == "" || strings.TrimSpace(manifest.Kind) == "" {
		t.Fatal("manifest missing apiVersion/kind")
	}
	if strings.TrimSpace(manifest.Metadata.ID) == "" || strings.TrimSpace(manifest.Metadata.Version) == "" {
		t.Fatal("manifest missing metadata id/version")
	}
	if manifest.Metadata.ID != ComponentID {
		t.Fatalf("manifest id = %q, want %q", manifest.Metadata.ID, ComponentID)
	}
	if len(manifest.Spec.CollectorKinds) != 1 || manifest.Spec.CollectorKinds[0] != CollectorKind {
		t.Fatalf("manifest collectorKinds = %v, want [%s]", manifest.Spec.CollectorKinds, CollectorKind)
	}
	if manifest.Spec.Runtime.SDKProtocol != "collector-sdk/v1alpha1" {
		t.Fatalf("manifest sdkProtocol = %q, want collector-sdk/v1alpha1", manifest.Spec.Runtime.SDKProtocol)
	}
	if manifest.Spec.Runtime.Adapter != "process" {
		t.Fatalf("manifest adapter = %q, want process", manifest.Spec.Runtime.Adapter)
	}
	if len(manifest.Spec.Artifacts) == 0 {
		t.Fatal("manifest declares no artifacts")
	}
	for _, artifact := range manifest.Spec.Artifacts {
		if !digestPattern.MatchString(artifact.Image) {
			t.Fatalf("artifact image %q is not digest-pinned", artifact.Image)
		}
		if strings.Contains(artifact.Image, ":latest") {
			t.Fatalf("artifact image %q uses a mutable tag", artifact.Image)
		}
	}
	declared := make(map[string]manifestFact, len(manifest.Spec.EmittedFacts))
	for _, family := range manifest.Spec.EmittedFacts {
		if strings.TrimSpace(family.Kind) == "" {
			t.Fatal("emitted fact kind empty")
		}
		if _, dup := declared[family.Kind]; dup {
			t.Fatalf("manifest declares %q twice", family.Kind)
		}
		declared[family.Kind] = family
		if strings.TrimSpace(family.PayloadSchemaRef) == "" {
			t.Fatalf("fact kind %q has no payload schema ref", family.Kind)
		}
		if len(family.SchemaVersions) == 0 {
			t.Fatalf("fact kind %q has no schema versions", family.Kind)
		}
		if len(family.SourceConfidence) == 0 {
			t.Fatalf("fact kind %q has no source confidence", family.Kind)
		}
	}
	for _, want := range gcpEmittedFacts {
		got, ok := declared[want.Kind]
		if !ok {
			t.Errorf("manifest does not declare %q", want.Kind)
			continue
		}
		if len(got.SchemaVersions) != 1 || got.SchemaVersions[0] != want.SchemaVersions[0] {
			t.Errorf("manifest %q versions = %v, want %v", want.Kind, got.SchemaVersions, want.SchemaVersions)
		}
		if len(got.SourceConfidence) != 1 || got.SourceConfidence[0] != "reported" {
			t.Errorf("manifest %q confidence = %v, want [reported]", want.Kind, got.SourceConfidence)
		}
	}
	if len(declared) != len(gcpEmittedFacts) {
		t.Errorf("manifest declares %d kinds, want %d (no unlisted families)", len(declared), len(gcpEmittedFacts))
	}
	if strings.TrimSpace(manifest.Spec.Telemetry.MetricsPrefix) == "" {
		t.Fatal("manifest missing telemetry metrics prefix")
	}
	if manifest.Spec.Telemetry.MetricsPrefix != MetricsPrefix {
		t.Fatalf("manifest metrics prefix = %q, want %q", manifest.Spec.Telemetry.MetricsPrefix, MetricsPrefix)
	}
}

// TestManifestArtifactPlaceholderPinnedOnFirstPublish documents the
// first-publish state: the artifact carries the all-zero placeholder digest
// until the tag workflow publishes an image and scripts/pin-digest.sh
// records the real pushed digest. The placeholder is tolerated only while
// the manifest still declares the initial 0.1.0 version it shipped with;
// any version bump must pin a real digest.
func TestManifestArtifactPlaceholderPinnedOnFirstPublish(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	for _, artifact := range manifest.Spec.Artifacts {
		if !placeholderDigestPattern.MatchString(artifact.Image) {
			continue
		}
		if manifest.Metadata.Version != "0.1.0" {
			t.Fatalf("artifact image %q is the unpinned placeholder digest past the initial version: run scripts/pin-digest.sh", artifact.Image)
		}
	}
}
