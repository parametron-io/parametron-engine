package cadruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/integrity"
)

const (
	evidenceDigestAlpha = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f60a1b"
	evidenceDigestZeta  = "0123456789ab0123456789ab0123456789ab0123456789ab0123456789abcdef"
	evidenceDigestOther = "f0e1d2c3b4a5f0e1d2c3b4a5f0e1d2c3b4a5f0e1d2c3b4a5f0e1d2c3b4a56789"
)

func integrityEvidenceDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func integrityEvidenceAttempt(t *testing.T) freecad.FreeCADRuntimeAttempt {
	t.Helper()
	dir := t.TempDir()
	layout := freecad.FreeCADRuntimeWorkingCopyLayout{
		WorkingCopyDir:     dir,
		SourceDocument:     "source/Alpha.FCStd",
		SourceDocumentPath: filepath.Join(dir, "source", "Alpha.FCStd"),
		OutputDir:          filepath.Join(dir, "outputs"),
	}
	if err := os.MkdirAll(filepath.Dir(layout.SourceDocumentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	return freecad.FreeCADRuntimeAttempt{Layout: layout}
}

func integrityEvidenceRequest() *FreeCADIntegrityRequest {
	return &FreeCADIntegrityRequest{
		SchemaVersion: FreeCADIntegrityRequestSchemaVersion,
		Resources: []FreeCADIntegrityRequestResource{
			{
				LogicalID: "alpha", Kind: "model", MaterializedCoordinate: "source/Alpha.FCStd",
				BaselineSHA256: evidenceDigestAlpha, ExpectsPersistedResult: true,
			},
			{
				LogicalID: "zeta", Kind: "model", MaterializedCoordinate: "resources/zeta/Zeta.FCStd",
				BaselineSHA256: evidenceDigestZeta, ExpectsPersistedResult: false,
			},
		},
	}
}

func observedEvidence(logicalID, coordinate, baseline string) FreeCADIntegrityEvidenceResource {
	return FreeCADIntegrityEvidenceResource{
		LogicalID:              logicalID,
		MaterializedCoordinate: coordinate,
		Status:                 FreeCADIntegrityStatusObserved,
		BaselineSHA256:         baseline,
	}
}

func writePersistedResult(t *testing.T, attempt freecad.FreeCADRuntimeAttempt, content []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(attempt.Layout.SourceDocumentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attempt.Layout.SourceDocumentPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return integrityEvidenceDigest(content)
}

func evaluateIntegrity(t *testing.T, attempt freecad.FreeCADRuntimeAttempt, request *FreeCADIntegrityRequest, evidence *FreeCADIntegrityEvidence) (*integrity.OperationIntegrity, error) {
	t.Helper()
	return EvaluateFreeCADRuntimeIntegrity(attempt, request, evidence)
}

func TestEvaluateFreeCADRuntimeIntegrityAcceptsMatchingEvidence(t *testing.T) {
	attempt := integrityEvidenceAttempt(t)
	persistedDigest := writePersistedResult(t, attempt, []byte("mutated and persisted configured result"))
	if persistedDigest == evidenceDigestAlpha {
		t.Fatal("test fixture result digest unexpectedly equals the baseline digest")
	}

	evidence := &FreeCADIntegrityEvidence{
		SchemaVersion: FreeCADIntegritySchemaVersion,
		Resources: []FreeCADIntegrityEvidenceResource{
			{
				LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd",
				Status: FreeCADIntegrityStatusObserved, BaselineSHA256: evidenceDigestAlpha,
				PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: persistedDigest},
			},
			observedEvidence("zeta", "resources/zeta/Zeta.FCStd", evidenceDigestZeta),
		},
	}

	operation, err := evaluateIntegrity(t, attempt, integrityEvidenceRequest(), evidence)
	if err != nil {
		t.Fatalf("EvaluateFreeCADRuntimeIntegrity returned error: %v", err)
	}
	resources := operation.Resources
	if len(resources) != 2 || resources[0].LogicalID != "alpha" || resources[1].LogicalID != "zeta" {
		t.Fatalf("operation resources = %#v, want both resources in logical ID order", resources)
	}
	alpha := resources[0]
	if alpha.MaterializedDigest.String() != evidenceDigestAlpha || alpha.MaterializationStatus != integrity.StatusVerified {
		t.Fatalf("alpha materialization = %#v, want the requested baseline verified", alpha)
	}
	if alpha.RuntimeBaselineDigest.String() != evidenceDigestAlpha || alpha.RuntimeBaselineStatus != integrity.StatusVerified {
		t.Fatalf("alpha runtime baseline = %#v, want the matching reported baseline verified", alpha)
	}
	if alpha.PersistedResultCoordinate != "source/Alpha.FCStd" {
		t.Fatalf("alpha persisted coordinate = %q, want the Engine coordinate", alpha.PersistedResultCoordinate)
	}
	if alpha.PersistedResultDigest.String() != persistedDigest || alpha.ResultStatus != integrity.StatusVerified {
		t.Fatalf("alpha persisted result = %#v, want the Engine-computed digest verified", alpha)
	}
	if alpha.PersistedResultDigest.String() == alpha.MaterializedDigest.String() {
		t.Fatal("configured-result digest replaced the materialized baseline identity")
	}

	zeta := resources[1]
	if zeta.RuntimeBaselineStatus != integrity.StatusVerified || zeta.ResultStatus != integrity.StatusNotReported {
		t.Fatalf("zeta statuses = %#v, want a verified baseline and no persisted result", zeta)
	}
	if zeta.PersistedResultDigest != "" {
		t.Fatalf("zeta gained a persisted result identity: %#v", zeta)
	}
}

func TestEvaluateFreeCADRuntimeIntegrityMissingBaselineEvidence(t *testing.T) {
	tests := []struct {
		name     string
		evidence *FreeCADIntegrityEvidence
	}{
		{
			name: "observed resource without baseline digest",
			evidence: &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
				{LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved},
			}},
		},
		{
			name: "primary resource omitted from evidence",
			evidence: &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
				observedEvidence("zeta", "resources/zeta/Zeta.FCStd", evidenceDigestZeta),
			}},
		},
		{
			name:     "evidence is entirely absent",
			evidence: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attempt := integrityEvidenceAttempt(t)
			operation, err := evaluateIntegrity(t, attempt, integrityEvidenceRequest(), tc.evidence)
			if tc.evidence == nil {
				if err != nil {
					t.Fatalf("absent evidence returned error: %v", err)
				}
				if len(operation.Resources) != 2 {
					t.Fatalf("absent evidence dropped requested resources: %#v", operation.Resources)
				}
				for _, resource := range operation.Resources {
					if resource.RuntimeBaselineStatus != integrity.StatusNotReported {
						t.Fatalf("absent evidence claimed runtime baseline status %q for %q", resource.RuntimeBaselineStatus, resource.LogicalID)
					}
				}
				return
			}
			failure := integrity.AsFailure(err)
			if failure == nil || failure.Code != integrity.CodeRuntimeBaselineMissing {
				t.Fatalf("error = %v, want runtime_baseline_digest_missing", err)
			}
			if failure.LogicalID != "alpha" {
				t.Fatalf("failure logical ID = %q, want alpha", failure.LogicalID)
			}
			if operation == nil || operation.Resources[0].RuntimeBaselineStatus != integrity.StatusMissing {
				t.Fatalf("operation = %#v, want a missing baseline status", operation)
			}
		})
	}
}

