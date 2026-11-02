// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	collector "github.com/eshu-hq/eshu-collector-gcp-cloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud/catalog"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpruntime"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/redact"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// The nested config readers below are shared with the SDK-stdio parser in
// main.go: one strict reader family for the whole host activation shape, so
// "missing" (caller may default) and "malformed" (caller must fail closed)
// mean the same thing in both modes.

// liveRedactionKeyEnv is the fixed process variable that carries the
// deployment redaction material on the live path. The activation names this
// reference, never the material itself.
const liveRedactionKeyEnv = "ESHU_GCP_REDACTION_KEY"

// liveConfig is the parsed live-source activation: everything parseSDKStdio
// learns from a host request that carries no source.input file.
type liveConfig struct {
	CollectorInstanceID string
	Scopes              []gcpruntime.ScopeConfig
	// RedactionKeyEnv names the fixed process variable that carries the
	// deployment redaction material, never the material itself.
	RedactionKeyEnv string
	SourceURI       string
	PreviousDigest  string
	Limits          ResourceUse
}

// parseLiveConfig decodes one host activation config into the bounded GCP
// scopes the instance is authorized to scan. Anything malformed fails
// closed: an unbounded or uncredentialed scope must never scan.
func parseLiveConfig(config map[string]any) (liveConfig, error) {
	parsed := liveConfig{Limits: collector.DefaultResourceUse()}
	instanceID, _, err := nestedOptionalString(config, "collector_instance_id")
	if err != nil {
		return liveConfig{}, err
	}
	if strings.TrimSpace(instanceID) == "" {
		return liveConfig{}, fmt.Errorf("config \"collector_instance_id\" is required")
	}
	parsed.CollectorInstanceID = strings.TrimSpace(instanceID)
	scopes, err := parseLiveScopes(config)
	if err != nil {
		return liveConfig{}, err
	}
	// The static shape validates here so a malformed activation fails at
	// parse time; the host claim re-fences each scope at collect time.
	if err := (gcpruntime.Config{CollectorInstanceID: parsed.CollectorInstanceID, Scopes: scopes}).Validate(); err != nil {
		return liveConfig{}, err
	}
	parsed.Scopes = scopes
	envRef, _, err := nestedOptionalString(config, "source", "redactionKeyEnv")
	if err != nil {
		return liveConfig{}, err
	}
	parsed.RedactionKeyEnv = strings.TrimSpace(envRef)
	sourceURI, _, err := nestedOptionalString(config, "source", "sourceURI")
	if err != nil {
		return liveConfig{}, err
	}
	if strings.TrimSpace(sourceURI) == "" {
		sourceURI = "https://example.invalid/source/gcp-cloud"
	}
	parsed.SourceURI = sourceURI
	previousDigest, _, err := nestedOptionalString(config, "freshness", "previousDigest")
	if err != nil {
		return liveConfig{}, err
	}
	parsed.PreviousDigest = previousDigest
	limits, err := nestedLimits(config)
	if err != nil {
		return liveConfig{}, err
	}
	parsed.Limits = limits
	return parsed, nil
}

// parseLiveScopes decodes the bounded scope allowlist. Each scope names its
// CAI parent kind and id plus the read-only credential reference the page
// provider resolves out of band; shard families default like the runtime.
func parseLiveScopes(config map[string]any) ([]gcpruntime.ScopeConfig, error) {
	raw, ok := config["scopes"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("config \"scopes\" must list at least one bounded GCP scope")
	}
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("config \"scopes\" must list at least one bounded GCP scope")
	}
	scopes := make([]gcpruntime.ScopeConfig, 0, len(items))
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config scopes[%d] must be an object", i)
		}
		scope, err := parseLiveScope(entry)
		if err != nil {
			return nil, fmt.Errorf("config scopes[%d]: %w", i, err)
		}
		scopes = append(scopes, scope)
	}
	return scopes, nil
}

