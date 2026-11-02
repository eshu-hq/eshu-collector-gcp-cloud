// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
)

// TestSourceDrainStampsScopeSourceURI is the eshu-hq/eshu#8856 follow-up
// regression: resource observations leave the extractors without a source
// URI, and the SDK host validator fails every result terminally with
// invalid_result when a fact source_ref.uri is blank. The drain must stamp
// the collection-scope URI onto every resource envelope it builds.
func TestSourceDrainStampsScopeSourceURI(t *testing.T) {
	t.Parallel()

	scopeCfg := testScope().withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		scopeCfg.ScopeID: {
			readFixturePage(t, "assets_list_page1.json"),
		},
	})
	src := newSource(t, testConfig(testScope()), provider, nil)

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("Next returned ok=false, want a generation")
	}
	envs := drainFacts(t, collected)
	if len(envs) == 0 {
		t.Fatal("drained 0 envelopes, want resource facts with a source URI")
	}
	for _, env := range envs {
		if env.FactKind != gcpcloud.GCPCloudResourceFactKind {
			continue
		}
		if got, want := env.SourceRef.SourceURI, "cai://assets.list/my-project"; got != want {
			t.Fatalf("resource source URI = %q, want %q", got, want)
		}
	}
}

// TestStampSourceURIPreservesExplicitURI proves the drain stamp only fills
// blanks: an extractor-set URI wins, and a scope without a parent id falls
// back to the bare collection endpoint.
func TestStampSourceURIPreservesExplicitURI(t *testing.T) {
	t.Parallel()

	resources := []gcpcloud.ResourceObservation{
		{Name: "explicit", SourceURI: "cai://assets.list/custom"},
		{Name: "blank"},
	}
	stampSourceURI(resources, testScope())
	if got := resources[1].SourceURI; got != "cai://assets.list/my-project" {
		t.Fatalf("blank source URI = %q, want scope URI", got)
	}
	if got := resources[0].SourceURI; got != "cai://assets.list/custom" {
		t.Fatalf("explicit source URI = %q, want preserved", got)
	}
	bare := []gcpcloud.ResourceObservation{{Name: "bare"}}
	noparent := testScope()
	noparent.ParentScopeID = ""
	stampSourceURI(bare, noparent)
	if got := bare[0].SourceURI; got != "cai://assets.list" {
		t.Fatalf("parentless source URI = %q, want bare endpoint", got)
	}
}
