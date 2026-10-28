// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud/extractor"
)

// TestSDKPinsResolve proves the module builds against the exact published
// pins with no replace directives, and that the pinned fixture pack ships
// schemas for every fact kind this collector emits.
func TestSDKPinsResolve(t *testing.T) {
	t.Parallel()

	_ = sdkcollector.ResultState("complete")
	for _, kind := range []string{
		"gcp_cloud_resource",
		"gcp_cloud_relationship",
		"gcp_tag_observation",
		"gcp_iam_policy_observation",
		"gcp_dns_record",
		"gcp_image_reference",
		"gcp_collection_warning",
		"gcp_iam_principal",
		"gcp_iam_trust_policy",
		"gcp_iam_permission_policy",
	} {
		schema, ok := fixturepack.SchemaFor(kind)
		if !ok {
			t.Fatalf("pinned fixture pack ships no schema for %q", kind)
		}
		if len(schema) == 0 {
			t.Fatalf("pinned fixture pack ships empty schema for %q", kind)
		}
	}
}

// TestExtractorRegistrySurvivesExtraction proves every per-asset-type
// extractor registration traveled with the port: each distinct CAI asset type
// appearing in the extractor fixture corpus resolves to a registered
// extractor. The registrations live in init() functions across the 67
// extractor files; dropping a file would silently stop extracting that type,
// so the corpus — not a hand-kept list — is the oracle.
func TestExtractorRegistrySurvivesExtraction(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(filepath.Join("..", "gcpcloud", "testdata"))
	if err != nil {
		t.Fatalf("read extractor fixture corpus: %v", err)
	}
	assetTypes := map[string]bool{}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join("..", "gcpcloud", "testdata", entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var doc struct {
			Assets []struct {
				AssetType string `json:"assetType"`
			} `json:"assets"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		for _, asset := range doc.Assets {
			if asset.AssetType != "" {
				assetTypes[asset.AssetType] = true
			}
		}
	}
	if len(assetTypes) == 0 {
		t.Fatal("no asset types found in extractor fixture corpus")
	}
	sorted := make([]string, 0, len(assetTypes))
	for assetType := range assetTypes {
		sorted = append(sorted, assetType)
	}
	sort.Strings(sorted)
	t.Logf("corpus spans %d asset types", len(sorted))
	// dedicatedPaths holds corpus asset types that in-tree intentionally
	// routes around the extractor registry through a dedicated parse path.
	// dns.googleapis.com/ResourceRecordSet is handled by parseDNSRecords
	// (gcpcloud/parse.go), documented in catalog/gcp_platform.go; the port
	// preserves that dispatch, so the registry must NOT claim it.
	// run.googleapis.com/Job has no registry extractor in-tree either: it
	// is covered only by the parseImageReferences dedicated path
	// (isCloudRunRuntimeAsset in gcpcloud/parse.go), which the port keeps.
	// Any other unregistered corpus type is a dropped extractor file.
	dedicatedPaths := map[string]bool{
		"dns.googleapis.com/ResourceRecordSet": true,
		"run.googleapis.com/Job":               true,
	}
	for _, assetType := range sorted {
		if dedicatedPaths[assetType] {
			continue
		}
		if !extractor.HasAssetExtractor(assetType) {
			t.Fatalf("no extractor registered for corpus asset type %q", assetType)
		}
	}
}
