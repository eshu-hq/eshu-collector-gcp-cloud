// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package serverless holds the GCP serverless typed-depth extractors: one file
// per CAI asset type for App Engine, Cloud Functions, Cloud Scheduler, Cloud
// Tasks, Composer, Eventarc, Cloud Run, and Workflows.
//
// Each asset type registers its extractor from an init function against the
// extractor leaf registry; the collector binary imports this package for its
// side effects (see TestServerlessExtractorsRegistered in
// cmd/collector-gcp-cloud). Extractors receive the raw CAI resource.data
// transiently and return only bounded, redaction-safe attributes, correlation
// anchors, and typed relationships. They never persist the data blob and never
// return secrets, data-plane content, raw policy JSON, IPs, or response bodies.
//
// The package depends on the extractor leaf, the catalog, and one narrow
// parent edge: gcpcloud.GCPServiceAccountEmailDigest. The digest is the
// cross-package join anchor between serverless attributes and the IAM trust
// facts the parent emits for the same service account email, so it must stay
// single-sourced; a local copy would let the two sides drift silently. The
// edge is acyclic: the parent never imports this package (registrations flow
// through the leaf registry, and the runtime pulls this package in for its
// side effects), so the build itself forbids a future cycle.
package serverless
