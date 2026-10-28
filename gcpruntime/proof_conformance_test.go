// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"encoding/json"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/conformance"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
)

// proofManifest declares this producer's emitted families for the conformance
// harness: the 7 gcp_* kinds plus the 3 secrets/IAM mirror kinds, all
// reported confidence, all trusted to this producer's two collector kinds.
func proofManifest() conformance.Manifest {
	families := []conformance.FactFamily{
		{Kind: gcpcloud.GCPCloudResourceFactKind, SchemaVersions: []string{gcpcloud.GCPCloudResourceSchemaVersion}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPCloudRelationshipFactKind, SchemaVersions: []string{gcpcloud.GCPCloudRelationshipSchemaVersion}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPTagObservationFactKind, SchemaVersions: []string{gcpcloud.GCPTagObservationSchemaVersion}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPIAMPolicyObservationFactKind, SchemaVersions: []string{gcpcloud.GCPIAMPolicyObservationSchemaVersion}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPDNSRecordFactKind, SchemaVersions: []string{gcpcloud.GCPDNSRecordSchemaVersion}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPImageReferenceFactKind, SchemaVersions: []string{gcpcloud.GCPImageReferenceSchemaVersion}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPCollectionWarningFactKind, SchemaVersions: []string{gcpcloud.GCPCollectionWarningSchemaVersion}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPIAMPrincipalFactKind, SchemaVersions: []string{gcpcloud.SecretsIAMSchemaVersionV1}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPIAMTrustPolicyFactKind, SchemaVersions: []string{gcpcloud.SecretsIAMSchemaVersionV1}, SourceConfidence: []string{"reported"}},
		{Kind: gcpcloud.GCPIAMPermissionPolicyFactKind, SchemaVersions: []string{gcpcloud.SecretsIAMSchemaVersionV1}, SourceConfidence: []string{"reported"}},
	}
	return conformance.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: conformance.Metadata{
			ID:        "dev.eshu.collector.gcp-cloud",
			Name:      "GCP cloud collector",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: conformance.Spec{
			CompatibleCore: ">=0.0.5 <0.2.0",
			ComponentType:  "collector",
			CollectorKinds: []string{CollectorKind, PostureCollectorKind},
			Runtime: conformance.RuntimeContract{
				SDKProtocol: sdkcollector.ProtocolVersionV1Alpha1,
				Adapter:     "process",
			},
			// Proof placeholder: the release artifact does not exist yet
			// (cutover phase). The digest below is not a published claim;
			// it satisfies the harness's artifact-presence rule so payload
			// conformance can run before release.
			Artifacts: []conformance.Artifact{{
				Platform: "linux/amd64",
				Image:    "ghcr.io/eshu-hq/eshu-collector-gcp-cloud@sha256:0000000000000000000000000000000000000000000000000000000000000000",
			}},
			EmittedFacts: families,
			ConsumerContracts: conformance.ConsumerContracts{
				Reducer: conformance.ReducerContract{
					Phases: []string{conformance.SourceEvidenceOnlyReducerPhase},
				},
			},
		},
	}
}

// proofCoreFacts authorizes this producer's exact emission coverage per
// reserved kind, with payload schemas resolved from the pinned fixture pack.
// Mirror kinds carry the dual-kind authorization (primary claim kind plus
// family kind), as in the k8s-live proof.
func proofCoreFacts(t *testing.T, manifest conformance.Manifest) (*conformance.CoreFactContext, map[string]json.RawMessage) {
	t.Helper()

	authorizations := []conformance.CoreFactAuthorization{}
	schemas := map[string]json.RawMessage{}
	for _, family := range manifest.Spec.EmittedFacts {
		schema, ok := fixturepack.SchemaFor(family.Kind)
		if !ok {
			t.Fatalf("pinned fixture pack ships no schema for %q", family.Kind)
		}
		schemas[family.Kind] = schema
		emissions := []conformance.CoreFactEmission{{CollectorKind: CollectorKind, SchemaVersions: family.SchemaVersions}}
		if familyKind := proofWantCollectorKind(family.Kind); familyKind != CollectorKind {
			emissions = append(emissions, conformance.CoreFactEmission{CollectorKind: familyKind, SchemaVersions: family.SchemaVersions})
		}
		authorizations = append(authorizations, conformance.CoreFactAuthorization{
			Kind:             family.Kind,
			Emissions:        emissions,
			ReducerPhases:    []string{conformance.SourceEvidenceOnlyReducerPhase},
			PayloadSchemaRef: family.Kind,
		})
	}
	return &conformance.CoreFactContext{
		ComponentID:      manifest.Metadata.ID,
		ComponentVersion: manifest.Metadata.Version,
		Facts:            authorizations,
	}, schemas
}

