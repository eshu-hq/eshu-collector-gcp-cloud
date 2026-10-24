// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import "time"

// Provenance: shape ported from go/internal/facts/models.go (Envelope, Ref)
// at core commit 57d11add46. This is the collector-owned emission model;
// the wire contract is sdkcollector.Fact (see ../gcpruntime/sdkresult.go).

// Envelope is one durable fact envelope produced by this collector.
type Envelope struct {
	FactID           string
	ScopeID          string
	GenerationID     string
	FactKind         string
	StableFactKey    string
	SchemaVersion    string
	CollectorKind    string
	FencingToken     int64
	SourceConfidence string
	ObservedAt       time.Time
	Payload          map[string]any
	IsTombstone      bool
	SourceRef        Ref
}

// Ref identifies the source-local record that produced one fact.
type Ref struct {
	SourceSystem   string
	ScopeID        string
	GenerationID   string
	FactKey        string
	SourceURI      string
	SourceRecordID string
}

// SourceConfidenceReported is the only confidence this collector emits.
const SourceConfidenceReported = "reported"
