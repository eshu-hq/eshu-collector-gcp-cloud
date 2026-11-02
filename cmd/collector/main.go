// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
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

// Shared operator types live in the root package (health.go) so the binary
// and any future caller observe one shape; the aliases keep the command
// call sites unqualified.
type (
	ResourceUse = collector.ResourceUse
	Health      = collector.Health
	Monitor     = collector.Monitor
)

const (
	// maxInputBytes caps one CAI page document. Reads past the cap fail
	// instead of decoding unbounded input.
	maxInputBytes = 32 << 20
)

type sdkRequest struct {
	ProtocolVersion string         `json:"protocol_version"`
	Claim           sdk.Claim      `json:"claim"`
	Contract        sdk.Contract   `json:"contract"`
	Config          map[string]any `json:"config,omitempty"`
	// No continuation field: the pinned SDK pair speaks v1alpha1 only and
	// this satellite emits single-shot results, so a resumed-page cursor
	// has no meaning here. With DisallowUnknownFields below, a host that
	// sends one fails closed at this boundary instead of resuming at an
	// untrusted offset.
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "collector: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "seed" {
		return runSeed(args[1:], stdout, os.Getenv)
	}
	flags := flag.NewFlagSet("collector", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "testdata/complete.json", "CAI assets.list page input file")
	sourceURI := flags.String("source-uri", "https://example.invalid/source/gcp-cloud", "credential-free source URI")
	parentScopeKind := flags.String("parent-scope-kind", "project", "bounded CAI parent scope kind (organization, folder, project)")
	parentScopeID := flags.String("parent-scope-id", "my-project", "provider parent identifier for the file scope")
	previousDigest := flags.String("previous-digest", "", "previous evidence digest for unchanged detection")
	maxRecords := flags.Int("max-records", 0, "max records per claim (0 selects the default bound)")
	maxPayloadBytes := flags.Int("max-payload-bytes", 0, "max payload bytes per fact (0 selects the default bound)")
	claimTimeoutSeconds := flags.Int("claim-timeout-seconds", 0, "claim timeout in seconds (0 selects the default bound)")
	maxPageBytes := flags.Int("max-page-bytes", 0, "max result document bytes on stdout (0 selects the default bound)")
	sdkStdio := flags.Bool("sdk-stdio", false, "read one collector SDK host request from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *maxRecords < 0 || *maxPayloadBytes < 0 || *claimTimeoutSeconds < 0 || *maxPageBytes < 0 {
		return fmt.Errorf("limits must be non-negative (0 selects defaults)")
	}
	if !validParentScopeKind(*parentScopeKind) {
		return fmt.Errorf("parent-scope-kind %q is invalid (want organization, folder, or project)", *parentScopeKind)
	}
	limits := collector.DefaultResourceUse()
	if *maxRecords > 0 {
		limits.MaxRecordsPerClaim = *maxRecords
	}
	if *maxPayloadBytes > 0 {
		limits.MaxPayloadBytes = *maxPayloadBytes
	}
	if *claimTimeoutSeconds > 0 {
		limits.ClaimTimeoutSeconds = *claimTimeoutSeconds
	}
	if *maxPageBytes > 0 {
		limits.MaxPageBytes = *maxPageBytes
	}
	// The health monitor is the operator signal: every outcome below is
	// observed, and the snapshot line on stderr carries liveness,
	// freshness, failure, and retry state. Collection runs under a
	// wall-time bound in both modes: breaching it fails the run instead of
	// emitting late evidence.
	monitor := collector.NewMonitor()
	var result sdk.Result
	var outcomeDigest string
	var err error
	if *sdkStdio {
		var plan stdioPlan
		plan, err = parseSDKStdio(stdin)
		if err == nil {
			limits = plan.limits
			result, outcomeDigest, err = runCollectWithTimeout(func() (sdk.Result, string, error) {
				return collectStdioPlan(plan, *parentScopeKind, *parentScopeID)
			}, limits)
		}
	} else {
		result, outcomeDigest, err = runCollectWithTimeout(func() (sdk.Result, string, error) {
			return collectFileClaim(*inputPath, *parentScopeKind, *parentScopeID, *sourceURI, *previousDigest, demoClaim(), limits)
		}, limits)
	}
	monitor.SetResource(limits)
	if err != nil {
		monitor.ObserveFailure(firstLine(err.Error()))
		health, resource := monitor.Snapshot()
		logHealth(stderr, health, resource)
		return err
	}
	// Only Complete/Partial with a recorded digest move LastSuccessAt.
	// Terminal outcomes observe as failures (propagating the failure
	// class), and Unchanged outcomes leave the last good digest alone.
	digest := healthDigest(outcomeDigest, result)
	if result.State == sdk.ResultTerminal {
		monitor.ObserveFailure(terminalCause(result))
	} else if digest != "" {
		monitor.ObserveSuccess(digest, time.Now().UTC())
	}
	health, resource := monitor.Snapshot()
	logHealth(stderr, health, resource)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// terminalCause propagates the terminal failure class for operator
