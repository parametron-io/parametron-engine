package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/integrity"
	"parametron/internal/engine/job"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
	"parametron/internal/engine/resourcebinding"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/scheduler"
)

// This is the #66 cross-repository acceptance proof. It is opt-in and requires
// a real FreeCAD 1.1.4 host behind the production parametron-freecad launcher:
//
//	PARAMETRON_FREECAD_EVIDENCE_PROOF=1 \
//	PARAMETRON_FREECAD_RUNTIME=/path/to/bin/parametron-freecad \
//	PARAMETRON_FREECAD_REPO=/path/to/parametron-freecad \
//	go test ./cmd/parametron -run '^TestRealFreeCADEvidenceProof$' -count=1 -v
//
// The proof never substitutes a controlled in-process capability. Engine writes
// the authoritative occurrence, fact, and integrity request documents, the
// production launcher executes real FreeCAD, and Engine strict intake,
// normalization, quantity rollup, provenance, and package emission consume the
// returned raw evidence.
const (
	evidenceProofGateEnv      = "PARAMETRON_FREECAD_EVIDENCE_PROOF"
	evidenceProofRuntimeEnv   = "PARAMETRON_FREECAD_RUNTIME"
	evidenceProofRepoEnv      = "PARAMETRON_FREECAD_REPO"
	evidenceProofWorkspaceEnv = "PARAMETRON_FREECAD_EVIDENCE_WORKSPACE"
	evidenceProofReportEnv    = "PARAMETRON_FREECAD_EVIDENCE_REPORT"
	evidenceProofGateCommand  = "nix develop --command env PARAMETRON_FREECAD_EVIDENCE_PROOF=1 PARAMETRON_FREECAD_RUNTIME=<wrapper> PARAMETRON_FREECAD_REPO=<parametron-freecad> go test ./cmd/parametron -run '^TestRealFreeCADEvidenceProof$' -count=1 -v"
)

type evidenceProofContext struct {
	t         *testing.T
	runtime   string
	repo      string
	workspace string
	report    evidenceProofReport
}

type evidenceProofReport struct {
	SchemaVersion          string                    `json:"schemaVersion"`
	Status                 string                    `json:"status"`
	FreeCADVersion         []string                  `json:"freecadVersion,omitempty"`
	Scenarios              []evidenceProofScenario   `json:"scenarios"`
	CrossRepositoryBlocker *evidenceProofBlocker     `json:"crossRepositoryBlocker,omitempty"`
	Diagnostics            *evidenceProofDiagnostics `json:"diagnostics,omitempty"`
}

type evidenceProofScenario struct {
	Name       string         `json:"name"`
	Status     string         `json:"status"`
	EnginePath string         `json:"enginePath"`
	Facts      map[string]any `json:"facts,omitempty"`
}

type evidenceProofBlocker struct {
	Boundary string `json:"boundary"`
	Summary  string `json:"summary"`
	Impact   string `json:"impact"`
}

type evidenceProofDiagnostics struct {
	Message string `json:"message"`
}

func TestRealFreeCADEvidenceProof(t *testing.T) {
	if os.Getenv(evidenceProofGateEnv) != "1" {
		t.Skipf("real FreeCAD evidence proof is opt-in; run: %s", evidenceProofGateCommand)
	}
	runtime := os.Getenv(evidenceProofRuntimeEnv)
	repo := os.Getenv(evidenceProofRepoEnv)
	if runtime == "" || !filepath.IsAbs(runtime) {
		t.Fatalf("%s must be a canonical absolute launcher path", evidenceProofRuntimeEnv)
	}
	if repo == "" || !filepath.IsAbs(repo) {
		t.Fatalf("%s must be a canonical absolute repository path", evidenceProofRepoEnv)
	}
	if info, err := os.Stat(runtime); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("launcher is not executable: %s", runtime)
	}
	for _, fixture := range []string{
		"tests/fixtures/assembly_occurrence/assembly-root.FCStd",
		"tests/fixtures/assembly_occurrence/resources/component-a.FCStd",
		"tests/fixtures/assembly_occurrence/resources/component-b.FCStd",
		"tests/fixtures/assembly_occurrence/resources/nested-assembly.FCStd",
		"tests/fixtures/fact_evidence/fact-source.FCStd",
		"tests/fixtures/fact_evidence/resources/secondary.FCStd",
	} {
		if info, err := os.Stat(filepath.Join(repo, fixture)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("FreeCAD fixture unavailable: %s", fixture)
		}
	}

	workspace := os.Getenv(evidenceProofWorkspaceEnv)
	if workspace == "" {
		workspace = t.TempDir()
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	reportPath := os.Getenv(evidenceProofReportEnv)
	if reportPath == "" {
		reportPath = filepath.Join(workspace, "evidence-proof-report.json")
	}

	c := &evidenceProofContext{
		t: t, runtime: runtime, repo: repo, workspace: workspace,
		report: evidenceProofReport{SchemaVersion: "1.0", Status: "passed", Scenarios: []evidenceProofScenario{}},
	}
	t.Cleanup(func() {
		if t.Failed() {
			c.report.Status = "failed"
			c.report.Diagnostics = &evidenceProofDiagnostics{Message: "real FreeCAD evidence proof assertion failed; see go test diagnostics"}
		}
		data, err := json.MarshalIndent(c.report, "", "  ")
		if err != nil {
			return
		}
		if writeErr := os.WriteFile(reportPath, append(data, '\n'), 0o644); writeErr != nil {
			t.Logf("write evidence proof report: %v", writeErr)
		}
	})

	version, host := c.smoke()
	c.report.FreeCADVersion = version
	c.require(host == "freecadcmd", "unexpected launcher host %q", host)

	c.record("executor_facts_and_integrity", "executor", c.executorFactsAndIntegrity())
	c.record("executor_required_unavailable_fact", "executor", c.executorRequiredUnavailableFact())
	c.record("executor_multi_outcome", "executor", c.executorMultiOutcome())
	c.record("executor_occurrence_materialization_blocker", "executor", c.executorOccurrenceMaterializationBlocker())
	c.record("fixture_complete_scope", "fixture-layout-cadruntime", c.fixtureCompleteScope())
	c.record("fixture_mixed_scope", "fixture-layout-cadruntime", c.fixtureMixedScope())
	c.record("fixture_baseline_mismatch", "fixture-layout-cadruntime", c.fixtureBaselineMismatch())

	c.report.CrossRepositoryBlocker = &evidenceProofBlocker{
		Boundary: "freecad",
		Summary:  "FreeCAD resolves external link targets from the source document's stored relative path; Engine materializes the selected source at source/<basename> and additional declared resources at resources/<hash>/<coordinate>.",
		Impact:   "A source bundle whose external links are stored as resources/<name>.FCStd cannot resolve its targets through the normal executor materialization layout, so the executor occurrence scope is explicitly incomplete. The fixture-layout cadruntime proof uses the unchanged FreeCAD fixture layout and proves complete Engine acceptance, quantity rollup, normalization, and provenance without modifying FreeCAD or the fixture.",
	}
}

func (c *evidenceProofContext) record(name, enginePath string, facts map[string]any) {
	c.t.Helper()
	c.report.Scenarios = append(c.report.Scenarios, evidenceProofScenario{Name: name, Status: "passed", EnginePath: enginePath, Facts: facts})
}

func (c *evidenceProofContext) smoke() ([]string, string) {
	c.t.Helper()
	completed := runEvidenceProofCommand(c.t, c.workspace, c.runtime, "smoke")
	var payload struct {
		Status         string   `json:"status"`
		Host           string   `json:"host"`
		FreeCADVersion []string `json:"freecadVersion"`
	}
	if err := json.Unmarshal([]byte(completed.stdout), &payload); err != nil {
		c.t.Fatalf("decode FreeCAD smoke: %v\n%s", err, completed.stdout)
	}
	c.require(payload.Status == "ok", "FreeCAD smoke status = %q", payload.Status)
	c.require(len(payload.FreeCADVersion) >= 3 && payload.FreeCADVersion[0] == "1" && payload.FreeCADVersion[1] == "1" && payload.FreeCADVersion[2] == "4",
		"FreeCAD version = %v, want 1.1.4", payload.FreeCADVersion)
	return payload.FreeCADVersion, payload.Host
}

func (c *evidenceProofContext) require(condition bool, format string, args ...any) {
	c.t.Helper()
	if !condition {
		c.t.Fatalf(format, args...)
	}
}

// --- Executor-path scenarios ------------------------------------------------

