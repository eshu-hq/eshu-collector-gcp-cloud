// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	collector "github.com/eshu-hq/eshu-collector-gcp-cloud"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestNestedOptionalStringSeparatesMissingFromMalformed proves a missing key
// falls back to the placeholder while a present-but-malformed value fails
// closed instead of silently emitting against the placeholder.
func TestNestedOptionalStringSeparatesMissingFromMalformed(t *testing.T) {
	t.Parallel()

	value, present, err := nestedOptionalString(map[string]any{}, "source", "sourceURI")
	if err != nil || present || value != "" {
		t.Fatalf("missing key = (%q, %v, %v), want empty/absent/nil", value, present, err)
	}
	if _, _, err := nestedOptionalString(map[string]any{
		"source": map[string]any{"sourceURI": 42},
	}, "source", "sourceURI"); err == nil {
		t.Fatal("malformed value error = nil, want failure")
	}
	value, present, err = nestedOptionalString(map[string]any{
		"source": map[string]any{"sourceURI": "https://example.invalid/x"},
	}, "source", "sourceURI")
	if err != nil || !present || value != "https://example.invalid/x" {
		t.Fatalf("valid key = (%q, %v, %v), want value/present/nil", value, present, err)
	}
}

// TestNegativeLimitFlagsFailClosed proves negative CLI limits fail instead
// of silently selecting defaults, matching the config-file posture.
func TestNegativeLimitFlagsFailClosed(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"--max-records", "--max-payload-bytes", "--claim-timeout-seconds", "--max-page-bytes"} {
		var stdout, stderr bytes.Buffer
		err := run([]string{flag, "-5"}, strings.NewReader(""), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "non-negative") {
			t.Fatalf("run(negative %s) error = %v, want non-negative rejection", flag, err)
		}
	}
}

// TestParentScopeKindFlagRejected proves an unknown GCP parent scope kind
// fails closed instead of emitting facts under an unbounded shard identity.
func TestParentScopeKindFlagRejected(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--parent-scope-kind", "galaxy"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "parent-scope-kind") {
		t.Fatalf("run(bad kind) error = %v, want parent-scope-kind rejection", err)
	}
}

// TestTerminalObservedAsFailure proves a terminal claim observes as a
// Monitor failure (never a success): failures increment and LastSuccessAt
// stays unset.
func TestTerminalObservedAsFailure(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--input", "../../testdata/complete.json", "--max-records", "1"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v, want nil (terminal encodes, not errors)", err)
	}
	health := stderrHealth(t, stderr.String())
	if health["consecutive_failures"] != 1.0 {
		t.Fatalf("consecutive_failures = %v, want 1", health["consecutive_failures"])
	}
	// time.Time always marshals, so "unset" reads as the zero timestamp.
	if health["last_success_at"] != "0001-01-01T00:00:00Z" {
		t.Fatalf("last_success_at = %v after terminal, want zero", health["last_success_at"])
	}
	if health["last_error"] != "fact-budget-exceeded" {
		t.Fatalf("last_error = %v, want the terminal failure class", health["last_error"])
	}
}

