// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud/catalog"
)

// Provenance: fixture builders ported from
// gcpruntime/source_parity_8856_test.go at core commit 57d11add46. Only the
// assertions in proof_parity_test.go are new: they compare the extracted
// producer against the copied fixture-pack oracle instead of the in-tree
// cassette reader.

var (
	proof8856UpdateAt = time.Date(2026, 6, 9, 11, 50, 0, 0, time.UTC)
	proof8856ReadAt   = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
)

// proof8856Provider serves the representative fixture pages and scripted
// hostile outcomes per scope: a dangling continuation token for the expired
// scope and a typed provider warning for the quota scope.
type proof8856Provider struct {
	fixture *FixturePageProvider
	warning map[string]ProviderWarning
}

func (p *proof8856Provider) FetchPage(ctx context.Context, req PageRequest) (gcpcloud.AssetsListPage, error) {
	if warning, ok := p.warning[req.Scope.ScopeID]; ok {
		return gcpcloud.AssetsListPage{}, warning
	}
	return p.fixture.FetchPage(ctx, req)
}

// proof8856Scopes builds the three pack scopes: one representative mix that
// spans every emitted fact family, one page-token-expiry hostile, and one
// quota-warning hostile. Generation IDs are explicit so the pack is stable
// independent of the clock.
func proof8856Scopes() (representative, expired, quota ScopeConfig) {
	representative = ScopeConfig{
		ParentScopeKind: catalog.ParentScopeProject,
		ParentScopeID:   "parity-proj",
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    "gen-8856-representative",
		FencingToken:    7,
		CredentialRef:   "parity-readonly",
	}
	expired = ScopeConfig{
		ParentScopeKind: catalog.ParentScopeProject,
		ParentScopeID:   "expired-proj",
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    "gen-8856-expired",
		FencingToken:    9,
		CredentialRef:   "parity-readonly",
	}
	quota = ScopeConfig{
		ParentScopeKind: catalog.ParentScopeProject,
		ParentScopeID:   "quota-proj",
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    "gen-8856-quota",
		FencingToken:    11,
		CredentialRef:   "parity-readonly",
	}
	return representative, expired, quota
}

// proof8856LiveSource rebuilds the exact source the fixture pack was cut
// from: same scopes, same pages, same fixed clock and redaction key, so
// generation IDs and stable keys reproduce deterministically.
func proof8856LiveSource(t *testing.T) *Source {
	t.Helper()
	representative, expired, quota := proof8856Scopes()
	resolved := func(s ScopeConfig) ScopeConfig { return s.withDefaults() }
	pagesByScope := map[string][]gcpcloud.AssetsListPage{
		resolved(representative).ScopeID: {
			{ReadTime: proof8856ReadAt, Resources: proof8856PageOne(), NextPageToken: "PARITY-P2"},
			{ReadTime: proof8856ReadAt, Resources: proof8856PageTwo()},
		},
		// A single page whose continuation token dangles: resuming it is a
		// page-token-expired warning, not silent truncation.
		resolved(expired).ScopeID: {
			{ReadTime: proof8856ReadAt, Resources: proof8856ExpiredPage(), NextPageToken: "DANGLING"},
		},
	}
	return &Source{
		Config: Config{
			CollectorInstanceID: "gcp-parity-8856",
			PollInterval:        time.Minute,
			Scopes:              []ScopeConfig{representative, expired, quota},
		},
		Provider: &proof8856Provider{
			fixture: NewFixturePageProvider(pagesByScope),
			warning: map[string]ProviderWarning{
				resolved(quota).ScopeID: {
					WarningKind: catalog.WarningKindQuota,
					Outcome:     catalog.OutcomeUnavailable,
					Reason:      "CAI quota exhausted for scope",
					Retryable:   true,
					SourceURI:   "cai://assets.list",
				},
			},
		},
		RedactionKey: testRedactionKey(t),
		Tracker:      gcpcloud.NewGenerationTracker(),
		Metrics:      testMetrics(t),
		Clock:        fixedClock(),
	}
}

