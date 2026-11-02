// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"testing"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud/catalog"
)

func testRestampContext() GCPEnvelopeContext {
	return GCPEnvelopeContext{
		ScopeID:             "gcp:project:boat-trader:mixed:resource:global",
		GenerationID:        "gen-8856",
		CollectorInstanceID: "collector-8856",
		FencingToken:        7,
	}
}

// TestMirrorEnvelopesRestampToGCP pins the #8856 cutover shape: the three
// secrets/IAM mirror envelopes this collector emits must stamp
// collector_kind "gcp" while keeping SourceSystem "secrets_iam_posture".
// Removing the re-stamp must fail this test.
func TestMirrorEnvelopesRestampToGCP(t *testing.T) {
	t.Parallel()

	ctx := testRestampContext()

	principal, err := NewGCPPrincipalEnvelope(GCPPrincipalObservation{
		Context:              ctx,
		PrincipalFingerprint: "fp-principal-8856",
	})
	if err != nil {
		t.Fatalf("NewGCPPrincipalEnvelope: %v", err)
	}

	permission, err := NewGCPPermissionPolicyEnvelope(GCPPermissionPolicyObservation{
		Context:              ctx,
		PrincipalFingerprint: "fp-principal-8856",
		Role:                 "roles/viewer",
		ResourceFullName:     "//cloudresourcemanager.googleapis.com/projects/boat-trader",
	})
	if err != nil {
		t.Fatalf("NewGCPPermissionPolicyEnvelope: %v", err)
	}

	trust, err := NewGCPTrustPolicyEnvelope(GCPTrustPolicyObservation{
		Context:                         ctx,
		TargetPrincipalFingerprint:      "fp-target-8856",
		TrustedMemberFingerprint:        "fp-trusted-8856",
		Role:                            "roles/iam.workloadIdentityUser",
		ImpersonationMode:               "workload_identity",
		TargetServiceAccountEmailDigest: "digest-8856",
	})
	if err != nil {
		t.Fatalf("NewGCPTrustPolicyEnvelope: %v", err)
	}

	for name, env := range map[string]Envelope{
		"principal":  principal,
		"permission": permission,
		"trust":      trust,
	} {
		if env.CollectorKind != catalog.CollectorKind {
			t.Errorf("mirror %s stamps collector_kind %q, want %q", name, env.CollectorKind, catalog.CollectorKind)
		}
		if env.SourceRef.SourceSystem != PostureCollectorKind {
			t.Errorf("mirror %s has SourceSystem %q, want %q", name, env.SourceRef.SourceSystem, PostureCollectorKind)
		}
	}
}