func (c *evidenceProofContext) executorFactsAndIntegrity() map[string]any {
	c.t.Helper()
	project := c.stageFactProject("facts-executor")
	request := allFactBindingsRequest(project.projectID)
	provenance := c.factProjectProvenance(project)
	run := c.executeFactProject("facts-executor-run", project, provenance, request)
	c.require(run.err == nil, "executor fact run failed: %v", run.err)
	byProperty := map[string]recordcontract.ObservationFact{}
	for _, fact := range run.facts {
		byProperty[fact.Property] = fact
	}
	c.requireFact(byProperty, "Length", "number", "25.5", "mm", recordcontract.FactAvailabilityObserved, "fact_source")
	c.requireFact(byProperty, "MeasuredLength", "number", "12.5", "mm", recordcontract.FactAvailabilityObserved, "fact_source")
	c.requireFact(byProperty, "Ratio", "number", "3.25", "", recordcontract.FactAvailabilityObserved, "fact_source")
	c.requireFact(byProperty, "Count", "integer", "42", "", recordcontract.FactAvailabilityObserved, "fact_source")
	c.requireFact(byProperty, "Enabled", "boolean", "true", "", recordcontract.FactAvailabilityObserved, "fact_source")
	c.requireFact(byProperty, "PlainText", "string", `"bounded fact string"`, "", recordcontract.FactAvailabilityObserved, "fact_source")
	c.requireFact(byProperty, "SpecialText", "", "", "", recordcontract.FactAvailabilityUnsupported, "fact_source")
	c.requireFact(byProperty, "NonFinite", "", "", "", recordcontract.FactAvailabilityUnsupported, "fact_source")
	c.requireFact(byProperty, "MissingProperty", "", "", "", recordcontract.FactAvailabilityUnavailable, "fact_source")
	c.requireFact(byProperty, "SecondaryLength", "number", "20", "mm", recordcontract.FactAvailabilityObserved, "secondary")
	for _, property := range []string{"SpecialText", "NonFinite", "MissingProperty"} {
		c.require(byProperty[property].Value.Raw == "", "%s must not substitute a value", property)
		c.require(byProperty[property].Value.Kind == "", "%s must not substitute a value kind", property)
	}

	// Integrity: every declared resource is verified, the primary persisted
	// result is independently re-hashed, and the runtime-reported digest agrees.
	c.require(run.integrity != nil && len(run.integrity.Resources) == 2, "integrity provenance is incomplete: %#v", run.integrity)
	for _, resource := range run.integrity.Resources {
		c.require(resource.MaterializationStatus == integrity.StatusVerified, "%s materialization = %s", resource.LogicalID, resource.MaterializationStatus)
		c.require(resource.RuntimeBaselineStatus == integrity.StatusVerified, "%s runtime baseline = %s", resource.LogicalID, resource.RuntimeBaselineStatus)
	}
	primary := integrityResource(run.integrity, "fact_source")
	c.require(primary != nil && primary.ResultStatus == integrity.StatusVerified, "primary persisted result is not verified: %#v", primary)
	c.require(string(primary.PersistedResultDigest) == digestFile(c.t, filepath.Join(run.attemptDir, "source/fact-source.FCStd")),
		"runtime persisted result digest disagrees with independently read bytes")

	files := c.emitExecutorPackage("facts-executor-package", run, provenance)
	c.require(packageHasFamily(files, "execution"), "package lacks execution record: %v", packageFamilies(files))
	c.require(packageHasFamily(files, "observation"), "package lacks observation record: %v", packageFamilies(files))
	c.requirePackageRaw(files, recordpackage.RawFactEvidenceContractPath())
	c.requirePackageRaw(files, recordpackage.RawRuntimeIntegrityEvidenceContractPath())
	c.requirePackageRaw(files, recordpackage.RawRuntimeResultContractPath())
	observation := decodeObservationRecord(c.t, files)
	normalized := factFacts(observation.Observation.Facts)
	c.require(len(normalized) == len(run.facts), "normalized fact count = %d, want %d", len(normalized), len(run.facts))
	c.requireFact(normalized, "Length", "number", "25.5", "mm", recordcontract.FactAvailabilityObserved, "fact_source")
	c.requireFact(normalized, "Count", "integer", "42", "", recordcontract.FactAvailabilityObserved, "fact_source")
	c.requireFact(normalized, "SecondaryLength", "number", "20", "mm", recordcontract.FactAvailabilityObserved, "secondary")
	c.require(observation.Provenance.Integrity != nil, "normalized observation lost integrity provenance")

	// Project inputs remain unchanged; only the isolated attempt copy is the
	// configured-result mutation target.
	c.require(digestFile(c.t, filepath.Join(project.root, "input/fact-source.FCStd")) == project.resourceDigests["fact_source"], "project source changed")
	c.require(digestFile(c.t, filepath.Join(project.root, "input/resources/secondary.FCStd")) == project.resourceDigests["secondary"], "project secondary source changed")

	// Repeated equivalent input preserves normalized structure and evidence
	// ordering. Persisted .FCStd bytes are deliberately not required equal.
	secondProject := c.stageFactProject("facts-executor-2")
	secondProvenance := c.factProjectProvenance(secondProject)
	second := c.executeFactProject("facts-executor-run-2", secondProject, secondProvenance, allFactBindingsRequest(secondProject.projectID))
	c.require(second.err == nil, "second executor fact run failed: %v", second.err)
	c.require(reflectFactsEqual(run.facts, second.facts), "repeated fact observations differ")
	c.require(strings.Join(factEvidenceOrder(run.rawFact), ",") == strings.Join(factEvidenceOrder(second.rawFact), ","), "repeated fact evidence ordering differs")
	firstNormalized := normalizedFactKeys(observation)
	secondFiles := c.emitExecutorPackage("facts-executor-package-2", second, secondProvenance)
	secondObservation := decodeObservationRecord(c.t, secondFiles)
	c.require(strings.Join(firstNormalized, ",") == strings.Join(normalizedFactKeys(secondObservation), ","), "repeated normalized fact ordering differs")

	return map[string]any{
		"factCount":           len(run.facts),
		"observedCount":       countAvailability(run.facts, recordcontract.FactAvailabilityObserved),
		"unavailableCount":    countAvailability(run.facts, recordcontract.FactAvailabilityUnavailable),
		"unsupportedCount":    countAvailability(run.facts, recordcontract.FactAvailabilityUnsupported),
		"integrityResources":  len(run.integrity.Resources),
		"persistedResultHash": primary.PersistedResultDigest.String(),
		"normalizedFactOrder": firstNormalized,
	}
}

func (c *evidenceProofContext) executorRequiredUnavailableFact() map[string]any {
	c.t.Helper()
	project := c.stageFactProject("required-fact")
	provenance := c.factProjectProvenance(project)
	request := recordcontract.FactRequest{
		SchemaVersion: recordcontract.FactRequestSchemaVersion,
		Bindings: []recordcontract.FactBinding{
			factBinding(project.projectID, "fact_source", "Props", "MissingProperty", recordcontract.FactValueKindNumber, "", recordcontract.FactRequirementRequired),
		},
	}
	run := c.executeFactProject("required-fact-run", project, provenance, request)
	c.require(run.err != nil, "required unavailable fact unexpectedly succeeded")
	var consumption *executor.CADRuntimeConsumptionError
	c.require(errors.As(run.err, &consumption), "required fact failure is not a consumption error: %v", run.err)
	c.require(consumption.NativeStage == "fact_evidence", "required fact stage = %q, want fact_evidence", consumption.NativeStage)
	c.require(consumption.Boundary == "execution_entrypoint", "required fact boundary = %q", consumption.Boundary)
	c.require(consumption.Category == "execution", "required fact category = %q", consumption.Category)
	c.require(consumption.Code == "runtime_failure", "required fact code = %q", consumption.Code)
	c.require(run.report.Error != nil && run.report.Error.Stage == "fact_evidence", "report stage = %#v", run.report.Error)
	c.require(run.result != nil && run.result.Status == freecad.FreeCADRuntimeResultStatusFailed && run.result.Failure != nil,
		"required fact did not preserve a runtime-native failure result")
	c.require(run.result.Failure.Stage != nil && *run.result.Failure.Stage == "fact_evidence", "native failure stage = %#v", run.result.Failure.Stage)
	c.require(!evidenceFileExists(filepath.Join(run.attemptDir, "outputs/prm.fact-evidence.json")), "failed required fact emitted fact evidence")
	files := c.emitExecutorPackage("required-fact-package", run, provenance)
	c.require(packageHasFamily(files, "failure"), "failed required fact package lacks a failure record: %v", packageFamilies(files))
	c.require(!packageHasFamily(files, "observation"), "failed required fact emitted observation")
	c.require(!packageHasFamily(files, "verification"), "failed required fact emitted verification")
	c.requirePackageRaw(files, recordpackage.RawRuntimeResultContractPath())
	return map[string]any{
		"stage":          consumption.NativeStage,
		"boundary":       consumption.Boundary,
		"category":       consumption.Category,
		"code":           consumption.Code,
		"resultStatus":   string(run.result.Status),
		"recordFamilies": packageFamilies(files),
	}
}

func (c *evidenceProofContext) executorMultiOutcome() map[string]any {
	c.t.Helper()
	project := c.stageMultiProductFactProject("multi-outcome")
	provenance := c.factProjectProvenance(project)
	if len(project.jobs) != 2 {
		c.t.Fatalf("multi-outcome plan has %d products", len(project.jobs))
	}
	runs := make([]evidenceProofFactRun, 0, 2)
	for index, j := range project.jobs {
		request := recordcontract.FactRequest{
			SchemaVersion: recordcontract.FactRequestSchemaVersion,
			Bindings: []recordcontract.FactBinding{
				factBinding(project.projectID, "fact_source", "Box", "Length", recordcontract.FactValueKindNumber, "mm", recordcontract.FactRequirementOptional),
				factBinding(project.projectID, "fact_source", "Props", "Count", recordcontract.FactValueKindInteger, "", recordcontract.FactRequirementOptional),
			},
		}
		if index == 1 {
			request.Bindings[0] = factBinding(project.projectID, "fact_source", "Props", "Ratio", recordcontract.FactValueKindNumber, "", recordcontract.FactRequirementOptional)
		}
		run := c.executePlannedFactProduct("multi-outcome-"+j.ProductKey, project, j, provenance, request)
		c.require(run.err == nil, "multi-outcome %s failed: %v", j.ProductKey, run.err)
		runs = append(runs, run)
	}
	c.require(runs[0].rawFact != nil && runs[1].rawFact != nil, "multi-outcome fact evidence missing")
	c.require(!bytesEqual(runs[0].rawFact, runs[1].rawFact), "multi-outcome evidence was adapter-side aggregated")
	packageFiles := c.emitMultiExecutorPackage("multi-outcome-package", runs, provenance)
	rawPaths := packageRawPaths(packageFiles)
	c.require(containsPath(rawPaths, "raw/outcomes/"), "multi-outcome raw evidence is not outcome scoped: %v", rawPaths)
	outcomeFactFiles := 0
	for _, path := range rawPaths {
		if strings.HasSuffix(path, "fact/prm.fact-evidence.json") {
			outcomeFactFiles++
		}
	}
	c.require(outcomeFactFiles == 2, "expected two outcome-scoped fact evidence files, found %d", outcomeFactFiles)
	observationCount := countObservationRecords(packageFiles)
	c.require(observationCount == 2, "expected two outcome observation records, found %d", observationCount)
	return map[string]any{
		"outcomes":            len(runs),
		"distinctRawEvidence": !bytesEqual(runs[0].rawFact, runs[1].rawFact),
		"outcomeScopedRaw":    outcomeFactFiles,
		"observationRecords":  observationCount,
	}
}

