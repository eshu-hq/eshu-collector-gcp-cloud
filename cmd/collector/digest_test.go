// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

func digestEvidenceForTest(t *testing.T, keys []string) string {
	t.Helper()
	envelopes := make([]gcpcloud.Envelope, 0, len(keys))
	for _, key := range keys {
		envelopes = append(envelopes, gcpcloud.Envelope{
			FactKind:      gcpcloud.GCPCloudResourceFactKind,
			StableFactKey: key,
			Payload:       map[string]any{"k": key},
		})
	}
	return digestEvidence(envelopes)
}

// TestHealthDigestPrefersOutcomeDigest pins the freshness contract: the
// health digest comes from the evidence digest the collection computed,
// covering exactly the facts the host admits. Terminal and unchanged
// outcomes record nothing.
func TestHealthDigestPrefersOutcomeDigest(t *testing.T) {
	t.Parallel()

	complete := sdk.Result{State: sdk.ResultComplete, Facts: []sdk.Fact{
		{Kind: "gcp_cloud_resource", StableKey: "evidence:legacy"},
	}}

	if got := healthDigest("gcp:abc", complete); got != "gcp:abc" {
		t.Fatalf("healthDigest(evidence) = %q, want gcp:abc", got)
	}
	if got := healthDigest("", complete); got != "" {
		t.Fatalf("healthDigest(empty) = %q, want empty (no digest, no success)", got)
	}
	if got := healthDigest("gcp:abc", sdk.Result{State: sdk.ResultTerminal}); got != "" {
		t.Fatalf("healthDigest(terminal) = %q, want empty", got)
	}
	if got := healthDigest("gcp:abc", sdk.Result{State: sdk.ResultUnchanged}); got != "" {
		t.Fatalf("healthDigest(unchanged) = %q, want empty", got)
	}
}

// TestUnchangedOutcomeMatchesEvidenceDigest pins unchanged detection on the
// evidence digest: a matching previous digest short-circuits to
// ResultUnchanged with no facts, anything else proceeds.
func TestUnchangedOutcomeMatchesEvidenceDigest(t *testing.T) {
	t.Parallel()

	claim := demoClaim()
	unchanged, done := unchangedOutcome(claim, "gcp:abc", "gcp:abc")
	if !done {
		t.Fatal("matching digest did not short-circuit")
	}
	if unchanged.State != sdk.ResultUnchanged || len(unchanged.Facts) != 0 {
		t.Fatalf("short-circuit state = %v facts = %d, want Unchanged with 0 facts", unchanged.State, len(unchanged.Facts))
	}
	if _, done := unchangedOutcome(claim, "gcp:abc", "gcp:def"); done {
		t.Fatal("mismatched digest short-circuited")
	}
	if _, done := unchangedOutcome(claim, "", "gcp:abc"); done {
		t.Fatal("empty previous digest short-circuited")
	}
	if _, done := unchangedOutcome(claim, "gcp:abc", ""); done {
		t.Fatal("empty outcome digest short-circuited")
	}
}

// TestDigestEvidenceIsDeterministic proves the same envelope set always
// yields the same digest regardless of input order, so a replayed page
// reads as unchanged instead of churning the freshness signal.
func TestDigestEvidenceIsDeterministic(t *testing.T) {
	t.Parallel()

	first := digestEvidenceForTest(t, []string{"b", "a", "c"})
	second := digestEvidenceForTest(t, []string{"c", "a", "b"})
	if first == "" || first != second {
		t.Fatalf("digests = %q, %q, want equal non-empty", first, second)
	}
	if third := digestEvidenceForTest(t, []string{"a", "b"}); third == first {
		t.Fatalf("digest did not move when the evidence shrank: %q", third)
	}
}
