package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/resourcebinding"
	"parametron/internal/engine/runtimecap"
)

// Explicit intent must survive the executor's per-attempt request boundary;
// otherwise neither runtime sidecar materialization nor outcome retention can
// enforce the caller's required-complete contract.
func TestCADRuntimeAttemptRequestPreservesOccurrenceIntent(t *testing.T) {
	for _, requirement := range []string{"optional", "complete"} {
		t.Run(requirement, func(t *testing.T) {
			request := &recordcontract.OccurrenceRequest{SchemaVersion: "1.0", Requirement: requirement, Scope: recordcontract.OccurrenceScope{Parent: recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "p", Kind: "model", LogicalID: "assembly"}, NativeKind: "object", Name: "Assembly"}, Context: []recordcontract.OccurrenceContextFrame{}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism}}
			dispatch := &cadRuntimeDispatch{base: adapter.CADRuntimeOrchestrationRequest{OccurrenceObservation: request}}
			e := New(&legacyOnlyAdapter{})
			got := e.buildCADRuntimeOrchestrationRequest(dispatch, 2)
			if !reflect.DeepEqual(got.OccurrenceObservation, request) {
				t.Fatalf("attempt dropped explicit %s occurrence intent: got %#v", requirement, got.OccurrenceObservation)
			}
			if got.OccurrenceObservation == request {
				t.Fatal("attempt aliases caller-owned occurrence request")
			}
		})
	}
}

type occurrenceIntentAdapter struct {
	task12Adapter
	beforeRun func()
}

func (a *occurrenceIntentAdapter) Run(context.Context, planner.Step) error {
	if a.beforeRun != nil {
		a.beforeRun()
		a.beforeRun = nil
	}
	return nil
}

