// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extractor

import (
	sdkidentity "github.com/eshu-hq/eshu-sdk/factschema/identity"
)

// Provenance: stable-ID derivation resolving through the published
// factschema identity package (byte-identical to the in-tree
// facts.StableID); see ../stable.go.

// stableID returns a deterministic identifier for one fact-adjacent payload.
func stableID(factType string, identity map[string]any) string {
	return sdkidentity.StableID(factType, identity)
}
