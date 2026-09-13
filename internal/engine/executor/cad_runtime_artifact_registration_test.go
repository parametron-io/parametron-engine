package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/runtimecap"
)

// --- Shared fixtures for the aligned artifact-registration pipeline ---

// pipelineAdapter writes real WriteCSV/WriteExportManifest files (so the
// artifact store's existence check can find them) and delegates aligned
// orchestration to a caller-supplied closure, which is responsible for
// writing the aligned runtime's own result/artifact files.
type pipelineAdapter struct {
	productDir string

	mu          sync.Mutex
	run         cadruntime.FreeCADRuntimeVerifiedRun
	ok          bool
	orchestrate func(ctx context.Context, req adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error)
}

func (a *pipelineAdapter) Run(_ context.Context, step planner.Step) error {
	switch step.Type {
	case planner.StepWriteCSV:
		payload := step.Payload.(planner.WriteCSVPayload)
		return os.WriteFile(filepath.Join(a.productDir, payload.Filename), []byte("width\n10\n"), 0o644)
	case planner.StepWriteExportManifest:
		payload := step.Payload.(planner.WriteExportManifestPayload)
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(a.productDir, payload.ManifestFilename), data, 0o644)
	}
	return nil
}

func (a *pipelineAdapter) OrchestrateCADRuntime(ctx context.Context, req adapter.CADRuntimeOrchestrationRequest) error {
	run, ok, err := a.orchestrate(ctx, req)
	a.mu.Lock()
	a.run, a.ok = run, ok
	a.mu.Unlock()
	return err
}

func (a *pipelineAdapter) CADRuntimeVerifiedRun() (cadruntime.FreeCADRuntimeVerifiedRun, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.run, a.ok
}

// writeAlignedRuntimeFiles materializes the real result.json and declared
// artifact files a passing run claims, so the artifact store's existence
// checks succeed.
func writeAlignedRuntimeFiles(t *testing.T, run cadruntime.FreeCADRuntimeVerifiedRun) {
	t.Helper()
	resultPath := run.Runtime.ExecutionRequest.ResultPath
	if err := os.MkdirAll(filepath.Dir(resultPath), 0o755); err != nil {
		t.Fatalf("mkdir result dir: %v", err)
	}
	if err := os.WriteFile(resultPath, []byte(`{"schemaVersion":"1.0","status":"succeeded"}`), 0o644); err != nil {
		t.Fatalf("write result.json: %v", err)
	}
	for _, a := range run.Runtime.Artifacts {
		if err := os.MkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
			t.Fatalf("mkdir artifact dir: %v", err)
		}
		if err := os.WriteFile(a.Path, []byte("evidence:"+a.ID), 0o644); err != nil {
			t.Fatalf("write artifact %s: %v", a.ID, err)
		}
	}
}

// alignedRegistrationFixture wires a full ExecutePackage-driven pipeline:
// planner-shaped handoff package -> pipelineAdapter -> real FileSystemStore.
// The artifact store's baseDir contains productDir, so RecordExisting(Batch)
// containment checks succeed for both the legacy (CSV/manifest) and aligned
// (result/artifacts) files.
type alignedRegistrationFixture struct {
	baseDir    string
	productDir string
	store      *artifact.FileSystemStore
	adapter    *pipelineAdapter
	resolver   *cadResolver
}

func newAlignedRegistrationExecutor(t *testing.T, passing bool) (*Executor, *alignedRegistrationFixture) {
	t.Helper()
	baseDir := t.TempDir()
	productDir := filepath.Join(baseDir, "widget")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatalf("mkdir productDir: %v", err)
	}
	store := artifact.NewFileSystemStore(baseDir)
	resolver := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	adp := &pipelineAdapter{productDir: productDir}
	adp.orchestrate = func(_ context.Context, req adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
		run := task12RunForRequest(filepath.Join(baseDir, "_working"), req)
		if !passing {
			run.Runtime.Result.Status = freecad.FreeCADRuntimeResultStatusFailed
			run.Verification = nil
		}
		writeAlignedRuntimeFiles(t, run)
		if !passing {
			return run, true, &cadruntime.FreeCADRuntimeVerificationError{
				Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution,
				Err:   context.DeadlineExceeded,
			}
		}
		return run, true, nil
	}
	e := NewWithArtifactsAndCADRuntimeConsumption(adp, store, resolver, productDir)
	return e, &alignedRegistrationFixture{baseDir: baseDir, productDir: productDir, store: store, adapter: adp, resolver: resolver}
}

// ===================== Part H: executor aligned artifact registration =====================

