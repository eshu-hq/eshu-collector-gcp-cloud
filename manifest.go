// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	_ "embed"
)

const (
	// ComponentID is this collector's manifest identity. It stays in sync
	// with manifest.yaml metadata.id.
	ComponentID = "collector-gcp-cloud"
	// CollectorKind is the GCP collector family (scope.CollectorGCP).
	CollectorKind = "gcp"
	// SourceSystem names the observed source: the GCP APIs this collector scans.
	SourceSystem = "gcp"
	// MetricsPrefix is the component-owned metric prefix from the manifest.
	MetricsPrefix = "eshu_dp_gcp_cloud_collector_"
)

// ManifestYAML is the embedded collector manifest (manifest.yaml) that the
// seed installer stamps with the deployed image reference at install time.
//
//go:embed manifest.yaml
var ManifestYAML []byte
