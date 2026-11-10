package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
	"parametron/internal/engine/resourcebinding"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/scheduler"
	"parametron/internal/engine/verification"
)

// The existing external participant supplies ordinary CAD outputs and generic
// references. This controlled capability adds only explicit occurrence evidence;
// production code still materializes requests, verifies, retains and emits it.
type occurrenceExecutionCapability struct {
	t       *testing.T
	want    *recordcontract.OccurrenceRequest
	mode    string
	reverse bool
	calls   int
	raw     []byte
}

func (c *occurrenceExecutionCapability) Invoke(ctx context.Context, req runtimecap.Request) (*runtimecap.Result, error) {
	c.calls++
	content, err := os.ReadFile(filepath.Join(req.WorkingCopyDir, cadruntime.OccurrenceRequestFilename))
	if c.want == nil {
		if !os.IsNotExist(err) {
			c.t.Fatalf("unexpected request sidecar: %v", err)
		}
	} else {
		if err != nil {
			c.t.Fatal(err)
		}
		got, err := cadruntime.DecodeOccurrenceRequest(content)
		if err != nil || !reflect.DeepEqual(got, *c.want) {
			c.t.Fatalf("materialized intent changed: %+v %v", got, err)
		}
	}
	result, err := runtimecap.NewExternalCapability().Invoke(ctx, req)
	if err != nil || c.want == nil || c.mode == "absent" {
		return result, err
	}
	e := cadruntime.OccurrenceRuntimeEvidence{SchemaVersion: "1.1", Kind: "native_occurrence_evidence", FreeCADVersion: "1.1.4", Scope: c.want.Scope, Parent: &cadruntime.OccurrenceNativeEntity{Entity: c.want.Scope.Parent, Document: "source/box.FCStd", NativeType: "Assembly::AssemblyObject"}, Completeness: "complete", InspectionCompleted: true, Group: []string{"Second", "Third", "First"}, Members: []cadruntime.OccurrenceRuntimeMember{}, Diagnostics: []string{}}
	for _, name := range e.Group {
		zero, valid := 0, true
		target := c.want.Scope.Parent
		target.Name = "Body"
		if name == "Third" {
			target.Name = "Other"
		}
		e.Members = append(e.Members, cadruntime.OccurrenceRuntimeMember{Name: name, Parent: c.want.Scope.Parent, NativeType: "App::Link", ElementCount: &zero, MembershipCorroborated: true, Classification: "occurrence", Resolution: "resolved", NativeStateValid: &valid, Target: &cadruntime.OccurrenceNativeEntity{Entity: target, Document: "source/box.FCStd", NativeType: "PartDesign::Body"}})
	}
	if c.mode == "incomplete" || c.mode == "unsupported" {
		e.Completeness = c.mode
		e.Diagnostics = []string{"controlled incomplete coverage"}
	}
	if c.mode == "wrong resource" {
		e.Members[0].Target.Entity.Resource.LogicalID = "undeclared"
	}
	if c.reverse {
		slices.Reverse(e.Group)
		slices.Reverse(e.Members)
	}
	c.raw, err = json.MarshalIndent(e, "", "  ")
	if err != nil {
		c.t.Fatal(err)
	}
	c.raw = append(c.raw, '\n')
	if c.mode == "malformed" {
		c.raw = []byte(`{"schemaVersion":`)
	}
	path := filepath.Join(req.OutputDir, cadruntime.OccurrenceEvidenceFilename)
	if c.mode == "alternate path" {
		path = filepath.Join(req.WorkingCopyDir, cadruntime.OccurrenceEvidenceFilename)
	}
	if c.mode == "symlink escape" {
		outside := filepath.Join(c.t.TempDir(), "evidence.json")
		if err := os.WriteFile(outside, c.raw, 0600); err != nil {
			c.t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			c.t.Fatal(err)
		}
	} else if err := os.WriteFile(path, c.raw, 0600); err != nil {
		c.t.Fatal(err)
	}
	return result, nil
}

