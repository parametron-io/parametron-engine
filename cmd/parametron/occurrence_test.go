package main

import (
	"bytes"
	"reflect"
	"testing"

	"parametron/internal/engine/executor"
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
				outcome := cadRuntimeEvidenceOutcome(t, "job", "widget", "2", []byte("result"), nil, nil)
				outcome.OccurrenceRequest = recordcontract.CloneOccurrenceRequest(request)
				outcome.OccurrenceJSON = bytes.Clone(raw)
				outcome.SourceDocument = "source/assembly.FCStd"
				execution.Jobs = append(execution.Jobs, scheduler.JobExecution{CADRuntimeOutcome: outcome})
			}
			got, err := cadRuntimeRunEvidence(execution, true)
			if err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				if got != nil {
					t.Fatal("projected other than exactly one eligible outcome")
				}
				return
			}
			if got == nil || got.Occurrence == nil || !bytes.Equal(got.Occurrence.Input.Content, raw) || !reflect.DeepEqual(got.Occurrence.Input.Request, request) || got.Occurrence.Input.ExpectedSourceDocument != "source/assembly.FCStd" {
				t.Fatalf("occurrence projection lost: %+v", got)
			}
			got.Occurrence.Input.Request.Requirement = "changed"
			got.Occurrence.Input.Content[0] = '!'
			original := execution.Jobs[0].CADRuntimeOutcome
			if original.OccurrenceRequest.Requirement != "optional" || !bytes.Equal(original.OccurrenceJSON, raw) {
				t.Fatal("projection aliases outcome")
			}
		})
	}
	// A failed overall execution does not expose otherwise eligible occurrence data.
	outcome := &executor.CADRuntimeOutcome{OccurrenceRequest: request, OccurrenceJSON: raw}
	got, err := cadRuntimeRunEvidence(scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}}, false)
	if err != nil || got != nil {
		t.Fatalf("failed execution projected: %+v %v", got, err)
	}
}