func (c *evidenceProofContext) executorOccurrenceMaterializationBlocker() map[string]any {
	c.t.Helper()
	project := c.stageAssemblyExecutorProject("occurrence-executor")
	provenance := occurrenceProjectProvenance(project)
	j := project.jobs[0]
	runRoot := filepath.Join(c.workspace, "occurrence-executor-run")
	productDir := filepath.Join(runRoot, "products", j.ProductKey)
	if err := os.MkdirAll(productDir, 0o700); err != nil {
		c.t.Fatal(err)
	}
	pkg, err := handoff.FromJob(j)
	if err != nil {
		c.t.Fatalf("handoff package: %v", err)
	}
	aligned, err := cadruntime.NewFreeCADRuntimeAdapter(freecad.NewFreeCADAdapter(productDir), runtimecap.NewExternalCapability())
	if err != nil {
		c.t.Fatal(err)
	}
	resolver, err := runtimecap.NewExecutableResolver(map[string]string{"freecad": c.runtime})
	if err != nil {
		c.t.Fatal(err)
	}
	e := executor.NewWithCADRuntimeConsumption(aligned, resolver, productDir)
	e.SetResourceContext(project.planned.ResourceContexts[j.ProductKey])
	e.SetSourceIntegrity(provenance)
	e.SetIntegrityRequestDir(runRoot)
	request := supportedOccurrenceRequest(project.projectID, "optional")
	if err := e.ExecutePackageWithOptions(context.Background(), pkg, executor.PackageExecutionOptions{OccurrenceObservation: &request}); err != nil {
		c.t.Fatalf("executor occurrence scenario failed unexpectedly: %v", err)
	}
	outcome := e.CADRuntimeOutcome()
	c.require(outcome != nil && outcome.OccurrenceRequest != nil && len(outcome.OccurrenceJSON) > 0, "executor occurrence evidence missing")
	summary, err := cadruntime.MapOccurrenceEvidence(cadruntime.OccurrenceMappingInput{
		Request: outcome.OccurrenceRequest, Resources: outcome.Resources,
		ExpectedSourceDocument: outcome.SourceDocument, Content: outcome.OccurrenceJSON,
	})
	c.require(err == nil, "executor occurrence evidence intake failed: %v", err)
	c.require(summary.Completeness == "incomplete", "executor occurrence completeness = %q, want incomplete", summary.Completeness)
	c.require(len(summary.Occurrences) == 0, "executor occurrence unexpectedly accepted %d occurrences", len(summary.Occurrences))
	c.require(summary.Quantities == nil, "executor incomplete occurrence scope fabricated quantities")
	decoded, err := cadruntime.DecodeOccurrenceEvidence(outcome.OccurrenceJSON)
	c.require(err == nil, "strict executor occurrence decode failed: %v", err)
	c.require(len(decoded.Members) == 10, "executor occurrence member count = %d", len(decoded.Members))
	unresolved := 0
	for _, member := range decoded.Members {
		if member.Classification == "unresolved" {
			unresolved++
		}
	}
	c.require(unresolved == 10, "executor unresolved member count = %d", unresolved)
	return map[string]any{
		"completeness":        summary.Completeness,
		"acceptedOccurrences": 0,
		"unresolvedMembers":   unresolved,
		"sourceDocument":      outcome.SourceDocument,
		"blocker":             "external link targets are stored relative to the source document, while Engine materializes the source at source/<basename>",
	}
}

func occurrenceProjectProvenance(project *evidenceProofProject) *integrity.ProjectIntegrity {
	sources := []integrity.ResourceSource{}
	for _, binding := range project.planned.ResourceContexts[project.jobs[0].ProductKey].Inventory().Bindings() {
		digest, err := integrity.ParseDigest(binding.SourceDigest)
		if err != nil {
			continue
		}
		sources = append(sources, integrity.ResourceSource{
			LogicalID: binding.LogicalID, SourceCoordinate: binding.SourceCoordinate,
			SourceDigest: digest, LockState: integrity.LockStateVerified, LockedDigest: digest,
		})
	}
	return &integrity.ProjectIntegrity{Lock: integrity.LockStateVerified, Resources: sources}
}

func (c *evidenceProofContext) stageAssemblyExecutorProject(name string) *evidenceProofProject {
	c.t.Helper()
	root := filepath.Join(c.workspace, name, "project")
	if err := os.MkdirAll(filepath.Join(root, "input/resources"), 0o755); err != nil {
		c.t.Fatal(err)
	}
	copyEvidenceProofFile(c.t, filepath.Join(c.repo, "tests/fixtures/assembly_occurrence/assembly-root.FCStd"), filepath.Join(root, "input/assembly-root.FCStd"))
	for _, component := range []string{"component-a", "component-b", "nested-assembly"} {
		copyEvidenceProofFile(c.t, filepath.Join(c.repo, "tests/fixtures/assembly_occurrence/resources", component+".FCStd"), filepath.Join(root, "input/resources", component+".FCStd"))
	}
	c.writeJSON(filepath.Join(root, "prm.project.json"), map[string]any{
		"version": "1.0", "projectId": "evidence-occurrence", "dsl": "occurrence.project.dsl",
		"resources": map[string]any{"models": map[string]string{
			"assembly_root":   "input/assembly-root.FCStd",
			"component_a":     "input/resources/component-a.FCStd",
			"component_b":     "input/resources/component-b.FCStd",
			"nested_assembly": "input/resources/nested-assembly.FCStd",
		}},
	})
	c.writeFile(filepath.Join(root, "occurrence.project.dsl"), withVersionHeader(`
product AssemblyWidget {
  adapter = "freecad"
  source_model = "assembly_root"
  outputs = ["none"]
}
`))
	if _, err := executeHarnessCommand(c.t, []string{"sync", "--file", root}); err != nil {
		c.t.Fatalf("sync assembly evidence project: %v", err)
	}
	planned, err := loadPlannedRun(root, map[string]string{}, nil)
	if err != nil {
		c.t.Fatalf("load planned run: %v", err)
	}
	jobs, err := job.SplitPlan(planned.Plan)
	if err != nil {
		c.t.Fatalf("split plan: %v", err)
	}
	sort.SliceStable(jobs, func(i, k int) bool { return jobs[i].ProductKey < jobs[k].ProductKey })
	return &evidenceProofProject{
		root: root, projectID: "evidence-occurrence", planned: planned, jobs: jobs,
		resourceDigests: map[string]string{
			"fact_source": digestFile(c.t, filepath.Join(root, "input/assembly-root.FCStd")),
			"secondary":   digestFile(c.t, filepath.Join(root, "input/resources/component-a.FCStd")),
		},
	}
}

// --- Fixture-layout cadruntime scenarios ------------------------------------

