// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"fmt"
	"strconv"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
)

// This file is new in the extraction (no in-tree origin): it is the boundary
// where the collector-owned emission model meets the published SDK wire
// contract. Per SHIM_MAP section 1 (eshu-hq/eshu#8856), in-tree outcomes map
// to SDK ResultState as follows:
//
//   - envelopes committed -> "complete"
//   - ok=false, batch exhausted -> "complete" with zero facts, or "unchanged"
//   - hard error (ping failure, missing auth, invalid config) -> "terminal"
//   - forbidden/partial family list -> "partial" + warning Status +
//     gcp_collection_warning facts (already in-band in the envelopes)
//   - cancellation -> no result
//
// The retryable set for transient API-server errors is a cutover follow-up
// (SHIM_MAP section 6.4); this proof returns what the source yields and lets
// the host classify transport errors.

// SDKProtocolVersion is the collector extension wire contract this producer
// speaks (mirrors the SDK const so a bump is a deliberate edit).
const SDKProtocolVersion = sdkcollector.ProtocolVersionV1Alpha1

// ToSDKResult converts one collected snapshot into the SDK Result wire form
// for the given claim. The claim carries the core-owned work-item identity;
// the snapshot carries the observed evidence. FencingToken arrives on the
// claim as a string (SHIM_MAP section 6.7) and must round-trip exactly: a
// claim fencing token that is present but unparseable as an int64 is a
// terminal caller error, never a silent zero.
func ToSDKResult(claim sdkcollector.Claim, snap CollectedSnapshot) (sdkcollector.Result, error) {
	if strings.TrimSpace(string(claim.FencingToken)) != "" {
		if _, err := strconv.ParseInt(strings.TrimSpace(string(claim.FencingToken)), 10, 64); err != nil {
			return sdkcollector.Result{}, fmt.Errorf("claim fencing_token %q does not round-trip to int64: %w", claim.FencingToken, err)
		}
	}
	state := sdkcollector.ResultComplete
	if snap.Partial {
		state = sdkcollector.ResultPartial
	}
	facts := make([]sdkcollector.Fact, 0, len(snap.Envelopes))
	for _, envelope := range snap.Envelopes {
		facts = append(facts, sdkFact(claim, envelope))
	}
	result := sdkcollector.Result{
		ProtocolVersion: SDKProtocolVersion,
		State:           state,
		Claim:           claim,
		Generation: sdkcollector.Generation{
			ID:            snap.Generation.GenerationID,
			ObservedAt:    snap.Generation.ObservedAt,
			FreshnessHint: snap.Generation.FreshnessHint,
		},
		Facts: facts,
	}
	if snap.Partial {
		result.Statuses = []sdkcollector.Status{{
			Class:        sdkcollector.StatusWarning,
			Partial:      true,
			WarningCount: countWarnings(snap.Envelopes),
			FactCount:    len(facts),
		}}
	}
	return result, nil
}

// ExhaustedResult builds the batch-drained result: complete with zero facts.
func ExhaustedResult(claim sdkcollector.Claim) sdkcollector.Result {
	return sdkcollector.Result{
		ProtocolVersion: SDKProtocolVersion,
		State:           sdkcollector.ResultComplete,
		Claim:           claim,
	}
}

func sdkFact(claim sdkcollector.Claim, envelope gcpcloud.Envelope) sdkcollector.Fact {
	return sdkcollector.Fact{
		Kind:             envelope.FactKind,
		SchemaVersion:    envelope.SchemaVersion,
		StableKey:        envelope.StableFactKey,
		SourceConfidence: sdkcollector.SourceConfidence(envelope.SourceConfidence),
		ObservedAt:       envelope.ObservedAt,
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: claim.SourceSystem,
			ScopeID:      envelope.SourceRef.ScopeID,
			GenerationID: envelope.SourceRef.GenerationID,
			FactKey:      envelope.SourceRef.FactKey,
			URI:          envelope.SourceRef.SourceURI,
			RecordID:     envelope.SourceRef.SourceRecordID,
		},
		Payload: envelope.Payload,
	}
}

func countWarnings(envelopes []gcpcloud.Envelope) int {
	n := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == gcpcloud.GCPCollectionWarningFactKind {
			n++
		}
	}
	return n
}
