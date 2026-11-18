package main

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/scheduler"
)

// This is projection coverage, not proof of occurrence opt-in from project DSL.
func TestCADRuntimeRunEvidenceOccurrenceCardinalityAndCopy(t *testing.T) {
	request := &recordcontract.OccurrenceRequest{SchemaVersion: "1.0", Requirement: "optional", Scope: recordcontract.OccurrenceScope{Parent: recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "p", Kind: "model", LogicalID: "assembly"}, NativeKind: "object", Name: "Assembly"}, Context: []recordcontract.OccurrenceContextFrame{}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism}}
	raw := []byte("{  \"controlled\": true }\n")
	// The outcome is already accepted at this boundary. Syntax and native evidence
	// are validated by intake tests; here exact transport must not reinterpret it.
	for _, count := range []int{0, 1, 2} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			execution := scheduler.ExecutionResult{}
			for i := 0; i < count; i++ {
				outcome := cadRuntimeEvidenceOutcome(t, fmt.Sprintf("job-%d", i), fmt.Sprintf("widget-%d", i), "2", []byte("result"), nil, nil)
				outcome.OccurrenceRequest = recordcontract.CloneOccurrenceRequest(request)
				outcome.OccurrenceJSON = bytes.Clone(raw)
				outcome.SourceDocument = "source/assembly.FCStd"
				execution.Jobs = append(execution.Jobs, scheduler.JobExecution{CADRuntimeOutcome: outcome})
			}
			got, err := runOutcomeEvidences(execution)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != count {
				t.Fatalf("outcome groups = %d, want %d", len(got), count)
			}
			for _, group := range got {
				if group.CADRuntime == nil || group.CADRuntime.Occurrence == nil || !bytes.Equal(group.CADRuntime.Occurrence.Input.Content, raw) || !reflect.DeepEqual(group.CADRuntime.Occurrence.Input.Request, request) || group.CADRuntime.Occurrence.Input.ExpectedSourceDocument != "source/assembly.FCStd" {
					t.Fatalf("occurrence projection lost: %+v", group)
				}
			}
			if count == 0 {
				return
			}
			got[0].CADRuntime.Occurrence.Input.Request.Requirement = "changed"
			got[0].CADRuntime.Occurrence.Input.Content[0] = '!'
			original := execution.Jobs[0].CADRuntimeOutcome
			if original.OccurrenceRequest.Requirement != "optional" || !bytes.Equal(original.OccurrenceJSON, raw) {
				t.Fatal("projection aliases outcome")
			}
		})
	}
}
