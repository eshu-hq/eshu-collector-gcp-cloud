// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"strconv"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud/catalog"
)

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
