package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/scheduler"
	"parametron/internal/engine/verification"
)

// This rehearsal is the #45 closure proof. It drives the real Engine
// executor/cadruntime/verification/recordemit path with a controlled external
// runtime participant that returns only raw adapter evidence. Engine remains
// responsible for declared-resource joins, reference/occurrence/fact
// normalization, integrity validation, and outcome-scoped package emission.

type coordinatedClosureFixture struct {
	planned *plannedRun
	jobs    []*job.Job
	root    string
	command string

	occurrenceRequest *recordcontract.OccurrenceRequest
	factRequest       *recordcontract.FactRequest
	traversal         []byte
}

type coordinatedClosureCapability struct {
	t *testing.T

	occurrenceRequest *recordcontract.OccurrenceRequest
	factRequest       *recordcontract.FactRequest

	mode              string
	occurrenceRaw     []byte
	factRaw           []byte
	integrityRaw      []byte
	baselineDigests   map[string]string
	resultDigests     map[string]string
	resourceCoords    map[string]string
	primaryCoordinate string
}

func coordinatedEntity(resource, kind, name string) recordcontract.OccurrenceEntity {
	return recordcontract.OccurrenceEntity{
		Resource:   recordcontract.ReferenceResource{ProjectID: "coordinated-closure", Kind: "model", LogicalID: resource},
		NativeKind: kind,
		Name:       name,
	}
}

