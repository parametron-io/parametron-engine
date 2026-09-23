package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/scheduler"
	"parametron/internal/engine/verification"
	"parametron/internal/shared/config"
)

// Issue #1: a real normal run (planner -> scheduler -> executor -> aligned
// runtime -> recordemit -> recordpackage) emits the normalized artifact,
// observation and verification records, and the target-state material flows
// through those same families. The runtime is the controlled aligned runtime
// installed by installControlledAlignedRuntime; no FreeCAD is required.

func packageFilesFor(t *testing.T, run *executionResult) map[string][]byte {
	t.Helper()
	return readRecordPackageFiles(t, recordPackageRoot(run.RunRoot))
}

func sha256HexOf(content []byte) string { return fmt.Sprintf("%x", sha256.Sum256(content)) }

// runRootArtifacts reads the artifacts the run persisted in its own artifact
// store manifest, independently of the record package.
func runRootArtifacts(t *testing.T, runRoot string) []artifact.Artifact {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runRoot, "manifest.json"))
	if err != nil {
		t.Fatalf("read run-root artifact manifest: %v", err)
	}
	var manifest artifact.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode run-root artifact manifest: %v", err)
	}
	return manifest.Artifacts
}

// singleCADRuntimeOutcome returns the one passed CAD runtime outcome of a run.
func singleCADRuntimeOutcome(t *testing.T, execution scheduler.ExecutionResult) *executor.CADRuntimeOutcome {
	t.Helper()
	var found *executor.CADRuntimeOutcome
	for _, job := range execution.Jobs {
		if job.CADRuntimeOutcome != nil {
			if found != nil {
				t.Fatal("expected exactly one CAD runtime outcome")
			}
			found = job.CADRuntimeOutcome
		}
	}
	if found == nil {
		t.Fatal("run produced no CAD runtime outcome")
	}
	return found
}

func manifestFamilies(manifest cliRecordPackageManifest) map[string][]cliRecordPackageManifestRecord {
	out := map[string][]cliRecordPackageManifestRecord{}
	for _, record := range manifest.Records {
		out[record.Family] = append(out[record.Family], record)
	}
	return out
}

func decodePackageRecord(t *testing.T, files map[string][]byte, contractPath string, out any) {
	t.Helper()
	content, ok := files[contractPath]
	if !ok {
		t.Fatalf("package missing %q; has %v", contractPath, sortedPackagePaths(files))
	}
	if err := json.Unmarshal(content, out); err != nil {
		t.Fatalf("decode %q: %v", contractPath, err)
	}
}

func evidenceRefDigest(refs []recordcontract.EvidenceReference, kind, ref string) (string, bool) {
	for _, item := range refs {
		if item.Kind == kind && item.Ref == ref {
			return item.DigestSHA256, true
		}
	}
	return "", false
}

