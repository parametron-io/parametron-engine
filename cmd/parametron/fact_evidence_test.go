package main

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/scheduler"
)

// This is projection coverage for the fact sidecar, not proof of fact opt-in
// from project DSL. Accepted evidence is transported unchanged.
func TestCADRuntimeRunEvidenceFactCardinalityAndCopy(t *testing.T) {
	request := &recordcontract.FactRequest{
		SchemaVersion: recordcontract.FactRequestSchemaVersion,
		Bindings: []recordcontract.FactBinding{{
			Kind:        recordcontract.FactKindBoundProperty,
			Subject:     recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "p", Kind: "model", LogicalID: "assembly"}, NativeKind: "object", Name: "Body"},
			Context:     []recordcontract.OccurrenceContextFrame{},
			Property:    "Length",
			ValueKind:   recordcontract.FactValueKindNumber,
			Unit:        "mm",
			Requirement: recordcontract.FactRequirementOptional,
		}},
	}
	raw := []byte("{  \"controlled\": true }\n")

	for _, count := range []int{0, 1, 2} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			execution := scheduler.ExecutionResult{}
			for i := 0; i < count; i++ {
				outcome := cadRuntimeEvidenceOutcome(t, fmt.Sprintf("job-%d", i), fmt.Sprintf("widget-%d", i), "2", []byte("result"), nil, nil)
				outcome.FactRequest = recordcontract.CloneFactRequest(request)
				outcome.FactJSON = bytes.Clone(raw)
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
				if group.CADRuntime == nil || group.CADRuntime.Fact == nil || !bytes.Equal(group.CADRuntime.Fact.Input.Content, raw) || !reflect.DeepEqual(group.CADRuntime.Fact.Input.Request, request) || group.CADRuntime.Fact.Input.ExpectedSourceDocument != "source/assembly.FCStd" {
					t.Fatalf("fact projection lost: %+v", group)
				}
			}
			if count == 0 {
				return
			}
			got[0].CADRuntime.Fact.Input.Request.Bindings[0].Unit = "cm"
			got[0].CADRuntime.Fact.Input.Content[0] = '!'
			original := execution.Jobs[0].CADRuntimeOutcome
			if original.FactRequest.Bindings[0].Unit != "mm" || !bytes.Equal(original.FactJSON, raw) {
				t.Fatal("projection aliases outcome")
			}
		})
	}

	// Evidence without explicit intent is still transported so the Engine-owned
	// contract rejects it; projection must not silently drop it.
	outcome := cadRuntimeEvidenceOutcome(t, "job", "widget", "2", []byte("result"), nil, nil)
	outcome.FactJSON = bytes.Clone(raw)
	got, err := runOutcomeEvidences(scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}})
	if err != nil || len(got) != 1 || got[0].CADRuntime == nil || got[0].CADRuntime.Fact == nil || got[0].CADRuntime.Fact.Input.Request != nil || !bytes.Equal(got[0].CADRuntime.Fact.Input.Content, raw) {
		t.Fatalf("unsolicited evidence was not transported for rejection: %+v %v", got, err)
	}

	// No intent and no evidence requests nothing.
	outcome = cadRuntimeEvidenceOutcome(t, "job", "widget", "2", []byte("result"), nil, nil)
	got, err = runOutcomeEvidences(scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}})
	if err != nil || len(got) != 1 || got[0].CADRuntime == nil || got[0].CADRuntime.Fact != nil {
		t.Fatalf("absent fact intent produced evidence: %+v %v", got, err)
	}
}
