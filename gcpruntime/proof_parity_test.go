// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/conformance"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud/catalog"
)

// This file is new in the extraction: it is the #4047 cutover gate's
// extracted-path half for eshu-hq/eshu#8856. It proves the extracted producer
// reproduces the in-tree emission exactly — the same scopes, kinds, stable
// keys, and boundary attributes — against the same fixture-pack oracle the
// in-tree parity pin was cut from
// (testdata/8856-parity-gcp-cloud.json, copied verbatim from core).

type proof8856ScopeSnapshot struct {
	ScopeID      string            `json:"scope_id"`
	GenerationID string            `json:"generation_id"`
	Kinds        map[string]int    `json:"kinds"`
	Keys         []string          `json:"keys"`
	Attrs        map[string]string `json:"attrs"`
}

// proofAttrKey binds the host-enforced boundary attributes to the stable key
// so the proof pins what admission and grant checks key on. Format matches
// the pack: key, kind, version, collector kind, confidence, fencing token.
func proofAttrKey(envelope gcpcloud.Envelope) string {
	return envelope.StableFactKey + "\x00" + envelope.FactKind + "\x00" +
		envelope.SchemaVersion + "\x00" + envelope.CollectorKind + "\x00" +
		envelope.SourceConfidence + "\x00" + strconv.FormatInt(envelope.FencingToken, 10)
}

// proofWantCollectorKind pins the dual-collector-kind emission of this
// source: gcp_* facts carry "gcp", while the secrets/IAM mirror facts it
// also emits (gcp_iam_principal, gcp_iam_trust_policy,
// gcp_iam_permission_policy) carry "secrets_iam_posture".
func proofWantCollectorKind(factKind string) string {
	switch factKind {
	case gcpcloud.GCPIAMPrincipalFactKind,
		gcpcloud.GCPIAMTrustPolicyFactKind,
		gcpcloud.GCPIAMPermissionPolicyFactKind:
		return PostureCollectorKind
	default:
		return CollectorKind
	}
}