// TestUnchangedMovesNothing proves an unchanged outcome performs no Monitor
// observation at all: no success timestamp advance and no digest write, so a
// long-lived process never wipes its last good digest on a freshness check.
func TestUnchangedMovesNothing(t *testing.T) {
	t.Parallel()

	var firstOut, firstErr bytes.Buffer
	if err := run([]string{"--input", "../../testdata/complete.json"}, strings.NewReader(""), &firstOut, &firstErr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	digest, ok := stderrHealth(t, firstErr.String())["last_digest"].(string)
	if !ok || digest == "" {
		t.Fatal("first run produced no digest to replay")
	}
	var secondOut, secondErr bytes.Buffer
	if err := run([]string{"--input", "../../testdata/complete.json", "--previous-digest", digest}, strings.NewReader(""), &secondOut, &secondErr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	secondHealth := stderrHealth(t, secondErr.String())
	if secondHealth["last_success_at"] != "0001-01-01T00:00:00Z" {
		t.Fatalf("unchanged run advanced last_success_at: %v", secondHealth)
	}
	if failures, ok := secondHealth["consecutive_failures"]; ok && failures != 0.0 {
		t.Fatalf("unchanged run recorded failures: %v", secondHealth)
	}
	var result sdk.Result
	if err := json.Unmarshal(secondOut.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.State != sdk.ResultUnchanged || len(result.Facts) != 0 {
		t.Fatalf("state = %v facts = %d, want unchanged with 0 facts", result.State, len(result.Facts))
	}
}

// TestWrongTypedIntermediateNodeFailsClosed proves a present-but-wrong-typed
// config node errors instead of silently selecting defaults.
func TestWrongTypedIntermediateNodeFailsClosed(t *testing.T) {
	t.Parallel()

	if _, _, err := nestedOptionalString(map[string]any{"source": 42.0}, "source", "sourceURI"); err == nil {
		t.Fatal("wrong-typed intermediate error = nil, want failure")
	}
	if _, _, err := nestedOptionalNumber(map[string]any{"limits": "many"}, "limits", "maxRecordsPerClaim"); err == nil {
		t.Fatal("wrong-typed limits error = nil, want failure")
	}
}

func stderrHealth(t *testing.T, stderr string) map[string]any {
	t.Helper()
	const prefix = "collector health: "
	index := strings.LastIndex(stderr, prefix)
	if index < 0 {
		t.Fatalf("stderr missing health snapshot: %q", stderr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stderr[index+len(prefix):])), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(health) error = %v", err)
	}
	health, ok := envelope["health"].(map[string]any)
	if !ok {
		t.Fatalf("health envelope shape wrong: %v", envelope)
	}
	return health
}

// TestNestedLimitsConsumesConfigBlock proves the limits block from the host
// activation shape reaches collection: absent selects defaults, partial
// overrides per field, malformed fails closed.
func TestNestedLimitsConsumesConfigBlock(t *testing.T) {
	t.Parallel()

	deflated, err := nestedLimits(map[string]any{})
	if err != nil {
		t.Fatalf("nestedLimits(empty) error = %v", err)
	}
	if deflated != collector.DefaultResourceUse() {
		t.Fatalf("nestedLimits(empty) = %+v, want defaults", deflated)
	}
	partial, err := nestedLimits(map[string]any{
		"limits": map[string]any{"maxRecordsPerClaim": 7.0},
	})
	if err != nil {
		t.Fatalf("nestedLimits(partial) error = %v", err)
	}
	if partial.MaxRecordsPerClaim != 7 || partial.MaxPayloadBytes != collector.DefaultResourceUse().MaxPayloadBytes {
		t.Fatalf("nestedLimits(partial) = %+v, want override + defaults", partial)
	}
	if _, err := nestedLimits(map[string]any{
		"limits": map[string]any{"maxRecordsPerClaim": "many"},
	}); err == nil {
		t.Fatal("nestedLimits(malformed) error = nil, want failure")
	}
}

// TestRunWithTimeoutFailsSlowWork proves the shared wall-time runner fails
// a collection that outruns ClaimTimeoutSeconds instead of waiting
// unbounded. Both entry modes (local flags and SDK stdio) run through it,
// so one mechanism test covers the bound behind both.
func TestRunWithTimeoutFailsSlowWork(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	defer close(release)
	bounds := collector.DefaultResourceUse()
	bounds.ClaimTimeoutSeconds = 1
	_, err := runWithTimeout(func() (sdk.Result, error) {
		<-release
		return sdk.Result{}, nil
	}, bounds)
	if err == nil || !strings.Contains(err.Error(), "wall-time bound of 1 seconds exceeded") {
		t.Fatalf("runWithTimeout(slow) error = %v, want wall-time bound failure", err)
	}
}

// TestStdioPathCollectsRequest proves the --sdk-stdio path still collects
// after the parse/collect split and reports the request's limits: a host
// request against the smoke fixture emits a complete result, and the
// request's custom claim timeout reaches the health snapshot.
func TestStdioPathCollectsRequest(t *testing.T) {
	t.Parallel()

	request := sdkRequest{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           sdk.Claim{Scope: sdk.Scope{ID: "gcp:project:my-project:mixed:resource:global", Kind: "account"}},
		Config: map[string]any{
			"source": map[string]any{"input": "../../testdata/complete.json"},
			"limits": map[string]any{"claimTimeoutSeconds": 60.0},
		},
	}
	var stdin bytes.Buffer
	if err := json.NewEncoder(&stdin).Encode(request); err != nil {
		t.Fatalf("json.Encode(request) error = %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--sdk-stdio"}, &stdin, &stdout, &stderr); err != nil {
		t.Fatalf("run(--sdk-stdio) error = %v", err)
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete for the stdio fixture run", result.State)
	}
	if len(result.Facts) == 0 {
		t.Fatal("stdio fixture run emitted no facts")
	}
	if !strings.Contains(stderr.String(), `"claim_timeout_seconds":60`) {
		t.Fatalf("health snapshot missing the request timeout: %q", stderr.String())
	}
}

// TestParseSDKStdioRejectsBadProtocol proves an unknown wire protocol fails
// closed at the stdin boundary instead of collecting under a skew contract.
func TestParseSDKStdioRejectsBadProtocol(t *testing.T) {
	t.Parallel()

	request := sdkRequest{
		ProtocolVersion: "collector-sdk/v9alpha9",
		Config:          map[string]any{},
	}
	var stdin bytes.Buffer
	if err := json.NewEncoder(&stdin).Encode(request); err != nil {
		t.Fatalf("json.Encode(request) error = %v", err)
	}
	if _, err := parseSDKStdio(&stdin); err == nil {
		t.Fatal("parseSDKStdio(bad protocol) error = nil, want failure")
	}
}

// TestParseSDKStdioRejectsTrailingJSON proves a second JSON value on stdin
// fails closed instead of silently collecting the first request.
func TestParseSDKStdioRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	request := sdkRequest{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Config:          map[string]any{},
	}
	var stdin bytes.Buffer
	encoder := json.NewEncoder(&stdin)
	if err := encoder.Encode(request); err != nil {
		t.Fatalf("json.Encode(request) error = %v", err)
	}
	if err := encoder.Encode(request); err != nil {
		t.Fatalf("json.Encode(second) error = %v", err)
	}
	if _, err := parseSDKStdio(&stdin); err == nil {
		t.Fatal("parseSDKStdio(trailing JSON) error = nil, want failure")
	}
}

// TestPayloadBudgetFailsClosed proves a fact whose payload exceeds the byte
// bound fails the claim terminal with payload-budget-exceeded instead of
// emitting evidence the host would reject.
func TestPayloadBudgetFailsClosed(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--input", "../../testdata/complete.json", "--max-payload-bytes", "10"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v, want nil (terminal encodes, not errors)", err)
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for the over-budget payload", result.State)
	}
	if stderrHealth(t, stderr.String())["last_error"] != "payload-budget-exceeded" {
		t.Fatalf("health last_error = %v, want payload-budget-exceeded", stderrHealth(t, stderr.String())["last_error"])
	}
}

