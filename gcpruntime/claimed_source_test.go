// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
)

// Provenance: ported from gcpruntime/claimed_source_test.go at core commit
// 57d11add46 with workflow.WorkItem exchanged for sdkcollector.Claim, per the
// scopeForClaim mapping. Expectations verbatim.

func TestSourceNextClaimedCollectsMatchingWorkItem(t *testing.T) {
	t.Parallel()

	scopeCfg := testScope()
	scopeCfg.FencingToken = 0
	scopeID := scopeCfg.withDefaults().ScopeID
	source := newSource(
		t,
		testConfig(scopeCfg),
		NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
			scopeID: {readFixturePage(t, "assets_list_page1.json")},
		}),
		nil,
	)

	claim := gcpClaim(scopeID)
	collected, ok, err := source.NextClaimed(context.Background(), claim)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.ScopeID, scopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := collected.Generation.GenerationID, claim.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want claim generation %q", got, want)
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, gcpcloud.GCPCloudResourceFactKind); got == 0 {
		t.Fatal("gcp_cloud_resource count = 0, want claimed source facts")
	}
}

func TestSourceNextClaimedRejectsUnauthorizedScope(t *testing.T) {
	t.Parallel()

	scopeCfg := testScope()
	scopeCfg.FencingToken = 0
	source := newSource(
		t,
		testConfig(scopeCfg),
		NewFixturePageProvider(nil),
		nil,
	)
	claim := gcpClaim("gcp:project:not-configured:mixed:resource:global")

	_, _, err := source.NextClaimed(context.Background(), claim)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want unauthorized scope rejection")
	}
	if strings.Contains(err.Error(), "my-project") {
		t.Fatalf("NextClaimed() leaked configured parent id in error: %q", err.Error())
	}
}

func TestSourceNextClaimedRejectsBadFencingToken(t *testing.T) {
	t.Parallel()

	scopeCfg := testScope()
	scopeCfg.FencingToken = 0
	scopeID := scopeCfg.withDefaults().ScopeID
	source := newSource(
		t,
		testConfig(scopeCfg),
		NewFixturePageProvider(nil),
		nil,
	)
	claim := gcpClaim(scopeID)
	claim.FencingToken = "not-an-int"

	if _, _, err := source.NextClaimed(context.Background(), claim); err == nil {
		t.Fatal("NextClaimed() with unparseable fencing token succeeded, want fail-closed rejection")
	}
}

func gcpClaim(scopeID string) sdkcollector.Claim {
	now := time.Date(2026, 6, 18, 16, 0, 0, 0, time.UTC)
	return sdkcollector.Claim{
		ComponentID:   "dev.eshu.collector.gcp-cloud",
		InstanceID:    "gcp-instance-1",
		CollectorKind: CollectorKind,
		SourceSystem:  CollectorKind,
		Scope:         sdkcollector.Scope{ID: scopeID, Kind: "account"},
		SourceRunID:   "gcp-generation-1",
		GenerationID:  "gcp-generation-1",
		WorkItemID:    "gcp:gcp-instance-1:gcp-generation-1",
		FencingToken:  strconv.FormatInt(41, 10),
		Attempt:       1,
		Deadline:      now.Add(time.Hour),
		ConfigHandle:  "proof-config:gcp-cloud",
	}
}
