package jobstatus

import (
	"encoding/json"
	"strings"
	"testing"

	"parametron/internal/engine/executor"
)

func TestFailure_AlignedFieldsOmitWhenEmpty(t *testing.T) {
	data, err := json.Marshal(Failure{Message: "legacy", ProductID: "widget"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"classification", "boundary", "category", "code", "stage", "attemptId"} {
		if strings.Contains(string(data), `"`+field+`"`) {
			t.Fatalf("%s", data)
		}
	}
}

func TestFailure_AlignedFieldsSerializeExactly(t *testing.T) {
	failure := FailureFromError("widget", &executor.ExecutionError{
		ProductID: "widget", StepID: "2", RetryCount: 1,
		Err: &executor.CADRuntimeConsumptionError{
			Stage: executor.CADRuntimeConsumptionStageRuntimeFailure, AttemptID: "attempt-12",
			Classification: "export_failed", Boundary: "freecad", Category: "runtime",
			Code: "export_failed", NativeStage: "export", Message: "safe failure",
		},
	})
	data, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"message":"safe failure","productId":"widget","stepId":"2","retryCount":1,"timeout":false,"canceled":false,"classification":"export_failed","boundary":"freecad","category":"runtime","code":"export_failed","stage":"export","attemptId":"attempt-12"}`
	if string(data) != want {
		t.Fatalf("\n got %s\nwant %s", data, want)
	}
}

func TestFailure_AlignedFieldsPreserveExistingJSON(t *testing.T) {
	legacy := Failure{Message: "failed", ProductID: "widget", StepID: "2", RetryCount: 3, Timeout: true}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"message":"failed","productId":"widget","stepId":"2","retryCount":3,"timeout":true,"canceled":false}`
	if string(data) != want {
		t.Fatalf("\n got %s\nwant %s", data, want)
	}
}