func TestExecutePackageWithOptionsOwnsOccurrenceIntentAcrossRetriesAndExecutions(t *testing.T) {
	for _, requirement := range []string{"optional", "complete"} {
		t.Run(requirement, func(t *testing.T) {
			root := t.TempDir()
			inventory, err := resourcebinding.New([]resourcebinding.Binding{{ProjectID: "p", Kind: "model", LogicalID: "assembly", SourceCoordinate: "assembly.FCStd"}})
			if err != nil {
				t.Fatal(err)
			}
			resources, err := resourcebinding.Select(inventory, "p", "assembly", root)
			if err != nil {
				t.Fatal(err)
			}
			pkg := alignedPackage(t, "freecad")
			pkg.Manifest.Inputs.SourceModel = filepath.Join(root, "assembly.FCStd")
			for i := range pkg.Steps {
				if pkg.Steps[i].Manifest != nil {
					pkg.Steps[i].Manifest.Inputs = pkg.Manifest.Inputs
				}
			}
			parent := recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "p", Kind: "model", LogicalID: "assembly"}, NativeKind: "object", Name: "Assembly"}
			outer := parent
			outer.Name = "Outer"
			request := &recordcontract.OccurrenceRequest{SchemaVersion: "1.0", Requirement: requirement, Scope: recordcontract.OccurrenceScope{Parent: parent, Context: []recordcontract.OccurrenceContextFrame{{Parent: outer, Member: "Instance"}}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism}}
			want := recordcontract.CloneOccurrenceRequest(request)
			a := &occurrenceIntentAdapter{}
			// Mutate caller input during the first non-runtime step, after entry
			// accepted it but before the runtime adapter receives its request.
			a.beforeRun = func() {
				request.Requirement = "changed"
				request.Scope.Parent.Name = "Changed"
				request.Scope.Context[0].Member = "Changed"
			}
			calls := 0
			a.onCall = func(q adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
				calls++
				if q.Attempt != calls || !reflect.DeepEqual(q.OccurrenceObservation, want) {
					t.Fatalf("attempt %d lost accepted intent: %+v", calls, q.OccurrenceObservation)
				}
				q.OccurrenceObservation.Scope.Parent.Name = "AttemptMutation"
				q.OccurrenceObservation.Scope.Context[0].Parent.Name = "AttemptMutation"
				if calls < 3 {
					return cadruntime.FreeCADRuntimeVerifiedRun{}, false, errors.New("controlled retry")
				}
				return cadruntime.FreeCADRuntimeVerifiedRun{}, false, nil
			}
			e, _ := newCADExecutor(t, a, &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}})
			e.SetResourceContext(resources)
			if err := e.ExecutePackageWithOptions(context.Background(), pkg, PackageExecutionOptions{OccurrenceObservation: request}); err != nil {
				t.Fatal(err)
			}
			if calls != 3 {
				t.Fatalf("attempts = %d, want 3", calls)
			}
			a.onCall = func(q adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
				if q.OccurrenceObservation != nil {
					t.Fatal("occurrence intent leaked into default execution")
				}
				return cadruntime.FreeCADRuntimeVerifiedRun{}, false, nil
			}
			if err := e.ExecutePackage(context.Background(), pkg); err != nil {
				t.Fatal(err)
			}
			if err := e.ExecutePackageWithOptions(context.Background(), pkg, PackageExecutionOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := e.ExecutePackageWithOptions(context.Background(), nil, PackageExecutionOptions{OccurrenceObservation: want}); err == nil {
				t.Fatal("nil package accepted")
			}
			if err := e.ExecutePackage(context.Background(), pkg); err != nil {
				t.Fatal(err)
			}
			noIntent := a.onCall
			a.onCall = func(q adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
				if !reflect.DeepEqual(q.OccurrenceObservation, want) {
					t.Fatal("failed execution lost intent")
				}
				return cadruntime.FreeCADRuntimeVerifiedRun{}, false, errors.New("controlled terminal failure")
			}
			if err := e.ExecutePackageWithOptions(context.Background(), pkg, PackageExecutionOptions{OccurrenceObservation: want}); err == nil {
				t.Fatal("runtime failure accepted")
			}
			a.onCall = noIntent
			if err := e.ExecutePackage(context.Background(), pkg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOccurrenceOutcomeRetentionAndRequiredFailure(t *testing.T) {
	for _, mode := range []string{"complete", "optional omitted", "required omitted", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			var raw []byte
			e, err := consumeTask12(t, func(req *adapter.CADRuntimeOrchestrationRequest, run *cadruntime.FreeCADRuntimeVerifiedRun) {
				parent := recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "p", Kind: "model", LogicalID: "assembly"}, NativeKind: "object", Name: "Assembly"}
				req.OccurrenceObservation = &recordcontract.OccurrenceRequest{SchemaVersion: "1.0", Requirement: "optional", Scope: recordcontract.OccurrenceScope{Parent: parent, Context: []recordcontract.OccurrenceContextFrame{}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism}}
				if mode == "required omitted" {
					req.OccurrenceObservation.Requirement = "complete"
				}
				attempt := &run.Runtime.ObservationRequest.Manifest.Attempt
				var err error
				attempt.Resources, err = resourcebinding.New([]resourcebinding.Binding{{ProjectID: "p", Kind: "model", LogicalID: "assembly", SourceCoordinate: "assembly.FCStd", MaterializedCoordinate: "source/assembly.FCStd"}})
				if err != nil {
					t.Fatal(err)
				}
				attempt.Layout.SourceDocument = "source/assembly.FCStd"
				if err := os.MkdirAll(attempt.Layout.OutputDir, 0o700); err != nil {
					t.Fatal(err)
				}
				evidence := cadruntime.OccurrenceRuntimeEvidence{SchemaVersion: "1.0", Kind: "native_occurrence_evidence", FreeCADVersion: "1.1.4", Scope: req.OccurrenceObservation.Scope, Parent: &cadruntime.OccurrenceNativeEntity{Entity: parent, Document: "source/assembly.FCStd", NativeType: "Assembly::AssemblyObject"}, Completeness: "complete", InspectionCompleted: true, Group: []string{}, Members: []cadruntime.OccurrenceRuntimeMember{}, Diagnostics: []string{}}
				raw, err = json.MarshalIndent(evidence, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if mode == "malformed" {
					raw = []byte(`{`)
				}
				if mode == "complete" || mode == "malformed" {
					if err := os.WriteFile(filepath.Join(attempt.Layout.OutputDir, cadruntime.OccurrenceEvidenceFilename), raw, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}, nil)
			outcome := e.CADRuntimeOutcome()
			if mode == "required omitted" || mode == "malformed" {
				sentinel := recordcontract.ErrInvalidOccurrence
				if mode == "required omitted" {
					sentinel = recordcontract.ErrOccurrenceCompletenessRequired
				}
				if !errors.Is(err, sentinel) || outcome.Verification == artifact.VerificationOutcomePassed {
					t.Fatalf("failure lost: %v %+v", err, outcome)
				}
				return
			}
			if err != nil || outcome == nil || outcome.OccurrenceRequest == nil || outcome.Verification != artifact.VerificationOutcomePassed {
				t.Fatalf("outcome: %+v %v", outcome, err)
			}
			if mode == "complete" && !bytes.Equal(outcome.OccurrenceJSON, raw) {
				t.Fatal("raw bytes changed")
			}
			if mode == "optional omitted" && len(outcome.OccurrenceJSON) != 0 {
				t.Fatal("fabricated evidence")
			}
			outcome.OccurrenceRequest.Requirement = "mutated"
			if len(outcome.OccurrenceJSON) > 0 {
				outcome.OccurrenceJSON[0] = '!'
			}
			again := e.CADRuntimeOutcome()
			if again.OccurrenceRequest.Requirement != "optional" || (mode == "complete" && !bytes.Equal(again.OccurrenceJSON, raw)) {
				t.Fatal("outcome aliases caller storage")
			}
		})
	}
}