type occurrenceExecutionFixture struct {
	t         *testing.T
	e         *executor.Executor
	pkg       *handoff.Package
	job       *job.Job
	planned   *plannedRun
	root      string
	cap       *occurrenceExecutionCapability
	request   *recordcontract.OccurrenceRequest
	traversal []byte
}

func newOccurrenceExecutionFixture(t *testing.T) *occurrenceExecutionFixture {
	t.Helper()
	contractPackageEnv(t)
	command := installControlledAlignedRuntime(t, "success")
	resetGlobals()
	planned, err := loadPlannedRun(writeContractPackageFixture(t, nil), map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := job.SplitPlan(planned.Plan)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs: %v %v", jobs, err)
	}
	pkg, err := handoff.FromJob(jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	resources := planned.ResourceContexts[jobs[0].ProductKey]
	selected := resources.Selected()
	parent := recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: selected.ProjectID, Kind: selected.Kind, LogicalID: selected.LogicalID}, NativeKind: "object", Name: "Assembly"}
	request := &recordcontract.OccurrenceRequest{SchemaVersion: "1.0", Requirement: "optional", Scope: recordcontract.OccurrenceScope{Parent: parent, Context: []recordcontract.OccurrenceContextFrame{}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism}}
	cap := &occurrenceExecutionCapability{t: t, want: recordcontract.CloneOccurrenceRequest(request), mode: "complete"}
	root := t.TempDir()
	productDir := filepath.Join(root, "products", jobs[0].ProductKey)
	if err := os.MkdirAll(productDir, 0700); err != nil {
		t.Fatal(err)
	}
	aligned, err := cadruntime.NewFreeCADRuntimeAdapter(freecad.NewFreeCADAdapter(productDir), cap)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := runtimecap.NewExecutableResolver(map[string]string{"freecad": command})
	if err != nil {
		t.Fatal(err)
	}
	e := executor.NewWithCADRuntimeConsumption(aligned, resolver, productDir)
	e.SetResourceContext(resources)
	property, mechanism := "Link", "App::PropertyXLink"
	traversal := recordmap.ReferenceTraversalRuntimeEvidence{SchemaVersion: "1.0", Kind: "raw_reference_traversal", Boundary: "reference_traversal_entrypoint", Operation: "reference_traversal", Status: "succeeded", SourceDocument: "source/box.FCStd", Nodes: []recordmap.ReferenceTraversalRuntimeNode{{ID: "doc", Kind: "document", State: "resolved", DocumentPath: "source/box.FCStd"}, {ID: "from", Kind: "object", State: "resolved", DocumentPath: "source/box.FCStd", ObjectName: stringPointer("Assembly")}, {ID: "to", Kind: "object", State: "resolved", DocumentPath: "source/box.FCStd", ObjectName: stringPointer("Body")}}, Edges: []recordmap.ReferenceTraversalRuntimeEdge{{Source: "from", Target: "to", Kind: "document_internal_reference", State: "resolved", SourceProperty: &property, ReferenceMechanism: &mechanism}}, Diagnostics: []recordmap.ReferenceTraversalRuntimeDiagnostic{}}
	raw, err := json.Marshal(traversal)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", string(raw))
	return &occurrenceExecutionFixture{t: t, e: e, pkg: pkg, job: jobs[0], planned: planned, root: root, cap: cap, request: request, traversal: raw}
}

func (f *occurrenceExecutionFixture) execute() (scheduler.ExecutionResult, error) {
	f.t.Helper()
	f.cap.want = recordcontract.CloneOccurrenceRequest(f.request)
	err := f.e.ExecutePackageWithOptions(context.Background(), f.pkg, executor.PackageExecutionOptions{OccurrenceObservation: f.request})
	return scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{Job: f.job, States: f.e.States(), Err: err, CADRuntimeOutcome: f.e.CADRuntimeOutcome()}}}, err
}