func parseLiveScope(entry map[string]any) (gcpruntime.ScopeConfig, error) {
	scope := gcpruntime.ScopeConfig{}
	kind, _, err := nestedOptionalString(entry, "parent_scope_kind")
	if err != nil {
		return gcpruntime.ScopeConfig{}, err
	}
	if !catalog.ParentScopeKind(strings.TrimSpace(kind)).Valid() {
		return gcpruntime.ScopeConfig{}, fmt.Errorf("invalid parent_scope_kind %q", kind)
	}
	scope.ParentScopeKind = catalog.ParentScopeKind(strings.TrimSpace(kind))
	parentID, _, err := nestedOptionalString(entry, "parent_scope_id")
	if err != nil {
		return gcpruntime.ScopeConfig{}, err
	}
	if strings.TrimSpace(parentID) == "" {
		return gcpruntime.ScopeConfig{}, fmt.Errorf("parent_scope_id is required")
	}
	scope.ParentScopeID = strings.TrimSpace(parentID)
	credentialRef, _, err := nestedOptionalString(entry, "credential_ref")
	if err != nil {
		return gcpruntime.ScopeConfig{}, err
	}
	if strings.TrimSpace(credentialRef) == "" {
		return gcpruntime.ScopeConfig{}, fmt.Errorf("credential_ref is required (read-only credential name)")
	}
	scope.CredentialRef = strings.TrimSpace(credentialRef)
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"asset_type_family", &scope.AssetTypeFamily},
		{"content_family", &scope.ContentFamily},
		{"location_bucket", &scope.LocationBucket},
	} {
		value, _, err := nestedOptionalString(entry, field.key)
		if err != nil {
			return gcpruntime.ScopeConfig{}, err
		}
		*field.target = strings.TrimSpace(value)
	}
	// FencingToken arrives on the host claim at collect time; the parse
	// plants the smallest positive placeholder so the static shape always
	// validates before the claim re-fences the scope.
	scope.FencingToken = 1
	return scope, nil
}

// collectLivePlan runs a bounded GCP claim from a parsed host activation.
// The CAI transport is the ported LiveClient (Application Default
// Credentials, GET-only assets.list); anything that cannot scan fails
// closed with a terminal result instead of reporting complete with zero
// facts for a scope it did not read.
func collectLivePlan(plan stdioPlan) (sdk.Result, string, error) {
	return collectLivePlanWithLookup(plan, os.LookupEnv)
}

func collectLivePlanWithLookup(plan stdioPlan, lookup func(string) (string, bool)) (sdk.Result, string, error) {
	key, err := resolveLiveRedactionKey(plan.live.RedactionKeyEnv, lookup)
	if err != nil {
		return sdk.Result{}, "", err
	}
	observedAt := time.Now().UTC()
	// One ADC identity per collect: scopes naming different credentials fail
	// closed rather than mixing identities in one claim.
	credentialRefs := map[string]bool{}
	for _, scope := range plan.live.Scopes {
		credentialRefs[strings.TrimSpace(scope.CredentialRef)] = true
	}
	if len(credentialRefs) != 1 {
		return terminalResult(plan.claim, observedAt, "multi-credential-activations-unsupported"), "", nil
	}
	var credentialRef string
	for ref := range credentialRefs {
		credentialRef = ref
	}
	// Service-account ADC is its own quota consumer, so no quota project is
	// configured; user-credential ADC resolves it from the environment.
	client, err := gcpruntime.NewADCLiveClient(context.Background(), credentialRef, "")
	if err != nil {
		return terminalResult(plan.claim, observedAt, "live-credential-unavailable"), "", nil
	}
	config := gcpruntime.Config{
		CollectorInstanceID: plan.live.CollectorInstanceID,
		Scopes:              plan.live.Scopes,
	}
	source := &gcpruntime.Source{
		Config:       config,
		Provider:     client,
		TagProvider:  client,
		RedactionKey: key,
	}
	// The host validates the returned generation against the claim it
	// issued: an unbound drain echoes a derived digest and fails every run
	// terminally with invalid_result (#8856). A claim the binding rejects
	// is a host contract violation, so it fails hard instead of reporting
	// a terminal result under a generation the host cannot accept.
	if err := source.BindClaim(plan.claim); err != nil {
		return sdk.Result{}, "", err
	}
	merged, terminalReason, err := drainLiveSnapshots(context.Background(), source, plan.live.Limits)
	if err != nil {
		return sdk.Result{}, "", err
	}
	if terminalReason != "" {
		return terminalResult(plan.claim, observedAt, terminalReason), "", nil
	}
	return collectSnapshotResult(plan.claim, merged, plan.live.Limits, plan.live.PreviousDigest, observedAt)
}

