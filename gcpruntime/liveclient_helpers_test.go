// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud"
	"github.com/eshu-hq/eshu-collector-gcp-cloud/gcpcloud/catalog"
)

// TestLiveParentPathCoversScopeKinds proves every supported parent scope kind
// resolves to its assets API path, and malformed scopes fail closed.
func TestLiveParentPathCoversScopeKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind catalog.ParentScopeKind
		id   string
		want string
		fail bool
	}{
		{name: "project", kind: catalog.ParentScopeProject, id: "boat-trader", want: "projects/boat-trader"},
		{name: "folder", kind: catalog.ParentScopeFolder, id: "123", want: "folders/123"},
		{name: "organization", kind: catalog.ParentScopeOrganization, id: "456", want: "organizations/456"},
		{name: "empty id", kind: catalog.ParentScopeProject, id: "  ", fail: true},
		{name: "path injection", kind: catalog.ParentScopeProject, id: "a/b", fail: true},
		{name: "unsupported kind", kind: "fleet", id: "boat-trader", fail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := liveParentPath(ScopeConfig{ParentScopeKind: gcpcloud.ParentScopeKind(tc.kind), ParentScopeID: tc.id})
			if tc.fail {
				if err == nil {
					t.Fatalf("liveParentPath(%q, %q) error = nil, want failure", tc.kind, tc.id)
				}
				return
			}
			if err != nil {
				t.Fatalf("liveParentPath: %v", err)
			}
			if got != tc.want {
				t.Fatalf("liveParentPath = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLiveContentTypeCoversFamilies proves every documented content family
// maps to its assets API content type and unknown families warn unsupported.
func TestLiveContentTypeCoversFamilies(t *testing.T) {
	t.Parallel()

	for _, family := range []string{"resource", "iam_policy", "org_policy", "access_policy", "os_inventory", "relationship", " iam-policy "} {
		got, err := liveContentType(family)
		if err != nil {
			t.Fatalf("liveContentType(%q): %v", family, err)
		}
		if got == "" {
			t.Fatalf("liveContentType(%q) = empty, want a content type", family)
		}
	}
	if _, err := liveContentType("billing"); err == nil {
		t.Fatal("liveContentType(billing) error = nil, want unsupported warning")
	}
}

// TestLiveAssetTypesCoversFamilies proves the asset family filter mapping:
// empty and mixed list everything, bare names get the bounded wildcard
// suffix, fully qualified names pass through, and junk fails closed.
func TestLiveAssetTypesCoversFamilies(t *testing.T) {
	t.Parallel()

	for _, family := range []string{"", "mixed", "  "} {
		types, err := liveAssetTypes(family)
		if err != nil || len(types) != 0 {
			t.Fatalf("liveAssetTypes(%q) = %v, %v, want empty and nil error", family, types, err)
		}
	}
	types, err := liveAssetTypes("compute, storage.googleapis.com/Bucket")
	if err != nil {
		t.Fatalf("liveAssetTypes: %v", err)
	}
	if len(types) != 2 || types[0] != "compute.googleapis.com.*" || types[1] != "storage.googleapis.com/Bucket" {
		t.Fatalf("liveAssetTypes = %v, want bounded wildcard plus passthrough", types)
	}
	if _, err := liveAssetTypes("UPPER!"); err == nil {
		t.Fatal("liveAssetTypes(UPPER!) error = nil, want unsupported warning")
	}
}

// TestClassifyLiveStatusCoversBranches proves every status branch maps to the
// documented warning kind, and 5xx stays retryable while 4xx does not.
func TestClassifyLiveStatusCoversBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status    int
		kind      string
		retryable bool
	}{
		{http.StatusUnauthorized, catalog.WarningKindPartialPermission, true},
		{http.StatusForbidden, catalog.WarningKindPartialPermission, false},
		{http.StatusBadRequest, catalog.WarningKindUnsupported, false},
		{http.StatusNotFound, catalog.WarningKindUnsupported, false},
		{http.StatusNotImplemented, catalog.WarningKindUnsupported, false},
		{http.StatusTooManyRequests, catalog.WarningKindQuota, true},
		{http.StatusBadGateway, catalog.WarningKindUnavailable, true},
		{http.StatusTeapot, catalog.WarningKindUnavailable, false},
	}
	for _, tc := range cases {
		got := classifyLiveStatus(tc.status)
		if got.WarningKind != tc.kind {
			t.Errorf("classifyLiveStatus(%d).WarningKind = %q, want %q", tc.status, got.WarningKind, tc.kind)
		}
		if got.Retryable != tc.retryable {
			t.Errorf("classifyLiveStatus(%d).Retryable = %v, want %v", tc.status, got.Retryable, tc.retryable)
		}
	}
}

// TestReadBoundedCoversLimits proves reads pass under the limit, exact-size
// reads pass, oversized reads fail closed, and reader errors propagate.
func TestReadBoundedCoversLimits(t *testing.T) {
	t.Parallel()

	if got, err := readBounded(strings.NewReader("abc"), 3); err != nil || string(got) != "abc" {
		t.Fatalf("readBounded(exact) = %q, %v, want abc and nil error", got, err)
	}
	if _, err := readBounded(strings.NewReader("abcd"), 3); err == nil {
		t.Fatal("readBounded(over) error = nil, want limit failure")
	}
	if _, err := readBounded(errorReader{}, 8); err == nil {
		t.Fatal("readBounded(reader error) error = nil, want propagation")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("8856 read boom") }

// TestLiveClientKnobDefaults proves zero-value clients fall back to the
// catalog budgets and explicit knobs (including the page-size clamp and the
// custom backoff hook) take effect.
func TestLiveClientKnobDefaults(t *testing.T) {
	t.Parallel()

	var zero LiveClient
	if got := zero.pageSize(); got != catalog.DefaultLivePageSize {
		t.Errorf("pageSize() = %d, want default %d", got, catalog.DefaultLivePageSize)
	}
	if got := zero.requestTimeout(); got != catalog.DefaultLiveRequestTimeout {
		t.Errorf("requestTimeout() = %v, want default %v", got, catalog.DefaultLiveRequestTimeout)
	}
	if got := zero.maxAttempts(); got != catalog.DefaultLiveMaxAttempts {
		t.Errorf("maxAttempts() = %d, want default %d", got, catalog.DefaultLiveMaxAttempts)
	}
	if got := zero.maxResponseBytes(); got != catalog.DefaultLiveResponseBytes {
		t.Errorf("maxResponseBytes() = %d, want default %d", got, catalog.DefaultLiveResponseBytes)
	}
	if zero.httpClient() != http.DefaultClient {
		t.Error("httpClient() without override does not return the default client")
	}

	custom := LiveClient{
		HTTPClient:       &http.Client{},
		PageSize:         catalog.MaxLivePageSize + 1,
		RequestTimeout:   time.Second,
		MaxAttempts:      5,
		MaxResponseBytes: 8,
		RetryBackoff:     func(int) time.Duration { return 7 * time.Millisecond },
	}
	if got := custom.pageSize(); got != catalog.MaxLivePageSize {
		t.Errorf("pageSize() over max = %d, want clamp %d", got, catalog.MaxLivePageSize)
	}
	if custom.httpClient() != custom.HTTPClient {
		t.Error("httpClient() with override does not return the custom client")
	}
	if got := custom.backoff(2); got != 7*time.Millisecond {
		t.Errorf("backoff() with hook = %v, want the hook value", got)
	}
	plain := LiveClient{}
	if got := plain.backoff(1); got != 100*time.Millisecond {
		t.Errorf("backoff(1) = %v, want 100ms", got)
	}
	if got := plain.backoff(99); got != time.Second {
		t.Errorf("backoff(99) = %v, want the 1s cap", got)
	}
	if got := plain.backoff(0); got != 100*time.Millisecond {
		t.Errorf("backoff(0) = %v, want the 100ms floor", got)
	}
}

// TestLiveClientSleepCoversHooks proves the sleep helper honors a canceled
// context on a zero delay, the custom hook, and a short real timer.
func TestLiveClientSleepCoversHooks(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (LiveClient{}.sleep(ctx, 0)); err == nil {
		t.Fatal("sleep(canceled, 0) error = nil, want context error")
	}
	hooked := LiveClient{Sleep: func(context.Context, time.Duration) error { return errors.New("8856 sleep boom") }}
	if err := hooked.sleep(context.Background(), time.Second); err == nil {
		t.Fatal("sleep(hook) error = nil, want hook error")
	}
	if err := (LiveClient{}.sleep(context.Background(), time.Millisecond)); err != nil {
		t.Fatalf("sleep(1ms): %v", err)
	}
}

// TestNewADCLiveClientFailsClosedWithoutCredentials proves the ADC
// constructor never returns a usable client when no credential is available.
func TestNewADCLiveClientFailsClosedWithoutCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent-8856-adc.json")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")

	if _, err := NewADCLiveClient(context.Background(), "  ", ""); err == nil {
		t.Fatal("NewADCLiveClient(blank ref) error = nil, want failure")
	}
	if _, err := NewADCLiveClient(context.Background(), "sa-test", ""); err == nil {
		t.Fatal("NewADCLiveClient(no ADC) error = nil, want failure")
	}
}

// TestLiveClientTokenFailsClosed proves the token helper rejects a missing
// source, a source error, and an empty access token.
func TestLiveClientTokenFailsClosed(t *testing.T) {
	t.Parallel()

	if _, err := (LiveClient{}.token()); err == nil {
		t.Fatal("token() without source error = nil, want failure")
	}
	broken := LiveClient{TokenSource: errorTokenSource{err: errors.New("8856 token boom")}}
	if _, err := broken.token(); err == nil {
		t.Fatal("token() with failing source error = nil, want warning")
	}
	empty := LiveClient{TokenSource: errorTokenSource{token: &oauth2.Token{}}}
	if _, err := empty.token(); err == nil {
		t.Fatal("token() with empty token error = nil, want warning")
	}
	good := LiveClient{TokenSource: errorTokenSource{token: &oauth2.Token{AccessToken: "ya29.test"}}}
	token, err := good.token()
	if err != nil || token.AccessToken != "ya29.test" {
		t.Fatalf("token() = %v, %v, want the source token", token, err)
	}
}

type errorTokenSource struct {
	token *oauth2.Token
	err   error
}

func (s errorTokenSource) Token() (*oauth2.Token, error) { return s.token, s.err }

// TestLiveClientFetchPageFailsClosedWithoutToken proves no HTTP request is
// attempted when the client has no usable credential.
func TestLiveClientFetchPageFailsClosedWithoutToken(t *testing.T) {
	t.Parallel()

	req := PageRequest{Scope: ScopeConfig{
		ParentScopeKind: catalog.ParentScopeProject,
		ParentScopeID:   "boat-trader",
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
	}}
	if _, err := (LiveClient{}.FetchPage(context.Background(), req)); err == nil {
		t.Fatal("FetchPage() without token source error = nil, want failure")
	}
}