func TestNewWithArtifactsAndCADRuntimeConsumption_ConfiguresBothBoundaries(t *testing.T) {
	adp := &legacyOnlyAdapter{}
	store := artifact.NewFileSystemStore(t.TempDir())
	resolver := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	productDir := t.TempDir()
	e := NewWithArtifactsAndCADRuntimeConsumption(adp, store, resolver, productDir)

	if !e.cadRuntimeConsumption {
		t.Fatal("expected Task 12 consumption to be enabled")
	}
	if e.store == nil {
		t.Fatal("expected artifact store to be attached")
	}
	if e.productDir != productDir {
		t.Fatalf("expected productDir %q, got %q", productDir, e.productDir)
	}
	if e.cadRuntime == nil || e.cadRuntime.resolver != resolver {
		t.Fatal("expected the CAD runtime executable resolver to be retained")
	}
	if e.strategy == nil {
		t.Fatal("expected normal cache strategy initialization to be preserved")
	}
}

func TestExecutor_AlignedRuntimeRegistersResultAndArtifacts(t *testing.T) {
	e, fx := newAlignedRegistrationExecutor(t, true)

	realPkg := alignedPackage(t, "freecad")
	if err := e.ExecutePackage(context.Background(), realPkg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := fx.store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var resultRecords, stepRecords, csvRecords, pdfRecords []artifact.Artifact
	for _, a := range got {
		switch {
		case a.Filename == "result.json":
			resultRecords = append(resultRecords, a)
		case a.Filename == "outputs/widget.step":
			stepRecords = append(stepRecords, a)
		case a.Filename == "outputs/nested/table.csv":
			csvRecords = append(csvRecords, a)
		case a.Filename == "outputs/drawing.pdf":
			pdfRecords = append(pdfRecords, a)
		}
	}
	if len(resultRecords) != 1 || resultRecords[0].Type != artifact.ArtifactTypeJSON || resultRecords[0].Class != artifact.ArtifactClassExecutionOutput {
		t.Fatalf("result.json registration=%#v", resultRecords)
	}
	assertExecutionAndVerified := func(name string, records []artifact.Artifact, wantType artifact.ArtifactType) {
		t.Helper()
		if len(records) != 2 {
			t.Fatalf("%s: expected 2 records (execution output + verified), got %d: %#v", name, len(records), records)
		}
		var hasExecOutput, hasVerified bool
		for _, r := range records {
			if r.Type != wantType {
				t.Fatalf("%s: expected type %q, got %q", name, wantType, r.Type)
			}
			switch r.Class {
			case artifact.ArtifactClassExecutionOutput:
				hasExecOutput = true
			case artifact.ArtifactClassVerified:
				hasVerified = true
			}
		}
		if !hasExecOutput || !hasVerified {
			t.Fatalf("%s: expected both execution-output and verified classes, got %#v", name, records)
		}
	}
	assertExecutionAndVerified("step", stepRecords, artifact.ArtifactTypeSTEP)
	assertExecutionAndVerified("csv", csvRecords, artifact.ArtifactTypeCSV)
	assertExecutionAndVerified("pdf", pdfRecords, artifact.ArtifactTypePDF)
}

// nativeOnlyAlignedPackage mirrors alignedPackage but declares zero
// manifest outputs, mirroring authoring-only outputs=["none"] planning:
// parameter assignments/verification intent survive, but the manifest
// requests zero derived exports.
func nativeOnlyAlignedPackage(t *testing.T, id string) *handoff.Package {
	t.Helper()
	steps := []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv", Headers: []string{"width"}, Values: []any{10.0}}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{
			ProductKey: "widget", ManifestFilename: planner.ExportManifestFilename, SchemaVersion: planner.ExportManifestSchemaVersion,
			Adapter: id, Product: planner.ExportManifestProduct{ID: "widget"}, Values: map[string]any{"width": 10.0},
			ParameterAssignments: []planner.ExportManifestParameterAssignment{{Name: "width", Value: 10.0, Type: "float", Unit: "mm"}},
			Outputs:              []planner.ExportManifestOutput{},
			Verification:         planner.VerificationManifestIntent{ExpectedParameters: []planner.VerificationExpectedParameter{{ID: "width", Name: "width", Value: 10.0, Type: "float", Unit: "mm"}}},
		}},
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: id, ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json"}}}
	j, err := job.New("widget", steps)
	if err != nil {
		t.Fatal(err)
	}
	p, err := handoff.FromJob(j)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestExecutor_AlignedRuntimeZeroDerivedArtifactsRegistersResultOnly proves