// TestExpiredDeadlineFailsTerminal proves a claim whose deadline already
// passed fails terminal with claim-deadline-exceeded before any file is
// read.
func TestExpiredDeadlineFailsTerminal(t *testing.T) {
	t.Parallel()

	request := sdkRequest{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim: sdk.Claim{
			Scope:    sdk.Scope{ID: "gcp:project:my-project:mixed:resource:global", Kind: "account"},
			Deadline: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		Config: map[string]any{
			"source": map[string]any{"input": "../../testdata/complete.json"},
		},
	}
	var stdin bytes.Buffer
	if err := json.NewEncoder(&stdin).Encode(request); err != nil {
		t.Fatalf("json.Encode(request) error = %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--sdk-stdio"}, &stdin, &stdout, &stderr); err != nil {
		t.Fatalf("run(--sdk-stdio) error = %v, want nil (terminal encodes, not errors)", err)
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for the expired deadline", result.State)
	}
}

// TestMissingInputFileErrors proves a missing input file fails the run with
// an observed failure instead of emitting an empty complete result.
func TestMissingInputFileErrors(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--input", "../../testdata/does-not-exist.json"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("run(missing input) error = nil, want failure")
	}
	if stderrHealth(t, stderr.String())["consecutive_failures"] != 1.0 {
		t.Fatalf("missing input did not observe a failure: %q", stderr.String())
	}
}

// TestMalformedInputErrors proves an unparsable input file fails the run
// instead of emitting evidence from a torn page.
func TestMalformedInputErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--input", bad}, strings.NewReader(""), &stdout, &stderr); err == nil {
		t.Fatal("run(malformed input) error = nil, want failure")
	}
}

// TestSourceURIRejectsCredentials proves a source URI that embeds userinfo
// fails the run, and that the error never echoes the value.
func TestSourceURIRejectsCredentials(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--source-uri", "https://user:secret@example.invalid/x"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("run(credentialed URI) error = %v, want a static rejection", err)
	}
}

// TestBlankSourceURIRejectsEmpty proves a blank source URI fails the run
// instead of emitting facts with no source identity.
func TestBlankSourceURIRejectsEmpty(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--source-uri", ""}, strings.NewReader(""), &stdout, &stderr); err == nil {
		t.Fatal("run(blank source URI) error = nil, want failure")
	}
}

// TestPageBudgetFailsClosed proves an encoded result larger than the page
// bound fails terminal with page-budget-exceeded instead of emitting an
// over-budget stdout write the host would reject mid-stream.
func TestPageBudgetFailsClosed(t *testing.T) {
	t.Parallel()

	request := sdkRequest{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Config: map[string]any{
			"source": map[string]any{"input": "../../testdata/complete.json"},
			"limits": map[string]any{"maxPageBytes": 100.0},
		},
	}
	var stdin bytes.Buffer
	if err := json.NewEncoder(&stdin).Encode(request); err != nil {
		t.Fatalf("json.Encode(request) error = %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--sdk-stdio"}, &stdin, &stdout, &stderr); err != nil {
		t.Fatalf("run(--sdk-stdio) error = %v, want nil (terminal encodes, not errors)", err)
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for the over-budget page", result.State)
	}
	if stderrHealth(t, stderr.String())["last_error"] != "page-budget-exceeded" {
		t.Fatalf("health last_error = %v, want page-budget-exceeded", stderrHealth(t, stderr.String())["last_error"])
	}
}