func (f *occurrenceExecutionFixture) emit(execution scheduler.ExecutionResult) map[string][]byte {
	f.t.Helper()
	cad, err := cadRuntimeRunEvidence(execution, true)
	if err != nil || cad == nil {
		f.t.Fatalf("eligible projection: %+v %v", cad, err)
	}
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rep, err := report.Build(report.BuildInput{PlanHash: f.planned.PlanHash, DSLHash: f.planned.DSLHash, Execution: execution, StartedAt: stamp, EndedAt: stamp, WorkerCount: 1})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{RunRoot: f.root, PlanHash: f.planned.PlanHash, Report: rep, CADRuntime: cad, ReferenceTraversal: referenceTraversalRunEvidence(execution, true)}); err != nil {
		f.t.Fatal(err)
	}
	return readRecordPackageFiles(f.t, recordPackageRoot(f.root))
}

func TestOccurrenceExecutionCompletePackageAndProvenance(t *testing.T) {
	f := newOccurrenceExecutionFixture(t)
	var previous *recordcontract.ObservationRecord
	var previousRaw []byte
	for iteration := 0; iteration < 3; iteration++ {
		f.cap.reverse = iteration == 1
		execution, err := f.execute()
		if err != nil {
			t.Fatal(err)
		}
		outcome := singleCADRuntimeOutcome(t, execution)
		if outcome.Verification != artifact.VerificationOutcomePassed || outcome.VerificationResult.Status != verification.StatusPass || !reflect.DeepEqual(outcome.OccurrenceRequest, f.request) || !bytes.Equal(outcome.OccurrenceJSON, f.cap.raw) {
			t.Fatal("verified outcome lost request or exact evidence")
		}
		files := f.emit(execution)
		path := "raw/runtime/prm.occurrence-evidence.json"
		entry, ok := recordpackage.EntryForContractPath(path)
		if !ok || entry.Role != recordpackage.EntryRoleRawEvidence || !bytes.Equal(files[path], f.cap.raw) {
			t.Fatal("raw occurrence classification or bytes lost")
		}
		var observation recordcontract.ObservationRecord
		decodePackageRecord(t, files, recordpackage.MustRecordContractPath("observation"), &observation)
		if err := recordcontract.ValidateObservationRecord(observation); err != nil {
			t.Fatal(err)
		}
		s := observation.Observation.Occurrences
		if s == nil || s.Completeness != "complete" || !reflect.DeepEqual(s.Scope, f.request.Scope) || len(s.Occurrences) != 3 || len(s.Quantities) != 2 || s.Quantities[0].Target.Name != "Body" || s.Quantities[0].Quantity != 2 || s.Quantities[1].Target.Name != "Other" || s.Quantities[1].Quantity != 1 {
			t.Fatalf("incorrect normalized occurrence output: %+v", s)
		}
		if s.Occurrences[0].Member != "First" || s.Occurrences[1].Member != "Second" || s.Occurrences[2].Member != "Third" || s.Occurrences[0].Target.Resource != f.request.Scope.Parent.Resource {
			t.Fatal("native member ordering or resource identity lost")
		}
		if s.Evidence.DigestSHA256 != sha256HexOf(f.cap.raw) {
			t.Fatal("raw digest changed")
		}
		found := false
		for _, ref := range observation.Provenance.Evidence {
			if ref.Ref == path && ref.DigestSHA256 == sha256HexOf(f.cap.raw) {
				found = true
			}
		}
		if !found {
			t.Fatal("occurrence provenance missing")
		}
		var reference recordcontract.ReferenceRecord
		decodePackageRecord(t, files, recordpackage.MustRecordContractPath("reference"), &reference)
		if err := recordcontract.ValidateReferenceRecord(reference); err != nil {
			t.Fatal(err)
		}
		if len(reference.Reference.Edges) != 1 || reference.Reference.Edges[0].ReferenceMechanism == nil || *reference.Reference.Edges[0].ReferenceMechanism != "App::PropertyXLink" || !bytes.Equal(files[recordpackage.RawRuntimeReferenceTraversalContractPath()], f.traversal) {
			t.Fatal("generic references changed or became occurrence counts")
		}
		if previous != nil {
			if !reflect.DeepEqual(previous.Observation.Occurrences.Occurrences, s.Occurrences) || !reflect.DeepEqual(previous.Observation.Occurrences.Quantities, s.Quantities) {
				t.Fatal("evidence ordering changed normalized semantics")
			}
			if bytes.Equal(previousRaw, f.cap.raw) || previous.Identity == observation.Identity {
				t.Fatal("raw provenance stopped influencing containing identity")
			}
		}
		previous = &observation
		previousRaw = bytes.Clone(f.cap.raw)
	}
}