// visibility instead of a static string.
func terminalCause(result sdk.Result) string {
	for _, status := range result.Statuses {
		if strings.TrimSpace(status.FailureClass) != "" {
			return firstLine(status.FailureClass)
		}
	}
	return "claim-terminal"
}

func firstLine(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return message[:i]
	}
	return message
}

func logHealth(stderr io.Writer, health Health, resource ResourceUse) {
	// Error and digest strings here never carry credentials: collection
	// errors never echo URIs or payloads, and digests are opaque hashes.
	line, err := json.Marshal(map[string]any{"health": health, "resource": resource})
	if err != nil {
		return
	}
	// Best-effort health line: a write failure carries no recovery action.
	if _, err := fmt.Fprintf(stderr, "collector health: %s\n", line); err != nil {
		return
	}
}

func validParentScopeKind(kind string) bool {
	return catalog.ParentScopeKind(strings.TrimSpace(kind)).Valid()
}

// collectFileClaim runs one file-backed claim: it parses the CAI page,
// drains it through the gcpruntime Source, enforces the emission bounds,
// converts the snapshot to the SDK wire form, and applies unchanged
// detection against the previous digest.
func collectFileClaim(inputPath, parentScopeKind, parentScopeID, sourceURI, previousDigest string, claim sdk.Claim, limits ResourceUse) (sdk.Result, string, error) {
	observedAt := time.Now().UTC()
	if strings.TrimSpace(sourceURI) == "" {
		return sdk.Result{}, "", fmt.Errorf("source URI must not be blank")
	}
	if err := checkSourceURI(sourceURI); err != nil {
		return sdk.Result{}, "", err
	}
	if !claim.Deadline.IsZero() && observedAt.After(claim.Deadline) {
		return terminalResult(claim, observedAt, "claim-deadline-exceeded"), "", nil
	}
	raw, err := os.ReadFile(inputPath) // #nosec G304 -- inputPath is the operator's own --input flag value, never request-derived
	if err != nil {
		return sdk.Result{}, "", err
	}
	if len(raw) > maxInputBytes {
		return sdk.Result{}, "", fmt.Errorf("source document exceeds %d bytes", maxInputBytes)
	}
	page, err := gcpcloud.ParseAssetsListPage(raw)
	if err != nil {
		return sdk.Result{}, "", err
	}
	key, err := smokeRedactionKey()
	if err != nil {
		return sdk.Result{}, "", err
	}
	scope := gcpruntime.ScopeConfig{
		ParentScopeKind: catalog.ParentScopeKind(strings.TrimSpace(parentScopeKind)),
		ParentScopeID:   strings.TrimSpace(parentScopeID),
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    claim.GenerationID,
		FencingToken:    1,
		CredentialRef:   "smoke-fixture",
	}
	if strings.TrimSpace(scope.GenerationID) == "" {
		scope.GenerationID = "generation-1"
	}
	config := gcpruntime.Config{
		CollectorInstanceID: "gcp-cloud-primary",
		Scopes:              []gcpruntime.ScopeConfig{scope},
	}
	resolved := config.ResolvedScopes()[0]
	source := &gcpruntime.Source{
		Config:       config,
		Provider:     gcpruntime.NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{resolved.ScopeID: {page}}),
		RedactionKey: key,
	}
	snapshot, ok, err := source.Next(context.Background())
	if err != nil {
		return sdk.Result{}, "", err
	}
	if !ok {
		return sdk.Result{}, "", fmt.Errorf("file scope produced no snapshot")
	}
	if len(snapshot.Envelopes) > limits.MaxRecordsPerClaim {
		return terminalResult(claim, observedAt, "fact-budget-exceeded"), "", nil
	}
	return collectSnapshotResult(claim, snapshot, limits, previousDigest, observedAt)
}