func TestEvaluateFreeCADRuntimeIntegrityMalformedBaselineDigest(t *testing.T) {
	attempt := integrityEvidenceAttempt(t)
	request := integrityEvidenceRequest()
	evidence := &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
		{LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved, BaselineSHA256: strings.ToUpper(evidenceDigestAlpha)},
	}}
	_, err := evaluateIntegrity(t, attempt, request, evidence)
	failure := integrity.AsFailure(err)
	if failure == nil || failure.Code != integrity.CodeRuntimeBaselineMalformed {
		t.Fatalf("error = %v, want runtime_baseline_digest_malformed", err)
	}
}

func TestEvaluateFreeCADRuntimeIntegrityBaselineMismatchIsDistinct(t *testing.T) {
	attempt := integrityEvidenceAttempt(t)
	evidence := &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
		{LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved, BaselineSHA256: evidenceDigestOther},
	}}
	operation, err := evaluateIntegrity(t, attempt, integrityEvidenceRequest(), evidence)
	failure := integrity.AsFailure(err)
	if failure == nil || failure.Code != integrity.CodeRuntimeBaselineMismatch {
		t.Fatalf("error = %v, want runtime_baseline_mismatch", err)
	}
	if failure.Code == integrity.CodeMaterializationMismatch || failure.Code == integrity.CodeLockSourceMismatch {
		t.Fatalf("runtime baseline mismatch collapsed into %q", failure.Code)
	}
	if failure.ExpectedDigest.String() != evidenceDigestAlpha || failure.ActualDigest.String() != evidenceDigestOther {
		t.Fatalf("failure digests = %q/%q, want Engine baseline and runtime-reported baseline",
			failure.ExpectedDigest, failure.ActualDigest)
	}
	if operation.Resources[0].RuntimeBaselineStatus != integrity.StatusMismatch {
		t.Fatalf("runtime baseline status = %q, want mismatch", operation.Resources[0].RuntimeBaselineStatus)
	}
}