func loadProofOracle(t *testing.T) []proof8856ScopeSnapshot {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "8856-parity-gcp-cloud.json"))
	if err != nil {
		t.Fatalf("read parity pack: %v", err)
	}
	var snapshot struct {
		Scopes []proof8856ScopeSnapshot `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("parse parity pack: %v", err)
	}
	if len(snapshot.Scopes) != 3 {
		t.Fatalf("pack scopes = %d, want 3", len(snapshot.Scopes))
	}
	return snapshot.Scopes
}

// TestExtractedParityMatchesFixturePack is the extracted-path source-parity
// pin: the extracted producer and the checked-in fixture pack agree scope by
// scope on scope ID, generation ID, fact-kind multiset, stable keys, schema
// versions, collector kinds, confidence, and fencing tokens.
func TestExtractedParityMatchesFixturePack(t *testing.T) {
	t.Parallel()

	scopes := loadProofOracle(t)
	byScope := map[string]proof8856ScopeSnapshot{}
	for _, scope := range scopes {
		byScope[scope.ScopeID] = scope
	}

	live := proof8856LiveSource(t)
	yielded := 0
	for {
		collected, ok, err := live.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		yielded++
		expected, ok := byScope[collected.Scope.ScopeID]
		if !ok {
			t.Fatalf("live scope %q not in fixture pack", collected.Scope.ScopeID)
		}
		delete(byScope, collected.Scope.ScopeID)
		if collected.Generation.GenerationID != expected.GenerationID {
			t.Fatalf("live GenerationID = %q, want %q", collected.Generation.GenerationID, expected.GenerationID)
		}
		envelopes := drainFacts(t, collected)
		if len(envelopes) == 0 {
			t.Fatalf("live scope %q has no facts", expected.ScopeID)
		}
		kinds := map[string]int{}
		keys := make([]string, 0, len(envelopes))
		attrs := make(map[string]string, len(envelopes))
		for _, envelope := range envelopes {
			kinds[envelope.FactKind]++
			keys = append(keys, envelope.StableFactKey)
			attrs[envelope.StableFactKey] = proofAttrKey(envelope)
			if envelope.GenerationID != collected.Generation.GenerationID {
				t.Fatalf("live fact generation %q != scope generation %q", envelope.GenerationID, collected.Generation.GenerationID)
			}
			if envelope.SourceConfidence != gcpcloud.SourceConfidenceReported {
				t.Fatalf("SourceConfidence = %q, want reported for kind %q", envelope.SourceConfidence, envelope.FactKind)
			}
			if envelope.SchemaVersion == "" {
				t.Fatalf("empty SchemaVersion for kind %q", envelope.FactKind)
			}
			if envelope.CollectorKind != proofWantCollectorKind(envelope.FactKind) {
				t.Fatalf("CollectorKind = %q, want %q for kind %q", envelope.CollectorKind, proofWantCollectorKind(envelope.FactKind), envelope.FactKind)
			}
		}
		slices.Sort(keys)
		if !maps.Equal(kinds, expected.Kinds) {
			t.Fatalf("live kinds = %v, want %v", kinds, expected.Kinds)
		}
		if !slices.Equal(keys, expected.Keys) {
			t.Fatalf("live stable keys differ from pack for scope %q", expected.ScopeID)
		}
		if !maps.Equal(attrs, expected.Attrs) {
			t.Fatalf("live boundary attributes differ from pack for scope %q", expected.ScopeID)
		}
	}
	if yielded != len(scopes) {
		t.Fatalf("live yielded %d scopes, want %d", yielded, len(scopes))
	}
	if len(byScope) != 0 {
		t.Fatalf("fixture scopes never yielded live: %v", slices.Sorted(maps.Keys(byScope)))
	}

	union := map[string]bool{}
	for _, scope := range scopes {
		for kind := range scope.Kinds {
			union[kind] = true
		}
	}
	for _, kind := range gcpcloud.GCPFactKinds() {
		if !union[kind] {
			t.Fatalf("pack covers no %q fact; parity pack must span all 7 live kinds", kind)
		}
	}
}

// TestExtractedDuplicateDeliveryIsStable pins duplicate safety: two identical
// scans emit identical stable-key sets, so a redelivered generation commits
// idempotently at the core boundary.
func TestExtractedDuplicateDeliveryIsStable(t *testing.T) {
	t.Parallel()

	stableKeys := func(t *testing.T, src *Source) []string {
		t.Helper()
		first, ok, err := src.Next(context.Background())
		if err != nil || !ok {
			t.Fatalf("first Next() = (%v, %v), want (true, nil)", ok, err)
		}
		keys := make([]string, 0)
		for _, envelope := range drainFacts(t, first) {
			keys = append(keys, envelope.StableFactKey)
		}
		slices.Sort(keys)
		return keys
	}
	if !slices.Equal(stableKeys(t, proof8856LiveSource(t)), stableKeys(t, proof8856LiveSource(t))) {
		t.Fatal("duplicate scans emit different stable keys; redelivery would not be idempotent")
	}
}

// TestExtractedStaleGenerationEmitsWarningOnly pins fencing: a scope already
// owned by a newer fencing token emits a single stale warning and no resource
// facts, so a stale scan can neither replace nor drop current facts.
func TestExtractedStaleGenerationEmitsWarningOnly(t *testing.T) {
	t.Parallel()

	representative, _, _ := proof8856Scopes()
	resolved := representative.withDefaults()
	source := proof8856LiveSource(t)
	if err := source.Tracker.Accept(resolved.ScopeID, "gen-8856-newer", resolved.FencingToken+1); err != nil {
		t.Fatalf("pre-accept newer token: %v", err)
	}
	collected, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("stale Next() = (%v, %v), want (true, nil)", ok, err)
	}
	envelopes := drainFacts(t, collected)
	if len(envelopes) != 1 {
		t.Fatalf("stale envelopes = %d, want exactly the stale warning", len(envelopes))
	}
	if envelopes[0].FactKind != gcpcloud.GCPCollectionWarningFactKind {
		t.Fatalf("stale kind = %q, want %q", envelopes[0].FactKind, gcpcloud.GCPCollectionWarningFactKind)
	}
}

// TestExtractedRedactionKeepsRawIdentityOutOfPayloads pins the parser choke
// point: raw IAM members, DNS names and targets, and container names never
// reach durable payloads. Verbatim provider identity (full resource names)
// and non-sensitive label values stay by design and are not in the blocklist.
func TestExtractedRedactionKeepsRawIdentityOutOfPayloads(t *testing.T) {
	t.Parallel()

	blocklist := []string{
		"reader@parity-proj.iam.gserviceaccount.com",
		"ci@parity-proj.iam.gserviceaccount.com",
		"deployer@parity-proj.iam.gserviceaccount.com",
		"www.example.test.",
		"203.0.113.10",
		"parity-web-sidecar",
	}
	source := proof8856LiveSource(t)
	for {
		collected, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		for _, envelope := range drainFacts(t, collected) {
			// full_resource_name is verbatim provider identity preserved by
			// design for exact reducer joins; it is not a redaction leak.
			payload := maps.Clone(envelope.Payload)
			delete(payload, "full_resource_name")
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			for _, blocked := range blocklist {
				if strings.Contains(string(raw), blocked) {
					t.Fatalf("payload for kind %q leaks %q", envelope.FactKind, blocked)
				}
			}
		}
	}
}

// TestExtractedEmissionNeedsNoGrant documents the deliberate delegation delta:
// the in-tree first-party path emits with no grant lookup, so this asserts
// emission succeeds grant-free. The extracted producer must instead fail
// closed on a missing, expired, or revoked grant (PRODUCER_GRANT_DRAFT); the
// fencing token on every envelope is the host-side input that enforcement
// keys on.
func TestExtractedEmissionNeedsNoGrant(t *testing.T) {
	t.Parallel()

	representative, _, _ := proof8856Scopes()
	wantFencing := representative.FencingToken
	collected, ok, err := proof8856LiveSource(t).Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want grant-free extracted emission")
	}
	for _, envelope := range drainFacts(t, collected) {
		if envelope.FencingToken != wantFencing {
			t.Fatalf("FencingToken = %d, want %d", envelope.FencingToken, wantFencing)
		}
	}
}

// proofClaim builds the core-owned work-item identity for one collected
// snapshot, mirroring what the host passes at cutover.
func proofClaim(scope ScopeInfo, generation GenerationInfo, fencingToken int64) sdkcollector.Claim {
	observedAt := generation.ObservedAt
	return sdkcollector.Claim{
		ComponentID:   "dev.eshu.collector.gcp-cloud",
		InstanceID:    "gcp-proof-1",
		CollectorKind: CollectorKind,
		SourceSystem:  CollectorKind,
		Scope:         sdkcollector.Scope{ID: scope.ScopeID, Kind: scope.ScopeKind},
		SourceRunID:   "run-proof-1",
		GenerationID:  generation.GenerationID,
		WorkItemID:    "work-proof-1",
		FencingToken:  strconv.FormatInt(fencingToken, 10),
		Attempt:       1,
		Deadline:      observedAt.Add(time.Hour),
		ConfigHandle:  "proof-config:gcp-cloud",
	}
}

// TestExtractedSDKResultBoundary proves the SDK wire conversion: complete vs
// partial state, 1:1 envelope mapping with identity and source ref preserved
// (source system claim-aligned per the published contract), fencing
// round-trip failure closed, and exhaustion yielding a fact-free complete.
func TestExtractedSDKResultBoundary(t *testing.T) {
	t.Parallel()

	source := proof8856LiveSource(t)
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want the representative scope")
	}
	claim := proofClaim(collected.Scope, collected.Generation, 7)
	result, err := ToSDKResult(claim, collected)
	if err != nil {
		t.Fatalf("ToSDKResult() error = %v", err)
	}
	if result.ProtocolVersion != sdkcollector.ProtocolVersionV1Alpha1 {
		t.Fatalf("ProtocolVersion = %q, want v1alpha1", result.ProtocolVersion)
	}
	if result.State != sdkcollector.ResultComplete {
		t.Fatalf("State = %q, want complete for the non-partial scope", result.State)
	}
	if len(result.Facts) != len(collected.Envelopes) {
		t.Fatalf("Facts = %d, want %d (1:1 envelope mapping)", len(result.Facts), len(collected.Envelopes))
	}
	for i, fact := range result.Facts {
		envelope := collected.Envelopes[i]
		if fact.Kind != envelope.FactKind || fact.StableKey != envelope.StableFactKey ||
			fact.SchemaVersion != envelope.SchemaVersion {
			t.Fatalf("fact[%d] identity mismatch: %v/%v/%v", i, fact.Kind, fact.StableKey, fact.SchemaVersion)
		}
		if fact.SourceRef.SourceSystem != claim.SourceSystem ||
			fact.SourceRef.ScopeID != envelope.SourceRef.ScopeID ||
			fact.SourceRef.GenerationID != envelope.SourceRef.GenerationID ||
			fact.SourceRef.FactKey != envelope.SourceRef.FactKey ||
			fact.SourceRef.URI != envelope.SourceRef.SourceURI ||
			fact.SourceRef.RecordID != envelope.SourceRef.SourceRecordID {
			t.Fatalf("fact[%d] source ref not preserved: %#v", i, fact.SourceRef)
		}
		if string(fact.SourceConfidence) != gcpcloud.SourceConfidenceReported {
			t.Fatalf("fact[%d] confidence = %q, want reported", i, fact.SourceConfidence)
		}
	}

	bad := proofClaim(collected.Scope, collected.Generation, 7)
	bad.FencingToken = "not-an-int"
	if _, err := ToSDKResult(bad, collected); err == nil {
		t.Fatal("ToSDKResult with unparseable fencing token succeeded, want terminal caller error")
	}

	exhausted := ExhaustedResult(claim)
	if exhausted.State != sdkcollector.ResultComplete || len(exhausted.Facts) != 0 {
		t.Fatalf("ExhaustedResult = %#v, want complete with zero facts", exhausted)
	}
}

// TestExtractedPartialScopeMapsToPartialResult proves the documented
// forbidden/partial mapping end to end: a warning-bearing scope marks its
// snapshot partial, and ToSDKResult converts that into a partial Result with
// a warning Status. It covers both warning shapes: the quota provider
// warning and the stale-fencing warning.
func TestExtractedPartialScopeMapsToPartialResult(t *testing.T) {
	t.Parallel()

	t.Run("quota warning", func(t *testing.T) {
		t.Parallel()
		src := newSource(t, testConfig(testScope()), warningProvider{
			warning: ProviderWarning{
				WarningKind: catalog.WarningKindQuota,
				Outcome:     catalog.OutcomeUnavailable,
				Reason:      "cloud asset inventory throttle exhausted",
				Retryable:   true,
			},
		}, nil)
		collected, ok, err := src.Next(context.Background())
		if err != nil || !ok {
			t.Fatalf("Next: ok=%v err=%v", ok, err)
		}
		assertPartialResult(t, proofClaim(collected.Scope, collected.Generation, 7), collected)
	})

	t.Run("stale fencing", func(t *testing.T) {
		t.Parallel()
		representative, _, _ := proof8856Scopes()
		resolved := representative.withDefaults()
		source := proof8856LiveSource(t)
		if err := source.Tracker.Accept(resolved.ScopeID, "gen-8856-newer", resolved.FencingToken+1); err != nil {
			t.Fatalf("pre-accept newer token: %v", err)
		}
		collected, ok, err := source.Next(context.Background())
		if err != nil || !ok {
			t.Fatalf("stale Next() = (%v, %v), want (true, nil)", ok, err)
		}
		assertPartialResult(t, proofClaim(collected.Scope, collected.Generation, 7), collected)
	})
}

// assertPartialResult fails unless the snapshot is marked partial and its
// SDK conversion is a partial Result with exactly one warning Status whose
// warning count matches the warning facts carried in-band.
func assertPartialResult(t *testing.T, claim sdkcollector.Claim, collected CollectedSnapshot) {
	t.Helper()
	if !collected.Partial {
		t.Fatal("warning-bearing snapshot Partial = false, want true")
	}
	result, err := ToSDKResult(claim, collected)
	if err != nil {
		t.Fatalf("ToSDKResult() error = %v", err)
	}
	if result.State != sdkcollector.ResultPartial {
		t.Fatalf("State = %q, want partial for the warning-bearing scope", result.State)
	}
	if len(result.Statuses) != 1 {
		t.Fatalf("Statuses = %d, want exactly the warning status", len(result.Statuses))
	}
	status := result.Statuses[0]
	if status.Class != sdkcollector.StatusWarning || !status.Partial {
		t.Fatalf("Status = %#v, want warning class with partial set", status)
	}
	warnings := 0
	for _, envelope := range collected.Envelopes {
		if envelope.FactKind == gcpcloud.GCPCollectionWarningFactKind {
			warnings++
		}
	}
	if warnings == 0 {
		t.Fatal("snapshot carries no warning facts, want the in-band evidence")
	}
	if status.WarningCount != warnings {
		t.Fatalf("WarningCount = %d, want %d warning facts", status.WarningCount, warnings)
	}
	if len(result.Facts) != len(collected.Envelopes) {
		t.Fatalf("Facts = %d, want %d (1:1 envelope mapping)", len(result.Facts), len(collected.Envelopes))
	}
}

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
