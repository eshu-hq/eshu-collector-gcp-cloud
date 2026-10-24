// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"fmt"
	"strings"
	"time"

	sdkidentity "github.com/eshu-hq/eshu-sdk/factschema/identity"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
)

// stableID returns a deterministic identifier for one fact-adjacent payload,
// resolving through the published factschema identity package (byte-identical
// derivation to the in-tree facts.StableID).
func stableID(factType string, identity map[string]any) string {
	return sdkidentity.StableID(factType, identity)
}

// Provenance: collector-owned snapshot model for the extracted GCP runtime.
// It replaces collector.CollectedGeneration / scope.IngestionScope /
// scope.ScopeGeneration with identical literal values; the SDK wire form is
// built by ToSDKResult in sdkresult.go.

// ScopeInfo is the collector-owned view of the ingestion scope for one GCP
// parent scope snapshot.
type ScopeInfo struct {
	ScopeID       string
	SourceSystem  string
	ScopeKind     string
	CollectorKind string
	PartitionKey  string
	Metadata      map[string]string
}

// GenerationInfo is the collector-owned view of one snapshot generation.
type GenerationInfo struct {
	ScopeID       string
	GenerationID  string
	Status        string
	ObservedAt    time.Time
	IngestedAt    time.Time
	TriggerKind   string
	FreshnessHint string
}

// CollectedSnapshot is one parent-scope snapshot generation: its scope, its
// generation, and its envelopes in emission order.
type CollectedSnapshot struct {
	Scope      ScopeInfo
	Generation GenerationInfo
	Envelopes  []gcpcloud.Envelope
	Partial    bool
}

// CollectorKind is the durable collector family identifier for facts and
// scopes emitted by the GCP cloud collector (in-tree scope.CollectorGCP).
const CollectorKind = "gcp"

// PostureCollectorKind is the durable collector_kind for the secrets/IAM
// mirror facts this collector also emits (in-tree posture.CollectorKind).
const PostureCollectorKind = "secrets_iam_posture"

// Validate checks the minimum scope identity fields.
func (s ScopeInfo) Validate() error {
	if strings.TrimSpace(s.ScopeID) == "" {
		return fmt.Errorf("scope_id must not be blank")
	}
	if strings.TrimSpace(s.ScopeKind) == "" {
		return fmt.Errorf("scope_kind must not be blank")
	}
	if strings.TrimSpace(s.CollectorKind) == "" {
		return fmt.Errorf("collector_kind must not be blank")
	}
	return nil
}

// Validate checks the minimum generation identity fields.
func (g GenerationInfo) Validate() error {
	if strings.TrimSpace(g.GenerationID) == "" {
		return fmt.Errorf("generation_id must not be blank")
	}
	if strings.TrimSpace(g.ScopeID) == "" {
		return fmt.Errorf("generation scope_id must not be blank")
	}
	return nil
}

// ValidateForScope checks generation/scope identity consistency, mirroring
// the in-tree scope generation check the host used to run.
func (g GenerationInfo) ValidateForScope(scope ScopeInfo) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := g.Validate(); err != nil {
		return err
	}
	if g.ScopeID != scope.ScopeID {
		return fmt.Errorf("generation scope_id %q does not match scope scope_id %q", g.ScopeID, scope.ScopeID)
	}
	return nil
}