func TestCLIProjectRun_NormalRunEmitsArtifactObservationAndVerificationRecords(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	run := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out"))
	files := packageFilesFor(t, run.result)
	manifest := readCLIRecordPackageManifest(t, recordPackageRoot(run.result.RunRoot))
	families := manifestFamilies(manifest)
	wantPackageKey := "engine-run:" + run.planned.PlanHash

	for _, family := range []string{"execution", "observation", "verification"} {
		if len(families[family]) != 1 {
			t.Fatalf("manifest %s records = %#v, want exactly one", family, families[family])
		}
	}
	if len(families["reference"]) != 0 || len(families["failure"]) != 0 {
		t.Fatalf("unexpected reference/failure records in a successful run without traversal: %#v", families)
	}

	// Every artifact registered by the run has its own identity-addressed record.
	stored := runRootArtifacts(t, run.result.RunRoot)
	if len(stored) < 2 {
		t.Fatalf("fixture must register several artifacts, got %d", len(stored))
	}
	artifactEntries := families["artifact"]
	if len(artifactEntries) != len(stored) {
		t.Fatalf("manifest artifact records = %d, run registered %d artifacts", len(artifactEntries), len(stored))
	}
	byKey := map[string]cliRecordPackageManifestRecord{}
	var identities []string
	for _, entry := range artifactEntries {
		byKey[entry.RecordKey] = entry
		identities = append(identities, entry.IdentityID)
		if want := "records/artifacts/" + entry.IdentityID + "/parametron.artifact-record.json"; entry.ContractPath != want {
			t.Fatalf("artifact entry %#v path != identity-addressed %q", entry, want)
		}
		assertPackageFileExists(t, recordPackageRoot(run.result.RunRoot), entry.ContractPath)
	}
	if !sort.StringsAreSorted(identities) {
		t.Fatalf("artifact manifest entries are not ordered by identity: %v", identities)
	}
	for _, item := range stored {
		entry, ok := byKey[wantPackageKey+":artifact:"+item.ID]
		if !ok {
			t.Fatalf("no artifact record for stored artifact %q (%s); records = %v", item.ID, item.Filename, byKey)
		}
		var record recordcontract.ArtifactRecord
		decodePackageRecord(t, files, entry.ContractPath, &record)
		if err := recordcontract.ValidateArtifactRecord(record); err != nil {
			t.Fatalf("artifact record %q invalid: %v", entry.ContractPath, err)
		}
		if record.Artifact.Filename != item.Filename || record.Artifact.ChecksumSHA256 != item.ChecksumSHA256 ||
			record.Artifact.Linkage.JobID != item.JobID || record.Artifact.Linkage.ProductKey != item.ProductID || record.Artifact.Linkage.StepRef != item.StepID {
			t.Fatalf("artifact record %+v does not carry stored artifact %+v", record.Artifact, item)
		}
	}
	// The superseded singleton artifact path is never written, even though
	// several (and, in other runs, exactly one) artifact records exist.
	if _, ok := files["records/parametron.artifact-record.json"]; ok {
		t.Fatal("superseded singleton artifact record path was written")
	}

	// Raw evidence: packaged bytes equal the attempt's original files, and the
	// normalized records reference their digests.
	outcome := singleCADRuntimeOutcome(t, run.result.Execution)
	for _, source := range []struct{ contractPath, runPath string }{
		{recordpackage.RawObservedContractPath(), outcome.ObservedPath},
		{recordpackage.RawVerificationContractPath(), outcome.ObservationRequestPath},
		{recordpackage.RawRuntimeResultContractPath(), outcome.ResultPath},
	} {
		original, err := os.ReadFile(source.runPath)
		if err != nil {
			t.Fatalf("read attempt evidence %q: %v", source.runPath, err)
		}
		if !bytes.Equal(files[source.contractPath], original) {
			t.Fatalf("raw evidence %q differs from the runtime's original bytes", source.contractPath)
		}
	}
	rawArtifactManifest, err := os.ReadFile(filepath.Join(run.result.RunRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files[recordpackage.RawArtifactStoreManifestContractPath()], rawArtifactManifest) {
		t.Fatal("raw artifact-store manifest was not preserved byte-for-byte")
	}

	var observation recordcontract.ObservationRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("observation"), &observation)
	if err := recordcontract.ValidateObservationRecord(observation); err != nil {
		t.Fatalf("observation record invalid: %v", err)
	}
	if observation.RecordKey != wantPackageKey+":observation" || len(observation.Observation.Facts) == 0 {
		t.Fatalf("observation record key/facts = %q/%d", observation.RecordKey, len(observation.Observation.Facts))
	}
	observedDigest := sha256HexOf(files[recordpackage.RawObservedContractPath()])
	if got, ok := evidenceRefDigest(observation.Provenance.Evidence, "observed", recordpackage.RawObservedContractPath()); !ok || got != observedDigest {
		t.Fatalf("observation provenance digest = %q (present=%t), want digest of preserved raw observed %q", got, ok, observedDigest)
	}
	for _, fact := range observation.Observation.Facts {
		if fact.Linkage.JobID != outcome.JobID || fact.Linkage.ProductKey != outcome.ProductKey || fact.Linkage.StepRef != outcome.StepID {
			t.Fatalf("observation fact linkage = %+v, want outcome %s/%s/%s", fact.Linkage, outcome.JobID, outcome.ProductKey, outcome.StepID)
		}
	}

	var verificationRecord recordcontract.VerificationRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("verification"), &verificationRecord)
	if err := recordcontract.ValidateVerificationRecord(verificationRecord); err != nil {
		t.Fatalf("verification record invalid: %v", err)
	}
	if verificationRecord.RecordKey != wantPackageKey+":verification" || verificationRecord.Verification.Outcome != recordcontract.VerificationOutcomePass {
		t.Fatalf("verification record key/outcome = %q/%q", verificationRecord.RecordKey, verificationRecord.Verification.Outcome)
	}
	requestDigest := sha256HexOf(files[recordpackage.RawVerificationContractPath()])
	if got, ok := evidenceRefDigest(verificationRecord.Provenance.Evidence, "verification", recordpackage.RawVerificationContractPath()); !ok || got != requestDigest {
		t.Fatalf("verification provenance digest = %q (present=%t), want digest of preserved raw request %q", got, ok, requestDigest)
	}
	// prm.verification.json is the request; the verification record reports the
	// in-memory result (pass), not anything decoded from the request file.
	if bytes.Contains(files[recordpackage.MustRecordContractPath("verification")], []byte(`"expected"`)) {
		t.Fatal("verification record must not embed the raw verification request contract")
	}

	// Manifest ordering: registry family order, artifact records by identity.
	assertManifestFamilyOrdering(t, manifest)
}

