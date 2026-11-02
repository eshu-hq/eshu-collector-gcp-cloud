// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// digestEvidence computes the opaque evidence digest for one emission: a
// hash over the sorted fact kinds, stable keys, and payload bytes. An empty
// envelope set yields no digest, so an empty scan never reads as unchanged
// evidence and never advances the freshness signal.
func digestEvidence(envelopes []gcpcloud.Envelope) string {
	if len(envelopes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		payload, err := json.Marshal(envelope.Payload)
		if err != nil {
			payload = []byte(strings.TrimSpace(envelope.StableFactKey))
		}
		parts = append(parts, envelope.FactKind+"\x00"+envelope.StableFactKey+"\x00"+string(payload))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return "gcp:" + hex.EncodeToString(sum[:])
}

// healthDigest selects the digest the health monitor records for one
// emitted result: the evidence digest the collection computed, covering
// exactly the facts the host admits. Terminal and unchanged outcomes record
// nothing, and an empty digest records nothing.
func healthDigest(outcomeDigest string, result sdk.Result) string {
	if result.State == sdk.ResultTerminal || result.State == sdk.ResultUnchanged {
		return ""
	}
	return outcomeDigest
}

// unchangedOutcome short-circuits a claim whose evidence has not moved since
// the previous digest the host recorded. It reports the unchanged shape
// with no facts, leaving the host's digest alone. An empty previous or
// outcome digest never matches: the first claim after a fresh scan always
// performs a full collection.
func unchangedOutcome(claim sdk.Claim, previousDigest, outcomeDigest string) (sdk.Result, bool) {
	if previousDigest == "" || outcomeDigest == "" || previousDigest != outcomeDigest {
		return sdk.Result{}, false
	}
	now := time.Now().UTC()
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultUnchanged,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: now, FreshnessHint: "unchanged-digest"},
	}, true
}