func TestEvaluateFreeCADRuntimeIntegrityRejectsUnrequestedCoordinate(t *testing.T) {
	attempt := integrityEvidenceAttempt(t)
	evidence := &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
		observedEvidence("other", "source/Other.FCStd", evidenceDigestOther),
	}}
	_, err := evaluateIntegrity(t, attempt, integrityEvidenceRequest(), evidence)
	failure := integrity.AsFailure(err)
	if failure == nil || failure.Code != integrity.CodeEvidenceUnrequested {
		t.Fatalf("error = %v, want integrity_evidence_unrequested_resource", err)
	}
	if failure.Coordinate != "source/Other.FCStd" {
		t.Fatalf("failure coordinate = %q, want the unrequested coordinate", failure.Coordinate)
	}
}

func TestEvaluateFreeCADRuntimeIntegrityNonObservedSecondaryStatuses(t *testing.T) {
	for _, status := range []string{
		FreeCADIntegrityStatusUnavailable,
		FreeCADIntegrityStatusMissing,
		FreeCADIntegrityStatusUnsupported,
	} {
		t.Run(status, func(t *testing.T) {
			attempt := integrityEvidenceAttempt(t)
			persistedDigest := writePersistedResult(t, attempt, []byte("persisted bytes"))
			secondaryStatus := map[string]integrity.Status{
				FreeCADIntegrityStatusUnavailable: integrity.StatusUnavailable,
				FreeCADIntegrityStatusMissing:     integrity.StatusMissing,
				FreeCADIntegrityStatusUnsupported: integrity.StatusUnsupported,
			}[status]
			evidence := &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
				{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd",
					Status: FreeCADIntegrityStatusObserved, BaselineSHA256: evidenceDigestAlpha,
					PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: persistedDigest},
				},
				{LogicalID: "zeta", MaterializedCoordinate: "resources/zeta/Zeta.FCStd", Status: status},
			}}
			operation, err := evaluateIntegrity(t, attempt, integrityEvidenceRequest(), evidence)
			if err != nil {
				t.Fatalf("EvaluateFreeCADRuntimeIntegrity returned error: %v", err)
			}
			zeta := operation.Resources[1]
			if zeta.LogicalID != "zeta" || zeta.RuntimeBaselineStatus != secondaryStatus {
				t.Fatalf("zeta = %#v, want runtime baseline status %q", zeta, secondaryStatus)
			}
			if zeta.ResultStatus != integrity.StatusNotReported {
				t.Fatalf("zeta result status = %q, want not_reported", zeta.ResultStatus)
			}
		})
	}
}

func TestEvaluateFreeCADRuntimeIntegrityNonObservedPrimaryIsRejected(t *testing.T) {
	for _, status := range []string{
		FreeCADIntegrityStatusUnavailable,
		FreeCADIntegrityStatusMissing,
		FreeCADIntegrityStatusUnsupported,
	} {
		t.Run(status, func(t *testing.T) {
			attempt := integrityEvidenceAttempt(t)
			evidence := &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
				{LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: status},
			}}
			_, err := evaluateIntegrity(t, attempt, integrityEvidenceRequest(), evidence)
			failure := integrity.AsFailure(err)
			if failure == nil || failure.Code != integrity.CodeRuntimeBaselineMissing {
				t.Fatalf("error = %v, want runtime_baseline_digest_missing for primary status %q", err, status)
			}
		})
	}
}