func TestCLIProjectRun_TargetStateMaterialFlowsThroughExistingRecordFamilies(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeTask13TargetMutationHandoffFixture(t)
	installControlledAlignedRuntime(t, "success")
	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun: %v", err)
	}
	result, err := executePlanRun(executionOptions{Planned: planned, OutputDir: filepath.Join(t.TempDir(), "out"), UseCache: true})
	if err != nil {
		t.Fatalf("executePlanRun: %v", err)
	}
	files := packageFilesFor(t, result)
	manifest := readCLIRecordPackageManifest(t, recordPackageRoot(result.RunRoot))
	families := manifestFamilies(manifest)

	// No target-state-specific family or file: only the existing families.
	for family := range families {
		switch family {
		case "execution", "artifact", "observation", "verification":
		default:
			t.Fatalf("unexpected record family %q in target-state run", family)
		}
	}
	for path := range files {
		if strings.Contains(strings.ToLower(path), "target") {
			t.Fatalf("unexpected target-state-specific package file %q", path)
		}
	}
	if len(families["artifact"]) < 2 || len(families["observation"]) != 1 || len(families["verification"]) != 1 {
		t.Fatalf("manifest families = %#v", families)
	}

	// The raw observed evidence carries the target-state material the runtime
	// reported, and it is preserved verbatim.
	rawObserved := files[recordpackage.RawObservedContractPath()]
	var decoded observed.Observed
	if err := json.Unmarshal(rawObserved, &decoded); err != nil || decoded.Observation.TargetState == nil {
		t.Fatalf("raw observed evidence lacks target-state material (err=%v): %s", err, rawObserved)
	}
	outcome := singleCADRuntimeOutcome(t, result.Execution)
	original, err := os.ReadFile(outcome.ObservedPath)
	if err != nil || !bytes.Equal(rawObserved, original) {
		t.Fatalf("raw observed evidence differs from the runtime's original bytes (err=%v)", err)
	}

	var observation recordcontract.ObservationRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("observation"), &observation)
	facts := map[string]string{}
	for _, fact := range observation.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindTargetState {
			facts[fact.Subject.ID+"/"+fact.Subject.Name+"/"+fact.Key] = fact.Value.Raw
		}
	}
	wantFacts := map[string]string{}
	for _, entry := range decoded.Observation.TargetState.Suppression {
		wantFacts[entry.Destination+"/"+entry.Object+"/suppression"] = `{"status":"observed","value":true}`
	}
	for _, entry := range decoded.Observation.TargetState.Visibility {
		wantFacts[entry.Destination+"/"+entry.Object+"/visibility"] = `{"status":"observed","value":false}`
	}
	for _, entry := range decoded.Observation.TargetState.Existence {
		wantFacts[entry.Destination+"/"+entry.Object+"/existence"] = `{"status":"absent"}`
	}
	if len(wantFacts) != 8 {
		t.Fatalf("fixture must request four suppression, two visibility and two existence targets, got %v", wantFacts)
	}
	for key, want := range wantFacts {
		if facts[key] != want {
			t.Fatalf("target-state fact %q = %q, want %q (all facts: %v)", key, facts[key], want, facts)
		}
	}
	if len(facts) != len(wantFacts) {
		t.Fatalf("target-state facts = %v, want exactly %v", facts, wantFacts)
	}

	var verificationRecord recordcontract.VerificationRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("verification"), &verificationRecord)
	var category *recordcontract.VerificationCategoryResult
	for i := range verificationRecord.Verification.Categories {
		if verificationRecord.Verification.Categories[i].Category == recordcontract.VerificationCategoryTargetState {
			category = &verificationRecord.Verification.Categories[i]
		}
	}
	if category == nil || !category.Enabled || category.Outcome != recordcontract.VerificationOutcomePass {
		t.Fatalf("target-state verification category = %+v, want enabled pass", category)
	}
	if verificationRecord.Verification.Outcome != recordcontract.VerificationOutcomePass {
		t.Fatalf("verification outcome = %q", verificationRecord.Verification.Outcome)
	}
	if got, ok := evidenceRefDigest(verificationRecord.Provenance.Evidence, "observed", recordpackage.RawObservedContractPath()); !ok || got != sha256HexOf(rawObserved) {
		t.Fatalf("target-state verification observed digest = %q (present=%t), want %q", got, ok, sha256HexOf(rawObserved))
	}
}

