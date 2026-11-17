package integrity

import (
	"errors"
	"strconv"
	"strings"
)

// Integrity failure codes classify distinguishable Engine-owned integrity
// violations. They are stable, path-free identifiers.
const (
	// CodeEvidenceSchemaUnsupported reports an integrity evidence document whose
	// schema version is not supported.
	CodeEvidenceSchemaUnsupported = "integrity_evidence_schema_unsupported"
	// CodeEvidenceInvalid reports structurally invalid integrity evidence.
	CodeEvidenceInvalid = "integrity_evidence_invalid"
	// CodeEvidenceUnrequested reports evidence naming a resource that the
	// Engine-authored integrity request did not ask about.
	CodeEvidenceUnrequested = "integrity_evidence_unrequested_resource"

	// CodeLockUnavailable reports a project lock that cannot be read or parsed.
	CodeLockUnavailable = "lock_unavailable"
	// CodeLockDigestMalformed reports a locked digest that is not canonical.
	CodeLockDigestMalformed = "lock_digest_malformed"
	// CodeLockResourceMissing reports a declared resource absent from the
	// required project-lock scope.
	CodeLockResourceMissing = "lock_resource_missing"
	// CodeLockSourceMismatch reports source bytes changed after lock creation.
	CodeLockSourceMismatch = "lock_source_mismatch"

	// CodeMaterializationMismatch reports materialized bytes that differ from
	// the accepted source bytes.
	CodeMaterializationMismatch = "materialization_content_mismatch"
	// CodeMaterializationMissing reports a declared resource that is absent from
	// the prepared attempt workspace.
	CodeMaterializationMissing = "materialized_resource_missing"

	// CodeRuntimeBaselineMissing reports a required runtime-reported baseline
	// digest that is absent.
	CodeRuntimeBaselineMissing = "runtime_baseline_digest_missing"
	// CodeRuntimeBaselineMalformed reports a runtime-reported baseline digest
	// that is not canonical.
	CodeRuntimeBaselineMalformed = "runtime_baseline_digest_malformed"
	// CodeRuntimeBaselineMismatch reports runtime-reported baseline bytes that
	// differ from the Engine materialized baseline.
	CodeRuntimeBaselineMismatch = "runtime_baseline_mismatch"

	// CodeResultDigestMissing reports a required persisted-result digest that is
	// absent.
	CodeResultDigestMissing = "persisted_result_digest_missing"
	// CodeResultDigestMalformed reports a persisted-result digest that is not
	// canonical.
	CodeResultDigestMalformed = "persisted_result_digest_malformed"
	// CodeResultFileMissing reports a persisted result file that does not exist.
	CodeResultFileMissing = "persisted_result_file_missing"
	// CodeResultFileInaccessible reports a persisted result file that cannot be
	// read as a regular file.
	CodeResultFileInaccessible = "persisted_result_file_inaccessible"
	// CodeResultMismatch reports a persisted-result digest that differs from the
	// Engine-computed digest of the persisted bytes.
	CodeResultMismatch = "persisted_result_content_mismatch"

	// CodeProvenanceConflict reports contradictory or duplicated provenance
	// evidence for one resource identity.
	CodeProvenanceConflict = "integrity_provenance_conflict"
)

// Failure is an Engine-owned integrity violation with stable classification.
//
// Error text is intentionally derived only from stable fields: codes, logical
// resource identity, attempt-relative coordinates, and digests. Host absolute
// paths and operational diagnostics are never rendered, so failure text and
// failure-derived record identity do not depend on where a project is
// checked out. The underlying operational cause remains reachable through
// Unwrap.
type Failure struct {
	Code           string
	LogicalID      string
	Coordinate     string
	ExpectedDigest Digest
	ActualDigest   Digest
	Message        string
	Err            error
}

// Error renders the stable, path-free failure description.
func (f *Failure) Error() string {
	if f == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("resource integrity")
	if f.Code != "" {
		b.WriteString(": ")
		b.WriteString(f.Code)
	}
	if f.LogicalID != "" {
		b.WriteString(": resource=")
		b.WriteString(strconv.Quote(f.LogicalID))
	}
	if f.Coordinate != "" {
		b.WriteString(": coordinate=")
		b.WriteString(strconv.Quote(f.Coordinate))
	}
	if !f.ExpectedDigest.IsZero() {
		b.WriteString(": expected=")
		b.WriteString(f.ExpectedDigest.String())
	}
	if !f.ActualDigest.IsZero() {
		b.WriteString(": actual=")
		b.WriteString(f.ActualDigest.String())
	}
	if f.Message != "" {
		b.WriteString(": ")
		b.WriteString(f.Message)
	}
	return b.String()
}

// Unwrap returns the operational cause, when one was retained.
func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Err
}

// AsFailure returns the Engine-owned integrity failure carried by err.
func AsFailure(err error) *Failure {
	if err == nil {
		return nil
	}
	var failure *Failure
	if errors.As(err, &failure) {
		return failure
	}
	return nil
}
