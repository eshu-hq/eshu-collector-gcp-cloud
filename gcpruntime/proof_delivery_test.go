// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
)

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
