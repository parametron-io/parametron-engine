package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/verification"
)

// Issue #19: permanent proof of the complete Engine-owned FreeCAD runtime
// contract package through the normal production path:
//
//	project + DSL target actions -> semantic lowering -> planner -> scheduler
//	-> executor -> attempt working copy -> prm.export-manifest.json +
//	prm.verification.json + prm.reference-traversal-request.json -> runtimecap
//	external process -> prm.result.json + prm.observed.json + optional
//	prm.reference-traversal.json -> evidence intake -> Engine verification -> outcome
//	consumption -> recordemit/recordpackage
//
// The external participant is the controlled aligned runtime installed by
// installControlledAlignedRuntime. Each scenario scripts the native
// target-state evidence it returns (PARAMETRON_TASK13_RUNTIME_TARGET_STATE_JSON)
// independently of the request, and the runtime logs what it received
// (PARAMETRON_TASK13_RUNTIME_INVOCATION_LOG). It never verifies, normalizes,
// or emits records. No FreeCAD is required.

type contractPackageTarget struct {
	name   string
	kind   string // "part" or "assembly": the capture component kind
	action string
}

func contractPackageComponent(id, kind, name string, targetable bool) string {
	capability := task13BoolJSON(targetable)
	return `{
        "id": "` + id + `",
        "kind": "` + kind + `",
        "name": "` + name + `",
        "displayName": "` + name + `",
        "cadType": "App::Part",
        "quantity": 1,
        "material": "",
        "stabilityClass": "stable",
        "targetability": {"suppress": ` + capability + `, "unsuppress": ` + capability + `, "hide": ` + capability + `, "unhide": ` + capability + `, "delete": ` + capability + `},
        "annotations": {"description": "", "comment": "", "purpose": ""}
      }`
}