func writeCoordinatedClosureFixture(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	for _, dir := range []string{"models", "input", "standalone"} {
		if err := os.MkdirAll(filepath.Join(projectDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []struct {
		path    string
		content string
	}{
		{"models/box.FCStd", "coordinated-primary-source"},
		{"input/box.FCStd", "coordinated-alias-source"},
		{"standalone/box.FCStd", "coordinated-standalone-source"},
	} {
		if err := os.WriteFile(filepath.Join(projectDir, filepath.FromSlash(file.path)), []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.dsl"), []byte(withVersionHeader(`
const PRIMARY_MODEL = "box_model"

product Widget {
    adapter = "freecad"
    source_model = PRIMARY_MODEL
    outputs = ["step"]
}

product Gadget {
    adapter = "freecad"
    source_model = PRIMARY_MODEL
    outputs = ["step"]
}
`)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "prm.project.json"), []byte(`{
  "version": "1.0",
  "projectId": "coordinated-closure",
  "dsl": "./project.dsl",
  "resources": {
    "models": {
      "box_model": "./models/box.FCStd",
      "alias": "./input/box.FCStd",
      "unused": "./standalone/box.FCStd"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

func newCoordinatedClosureFixture(t *testing.T) *coordinatedClosureFixture {
	t.Helper()
	contractPackageEnv(t)
	t.Setenv("PARAMETRON_TASK13_RUNTIME_MODE", "success")
	command := installControlledAlignedRuntime(t, "success")

	projectDir := writeCoordinatedClosureFixture(t)
	if _, err := executeHarnessCommand(t, []string{"sync", "--file", projectDir}); err != nil {
		t.Fatalf("sync project lock: %v", err)
	}
	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := job.SplitPlan(planned.Plan)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %#v, err = %v; want two products", jobs, err)
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].ProductKey < jobs[j].ProductKey })

	parent := coordinatedEntity("box_model", "object", "Assembly")
	occurrenceRequest := &recordcontract.OccurrenceRequest{
		SchemaVersion: recordcontract.OccurrenceSchemaVersion, Requirement: "optional",
		Scope: recordcontract.OccurrenceScope{Parent: parent, Context: []recordcontract.OccurrenceContextFrame{}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism},
	}
	context := []recordcontract.OccurrenceContextFrame{{Parent: parent, Member: "Link1"}}
	factRequest := &recordcontract.FactRequest{
		SchemaVersion: recordcontract.FactRequestSchemaVersion,
		Bindings: []recordcontract.FactBinding{
			{Kind: recordcontract.FactKindBoundProperty, Subject: coordinatedEntity("box_model", "object", "Body"), Context: []recordcontract.OccurrenceContextFrame{}, Property: "Material", ValueKind: recordcontract.FactValueKindString, Requirement: recordcontract.FactRequirementOptional},
			{Kind: recordcontract.FactKindBoundProperty, Subject: coordinatedEntity("alias", "object", "Body"), Context: context, Property: "Length", ValueKind: recordcontract.FactValueKindNumber, Unit: "mm", Requirement: recordcontract.FactRequirementOptional},
			{Kind: recordcontract.FactKindBoundProperty, Subject: coordinatedEntity("unused", "object", "Body"), Context: []recordcontract.OccurrenceContextFrame{}, Property: "Density", ValueKind: recordcontract.FactValueKindNumber, Requirement: recordcontract.FactRequirementOptional},
		},
	}

	property, mechanism := "Link", "App::PropertyXLink"
	traversal := recordmapTraversal("source/box.FCStd", fmt.Sprintf("resources/%x/input/box.FCStd", sha256.Sum256([]byte("alias"))), property, mechanism)
	raw, err := json.MarshalIndent(traversal, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", string(raw))

	return &coordinatedClosureFixture{
		planned: planned, jobs: jobs, root: t.TempDir(), command: command,
		occurrenceRequest: occurrenceRequest, factRequest: factRequest, traversal: raw,
	}
}

func recordmapTraversal(sourceDocument, aliasCoordinate, property, mechanism string) map[string]any {
	return map[string]any{
		"schemaVersion": "1.0", "kind": "raw_reference_traversal", "boundary": "reference_traversal_entrypoint", "operation": "reference_traversal", "status": "succeeded", "sourceDocument": sourceDocument,
		"nodes": []any{
			map[string]any{"sequence": 0, "id": "doc", "kind": "document", "state": "resolved", "documentPath": sourceDocument},
			map[string]any{"sequence": 1, "id": "source", "kind": "object", "state": "resolved", "documentPath": sourceDocument, "objectName": "Assembly"},
			map[string]any{"sequence": 2, "id": "alias", "kind": "object", "state": "resolved", "documentPath": aliasCoordinate, "objectName": "Part"},
			map[string]any{"sequence": 3, "id": "unknown", "kind": "external_document", "state": "resolved", "documentPath": "external/unknown.FCStd"},
			map[string]any{"sequence": 4, "id": "file", "kind": "external_file", "state": "resolved", "documentPath": "external/unknown.step"},
		},
		"edges": []any{
			map[string]any{"sequence": 0, "source": "source", "target": "alias", "kind": "external_document_reference", "state": "resolved", "sourceProperty": property, "referenceMechanism": mechanism},
			map[string]any{"sequence": 1, "source": "source", "target": "unknown", "kind": "external_document_reference", "state": "resolved", "sourceProperty": "Support", "referenceMechanism": mechanism},
			map[string]any{"sequence": 2, "source": "source", "target": "file", "kind": "external_file_reference", "state": "resolved", "sourceProperty": "Source", "referenceMechanism": mechanism},
		},
		"diagnostics": []any{},
	}
}

func (c *coordinatedClosureCapability) Invoke(ctx context.Context, req runtimecap.Request) (*runtimecap.Result, error) {
	occurrenceContent, err := os.ReadFile(filepath.Join(req.WorkingCopyDir, cadruntime.OccurrenceRequestFilename))
	if err != nil {
		c.t.Fatal(err)
	}
	occurrenceRequest, err := cadruntime.DecodeOccurrenceRequest(occurrenceContent)
	if err != nil || !reflect.DeepEqual(occurrenceRequest, *c.occurrenceRequest) {
		c.t.Fatalf("occurrence intent changed: %+v %v", occurrenceRequest, err)
	}
	factContent, err := os.ReadFile(filepath.Join(req.WorkingCopyDir, cadruntime.FactRequestFilename))
	if err != nil {
		c.t.Fatal(err)
	}
	factRequest, err := cadruntime.DecodeFactRequest(factContent)
	if err != nil || !reflect.DeepEqual(factRequest, *c.factRequest) {
		c.t.Fatalf("fact intent changed: %+v %v", factRequest, err)
	}

	integrityRequestBytes, err := os.ReadFile(req.IntegrityRequestPath)
	if err != nil {
		c.t.Fatal(err)
	}
	integrityRequest, err := cadruntime.ParseFreeCADIntegrityRequest(integrityRequestBytes)
	if err != nil {
		c.t.Fatal(err)
	}
	c.resourceCoords = map[string]string{}
	for _, resource := range integrityRequest.Resources {
		c.resourceCoords[resource.LogicalID] = resource.MaterializedCoordinate
		if resource.ExpectsPersistedResult {
			c.primaryCoordinate = resource.MaterializedCoordinate
		}
	}

	result, err := runtimecap.NewExternalCapability().Invoke(ctx, req)
	if err != nil {
		return result, err
	}

	c.occurrenceRaw = c.buildOccurrenceEvidence()
	if err := os.WriteFile(filepath.Join(req.OutputDir, cadruntime.OccurrenceEvidenceFilename), c.occurrenceRaw, 0o600); err != nil {
		c.t.Fatal(err)
	}
	c.factRaw = c.buildFactEvidence()
	if err := os.WriteFile(filepath.Join(req.OutputDir, cadruntime.FactEvidenceFilename), c.factRaw, 0o600); err != nil {
		c.t.Fatal(err)
	}
	c.integrityRaw = c.buildIntegrityEvidence(req.WorkingCopyDir, integrityRequest, coordinatedProductLabel(req.WorkingCopyDir))
	if err := os.WriteFile(filepath.Join(req.OutputDir, cadruntime.FreeCADIntegrityEvidenceFilename), c.integrityRaw, 0o600); err != nil {
		c.t.Fatal(err)
	}
	return result, nil
}

func (c *coordinatedClosureCapability) buildOccurrenceEvidence() []byte {
	parent := c.occurrenceRequest.Scope.Parent
	evidence := cadruntime.OccurrenceRuntimeEvidence{
		SchemaVersion: cadruntime.OccurrenceEvidenceSchemaVersion, Kind: "native_occurrence_evidence", FreeCADVersion: "1.1.4",
		Scope: c.occurrenceRequest.Scope, Parent: &cadruntime.OccurrenceNativeEntity{Entity: parent, Document: c.resourceCoords[parent.Resource.LogicalID], NativeType: "Assembly::AssemblyObject"},
		Completeness: "complete", InspectionCompleted: true, Group: []string{"Second", "AliasPart", "First"}, Members: []cadruntime.OccurrenceRuntimeMember{}, Diagnostics: []string{},
	}
	if c.mode == "incomplete" {
		evidence.Completeness = "incomplete"
		evidence.Diagnostics = []string{"controlled incomplete occurrence scope"}
	}
	targets := []struct {
		name     string
		resource string
		entity   string
	}{
		{"Second", "box_model", "Body"},
		{"AliasPart", "alias", "Part"},
		{"First", "box_model", "Body"},
	}
	for _, target := range targets {
		zero, valid := 0, true
		entity := coordinatedEntity(target.resource, "object", target.entity)
		evidence.Members = append(evidence.Members, cadruntime.OccurrenceRuntimeMember{
			Name: target.name, Parent: parent, NativeType: "App::Link", ElementCount: &zero, MembershipCorroborated: true,
			Classification: "occurrence", Resolution: "resolved", NativeStateValid: &valid,
			Target: &cadruntime.OccurrenceNativeEntity{Entity: entity, Document: c.resourceCoords[target.resource], NativeType: "PartDesign::Body"},
		})
	}
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		c.t.Fatal(err)
	}
	return append(raw, '\n')
}

func (c *coordinatedClosureCapability) buildFactEvidence() []byte {
	evidence := cadruntime.FactRuntimeEvidence{SchemaVersion: recordcontract.FactEvidenceSchemaVersion, Kind: "native_fact_evidence", FreeCADVersion: "1.1.4", Facts: []cadruntime.FactRuntimeEvidenceFact{}}
	for _, binding := range c.factRequest.Bindings {
		fact := cadruntime.FactRuntimeEvidenceFact{
			Kind: binding.Kind, Context: append([]recordcontract.OccurrenceContextFrame{}, binding.Context...), Property: binding.Property,
			Subject: cadruntime.FactRuntimeSubject{Entity: binding.Subject, Document: c.resourceCoords[binding.Subject.Resource.LogicalID], NativeType: "PartDesign::Body"},
		}
		switch {
		case binding.Property == "Material":
			fact.ValueKind = recordcontract.FactValueKindString
			fact.Value = json.RawMessage(`"AL-6061"`)
			fact.Availability = recordcontract.FactAvailabilityObserved
		case binding.Property == "Length":
			fact.ValueKind = recordcontract.FactValueKindNumber
			fact.Value = json.RawMessage("42.5")
			fact.Unit = binding.Unit
			fact.Availability = recordcontract.FactAvailabilityObserved
		default:
			fact.Availability = recordcontract.FactAvailabilityUnavailable
			fact.Diagnostic = "controlled unavailable density observation"
		}
		evidence.Facts = append(evidence.Facts, fact)
	}
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		c.t.Fatal(err)
	}
	return append(raw, '\n')
}

func (c *coordinatedClosureCapability) buildIntegrityEvidence(workingCopy string, request *cadruntime.FreeCADIntegrityRequest, product string) []byte {
	c.baselineDigests = map[string]string{}
	c.resultDigests = map[string]string{}
	resources := make([]map[string]any, 0, len(request.Resources))
	for _, resource := range request.Resources {
		path := filepath.Join(workingCopy, filepath.FromSlash(resource.MaterializedCoordinate))
		baseline := coordinatedFileDigest(c.t, path)
		if c.mode == "baseline_mismatch" {
			baseline = strings.Repeat("0", 64)
		}
		c.baselineDigests[resource.LogicalID] = baseline
		entry := map[string]any{"logicalId": resource.LogicalID, "materializedCoordinate": resource.MaterializedCoordinate, "status": "observed", "baselineSha256": baseline}
		if resource.ExpectsPersistedResult {
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				c.t.Fatal(err)
			}
			if _, err := file.WriteString("controlled-configured-result-" + product); err != nil {
				file.Close()
				c.t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				c.t.Fatal(err)
			}
			persisted := coordinatedFileDigest(c.t, path)
			if c.mode == "persisted_mismatch" {
				persisted = strings.Repeat("0", 64)
			}
			c.resultDigests[resource.LogicalID] = persisted
			entry["persistedResult"] = map[string]any{"coordinate": resource.MaterializedCoordinate, "sha256": persisted}
		}
		resources = append(resources, entry)
	}
	raw, err := json.MarshalIndent(map[string]any{"schemaVersion": cadruntime.FreeCADIntegritySchemaVersion, "resources": resources}, "", "  ")
	if err != nil {
		c.t.Fatal(err)
	}
	return append(raw, '\n')
}

func coordinatedProductLabel(workingCopy string) string {
	parts := strings.Split(filepath.ToSlash(workingCopy), "/")
	for index, part := range parts {
		if part == "products" && index+1 < len(parts) {
			return parts[index+1]
		}
	}
	return filepath.Base(workingCopy)
}

func coordinatedFileDigest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

func (f *coordinatedClosureFixture) execute(t *testing.T, capabilityMode string) (scheduler.ExecutionResult, []*coordinatedClosureCapability) {
	t.Helper()
	execution := scheduler.ExecutionResult{Jobs: make([]scheduler.JobExecution, 0, len(f.jobs))}
	var capabilities []*coordinatedClosureCapability
	for _, j := range f.jobs {
		pkg, err := handoff.FromJob(j)
		if err != nil {
			t.Fatal(err)
		}
		resources := f.planned.ResourceContexts[j.ProductKey]
		productDir := filepath.Join(f.root, "products", j.ProductKey)
		if err := os.MkdirAll(productDir, 0o700); err != nil {
			t.Fatal(err)
		}
		capability := &coordinatedClosureCapability{t: t, occurrenceRequest: recordcontract.CloneOccurrenceRequest(f.occurrenceRequest), factRequest: recordcontract.CloneFactRequest(f.factRequest), mode: capabilityMode}
		aligned, err := cadruntime.NewFreeCADRuntimeAdapter(freecad.NewFreeCADAdapter(productDir), capability)
		if err != nil {
			t.Fatal(err)
		}
		resolver, err := runtimecap.NewExecutableResolver(map[string]string{"freecad": f.command})
		if err != nil {
			t.Fatal(err)
		}
		e := executor.NewWithCADRuntimeConsumption(aligned, resolver, productDir)
		e.SetResourceContext(resources)
		e.SetSourceIntegrity(f.planned.SourceIntegrity)
		e.SetIntegrityRequestDir(f.root)
		err = e.ExecutePackageWithOptions(context.Background(), pkg, executor.PackageExecutionOptions{OccurrenceObservation: recordcontract.CloneOccurrenceRequest(f.occurrenceRequest), FactObservation: recordcontract.CloneFactRequest(f.factRequest)})
		execution.Jobs = append(execution.Jobs, scheduler.JobExecution{Job: j, States: e.States(), Err: err, CADRuntimeOutcome: e.CADRuntimeOutcome()})
		capabilities = append(capabilities, capability)
	}
	return execution, capabilities
}

func (f *coordinatedClosureFixture) emit(t *testing.T, execution scheduler.ExecutionResult) map[string][]byte {
	t.Helper()
	outcomes, err := runOutcomeEvidences(execution)
	if err != nil || len(outcomes) != 2 {
		t.Fatalf("outcome projection = %#v, err = %v", outcomes, err)
	}
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rep, err := report.Build(report.BuildInput{PlanHash: f.planned.PlanHash, DSLHash: f.planned.DSLHash, Execution: execution, StartedAt: stamp, EndedAt: stamp, WorkerCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{RunRoot: f.root, PlanHash: f.planned.PlanHash, Report: rep, SourceIntegrity: f.planned.SourceIntegrity, Outcomes: outcomes}); err != nil {
		t.Fatalf("EmitRunPackage: %v", err)
	}
	return readRecordPackageFiles(t, recordPackageRoot(f.root))
}

func TestCoordinatedClosure_MultiResourceMultiOutcomeRehearsal(t *testing.T) {
	f := newCoordinatedClosureFixture(t)
	execution, capabilities := f.execute(t, "complete")
	for _, jobExecution := range execution.Jobs {
		if jobExecution.Err != nil {
			t.Fatalf("job execution failed: %v", jobExecution.Err)
		}
		outcome := jobExecution.CADRuntimeOutcome
		if outcome == nil || outcome.Verification != artifact.VerificationOutcomePassed || outcome.VerificationResult.Status != verification.StatusPass {
			t.Fatalf("outcome not verified: %#v", outcome)
		}
		if len(outcome.Resources.Bindings()) != 3 {
			t.Fatalf("outcome resources = %#v, want finite three-resource inventory", outcome.Resources.Bindings())
		}
	}

	files := f.emit(t, execution)
	observations := decodeCoordinatedObservations(t, files)
	if len(observations) != 2 {
		t.Fatalf("observation records = %d, want two outcome-scoped records", len(observations))
	}
	references := decodeCoordinatedReferences(t, files)
	if len(references) != 2 {
		t.Fatalf("reference records = %d, want two outcome-scoped records", len(references))
	}
	for _, capability := range capabilities {
		for _, resource := range []string{"box_model", "alias", "unused"} {
			if capability.resourceCoords[resource] == "" || capability.baselineDigests[resource] == "" {
				t.Fatalf("resource %q missing from attempt integrity request/evidence", resource)
			}
		}
		if capability.baselineDigests["box_model"] == capability.resultDigests["box_model"] {
			t.Fatal("configured-result digest equals baseline")
		}
	}
	if len(capabilities) == 2 {
		if capabilities[0].baselineDigests["box_model"] != capabilities[1].baselineDigests["box_model"] {
			t.Fatal("shared source identity differs across outcomes")
		}
		if capabilities[0].resultDigests["box_model"] == capabilities[1].resultDigests["box_model"] {
			t.Fatal("configured-result identity is not outcome-specific")
		}
	}

	for _, observation := range observations {
		if observation.Observation.Occurrences == nil || observation.Observation.Occurrences.Completeness != "complete" {
			t.Fatalf("occurrence summary = %#v, want complete scope", observation.Observation.Occurrences)
		}
		quantities := observation.Observation.Occurrences.Quantities
		if len(quantities) != 2 {
			t.Fatalf("quantity groups = %#v, want two", quantities)
		}
		for _, quantity := range quantities {
			if quantity.Target.Resource.LogicalID == "box_model" && quantity.Quantity != 2 {
				t.Fatalf("primary quantity = %#v, want 2 from two occurrences, not generic reference count", quantity)
			}
			if quantity.Target.Resource.LogicalID == "alias" && quantity.Quantity != 1 {
				t.Fatalf("alias quantity = %#v, want 1", quantity)
			}
		}
		facts := observationFacts(observation)
		if facts["box_model/Material"].Availability != recordcontract.FactAvailabilityObserved || facts["box_model/Material"].Value.Raw != `"AL-6061"` {
			t.Fatalf("material fact = %#v", facts["box_model/Material"])
		}
		if facts["alias/Length/context"].Value.Raw != "42.5" || facts["alias/Length/context"].Unit != "mm" || len(facts["alias/Length/context"].Subject.Context) != 1 {
			t.Fatalf("unit-bearing numeric fact = %#v", facts["alias/Length/context"])
		}
		if facts["unused/Density"].Availability != recordcontract.FactAvailabilityUnavailable || facts["unused/Density"].Value.Raw != "" {
			t.Fatalf("unavailable fact = %#v, want explicit unavailability", facts["unused/Density"])
		}
		if observation.Provenance.Integrity == nil || len(observation.Provenance.Integrity.Resources) != 3 {
			t.Fatalf("observation integrity provenance = %#v", observation.Provenance.Integrity)
		}
		if observation.Provenance.Integrity.Lock != recordcontract.LockVerificationVerified {
			t.Fatalf("lock state = %q, want verified", observation.Provenance.Integrity.Lock)
		}
	}

	for _, reference := range references {
		if reference.Reference.Completeness != "incomplete" || !reference.Reference.Unsupported {
			t.Fatalf("reference scope = %#v, want incomplete unsupported undeclared target", reference.Reference)
		}
		if len(reference.Reference.Edges) != 3 {
			t.Fatalf("reference edges = %d, want three distinct relationships", len(reference.Reference.Edges))
		}
		qualifications := map[string]bool{}
		for _, edge := range reference.Reference.Edges {
			if edge.Source.Resource == nil || edge.Source.Resource.LogicalID != "box_model" {
				t.Fatalf("reference source qualification = %#v", edge.Source)
			}
			qualifications[edge.Target.Qualification] = true
			if edge.Target.Qualification == "declared" {
				if edge.Target.Resource == nil || edge.Target.Resource.LogicalID != "alias" || edge.SourceProperty == nil || edge.ReferenceMechanism == nil {
					t.Fatalf("declared alias target lost qualification/discriminators: %#v", edge)
				}
			}
		}
		if !qualifications["declared"] || !qualifications["undeclared"] || !qualifications["unsupported"] {
			t.Fatalf("reference target qualifications = %#v, want declared, undeclared, and unsupported", qualifications)
		}
	}

	// Raw family evidence is outcome-scoped and exact.
	for _, capability := range capabilities {
		if !bytesContainedInScopedRaw(files, capability.occurrenceRaw) || !bytesContainedInScopedRaw(files, capability.factRaw) || !bytesContainedInScopedRaw(files, capability.integrityRaw) || !bytesContainedInScopedRaw(files, f.traversal) {
			t.Fatal("outcome-scoped raw evidence lost or altered")
		}
	}
	executionRecord := decodeCoordinatedExecution(t, files)
	if executionRecord.Provenance.Integrity == nil {
		t.Fatal("run-level source integrity provenance missing")
	}
	for _, resource := range executionRecord.Provenance.Integrity.Resources {
		if resource.PersistedResultDigestSHA256 != "" || resource.ResultStatus != recordcontract.IntegrityStatusNotPerformed {
			t.Fatalf("run-level provenance absorbed configured-result identity: %#v", resource)
		}
	}
}

func TestCoordinatedClosure_UnsupportedVersionContractsAreRejected(t *testing.T) {
	cases := map[string]func() error{
		"observed evidence": func() error {
			return observed.Validate(&observed.Observed{SchemaVersion: "2.0"})
		},
		"occurrence request": func() error {
			_, err := cadruntime.DecodeOccurrenceRequest([]byte(`{"schemaVersion":"2.0","requirement":"optional","scope":{}}`))
			return err
		},
		"fact request": func() error {
			_, err := cadruntime.DecodeFactRequest([]byte(`{"schemaVersion":"2.0","bindings":[]}`))
			return err
		},
		"integrity request": func() error {
			_, err := cadruntime.ParseFreeCADIntegrityRequest([]byte(`{"schemaVersion":"2.0","resources":[]}`))
			return err
		},
		"integrity evidence": func() error {
			_, err := cadruntime.ParseFreeCADIntegrityEvidence([]byte(`{"schemaVersion":"2.0","resources":[]}`))
			return err
		},
		"reference traversal evidence": func() error {
			_, err := recordmap.MapReferenceTraversal(recordmap.ReferenceTraversalMappingInput{RecordKey: "unsupported-version", Traversal: recordmap.ReferenceTraversalRuntimeEvidence{SchemaVersion: "2.0"}})
			return err
		},
		"normalized record version": func() error {
			return recordcontract.ValidateVersion("2.0")
		},
	}
	for name, validate := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validate(); err == nil {
				t.Fatalf("%s accepted an unsupported schema version", name)
			}
		})
	}
}

func TestCoordinatedClosure_RepeatRunDeterministicSurfaces(t *testing.T) {
	f := newCoordinatedClosureFixture(t)
	firstExecution, _ := f.execute(t, "complete")
	firstFiles := f.emit(t, firstExecution)
	secondExecution, _ := f.execute(t, "complete")
	secondFiles := f.emit(t, secondExecution)

	firstKeys := coordinatedNormalizedPackageKeys(firstFiles)
	secondKeys := coordinatedNormalizedPackageKeys(secondFiles)
	if !reflect.DeepEqual(firstKeys, secondKeys) {
		t.Fatalf("normalized/package paths differ across repeated runs:\nfirst:  %v\nsecond: %v", firstKeys, secondKeys)
	}
	for _, path := range firstKeys {
		if !reflect.DeepEqual(firstFiles[path], secondFiles[path]) {
			t.Fatalf("deterministic Engine-owned package file %q differs across repeated runs", path)
		}
	}
}

func coordinatedNormalizedPackageKeys(files map[string][]byte) []string {
	var out []string
	for path := range files {
		if path == recordpackage.PackageManifestContractPath() || strings.HasPrefix(path, "records/") {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func TestCoordinatedClosure_NegativeSurfaceIsolation(t *testing.T) {
	f := newCoordinatedClosureFixture(t)
	execution, _ := f.execute(t, "incomplete")
	for _, jobExecution := range execution.Jobs {
		if jobExecution.Err != nil {
			t.Fatalf("incomplete occurrence should remain a retained observation, got %v", jobExecution.Err)
		}
	}
	files := f.emit(t, execution)
	observations := decodeCoordinatedObservations(t, files)
	if len(observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(observations))
	}
	for _, observation := range observations {
		if observation.Observation.Occurrences == nil || observation.Observation.Occurrences.Completeness != "incomplete" || len(observation.Observation.Occurrences.Quantities) != 0 {
			t.Fatalf("incomplete occurrence claimed quantity: %#v", observation.Observation.Occurrences)
		}
		if !hasObservedCoordinatedFact(observation) {
			t.Fatal("incomplete occurrence corrupted otherwise valid facts")
		}
	}
}

func decodeCoordinatedObservations(t *testing.T, files map[string][]byte) []recordcontract.ObservationRecord {
	t.Helper()
	var out []recordcontract.ObservationRecord
	for path, content := range files {
		if strings.HasPrefix(path, "records/outcomes/") && strings.HasSuffix(path, "prm.observation-record.json") {
			var record recordcontract.ObservationRecord
			if err := json.Unmarshal(content, &record); err != nil {
				t.Fatal(err)
			}
			if err := recordcontract.ValidateObservationRecord(record); err != nil {
				t.Fatal(err)
			}
			out = append(out, record)
		}
	}
	return out
}

func decodeCoordinatedReferences(t *testing.T, files map[string][]byte) []recordcontract.ReferenceRecord {
	t.Helper()
	var out []recordcontract.ReferenceRecord
	for path, content := range files {
		if strings.HasPrefix(path, "records/outcomes/") && strings.HasSuffix(path, "prm.reference-record.json") {
			var record recordcontract.ReferenceRecord
			if err := json.Unmarshal(content, &record); err != nil {
				t.Fatal(err)
			}
			if err := recordcontract.ValidateReferenceRecord(record); err != nil {
				t.Fatal(err)
			}
			out = append(out, record)
		}
	}
	return out
}

func decodeCoordinatedExecution(t *testing.T, files map[string][]byte) recordcontract.ExecutionRecord {
	t.Helper()
	var record recordcontract.ExecutionRecord
	if err := json.Unmarshal(files[recordpackage.MustRecordContractPath("execution")], &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func observationFacts(record recordcontract.ObservationRecord) map[string]recordcontract.ObservationFact {
	out := map[string]recordcontract.ObservationFact{}
	for _, fact := range record.Observation.Facts {
		if fact.Kind != recordcontract.ObservationKindFact || fact.Subject.Resource == nil {
			continue
		}
		key := fact.Subject.Resource.LogicalID + "/" + fact.Property
		if len(fact.Subject.Context) > 0 {
			key += "/context"
		}
		out[key] = fact
	}
	return out
}

func hasObservedCoordinatedFact(record recordcontract.ObservationRecord) bool {
	for _, fact := range record.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindFact && fact.Availability == recordcontract.FactAvailabilityObserved {
			return true
		}
	}
	return false
}

func bytesContainedInScopedRaw(files map[string][]byte, want []byte) bool {
	for path, content := range files {
		if strings.HasPrefix(path, "raw/outcomes/") && string(content) == string(want) {
			return true
		}
	}
	return false
}