func TestEvaluateFreeCADRuntimeIntegrityPersistedResultValidation(t *testing.T) {
	type setup func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource
	tests := []struct {
		name     string
		setup    setup
		code     string
		status   integrity.Status
		verify   bool
		reported string
	}{
		{
			name: "valid reported digest matches Engine computed digest",
			setup: func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource {
				digest := writePersistedResult(t, attempt, []byte("valid configured result"))
				return &FreeCADIntegrityEvidenceResource{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
					BaselineSHA256:  evidenceDigestAlpha,
					PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: digest},
				}
			},
			verify: true,
			status: integrity.StatusVerified,
		},
		{
			name: "missing persisted result object",
			setup: func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource {
				writePersistedResult(t, attempt, []byte("bytes"))
				return &FreeCADIntegrityEvidenceResource{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
					BaselineSHA256: evidenceDigestAlpha,
				}
			},
			code:   integrity.CodeResultDigestMissing,
			status: integrity.StatusMissing,
		},
		{
			name: "missing persisted result digest",
			setup: func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource {
				writePersistedResult(t, attempt, []byte("bytes"))
				return &FreeCADIntegrityEvidenceResource{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
					BaselineSHA256:  evidenceDigestAlpha,
					PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd"},
				}
			},
			code:   integrity.CodeResultDigestMissing,
			status: integrity.StatusMissing,
		},
		{
			name: "malformed persisted result digest",
			setup: func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource {
				writePersistedResult(t, attempt, []byte("bytes"))
				return &FreeCADIntegrityEvidenceResource{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
					BaselineSHA256:  evidenceDigestAlpha,
					PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: strings.ToUpper(evidenceDigestOther)},
				}
			},
			code:   integrity.CodeResultDigestMalformed,
			status: integrity.StatusMalformed,
		},
		{
			name: "missing persisted file",
			setup: func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource {
				return &FreeCADIntegrityEvidenceResource{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
					BaselineSHA256:  evidenceDigestAlpha,
					PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: evidenceDigestOther},
				}
			},
			code:   integrity.CodeResultFileMissing,
			status: integrity.StatusMissing,
		},
		{
			name: "inaccessible persisted file",
			setup: func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource {
				if err := os.MkdirAll(attempt.Layout.SourceDocumentPath, 0o755); err != nil {
					t.Fatal(err)
				}
				return &FreeCADIntegrityEvidenceResource{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
					BaselineSHA256:  evidenceDigestAlpha,
					PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: evidenceDigestOther},
				}
			},
			code:   integrity.CodeResultFileInaccessible,
			status: integrity.StatusInaccessible,
		},
		{
			name: "reported digest disagrees with Engine computed digest",
			setup: func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource {
				writePersistedResult(t, attempt, []byte("actual persisted configured bytes"))
				return &FreeCADIntegrityEvidenceResource{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
					BaselineSHA256:  evidenceDigestAlpha,
					PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: evidenceDigestOther},
				}
			},
			code:     integrity.CodeResultMismatch,
			status:   integrity.StatusMismatch,
			reported: evidenceDigestOther,
		},
		{
			name: "persisted coordinate conflicts with the request",
			setup: func(t *testing.T, attempt freecad.FreeCADRuntimeAttempt) *FreeCADIntegrityEvidenceResource {
				writePersistedResult(t, attempt, []byte("bytes"))
				return &FreeCADIntegrityEvidenceResource{
					LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
					BaselineSHA256:  evidenceDigestAlpha,
					PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Other.FCStd", SHA256: evidenceDigestOther},
				}
			},
			code:   integrity.CodeProvenanceConflict,
			status: integrity.StatusNotReported,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attempt := integrityEvidenceAttempt(t)
			resource := tc.setup(t, attempt)
			evidence := &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{*resource}}
			operation, err := evaluateIntegrity(t, attempt, integrityEvidenceRequest(), evidence)
			if tc.verify {
				if err != nil {
					t.Fatalf("EvaluateFreeCADRuntimeIntegrity returned error: %v", err)
				}
				alpha := operation.Resources[0]
				if alpha.ResultStatus != integrity.StatusVerified || alpha.PersistedResultDigest.String() != resource.PersistedResult.SHA256 {
					t.Fatalf("alpha result = %#v, want verified agreement", alpha)
				}
				if tc.reported != "" && alpha.PersistedResultDigest.String() != tc.reported {
					t.Fatalf("result digest = %q, want reported %q", alpha.PersistedResultDigest, tc.reported)
				}
				return
			}
			failure := integrity.AsFailure(err)
			if failure == nil || failure.Code != tc.code {
				t.Fatalf("error = %v, want integrity code %q", err, tc.code)
			}
			if operation == nil || len(operation.Resources) == 0 {
				t.Fatal("invalid result evidence did not retain the reached identity chain")
			}
			if got := operation.Resources[0].ResultStatus; got != tc.status {
				t.Fatalf("result status = %q, want %q", got, tc.status)
			}
		})
	}
}