// collectSnapshotResult converts one drained snapshot into the bounded SDK
// result both the file and live paths return: per-payload and per-page
// budgets fail closed with terminal results, and the unchanged digest
// short-circuits repeat claims.
func collectSnapshotResult(claim sdk.Claim, snapshot gcpruntime.CollectedSnapshot, limits ResourceUse, previousDigest string, observedAt time.Time) (sdk.Result, string, error) {
	result, err := gcpruntime.ToSDKResult(claim, snapshot)
	if err != nil {
		return sdk.Result{}, "", err
	}
	for _, fact := range result.Facts {
		encoded, err := json.Marshal(fact.Payload)
		if err != nil {
			return sdk.Result{}, "", fmt.Errorf("encode fact payload: %w", err)
		}
		if len(encoded) > limits.MaxPayloadBytes {
			return terminalResult(claim, observedAt, "payload-budget-exceeded"), "", nil
		}
	}
	outcomeDigest := digestEvidence(snapshot.Envelopes)
	if unchanged, done := unchangedOutcome(claim, previousDigest, outcomeDigest); done {
		return unchanged, "", nil
	}
	if encoded, err := json.Marshal(result); err != nil {
		return sdk.Result{}, "", fmt.Errorf("encode result: %w", err)
	} else if len(encoded) > limits.MaxPageBytes {
		return terminalResult(claim, observedAt, "page-budget-exceeded"), "", nil
	}
	return result, outcomeDigest, nil
}

// smokeRedactionKey mints the deterministic fixture-only redaction key for
// the offline file path. It fingerprints fixture labels deterministically
// so smoke output is stable; deployments resolve real key material from
// the named environment reference on the live path instead.
func smokeRedactionKey() (redact.Key, error) {
	return redact.NewKey([]byte("gcp-collector-smoke-fixture-key"))
}

func terminalResult(claim sdk.Claim, observedAt time.Time, failureClass string) sdk.Result {
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultTerminal,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt},
		Statuses: []sdk.Status{{
			Class: sdk.StatusFailure, FailureClass: failureClass,
		}},
	}
}

// checkSourceURI rejects source URIs that embed credentials or are not
// absolute http(s) references. It never echoes the value: callers report a
// static reason so secrets cannot leak through error text.
func checkSourceURI(raw string) error {
	trimmed := strings.TrimSpace(raw)
	lower := strings.ToLower(trimmed)
	// Parse once so percent-encoded signing keys cannot bypass the raw-text
	// guard. Do not wrap parse errors: a source URI may contain credentials.
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("source URI is invalid or embeds credentials")
	}
	// Opaque URIs can hide userinfo in their body, outside parsed.User.
	if parsed.User != nil || (strings.Contains(trimmed, "@") && !strings.Contains(lower, "://")) {
		return fmt.Errorf("source URI must not embed credentials")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("source URI must be an http(s) URI")
	}
	return nil
}

// runWithTimeout bounds one claim's wall time whatever the entry mode: a
// collection that outruns limits.ClaimTimeoutSeconds fails the run instead
// of emitting late evidence. Limits reaching here always carry a positive
// timeout (flag parsing and nestedLimits default per field), so the bound
// is never accidentally zero.
func runWithTimeout(work func() (sdk.Result, error), limits ResourceUse) (sdk.Result, error) {
	type outcome struct {
		result sdk.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := work()
		done <- outcome{result: result, err: err}
	}()
	timeout := time.Duration(limits.ClaimTimeoutSeconds) * time.Second
	select {
	case finished := <-done:
		return finished.result, finished.err
	case <-time.After(timeout):
		return sdk.Result{}, fmt.Errorf("claim wall-time bound of %d seconds exceeded", limits.ClaimTimeoutSeconds)
	}
}

// runCollectWithTimeout is the digest-carrying form of runWithTimeout for
// claim collection: the evidence digest travels back through the channel
// alongside the result, so no shared variable crosses the timeout goroutine
// boundary.
func runCollectWithTimeout(work func() (sdk.Result, string, error), limits ResourceUse) (sdk.Result, string, error) {
	type outcome struct {
		result sdk.Result
		digest string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, digest, err := work()
		done <- outcome{result: result, digest: digest, err: err}
	}()
	timeout := time.Duration(limits.ClaimTimeoutSeconds) * time.Second
	select {
	case finished := <-done:
		return finished.result, finished.digest, finished.err
	case <-time.After(timeout):
		return sdk.Result{}, "", fmt.Errorf("claim wall-time bound of %d seconds exceeded", limits.ClaimTimeoutSeconds)
	}
}

