// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	collector "github.com/eshu-hq/eshu-collector-gcp-cloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpruntime"
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

// drainTestScope builds one resolvable scope for the drain tests.
func drainTestScope(parentID, credentialRef string) gcpruntime.ScopeConfig {
	return gcpruntime.ScopeConfig{
		ParentScopeKind: "project",
		ParentScopeID:   parentID,
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    "gen-drain",
		FencingToken:    7,
		CredentialRef:   credentialRef,
	}
}

// drainTestSource builds a live-shaped source over parsed fixture pages. The
// pages map is keyed by ParentScopeID; the helper resolves each scope and
// re-keys the fixtures by the resolved ScopeID the fixture page provider uses.
func drainTestSource(t *testing.T, scopes []gcpruntime.ScopeConfig, pages map[string][]gcpcloud.AssetsListPage) (*gcpruntime.Source, []string) {
	t.Helper()
	cfg := gcpruntime.Config{
		CollectorInstanceID: "gcp-cloud-drain-test",
		Scopes:              scopes,
	}
	resolved := cfg.ResolvedScopes()
	if len(resolved) == 0 {
		t.Fatal("ResolvedScopes returned no scopes")
	}
	byScopeID := map[string][]gcpcloud.AssetsListPage{}
	resolvedIDs := make([]string, 0, len(resolved))
	for _, scope := range resolved {
		resolvedIDs = append(resolvedIDs, scope.ScopeID)
		if set, ok := pages[scope.ParentScopeID]; ok {
			byScopeID[scope.ScopeID] = set
		}
	}
	key, err := smokeRedactionKey()
	if err != nil {
		t.Fatalf("smokeRedactionKey: %v", err)
	}
	return &gcpruntime.Source{
		Config:       cfg,
		Provider:     gcpruntime.NewFixturePageProvider(byScopeID),
		RedactionKey: key,
	}, resolvedIDs
}

func drainTestPage(t *testing.T, name string) gcpcloud.AssetsListPage {
	t.Helper()
	raw, err := os.ReadFile("../../gcpruntime/testdata/" + name)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	page, err := gcpcloud.ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("ParseAssetsListPage(%s): %v", name, err)
	}
	return page
}

func drainLimits(maxRecords int) collector.ResourceUse {
	limits := collector.DefaultResourceUse()
	limits.MaxRecordsPerClaim = maxRecords
	return limits
}

// TestParseLiveConfigFailsClosed proves every malformed activation fails at
// parse time: an unbounded or uncredentialed scope must never reach a scan.
func TestParseLiveConfigFailsClosed(t *testing.T) {
	t.Parallel()

	validScope := func() map[string]any {
		return map[string]any{
			"parent_scope_kind": "project",
			"parent_scope_id":   "my-project",
			"credential_ref":    "gcp-readonly-sa",
		}
	}
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing instance id", func(c map[string]any) { delete(c, "collector_instance_id") }},
		{"missing scopes", func(c map[string]any) { delete(c, "scopes") }},
		{"scopes not an array", func(c map[string]any) { c["scopes"] = "project" }},
		{"scope not an object", func(c map[string]any) { c["scopes"] = []any{"project"} }},
		{"invalid scope kind", func(c map[string]any) {
			s := validScope()
			s["parent_scope_kind"] = "fleet"
			c["scopes"] = []any{s}
		}},
		{"missing scope id", func(c map[string]any) {
			s := validScope()
			delete(s, "parent_scope_id")
			c["scopes"] = []any{s}
		}},
		{"missing credential ref", func(c map[string]any) {
			s := validScope()
			delete(s, "credential_ref")
			c["scopes"] = []any{s}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := hostActivationConfig()
			tc.mutate(config)
			if _, err := parseLiveConfig(config); err == nil {
				t.Fatalf("parseLiveConfig(%s) error = nil, want failure", tc.name)
			}
		})
	}
}

// TestParseLiveConfigAcceptsValidActivation proves the control case: a
// well-formed activation parses into one fenced scope.
func TestParseLiveConfigAcceptsValidActivation(t *testing.T) {
	t.Parallel()

	parsed, err := parseLiveConfig(hostActivationConfig())
	if err != nil {
		t.Fatalf("parseLiveConfig(valid): %v", err)
	}
	if len(parsed.Scopes) != 1 {
		t.Fatalf("scopes = %d, want 1", len(parsed.Scopes))
	}
	if parsed.Scopes[0].FencingToken <= 0 {
		t.Fatal("parsed scope carries no positive fencing placeholder")
	}
}

// TestDrainLiveSnapshotsMergesScopes proves the drain merges every scope
// snapshot into one claim snapshot instead of dropping all but one scope.
func TestDrainLiveSnapshotsMergesScopes(t *testing.T) {
	t.Parallel()

	scopes := []gcpruntime.ScopeConfig{
		drainTestScope("drain-a", "sa-drain"),
		drainTestScope("drain-b", "sa-drain"),
	}
	pages := map[string][]gcpcloud.AssetsListPage{
		"drain-a": {drainTestPage(t, "assets_list_iam_policy.json")},
		"drain-b": {drainTestPage(t, "assets_list_iam_policy.json")},
	}
	source, resolved := drainTestSource(t, scopes, pages)
	merged, reason, err := drainLiveSnapshots(context.Background(), source, drainLimits(5000))
	if err != nil {
		t.Fatalf("drainLiveSnapshots: %v", err)
	}
	if reason != "" {
		t.Fatalf("terminal reason = %q, want empty on a healthy drain", reason)
	}
	if len(merged.Envelopes) == 0 {
		t.Fatal("merged zero envelopes from two scoped snapshots")
	}
	for _, scopeID := range resolved {
		found := false
		for _, env := range merged.Envelopes {
			if env.ScopeID == scopeID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no merged envelope carries scope %q", scopeID)
		}
	}
}

// TestDrainLiveSnapshotsEmpty proves an empty drain reports terminal instead
// of an empty complete result.
func TestDrainLiveSnapshotsEmpty(t *testing.T) {
	t.Parallel()

	scopes := []gcpruntime.ScopeConfig{drainTestScope("drain-empty", "sa-drain")}
	source, _ := drainTestSource(t, scopes, nil)
	_, reason, err := drainLiveSnapshots(context.Background(), source, drainLimits(5000))
	if err != nil {
		t.Fatalf("drainLiveSnapshots: %v", err)
	}
	if reason != "live-scope-empty" {
		t.Fatalf("terminal reason = %q, want live-scope-empty", reason)
	}
}

// TestDrainLiveSnapshotsOverBudget proves an oversized drain reports terminal
// instead of an oversized complete result.
func TestDrainLiveSnapshotsOverBudget(t *testing.T) {
	t.Parallel()

	scopes := []gcpruntime.ScopeConfig{drainTestScope("drain-big", "sa-drain")}
	pages := map[string][]gcpcloud.AssetsListPage{
		"drain-big": {drainTestPage(t, "assets_list_iam_policy.json")},
	}
	source, _ := drainTestSource(t, scopes, pages)
	_, reason, err := drainLiveSnapshots(context.Background(), source, drainLimits(1))
	if err != nil {
		t.Fatalf("drainLiveSnapshots: %v", err)
	}
	if reason != "fact-budget-exceeded" {
		t.Fatalf("terminal reason = %q, want fact-budget-exceeded", reason)
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
