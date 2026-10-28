// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"strings"
	"time"
)

// Provenance: secrets/IAM posture shared core ported from
// go/internal/collector/access/posture/ (types.go shared consts and
// EnvelopeContext, envelope.go newEnvelope/validateContext/secretsIAMFactID,
// normalize.go helpers) at core commit 57d11add46. Only the GCP-relevant
// subset travels with this extraction; AWS constructors, payloads, and the
// AWS typed common struct stay in core. The shared newEnvelope is renamed to
// newPostureEnvelope to avoid colliding with this package's envelope
// builder. Import rewrites only otherwise.

const (
	// PostureCollectorKind is the durable collector_kind for secrets/IAM
	// posture facts (in-tree posture.CollectorKind).
	PostureCollectorKind = "secrets_iam_posture"
	// RedactionPolicyVersion identifies the redaction contract used for this
	// source fact family.
	RedactionPolicyVersion = "secrets_iam_posture_v1"

	// PolicySourceInline marks a statement from an inline identity policy.
	PolicySourceInline = "inline"
	// PolicySourceAttachedManaged marks a statement from an attached managed
	// policy document.
	PolicySourceAttachedManaged = "attached_managed"
	// PolicySourceTrust marks a statement from an IAM role trust policy.
	PolicySourceTrust = "trust"
	// PolicySourcePermissionBoundary marks a statement from a managed policy
	// attached as a permissions boundary.
	PolicySourcePermissionBoundary = "permission_boundary"

	// SourceStateUnsupported marks evidence the collector cannot observe in the
	// current runtime slice.
	SourceStateUnsupported = "unsupported"
	// SourceStatePartial marks source evidence that was only partly observed.
	SourceStatePartial = "partial"
	// SourceStatePermissionHidden marks source evidence hidden by IAM
	// permissions.
	SourceStatePermissionHidden = "permission_hidden"
	// SourceStateRateLimited marks source evidence skipped because the provider
	// rate-limited the collector.
	SourceStateRateLimited = "rate_limited"
	// SourceStateStale marks source evidence known to be stale.
	SourceStateStale = "stale"
)

// EnvelopeContext carries fact boundary fields for one secrets/IAM source
// observation.
type EnvelopeContext struct {
	AccountID           string
	Region              string
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

func newPostureEnvelope(
	ctx EnvelopeContext,
	factKind string,
	stableKey string,
	sourceRecordID string,
	sourceURI string,
	payload map[string]any,
) Envelope {
	return Envelope{
		FactID:           secretsIAMFactID(factKind, stableKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          strings.TrimSpace(ctx.ScopeID),
		GenerationID:     strings.TrimSpace(ctx.GenerationID),
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    SecretsIAMSchemaVersionV1,
		CollectorKind:    PostureCollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: Ref{
			SourceSystem:   PostureCollectorKind,
			ScopeID:        strings.TrimSpace(ctx.ScopeID),
			GenerationID:   strings.TrimSpace(ctx.GenerationID),
			FactKey:        stableKey,
			SourceURI:      strings.TrimSpace(sourceURI),
			SourceRecordID: strings.TrimSpace(sourceRecordID),
		},
	}
}

func secretsIAMFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return stableID("SecretsIAMFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func normalizedObservedAt(input time.Time) time.Time {
	if input.IsZero() {
		return time.Now().UTC()
	}
	return input.UTC()
}

// WebIdentitySubjectFingerprint returns the redaction-safe join fingerprint for
// an IAM web-identity subject.
func WebIdentitySubjectFingerprint(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return ""
	}
	return "sha256:" + stableID("SecretsIAMWebIdentitySubject", map[string]any{
		"subject": subject,
	})
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