func (c *evidenceProofContext) fixtureCompleteScope() map[string]any {
	c.t.Helper()
	bundle := c.stageAssemblyBundle("complete", filepath.Join(c.workspace, "relocated", "deep", "attempt"), nil)
	request := supportedOccurrenceRequest(bundle.projectID, "complete")
	factRequest := recordcontract.FactRequest{
		SchemaVersion: recordcontract.FactRequestSchemaVersion,
		Bindings: []recordcontract.FactBinding{
			factBinding(bundle.projectID, "assembly_root", "Link01", "Label", recordcontract.FactValueKindString, "", recordcontract.FactRequirementOptional),
		},
	}
	c.writeOccurrenceRequest(bundle, &request)
	c.writeFactRequest(bundle, &factRequest)
	result := c.invokeFixtureBundle(bundle)
	c.require(result != nil && result.Status == freecad.FreeCADRuntimeResultStatusSucceeded, "fixture complete run result = %#v", result)

	occurrenceRaw, decoded, summary := c.acceptOccurrence(bundle, request)
	c.require(decoded.SchemaVersion == cadruntime.OccurrenceEvidenceSchemaVersion, "occurrence evidence schema = %q", decoded.SchemaVersion)
	c.require(decoded.FreeCADVersion == "1.1.4", "occurrence evidence FreeCAD version = %q", decoded.FreeCADVersion)
	c.require(decoded.Completeness == "complete", "occurrence completeness = %q", decoded.Completeness)
	c.require(decoded.InspectionCompleted, "occurrence inspection did not complete")
	c.require(len(decoded.Group) == 10, "occurrence group size = %d", len(decoded.Group))
	c.require(summary.Completeness == "complete", "normalized completeness = %q", summary.Completeness)
	c.require(len(summary.Occurrences) == 10, "normalized occurrence count = %d", len(summary.Occurrences))
	c.require(len(summary.Quantities) == 1, "quantity group count = %d", len(summary.Quantities))
	quantity := summary.Quantities[0]
	c.require(quantity.Quantity == 10, "quantity = %d, want 10", quantity.Quantity)
	c.require(quantity.Target.Resource.LogicalID == "component_a" && quantity.Target.Name == "Body", "quantity target = %#v", quantity.Target)
	members := []string{}
	for _, occurrence := range summary.Occurrences {
		members = append(members, occurrence.Member)
		c.require(occurrence.Target.Resource.LogicalID == "component_a" && occurrence.Target.Name == "Body", "occurrence target = %#v", occurrence.Target)
	}
	c.require(strings.Join(members, ",") == strings.Join(sortedStrings([]string{"Link01", "Link02", "Link03", "Link04", "Link05", "Link06", "Link07", "Link08", "Link09", "Link10"}), ","),
		"normalized occurrence ordering = %v", members)
	for _, member := range decoded.Members {
		c.require(member.Classification == "occurrence", "member %s classification = %q", member.Name, member.Classification)
	}
	// Ordinary PropertyLinkList references are not Assembly Group members and
	// therefore contribute neither occurrences nor quantity.
	c.require(!containsEvidenceString(decoded.Group, "InternalBody") && !containsEvidenceString(decoded.Group, "Holder"), "generic references leaked into the occurrence scope")

	factRaw, facts := c.acceptFact(bundle, factRequest)
	c.require(len(facts) == 1 && facts[0].Property == "Label" && facts[0].Value.Raw == `"Link01"` && facts[0].Unit == "", "fact evidence = %#v", facts)
	c.require(facts[0].Availability == recordcontract.FactAvailabilityObserved, "fact availability = %q", facts[0].Availability)

	integrityRaw, operation := c.acceptIntegrity(bundle, result)
	primary := integrityResource(operation, "assembly_root")
	c.require(primary != nil && primary.ResultStatus == integrity.StatusVerified, "primary integrity = %#v", primary)
	c.require(string(primary.PersistedResultDigest) == digestFile(c.t, filepath.Join(bundle.root, "assembly-root.FCStd")), "independent persisted hash disagrees")
	missing := integrityResource(operation, "missing_component")
	c.require(missing != nil && missing.RuntimeBaselineStatus == integrity.StatusMissing, "missing component status = %#v", missing)

	files := c.emitFixturePackage("complete-package", bundle, request, occurrenceRaw, factRequest, factRaw, integrityRaw, operation)
	c.require(packageHasFamily(files, "execution"), "package lacks execution record: %v", packageFamilies(files))
	c.require(packageHasFamily(files, "observation"), "package lacks observation record: %v", packageFamilies(files))
	c.requirePackageRaw(files, recordpackage.RawRuntimeOccurrenceEvidenceContractPath())
	c.requirePackageRaw(files, recordpackage.RawFactEvidenceContractPath())
	c.requirePackageRaw(files, recordpackage.RawRuntimeIntegrityEvidenceContractPath())
	c.requirePackageRaw(files, recordpackage.RawRuntimeResultContractPath())
	observation := decodeObservationRecord(c.t, files)
	c.require(observation.Observation.Occurrences != nil, "normalized package lost occurrence summary")
	c.require(observation.Observation.Occurrences.Completeness == "complete" &&
		len(observation.Observation.Occurrences.Quantities) == 1 &&
		observation.Observation.Occurrences.Quantities[0].Quantity == 10,
		"normalized package quantity mismatch: %#v", observation.Observation.Occurrences)
	c.require(len(observation.Observation.Facts) == 1 && observation.Observation.Facts[0].Property == "Label", "normalized package lost the bound fact")
	c.require(observation.Provenance.Integrity != nil, "normalized package lost integrity provenance")

	// Relocation: the same fixture material copied to a second arbitrary nested
	// path must preserve durable logical and coordinate identity. Raw evidence
	// bytes are stable because the runtime emits coordinates, not host paths.
	relocated := c.stageAssemblyBundle("relocated", filepath.Join(c.workspace, "second", "arbitrary", "nested", "attempt"), nil)
	c.writeOccurrenceRequest(relocated, &request)
	c.writeFactRequest(relocated, &factRequest)
	secondResult := c.invokeFixtureBundle(relocated)
	c.require(secondResult != nil && secondResult.Status == freecad.FreeCADRuntimeResultStatusSucceeded, "relocated fixture run result = %#v", secondResult)
	secondOccurrenceRaw, _, secondSummary := c.acceptOccurrence(relocated, request)
	secondFactRaw, secondFacts := c.acceptFact(relocated, factRequest)
	secondIntegrityRaw, secondOperation := c.acceptIntegrity(relocated, secondResult)
	c.require(bytesEqual(occurrenceRaw, secondOccurrenceRaw), "relocated occurrence evidence bytes differ")
	c.require(bytesEqual(factRaw, secondFactRaw), "relocated fact evidence bytes differ")
	c.require(reflectOccurrenceEqual(summary, secondSummary), "relocated occurrence summary differs")
	c.require(reflectFactsEqual(facts, secondFacts), "relocated fact summary differs")
	c.require(integrityCoordinates(operation) == integrityCoordinates(secondOperation), "relocated integrity coordinates differ")
	_ = c.emitFixturePackage("relocated-package", relocated, request, secondOccurrenceRaw, factRequest, secondFactRaw, secondIntegrityRaw, secondOperation)

	c.require(digestFile(c.t, filepath.Join(c.repo, "tests/fixtures/assembly_occurrence/assembly-root.FCStd")) == bundle.fixtureSourceDigest, "authoritative FreeCAD fixture changed")

	return map[string]any{
		"occurrences":        len(summary.Occurrences),
		"quantity":           quantity.Quantity,
		"quantityTarget":     quantity.Target.Resource.LogicalID + "/" + quantity.Target.Name,
		"factProperty":       facts[0].Property,
		"integrityResources": len(operation.Resources),
		"missingStatus":      missing.RuntimeBaselineStatus,
		"relocatedEvidence":  "occurrence and fact bytes stable",
		"normalizedFamilies": packageFamilies(files),
	}
}

func (c *evidenceProofContext) fixtureMixedScope() map[string]any {
	c.t.Helper()
	bundle := c.stageAssemblyBundle("mixed", filepath.Join(c.workspace, "mixed", "arbitrary", "attempt"), nil)
	request := supportedOccurrenceRequest(bundle.projectID, "optional")
	request.Scope.Parent.Name = "Assembly"
	c.writeOccurrenceRequest(bundle, &request)
	result := c.invokeFixtureBundle(bundle)
	c.require(result != nil && result.Status == freecad.FreeCADRuntimeResultStatusSucceeded, "mixed fixture run result = %#v", result)
	_, decoded, summary := c.acceptOccurrence(bundle, request)
	c.require(decoded.Completeness == "incomplete", "mixed completeness = %q", decoded.Completeness)
	c.require(decoded.InspectionCompleted, "mixed inspection did not complete")
	c.require(len(decoded.Diagnostics) > 0, "mixed scope has no diagnostics")
	c.require(summary.Completeness == "incomplete", "normalized mixed completeness = %q", summary.Completeness)
	c.require(summary.Quantities == nil, "incomplete scope fabricated quantities: %#v", summary.Quantities)

	members := map[string]cadruntime.OccurrenceRuntimeMember{}
	for _, member := range decoded.Members {
		members[member.Name] = member
	}
	c.require(members["SharedSourceA"].Classification == "occurrence" && members["SharedSourceA"].Target != nil &&
		members["SharedSourceA"].Target.Entity.Resource.LogicalID == "component_a", "SharedSourceA = %#v", members["SharedSourceA"])
	c.require(members["SharedSourceB"].Classification == "occurrence" && members["SharedSourceB"].Target != nil &&
		members["SharedSourceB"].Target.Entity.Resource.LogicalID == "component_a", "SharedSourceB = %#v", members["SharedSourceB"])
	c.require(members["SharedSourceA"].Target.Entity.Resource.LogicalID == members["SharedSourceB"].Target.Entity.Resource.LogicalID, "shared target resource identity differs")
	c.require(members["SharedSourceA"].Name != members["SharedSourceB"].Name, "shared target members collapsed")
	c.require(members["CrossResourceBody"].Classification == "occurrence" && members["CrossResourceBody"].Target != nil &&
		members["CrossResourceBody"].Target.Entity.Resource.LogicalID == "component_b", "CrossResourceBody = %#v", members["CrossResourceBody"])
	c.require(members["CrossResourceBody"].Target.Entity.Name == members["SharedSourceA"].Target.Entity.Name, "duplicate native name fixture changed")
	c.require(members["CrossResourceBody"].Target.Entity.Resource.LogicalID != members["SharedSourceA"].Target.Entity.Resource.LogicalID, "duplicate native names were not separated by declared resource identity")
	c.require(members["SharedSourceA"].Target.Document == "resources/component-a.FCStd" &&
		members["SharedSourceB"].Target.Document == "resources/component-a.FCStd" &&
		members["CrossResourceBody"].Target.Document == "resources/component-b.FCStd",
		"duplicate native names were not separated by materialized coordinates: %q %q %q",
		members["SharedSourceA"].Target.Document, members["SharedSourceB"].Target.Document, members["CrossResourceBody"].Target.Document)
	c.require(members["PartLink"].Classification == "occurrence" && members["PartLink"].Target != nil && members["PartLink"].Target.NativeType == "App::Part", "PartLink = %#v", members["PartLink"])
	c.require(members["MarkerLink"].Classification == "unsupported" && members["MarkerLink"].Target == nil && members["MarkerLink"].Diagnostic != "", "MarkerLink = %#v", members["MarkerLink"])
	c.require(members["NestedLink"].Classification == "unsupported" && strings.Contains(members["NestedLink"].Diagnostic, "AssemblyLink"), "NestedLink = %#v", members["NestedLink"])
	c.require(members["ArrayLink"].Classification == "unsupported" && members["ArrayLink"].ElementCount != nil && *members["ArrayLink"].ElementCount == 1, "ArrayLink = %#v", members["ArrayLink"])
	c.require(members["MissingTarget"].Classification == "unresolved" && members["MissingTarget"].NativeStateValid != nil && !*members["MissingTarget"].NativeStateValid, "MissingTarget = %#v", members["MissingTarget"])
	c.require(members["Joints"].Classification == "auxiliary" && members["Joints"].Resolution == "resolved" && members["Joints"].Contents != nil &&
		members["Joints"].Contents.InspectionCompleted && len(members["Joints"].Contents.Group) == 0, "Joints = %#v", members["Joints"])
	c.require(!containsEvidenceString(decoded.Group, "InternalBody") && !containsEvidenceString(decoded.Group, "Holder"), "generic references leaked into the mixed scope")
	c.require(len(summary.Occurrences) == 4, "mixed normalized occurrence count = %d", len(summary.Occurrences))
	byMember := map[string]recordcontract.Occurrence{}
	for _, occurrence := range summary.Occurrences {
		byMember[occurrence.Member] = occurrence
	}
	c.require(byMember["SharedSourceA"].Target.Resource.LogicalID == "component_a" && byMember["SharedSourceB"].Target.Resource.LogicalID == "component_a", "shared target normalized identity mismatch")
	c.require(byMember["CrossResourceBody"].Target.Resource.LogicalID == "component_b" && byMember["CrossResourceBody"].Target.Name == "Body", "duplicate name normalized identity mismatch")

	return map[string]any{
		"completeness":        decoded.Completeness,
		"diagnostics":         len(decoded.Diagnostics),
		"acceptedOccurrences": len(summary.Occurrences),
		"quantities":          summary.Quantities,
		"sharedTarget":        "component_a/Body",
		"duplicateNativeName": "Body",
		"unsupported":         []string{"MarkerLink", "NestedLink", "ArrayLink"},
		"unresolved":          []string{"MissingTarget"},
		"auxiliary":           []string{"Joints"},
	}
}