// drainLiveSnapshots drains every scope snapshot from a live source and
// merges the envelopes into one claim snapshot. A drain that yields nothing
// or exceeds the record budget reports a terminal reason instead of an
// empty or oversized complete result.
func drainLiveSnapshots(ctx context.Context, source *gcpruntime.Source, limits ResourceUse) (gcpruntime.CollectedSnapshot, string, error) {
	var envelopes []gcpcloud.Envelope
	var first gcpruntime.CollectedSnapshot
	partial := false
	for {
		snapshot, ok, err := source.Next(ctx)
		if err != nil {
			return gcpruntime.CollectedSnapshot{}, "", err
		}
		if !ok {
			break
		}
		if first.Scope.ScopeID == "" {
			first = snapshot
		}
		partial = partial || snapshot.Partial
		envelopes = append(envelopes, snapshot.Envelopes...)
	}
	if len(envelopes) == 0 {
		return gcpruntime.CollectedSnapshot{}, "live-scope-empty", nil
	}
	if len(envelopes) > limits.MaxRecordsPerClaim {
		return gcpruntime.CollectedSnapshot{}, "fact-budget-exceeded", nil
	}
	merged := first
	merged.Envelopes = envelopes
	merged.Partial = partial
	return merged, "", nil
}

// resolveLiveRedactionKey obtains deployment key material only at the command
// boundary. The activation names the fixed environment variable, never its
// value; anything else fails closed.
func resolveLiveRedactionKey(envRef string, lookup func(string) (string, bool)) (redact.Key, error) {
	if strings.TrimSpace(envRef) != liveRedactionKeyEnv {
		return redact.Key{}, fmt.Errorf("live collection requires source.redactionKeyEnv reference")
	}
	raw, present := lookup(liveRedactionKeyEnv)
	if !present {
		return redact.Key{}, fmt.Errorf("live collection requires ESHU_GCP_REDACTION_KEY")
	}
	key, err := redact.NewKey([]byte(raw))
	if err != nil || key.IsZero() {
		return redact.Key{}, fmt.Errorf("live collection requires nonblank ESHU_GCP_REDACTION_KEY")
	}
	return key, nil
}

// nestedOptionalString separates "missing" (present=false, err=nil, caller
// may default) from "malformed" (err non-nil, caller must fail closed).
func nestedOptionalString(config map[string]any, keys ...string) (string, bool, error) {
	var current any = config
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			// A present-but-wrong-typed intermediate node fails closed;
			// only a genuinely absent (nil) node selects the default.
			if current != nil {
				return "", true, fmt.Errorf("config %q must be an object", strings.Join(keys, "."))
			}
			return "", false, nil
		}
		current, ok = m[key]
		if !ok {
			return "", false, nil
		}
	}
	s, ok := current.(string)
	if !ok {
		return "", true, fmt.Errorf("config %q must be a string", strings.Join(keys, "."))
	}
	return s, true, nil
}

// nestedLimits reads the optional limits block from the host activation
// shape (limits.maxRecordsPerClaim, limits.maxPayloadBytes,
// limits.claimTimeoutSeconds, limits.maxPageBytes). Absent or partial blocks
// select DefaultResourceUse for the missing fields; malformed values fail
// closed.
func nestedLimits(config map[string]any) (ResourceUse, error) {
	limits := collector.DefaultResourceUse()
	for _, field := range []struct {
		key    string
		target *int
	}{
		{"maxRecordsPerClaim", &limits.MaxRecordsPerClaim},
		{"maxPayloadBytes", &limits.MaxPayloadBytes},
		{"claimTimeoutSeconds", &limits.ClaimTimeoutSeconds},
		{"maxPageBytes", &limits.MaxPageBytes},
	} {
		raw, present, err := nestedOptionalNumber(config, "limits", field.key)
		if err != nil {
			return ResourceUse{}, err
		}
		if present {
			*field.target = raw
		}
	}
	return limits, nil
}

func nestedOptionalNumber(config map[string]any, keys ...string) (int, bool, error) {
	var current any = config
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			if current != nil {
				return 0, true, fmt.Errorf("config %q must be an object", strings.Join(keys, "."))
			}
			return 0, false, nil
		}
		current, ok = m[key]
		if !ok {
			return 0, false, nil
		}
	}
	switch number := current.(type) {
	case float64:
		if number != float64(int(number)) || number <= 0 {
			return 0, true, fmt.Errorf("config %q must be a positive integer", strings.Join(keys, "."))
		}
		return int(number), true, nil
	default:
		return 0, true, fmt.Errorf("config %q must be a number", strings.Join(keys, "."))
	}
}