// the successful native-only completion contract end to end through
// ExecutePackage: runtime status succeeded, verification passed, zero
// derived artifacts. result.json remains a registered execution output,
// zero derived execution/verified artifacts are added, and no native
// working-copy CAD document (or any fabricated artifact) is registered.
func TestExecutor_AlignedRuntimeZeroDerivedArtifactsRegistersResultOnly(t *testing.T) {
	baseDir := t.TempDir()
	productDir := filepath.Join(baseDir, "widget")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := artifact.NewFileSystemStore(baseDir)
	resolver := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	adp := &pipelineAdapter{productDir: productDir}
	adp.orchestrate = func(_ context.Context, req adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
		run := task12RunForRequest(filepath.Join(baseDir, "_working"), req)
		run.Runtime.Artifacts = []cadruntime.FreeCADRuntimeValidatedArtifact{}
		writeAlignedRuntimeFiles(t, run)
		return run, true, nil
	}
	e := NewWithArtifactsAndCADRuntimeConsumption(adp, store, resolver, productDir)

	pkg := nativeOnlyAlignedPackage(t, "freecad")
	if err := e.ExecutePackage(context.Background(), pkg); err != nil {
		t.Fatalf("unexpected error for native-only successful completion: %v", err)
	}

	got, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var resultRecords []artifact.Artifact
	var derivedRecords []artifact.Artifact
	for _, a := range got {
		if a.Filename == "result.json" {
			resultRecords = append(resultRecords, a)
			continue
		}
		if a.Filename == "widget.csv" || a.Filename == planner.ExportManifestFilename {
			continue
		}
		derivedRecords = append(derivedRecords, a)
	}
	if len(resultRecords) != 1 || resultRecords[0].Type != artifact.ArtifactTypeJSON || resultRecords[0].Class != artifact.ArtifactClassExecutionOutput {
		t.Fatalf("expected exactly one result.json execution-output registration, got %#v", resultRecords)
	}
	if len(derivedRecords) != 0 {
		t.Fatalf("expected zero derived (non-CSV/manifest/result) artifact registrations, got %#v", derivedRecords)
	}
	for _, a := range got {
		if strings.Contains(a.Filename, ".FCStd") {
			t.Fatalf("expected no native CAD working-copy document to be registered, found %+v", a)
		}
	}
}

func TestExecutor_AlignedRuntimeRequiresVerificationPassForVerifiedArtifacts(t *testing.T) {
	cases := []artifact.VerificationOutcome{
		artifact.VerificationOutcomeFailed,
		artifact.VerificationOutcomeNotRun,
		artifact.VerificationOutcomeUnknown,
	}
	for _, v := range cases {
		t.Run(string(v), func(t *testing.T) {
			baseDir := t.TempDir()
			productDir := filepath.Join(baseDir, "widget")
			if err := os.MkdirAll(productDir, 0o755); err != nil {
				t.Fatal(err)
			}
			store := artifact.NewFileSystemStore(baseDir)
			e := NewWithArtifacts(&legacyOnlyAdapter{}, store, productDir)
			e.jobID = "job-h3"
			e.cadRuntimeOutcome = &CADRuntimeOutcome{
				JobID: "job-h3", ProductKey: "widget", StepID: "2", Adapter: "freecad",
				ResultPath: filepath.Join(productDir, "result.json"), Verification: v,
			}
			step := planner.Step{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{
				ProductKey: "widget", Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json",
			}}
			if err := e.registerCADRuntimeArtifacts(context.Background(), step, "2"); err == nil {
				t.Fatalf("expected registration to be refused for verification outcome %q", v)
			}
			got, err := store.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 0 {
				t.Fatalf("expected no artifacts registered without an explicit pass, got %d", len(got))
			}
		})
	}
}