func (c *evidenceProofContext) fixtureBaselineMismatch() map[string]any {
	c.t.Helper()
	bundle := c.stageAssemblyBundle("baseline-mismatch", filepath.Join(c.workspace, "mismatch", "arbitrary", "attempt"), map[string]string{
		"assembly_root": strings.Repeat("0", 64),
	})
	request := supportedOccurrenceRequest(bundle.projectID, "complete")
	c.writeOccurrenceRequest(bundle, &request)
	sourceBefore := bundle.sourceDigest
	result, invokeErr := c.invokeFixtureBundleAllowFailure(bundle)
	c.require(invokeErr != nil, "baseline mismatch unexpectedly succeeded")
	c.require(result != nil && result.Status == freecad.FreeCADRuntimeResultStatusFailed && result.Failure != nil, "baseline mismatch result = %#v", result)
	c.require(result.Failure.Stage != nil && *result.Failure.Stage == "integrity_baseline", "baseline mismatch stage = %#v", result.Failure.Stage)
	c.require(digestFile(c.t, filepath.Join(bundle.root, "assembly-root.FCStd")) == sourceBefore, "baseline mismatch mutated the source")
	c.require(!evidenceFileExists(filepath.Join(bundle.outputDir, cadruntime.OccurrenceEvidenceFilename)), "baseline mismatch emitted occurrence evidence")
	c.require(digestFile(c.t, filepath.Join(c.repo, "tests/fixtures/assembly_occurrence/assembly-root.FCStd")) == bundle.fixtureSourceDigest,
		"baseline mismatch altered the authoritative FreeCAD fixture")
	return map[string]any{
		"stage":           *result.Failure.Stage,
		"sourceUnchanged": true,
		"evidenceBlocked": true,
	}
}

// --- Fact project staging and executor helpers ------------------------------

type evidenceProofProject struct {
	root            string
	projectID       string
	resourceDigests map[string]string
	planned         *plannedRun
	jobs            []*job.Job
}

type evidenceProofFactRun struct {
	productKey string
	job        *job.Job
	attemptDir string
	request    recordcontract.FactRequest
	facts      []recordcontract.ObservationFact
	rawFact    []byte
	integrity  *integrity.OperationIntegrity
	result     *freecad.FreeCADRuntimeResult
	report     report.Report
	err        error
	states     []executor.ExecutionState
	outcome    *executor.CADRuntimeOutcome
}

func (c *evidenceProofContext) stageFactProject(name string) *evidenceProofProject {
	c.t.Helper()
	return c.stageEvidenceProject(name,
		"evidence-fact",
		map[string]string{
			"fact_source": "input/fact-source.FCStd",
			"secondary":   "input/resources/secondary.FCStd",
		},
		"fact.project.dsl",
		`product FactWidget {
  adapter = "freecad"
  source_model = "fact_source"
  outputs = ["none"]
}
`)
}

func (c *evidenceProofContext) stageMultiProductFactProject(name string) *evidenceProofProject {
	c.t.Helper()
	return c.stageEvidenceProject(name,
		"evidence-multi",
		map[string]string{
			"fact_source": "input/fact-source.FCStd",
			"secondary":   "input/resources/secondary.FCStd",
		},
		"multi.project.dsl",
		`product FactWidgetA {
  adapter = "freecad"
  source_model = "fact_source"
  outputs = ["none"]
}

product FactWidgetB {
  adapter = "freecad"
  source_model = "fact_source"
  outputs = ["none"]
}
`)
}

func (c *evidenceProofContext) stageEvidenceProject(directory, projectID string, models map[string]string, dslName, dsl string) *evidenceProofProject {
	c.t.Helper()
	root := filepath.Join(c.workspace, directory, "project")
	if err := os.MkdirAll(filepath.Join(root, "input/resources"), 0o755); err != nil {
		c.t.Fatal(err)
	}
	copyEvidenceProofFile(c.t, filepath.Join(c.repo, "tests/fixtures/fact_evidence/fact-source.FCStd"), filepath.Join(root, "input/fact-source.FCStd"))
	copyEvidenceProofFile(c.t, filepath.Join(c.repo, "tests/fixtures/fact_evidence/resources/secondary.FCStd"), filepath.Join(root, "input/resources/secondary.FCStd"))
	c.writeJSON(filepath.Join(root, "prm.project.json"), map[string]any{
		"version": "1.0", "projectId": projectID, "dsl": dslName,
		"resources": map[string]any{"models": models},
	})
	c.writeFile(filepath.Join(root, dslName), withVersionHeader("\n"+dsl))
	if _, err := executeHarnessCommand(c.t, []string{"sync", "--file", root}); err != nil {
		c.t.Fatalf("sync evidence proof project: %v", err)
	}
	planned, err := loadPlannedRun(root, map[string]string{}, nil)
	if err != nil {
		c.t.Fatalf("load planned run: %v", err)
	}
	jobs, err := job.SplitPlan(planned.Plan)
	if err != nil {
		c.t.Fatalf("split plan: %v", err)
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].ProductKey < jobs[j].ProductKey })
	return &evidenceProofProject{
		root: root, projectID: projectID, planned: planned, jobs: jobs,
		resourceDigests: map[string]string{
			"fact_source": digestFile(c.t, filepath.Join(root, "input/fact-source.FCStd")),
			"secondary":   digestFile(c.t, filepath.Join(root, "input/resources/secondary.FCStd")),
		},
	}
}

func (c *evidenceProofContext) factProjectProvenance(project *evidenceProofProject) *integrity.ProjectIntegrity {
	sources := []integrity.ResourceSource{}
	for _, logical := range []string{"fact_source", "secondary"} {
		digest, err := integrity.ParseDigest(project.resourceDigests[logical])
		c.require(err == nil, "parse %s digest: %v", logical, err)
		coordinate := "input/fact-source.FCStd"
		if logical == "secondary" {
			coordinate = "input/resources/secondary.FCStd"
		}
		sources = append(sources, integrity.ResourceSource{
			LogicalID: logical, SourceCoordinate: coordinate, SourceDigest: digest,
			LockState: integrity.LockStateVerified, LockedDigest: digest,
		})
	}
	return &integrity.ProjectIntegrity{Lock: integrity.LockStateVerified, Resources: sources}
}

func (c *evidenceProofContext) executeFactProject(name string, project *evidenceProofProject, provenance *integrity.ProjectIntegrity, request recordcontract.FactRequest) evidenceProofFactRun {
	c.t.Helper()
	return c.executePlannedFactProduct(name, project, project.jobs[0], provenance, request)
}

func (c *evidenceProofContext) executePlannedFactProduct(name string, project *evidenceProofProject, j *job.Job, provenance *integrity.ProjectIntegrity, request recordcontract.FactRequest) evidenceProofFactRun {
	c.t.Helper()
	runRoot := filepath.Join(c.workspace, name)
	productDir := filepath.Join(runRoot, "products", j.ProductKey)
	if err := os.MkdirAll(productDir, 0o700); err != nil {
		c.t.Fatal(err)
	}
	pkg, err := handoff.FromJob(j)
	if err != nil {
		c.t.Fatalf("handoff package: %v", err)
	}
	aligned, err := cadruntime.NewFreeCADRuntimeAdapter(freecad.NewFreeCADAdapter(productDir), runtimecap.NewExternalCapability())
	if err != nil {
		c.t.Fatal(err)
	}
	resolver, err := runtimecap.NewExecutableResolver(map[string]string{"freecad": c.runtime})
	if err != nil {
		c.t.Fatal(err)
	}
	e := executor.NewWithCADRuntimeConsumption(aligned, resolver, productDir)
	e.SetResourceContext(project.planned.ResourceContexts[j.ProductKey])
	e.SetSourceIntegrity(provenance)
	e.SetIntegrityRequestDir(runRoot)
	err = e.ExecutePackageWithOptions(context.Background(), pkg, executor.PackageExecutionOptions{FactObservation: recordcontract.CloneFactRequest(&request)})
	run := evidenceProofFactRun{productKey: j.ProductKey, job: j, request: request, err: err, states: e.States(), outcome: e.CADRuntimeOutcome()}
	if run.outcome != nil {
		run.attemptDir = run.outcome.WorkingCopyDir
		run.integrity = run.outcome.Integrity
		if len(run.outcome.FactJSON) > 0 {
			run.rawFact = append([]byte(nil), run.outcome.FactJSON...)
			if facts, mapErr := cadruntime.MapFactEvidence(cadruntime.FactMappingInput{
				Request: run.outcome.FactRequest, Resources: run.outcome.Resources,
				ExpectedSourceDocument: run.outcome.SourceDocument, Content: run.outcome.FactJSON,
			}); mapErr == nil {
				run.facts = facts
			} else if err == nil {
				err = mapErr
				run.err = err
			}
		}
		if run.outcome.ResultPath != "" {
			if parsed, parseErr := freecad.LoadFreeCADRuntimeResult(run.outcome.ResultPath); parseErr == nil {
				run.result = parsed
			}
		}
	}
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rep, reportErr := report.Build(report.BuildInput{
		PlanHash: project.planned.PlanHash, DSLHash: project.planned.DSLHash,
		Execution: scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{Job: j, States: run.states, Err: run.err, CADRuntimeOutcome: run.outcome}}},
		StartedAt: stamp, EndedAt: stamp, WorkerCount: 1,
		StepTimeoutSeconds: int(executor.DefaultStepTimeout / time.Second), MaxRetries: executor.DefaultMaxRetries,
		Err: run.err,
	})
	if reportErr == nil {
		run.report = rep
	}
	return run
}

