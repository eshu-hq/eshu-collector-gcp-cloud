// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// hostActivationConfig returns the host's activation shape for the GCP
// collector: the collector instance, the bounded parent scopes to scan with
// their read-only credential references, and limits -- with no source.input
// file. The live claim path must accept this without demanding a file.
func hostActivationConfig() map[string]any {
	return map[string]any{
		"collector_instance_id": "gcp-cloud-primary",
		"scopes": []any{
			map[string]any{
				"parent_scope_kind": "project",
				"parent_scope_id":   "my-project",
				"credential_ref":    "gcp-readonly-sa",
			},
		},
		"limits": map[string]any{
			"maxRecordsPerClaim":  5000.0,
			"maxPayloadBytes":     65536.0,
			"claimTimeoutSeconds": 300.0,
		},
		"source": map[string]any{
			"redactionKeyEnv": "ESHU_GCP_REDACTION_KEY",
		},
	}
}

func hostActivationRequest(config map[string]any) bytes.Buffer {
	var stdin bytes.Buffer
	request := sdkRequest{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim: sdk.Claim{
			CollectorKind: "gcp",
			SourceSystem:  "gcp",
			InstanceID:    "gcp-cloud-primary",
			Scope:         sdk.Scope{ID: "gcp:project:my-project:mixed:resource:global", Kind: "account"},
			GenerationID:  "generation-1",
			SourceRunID:   "generation-1",
			FencingToken:  "7",
		},
		Config: config,
	}
	if err := json.NewEncoder(&stdin).Encode(request); err != nil {
		panic(err)
	}
	return stdin
}

// TestParseSDKStdioAcceptsLiveConfigWithoutInput proves a host activation
// request that carries the collector instance and bounded GCP scopes -- but
// no source.input file -- parses into the live-source plan instead of
// failing with a missing-input error.
func TestParseSDKStdioAcceptsLiveConfigWithoutInput(t *testing.T) {
	t.Parallel()

	stdin := hostActivationRequest(hostActivationConfig())
	plan, err := parseSDKStdio(&stdin)
	if err != nil {
		t.Fatalf("parseSDKStdio(live config) error = %v, want live-source plan", err)
	}
	if !plan.liveMode {
		t.Fatal("parseSDKStdio(live config) liveMode = false, want true")
	}
	if plan.live.CollectorInstanceID != "gcp-cloud-primary" {
		t.Fatalf("live.CollectorInstanceID = %q, want gcp-cloud-primary", plan.live.CollectorInstanceID)
	}
	if len(plan.live.Scopes) != 1 || string(plan.live.Scopes[0].ParentScopeKind) != "project" {
		t.Fatalf("live.Scopes = %+v, want one project scope", plan.live.Scopes)
	}
	if plan.limits.MaxRecordsPerClaim != 5000 || plan.limits.ClaimTimeoutSeconds != 300 {
		t.Fatalf("limits = %+v, want host limits", plan.limits)
	}
}

// TestParseSDKStdioRejectsUnknownParentScopeKind proves an unknown GCP
// parent scope kind fails closed at parse time instead of scanning an
// unbounded scope.
func TestParseSDKStdioRejectsUnknownParentScopeKind(t *testing.T) {
	t.Parallel()

	config := hostActivationConfig()
	config["scopes"] = []any{
		map[string]any{
			"parent_scope_kind": "galaxy",
			"parent_scope_id":   "my-project",
			"credential_ref":    "gcp-readonly-sa",
		},
	}
	stdin := hostActivationRequest(config)
	if _, err := parseSDKStdio(&stdin); err == nil {
		t.Fatal("parseSDKStdio(unknown kind) error = nil, want failure")
	}
}

// TestParseSDKStdioRejectsLiveConfigWithoutScopes proves a live config with
// no bounded scopes fails closed instead of scanning an unbounded scope.
func TestParseSDKStdioRejectsLiveConfigWithoutScopes(t *testing.T) {
	t.Parallel()

	config := hostActivationConfig()
	delete(config, "scopes")
	stdin := hostActivationRequest(config)
	_, err := parseSDKStdio(&stdin)
	if err == nil || !strings.Contains(err.Error(), "scopes") {
		t.Fatalf("parseSDKStdio(no scopes) error = %v, want scopes failure", err)
	}
}

// TestParseSDKStdioRejectsLiveScopeWithoutCredential proves a scope that
// names no read-only credential fails closed instead of silently falling
// back to ambient credentials.
func TestParseSDKStdioRejectsLiveScopeWithoutCredential(t *testing.T) {
	t.Parallel()

	config := hostActivationConfig()
	config["scopes"] = []any{
		map[string]any{
			"parent_scope_kind": "project",
			"parent_scope_id":   "my-project",
		},
	}
	stdin := hostActivationRequest(config)
	_, err := parseSDKStdio(&stdin)
	if err == nil || !strings.Contains(err.Error(), "credential_ref") {
		t.Fatalf("parseSDKStdio(no credential) error = %v, want credential_ref failure", err)
	}
}

