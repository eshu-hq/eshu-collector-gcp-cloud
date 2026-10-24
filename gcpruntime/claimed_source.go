// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// NextClaimed collects claim-driven GCP Cloud Asset Inventory work. The
// static config authorizes scopes and credential references; the SDK claim
// supplies generation and fencing identity. Provenance: ported from
// gcpruntime/claimed_source.go at core commit 57d11add46 with the host
// workflow.WorkItem exchanged for sdkcollector.Claim:
//
//   - CollectorKind/SourceSystem compare against the "gcp" literal (was
//     scope.CollectorGCP).
//   - InstanceID carries the collector instance identity (was
//     WorkItem.CollectorInstanceID).
//   - The claimed-status gate is dropped: a delivered SDK claim IS claimed
//     work; status lives host-side now.
//   - AcceptanceUnitID has no SDK-claim equivalent and is dropped; scope
//     authorization stays config-side, which is the cutover source of truth.
//   - FencingToken arrives as a formatted string (SHIM_MAP section 6.7) and
//     must parse to a positive int64, else collection fails closed.
func (s *Source) NextClaimed(
	ctx context.Context,
	claim sdkcollector.Claim,
) (CollectedSnapshot, bool, error) {
	scopeCfg, err := s.scopeForClaim(claim)
	if err != nil {
		return CollectedSnapshot{}, false, err
	}
	collected, err := s.collectScope(ctx, scopeCfg)
	if err != nil {
		return CollectedSnapshot{}, false, err
	}
	return collected, true, nil
}

func (s *Source) scopeForClaim(claim sdkcollector.Claim) (ScopeConfig, error) {
	if strings.TrimSpace(s.Config.CollectorInstanceID) == "" {
		return ScopeConfig{}, fmt.Errorf("gcp collector instance id is required")
	}
	if s.Provider == nil {
		return ScopeConfig{}, errors.New("gcp collector page provider is required")
	}
	if s.RedactionKey.IsZero() {
		return ScopeConfig{}, errors.New("gcp collector redaction key is required")
	}
	if strings.TrimSpace(claim.CollectorKind) != CollectorKind {
		return ScopeConfig{}, fmt.Errorf("claimed collector_kind %q must be %q", claim.CollectorKind, CollectorKind)
	}
	if strings.TrimSpace(claim.SourceSystem) != CollectorKind {
		return ScopeConfig{}, fmt.Errorf("claimed source_system %q must be %q", claim.SourceSystem, CollectorKind)
	}
	if strings.TrimSpace(claim.InstanceID) != s.Config.CollectorInstanceID {
		return ScopeConfig{}, fmt.Errorf("claimed collector_instance_id %q must match configured GCP collector instance", claim.InstanceID)
	}
	scopeID := strings.TrimSpace(claim.Scope.ID)
	if scopeID == "" {
		return ScopeConfig{}, fmt.Errorf("claimed GCP scope_id is required")
	}
	fencingToken, err := strconv.ParseInt(strings.TrimSpace(string(claim.FencingToken)), 10, 64)
	if err != nil || fencingToken <= 0 {
		return ScopeConfig{}, fmt.Errorf("claimed GCP fencing token must be a positive int64")
	}
	generationID := strings.TrimSpace(claim.GenerationID)
	if generationID == "" || strings.TrimSpace(claim.SourceRunID) == "" {
		return ScopeConfig{}, fmt.Errorf("claimed GCP generation identity is required")
	}
	if strings.TrimSpace(claim.SourceRunID) != generationID {
		return ScopeConfig{}, fmt.Errorf("claimed GCP source_run_id must match generation_id")
	}

	for _, candidate := range s.Config.Scopes {
		resolved := candidate.withDefaults()
		if resolved.ScopeID != scopeID {
			continue
		}
		resolved.GenerationID = generationID
		resolved.FencingToken = fencingToken
		if err := resolved.validate(); err != nil {
			return ScopeConfig{}, fmt.Errorf("claimed GCP scope configuration: %w", err)
		}
		return resolved, nil
	}
	return ScopeConfig{}, fmt.Errorf("gcp claim scope_id is not configured")
}