// --- Fixture bundle helper --------------------------------------------------

type evidenceProofBundle struct {
	root                 string
	projectID            string
	inventory            resourcebinding.Inventory
	layout               freecad.FreeCADRuntimeWorkingCopyLayout
	attempt              freecad.FreeCADRuntimeAttempt
	integrityRequestJSON []byte
	integrityRequestPath string
	manifestPath         string
	resultPath           string
	outputDir            string
	sourceDigest         string
	fixtureSourceDigest  string
}

func (c *evidenceProofContext) stageAssemblyBundle(name, dest string, baselineOverrides map[string]string) *evidenceProofBundle {
	c.t.Helper()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		c.t.Fatal(err)
	}
	copyEvidenceProofTree(c.t, filepath.Join(c.repo, "tests/fixtures/assembly_occurrence"), dest)
	outputDir := filepath.Join(dest, "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		c.t.Fatal(err)
	}
	projectID := "evidence-assembly"
	coordinates := map[string]string{
		"assembly_root":     "assembly-root.FCStd",
		"component_a":       "resources/component-a.FCStd",
		"component_b":       "resources/component-b.FCStd",
		"missing_component": "resources/missing-component.FCStd",
		"nested_assembly":   "resources/nested-assembly.FCStd",
	}
	bindings := make([]resourcebinding.Binding, 0, len(coordinates))
	for logical, coordinate := range coordinates {
		digest := strings.Repeat("0", 64)
		if data, err := os.ReadFile(filepath.Join(dest, coordinate)); err == nil {
			digest = integrity.NewDigest(data).String()
		}
		bindings = append(bindings, resourcebinding.Binding{
			ProjectID: projectID, Kind: resourcebinding.ResourceKindModel, LogicalID: logical,
			SourceCoordinate: coordinate, SourceDigest: digest, MaterializedCoordinate: coordinate,
		})
	}
	inventory, err := resourcebinding.New(bindings)
	if err != nil {
		c.t.Fatal(err)
	}
	layout := freecad.FreeCADRuntimeWorkingCopyLayout{
		ProductDir: dest, WorkingRoot: dest, WorkingCopyDir: dest, WorkingCopyID: "fixture-attempt",
		SourceDir: dest, SourceDocument: "assembly-root.FCStd",
		SourceDocumentPath:    filepath.Join(dest, "assembly-root.FCStd"),
		ManifestPath:          filepath.Join(dest, "prm.export-manifest.json"),
		ResultPath:            filepath.Join(dest, "prm.result.json"),
		OutputDir:             outputDir,
		OutputDirRelativePath: "outputs",
	}
	attempt := freecad.FreeCADRuntimeAttempt{
		Resources:  inventory,
		Identity:   freecad.FreeCADRuntimeAttemptIdentity{ID: "fixture-attempt", JobID: "evidence-job", ProductKey: "assembly", StepID: "0", Attempt: 1, Adapter: "freecad", PlanHash: "evidence-plan"},
		SourcePath: layout.SourceDocumentPath, Layout: layout,
	}
	baselines := make([]freecad.FreeCADRuntimeMaterializedBaseline, 0, len(inventory.Bindings()))
	for _, binding := range inventory.Bindings() {
		path := filepath.Join(dest, filepath.FromSlash(binding.MaterializedCoordinate))
		digest, err := integrity.ParseDigest(binding.SourceDigest)
		if err != nil {
			c.t.Fatalf("parse baseline digest: %v", err)
		}
		if override, ok := baselineOverrides[binding.LogicalID]; ok {
			digest, err = integrity.ParseDigest(override)
			if err != nil {
				c.t.Fatalf("parse baseline override: %v", err)
			}
		} else if data, readErr := os.ReadFile(path); readErr == nil {
			c.require(integrity.NewDigest(data) == digest, "%s copied source identity disagrees with its baseline", binding.LogicalID)
		}
		baselines = append(baselines, freecad.FreeCADRuntimeMaterializedBaseline{
			Binding: binding, Coordinate: binding.MaterializedCoordinate, Path: path, Digest: digest,
		})
	}
	_, data, err := cadruntime.ComposeFreeCADIntegrityRequest(baselines, layout.SourceDocument)
	if err != nil {
		c.t.Fatalf("compose integrity request: %v", err)
	}
	requestPath, err := cadruntime.WriteFreeCADIntegrityRequest(dest, attempt.Identity.ID, data)
	if err != nil {
		c.t.Fatalf("write integrity request: %v", err)
	}
	manifest := &freecad.FreeCADRuntimeExportManifest{
		SchemaVersion: "1.0", SourceDocument: layout.SourceDocument,
		ParameterAssignments: []freecad.FreeCADRuntimeParameterAssignment{},
		Outputs:              []freecad.FreeCADRuntimeManifestOutput{},
	}
	manifestJSON, err := freecad.MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := os.WriteFile(layout.ManifestPath, manifestJSON, 0o644); err != nil {
		c.t.Fatal(err)
	}
	return &evidenceProofBundle{
		root: dest, projectID: projectID, inventory: inventory, layout: layout, attempt: attempt,
		integrityRequestJSON: data, integrityRequestPath: requestPath,
		manifestPath: layout.ManifestPath, resultPath: layout.ResultPath, outputDir: outputDir,
		sourceDigest:        digestFile(c.t, layout.SourceDocumentPath),
		fixtureSourceDigest: digestFile(c.t, filepath.Join(c.repo, "tests/fixtures/assembly_occurrence/assembly-root.FCStd")),
	}
}

func (c *evidenceProofContext) writeOccurrenceRequest(bundle *evidenceProofBundle, request *recordcontract.OccurrenceRequest) {
	c.t.Helper()
	content, err := cadruntime.ComposeOccurrenceRequestJSON(request)
	c.require(err == nil, "compose occurrence request: %v", err)
	c.require(len(content) > 0, "Engine produced no occurrence request bytes")
	if err := os.WriteFile(filepath.Join(bundle.root, cadruntime.OccurrenceRequestFilename), content, 0o644); err != nil {
		c.t.Fatal(err)
	}
}

func (c *evidenceProofContext) writeFactRequest(bundle *evidenceProofBundle, request *recordcontract.FactRequest) {
	c.t.Helper()
	content, err := cadruntime.ComposeFactRequestJSON(request)
	c.require(err == nil, "compose fact request: %v", err)
	c.require(len(content) > 0, "Engine produced no fact request bytes")
	if err := os.WriteFile(filepath.Join(bundle.root, cadruntime.FactRequestFilename), content, 0o644); err != nil {
		c.t.Fatal(err)
	}
}

func (c *evidenceProofContext) invokeFixtureBundle(bundle *evidenceProofBundle) *freecad.FreeCADRuntimeResult {
	c.t.Helper()
	result, err := c.invokeFixtureBundleAllowFailure(bundle)
	c.require(err == nil, "fixture runtime invocation failed: %v", err)
	c.require(result != nil, "fixture runtime produced no result")
	return result
}

func (c *evidenceProofContext) invokeFixtureBundleAllowFailure(bundle *evidenceProofBundle) (*freecad.FreeCADRuntimeResult, error) {
	c.t.Helper()
	_, invokeErr := freecad.InvokeFreeCADRuntime(runtimecap.NewExternalCapability(), context.Background(), freecad.FreeCADRuntimeExecutionRequest{
		RuntimeCommand: c.runtime, WorkingCopyDir: bundle.root, ManifestPath: bundle.manifestPath,
		ResultPath: bundle.resultPath, OutputDir: bundle.outputDir, IntegrityRequestPath: bundle.integrityRequestPath,
	})
	result, loadErr := freecad.LoadFreeCADRuntimeResult(bundle.resultPath)
	c.require(loadErr == nil, "read fixture runtime result: %v", loadErr)
	return result, invokeErr
}

func (c *evidenceProofContext) acceptOccurrence(bundle *evidenceProofBundle, request recordcontract.OccurrenceRequest) ([]byte, cadruntime.OccurrenceRuntimeEvidence, *recordcontract.OccurrenceSummary) {
	c.t.Helper()
	raw := readEvidenceProofFile(c.t, filepath.Join(bundle.outputDir, cadruntime.OccurrenceEvidenceFilename))
	c.require(len(raw) > 0, "real FreeCAD emitted no occurrence evidence")
	decoded, err := cadruntime.DecodeOccurrenceEvidence(raw)
	c.require(err == nil, "strict occurrence evidence decode failed: %v", err)
	summary, err := cadruntime.MapOccurrenceEvidence(cadruntime.OccurrenceMappingInput{
		Request: &request, Resources: bundle.inventory, ExpectedSourceDocument: bundle.layout.SourceDocument, Content: raw,
	})
	c.require(err == nil, "Engine occurrence acceptance failed: %v", err)
	c.require(summary != nil, "Engine accepted no occurrence summary")
	return raw, decoded, summary
}