// writeContractPackageFixture writes a capture-backed project (same shape as
// writeTask13TargetMutationHandoffFixture) whose DSL declares exactly the
// given target actions, in the given order, on fully targetable components.
func writeContractPackageFixture(t *testing.T, targets []contractPackageTarget) string {
	t.Helper()

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, "input"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "input", "box.FCStd"), []byte("contract-package-fixture"), 0o644); err != nil {
		t.Fatal(err)
	}

	var dslTargets strings.Builder
	components := []string{contractPackageComponent("cmp.root", "assembly", "MainAssembly", false)}
	var leafIDs []string
	for _, target := range targets {
		dslTargets.WriteString("    target " + target.name + ": action = " + target.action + "\n")
		id := "cmp." + strings.ToLower(target.name)
		leafIDs = append(leafIDs, id)
		components = append(components, contractPackageComponent(id, target.kind, target.name, true))
	}
	structureNodes := []string{task13StructureNode("cmp.root", "", leafIDs)}
	for _, id := range leafIDs {
		structureNodes = append(structureNodes, task13StructureNode(id, "cmp.root", nil))
	}

	files := map[string]string{
		"project.dsl": withVersionHeader(`
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 42

` + dslTargets.String() + `}
`),
		"parametron.project.json": `{
  "version": "1.0",
  "projectId": "contract-package-fixture",
  "dsl": "project.dsl",
  "resources": {"models": {"box_model": "input/box.FCStd"}}
}`,
		"parametron.semantic-map.json": defaultCaptureBackedSemanticMapJSON(),
		"parametron.cad.json": `{
  "schemaVersion": "1.0",
  "captureId": "cap.contract.package",
  "adapter": {"name": "freecad", "version": ""},
  "cadSystem": {"name": "FreeCAD", "version": ""},
  "sourceDocument": {"logicalId": "box_model", "path": "input/box.FCStd", "fingerprint": "sha256:fixture"},
  "rootProduct": {"id": "cmp.root"},
  "annotations": {"description": "", "comment": "", "purpose": ""},
  "entities": {
    "components": [
` + strings.Join(components, ",\n") + `
    ],
    "features": [],
    "relationships": [],
    "parameterGroups": [],
    "parameters": [
      {
        "id": "par.root.width",
        "ownerKind": "component",
        "ownerId": "cmp.root",
        "componentId": "cmp.root",
        "name": "width",
        "displayName": "Width",
        "cadType": "Length",
        "valueType": "number",
        "observable": true,
        "writable": true,
        "currentValue": 42,
        "unit": "mm",
        "stabilityClass": "stable",
        "annotations": {"description": "", "comment": "", "purpose": ""}
      }
    ],
    "metadata": []
  },
  "structure": {
    "rootComponentId": "cmp.root",
    "nodes": [
` + strings.Join(structureNodes, ",\n") + `
    ]
  }
}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return projectDir
}

// --- scripted native target-state evidence (fake-runtime response only) ---

type scriptedBooleanEvidence struct {
	Destination string `json:"destination"`
	Object      string `json:"object"`
	Status      string `json:"status"`
	Value       *bool  `json:"value,omitempty"`
}

type scriptedExistenceEvidence struct {
	Destination string `json:"destination"`
	Object      string `json:"object"`
	Status      string `json:"status"`
}

type scriptedTargetState struct {
	Suppression []scriptedBooleanEvidence   `json:"suppression"`
	Visibility  []scriptedBooleanEvidence   `json:"visibility"`
	Existence   []scriptedExistenceEvidence `json:"existence"`
}

func observedBoolean(destination, object string, value bool) scriptedBooleanEvidence {
	return scriptedBooleanEvidence{Destination: destination, Object: object, Status: "observed", Value: &value}
}

func booleanWithStatus(destination, object, status string) scriptedBooleanEvidence {
	return scriptedBooleanEvidence{Destination: destination, Object: object, Status: status}
}

func existenceWithStatus(destination, object, status string) scriptedExistenceEvidence {
	return scriptedExistenceEvidence{Destination: destination, Object: object, Status: status}
}

func (s scriptedTargetState) json(t *testing.T) string {
	t.Helper()
	if s.Suppression == nil {
		s.Suppression = []scriptedBooleanEvidence{}
	}
	if s.Visibility == nil {
		s.Visibility = []scriptedBooleanEvidence{}
	}
	if s.Existence == nil {
		s.Existence = []scriptedExistenceEvidence{}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// --- controlled runtime invocation log ---

type contractRuntimeInvocation struct {
	Argv                     []string `json:"argv"`
	ManifestSHA256           string   `json:"manifestSHA256"`
	ObservationRequestSHA256 string   `json:"observationRequestSHA256"`
	TraversalRequestSHA256   string   `json:"traversalRequestSHA256"`
	SourceDocumentPath       string   `json:"sourceDocumentPath"`
	SourceDocumentSHA256     string   `json:"sourceDocumentSHA256"`
}

// flag returns the value the runtime received for a command-line flag.
func (inv contractRuntimeInvocation) flag(t *testing.T, name string) string {
	t.Helper()
	for i := 0; i+1 < len(inv.Argv); i++ {
		if inv.Argv[i] == name {
			return inv.Argv[i+1]
		}
	}
	t.Fatalf("runtime was not invoked with %s: %v", name, inv.Argv)
	return ""
}

type contractPackageRun struct {
	planned     *plannedRun
	result      *executionResult
	err         error
	invocations []contractRuntimeInvocation
}

// runContractPackage executes the project through the normal CLI execution
// path (loadPlannedRun + executePlanRun, as the root command does) against the
// controlled runtime in the given mode. targetState, when non-nil, is the
// native evidence the runtime returns verbatim.
func runContractPackage(t *testing.T, projectDir, outDir, mode string, targetState *string) contractPackageRun {
	t.Helper()
	installControlledAlignedRuntime(t, mode)
	logPath := filepath.Join(t.TempDir(), "invocations.jsonl")
	t.Setenv("PARAMETRON_TASK13_RUNTIME_INVOCATION_LOG", logPath)
	if targetState != nil {
		t.Setenv("PARAMETRON_TASK13_RUNTIME_TARGET_STATE_JSON", *targetState)
	} else {
		unsetEnvForTest(t, "PARAMETRON_TASK13_RUNTIME_TARGET_STATE_JSON")
	}

	resetGlobals()
	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun: %v", err)
	}
	result, err := executePlanRun(executionOptions{Planned: planned, OutputDir: outDir, UseCache: true})
	if result == nil {
		t.Fatalf("executePlanRun returned no result: %v", err)
	}
	return contractPackageRun{planned: planned, result: result, err: err, invocations: readContractRuntimeInvocations(t, logPath)}
}

func readContractRuntimeInvocations(t *testing.T, logPath string) []contractRuntimeInvocation {
	t.Helper()
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("controlled runtime was never invoked (no invocation log): %v", err)
	}
	defer file.Close()
	var out []contractRuntimeInvocation
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var inv contractRuntimeInvocation
		if err := json.Unmarshal(scanner.Bytes(), &inv); err != nil {
			t.Fatalf("decode invocation log line: %v", err)
		}
		out = append(out, inv)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// --- request-side proof ---

// contractRequestExpectation is the literal canonical request content a
// scenario must hand the runtime. Mutation sections and the observation
// request are compact JSON ("" means the key must be absent).
type contractRequestExpectation struct {
	partMutations      string
	assemblyMutations  string
	targetStateRequest string
}

type contractAttempt struct {
	workingCopy string
}

func decodeJSONFile(t *testing.T, path string) (map[string]any, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, data)
	}
	return decoded, data
}

func compactJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func canonicalJSONString(t *testing.T, raw string) string {
	t.Helper()
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return compactJSON(t, decoded)
}

func assertOptionalJSONKey(t *testing.T, label string, object map[string]any, key, want string) {
	t.Helper()
	got, ok := object[key]
	if want == "" {
		if ok {
			t.Fatalf("%s: %q must be absent, got %s", label, key, compactJSON(t, got))
		}
		return
	}
	if !ok {
		t.Fatalf("%s: %q missing, want %s", label, key, want)
	}
	if compactJSON(t, got) != canonicalJSONString(t, want) {
		t.Fatalf("%s: %q = %s, want %s", label, key, compactJSON(t, got), canonicalJSONString(t, want))
	}
}

// assertContractRequest proves what the external runtime actually received:
// canonical argv, attempt-contained authoritative paths, canonical filenames,
// schema 1.0, the projected mutation intent, and the observation request.
func assertContractRequest(t *testing.T, projectDir string, run contractPackageRun, inv contractRuntimeInvocation, want contractRequestExpectation) contractAttempt {
	t.Helper()

	if len(inv.Argv) == 0 || inv.Argv[0] != "execute" {
		t.Fatalf("runtime argv = %v, want the execute subcommand first", inv.Argv)
	}
	working := inv.flag(t, "--working-copy")
	productWorkingRoot := filepath.Join(run.result.RunRoot, "products", "Box", "_working")
	if filepath.Dir(working) != productWorkingRoot || !filepath.IsAbs(working) || filepath.Clean(working) != working {
		t.Fatalf("working copy %q is not a canonical attempt directory under %q", working, productWorkingRoot)
	}
	for flag, want := range map[string]string{
		"--manifest":                    filepath.Join(working, "prm.export-manifest.json"),
		"--observation-request":         filepath.Join(working, "prm.verification.json"),
		"--result":                      filepath.Join(working, "prm.result.json"),
		"--output-dir":                  filepath.Join(working, "outputs"),
		"--reference-traversal-request": filepath.Join(working, "prm.reference-traversal-request.json"),
	} {
		if got := inv.flag(t, flag); got != want {
			t.Fatalf("runtime %s = %q, want authoritative attempt path %q", flag, got, want)
		}
	}

	// Source staging: the runtime opened the attempt-local staged copy, which
	// is byte-identical to the project's model, and the original is untouched.
	original := filepath.Join(projectDir, "input", "box.FCStd")
	originalBytes, err := os.ReadFile(original)
	if err != nil || string(originalBytes) != "contract-package-fixture" {
		t.Fatalf("original source document was mutated or removed (err=%v): %q", err, originalBytes)
	}
	if inv.SourceDocumentPath != filepath.Join(working, "source", "box.FCStd") || inv.SourceDocumentPath == original {
		t.Fatalf("runtime source document = %q, want the staged copy inside %q", inv.SourceDocumentPath, working)
	}
	if inv.SourceDocumentSHA256 != sha256HexOf(originalBytes) {
		t.Fatalf("staged source digest = %s, want original digest %s", inv.SourceDocumentSHA256, sha256HexOf(originalBytes))
	}

	manifest, manifestBytes := decodeJSONFile(t, inv.flag(t, "--manifest"))
	request, requestBytes := decodeJSONFile(t, inv.flag(t, "--observation-request"))
	traversal, traversalBytes := decodeJSONFile(t, inv.flag(t, "--reference-traversal-request"))
	if sha256HexOf(manifestBytes) != inv.ManifestSHA256 || sha256HexOf(requestBytes) != inv.ObservationRequestSHA256 ||
		sha256HexOf(traversalBytes) != inv.TraversalRequestSHA256 {
		t.Fatal("request files on disk differ from the bytes the runtime read")
	}
	canonicalTraversal, err := cadruntime.DecodeFreeCADReferenceTraversalRequest(traversalBytes)
	if err != nil || traversal["schemaVersion"] != "1.0" || canonicalTraversal.ExternalTargets == nil || len(canonicalTraversal.ExternalTargets) != 0 {
		t.Fatalf("traversal request = %s; decoded = %+v; err = %v", traversalBytes, canonicalTraversal, err)
	}

	// prm.export-manifest.json: canonical schema 1.0 and projected mutations.
	if manifest["schemaVersion"] != "1.0" || manifest["sourceDocument"] != "source/box.FCStd" {
		t.Fatalf("manifest schemaVersion/sourceDocument = %v/%v", manifest["schemaVersion"], manifest["sourceDocument"])
	}
	assertOptionalJSONKey(t, "attempt manifest", manifest, "partMutations", want.partMutations)
	assertOptionalJSONKey(t, "attempt manifest", manifest, "assemblyMutations", want.assemblyMutations)
	product, _ := decodeJSONFile(t, filepath.Join(run.result.RunRoot, "products", "Box", "prm.export-manifest.json"))
	if product["schemaVersion"] != "1.0" {
		t.Fatalf("product manifest schemaVersion = %v", product["schemaVersion"])
	}
	assertOptionalJSONKey(t, "product manifest", product, "partMutations", want.partMutations)
	assertOptionalJSONKey(t, "product manifest", product, "assemblyMutations", want.assemblyMutations)

	// prm.verification.json: target identities only. Expected target-state
	// values are Engine-private, so the runtime cannot echo them back.
	if request["schemaVersion"] != "1.0" {
		t.Fatalf("observation request schemaVersion = %v", request["schemaVersion"])
	}
	observe, _ := request["observe"].(map[string]any)
	context, _ := request["observationContext"].(map[string]any)
	expected, _ := request["expected"].(map[string]any)
	if observe == nil || context == nil || expected == nil {
		t.Fatalf("observation request lacks observe/observationContext/expected: %s", requestBytes)
	}
	assertOptionalJSONKey(t, "observation request context", context, "targetState", want.targetStateRequest)
	if got, ok := observe["targetState"]; (want.targetStateRequest != "") != (ok && got == true) {
		t.Fatalf("observe.targetState = %v (present=%t), want requested=%t", got, ok, want.targetStateRequest != "")
	}
	if _, ok := expected["targetState"]; ok {
		t.Fatalf("observation request discloses expected target state: %s", requestBytes)
	}
	return contractAttempt{workingCopy: working}
}

// assertActiveAttemptFiles audits the active attempt surface: exactly the
// canonical request/response files (plus staging and declared outputs), and
// no schema-version-bearing filename.
func assertActiveAttemptFiles(t *testing.T, working string, want ...string) {
	t.Helper()
	var got []string
	err := filepath.WalkDir(working, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(working, path)
		got = append(got, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attempt files = %v, want %v", got, want)
	}
	versioned := regexp.MustCompile(`(?i)(^|[._-])v\d|\d+\.\d+`)
	for _, rel := range got {
		if versioned.MatchString(filepath.Base(rel)) {
			t.Fatalf("active attempt filename %q carries a schema version", rel)
		}
	}
}

var (
	contractAttemptRequestFiles = []string{
		"prm.export-manifest.json", "prm.verification.json", "prm.reference-traversal-request.json", "source/box.FCStd",
	}
	contractAttemptSuccessFiles = append(append([]string(nil), contractAttemptRequestFiles...),
		"prm.result.json", "outputs/prm.observed.json", "outputs/Box.step")
)

// --- response-side and package proof ---

// assertRawObservedTargetState proves the observed evidence on disk is exactly
// what the runtime returned, and returns its bytes.
func assertRawObservedTargetState(t *testing.T, observedPath, wantTargetState string) []byte {
	t.Helper()
	decoded, data := decodeJSONFile(t, observedPath)
	if decoded["schemaVersion"] != "1.0" {
		t.Fatalf("observed schemaVersion = %v", decoded["schemaVersion"])
	}
	observation, _ := decoded["observation"].(map[string]any)
	assertOptionalJSONKey(t, "raw observed observation", observation, "targetState", wantTargetState)
	return data
}

func targetStateFacts(observation recordcontract.ObservationRecord) map[string]string {
	facts := map[string]string{}
	for _, fact := range observation.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindTargetState {
			facts[fact.Subject.ID+"/"+fact.Subject.Name+"/"+fact.Key] = fact.Value.Raw
		}
	}
	return facts
}

func verificationCategory(record recordcontract.VerificationRecord, category recordcontract.VerificationCategory) *recordcontract.VerificationCategoryResult {
	for i := range record.Verification.Categories {
		if record.Verification.Categories[i].Category == category {
			return &record.Verification.Categories[i]
		}
	}
	return nil
}

// assertContractSuccessPackage proves Engine consumed the runtime's evidence,
// verified it, and emitted the normal record package. wantFacts maps
// "destination/object/family" to the normalized raw evidence value; an empty
// map means target-state verification is not applicable.
func assertContractSuccessPackage(t *testing.T, run contractPackageRun, attempt contractAttempt, scripted string, wantFacts map[string]string) {
	t.Helper()
	if run.err != nil {
		t.Fatalf("run failed: %v", run.err)
	}
	if len(run.invocations) != 1 {
		t.Fatalf("runtime invoked %d times, want once", len(run.invocations))
	}
	inv := run.invocations[0]
	outcome := singleCADRuntimeOutcome(t, run.result.Execution)
	if outcome.WorkingCopyDir != attempt.workingCopy || outcome.ResultPath != inv.flag(t, "--result") ||
		outcome.ObservationRequestPath != inv.flag(t, "--observation-request") ||
		outcome.ObservedPath != filepath.Join(inv.flag(t, "--output-dir"), "prm.observed.json") {
		t.Fatalf("outcome paths %+v are not the invocation's authoritative attempt paths", outcome)
	}
	assertActiveAttemptFiles(t, attempt.workingCopy, contractAttemptSuccessFiles...)

	// Response evidence as written by the runtime, and as Engine parsed it.
	result, resultBytes := decodeJSONFile(t, outcome.ResultPath)
	if result["schemaVersion"] != "1.0" || result["status"] != "succeeded" {
		t.Fatalf("runtime result = %s", resultBytes)
	}
	observedBytes := assertRawObservedTargetState(t, outcome.ObservedPath, scripted)
	var engineParsed any
	if outcome.Observed != nil && outcome.Observed.Observation.TargetState != nil {
		engineParsed = outcome.Observed.Observation.TargetState
	}
	if scripted == "" {
		if engineParsed != nil {
			t.Fatalf("Engine consumed target state the runtime never returned: %s", compactJSON(t, engineParsed))
		}
	} else if engineParsed == nil || canonicalJSONString(t, compactJSON(t, engineParsed)) != canonicalJSONString(t, scripted) {
		t.Fatalf("Engine-consumed target state = %v, want the runtime's %s", engineParsed, scripted)
	}

	// Engine verification result.
	if outcome.Verification != artifact.VerificationOutcomePassed || outcome.VerificationResult == nil ||
		outcome.VerificationResult.Status != verification.StatusPass || outcome.Failure != nil {
		t.Fatalf("outcome verification = %q result %+v failure %+v", outcome.Verification, outcome.VerificationResult, outcome.Failure)
	}
	targetCategory := outcome.VerificationResult.Categories.TargetState
	if targetCategory.Enabled != (len(wantFacts) > 0) {
		t.Fatalf("target-state category enabled = %t, want %t", targetCategory.Enabled, len(wantFacts) > 0)
	}
	if len(wantFacts) > 0 && (targetCategory.Status != verification.CategoryStatusPass ||
		targetCategory.Message != fmt.Sprintf("verified %d target-state entries", len(wantFacts))) {
		t.Fatalf("target-state category = %+v, want pass over %d entries", targetCategory, len(wantFacts))
	}

	// Normal record package.
	packageRoot := recordPackageRoot(run.result.RunRoot)
	files := readRecordPackageFiles(t, packageRoot)
	manifest := readCLIRecordPackageManifest(t, packageRoot)
	families := manifestFamilies(manifest)
	for family, want := range map[string]int{"execution": 1, "observation": 1, "verification": 1, "failure": 0, "reference": 0} {
		if len(families[family]) != want {
			t.Fatalf("package %s records = %#v, want %d", family, families[family], want)
		}
	}
	stored := runRootArtifacts(t, run.result.RunRoot)
	if len(families["artifact"]) != len(stored) {
		t.Fatalf("package artifact records = %d, run registered %d", len(families["artifact"]), len(stored))
	}
	// The runtime-declared STEP output is accepted as a verified artifact
	// from the attempt's output directory and has its own artifact record.
	wantStepPath := filepath.ToSlash(filepath.Join(strings.TrimPrefix(attempt.workingCopy, run.result.RunRoot+string(filepath.Separator)), "outputs", "Box.step"))
	var verifiedSteps []artifact.Artifact
	for _, item := range stored {
		if item.Class == artifact.ArtifactClassVerified && item.Type == "step" {
			verifiedSteps = append(verifiedSteps, item)
		}
	}
	if len(verifiedSteps) != 1 || verifiedSteps[0].Path != wantStepPath {
		t.Fatalf("verified STEP artifacts = %+v, want exactly the runtime output %q", verifiedSteps, wantStepPath)
	}
	stepRecords := 0
	for _, entry := range families["artifact"] {
		var record recordcontract.ArtifactRecord
		decodePackageRecord(t, files, entry.ContractPath, &record)
		if entry.RecordKey == manifest.PackageKey+":artifact:"+verifiedSteps[0].ID && record.Artifact.ChecksumSHA256 == verifiedSteps[0].ChecksumSHA256 {
			stepRecords++
		}
	}
	if stepRecords != 1 {
		t.Fatalf("package has %d artifact records for the verified STEP output %+v, want 1", stepRecords, verifiedSteps[0])
	}
	assertManifestOrdering(t, manifest)
	assertManifestRawEvidencePaths(t, manifest, []string{
		recordpackage.RawReportContractPath(),
		recordpackage.RawMetadataContractPath(),
		recordpackage.RawArtifactStoreManifestContractPath(),
		"raw/observed/prm.observed.json",
		"raw/verification/prm.verification.json",
		"raw/runtime/prm.result.json",
	})
	requestBytes, err := os.ReadFile(outcome.ObservationRequestPath)
	if err != nil {
		t.Fatal(err)
	}
	for contractPath, original := range map[string][]byte{
		"raw/observed/prm.observed.json":         observedBytes,
		"raw/verification/prm.verification.json": requestBytes,
		"raw/runtime/prm.result.json":            resultBytes,
	} {
		if !bytes.Equal(files[contractPath], original) {
			t.Fatalf("raw evidence %q is not the runtime-attempt bytes", contractPath)
		}
	}

	var observation recordcontract.ObservationRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("observation"), &observation)
	if err := recordcontract.ValidateObservationRecord(observation); err != nil {
		t.Fatalf("observation record invalid: %v", err)
	}
	if got := targetStateFacts(observation); !reflect.DeepEqual(got, wantFacts) && !(len(got) == 0 && len(wantFacts) == 0) {
		t.Fatalf("observation target-state facts = %v, want %v", got, wantFacts)
	}
	if got, ok := evidenceRefDigest(observation.Provenance.Evidence, "observed", "raw/observed/prm.observed.json"); !ok || got != sha256HexOf(observedBytes) {
		t.Fatalf("observation provenance observed digest = %q (present=%t)", got, ok)
	}

	var verificationRecord recordcontract.VerificationRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("verification"), &verificationRecord)
	if err := recordcontract.ValidateVerificationRecord(verificationRecord); err != nil {
		t.Fatalf("verification record invalid: %v", err)
	}
	if verificationRecord.Verification.Outcome != recordcontract.VerificationOutcomePass {
		t.Fatalf("verification record outcome = %q", verificationRecord.Verification.Outcome)
	}
	category := verificationCategory(verificationRecord, recordcontract.VerificationCategoryTargetState)
	if len(wantFacts) == 0 {
		if category != nil {
			t.Fatalf("target-state verification category emitted without a target-state request: %+v", category)
		}
		return
	}
	if category == nil || !category.Enabled || category.Outcome != recordcontract.VerificationOutcomePass {
		t.Fatalf("target-state verification category = %+v, want enabled pass", category)
	}
	if got, ok := evidenceRefDigest(verificationRecord.Provenance.Evidence, "verification", "raw/verification/prm.verification.json"); !ok || got != sha256HexOf(requestBytes) {
		t.Fatalf("verification provenance request digest = %q (present=%t)", got, ok)
	}
}

// assertContractVerificationFailure proves a valid, runtime-successful
// response that Engine verification rejects with wantClass, and the
// established report-derived failure package.
func assertContractVerificationFailure(t *testing.T, run contractPackageRun, attempt contractAttempt, scripted string, wantClass verification.FailureClass) {
	t.Helper()
	var consumptionErr *executor.CADRuntimeConsumptionError
	if run.err == nil || !errors.As(run.err, &consumptionErr) ||
		consumptionErr.Stage != executor.CADRuntimeConsumptionStageVerificationFailure ||
		consumptionErr.Classification != string(wantClass) {
		t.Fatalf("run error = %v, want %s verification failure", run.err, wantClass)
	}
	if len(run.invocations) != 1 {
		t.Fatalf("runtime invoked %d times, want once (verification failures are terminal)", len(run.invocations))
	}
	outcome := failedCADOutcome(t, run.result.Execution)
	assertActiveAttemptFiles(t, attempt.workingCopy, contractAttemptSuccessFiles...)
	result, resultBytes := decodeJSONFile(t, outcome.ResultPath)
	if result["status"] != "succeeded" {
		t.Fatalf("runtime result = %s, want the runtime itself to have succeeded", resultBytes)
	}
	assertRawObservedTargetState(t, outcome.ObservedPath, scripted)

	if outcome.Verification != artifact.VerificationOutcomeFailed || outcome.VerificationClass != wantClass ||
		outcome.Failure.RuntimeNative || outcome.Failure.Boundary != "engine" || outcome.Failure.Category != "verification" ||
		outcome.Failure.Code != string(wantClass) {
		t.Fatalf("outcome = verification %q class %q failure %+v, want Engine %s", outcome.Verification, outcome.VerificationClass, outcome.Failure, wantClass)
	}
	vr := outcome.VerificationResult
	if vr == nil || vr.Status != verification.StatusFail || vr.Failure != wantClass ||
		!vr.Categories.TargetState.Enabled || vr.Categories.TargetState.Status != verification.CategoryStatusFail ||
		vr.Categories.Metadata.Status != verification.CategoryStatusPass || vr.Categories.References.Status != verification.CategoryStatusPass {
		t.Fatalf("verification result = %+v, want only the target-state category failing with %s", vr, wantClass)
	}

	// Established failed-run package semantics: a report-derived failure
	// record whose code is the verification class; no runtime-result evidence.
	failure, files := singleFailureRecord(t, contractFailedRun(run))
	assertReportDerivedFailure(t, contractFailedRun(run), failure, files)
	if run.result.Report.Error.Classification != wantClass || failure.Failure.Code != string(wantClass) {
		t.Fatalf("report classification/failure code = %q/%q, want %s", run.result.Report.Error.Classification, failure.Failure.Code, wantClass)
	}
	if hasFailureRecordEvidence(failure, "runtime-result", "raw/runtime/prm.result.json") {
		t.Fatal("verification failure cites runtime-native result evidence")
	}
	assertManifestRawEvidencePaths(t, readCLIRecordPackageManifest(t, recordPackageRoot(run.result.RunRoot)), []string{recordpackage.RawReportContractPath()})
}

func contractFailedRun(run contractPackageRun) failedCLIProjectExecution {
	return failedCLIProjectExecution{result: run.result, planned: run.planned, err: run.err}
}

func scripted(t *testing.T, state scriptedTargetState) *string {
	t.Helper()
	value := state.json(t)
	return &value
}

func contractPackageEnv(t *testing.T) {
	t.Helper()
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)
}

// --- scenarios ---

func TestContractPackage_OrdinaryExecutionWithoutTargetMutations(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	run := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "out"), "success", nil)
	if len(run.invocations) != 1 {
		t.Fatalf("runtime invoked %d times", len(run.invocations))
	}
	attempt := assertContractRequest(t, projectDir, run, run.invocations[0], contractRequestExpectation{})
	assertContractSuccessPackage(t, run, attempt, "", nil)
}

func TestContractPackage_TraversalEvidenceUsesCanonicalSchema(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	traversalBytes := validReferenceTraversalJSONFixture()
	t.Setenv("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", string(traversalBytes))
	run := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "out"), "success", nil)
	if run.err != nil || len(run.invocations) != 1 {
		t.Fatalf("normal runtime result = %v; invocations = %d", run.err, len(run.invocations))
	}
	attempt := assertContractRequest(t, projectDir, run, run.invocations[0], contractRequestExpectation{})
	wantFiles := append(append([]string(nil), contractAttemptSuccessFiles...), "outputs/prm.reference-traversal.json")
	assertActiveAttemptFiles(t, attempt.workingCopy, wantFiles...)

	outcome := singleCADRuntimeOutcome(t, run.result.Execution)
	result, resultBytes := decodeJSONFile(t, outcome.ResultPath)
	observed, observedBytes := decodeJSONFile(t, outcome.ObservedPath)
	traversal, actualTraversalBytes := decodeJSONFile(t, outcome.ReferenceTraversalPath)
	for name, contract := range map[string]map[string]any{
		"result": result, "observed": observed, "reference traversal": traversal,
	} {
		if contract["schemaVersion"] != "1.0" {
			t.Fatalf("%s schemaVersion = %v", name, contract["schemaVersion"])
		}
	}
	if !bytes.Equal(actualTraversalBytes, traversalBytes) || !bytes.Equal(outcome.ReferenceTraversalJSON, traversalBytes) {
		t.Fatal("Engine did not consume the exact runtime traversal bytes")
	}
	nodes, ok := traversal["nodes"].([]any)
	if !ok || len(nodes) != 2 {
		t.Fatalf("traversal nodes = %v", traversal["nodes"])
	}
	object, ok := nodes[1].(map[string]any)
	if !ok || object["objectType"] != "PartDesign::Body" {
		t.Fatalf("rich traversal object node = %v", nodes[1])
	}
	edges, ok := traversal["edges"].([]any)
	if !ok || len(edges) != 1 {
		t.Fatalf("traversal edges = %v", traversal["edges"])
	}
	edge, ok := edges[0].(map[string]any)
	if !ok || edge["sourceProperty"] != "Group" || edge["referenceMechanism"] != "App::PropertyLinkList" {
		t.Fatalf("rich traversal edge = %v", edges[0])
	}

	packageRoot := recordPackageRoot(run.result.RunRoot)
	files := readRecordPackageFiles(t, packageRoot)
	rawPath := recordpackage.RawRuntimeReferenceTraversalContractPath()
	if !bytes.Equal(files[rawPath], traversalBytes) ||
		!bytes.Equal(files[recordpackage.RawRuntimeResultContractPath()], resultBytes) ||
		!bytes.Equal(files[recordpackage.RawObservedContractPath()], observedBytes) {
		t.Fatal("package did not preserve the returned runtime contract bytes")
	}
	manifest := readCLIRecordPackageManifest(t, packageRoot)
	assertManifestHasRecord(t, manifest, "reference", recordpackage.MustRecordContractPath("reference"), manifest.PackageKey+":reference")
	assertManifestRawEvidencePaths(t, manifest, []string{
		recordpackage.RawReportContractPath(), recordpackage.RawMetadataContractPath(),
		recordpackage.RawArtifactStoreManifestContractPath(), recordpackage.RawObservedContractPath(),
		recordpackage.RawVerificationContractPath(), recordpackage.RawRuntimeResultContractPath(), rawPath,
	})
	var reference recordcontract.ReferenceRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("reference"), &reference)
	if err := recordcontract.ValidateReferenceRecord(reference); err != nil || len(reference.Reference.Edges) != 1 {
		t.Fatalf("normalized reference record = %+v; err = %v", reference, err)
	}
	if got := reference.Reference.Edges[0].Evidence.DigestSHA256; got != sha256HexOf(traversalBytes) {
		t.Fatalf("raw traversal digest = %q, want %q", got, sha256HexOf(traversalBytes))
	}
}

func TestContractPackage_SingleFamilySuccess(t *testing.T) {
	for _, tc := range []struct {
		name      string
		target    contractPackageTarget
		want      contractRequestExpectation
		response  scriptedTargetState
		wantFacts map[string]string
	}{
		{
			name:   "suppress",
			target: contractPackageTarget{"LegA", "part", "suppress"},
			want: contractRequestExpectation{
				partMutations:      `{"suppression":[{"object":"LegA","suppressed":true}]}`,
				targetStateRequest: `{"suppression":[{"destination":"part","object":"LegA"}],"visibility":[],"existence":[]}`,
			},
			response:  scriptedTargetState{Suppression: []scriptedBooleanEvidence{observedBoolean("part", "LegA", true)}},
			wantFacts: map[string]string{"part/LegA/suppression": `{"status":"observed","value":true}`},
		},
		{
			name:   "unsuppress",
			target: contractPackageTarget{"SubA", "assembly", "unsuppress"},
			want: contractRequestExpectation{
				assemblyMutations:  `{"suppression":[{"object":"SubA","suppressed":false}]}`,
				targetStateRequest: `{"suppression":[{"destination":"assembly","object":"SubA"}],"visibility":[],"existence":[]}`,
			},
			response:  scriptedTargetState{Suppression: []scriptedBooleanEvidence{observedBoolean("assembly", "SubA", false)}},
			wantFacts: map[string]string{"assembly/SubA/suppression": `{"status":"observed","value":false}`},
		},
		{
			name:   "hide",
			target: contractPackageTarget{"LegA", "part", "hide"},
			want: contractRequestExpectation{
				partMutations:      `{"visibility":[{"object":"LegA","visible":false}]}`,
				targetStateRequest: `{"suppression":[],"visibility":[{"destination":"part","object":"LegA"}],"existence":[]}`,
			},
			response:  scriptedTargetState{Visibility: []scriptedBooleanEvidence{observedBoolean("part", "LegA", false)}},
			wantFacts: map[string]string{"part/LegA/visibility": `{"status":"observed","value":false}`},
		},
		{
			name:   "unhide",
			target: contractPackageTarget{"SubA", "assembly", "unhide"},
			want: contractRequestExpectation{
				assemblyMutations:  `{"visibility":[{"object":"SubA","visible":true}]}`,
				targetStateRequest: `{"suppression":[],"visibility":[{"destination":"assembly","object":"SubA"}],"existence":[]}`,
			},
			response:  scriptedTargetState{Visibility: []scriptedBooleanEvidence{observedBoolean("assembly", "SubA", true)}},
			wantFacts: map[string]string{"assembly/SubA/visibility": `{"status":"observed","value":true}`},
		},
		{
			name:   "delete",
			target: contractPackageTarget{"LegA", "part", "delete"},
			want: contractRequestExpectation{
				partMutations:      `{"deletion":[{"object":"LegA"}]}`,
				targetStateRequest: `{"suppression":[],"visibility":[],"existence":[{"destination":"part","object":"LegA"}]}`,
			},
			// Deletion success is confirmed absence, not omitted evidence.
			response:  scriptedTargetState{Existence: []scriptedExistenceEvidence{existenceWithStatus("part", "LegA", "absent")}},
			wantFacts: map[string]string{"part/LegA/existence": `{"status":"absent"}`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contractPackageEnv(t)
			projectDir := writeContractPackageFixture(t, []contractPackageTarget{tc.target})
			response := scripted(t, tc.response)
			run := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "out"), "success", response)
			if len(run.invocations) != 1 {
				t.Fatalf("runtime invoked %d times (err=%v)", len(run.invocations), run.err)
			}
			attempt := assertContractRequest(t, projectDir, run, run.invocations[0], tc.want)
			assertContractSuccessPackage(t, run, attempt, *response, tc.wantFacts)
		})
	}
}

// contractCombinedTargets declares every family on both destinations, with
// same-family targets out of lexical order, so canonical ordering is real.
func contractCombinedTargets() []contractPackageTarget {
	return []contractPackageTarget{
		{"LegSuppressB", "part", "suppress"},
		{"SubDelete", "assembly", "delete"},
		{"LegShow", "part", "unhide"},
		{"LegSuppressA", "part", "suppress"},
		{"SubUnsuppress", "assembly", "unsuppress"},
		{"LegHide", "part", "hide"},
		{"SubHide", "assembly", "hide"},
		{"LegDelete", "part", "delete"},
	}
}

func contractCombinedExpectation() contractRequestExpectation {
	return contractRequestExpectation{
		partMutations: `{
			"suppression":[{"object":"LegSuppressA","suppressed":true},{"object":"LegSuppressB","suppressed":true}],
			"visibility":[{"object":"LegHide","visible":false},{"object":"LegShow","visible":true}],
			"deletion":[{"object":"LegDelete"}]}`,
		assemblyMutations: `{
			"suppression":[{"object":"SubUnsuppress","suppressed":false}],
			"visibility":[{"object":"SubHide","visible":false}],
			"deletion":[{"object":"SubDelete"}]}`,
		targetStateRequest: `{
			"suppression":[{"destination":"assembly","object":"SubUnsuppress"},{"destination":"part","object":"LegSuppressA"},{"destination":"part","object":"LegSuppressB"}],
			"visibility":[{"destination":"assembly","object":"SubHide"},{"destination":"part","object":"LegHide"},{"destination":"part","object":"LegShow"}],
			"existence":[{"destination":"assembly","object":"SubDelete"},{"destination":"part","object":"LegDelete"}]}`,
	}
}

// contractCombinedResponse is the runtime's native evidence, deliberately in
// non-canonical order: Engine must match it by target identity.
func contractCombinedResponse() scriptedTargetState {
	return scriptedTargetState{
		Suppression: []scriptedBooleanEvidence{
			observedBoolean("part", "LegSuppressB", true),
			observedBoolean("assembly", "SubUnsuppress", false),
			observedBoolean("part", "LegSuppressA", true),
		},
		Visibility: []scriptedBooleanEvidence{
			observedBoolean("part", "LegShow", true),
			observedBoolean("assembly", "SubHide", false),
			observedBoolean("part", "LegHide", false),
		},
		Existence: []scriptedExistenceEvidence{
			existenceWithStatus("part", "LegDelete", "absent"),
			existenceWithStatus("assembly", "SubDelete", "absent"),
		},
	}
}

func contractCombinedFacts() map[string]string {
	return map[string]string{
		"part/LegSuppressA/suppression":      `{"status":"observed","value":true}`,
		"part/LegSuppressB/suppression":      `{"status":"observed","value":true}`,
		"assembly/SubUnsuppress/suppression": `{"status":"observed","value":false}`,
		"part/LegHide/visibility":            `{"status":"observed","value":false}`,
		"part/LegShow/visibility":            `{"status":"observed","value":true}`,
		"assembly/SubHide/visibility":        `{"status":"observed","value":false}`,
		"part/LegDelete/existence":           `{"status":"absent"}`,
		"assembly/SubDelete/existence":       `{"status":"absent"}`,
	}
}

func TestContractPackage_CombinedMutationFamiliesSuccess(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, contractCombinedTargets())
	response := scripted(t, contractCombinedResponse())
	run := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "out"), "success", response)
	if len(run.invocations) != 1 {
		t.Fatalf("runtime invoked %d times (err=%v)", len(run.invocations), run.err)
	}
	attempt := assertContractRequest(t, projectDir, run, run.invocations[0], contractCombinedExpectation())
	assertContractSuccessPackage(t, run, attempt, *response, contractCombinedFacts())
}

// Requested mutation != observed evidence: one fixed request, two runtime
// responses. Only the runtime's evidence changes, and so does the outcome.
func TestContractPackage_ObservedStateComesFromRuntimeEvidence(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, []contractPackageTarget{{"LegA", "part", "suppress"}})
	want := contractRequestExpectation{
		partMutations:      `{"suppression":[{"object":"LegA","suppressed":true}]}`,
		targetStateRequest: `{"suppression":[{"destination":"part","object":"LegA"}],"visibility":[],"existence":[]}`,
	}
	agreeing := scripted(t, scriptedTargetState{Suppression: []scriptedBooleanEvidence{observedBoolean("part", "LegA", true)}})
	disagreeing := scripted(t, scriptedTargetState{Suppression: []scriptedBooleanEvidence{observedBoolean("part", "LegA", false)}})

	pass := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "pass"), "success", agreeing)
	passAttempt := assertContractRequest(t, projectDir, pass, pass.invocations[0], want)
	assertContractSuccessPackage(t, pass, passAttempt, *agreeing, map[string]string{"part/LegA/suppression": `{"status":"observed","value":true}`})

	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatal(err)
	}
	fail := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "fail"), "success", disagreeing)
	failAttempt := assertContractRequest(t, projectDir, fail, fail.invocations[0], want)
	assertContractVerificationFailure(t, fail, failAttempt, *disagreeing, verification.FailureClassTargetStateMismatch)

	if pass.invocations[0].ManifestSHA256 != fail.invocations[0].ManifestSHA256 {
		t.Fatal("the two runs did not receive the same runtime manifest")
	}
	if pass.planned.PlanHash != fail.planned.PlanHash {
		t.Fatal("the two runs did not plan identically")
	}
}

func TestContractPackage_VerificationFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		target    contractPackageTarget
		response  *scriptedTargetState // nil: the runtime returns no targetState at all
		wantClass verification.FailureClass
	}{
		{
			name:      "suppression mismatch",
			target:    contractPackageTarget{"LegA", "part", "suppress"},
			response:  &scriptedTargetState{Suppression: []scriptedBooleanEvidence{observedBoolean("part", "LegA", false)}},
			wantClass: verification.FailureClassTargetStateMismatch,
		},
		{
			name:      "unsuppression mismatch",
			target:    contractPackageTarget{"SubA", "assembly", "unsuppress"},
			response:  &scriptedTargetState{Suppression: []scriptedBooleanEvidence{observedBoolean("assembly", "SubA", true)}},
			wantClass: verification.FailureClassTargetStateMismatch,
		},
		{
			name:      "visibility mismatch",
			target:    contractPackageTarget{"LegA", "part", "hide"},
			response:  &scriptedTargetState{Visibility: []scriptedBooleanEvidence{observedBoolean("part", "LegA", true)}},
			wantClass: verification.FailureClassTargetStateMismatch,
		},
		{
			name:      "deletion expected but target still exists",
			target:    contractPackageTarget{"SubA", "assembly", "delete"},
			response:  &scriptedTargetState{Existence: []scriptedExistenceEvidence{existenceWithStatus("assembly", "SubA", "exists")}},
			wantClass: verification.FailureClassTargetStateMismatch,
		},
		{
			name:      "required target evidence omitted",
			target:    contractPackageTarget{"LegA", "part", "hide"},
			response:  &scriptedTargetState{Visibility: []scriptedBooleanEvidence{observedBoolean("part", "Unrequested", false)}},
			wantClass: verification.FailureClassRequiredObservationMissing,
		},
		{
			name:      "target-state observation omitted",
			target:    contractPackageTarget{"LegA", "part", "delete"},
			response:  nil,
			wantClass: verification.FailureClassRequiredObservationMissing,
		},
		{
			name:      "native suppression evidence unavailable",
			target:    contractPackageTarget{"LegA", "part", "suppress"},
			response:  &scriptedTargetState{Suppression: []scriptedBooleanEvidence{booleanWithStatus("part", "LegA", "unavailable")}},
			wantClass: verification.FailureClassNativeEvidenceUnavailable,
		},
		{
			name:      "native existence evidence unavailable",
			target:    contractPackageTarget{"LegA", "part", "delete"},
			response:  &scriptedTargetState{Existence: []scriptedExistenceEvidence{existenceWithStatus("part", "LegA", "unavailable")}},
			wantClass: verification.FailureClassNativeEvidenceUnavailable,
		},
		{
			name:      "target missing",
			target:    contractPackageTarget{"LegA", "part", "unhide"},
			response:  &scriptedTargetState{Visibility: []scriptedBooleanEvidence{booleanWithStatus("part", "LegA", "target_missing")}},
			wantClass: verification.FailureClassTargetMissing,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contractPackageEnv(t)
			projectDir := writeContractPackageFixture(t, []contractPackageTarget{tc.target})
			response, wantObserved := "null", ""
			if tc.response != nil {
				response = tc.response.json(t)
				wantObserved = response
			}
			run := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "out"), "success", &response)
			if len(run.invocations) == 0 {
				t.Fatal("runtime was not invoked")
			}
			attempt := assertContractRequest(t, projectDir, run, run.invocations[0], contractSingleTargetExpectation(t, tc.target))
			assertContractVerificationFailure(t, run, attempt, wantObserved, tc.wantClass)
		})
	}
}

// contractSingleTargetExpectation derives the request expectation for one
// target literally from the documented action vocabulary.
func contractSingleTargetExpectation(t *testing.T, target contractPackageTarget) contractRequestExpectation {
	t.Helper()
	var mutation, family string
	switch target.action {
	case "suppress", "unsuppress":
		mutation = fmt.Sprintf(`{"suppression":[{"object":%q,"suppressed":%t}]}`, target.name, target.action == "suppress")
		family = "suppression"
	case "hide", "unhide":
		mutation = fmt.Sprintf(`{"visibility":[{"object":%q,"visible":%t}]}`, target.name, target.action == "unhide")
		family = "visibility"
	case "delete":
		mutation = fmt.Sprintf(`{"deletion":[{"object":%q}]}`, target.name)
		family = "existence"
	default:
		t.Fatalf("unsupported action %q", target.action)
	}
	request := map[string][]map[string]string{"suppression": {}, "visibility": {}, "existence": {}}
	request[family] = []map[string]string{{"destination": target.kind, "object": target.name}}
	want := contractRequestExpectation{targetStateRequest: compactJSON(t, request)}
	if target.kind == "part" {
		want.partMutations = mutation
	} else {
		want.assemblyMutations = mutation
	}
	return want
}

// Malformed target-state evidence is rejected at evidence intake, before
// Engine semantic verification runs.
func TestContractPackage_MalformedTargetStateEvidenceFailsBeforeVerification(t *testing.T) {
	yes := true
	for _, tc := range []struct {
		name     string
		response scriptedTargetState
	}{
		{"observed status without value", scriptedTargetState{Suppression: []scriptedBooleanEvidence{booleanWithStatus("part", "LegA", "observed")}}},
		{"value with unavailable status", scriptedTargetState{Suppression: []scriptedBooleanEvidence{{Destination: "part", Object: "LegA", Status: "unavailable", Value: &yes}}}},
		{"unknown status", scriptedTargetState{Suppression: []scriptedBooleanEvidence{booleanWithStatus("part", "LegA", "suppressed")}}},
		{"duplicate target identity", scriptedTargetState{Suppression: []scriptedBooleanEvidence{observedBoolean("part", "LegA", true), observedBoolean("part", "LegA", true)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contractPackageEnv(t)
			target := contractPackageTarget{"LegA", "part", "suppress"}
			projectDir := writeContractPackageFixture(t, []contractPackageTarget{target})
			response := scripted(t, tc.response)
			run := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "out"), "success", response)
			if len(run.invocations) != 1 {
				t.Fatalf("runtime invoked %d times, want once (malformed evidence is terminal)", len(run.invocations))
			}
			attempt := assertContractRequest(t, projectDir, run, run.invocations[0], contractSingleTargetExpectation(t, target))

			var consumptionErr *executor.CADRuntimeConsumptionError
			var runErr *cadruntime.FreeCADRuntimeRunError
			var validationErr *observed.ValidationError
			if run.err == nil || !errors.As(run.err, &consumptionErr) || !errors.As(run.err, &runErr) || !errors.As(run.err, &validationErr) ||
				runErr.Stage != cadruntime.FreeCADRuntimeRunStageObservedLoad ||
				consumptionErr.Stage == executor.CADRuntimeConsumptionStageVerificationFailure {
				t.Fatalf("run error = %v, want an observed-evidence validation failure at intake", run.err)
			}
			outcome := failedCADOutcome(t, run.result.Execution)
			if outcome.Verification != artifact.VerificationOutcomeNotRun || outcome.VerificationResult != nil ||
				outcome.VerificationClass != verification.FailureClassNone || outcome.Observed != nil ||
				outcome.Failure.RuntimeNative || outcome.Failure.Code != cadruntime.FreeCADRuntimeRunStageObservedLoad {
				t.Fatalf("outcome = verification %q result %+v class %q failure %+v, want rejected before verification",
					outcome.Verification, outcome.VerificationResult, outcome.VerificationClass, outcome.Failure)
			}
			assertActiveAttemptFiles(t, attempt.workingCopy, contractAttemptSuccessFiles...)
			assertRawObservedTargetState(t, outcome.ObservedPath, *response)

			failure, files := singleFailureRecord(t, contractFailedRun(run))
			assertReportDerivedFailure(t, contractFailedRun(run), failure, files)
			for _, class := range []verification.FailureClass{
				verification.FailureClassTargetStateMismatch, verification.FailureClassRequiredObservationMissing,
				verification.FailureClassNativeEvidenceUnavailable, verification.FailureClassTargetMissing,
			} {
				if run.result.Report.Error.Classification == class || failure.Failure.Code == string(class) {
					t.Fatalf("malformed evidence classified as verification outcome %q", class)
				}
			}
		})
	}
}

func TestContractPackage_RuntimeNativeFailureStaysNative(t *testing.T) {
	contractPackageEnv(t)
	target := contractPackageTarget{"LegA", "part", "hide"}
	projectDir := writeContractPackageFixture(t, []contractPackageTarget{target})
	// A native failure never reaches observation; a scripted response must
	// not be consulted.
	response := scripted(t, scriptedTargetState{Visibility: []scriptedBooleanEvidence{observedBoolean("part", "LegA", true)}})
	run := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "out"), "failure", response)

	var consumptionErr *executor.CADRuntimeConsumptionError
	var runErr *cadruntime.FreeCADRuntimeRunError
	if run.err == nil || !errors.As(run.err, &consumptionErr) || !errors.As(run.err, &runErr) ||
		consumptionErr.Stage != executor.CADRuntimeConsumptionStageRuntimeFailure ||
		runErr.Stage != cadruntime.FreeCADRuntimeRunStageRuntimeReportedFailure {
		t.Fatalf("run error = %v, want a runtime-reported native failure", run.err)
	}
	if len(run.invocations) == 0 {
		t.Fatal("runtime was not invoked")
	}
	for _, inv := range run.invocations {
		assertContractRequest(t, projectDir, run, inv, contractSingleTargetExpectation(t, target))
	}
	last := run.invocations[len(run.invocations)-1]
	outcome := failedCADOutcome(t, run.result.Execution)
	if outcome.ResultPath != last.flag(t, "--result") || outcome.Attempt != len(run.invocations) {
		t.Fatalf("outcome attempt %d / result %q is not the last of %d invocations", outcome.Attempt, outcome.ResultPath, len(run.invocations))
	}
	if !outcome.Failure.RuntimeNative || outcome.Failure.Code != "controlled_failure" || outcome.Failure.Boundary != "freecad" ||
		outcome.Failure.Class != "runtime" || outcome.Verification != artifact.VerificationOutcomeNotRun ||
		outcome.VerificationResult != nil || outcome.VerificationClass != verification.FailureClassNone || outcome.Observed != nil {
		t.Fatalf("outcome = verification %q result %+v failure %+v, want a native runtime failure", outcome.Verification, outcome.VerificationResult, outcome.Failure)
	}
	requestOnly := append(append([]string(nil), contractAttemptRequestFiles...), "prm.result.json")
	assertActiveAttemptFiles(t, outcome.WorkingCopyDir, requestOnly...)

	failure, files := singleFailureRecord(t, contractFailedRun(run))
	rawResult, err := os.ReadFile(outcome.ResultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files["raw/runtime/prm.result.json"], rawResult) {
		t.Fatal("authoritative raw prm.result.json was not preserved byte-for-byte")
	}
	f := failure.Failure
	if f.Code != "controlled_failure" || f.Class != recordcontract.FailureClassRuntime || f.Message != "intentional controlled aligned runtime failure" ||
		!reflect.DeepEqual(f.Evidence, []recordcontract.FailureEvidence{{SourceKind: "runtime-result", SourceRef: "raw/runtime/prm.result.json", DigestSHA256: sha256HexOf(rawResult)}}) {
		t.Fatalf("failure record = %+v, want the native runtime failure with raw result evidence", f)
	}
	for _, class := range []verification.FailureClass{
		verification.FailureClassTargetStateMismatch, verification.FailureClassRequiredObservationMissing,
		verification.FailureClassNativeEvidenceUnavailable, verification.FailureClassTargetMissing,
	} {
		if run.result.Report.Error.Classification == class || f.Code == string(class) {
			t.Fatalf("native failure classified as verification outcome %q", class)
		}
	}
	assertManifestRawEvidencePaths(t, readCLIRecordPackageManifest(t, recordPackageRoot(run.result.RunRoot)),
		[]string{recordpackage.RawReportContractPath(), "raw/runtime/prm.result.json"})
}

// --- determinism ---

type contractDeterminismSnapshot struct {
	planHash, jobID, attemptID string
	attemptManifest            []byte
	productManifest            []byte
	observationRequest         []byte
	verification               *verification.Result
	packageFiles               map[string][]byte
}

func snapshotContractRun(t *testing.T, run contractPackageRun) contractDeterminismSnapshot {
	t.Helper()
	if run.err != nil {
		t.Fatalf("run failed: %v", run.err)
	}
	outcome := singleCADRuntimeOutcome(t, run.result.Execution)
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return contractDeterminismSnapshot{
		planHash:           run.planned.PlanHash,
		jobID:              outcome.JobID,
		attemptID:          outcome.AttemptID,
		attemptManifest:    read(run.invocations[0].flag(t, "--manifest")),
		productManifest:    read(filepath.Join(run.result.RunRoot, "products", "Box", "prm.export-manifest.json")),
		observationRequest: read(outcome.ObservationRequestPath),
		verification:       outcome.VerificationResult,
		packageFiles:       packageFilesFor(t, run.result),
	}
}

// Equivalent combined runs produce identical Engine-owned surfaces. The same
// output root makes attempt-local absolute paths equal too, so every
// normalized record, the package manifest, and the observation request are
// byte-identical; in a different output root only run-location material
// (absolute attempt paths and the digests/identities that cover them) moves.
func TestContractPackage_EquivalentRunsAreDeterministic(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, contractCombinedTargets())
	response := scripted(t, contractCombinedResponse())
	outDir := filepath.Join(t.TempDir(), "out")

	first := runContractPackage(t, projectDir, outDir, "success", response)
	firstSnapshot := snapshotContractRun(t, first)
	for _, path := range []string{".cache", first.result.RunRoot} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	second := runContractPackage(t, projectDir, outDir, "success", response)
	secondSnapshot := snapshotContractRun(t, second)
	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatal(err)
	}
	relocated := runContractPackage(t, projectDir, filepath.Join(t.TempDir(), "elsewhere"), "success", response)
	relocatedSnapshot := snapshotContractRun(t, relocated)

	for name, other := range map[string]contractDeterminismSnapshot{"same root": secondSnapshot, "relocated": relocatedSnapshot} {
		if other.planHash != firstSnapshot.planHash || other.jobID != firstSnapshot.jobID || other.attemptID != firstSnapshot.attemptID {
			t.Fatalf("%s: plan/job/attempt identity drifted: %s/%s/%s vs %s/%s/%s", name,
				other.planHash, other.jobID, other.attemptID, firstSnapshot.planHash, firstSnapshot.jobID, firstSnapshot.attemptID)
		}
		if !bytes.Equal(other.attemptManifest, firstSnapshot.attemptManifest) || !bytes.Equal(other.productManifest, firstSnapshot.productManifest) {
			t.Fatalf("%s: canonical runtime manifest bytes drifted", name)
		}
		if !reflect.DeepEqual(other.verification, firstSnapshot.verification) {
			t.Fatalf("%s: semantic verification result drifted: %+v vs %+v", name, other.verification, firstSnapshot.verification)
		}
		assertSamePackageFileSet(t, firstSnapshot.packageFiles, other.packageFiles)
	}

	// Same output root: all Engine-owned package material is byte-identical.
	if !bytes.Equal(secondSnapshot.observationRequest, firstSnapshot.observationRequest) {
		t.Fatal("observation request bytes drifted between equivalent runs")
	}
	for _, path := range deterministicRecordPackagePaths(t, firstSnapshot.packageFiles) {
		assertPackageFileBytesEqual(t, path, firstSnapshot.packageFiles[path], secondSnapshot.packageFiles[path])
	}
	// The packaged Engine-owned request is stable too. Runtime-produced raw
	// evidence (prm.result.json, prm.observed.json) carries no such promise
	// and is deliberately not compared.
	assertPackageFileBytesEqual(t, "raw/verification/prm.verification.json",
		firstSnapshot.packageFiles["raw/verification/prm.verification.json"], secondSnapshot.packageFiles["raw/verification/prm.verification.json"])

	// Relocated: the request differs only by the attempt's absolute location,
	// and the execution/artifact records and target-state facts are unchanged.
	firstRoot, relocatedRoot := first.result.RunRoot, relocated.result.RunRoot
	if got := strings.ReplaceAll(string(relocatedSnapshot.observationRequest), relocatedRoot, firstRoot); got != string(firstSnapshot.observationRequest) {
		t.Fatalf("relocated observation request differs beyond the run location:\n%s\n%s", got, firstSnapshot.observationRequest)
	}
	for path := range firstSnapshot.packageFiles {
		if path == recordpackage.MustRecordContractPath("execution") || strings.HasPrefix(path, "records/artifacts/") {
			assertPackageFileBytesEqual(t, path, firstSnapshot.packageFiles[path], relocatedSnapshot.packageFiles[path])
		}
	}
	var firstObservation, relocatedObservation recordcontract.ObservationRecord
	decodePackageRecord(t, firstSnapshot.packageFiles, recordpackage.MustRecordContractPath("observation"), &firstObservation)
	decodePackageRecord(t, relocatedSnapshot.packageFiles, recordpackage.MustRecordContractPath("observation"), &relocatedObservation)
	if !reflect.DeepEqual(targetStateFacts(firstObservation), targetStateFacts(relocatedObservation)) ||
		!reflect.DeepEqual(targetStateFacts(firstObservation), contractCombinedFacts()) {
		t.Fatalf("relocated target-state facts drifted: %v vs %v", targetStateFacts(relocatedObservation), targetStateFacts(firstObservation))
	}
	firstManifest := readCLIRecordPackageManifest(t, recordPackageRoot(firstRoot))
	relocatedManifest := readCLIRecordPackageManifest(t, recordPackageRoot(relocatedRoot))
	if len(firstManifest.Records) != len(relocatedManifest.Records) {
		t.Fatal("relocated package manifest record count drifted")
	}
	for i := range firstManifest.Records {
		a, b := firstManifest.Records[i], relocatedManifest.Records[i]
		if a.Family != b.Family || a.ContractPath != b.ContractPath || a.RecordKey != b.RecordKey {
			t.Fatalf("relocated package manifest entry %d drifted: %+v vs %+v", i, b, a)
		}
	}
	if !reflect.DeepEqual(firstManifest.RawEvidence, relocatedManifest.RawEvidence) {
		t.Fatal("relocated raw evidence index drifted")
	}
}