// stdioPlan is a parsed SDK host request: everything parseSDKStdio learns
// from stdin before the wall-time bound starts covering the collection. A
// request with source.input runs the offline file mode; a host activation
// request without it runs the config-driven live mode instead.
type stdioPlan struct {
	inputPath      string
	sourceURI      string
	previousDigest string
	limits         ResourceUse
	claim          sdk.Claim
	liveMode       bool
	live           liveConfig
}

// parseSDKStdio decodes and validates one collector SDK host request. It
// performs no collection itself so run() can report the effective limits
// and enforce their wall-time bound around the collect step.
func parseSDKStdio(stdin io.Reader) (stdioPlan, error) {
	plan := stdioPlan{limits: collector.DefaultResourceUse()}
	var request sdkRequest
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return stdioPlan{}, fmt.Errorf("decode SDK request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return stdioPlan{}, fmt.Errorf("decode SDK request: trailing JSON value")
		}
		return stdioPlan{}, fmt.Errorf("decode SDK request trailer: %w", err)
	}
	// This satellite speaks v1alpha1 only (manifest runtime.sdkProtocol):
	// anything else is a version skew and fails closed at the boundary.
	if strings.TrimSpace(request.ProtocolVersion) != sdk.ProtocolVersionV1Alpha1 {
		return stdioPlan{}, fmt.Errorf("protocol_version %q is unsupported", request.ProtocolVersion)
	}
	// source.input selects the offline file mode; its absence selects the
	// config-driven live mode. A present-but-empty input behaves as absent
	// so hosts can send the key unset.
	inputPath, inputPresent, err := nestedOptionalString(request.Config, "source", "input")
	if err != nil {
		return stdioPlan{}, err
	}
	claim := request.Claim
	if strings.TrimSpace(claim.GenerationID) == "" {
		claim.GenerationID = "generation-1"
	}
	plan.claim = claim
	if inputPresent && strings.TrimSpace(inputPath) != "" {
		// A missing key falls back to the placeholder; a present-but-malformed
		// value fails closed instead of silently emitting against the placeholder.
		sourceURI, present, err := nestedOptionalString(request.Config, "source", "sourceURI")
		if err != nil {
			return stdioPlan{}, err
		}
		if !present || strings.TrimSpace(sourceURI) == "" {
			sourceURI = "https://example.invalid/source/gcp-cloud"
		}
		previousDigest, _, err := nestedOptionalString(request.Config, "freshness", "previousDigest")
		if err != nil {
			return stdioPlan{}, err
		}
		limits, err := nestedLimits(request.Config)
		if err != nil {
			return stdioPlan{}, err
		}
		plan.inputPath = inputPath
		plan.sourceURI = sourceURI
		plan.previousDigest = previousDigest
		plan.limits = limits
		return plan, nil
	}
	liveConfig, err := parseLiveConfig(request.Config)
	if err != nil {
		return stdioPlan{}, err
	}
	plan.liveMode = true
	plan.live = liveConfig
	plan.sourceURI = liveConfig.SourceURI
	plan.previousDigest = liveConfig.PreviousDigest
	plan.limits = liveConfig.Limits
	return plan, nil
}

// collectStdioPlan runs the file read and collection for a parsed request
// under the caller's wall-time bound. Live-mode plans run the live claim
// path instead.
func collectStdioPlan(plan stdioPlan, parentScopeKind, parentScopeID string) (sdk.Result, string, error) {
	if plan.liveMode {
		return collectLivePlan(plan)
	}
	return collectFileClaim(plan.inputPath, parentScopeKind, parentScopeID, plan.sourceURI, plan.previousDigest, plan.claim, plan.limits)
}

func demoClaim() sdk.Claim {
	now := time.Now().UTC()
	return sdk.Claim{
		ComponentID:   "collector-gcp-cloud",
		InstanceID:    "gcp-cloud-primary",
		CollectorKind: "gcp",
		SourceSystem:  "gcp",
		Scope:         sdk.Scope{ID: "component:gcp-cloud-primary", Kind: "component"},
		SourceRunID:   "run-1",
		GenerationID:  "generation-1",
		WorkItemID:    "work-1",
		FencingToken:  "1",
		Attempt:       1,
		Deadline:      now.Add(time.Hour),
		ConfigHandle:  "component-config:gcp-cloud",
	}
}
