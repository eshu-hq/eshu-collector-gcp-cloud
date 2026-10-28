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
	"testing"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
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