func proofSnapshots(t *testing.T) []sdkcollector.Result {
	t.Helper()

	fencing := map[string]int64{
		"gcp:project:parity-proj:mixed:resource:global":  7,
		"gcp:project:expired-proj:mixed:resource:global": 9,
		"gcp:project:quota-proj:mixed:resource:global":   11,
	}
	source := proof8856LiveSource(t)
	var fixtures []sdkcollector.Result
	for {
		collected, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		fencingToken, ok := fencing[collected.Scope.ScopeID]
		if !ok {
			t.Fatalf("no fencing token for scope %q", collected.Scope.ScopeID)
		}
		result, err := ToSDKResult(proofClaim(collected.Scope, collected.Generation, fencingToken), collected)
		if err != nil {
			t.Fatalf("ToSDKResult() error = %v", err)
		}
		fixtures = append(fixtures, result)
	}
	if len(fixtures) != 3 {
		t.Fatalf("fixtures = %d, want 3 scopes", len(fixtures))
	}
	return fixtures
}

// TestExtractedPayloadsPassConformance proves the gcp_cloud_resource facts
// the extracted producer emits satisfy the full SDK publication contract in
// the producer's own module against the producer's own output. The remaining
// families travel the schema-only path below until the SDK payload-key
// allowlist grows (same handoff as the k8s-live proof, eshu-hq/eshu#8851):
// legacy posture and tag field names trip the fail-closed key heuristic.
func TestExtractedPayloadsPassConformance(t *testing.T) {
	t.Parallel()

	manifest := proofManifest()
	manifest.Spec.EmittedFacts = []conformance.FactFamily{manifest.Spec.EmittedFacts[0]}
	manifest.Spec.CollectorKinds = []string{CollectorKind}
	coreFacts, schemas := proofCoreFacts(t, manifest)
	reserved := []string{gcpcloud.GCPCloudResourceFactKind}

	var fixtures []sdkcollector.Result
	for _, fixture := range proofSnapshots(t) {
		kept := fixture
		kept.Facts = nil
		for _, fact := range fixture.Facts {
			if fact.Kind == gcpcloud.GCPCloudResourceFactKind {
				kept.Facts = append(kept.Facts, fact)
			}
		}
		fixtures = append(fixtures, kept)
	}

	report := conformance.Run(conformance.Request{
		Manifest:          manifest,
		Fixtures:          fixtures,
		Mode:              conformance.ModeFixture,
		ReservedFactKinds: reserved,
		CoreFacts:         coreFacts,
		PayloadSchemas:    schemas,
	})
	if !report.OK() {
		t.Fatalf("conformance findings = %#v, want passed", report.Findings)
	}
	if report.Summary.FixtureCount != 3 {
		t.Fatalf("FixtureCount = %d, want 3", report.Summary.FixtureCount)
	}
	if report.Summary.FactCount == 0 {
		t.Fatal("FactCount = 0, want every emitted fact validated")
	}
}

// TestExtractedPayloadsMatchPackSchemas proves every emitted fact payload
// satisfies the JSON Schema shapes the pinned fixture pack ships, via the
// schema-only check.
func TestExtractedPayloadsMatchPackSchemas(t *testing.T) {
	t.Parallel()

	manifest := proofManifest()
	_, schemas := proofCoreFacts(t, manifest)
	for i, fixture := range proofSnapshots(t) {
		if err := conformance.ValidatePayloadSchemas(schemas, fixture); err != nil {
			t.Fatalf("fixture[%d] payload schema: %v", i, err)
		}
	}
}
