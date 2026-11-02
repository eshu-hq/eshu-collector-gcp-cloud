// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpruntime"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestDrainLiveSnapshotsEchoesBoundClaimGeneration is the eshu-hq/eshu#8856
// live-path regression: the host validates the returned generation against
// the claim it issued, so a bound drain merged into one SDK result must echo
// the claim generation, not the static scope generation. Without the
// BindClaim step the live entry drains unbound and every run fails
// terminally with invalid_result.
func TestDrainLiveSnapshotsEchoesBoundClaimGeneration(t *testing.T) {
	t.Parallel()

	scope := drainTestScope("drain-bound", "sa-drain")
	scope.GenerationID = ""
	scope.FencingToken = 0
	pages := map[string][]gcpcloud.AssetsListPage{
		"drain-bound": {drainTestPage(t, "assets_list_iam_policy.json")},
	}
	source, _ := drainTestSource(t, []gcpruntime.ScopeConfig{scope}, pages)
	claim := sdk.Claim{
		CollectorKind: "gcp",
		SourceSystem:  "gcp",
		InstanceID:    "gcp-cloud-drain-test",
		Scope:         sdk.Scope{ID: "gcp:project:drain-bound:mixed:resource:global", Kind: "account"},
		GenerationID:  "component-generation:regression-8856",
		SourceRunID:   "component-generation:regression-8856",
		FencingToken:  "41",
	}
	if err := source.BindClaim(claim); err != nil {
		t.Fatalf("BindClaim() error = %v, want nil", err)
	}
	merged, reason, err := drainLiveSnapshots(context.Background(), source, drainLimits(5000))
	if err != nil {
		t.Fatalf("drainLiveSnapshots: %v", err)
	}
	if reason != "" {
		t.Fatalf("terminal reason = %q, want empty on a healthy drain", reason)
	}
	result, _, err := collectSnapshotResult(claim, merged, drainLimits(5000), "", time.Now().UTC())
	if err != nil {
		t.Fatalf("collectSnapshotResult: %v", err)
	}
	if result.Generation.ID != claim.GenerationID {
		t.Fatalf("result generation = %q, want claim generation %q", result.Generation.ID, claim.GenerationID)
	}
}
