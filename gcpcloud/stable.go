// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	sdkidentity "github.com/eshu-hq/eshu-sdk/factschema/identity"
)

// Provenance: stable-ID derivation ported from go/internal/facts/encode/stable.go
// (via go/internal/facts/stableid.go) at core commit 57d11add46. The
// implementation now resolves through the published factschema identity
// package, whose StableID body is identical to the in-tree one; the parity
// proof test pins byte-identical IDs on the fixture pack.

// stableID returns a deterministic identifier for one fact-adjacent payload.
func stableID(factType string, identity map[string]any) string {
	return sdkidentity.StableID(factType, identity)
}