func TestEvaluateFreeCADRuntimeIntegrityReportsDistinctEvidenceAndProvenanceConflict(t *testing.T) {
	attempt := integrityEvidenceAttempt(t)
	writePersistedResult(t, attempt, []byte("bytes"))
	evidence := &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
		{LogicalID: "different", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved, BaselineSHA256: evidenceDigestAlpha},
	}}
	_, err := evaluateIntegrity(t, attempt, integrityEvidenceRequest(), evidence)
	failure := integrity.AsFailure(err)
	if failure == nil || failure.Code != integrity.CodeProvenanceConflict {
		t.Fatalf("error = %v, want integrity_provenance_conflict", err)
	}
}

const validEvidenceResources = `{
    "logicalId": "alpha",
    "materializedCoordinate": "source/Alpha.FCStd",
    "status": "observed",
    "baselineSha256": "` + evidenceDigestAlpha + `",
    "persistedResult": {"coordinate": "source/Alpha.FCStd", "sha256": "` + evidenceDigestOther + `"}
  },
  {
    "logicalId": "zeta",
    "materializedCoordinate": "resources/zeta/Zeta.FCStd",
    "status": "observed",
    "baselineSha256": "` + evidenceDigestZeta + `",
    "persistedResult": null
  }`

func validEvidenceJSON(resources string) string {
	return `{"schemaVersion": "1.0", "resources": [` + resources + `]}`
}