func proof8856PageOne() []gcpcloud.ResourceObservation {
	return []gcpcloud.ResourceObservation{
		{
			Name:        "//compute.googleapis.com/projects/parity-proj/zones/us-central1-a/instances/parity-vm",
			AssetType:   "compute.googleapis.com/Instance",
			DisplayName: "parity-vm",
			State:       "RUNNING",
			Location:    "us-central1-a",
			Ancestors:   []string{"projects/424242", "organizations/9988776655"},
			Labels:      map[string]string{"env": "prod"},
			IAMPolicyBindings: []gcpcloud.IAMPolicyBindingObservation{{
				Role:    "roles/compute.viewer",
				Members: []string{"serviceAccount:reader@parity-proj.iam.gserviceaccount.com"},
			}},
			Relationships: []gcpcloud.RelationshipObservation{{
				RelationshipType:       "USES",
				TargetFullResourceName: "//storage.googleapis.com/projects/_/buckets/parity-bucket",
				TargetAssetType:        "storage.googleapis.com/Bucket",
			}},
			UpdateTime: proof8856UpdateAt,
			SourceURI:  "cai://assets.list/parity",
		},
		{
			Name:        "//storage.googleapis.com/projects/_/buckets/parity-bucket",
			AssetType:   "storage.googleapis.com/Bucket",
			DisplayName: "parity-bucket",
			Location:    "us-central1",
			Ancestors:   []string{"projects/424242", "organizations/9988776655"},
			Labels:      map[string]string{"team": "data"},
			UpdateTime:  proof8856UpdateAt,
			SourceURI:   "cai://assets.list/parity",
		},
	}
}

func proof8856PageTwo() []gcpcloud.ResourceObservation {
	return []gcpcloud.ResourceObservation{
		{
			Name:      "//dns.googleapis.com/projects/parity-proj/managedZones/parity-zone",
			AssetType: "dns.googleapis.com/ManagedZone",
			Location:  "global",
			Ancestors: []string{"projects/424242", "organizations/9988776655"},
			DNSRecords: []gcpcloud.DNSRecordObservation{{
				ManagedZoneFullResourceName: "//dns.googleapis.com/projects/parity-proj/managedZones/parity-zone",
				RecordType:                  "A",
				RecordName:                  "www.example.test.",
				Targets:                     []string{"203.0.113.10"},
				TTLSeconds:                  300,
			}},
			UpdateTime: proof8856UpdateAt,
			SourceURI:  "cai://assets.list/parity",
		},
		{
			Name:      "//run.googleapis.com/projects/parity-proj/locations/us-central1/services/parity-svc",
			AssetType: "run.googleapis.com/Service",
			Location:  "us-central1",
			Ancestors: []string{"projects/424242", "organizations/9988776655"},
			ImageReferences: []gcpcloud.ImageReferenceObservation{{
				ImageReference: "us-central1-docker.pkg.dev/parity-proj/repo/svc:1.2.3",
				ImageDigest:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ContainerName:  "parity-web-sidecar",
			}},
			UpdateTime: proof8856UpdateAt,
			SourceURI:  "cai://assets.list/parity",
		},
		{
			Name:                "//iam.googleapis.com/projects/parity-proj/serviceAccounts/deployer@parity-proj.iam.gserviceaccount.com",
			AssetType:           catalog.ServiceAccountAssetType,
			Ancestors:           []string{"projects/424242", "organizations/9988776655"},
			ServiceAccountEmail: "deployer@parity-proj.iam.gserviceaccount.com",
			IAMPolicyBindings: []gcpcloud.IAMPolicyBindingObservation{{
				Role:    "roles/iam.serviceAccountTokenCreator",
				Members: []string{"serviceAccount:ci@parity-proj.iam.gserviceaccount.com"},
			}},
			UpdateTime: proof8856UpdateAt,
			SourceURI:  "cai://assets.list/parity",
		},
	}
}

func proof8856ExpiredPage() []gcpcloud.ResourceObservation {
	return []gcpcloud.ResourceObservation{
		{
			Name:        "//storage.googleapis.com/projects/_/buckets/expired-bucket",
			AssetType:   "storage.googleapis.com/Bucket",
			DisplayName: "expired-bucket",
			Location:    "us-central1",
			Ancestors:   []string{"projects/777", "organizations/9988776655"},
			Labels:      map[string]string{"team": "expired"},
			UpdateTime:  proof8856UpdateAt,
			SourceURI:   "cai://assets.list/expired",
		},
	}
}