func TestCLIProjectRun_ReferenceTraversalCoexistsWithNormalizedRuntimeRecords(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	traversal := validReferenceTraversalJSONFixture()
	run := runCLIExecutionForProjectWithReferenceTraversal(t, projectDir, filepath.Join(t.TempDir(), "out"), traversal)
	files := packageFilesFor(t, run.result)
	manifest := readCLIRecordPackageManifest(t, recordPackageRoot(run.result.RunRoot))
	families := manifestFamilies(manifest)

	for family, want := range map[string]int{"execution": 1, "observation": 1, "verification": 1, "reference": 1} {
		if len(families[family]) != want {
			t.Fatalf("manifest %s records = %#v, want %d", family, families[family], want)
		}
	}
	if len(families["artifact"]) < 2 {
		t.Fatalf("manifest artifact records = %#v", families["artifact"])
	}
	if !bytes.Equal(files[recordpackage.RawRuntimeReferenceTraversalContractPath()], traversal) {
		t.Fatal("raw reference traversal evidence was not preserved byte-for-byte")
	}
	var reference recordcontract.ReferenceRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("reference"), &reference)
	if err := recordcontract.ValidateReferenceRecord(reference); err != nil || len(reference.Reference.Edges) != 1 {
		t.Fatalf("reference record invalid or wrong edge count (err=%v): %#v", err, reference.Reference.Edges)
	}
	assertManifestFamilyOrdering(t, manifest)
}

