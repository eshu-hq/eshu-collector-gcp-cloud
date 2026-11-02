// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"
)

// TestOperatorHealth proves operators can inspect liveness, freshness,
// failures, and bounded resource use.
func TestOperatorHealth(t *testing.T) {
	t.Parallel()

	monitor := NewMonitor()
	health, resource := monitor.Snapshot()
	if !health.Alive {
		t.Fatal("Alive = false, want true")
	}
	if resource.MaxRecordsPerClaim <= 0 || resource.MaxPayloadBytes <= 0 || resource.ClaimTimeoutSeconds <= 0 || resource.MaxPageBytes <= 0 {
		t.Fatalf("resource bounds unset: %+v", resource)
	}
	at := time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)
	monitor.ObserveSuccess("digest-1", at)
	monitor.ObserveFailure("transient source timeout")
	health, _ = monitor.Snapshot()
	if !health.LastSuccessAt.Equal(at) {
		t.Fatalf("LastSuccessAt = %v, want %v", health.LastSuccessAt, at)
	}
	if health.LastDigest != "digest-1" {
		t.Fatalf("LastDigest = %q, want digest-1", health.LastDigest)
	}
	if health.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1", health.ConsecutiveFailures)
	}
	if health.LastError == "" {
		t.Fatal("LastError empty after failure")
	}
	// A later success clears the failure streak and the error.
	monitor.ObserveSuccess("digest-2", at)
	health, resource = monitor.Snapshot()
	if health.ConsecutiveFailures != 0 || health.LastError != "" {
		t.Fatalf("success did not clear failures: %+v", health)
	}
	// The snapshot reports the effective bounds for the mode in use.
	custom := DefaultResourceUse()
	custom.MaxRecordsPerClaim = 7
	monitor.SetResource(custom)
	if _, resource = monitor.Snapshot(); resource.MaxRecordsPerClaim != 7 {
		t.Fatalf("resource = %+v, want the effective bound", resource)
	}
}
