// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

// Provenance: kind and schema-version constants ported from
// go/internal/facts/cloud/gcp.go at core commit 57d11add46. Values are pinned
// byte-exact by the parity proof test against the checked-in fixture pack;
// do not change them without a contract migration.

// GCP fact kinds and schema versions.
const (
	GCPCloudResourceFactKind        = "gcp_cloud_resource"
	GCPCollectionWarningFactKind    = "gcp_collection_warning"
	GCPCloudRelationshipFactKind    = "gcp_cloud_relationship"
	GCPTagObservationFactKind       = "gcp_tag_observation"
	GCPIAMPolicyObservationFactKind = "gcp_iam_policy_observation"
	GCPDNSRecordFactKind            = "gcp_dns_record"
	GCPImageReferenceFactKind       = "gcp_image_reference"

	GCPCloudResourceSchemaVersion        = "1.1.0"
	GCPCollectionWarningSchemaVersion    = "1.0.0"
	GCPCloudRelationshipSchemaVersion    = "1.0.0"
	GCPTagObservationSchemaVersion       = "1.0.0"
	GCPIAMPolicyObservationSchemaVersion = "1.0.0"
	GCPDNSRecordSchemaVersion            = "1.0.0"
	GCPImageReferenceSchemaVersion       = "1.0.0"
)

// GCPFactKinds lists every gcp_* kind this collector emits.
func GCPFactKinds() []string {
	return []string{
		GCPCloudResourceFactKind,
		GCPCloudRelationshipFactKind,
		GCPTagObservationFactKind,
		GCPIAMPolicyObservationFactKind,
		GCPDNSRecordFactKind,
		GCPImageReferenceFactKind,
		GCPCollectionWarningFactKind,
	}
}

// Secrets/IAM mirror kinds carried by this collector.
const (
	GCPIAMPrincipalFactKind        = "gcp_iam_principal"
	GCPIAMTrustPolicyFactKind      = "gcp_iam_trust_policy"
	GCPIAMPermissionPolicyFactKind = "gcp_iam_permission_policy"

	SecretsIAMSchemaVersionV1 = "1.0.0"
)