func TestCLIProjectRun_NonCADRunEmitsNoObservationOrVerificationRecords(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)
	unsetEnvForTest(t, config.FreeCADRuntimeEnv)

	dslPath := writeNoAdapterOnlyProject(t)
	planned, err := loadPlannedRun(dslPath, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun: %v", err)
	}
	result, err := executePlanRun(executionOptions{Planned: planned, OutputDir: filepath.Join(t.TempDir(), "out"), UseCache: true})
	if err != nil {
		t.Fatalf("executePlanRun: %v", err)
	}
	files := packageFilesFor(t, result)
	manifest := readCLIRecordPackageManifest(t, recordPackageRoot(result.RunRoot))
	families := manifestFamilies(manifest)

	if len(families["execution"]) != 1 {
		t.Fatalf("manifest execution records = %#v", families["execution"])
	}
	for _, family := range []string{"observation", "verification", "reference", "failure"} {
		if len(families[family]) != 0 {
			t.Fatalf("%s record synthesized without CAD runtime evidence: %#v", family, families[family])
		}
	}
	for _, contractPath := range []string{
		recordpackage.RawObservedContractPath(),
		recordpackage.RawVerificationContractPath(),
		recordpackage.RawRuntimeResultContractPath(),
		recordpackage.RawRuntimeReferenceTraversalContractPath(),
	} {
		if _, ok := files[contractPath]; ok {
			t.Fatalf("raw runtime evidence %q present for a non-CAD run", contractPath)
		}
	}
	// Artifact records still follow the artifacts the run registered.
	if want := len(runRootArtifacts(t, result.RunRoot)); len(families["artifact"]) != want {
		t.Fatalf("manifest artifact records = %d, run registered %d", len(families["artifact"]), want)
	}
	for _, entry := range families["artifact"] {
		if want := "records/artifacts/" + entry.IdentityID + "/parametron.artifact-record.json"; entry.ContractPath != want {
			t.Fatalf("artifact entry %#v path != %q", entry, want)
		}
	}
}

func TestCLIProjectRun_EquivalentRunsInDifferentOutputRootsKeepArtifactAndExecutionRecordsStable(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	first := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out-a"))
	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatal(err)
	}
	second := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out-b"))

	filesA, filesB := packageFilesFor(t, first.result), packageFilesFor(t, second.result)
	assertSamePackageFileSet(t, filesA, filesB)

	compared := 0
	for path := range filesA {
		if path == recordpackage.MustRecordContractPath("execution") || strings.HasPrefix(path, "records/artifacts/") {
			assertPackageFileBytesEqual(t, path, filesA[path], filesB[path])
			compared++
		}
	}
	if compared < 3 {
		t.Fatalf("compared only %d execution/artifact records", compared)
	}
	if first.planned.PlanHash != second.planned.PlanHash {
		t.Fatalf("plan hashes differ: %q vs %q", first.planned.PlanHash, second.planned.PlanHash)
	}
}

// --- cadRuntimeRunEvidence typed-value transport (helper level) ---

func TestCADRuntimeRunEvidence_CarriesTypedValuesAndLinkageFromOutcome(t *testing.T) {
	outcome := cadRuntimeEvidenceOutcome(t, "job-1", "widget", "2", []byte("r"), []byte("v"), []byte("o"))
	obs := &observed.Observed{SchemaVersion: observed.SchemaVersion}
	result := &verification.Result{Status: verification.StatusPass}
	outcome.Observed, outcome.VerificationResult = obs, result

	got, err := cadRuntimeRunEvidence(scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}}, true)
	if err != nil || got == nil {
		t.Fatalf("evidence=%#v err=%v", got, err)
	}
	if got.ObservedValue != obs || got.VerificationResult != result {
		t.Fatalf("typed values not carried: observed=%p result=%p", got.ObservedValue, got.VerificationResult)
	}
	if got.JobID != "job-1" || got.ProductKey != "widget" || got.StepRef != "2" {
		t.Fatalf("linkage = %q/%q/%q, want job-1/widget/2", got.JobID, got.ProductKey, got.StepRef)
	}
}