func TestExecutor_AlignedRuntimeMapsArtifactFormats(t *testing.T) {
	tests := []struct {
		format  string
		want    artifact.ArtifactType
		wantErr bool
	}{
		{"step", artifact.ArtifactTypeSTEP, false},
		{"csv", artifact.ArtifactTypeCSV, false},
		{"pdf", artifact.ArtifactTypePDF, false},
		{"stl", artifact.ArtifactTypeUnknown, true},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got, err := artifactTypeForOutput(tt.format)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for format %q", tt.format)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("format %q: got %q want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestExecutor_AlignedRuntimePreservesNestedArtifactFilenames(t *testing.T) {
	e, fx := newAlignedRegistrationExecutor(t, true)
	realPkg := alignedPackage(t, "freecad")
	if err := e.ExecutePackage(context.Background(), realPkg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := fx.store.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range got {
		if a.Filename == "outputs/nested/table.csv" {
			found = true
		}
		if filepath.Base(a.Filename) != "" && filepath.ToSlash(a.Filename) != a.Filename {
			t.Fatalf("expected slash-separated stored filename, got %q", a.Filename)
		}
	}
	if !found {
		t.Fatal("expected the nested artifact filename outputs/nested/table.csv to be preserved exactly")
	}
}

func TestExecutor_AlignedRuntimeDoesNotRegisterRawEvidence(t *testing.T) {
	e, fx := newAlignedRegistrationExecutor(t, true)
	realPkg := alignedPackage(t, "freecad")
	if err := e.ExecutePackage(context.Background(), realPkg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := fx.store.List()
	if err != nil {
		t.Fatal(err)
	}

	forbidden := []string{
		"parametron.observed.json",
		"observation_request.json",
		"native_manifest.json",
		"prepared_source",
		"source",
	}
	verifiedInputCSV := false
	for _, a := range got {
		for _, name := range forbidden {
			if a.Filename == name {
				t.Fatalf("did not expect raw evidence file %q to be registered", name)
			}
		}
		if a.Filename == "widget.csv" && a.Class == artifact.ArtifactClassVerified {
			verifiedInputCSV = true
		}
	}
	if verifiedInputCSV {
		t.Fatal("the input CSV must never be registered as a verified artifact")
	}
	// Exactly the expected records: CSV(1) + manifest(1) + result(1) + 3 aligned outputs x 2 classes(6) = 9.
	if len(got) != 9 {
		t.Fatalf("expected exactly 9 registered records, got %d: %#v", len(got), got)
	}
}

func TestExecutor_AlignedArtifactRegistrationRequiresFinalRuntimeStep(t *testing.T) {
	baseDir := t.TempDir()
	productDir := filepath.Join(baseDir, "widget")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := artifact.NewFileSystemStore(baseDir)
	adp := &legacyOnlyAdapter{}
	e := NewWithArtifacts(adp, store, productDir)

	plan := &planner.ExecutionPlan{Steps: []planner.Step{
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{
			ProductKey: "widget", Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json",
		}},
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv"}},
	}}

	err := e.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected an error when the RunCADRuntime step is not final")
	}
	if len(adp.runs) != 0 {
		t.Fatalf("expected no adapter calls before the final-step check, got %d", len(adp.runs))
	}
}

func TestExecutor_AlignedArtifactRegistrationIsAtomic(t *testing.T) {
	baseDir := t.TempDir()
	productDir := filepath.Join(baseDir, "widget")
	if err := os.MkdirAll(filepath.Join(productDir, "outputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(productDir, "result.json")
	if err := os.WriteFile(resultPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	goodPath := filepath.Join(productDir, "outputs", "widget.step")
	if err := os.WriteFile(goodPath, []byte("STEP"), 0o644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(productDir, "outputs", "widget.csv")
	if err := os.WriteFile(badPath, []byte("CSV"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := artifact.NewFileSystemStore(baseDir)
	// Seed an unrelated, earlier record to prove a failed aligned batch
	// neither corrupts nor rolls back prior store state.
	if _, err := store.RecordExisting(context.Background(), goodPath, artifact.Artifact{
		Type: artifact.ArtifactTypeSTEP, Class: artifact.ArtifactClassExecutionOutput,
		JobID: "job-h8", ProductID: "widget", StepID: "0", Filename: "prior.step",
	}); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	before, err := store.List()
	if err != nil {
		t.Fatal(err)
	}

	e := NewWithArtifacts(&legacyOnlyAdapter{}, store, productDir)
	e.jobID = "job-h8"
	e.cadRuntimeOutcome = &CADRuntimeOutcome{
		JobID: "job-h8", ProductKey: "widget", StepID: "2", Adapter: "freecad",
		ResultPath: resultPath, Verification: artifact.VerificationOutcomePassed,
		Artifacts: []CADRuntimeArtifactOutcome{
			{RuntimeID: "step", Format: "step", RelativePath: "outputs/widget.step", Path: goodPath},
			// An invalid (path-traversal) filename is rejected deep inside
			// the store's batch validation, after the earlier records in
			// this same batch have already passed validation but before any
			// of them are committed.
			{RuntimeID: "csv", Format: "csv", RelativePath: "../escape.csv", Path: badPath},
		},
	}
	step := planner.Step{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{
		ProductKey: "widget", Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json",
	}}

	if err := e.registerCADRuntimeArtifacts(context.Background(), step, "2"); err == nil {
		t.Fatal("expected the batch registration to fail")
	}

	after, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected no partial aligned-batch records to become visible after a mid-batch failure: before=%d after=%d (%#v)", len(before), len(after), after)
	}
}

func TestExecutor_FailedAlignedRuntimeRegistersNoRuntimeArtifacts(t *testing.T) {
	e, fx := newAlignedRegistrationExecutor(t, false)
	realPkg := alignedPackage(t, "freecad")
	err := e.ExecutePackage(context.Background(), realPkg)
	if err == nil {
		t.Fatal("expected a runtime failure error")
	}

	got, listErr := fx.store.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	for _, a := range got {
		if a.StepID == "2" {
			t.Fatalf("did not expect any RunCADRuntime-step artifact to be registered after a failed run: %#v", a)
		}
		if a.Filename == "result.json" || a.Class == artifact.ArtifactClassVerified {
			t.Fatalf("did not expect aligned runtime artifacts after failure: %#v", a)
		}
	}
}
