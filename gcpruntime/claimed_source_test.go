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

// TestSourceBindClaimStampsDrainGeneration is the eshu-hq/eshu#8856
// production regression: the live entry drains static scopes without claim
// identity, so the drained generation echoes a derived digest instead of the
// host claim generation and the SDK host validator fails every run
// terminally with invalid_result. Binding the host claim before the drain
// must stamp the claim generation (and fencing token) onto every scope the
// drain collects, including each emitted envelope.
func TestSourceBindClaimStampsDrainGeneration(t *testing.T) {
	t.Parallel()

	scopeCfg := testScope()
	scopeCfg.GenerationID = ""
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
	claim.GenerationID = "component-generation:regression-8856"
	claim.SourceRunID = claim.GenerationID
	if err := source.BindClaim(claim); err != nil {
		t.Fatalf("BindClaim() error = %v, want nil", err)
	}
	drained := 0
	for {
		collected, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v, want nil", err)
		}
		if !ok {
			break
		}
		drained++
		if got := collected.Generation.GenerationID; got != claim.GenerationID {
			t.Fatalf("GenerationID = %q, want claim generation %q", got, claim.GenerationID)
		}
		for _, env := range collected.Envelopes {
			if got := env.SourceRef.GenerationID; got != claim.GenerationID {
				t.Fatalf("envelope generation = %q, want claim generation %q", got, claim.GenerationID)
			}
		}
	}
	if drained == 0 {
		t.Fatal("drained 0 snapshots, want at least one bound to the claim")
	}
}

// TestSourceBindClaimRejectsForeignIdentity proves a claim that is not the
// host's own for this collector fails closed and leaves the static scopes
// untouched, so a foreign or malformed claim can never be stamped onto a
// drain.
func TestSourceBindClaimRejectsForeignIdentity(t *testing.T) {
	t.Parallel()

	scopeCfg := testScope()
	scopeCfg.GenerationID = ""
	scopeCfg.FencingToken = 0
	scopeID := scopeCfg.withDefaults().ScopeID
	newUnboundSource := func(t *testing.T) *Source {
		t.Helper()
		return newSource(
			t,
			testConfig(scopeCfg),
			NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
				scopeID: {readFixturePage(t, "assets_list_page1.json")},
			}),
			nil,
		)
	}
	cases := []struct {
		name   string
		mutate func(sdkcollector.Claim) sdkcollector.Claim
	}{
		{"wrong collector kind", func(c sdkcollector.Claim) sdkcollector.Claim { c.CollectorKind = "aws"; return c }},
		{"wrong source system", func(c sdkcollector.Claim) sdkcollector.Claim { c.SourceSystem = "aws"; return c }},
		{"wrong instance", func(c sdkcollector.Claim) sdkcollector.Claim { c.InstanceID = "gcp-instance-2"; return c }},
		{"blank generation", func(c sdkcollector.Claim) sdkcollector.Claim { c.GenerationID = ""; return c }},
		{"source run mismatch", func(c sdkcollector.Claim) sdkcollector.Claim { c.SourceRunID = "other-run"; return c }},
		{"zero fencing token", func(c sdkcollector.Claim) sdkcollector.Claim { c.FencingToken = "0"; return c }},
		{"negative fencing token", func(c sdkcollector.Claim) sdkcollector.Claim { c.FencingToken = "-3"; return c }},
		{"unparseable fencing token", func(c sdkcollector.Claim) sdkcollector.Claim { c.FencingToken = "not-an-int"; return c }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := newUnboundSource(t)
			if err := source.BindClaim(tc.mutate(gcpClaim(scopeID))); err == nil {
				t.Fatal("BindClaim() succeeded, want fail-closed rejection")
			}
			for _, scope := range source.Config.Scopes {
				if scope.GenerationID != "" || scope.FencingToken != 0 {
					t.Fatalf("rejected BindClaim mutated scope to generation %q token %d, want untouched",
						scope.GenerationID, scope.FencingToken)
				}
			}
		})
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