func TestParseFreeCADIntegrityEvidenceStrictDecoding(t *testing.T) {
	valid := []byte(validEvidenceJSON(validEvidenceResources))
	parsed, err := ParseFreeCADIntegrityEvidence(valid)
	if err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	if len(parsed.Resources) != 2 {
		t.Fatalf("parsed resources = %d, want 2", len(parsed.Resources))
	}

	tests := []struct {
		name string
		data []byte
		code string
	}{
		{
			name: "unsupported schema version",
			data: []byte(strings.Replace(validEvidenceJSON(validEvidenceResources), `"schemaVersion": "1.0"`, `"schemaVersion": "9.9"`, 1)),
			code: integrity.CodeEvidenceSchemaUnsupported,
		},
		{
			name: "unknown field",
			data: []byte(strings.Replace(validEvidenceJSON(validEvidenceResources), `"status": "observed"`, `"status": "observed", "unexpected": true`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "trailing content",
			data: append(append([]byte(nil), valid...), []byte("\n[]")...),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "invalid json",
			data: []byte(`{"schemaVersion": "1.0", "resources": [`),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "duplicate materialized coordinate",
			data: []byte(validEvidenceJSON(`{
  "logicalId": "alpha",
  "materializedCoordinate": "source/Alpha.FCStd",
  "status": "observed",
  "baselineSha256": "` + evidenceDigestAlpha + `"
},
{
  "logicalId": "zeta",
  "materializedCoordinate": "source/Alpha.FCStd",
  "status": "observed",
  "baselineSha256": "` + evidenceDigestZeta + `"
}`)),
			code: integrity.CodeProvenanceConflict,
		},
		{
			name: "non-canonical baseline digest",
			data: []byte(strings.Replace(validEvidenceJSON(validEvidenceResources), evidenceDigestAlpha, strings.ToUpper(evidenceDigestAlpha), 1)),
			code: integrity.CodeRuntimeBaselineMalformed,
		},
		{
			name: "non-canonical persisted digest",
			data: []byte(strings.Replace(validEvidenceJSON(validEvidenceResources), evidenceDigestOther, strings.ToUpper(evidenceDigestOther), 1)),
			code: integrity.CodeResultDigestMalformed,
		},
		{
			name: "non-canonical materialized coordinate",
			data: []byte(strings.Replace(validEvidenceJSON(validEvidenceResources), `"materializedCoordinate": "source/Alpha.FCStd"`, `"materializedCoordinate": "../source/Alpha.FCStd"`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "non-canonical persisted coordinate",
			data: []byte(strings.Replace(validEvidenceJSON(validEvidenceResources), `"persistedResult": {"coordinate": "source/Alpha.FCStd"`, `"persistedResult": {"coordinate": "/source/Alpha.FCStd"`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "unknown status",
			data: []byte(strings.Replace(validEvidenceJSON(validEvidenceResources), `"status": "observed"`, `"status": "guessed"`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "non-observed status claims content identity",
			data: []byte(validEvidenceJSON(`{
  "logicalId": "zeta",
  "materializedCoordinate": "resources/zeta/Zeta.FCStd",
  "status": "unavailable",
  "baselineSha256": "` + evidenceDigestZeta + `"
}`)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "no resources",
			data: []byte(`{"schemaVersion": "1.0", "resources": []}`),
			code: integrity.CodeEvidenceInvalid,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFreeCADIntegrityEvidence(tc.data)
			failure := integrity.AsFailure(err)
			if failure == nil || failure.Code != tc.code {
				t.Fatalf("error = %v, want integrity code %q", err, tc.code)
			}
		})
	}
}

func TestParseFreeCADIntegrityEvidenceOrdersResourcesDeterministically(t *testing.T) {
	resources := []string{
		`{"logicalId": "zeta", "materializedCoordinate": "resources/zeta/Zeta.FCStd", "status": "observed", "baselineSha256": "` + evidenceDigestZeta + `"}`,
		`{"logicalId": "alpha", "materializedCoordinate": "source/Alpha.FCStd", "status": "observed", "baselineSha256": "` + evidenceDigestAlpha + `"}`,
		`{"logicalId": "middle", "materializedCoordinate": "resources/middle/Middle.FCStd", "status": "observed", "baselineSha256": "` + evidenceDigestOther + `"}`,
	}
	first, err := ParseFreeCADIntegrityEvidence([]byte(validEvidenceJSON(strings.Join(resources, ","))))
	if err != nil {
		t.Fatal(err)
	}
	sort.SliceStable(resources, func(i, j int) bool { return resources[i] > resources[j] })
	second, err := ParseFreeCADIntegrityEvidence([]byte(validEvidenceJSON(strings.Join(resources, ","))))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("evidence order changed parsed result:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	got := []string{first.Resources[0].MaterializedCoordinate, first.Resources[1].MaterializedCoordinate, first.Resources[2].MaterializedCoordinate}
	want := []string{"resources/middle/Middle.FCStd", "resources/zeta/Zeta.FCStd", "source/Alpha.FCStd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed order = %v, want coordinate order %v", got, want)
	}
}

func TestEvaluateFreeCADRuntimeIntegrityIsOrderAndRelocationInvariant(t *testing.T) {
	firstAttempt := integrityEvidenceAttempt(t)
	secondAttempt := integrityEvidenceAttempt(t)
	firstDigest := writePersistedResult(t, firstAttempt, []byte("identical persisted result"))
	secondDigest := writePersistedResult(t, secondAttempt, []byte("identical persisted result"))
	if firstDigest != secondDigest {
		t.Fatalf("identical bytes produced different persisted digests: %q / %q", firstDigest, secondDigest)
	}

	alpha := FreeCADIntegrityEvidenceResource{
		LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
		BaselineSHA256:  evidenceDigestAlpha,
		PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: firstDigest},
	}
	zeta := observedEvidence("zeta", "resources/zeta/Zeta.FCStd", evidenceDigestZeta)

	firstOperation, err := evaluateIntegrity(t, firstAttempt, integrityEvidenceRequest(), &FreeCADIntegrityEvidence{
		SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{alpha, zeta},
	})
	if err != nil {
		t.Fatal(err)
	}
	secondOperation, err := evaluateIntegrity(t, secondAttempt, integrityEvidenceRequest(), &FreeCADIntegrityEvidence{
		SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{zeta, alpha},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstOperation, secondOperation) {
		t.Fatalf("evidence order or absolute attempt root changed normalized provenance:\nfirst:  %#v\nsecond: %#v",
			firstOperation, secondOperation)
	}
	for _, resource := range firstOperation.Resources {
		if strings.Contains(resource.MaterializedCoordinate, firstAttempt.Layout.WorkingCopyDir) {
			t.Fatalf("provenance leaked a host path: %#v", resource)
		}
	}
}

func TestEvaluateFreeCADRuntimeIntegrityMultipleResourcesDoNotCollide(t *testing.T) {
	attempt := integrityEvidenceAttempt(t)
	// Two declared resources share a native filename but have distinct logical
	// IDs and distinct materialized coordinates.
	request := &FreeCADIntegrityRequest{SchemaVersion: FreeCADIntegrityRequestSchemaVersion, Resources: []FreeCADIntegrityRequestResource{
		{LogicalID: "alpha", Kind: "model", MaterializedCoordinate: "source/Alpha.FCStd", BaselineSHA256: evidenceDigestAlpha, ExpectsPersistedResult: true},
		{LogicalID: "beta", Kind: "model", MaterializedCoordinate: "resources/beta/Alpha.FCStd", BaselineSHA256: evidenceDigestZeta},
	}}
	writePersistedResult(t, attempt, []byte("mutated primary"))
	persisted := integrityEvidenceDigest([]byte("mutated primary"))
	evidence := &FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: []FreeCADIntegrityEvidenceResource{
		{
			LogicalID: "alpha", MaterializedCoordinate: "source/Alpha.FCStd", Status: FreeCADIntegrityStatusObserved,
			BaselineSHA256:  evidenceDigestAlpha,
			PersistedResult: &FreeCADIntegrityPersistedResult{Coordinate: "source/Alpha.FCStd", SHA256: persisted},
		},
		observedEvidence("beta", "resources/beta/Alpha.FCStd", evidenceDigestZeta),
	}}
	operation, err := evaluateIntegrity(t, attempt, request, evidence)
	if err != nil {
		t.Fatalf("EvaluateFreeCADRuntimeIntegrity returned error: %v", err)
	}
	if len(operation.Resources) != 2 {
		t.Fatalf("operation resources = %#v, want both resources", operation.Resources)
	}
	if operation.Resources[0].LogicalID != "alpha" || operation.Resources[1].LogicalID != "beta" {
		t.Fatalf("resource identities collided: %#v", operation.Resources)
	}
	if operation.Resources[0].MaterializedCoordinate == operation.Resources[1].MaterializedCoordinate {
		t.Fatalf("materialized coordinates collided: %#v", operation.Resources)
	}
}

func TestLoadFreeCADIntegrityEvidence(t *testing.T) {
	attempt := integrityEvidenceAttempt(t)
	workingCopy := attempt.Layout.WorkingCopyDir
	outputDir := attempt.Layout.OutputDir
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := FreeCADIntegrityEvidencePath(attempt.Layout)
	if path != filepath.Join(outputDir, FreeCADIntegrityEvidenceFilename) {
		t.Fatalf("evidence path = %q, want the output directory contract path", path)
	}

	evidence, content, err := LoadFreeCADIntegrityEvidence(workingCopy, path)
	if err != nil || evidence != nil || content != nil {
		t.Fatalf("absent evidence = %#v/%q/%v, want nil without failure", evidence, content, err)
	}

	raw := []byte(validEvidenceJSON(validEvidenceResources))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	evidence, content, err = LoadFreeCADIntegrityEvidence(workingCopy, path)
	if err != nil {
		t.Fatalf("LoadFreeCADIntegrityEvidence returned error: %v", err)
	}
	if evidence == nil || len(evidence.Resources) != 2 {
		t.Fatalf("loaded evidence = %#v", evidence)
	}
	if !bytes.Equal(content, raw) {
		t.Fatal("LoadFreeCADIntegrityEvidence did not preserve exact bytes")
	}
}