// TestResolveLiveRedactionKeyRequiresFixedReference proves the deployment
// key arrives only through the fixed environment reference: a wrong name,
// a missing value, or blank material all fail closed.
func TestResolveLiveRedactionKeyRequiresFixedReference(t *testing.T) {
	t.Parallel()

	lookup := func(string) (string, bool) { return "material", true }
	if _, err := resolveLiveRedactionKey("OTHER_ENV", lookup); err == nil {
		t.Fatal("resolveLiveRedactionKey(wrong reference) error = nil, want failure")
	}
	if _, err := resolveLiveRedactionKey("ESHU_GCP_REDACTION_KEY", func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("resolveLiveRedactionKey(missing material) error = nil, want failure")
	}
	if _, err := resolveLiveRedactionKey("ESHU_GCP_REDACTION_KEY", func(string) (string, bool) { return "   ", true }); err == nil {
		t.Fatal("resolveLiveRedactionKey(blank material) error = nil, want failure")
	}
	key, err := resolveLiveRedactionKey("ESHU_GCP_REDACTION_KEY", lookup)
	if err != nil || key.IsZero() {
		t.Fatalf("resolveLiveRedactionKey(valid) = %v, %v, want keyed material", key, err)
	}
}

// TestLiveCollectRequiresRedactionKey proves a live activation that names no
// deployment key fails the run instead of collecting with unkeyed redaction
// markers.
func TestLiveCollectRequiresRedactionKey(t *testing.T) {
	t.Parallel()

	config := hostActivationConfig()
	delete(config, "source")
	stdin := hostActivationRequest(config)
	plan, err := parseSDKStdio(&stdin)
	if err != nil {
		t.Fatalf("parseSDKStdio(live config) error = %v", err)
	}
	if _, _, err := collectLivePlanWithLookup(plan, func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("collectLivePlan(missing key) error = nil, want failure")
	}
}

// TestLiveCollectReadsKeyFromProcessEnv proves the command entry point
// resolves deployment key material from the process environment.
func TestLiveCollectReadsKeyFromProcessEnv(t *testing.T) {
	t.Setenv("ESHU_GCP_REDACTION_KEY", "material")

	stdin := hostActivationRequest(hostActivationConfig())
	plan, err := parseSDKStdio(&stdin)
	if err != nil {
		t.Fatalf("parseSDKStdio(live config) error = %v", err)
	}
	result, _, err := collectLivePlan(plan)
	if err != nil {
		t.Fatalf("collectLivePlan error = %v, want nil (terminal encodes, not errors)", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal while live transport is unavailable", result.State)
	}
}

// TestLiveCollectFailsClosedWithoutCredentials proves the live path fails
// closed with a terminal result when ADC has no usable credential: it must
// never report complete with zero facts for a scope it did not read.
func TestLiveCollectFailsClosedWithoutCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent-8856-credentials.json")

	stdin := hostActivationRequest(hostActivationConfig())
	plan, err := parseSDKStdio(&stdin)
	if err != nil {
		t.Fatalf("parseSDKStdio(live config) error = %v", err)
	}
	result, digest, err := collectLivePlanWithLookup(plan, func(string) (string, bool) { return "material", true })
	if err != nil {
		t.Fatalf("collectLivePlan error = %v, want nil (terminal encodes, not errors)", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal without live credentials", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("facts = %d, want 0 on the fail-closed live path", len(result.Facts))
	}
	if digest != "" {
		t.Fatalf("digest = %q, want empty on the fail-closed live path", digest)
	}
	found := false
	for _, status := range result.Statuses {
		if status.FailureClass == "live-credential-unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("statuses = %+v, want the live-credential-unavailable failure class", result.Statuses)
	}
}

// TestLiveCollectRejectsMultiCredentialScopes proves one collect never mixes
// ADC identities: scopes naming different credentials fail closed.
func TestLiveCollectRejectsMultiCredentialScopes(t *testing.T) {
	t.Parallel()

	config := hostActivationConfig()
	config["scopes"] = []any{
		map[string]any{
			"parent_scope_kind": "project",
			"parent_scope_id":   "project-a",
			"credential_ref":    "sa-a",
		},
		map[string]any{
			"parent_scope_kind": "project",
			"parent_scope_id":   "project-b",
			"credential_ref":    "sa-b",
		},
	}
	stdin := hostActivationRequest(config)
	plan, err := parseSDKStdio(&stdin)
	if err != nil {
		t.Fatalf("parseSDKStdio(multi-scope config) error = %v", err)
	}
	result, _, err := collectLivePlanWithLookup(plan, func(string) (string, bool) { return "material", true })
	if err != nil {
		t.Fatalf("collectLivePlan error = %v, want nil (terminal encodes, not errors)", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for multi-credential scopes", result.State)
	}
	found := false
	for _, status := range result.Statuses {
		if status.FailureClass == "multi-credential-activations-unsupported" {
			found = true
		}
	}
	if !found {
		t.Fatalf("statuses = %+v, want the multi-credential failure class", result.Statuses)
	}
}
