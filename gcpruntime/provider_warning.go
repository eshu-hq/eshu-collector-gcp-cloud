// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import "fmt"

// Provenance: ProviderWarning ported from
// go/internal/collector/gcpcloud/gcpruntime/liveclient.go at core commit
// 57d11add46. Only the warning carrier travels with this extraction; the
// LiveClient REST transport stays in core until the live-backend phase.

// ProviderWarning is a bounded PageProvider coverage warning. Source converts
// it into a durable gcp_collection_warning fact instead of treating expected
// provider coverage gaps as silent success.
type ProviderWarning struct {
	WarningKind string
	Outcome     string
	Reason      string
	Retryable   bool
	HiddenCount int
	SourceURI   string
}

// Error returns only bounded warning metadata. It never includes provider
// response bodies, credentials, parent identifiers, or resource names.
func (w ProviderWarning) Error() string {
	return fmt.Sprintf("gcp provider warning kind=%s outcome=%s", w.WarningKind, w.Outcome)
}