func TestOccurrenceExecutionEvidenceStatesAndFailures(t *testing.T) {
	for _, requirement := range []string{"optional", "complete"} {
		for _, mode := range []string{"complete", "absent", "incomplete", "unsupported", "malformed", "wrong resource", "alternate path", "symlink escape"} {
			t.Run(requirement+"/"+mode, func(t *testing.T) {
				f := newOccurrenceExecutionFixture(t)
				f.request.Requirement = requirement
				f.cap.mode = mode
				execution, err := f.execute()
				requiredFailure := requirement == "complete" && (mode == "absent" || mode == "incomplete" || mode == "unsupported" || mode == "alternate path")
				invalid := mode == "malformed" || mode == "wrong resource" || mode == "symlink escape"
				if requiredFailure || invalid {
					if err == nil {
						t.Fatal("invalid evidence accepted")
					}
					if requiredFailure && !errors.Is(err, recordcontract.ErrOccurrenceCompletenessRequired) {
						t.Fatalf("completeness sentinel lost: %v", err)
					}
					if mode == "malformed" && !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
						t.Fatalf("malformed evidence classification: %v", err)
					}
					if mode == "wrong resource" && !errors.Is(err, recordcontract.ErrInvalidOccurrence) && !errors.Is(err, resourcebinding.ErrInvalid) {
						t.Fatalf("resource failure classification: %v", err)
					}
					var classified *cadruntime.FreeCADRuntimeVerificationError
					if !errors.As(err, &classified) || classified.Stage != "occurrence_validation" {
						t.Fatalf("wrong rejection boundary: %v", err)
					}
					for _, overallSucceeded := range []bool{false, true} {
						if got, projectionErr := cadRuntimeRunEvidence(execution, overallSucceeded); projectionErr != nil || got != nil {
							t.Fatal("failed evidence projected")
						}
					}
					if out := f.e.CADRuntimeOutcome(); out != nil && out.Verification == artifact.VerificationOutcomePassed {
						t.Fatal("rejected occurrence evidence marked eligible")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				files := f.emit(execution)
				var observation recordcontract.ObservationRecord
				decodePackageRecord(t, files, recordpackage.MustRecordContractPath("observation"), &observation)
				s := observation.Observation.Occurrences
				want := mode
				if mode == "absent" || mode == "alternate path" {
					want = "unavailable"
				}
				if s == nil || s.Completeness != want || (s.Quantities != nil) != (mode == "complete") {
					t.Fatalf("noncomplete evidence fabricated quantity: %+v", s)
				}
			})
		}
	}
	t.Run("no intent", func(t *testing.T) {
		f := newOccurrenceExecutionFixture(t)
		f.request = nil
		f.cap.mode = "absent"
		execution, err := f.execute()
		if err != nil {
			t.Fatal(err)
		}
		files := f.emit(execution)
		var observation recordcontract.ObservationRecord
		decodePackageRecord(t, files, recordpackage.MustRecordContractPath("observation"), &observation)
		if observation.Observation.Occurrences != nil || len(files[recordpackage.RawRuntimeOccurrenceEvidenceContractPath()]) != 0 {
			t.Fatal("ordinary execution synthesized occurrence evidence")
		}
	})
}