func (c *evidenceProofContext) acceptFact(bundle *evidenceProofBundle, request recordcontract.FactRequest) ([]byte, []recordcontract.ObservationFact) {
	c.t.Helper()
	raw := readEvidenceProofFile(c.t, filepath.Join(bundle.outputDir, cadruntime.FactEvidenceFilename))
	c.require(len(raw) > 0, "real FreeCAD emitted no fact evidence")
	decoded, err := cadruntime.DecodeFactEvidence(raw)
	c.require(err == nil, "strict fact evidence decode failed: %v", err)
	c.require(decoded.SchemaVersion == recordcontract.FactEvidenceSchemaVersion, "fact evidence schema = %q", decoded.SchemaVersion)
	facts, err := cadruntime.MapFactEvidence(cadruntime.FactMappingInput{
		Request: &request, Resources: bundle.inventory, ExpectedSourceDocument: bundle.layout.SourceDocument, Content: raw,
	})
	c.require(err == nil, "Engine fact acceptance failed: %v", err)
	return raw, facts
}

func (c *evidenceProofContext) acceptIntegrity(bundle *evidenceProofBundle, result *freecad.FreeCADRuntimeResult) ([]byte, *integrity.OperationIntegrity) {
	c.t.Helper()
	c.require(result != nil && result.Status == freecad.FreeCADRuntimeResultStatusSucceeded, "integrity requires a successful runtime result")
	evidence, raw, err := cadruntime.LoadFreeCADIntegrityEvidence(bundle.root, filepath.Join(bundle.outputDir, cadruntime.FreeCADIntegrityEvidenceFilename))
	c.require(err == nil, "load integrity evidence: %v", err)
	c.require(evidence != nil && len(raw) > 0, "real FreeCAD emitted no integrity evidence")
	request, err := cadruntime.ParseFreeCADIntegrityRequest(bundle.integrityRequestJSON)
	c.require(err == nil, "parse Engine integrity request: %v", err)
	operation, err := cadruntime.EvaluateFreeCADRuntimeIntegrity(bundle.attempt, request, evidence)
	c.require(err == nil, "Engine integrity acceptance failed: %v", err)
	return raw, operation
}

// --- Package emission -------------------------------------------------------

func (c *evidenceProofContext) emitExecutorPackage(name string, run evidenceProofFactRun, provenance *integrity.ProjectIntegrity) map[string][]byte {
	c.t.Helper()
	runRoot := filepath.Join(c.workspace, name)
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		c.t.Fatal(err)
	}
	c.require(report.Write(runRoot, run.report) == nil, "write report")
	execution := scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{Job: run.job, States: run.states, Err: run.err, CADRuntimeOutcome: run.outcome}}}
	if run.err != nil {
		failure, failureErr := cadRuntimeFailureRunEvidence(execution, run.report)
		c.require(failureErr == nil, "failure evidence: %v", failureErr)
		emit := recordemit.RunEmitInput{RunRoot: runRoot, PlanHash: run.report.PlanHash, Report: run.report, SourceIntegrity: provenance, CADRuntime: failure}
		c.require(recordemit.EmitRunPackage(emit) == nil, "emit failure package")
		return readRecordPackageFiles(c.t, filepath.Join(runRoot, recordpackage.PackageDirectoryName))
	}
	outcomes, err := runOutcomeEvidences(execution)
	c.require(err == nil, "project outcome evidence: %v", err)
	emit := recordemit.RunEmitInput{RunRoot: runRoot, PlanHash: run.report.PlanHash, Report: run.report, SourceIntegrity: provenance, Outcomes: outcomes}
	c.require(recordemit.EmitRunPackage(emit) == nil, "emit record package")
	return readRecordPackageFiles(c.t, filepath.Join(runRoot, recordpackage.PackageDirectoryName))
}

func (c *evidenceProofContext) emitMultiExecutorPackage(name string, runs []evidenceProofFactRun, provenance *integrity.ProjectIntegrity) map[string][]byte {
	c.t.Helper()
	runRoot := filepath.Join(c.workspace, name)
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		c.t.Fatal(err)
	}
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	execution := scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{}}
	for _, run := range runs {
		execution.Jobs = append(execution.Jobs, scheduler.JobExecution{Job: run.job, States: run.states, Err: run.err, CADRuntimeOutcome: run.outcome})
	}
	rep, err := report.Build(report.BuildInput{
		PlanHash: runs[0].report.PlanHash, Execution: execution, StartedAt: stamp, EndedAt: stamp, WorkerCount: 2,
		StepTimeoutSeconds: int(executor.DefaultStepTimeout / time.Second), MaxRetries: executor.DefaultMaxRetries,
	})
	c.require(err == nil, "build multi report: %v", err)
	c.require(report.Write(runRoot, rep) == nil, "write multi report")
	outcomes, err := runOutcomeEvidences(execution)
	c.require(err == nil, "multi outcome evidence: %v", err)
	emit := recordemit.RunEmitInput{RunRoot: runRoot, PlanHash: runs[0].report.PlanHash, Report: rep, SourceIntegrity: provenance, Outcomes: outcomes}
	c.require(recordemit.EmitRunPackage(emit) == nil, "emit multi package")
	return readRecordPackageFiles(c.t, filepath.Join(runRoot, recordpackage.PackageDirectoryName))
}

func (c *evidenceProofContext) emitFixturePackage(name string, bundle *evidenceProofBundle, occurrenceRequest recordcontract.OccurrenceRequest, occurrenceRaw []byte, factRequest recordcontract.FactRequest, factRaw []byte, integrityRaw []byte, operation *integrity.OperationIntegrity) map[string][]byte {
	c.t.Helper()
	runRoot := filepath.Join(c.workspace, name)
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		c.t.Fatal(err)
	}
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	productKey := "assembly"
	j, err := job.New(productKey, []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{
			ProductKey: productKey, Filename: productKey + ".csv", Headers: []string{"assembly"}, Values: []any{1},
		}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{
			ProductKey: productKey, ManifestFilename: planner.ExportManifestFilename,
			ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
			SchemaVersion:          planner.ExportManifestSchemaVersion, PlanHash: "evidence-assembly-plan", Adapter: "freecad",
			Product: planner.ExportManifestProduct{ID: productKey}, SourceDocument: bundle.layout.SourceDocument,
			Inputs: planner.ExportManifestInputs{SourceModel: bundle.layout.SourceDocumentPath},
			Values: map[string]any{"assembly": 1}, Outputs: []planner.ExportManifestOutput{},
		}},
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{
			ProductKey: productKey, Adapter: "freecad",
			ManifestFilename: planner.ExportManifestFilename, ResultFilename: planner.FreeCADRuntimeResultFilename,
		}},
	})
	c.require(err == nil, "fixture job: %v", err)
	states := []executor.ExecutionState{{ProductID: productKey, StepID: "0", Status: executor.StepSuccess, Attempts: 1, StartedAt: stamp, EndedAt: stamp}}
	rep, err := report.Build(report.BuildInput{
		PlanHash: "evidence-assembly-plan", Execution: scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{Job: j, States: states}}},
		StartedAt: stamp, EndedAt: stamp, WorkerCount: 1,
		StepTimeoutSeconds: int(executor.DefaultStepTimeout / time.Second), MaxRetries: executor.DefaultMaxRetries,
	})
	c.require(err == nil, "fixture report: %v", err)
	c.require(report.Write(runRoot, rep) == nil, "write fixture report")
	evidence := &recordemit.CADRuntimeRunEvidence{
		Occurrence: &recordemit.OccurrenceRunEvidence{Input: cadruntime.OccurrenceMappingInput{
			Request: &occurrenceRequest, Resources: bundle.inventory, ExpectedSourceDocument: bundle.layout.SourceDocument, Content: occurrenceRaw,
		}},
		Fact: &recordemit.FactRunEvidence{Input: cadruntime.FactMappingInput{
			Request: &factRequest, Resources: bundle.inventory, ExpectedSourceDocument: bundle.layout.SourceDocument, Content: factRaw,
		}},
		Result: readEvidenceProofFile(c.t, bundle.resultPath), IntegrityEvidence: integrityRaw, Integrity: operation,
		JobID: j.ID, ProductKey: productKey, StepRef: "0", Attempt: 1, AttemptID: bundle.attempt.Identity.ID,
	}
	emit := recordemit.RunEmitInput{
		RunRoot: runRoot, PlanHash: "evidence-assembly-plan", Report: rep, SourceIntegrity: fixtureSourceIntegrity(bundle),
		Outcomes: []recordemit.RunOutcomeEvidence{{
			JobID: j.ID, ProductKey: productKey, StepRef: "0", Attempt: 1, AttemptID: bundle.attempt.Identity.ID,
			CADRuntime: evidence,
		}},
	}
	c.require(recordemit.EmitRunPackage(emit) == nil, "emit fixture package")
	return readRecordPackageFiles(c.t, filepath.Join(runRoot, recordpackage.PackageDirectoryName))
}

func fixtureSourceIntegrity(bundle *evidenceProofBundle) *integrity.ProjectIntegrity {
	sources := []integrity.ResourceSource{}
	for _, binding := range bundle.inventory.Bindings() {
		digest, _ := integrity.ParseDigest(binding.SourceDigest)
		sources = append(sources, integrity.ResourceSource{
			LogicalID: binding.LogicalID, SourceCoordinate: binding.SourceCoordinate,
			SourceDigest: digest, LockState: integrity.LockStateVerified, LockedDigest: digest,
		})
	}
	return &integrity.ProjectIntegrity{Lock: integrity.LockStateVerified, Resources: sources}
}

