package report

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/verification"
)

func task12ReportError(class, boundary, category, code, stage string) error {
	return &executor.ExecutionError{
		ProductID: "widget", StepID: "2", RetryCount: 1,
		Err: &executor.CADRuntimeConsumptionError{
			Stage: executor.CADRuntimeConsumptionStageRuntimeFailure, AttemptID: "attempt-12",
			Classification: class, Boundary: boundary, Category: category, Code: code,
			NativeStage: stage, Message: "safe failure", Err: errors.New("cause"),
		},
	}
}

func assertTask12ReportFailure(t *testing.T, class, boundary, category, code, stage string) {
	t.Helper()
	got := buildErrorSummary(task12ReportError(class, boundary, category, code, stage))
	if got == nil || string(got.Classification) != class || got.Boundary != boundary || got.Category != category ||
		got.Code != code || got.Stage != stage || got.AttemptID != "attempt-12" || got.ProductID != "widget" || got.StepID != "2" {
		t.Fatalf("%#v", got)
	}
}

func TestReport_AlignedVerificationFailure(t *testing.T) {
	assertTask12ReportFailure(t, string(verification.FailureClassParameterMismatch), "engine", "verification", string(verification.FailureClassParameterMismatch), "verification_comparison")
}
func TestReport_AlignedVerificationResultFailure(t *testing.T) {
	assertTask12ReportFailure(t, string(verification.FailureClassInternalError), "engine", "verification", string(verification.FailureClassInternalError), "verification_result")
}
func TestReport_AlignedRuntimeNativeFailure(t *testing.T) {
	assertTask12ReportFailure(t, "export_failed", "freecad", "runtime", "export_failed", "export")
}
func TestReport_AlignedRuntimeInfrastructureFailure(t *testing.T) {
	assertTask12ReportFailure(t, "result_load", "engine", "runtime", "result_load", "result_load")
}
func TestReport_LegacyVerificationFailureRemainsCompatible(t *testing.T) {
	err := &executor.ExecutionError{ProductID: "widget", StepID: "2", Err: &verification.VerifyError{Class: verification.FailureClassMetadataMismatch, Message: "metadata"}}
	got := buildErrorSummary(err)
	if got.Classification != verification.FailureClassMetadataMismatch || got.Boundary != "" {
		t.Fatal(got)
	}
}
func TestReport_AlignedSuccessIncludesRegisteredArtifacts(t *testing.T) {
	items := []artifact.Artifact{{ProductID: "widget", StepID: "2", Type: artifact.ArtifactTypeSTEP, Filename: "outputs/widget.step", Path: "/run/widget.step"}}
	got := buildArtifacts(items)
	if len(got) != 1 || got[0].Filename != items[0].Filename {
		t.Fatal(got)
	}
}
func TestReport_AlignedFailureIncludesNoAcceptedArtifacts(t *testing.T) {
	if got := buildArtifacts(nil); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestReport_AlignedFailureFieldsAreDeterministic(t *testing.T) {
	a := buildErrorSummary(task12ReportError("export_failed", "freecad", "runtime", "export_failed", "export"))
	b := buildErrorSummary(task12ReportError("export_failed", "freecad", "runtime", "export_failed", "export"))
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if !reflect.DeepEqual(aj, bj) {
		t.Fatalf("%s != %s", aj, bj)
	}
}