func TestCADRuntimeRunEvidence_AbsentTypedValuesStayNil(t *testing.T) {
	outcome := cadRuntimeEvidenceOutcome(t, "job-1", "widget", "2", []byte("r"), nil, nil)
	got, err := cadRuntimeRunEvidence(scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}}, true)
	if err != nil || got == nil {
		t.Fatalf("evidence=%#v err=%v", got, err)
	}
	if got.ObservedValue != nil || got.VerificationResult != nil {
		t.Fatalf("typed values synthesized from absent evidence: observed=%v result=%v", got.ObservedValue, got.VerificationResult)
	}
}

// assertManifestFamilyOrdering checks the package manifest's record order: the
// record-contract family order, with artifact records (the only plural family)
// ordered by identity and every artifact path identity-addressed.
func assertManifestFamilyOrdering(t *testing.T, manifest cliRecordPackageManifest) {
	t.Helper()

	familyRank := map[string]int{}
	for i, def := range recordcontract.Definitions() {
		familyRank[string(def.Family)] = i
	}
	for i := 1; i < len(manifest.Records); i++ {
		prev, cur := manifest.Records[i-1], manifest.Records[i]
		if familyRank[prev.Family] > familyRank[cur.Family] {
			t.Fatalf("manifest records not in record-contract family order: %#v", manifest.Records)
		}
		if prev.Family == cur.Family {
			if prev.Family != "artifact" {
				t.Fatalf("non-artifact family %q appears more than once: %#v", prev.Family, manifest.Records)
			}
			if prev.IdentityID > cur.IdentityID {
				t.Fatalf("artifact records not ordered by identity: %#v", manifest.Records)
			}
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Re-emission is an in-process boundary, not a run-root reader: raw attempt
// bytes alone do not make EmitRunPackage re-derive typed records. The raw
// evidence is still preserved verbatim and the report-derived execution record
// is unchanged; the manifest (authoritative) indexes only what was mapped.
func TestRecordEmitRunPackage_ReemissionWithRawBytesOnlyDoesNotReconstructTypedRecords(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	run := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out"))
	packageRoot := recordPackageRoot(run.result.RunRoot)
	before := readRecordPackageFiles(t, packageRoot)

	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  run.result.RunRoot,
		PlanHash: run.planned.PlanHash,
		Report:   run.report,
		Metadata: &run.metadata,
		CADRuntime: &recordemit.CADRuntimeRunEvidence{
			Result:       before[recordpackage.RawRuntimeResultContractPath()],
			Verification: before[recordpackage.RawVerificationContractPath()],
			Observed:     before[recordpackage.RawObservedContractPath()],
		},
	}); err != nil {
		t.Fatalf("EmitRunPackage(raw bytes only) failed: %v", err)
	}

	after := readRecordPackageFiles(t, packageRoot)
	for _, path := range []string{
		recordpackage.MustRecordContractPath("execution"),
		recordpackage.RawReportContractPath(),
		recordpackage.RawMetadataContractPath(),
		recordpackage.RawArtifactStoreManifestContractPath(),
		recordpackage.RawObservedContractPath(),
		recordpackage.RawVerificationContractPath(),
		recordpackage.RawRuntimeResultContractPath(),
	} {
		assertPackageFileBytesEqual(t, path, before[path], after[path])
	}
	families := manifestFamilies(readCLIRecordPackageManifest(t, packageRoot))
	if len(families["execution"]) != 1 {
		t.Fatalf("manifest execution records = %#v", families["execution"])
	}
	for _, family := range []string{"artifact", "observation", "verification"} {
		if len(families[family]) != 0 {
			t.Fatalf("%s records reconstructed from raw bytes / run-root state: %#v", family, families[family])
		}
	}
}
