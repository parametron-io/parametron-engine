package integrity

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestFailureErrorRendersOnlyStableFields(t *testing.T) {
	cause := errors.New("open /secret/checkout/source.FCStd: permission denied")
	failure := &Failure{
		Code:           CodeMaterializationMismatch,
		LogicalID:      "widget_model",
		Coordinate:     "source/Widget.FCStd",
		ExpectedDigest: Digest(strings.Repeat("a", DigestLength)),
		ActualDigest:   Digest(strings.Repeat("b", DigestLength)),
		Message:        "materialized baseline bytes differ from the accepted source bytes",
		Err:            cause,
	}

	got := failure.Error()
	for _, want := range []string{
		CodeMaterializationMismatch,
		`resource="widget_model"`,
		`coordinate="source/Widget.FCStd"`,
		strings.Repeat("a", DigestLength),
		strings.Repeat("b", DigestLength),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Failure.Error() = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "/secret/checkout") || strings.Contains(got, cause.Error()) {
		t.Fatalf("Failure.Error() leaked the operational cause: %q", got)
	}
	if !errors.Is(failure, cause) {
		t.Fatal("Failure does not unwrap to its operational cause")
	}
}

func TestFailureErrorOmitsUnsetFields(t *testing.T) {
	failure := &Failure{Code: CodeLockUnavailable}
	if got := failure.Error(); got != "resource integrity: "+CodeLockUnavailable {
		t.Fatalf("Failure.Error() = %q, want only the code", got)
	}
	var nilFailure *Failure
	if got := nilFailure.Error(); got != "" {
		t.Fatalf("nil Failure.Error() = %q, want empty", got)
	}
	if nilFailure.Unwrap() != nil {
		t.Fatal("nil Failure.Unwrap() is not nil")
	}
}

func TestAsFailureExtractsWrappedFailure(t *testing.T) {
	failure := &Failure{Code: CodeLockSourceMismatch, LogicalID: "widget_model"}
	wrapped := fmt.Errorf("dispatch: %w", failure)

	got := AsFailure(wrapped)
	if got != failure {
		t.Fatalf("AsFailure(wrapped) = %#v, want the original failure", got)
	}
	if AsFailure(errors.New("plain")) != nil {
		t.Fatal("AsFailure reported a failure for an unrelated error")
	}
	if AsFailure(nil) != nil {
		t.Fatal("AsFailure(nil) is not nil")
	}
}

func TestIntegrityFailureCodesAreDistinctAndStable(t *testing.T) {
	codes := []string{
		CodeEvidenceSchemaUnsupported,
		CodeEvidenceInvalid,
		CodeEvidenceUnrequested,
		CodeLockUnavailable,
		CodeLockDigestMalformed,
		CodeLockResourceMissing,
		CodeLockSourceMismatch,
		CodeMaterializationMismatch,
		CodeMaterializationMissing,
		CodeRuntimeBaselineMissing,
		CodeRuntimeBaselineMalformed,
		CodeRuntimeBaselineMismatch,
		CodeResultDigestMissing,
		CodeResultDigestMalformed,
		CodeResultFileMissing,
		CodeResultFileInaccessible,
		CodeResultMismatch,
		CodeProvenanceConflict,
	}
	want := []string{
		"integrity_evidence_schema_unsupported",
		"integrity_evidence_invalid",
		"integrity_evidence_unrequested_resource",
		"lock_unavailable",
		"lock_digest_malformed",
		"lock_resource_missing",
		"lock_source_mismatch",
		"materialization_content_mismatch",
		"materialized_resource_missing",
		"runtime_baseline_digest_missing",
		"runtime_baseline_digest_malformed",
		"runtime_baseline_mismatch",
		"persisted_result_digest_missing",
		"persisted_result_digest_malformed",
		"persisted_result_file_missing",
		"persisted_result_file_inaccessible",
		"persisted_result_content_mismatch",
		"integrity_provenance_conflict",
	}
	if len(codes) != len(want) {
		t.Fatalf("code table length %d does not match expectation %d", len(codes), len(want))
	}
	seen := make(map[string]struct{}, len(codes))
	for index, code := range codes {
		if code != want[index] {
			t.Fatalf("code %d = %q, want %q", index, code, want[index])
		}
		if _, exists := seen[code]; exists {
			t.Fatalf("duplicate failure code %q", code)
		}
		seen[code] = struct{}{}
	}
}