// --- Assertions and small helpers ------------------------------------------

func (c *evidenceProofContext) requireFact(facts map[string]recordcontract.ObservationFact, property, kind, raw, unit, availability, logicalID string) {
	c.t.Helper()
	fact, ok := facts[property]
	c.require(ok, "fact %s missing", property)
	c.require(fact.Value.Kind == kind, "%s value kind = %q, want %q", property, fact.Value.Kind, kind)
	c.require(fact.Value.Raw == raw, "%s value = %q, want %q", property, fact.Value.Raw, raw)
	c.require(fact.Unit == unit, "%s unit = %q, want %q", property, fact.Unit, unit)
	c.require(fact.Availability == availability, "%s availability = %q, want %q", property, fact.Availability, availability)
	if fact.Subject.Resource != nil {
		c.require(fact.Subject.Resource.LogicalID == logicalID, "%s subject resource = %q, want %q", property, fact.Subject.Resource.LogicalID, logicalID)
	}
}

func (c *evidenceProofContext) requirePackageRaw(files map[string][]byte, path string) {
	c.t.Helper()
	c.require(files[path] != nil, "package lacks raw evidence %s: %v", path, packageRawPaths(files))
}

func (c *evidenceProofContext) writeFile(path, content string) {
	c.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		c.t.Fatal(err)
	}
}

func (c *evidenceProofContext) writeJSON(path string, value any) {
	c.t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		c.t.Fatal(err)
	}
	c.writeFile(path, string(append(data, '\n')))
}

func allFactBindingsRequest(projectID string) recordcontract.FactRequest {
	return recordcontract.FactRequest{
		SchemaVersion: recordcontract.FactRequestSchemaVersion,
		Bindings: []recordcontract.FactBinding{
			factBinding(projectID, "fact_source", "Box", "Length", recordcontract.FactValueKindNumber, "mm", recordcontract.FactRequirementOptional),
			factBinding(projectID, "fact_source", "Props", "MeasuredLength", recordcontract.FactValueKindNumber, "mm", recordcontract.FactRequirementOptional),
			factBinding(projectID, "fact_source", "Props", "Ratio", recordcontract.FactValueKindNumber, "", recordcontract.FactRequirementOptional),
			factBinding(projectID, "fact_source", "Props", "Count", recordcontract.FactValueKindInteger, "", recordcontract.FactRequirementOptional),
			factBinding(projectID, "fact_source", "Props", "Enabled", recordcontract.FactValueKindBoolean, "", recordcontract.FactRequirementOptional),
			factBinding(projectID, "fact_source", "Props", "PlainText", recordcontract.FactValueKindString, "", recordcontract.FactRequirementOptional),
			factBinding(projectID, "fact_source", "Props", "SpecialText", recordcontract.FactValueKindString, "", recordcontract.FactRequirementOptional),
			factBinding(projectID, "fact_source", "Props", "NonFinite", recordcontract.FactValueKindNumber, "", recordcontract.FactRequirementOptional),
			factBinding(projectID, "fact_source", "Props", "MissingProperty", recordcontract.FactValueKindNumber, "", recordcontract.FactRequirementOptional),
			factBinding(projectID, "secondary", "SecondaryProps", "SecondaryLength", recordcontract.FactValueKindNumber, "mm", recordcontract.FactRequirementOptional),
		},
	}
}

func supportedOccurrenceRequest(projectID, requirement string) recordcontract.OccurrenceRequest {
	return recordcontract.OccurrenceRequest{
		SchemaVersion: recordcontract.OccurrenceSchemaVersion,
		Requirement:   requirement,
		Scope: recordcontract.OccurrenceScope{
			Parent: recordcontract.OccurrenceEntity{
				Resource:   recordcontract.ReferenceResource{ProjectID: projectID, Kind: "model", LogicalID: "assembly_root"},
				NativeKind: "object", Name: "SupportedAssembly",
			},
			Context:   []recordcontract.OccurrenceContextFrame{},
			Mechanism: recordcontract.OccurrenceDirectMemberMechanism,
		},
	}
}

func factBinding(projectID, logicalID, name, property, valueKind, unit, requirement string) recordcontract.FactBinding {
	binding := recordcontract.FactBinding{
		Kind: recordcontract.FactKindBoundProperty,
		Subject: recordcontract.OccurrenceEntity{
			Resource:   recordcontract.ReferenceResource{ProjectID: projectID, Kind: "model", LogicalID: logicalID},
			NativeKind: "object", Name: name,
		},
		Context: []recordcontract.OccurrenceContextFrame{}, Property: property,
		ValueKind: valueKind, Requirement: requirement,
	}
	if unit != "" {
		binding.Unit = unit
	}
	return binding
}

func decodeObservationRecord(t *testing.T, files map[string][]byte) recordcontract.ObservationRecord {
	t.Helper()
	for path, content := range files {
		if strings.HasSuffix(path, "prm.observation-record.json") {
			var record recordcontract.ObservationRecord
			if err := json.Unmarshal(content, &record); err != nil {
				t.Fatalf("decode observation record: %v", err)
			}
			return record
		}
	}
	t.Fatalf("package has no observation record: %v", packageFamilies(files))
	return recordcontract.ObservationRecord{}
}

func factFacts(facts []recordcontract.ObservationFact) map[string]recordcontract.ObservationFact {
	out := map[string]recordcontract.ObservationFact{}
	for _, fact := range facts {
		if fact.Kind == "fact" {
			out[fact.Property] = fact
		}
	}
	return out
}

func normalizedFactKeys(record recordcontract.ObservationRecord) []string {
	keys := make([]string, 0, len(record.Observation.Facts))
	for _, fact := range record.Observation.Facts {
		if fact.Kind != "fact" {
			continue
		}
		keys = append(keys, fact.Property+":"+fact.Availability+":"+fact.Value.Kind+":"+fact.Value.Raw+":"+fact.Unit)
	}
	sort.Strings(keys)
	return keys
}

func factEvidenceOrder(raw []byte) []string {
	var payload struct {
		Facts []struct {
			Property string `json:"property"`
		} `json:"facts"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	out := make([]string, 0, len(payload.Facts))
	for _, fact := range payload.Facts {
		out = append(out, fact.Property)
	}
	return out
}

func countAvailability(facts []recordcontract.ObservationFact, availability string) int {
	count := 0
	for _, fact := range facts {
		if fact.Availability == availability {
			count++
		}
	}
	return count
}

func countObservationRecords(files map[string][]byte) int {
	count := 0
	for path := range files {
		if strings.HasSuffix(path, "prm.observation-record.json") {
			count++
		}
	}
	return count
}

func packageFamilies(files map[string][]byte) []string {
	families := map[string]bool{}
	for path := range files {
		if !strings.HasSuffix(path, "-record.json") {
			continue
		}
		base := filepath.Base(path)
		base = strings.TrimSuffix(base, "-record.json")
		base = strings.TrimPrefix(base, "prm.")
		families[base] = true
	}
	out := make([]string, 0, len(families))
	for family := range families {
		out = append(out, family)
	}
	sort.Strings(out)
	return out
}

func packageHasFamily(files map[string][]byte, family string) bool {
	for _, item := range packageFamilies(files) {
		if item == family {
			return true
		}
	}
	return false
}

func packageRawPaths(files map[string][]byte) []string {
	out := []string{}
	for path := range files {
		if strings.HasPrefix(path, "raw/") {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func containsPath(paths []string, fragment string) bool {
	for _, path := range paths {
		if strings.Contains(path, fragment) {
			return true
		}
	}
	return false
}

func integrityResource(operation *integrity.OperationIntegrity, logicalID string) *integrity.ResourceOperation {
	if operation == nil {
		return nil
	}
	for index := range operation.Resources {
		if operation.Resources[index].LogicalID == logicalID {
			return &operation.Resources[index]
		}
	}
	return nil
}

func integrityCoordinates(operation *integrity.OperationIntegrity) string {
	if operation == nil {
		return ""
	}
	parts := make([]string, 0, len(operation.Resources))
	for _, resource := range operation.Resources {
		parts = append(parts, resource.LogicalID+"="+resource.MaterializedCoordinate+"="+string(resource.RuntimeBaselineStatus)+"="+string(resource.ResultStatus))
	}
	return strings.Join(parts, "|")
}

func reflectFactsEqual(left, right []recordcontract.ObservationFact) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return bytesEqual(leftBytes, rightBytes)
}

func reflectOccurrenceEqual(left, right *recordcontract.OccurrenceSummary) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return bytesEqual(leftBytes, rightBytes)
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func containsEvidenceString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func evidenceFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func digestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func copyEvidenceProofFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatalf("read %s: %v", from, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", to, err)
	}
}

func copyEvidenceProofTree(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatalf("read %s: %v", from, err)
	}
	for _, entry := range entries {
		source := filepath.Join(from, entry.Name())
		destination := filepath.Join(to, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				t.Fatal(err)
			}
			copyEvidenceProofTree(t, source, destination)
			continue
		}
		copyEvidenceProofFile(t, source, destination)
	}
}

func readEvidenceProofFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

type evidenceProofCommandResult struct {
	stdout string
	stderr string
	code   int
}

func runEvidenceProofCommand(t *testing.T, cwd, command string, args ...string) evidenceProofCommandResult {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), command, args...)
	cmd.Dir = cwd
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := evidenceProofCommandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.code = exitErr.ExitCode()
	} else {
		result.code = -1
	}
	t.Fatalf("command failed: %s %v: %v\nstdout:\n%s\nstderr:\n%s", command, args, err, result.stdout, result.stderr)
	return result
}
