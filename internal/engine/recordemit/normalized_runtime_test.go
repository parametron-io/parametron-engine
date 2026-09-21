package recordemit_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/verification"
)

// Issue #1: normal-run emission carries the existing Engine-owned artifact,
// observation and verification mappings into the standard record package.
// These tests drive recordemit.EmitRunPackage and inspect the written package.

const artifactRecordFile = "parametron.artifact-record.json"

func testArtifact(id string, typ artifact.ArtifactType, filename, checksum string) artifact.Artifact {
	return artifact.Artifact{
		ID:             id,
		Class:          artifact.ArtifactClassExecutionOutput,
		Type:           typ,
		Path:           "products/Widget/" + filename,
		Filename:       filename,
		SizeBytes:      64,
		ChecksumSHA256: checksum,
		JobID:          "job-1",
		ProductID:      "Widget",
		StepID:         "step-export",
	}
}

func digestOf(fill string) string { return strings.Repeat(fill, 64) }

func sha256Hex(content []byte) string { return fmt.Sprintf("%x", sha256.Sum256(content)) }

func emitAndReadPackage(t *testing.T, input recordemit.RunEmitInput) (string, map[string][]byte) {
	t.Helper()
	if input.RunRoot == "" {
		input.RunRoot = t.TempDir()
	}
	if err := recordemit.EmitRunPackage(input); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}
	packageRoot := filepath.Join(input.RunRoot, recordpackage.PackageDirectoryName)
	files := map[string][]byte{}
	err := filepath.WalkDir(packageRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(packageRoot, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		files[filepath.ToSlash(rel)] = content
		return err
	})
	if err != nil {
		t.Fatalf("walk package: %v", err)
	}
	return packageRoot, files
}

func manifestRecordsOf(t *testing.T, files map[string][]byte, family string) []emitManifestRecord {
	t.Helper()
	var manifest emitManifest
	if err := json.Unmarshal(files[recordpackage.PackageManifestContractPath()], &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	var out []emitManifestRecord
	for _, record := range manifest.Records {
		if record.Family == family {
			out = append(out, record)
		}
	}
	return out
}

func artifactRecordPaths(files map[string][]byte) []string {
	var out []string
	for path := range files {
		if strings.HasSuffix(path, "/"+artifactRecordFile) || path == "records/"+artifactRecordFile {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func TestEmitRunPackage_Artifacts_SingleArtifactUsesIdentityAddressedPath(t *testing.T) {
	planHash := "single-artifact-plan"
	item := testArtifact("art-step", artifact.ArtifactTypeSTEP, "Widget.step", digestOf("a"))

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash:  planHash,
		Report:    syntheticSuccessReport(planHash),
		Metadata:  syntheticMetadata(planHash),
		Artifacts: []artifact.Artifact{item},
	})

	entries := manifestRecordsOf(t, files, "artifact")
	if len(entries) != 1 {
		t.Fatalf("manifest artifact records = %#v, want exactly one", entries)
	}
	wantPath := "records/artifacts/" + entries[0].IdentityID + "/" + artifactRecordFile
	if entries[0].ContractPath != wantPath {
		t.Fatalf("artifact record path = %q, want identity-addressed %q", entries[0].ContractPath, wantPath)
	}
	if got := artifactRecordPaths(files); !reflect.DeepEqual(got, []string{wantPath}) {
		t.Fatalf("artifact record files = %v, want only %v (no singleton fallback)", got, []string{wantPath})
	}
	if wantKey := "engine-run:" + planHash + ":artifact:art-step"; entries[0].RecordKey != wantKey {
		t.Fatalf("artifact record key = %q, want %q", entries[0].RecordKey, wantKey)
	}

	var record recordcontract.ArtifactRecord
	if err := json.Unmarshal(files[wantPath], &record); err != nil {
		t.Fatalf("decode artifact record: %v", err)
	}
	if err := recordcontract.ValidateArtifactRecord(record); err != nil {
		t.Fatalf("emitted artifact record invalid: %v", err)
	}
	if record.Artifact.Filename != "Widget.step" || record.Artifact.ChecksumSHA256 != digestOf("a") || record.Artifact.Type != recordcontract.ArtifactTypeSTEP {
		t.Fatalf("artifact summary = %+v does not carry the mapped artifact", record.Artifact)
	}
	if record.Artifact.Linkage.JobID != "job-1" || record.Artifact.Linkage.ProductKey != "Widget" || record.Artifact.Linkage.StepRef != "step-export" {
		t.Fatalf("artifact linkage = %+v", record.Artifact.Linkage)
	}
}

func TestEmitRunPackage_Artifacts_MultipleArtifactsAllReachPackageInIdentityOrder(t *testing.T) {
	planHash := "multi-artifact-plan"
	items := []artifact.Artifact{
		testArtifact("art-c", artifact.ArtifactTypeCSV, "Widget.csv", digestOf("c")),
		testArtifact("art-a", artifact.ArtifactTypeSTEP, "Widget.step", digestOf("a")),
		testArtifact("art-b", artifact.ArtifactTypeJSON, "manifest.json", digestOf("b")),
	}

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash:  planHash,
		Report:    syntheticSuccessReport(planHash),
		Metadata:  syntheticMetadata(planHash),
		Artifacts: items,
	})

	entries := manifestRecordsOf(t, files, "artifact")
	if len(entries) != len(items) {
		t.Fatalf("manifest artifact records = %d, want %d: %#v", len(entries), len(items), entries)
	}
	var identities, paths []string
	gotKeys := map[string]bool{}
	for _, entry := range entries {
		identities = append(identities, entry.IdentityID)
		paths = append(paths, entry.ContractPath)
		gotKeys[entry.RecordKey] = true
		if want := "records/artifacts/" + entry.IdentityID + "/" + artifactRecordFile; entry.ContractPath != want {
			t.Fatalf("entry %#v path != %q", entry, want)
		}
		if _, ok := files[entry.ContractPath]; !ok {
			t.Fatalf("manifest indexes %q but the file was not written", entry.ContractPath)
		}
	}
	for _, item := range items {
		if key := "engine-run:" + planHash + ":artifact:" + item.ID; !gotKeys[key] {
			t.Fatalf("no artifact record with key %q; got %v", key, gotKeys)
		}
	}
	if !sort.StringsAreSorted(identities) {
		t.Fatalf("artifact records are not ordered by identity: %v", identities)
	}
	if got := artifactRecordPaths(files); !reflect.DeepEqual(got, sortedCopy(paths)) {
		t.Fatalf("artifact record files = %v, manifest paths = %v", got, sortedCopy(paths))
	}
}

func TestEmitRunPackage_Artifacts_InputOrderDoesNotChangePackageBytes(t *testing.T) {
	planHash := "order-plan"
	items := []artifact.Artifact{
		testArtifact("art-1", artifact.ArtifactTypeSTEP, "one.step", digestOf("1")),
		testArtifact("art-2", artifact.ArtifactTypeCSV, "two.csv", digestOf("2")),
		testArtifact("art-3", artifact.ArtifactTypePDF, "three.pdf", digestOf("3")),
	}
	reversed := []artifact.Artifact{items[2], items[1], items[0]}

	emit := func(in []artifact.Artifact) map[string][]byte {
		_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
			PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), Artifacts: in,
		})
		return files
	}
	assertSameFiles(t, emit(items), emit(reversed))
}

func TestEmitRunPackage_Artifacts_AbsentArtifactsEmitNoArtifactRecords(t *testing.T) {
	planHash := "no-artifact-plan"
	for name, artifacts := range map[string][]artifact.Artifact{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
				PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), Artifacts: artifacts,
			})
			if got := artifactRecordPaths(files); len(got) != 0 {
				t.Fatalf("artifact record files = %v, want none", got)
			}
			if got := manifestRecordsOf(t, files, "artifact"); len(got) != 0 {
				t.Fatalf("manifest artifact records = %#v, want none", got)
			}
			if _, ok := files[recordpackage.MustRecordContractPath("execution")]; !ok {
				t.Fatal("execution record must still be emitted")
			}
		})
	}
}

func TestEmitRunPackage_Artifacts_DuplicateArtifactIdentityFailsClosed(t *testing.T) {
	planHash := "duplicate-artifact-plan"
	item := testArtifact("art-dup", artifact.ArtifactTypeSTEP, "Widget.step", digestOf("a"))
	runRoot := t.TempDir()

	err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:   runRoot,
		PlanHash:  planHash,
		Report:    syntheticSuccessReport(planHash),
		Metadata:  syntheticMetadata(planHash),
		Artifacts: []artifact.Artifact{item, item},
	})
	if !errors.Is(err, recordpackage.ErrInvalidRecord) || !strings.Contains(err.Error(), "duplicate artifact identity") {
		t.Fatalf("EmitRunPackage error = %v, want ErrInvalidRecord duplicate artifact identity", err)
	}
	if _, statErr := os.Stat(filepath.Join(runRoot, recordpackage.PackageDirectoryName)); !os.IsNotExist(statErr) {
		t.Fatalf("a rejected package must not be written, stat err = %v", statErr)
	}
}

func TestEmitRunPackage_Artifacts_InvalidArtifactFailsClosed(t *testing.T) {
	planHash := "invalid-artifact-plan"
	bad := testArtifact("art-bad", artifact.ArtifactTypeSTEP, "Widget.step", "not-a-digest")
	runRoot := t.TempDir()

	err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot: runRoot, PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash),
		Artifacts: []artifact.Artifact{bad},
	})
	if err == nil || !strings.Contains(err.Error(), "map artifacts") {
		t.Fatalf("EmitRunPackage error = %v, want a wrapped artifact mapping failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(runRoot, recordpackage.PackageDirectoryName)); !os.IsNotExist(statErr) {
		t.Fatalf("a rejected package must not be written, stat err = %v", statErr)
	}
}

// --- Observation / verification ---

// runtimeMaterial is real interpreted runtime material: an observed artifact
// carrying target-state evidence and the verification.Result Engine derives by
// verifying an Engine-derived contract against it.
type runtimeMaterial struct {
	observed    *observed.Observed
	result      *verification.Result
	observedRaw []byte
	requestRaw  []byte
	resultRaw   []byte
}

func newRuntimeMaterial(t *testing.T, withTargetState bool) runtimeMaterial {
	t.Helper()
	return newRuntimeMaterialWith(t, withTargetState, nil, verification.StatusPass)
}

// newRuntimeMaterialWith lets a test perturb the observed evidence before it
// is verified, so failing results are genuine Engine verification output.
func newRuntimeMaterialWith(t *testing.T, withTargetState bool, tweak func(*observed.Observed), wantStatus verification.Status) runtimeMaterial {
	t.Helper()
	workingCopy := filepath.Join(t.TempDir(), "attempt", "source", "widget.FCStd")
	if err := os.MkdirAll(filepath.Dir(workingCopy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workingCopy, []byte("working copy bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := planner.WriteExportManifestPayload{SchemaVersion: planner.ExportManifestSchemaVersion}
	if withTargetState {
		payload.PartMutations = &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Sketch", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Old"}},
		}
	}
	contract, err := verification.DeriveFromManifestAndWorkingCopy(payload, workingCopy)
	if err != nil {
		t.Fatalf("derive verification contract: %v", err)
	}
	meta, ref := contract.Expected.Metadata[0], contract.Expected.References[0]
	metaValue, err := observed.NewValue([]byte(`"` + meta.Value + `"`))
	if err != nil {
		t.Fatal(err)
	}
	obs := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy:   observed.WorkingCopy{Path: ref.Name, SHA256: meta.Value},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{},
			Metadata:   []observed.Metadata{{ID: "working-copy:sha256", Key: meta.Key, Value: metaValue, ValueKind: "string"}},
			References: []observed.Reference{{Kind: ref.Kind, Name: ref.Name}},
			Components: []observed.Component{},
		},
	}
	if withTargetState {
		suppressed, hidden := true, false
		obs.Observation.TargetState = &observed.TargetStateObservation{
			Suppression: []observed.BooleanTargetEvidence{{Destination: "part", Object: "Sketch", Status: observed.BooleanEvidenceStatusObserved, Value: &suppressed}},
			Visibility:  []observed.BooleanTargetEvidence{{Destination: "part", Object: "Body", Status: observed.BooleanEvidenceStatusObserved, Value: &hidden}},
			Existence:   []observed.ExistenceTargetEvidence{{Destination: "part", Object: "Old", Status: observed.ExistenceEvidenceStatusAbsent}},
		}
	}
	if tweak != nil {
		tweak(obs)
	}
	result, _ := verification.Verify(contract, obs)
	if result == nil || result.Status != wantStatus {
		t.Fatalf("verify fixture status = %+v, want %s", result, wantStatus)
	}
	return runtimeMaterial{
		observed:    obs,
		result:      result,
		observedRaw: nonCanonicalCADEvidenceJSON("observed-bytes"),
		requestRaw:  nonCanonicalCADEvidenceJSON("verification-request-bytes"),
		resultRaw:   nonCanonicalCADEvidenceJSON("runtime-result-bytes"),
	}
}

func (m runtimeMaterial) evidence() *recordemit.CADRuntimeRunEvidence {
	return &recordemit.CADRuntimeRunEvidence{
		Result: m.resultRaw, Verification: m.requestRaw, Observed: m.observedRaw,
		ObservedValue: m.observed, VerificationResult: m.result,
		JobID: "job-1", ProductKey: "Widget", StepRef: "step-cad",
	}
}

func decodeObservation(t *testing.T, files map[string][]byte) recordcontract.ObservationRecord {
	t.Helper()
	var record recordcontract.ObservationRecord
	content, ok := files[recordpackage.MustRecordContractPath("observation")]
	if !ok {
		t.Fatal("observation record was not emitted")
	}
	if err := json.Unmarshal(content, &record); err != nil {
		t.Fatalf("decode observation record: %v", err)
	}
	if err := recordcontract.ValidateObservationRecord(record); err != nil {
		t.Fatalf("emitted observation record invalid: %v", err)
	}
	return record
}

func decodeVerification(t *testing.T, files map[string][]byte) recordcontract.VerificationRecord {
	t.Helper()
	var record recordcontract.VerificationRecord
	content, ok := files[recordpackage.MustRecordContractPath("verification")]
	if !ok {
		t.Fatal("verification record was not emitted")
	}
	if err := json.Unmarshal(content, &record); err != nil {
		t.Fatalf("decode verification record: %v", err)
	}
	if err := recordcontract.ValidateVerificationRecord(record); err != nil {
		t.Fatalf("emitted verification record invalid: %v", err)
	}
	return record
}

func evidenceDigest(refs []recordcontract.EvidenceReference, kind, ref string) (string, bool) {
	for _, item := range refs {
		if item.Kind == kind && item.Ref == ref {
			return item.DigestSHA256, true
		}
	}
	return "", false
}

func TestEmitRunPackage_Observation_TypedObservedEmitsRecordWithRawProvenance(t *testing.T) {
	planHash := "observation-plan"
	material := newRuntimeMaterial(t, false)

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), CADRuntime: material.evidence(),
	})

	record := decodeObservation(t, files)
	if want := "engine-run:" + planHash + ":observation"; record.RecordKey != want {
		t.Fatalf("observation record key = %q, want %q", record.RecordKey, want)
	}
	if len(record.Observation.Facts) == 0 {
		t.Fatal("observation record carries no facts from the typed observed value")
	}
	rawDigest := sha256Hex(material.observedRaw)
	if got, ok := evidenceDigest(record.Provenance.Evidence, "observed", recordpackage.RawObservedContractPath()); !ok || got != rawDigest {
		t.Fatalf("provenance observed evidence digest = %q (present=%t), want digest of preserved raw bytes %q", got, ok, rawDigest)
	}
	for _, fact := range record.Observation.Facts {
		if fact.Evidence.DigestSHA256 != rawDigest || fact.Evidence.SourceRef != recordpackage.RawObservedContractPath() {
			t.Fatalf("fact %s evidence = %+v, want raw observed %s @ %s", fact.Key, fact.Evidence, recordpackage.RawObservedContractPath(), rawDigest)
		}
		if fact.Linkage.JobID != "job-1" || fact.Linkage.ProductKey != "Widget" || fact.Linkage.StepRef != "step-cad" {
			t.Fatalf("fact %s linkage = %+v", fact.Key, fact.Linkage)
		}
	}
	// The digest recorded in provenance is that of the preserved raw file.
	if !bytes.Equal(files[recordpackage.RawObservedContractPath()], material.observedRaw) || sha256Hex(files[recordpackage.RawObservedContractPath()]) != rawDigest {
		t.Fatal("raw observed evidence was not preserved byte-for-byte")
	}
	if manifest := manifestRecordsOf(t, files, "observation"); len(manifest) != 1 || manifest[0].ContractPath != recordpackage.MustRecordContractPath("observation") {
		t.Fatalf("manifest observation records = %#v", manifest)
	}
}

func TestEmitRunPackage_Verification_UsesInMemoryResultNotRawRequest(t *testing.T) {
	planHash := "verification-plan"
	material := newRuntimeMaterial(t, false)

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), CADRuntime: material.evidence(),
	})

	record := decodeVerification(t, files)
	if want := "engine-run:" + planHash + ":verification"; record.RecordKey != want {
		t.Fatalf("verification record key = %q, want %q", record.RecordKey, want)
	}
	if record.Verification.Outcome != recordcontract.VerificationOutcomePass {
		t.Fatalf("verification outcome = %q, want the in-memory result's pass", record.Verification.Outcome)
	}
	if record.Verification.Message != material.result.Message {
		t.Fatalf("verification message = %q, want in-memory result message %q", record.Verification.Message, material.result.Message)
	}

	requestDigest := sha256Hex(material.requestRaw)
	if got, ok := evidenceDigest(record.Provenance.Evidence, "verification", recordpackage.RawVerificationContractPath()); !ok || got != requestDigest {
		t.Fatalf("provenance verification evidence digest = %q (present=%t), want raw request digest %q", got, ok, requestDigest)
	}
	if len(record.Verification.Evidence) != 1 || record.Verification.Evidence[0].SourceRef != recordpackage.RawVerificationContractPath() || record.Verification.Evidence[0].DigestSHA256 != requestDigest {
		t.Fatalf("verification evidence = %+v, want the raw verification request digest %q", record.Verification.Evidence, requestDigest)
	}
	// Without target-state verification the existing mapper does not reference
	// the observed evidence from the verification record.
	if _, ok := evidenceDigest(record.Provenance.Evidence, "observed", recordpackage.RawObservedContractPath()); ok {
		t.Fatal("non-target-state verification record unexpectedly references observed evidence")
	}
	// prm.verification.json is request/input evidence: it is preserved verbatim
	// and its (deliberately non-verification-shaped) content is never decoded.
	if !bytes.Equal(files[recordpackage.RawVerificationContractPath()], material.requestRaw) {
		t.Fatal("raw verification request evidence was not preserved byte-for-byte")
	}
}

func TestEmitRunPackage_Verification_FailedResultIsMappedFromInMemoryResult(t *testing.T) {
	planHash := "verification-fail-plan"
	wrong, err := observed.NewValue([]byte("\"not-the-working-copy-digest\""))
	if err != nil {
		t.Fatal(err)
	}
	material := newRuntimeMaterialWith(t, false, func(obs *observed.Observed) {
		obs.Observation.Metadata[0].Value = wrong
	}, verification.StatusFail)

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), CADRuntime: material.evidence(),
	})

	record := decodeVerification(t, files)
	if record.Verification.Outcome != recordcontract.VerificationOutcomeFail || record.Verification.FailureClass != recordcontract.VerificationFailureClassMetadataMismatch {
		t.Fatalf("verification = %+v, want fail/metadata_mismatch from the in-memory result", record.Verification)
	}
}

func TestEmitRunPackage_TargetState_FlowsThroughExistingObservationAndVerificationFamilies(t *testing.T) {
	planHash := "target-state-plan"
	material := newRuntimeMaterial(t, true)

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), CADRuntime: material.evidence(),
	})

	// No target-state-specific record family or file exists.
	for path := range files {
		if strings.Contains(strings.ToLower(path), "target") {
			t.Fatalf("unexpected target-state-specific package file %q", path)
		}
	}

	observation := decodeObservation(t, files)
	facts := map[string]string{}
	for _, fact := range observation.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindTargetState {
			facts[fact.Subject.ID+"/"+fact.Subject.Name+"/"+fact.Key] = fact.Value.Raw
		}
	}
	wantFacts := map[string]string{
		"part/Sketch/suppression": `{"status":"observed","value":true}`,
		"part/Body/visibility":    `{"status":"observed","value":false}`,
		"part/Old/existence":      `{"status":"absent"}`,
	}
	if !reflect.DeepEqual(facts, wantFacts) {
		t.Fatalf("target-state observation facts = %v, want %v", facts, wantFacts)
	}

	verificationRecord := decodeVerification(t, files)
	var targetState *recordcontract.VerificationCategoryResult
	for i := range verificationRecord.Verification.Categories {
		if verificationRecord.Verification.Categories[i].Category == recordcontract.VerificationCategoryTargetState {
			targetState = &verificationRecord.Verification.Categories[i]
		}
	}
	if targetState == nil || !targetState.Enabled || targetState.Outcome != recordcontract.VerificationOutcomePass {
		t.Fatalf("target-state verification category = %+v, want enabled pass", targetState)
	}
	// Target-state verification is anchored to both preserved raw inputs.
	requestDigest, observedDigest := sha256Hex(material.requestRaw), sha256Hex(material.observedRaw)
	if got, ok := evidenceDigest(verificationRecord.Provenance.Evidence, "verification", recordpackage.RawVerificationContractPath()); !ok || got != requestDigest {
		t.Fatalf("verification provenance request digest = %q (present=%t), want %q", got, ok, requestDigest)
	}
	if got, ok := evidenceDigest(verificationRecord.Provenance.Evidence, "observed", recordpackage.RawObservedContractPath()); !ok || got != observedDigest {
		t.Fatalf("verification provenance observed digest = %q (present=%t), want %q", got, ok, observedDigest)
	}
	if got := manifestRecordsOf(t, files, "observation"); len(got) != 1 {
		t.Fatalf("manifest observation records = %#v", got)
	}
	if got := manifestRecordsOf(t, files, "verification"); len(got) != 1 {
		t.Fatalf("manifest verification records = %#v", got)
	}
}

func TestEmitRunPackage_TargetState_MismatchVerificationIsEmittedThroughVerificationFamily(t *testing.T) {
	planHash := "target-state-mismatch-plan"
	material := newRuntimeMaterialWith(t, true, func(obs *observed.Observed) {
		notSuppressed := false
		obs.Observation.TargetState.Suppression[0].Value = &notSuppressed
	}, verification.StatusFail)

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), CADRuntime: material.evidence(),
	})
	record := decodeVerification(t, files)
	if record.Verification.Outcome != recordcontract.VerificationOutcomeFail || record.Verification.FailureClass != recordcontract.VerificationFailureClassTargetStateMismatch {
		t.Fatalf("verification = %+v, want fail/target_state_mismatch", record.Verification)
	}
}

func TestEmitRunPackage_CADRuntime_RawEvidenceWithoutTypedValuesEmitsNoObservationOrVerification(t *testing.T) {
	planHash := "raw-only-plan"
	material := newRuntimeMaterial(t, false)

	cases := map[string]*recordemit.CADRuntimeRunEvidence{
		"raw bytes only": {Result: material.resultRaw, Verification: material.requestRaw, Observed: material.observedRaw},
		"typed observed only": {
			Result: material.resultRaw, Verification: material.requestRaw, Observed: material.observedRaw, ObservedValue: material.observed,
			JobID: "job-1", ProductKey: "Widget", StepRef: "step-cad",
		},
		"typed verification only": {
			Result: material.resultRaw, Verification: material.requestRaw, Observed: material.observedRaw, VerificationResult: material.result,
			JobID: "job-1", ProductKey: "Widget", StepRef: "step-cad",
		},
	}
	for name, evidence := range cases {
		t.Run(name, func(t *testing.T) {
			_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
				PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), CADRuntime: evidence,
			})
			_, hasObservation := files[recordpackage.MustRecordContractPath("observation")]
			_, hasVerification := files[recordpackage.MustRecordContractPath("verification")]
			if hasObservation != (evidence.ObservedValue != nil) {
				t.Fatalf("observation record present = %t, typed observed present = %t", hasObservation, evidence.ObservedValue != nil)
			}
			if hasVerification != (evidence.VerificationResult != nil) {
				t.Fatalf("verification record present = %t, typed result present = %t", hasVerification, evidence.VerificationResult != nil)
			}
			// Raw evidence is preserved regardless of what was mapped.
			for contractPath, want := range map[string][]byte{
				recordpackage.RawRuntimeResultContractPath(): material.resultRaw,
				recordpackage.RawVerificationContractPath():  material.requestRaw,
				recordpackage.RawObservedContractPath():      material.observedRaw,
			} {
				if !bytes.Equal(files[contractPath], want) {
					t.Fatalf("raw evidence %q was not preserved byte-for-byte", contractPath)
				}
			}
		})
	}
}

func TestEmitRunPackage_CADRuntime_TypedValuesWithoutRawBytesStillMap(t *testing.T) {
	planHash := "typed-no-raw-plan"
	material := newRuntimeMaterial(t, false)

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash),
		CADRuntime: &recordemit.CADRuntimeRunEvidence{ObservedValue: material.observed, VerificationResult: material.result, JobID: "job-1", ProductKey: "Widget", StepRef: "step-cad"},
	})
	observation := decodeObservation(t, files)
	if digest, ok := evidenceDigest(observation.Provenance.Evidence, "observed", recordpackage.RawObservedContractPath()); ok && digest != "" {
		t.Fatalf("observation provenance carries digest %q although no raw observed bytes exist", digest)
	}
	decodeVerification(t, files)
	for _, contractPath := range []string{recordpackage.RawObservedContractPath(), recordpackage.RawVerificationContractPath(), recordpackage.RawRuntimeResultContractPath()} {
		if _, ok := files[contractPath]; ok {
			t.Fatalf("raw file %q must not be synthesized when no raw bytes were supplied", contractPath)
		}
	}
}

func TestEmitRunPackage_CADRuntime_NilEvidenceEmitsNoObservationOrVerification(t *testing.T) {
	planHash := "no-cad-plan"
	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash),
	})
	for _, family := range []string{"observation", "verification"} {
		if _, ok := files[recordpackage.MustRecordContractPath(recordcontract.Family(family))]; ok {
			t.Fatalf("%s record synthesized without runtime evidence", family)
		}
	}
}

func TestEmitRunPackage_CADRuntime_ArtifactsObservationVerificationAndReferenceTogether(t *testing.T) {
	planHash := "full-normal-plan"
	material := newRuntimeMaterial(t, true)
	traversal := referenceTraversalJSON(validReferenceTraversalEdgeJSON())

	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash),
		Artifacts: []artifact.Artifact{
			testArtifact("art-step", artifact.ArtifactTypeSTEP, "Widget.step", digestOf("a")),
			testArtifact("art-csv", artifact.ArtifactTypeCSV, "Widget.csv", digestOf("b")),
		},
		CADRuntime:         material.evidence(),
		ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{Content: traversal, JobID: "job-1", ProductKey: "Widget", StepRef: "step-cad"},
	})

	gotFamilies := map[string]int{}
	var manifest emitManifest
	if err := json.Unmarshal(files[recordpackage.PackageManifestContractPath()], &manifest); err != nil {
		t.Fatal(err)
	}
	for _, record := range manifest.Records {
		gotFamilies[record.Family]++
	}
	want := map[string]int{"execution": 1, "artifact": 2, "observation": 1, "reference": 1, "verification": 1}
	if !reflect.DeepEqual(gotFamilies, want) {
		t.Fatalf("manifest record families = %v, want %v", gotFamilies, want)
	}
	// Traversal-derived reference behaviour is unchanged by the new mappings.
	if !bytes.Equal(files[recordpackage.RawRuntimeReferenceTraversalContractPath()], traversal) {
		t.Fatal("raw reference traversal bytes not preserved")
	}
	// Every indexed record file exists.
	for _, record := range manifest.Records {
		if _, ok := files[record.ContractPath]; !ok {
			t.Fatalf("manifest indexes %q but it was not written", record.ContractPath)
		}
	}
}

func TestEmitRunPackage_CADRuntime_MapperInvalidTypedEvidenceFailsClosed(t *testing.T) {
	planHash := "invalid-typed-plan"
	material := newRuntimeMaterial(t, false)
	invalid := *material.observed
	invalid.SchemaVersion = "0.0-unsupported"
	material.observed = &invalid
	runRoot := t.TempDir()

	err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot: runRoot, PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), CADRuntime: material.evidence(),
	})
	if err == nil || !strings.Contains(err.Error(), "map observed evidence") {
		t.Fatalf("EmitRunPackage error = %v, want wrapped observed mapping failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(runRoot, recordpackage.PackageDirectoryName)); !os.IsNotExist(statErr) {
		t.Fatalf("a rejected package must not be written, stat err = %v", statErr)
	}
}

// --- Report-derived failure and legacy runtime-result mapper ---

// alignedFailedResultJSON is an active aligned prm.result.json (status
// "failed"), the shape the legacy recordmap.MapRuntimeResult contract does
// not describe.
func alignedFailedResultJSON() []byte {
	return []byte("{\n  \"schemaVersion\": \"1.0\",\n  \"status\": \"failed\",\n  \"failure\": {\"boundary\": \"freecad\", \"category\": \"runtime\", \"code\": \"controlled_failure\", \"message\": \"aligned runtime failure\", \"stage\": \"execute\"}\n}\n")
}

func TestEmitRunPackage_FailureRecordRemainsReportDerivedAndIgnoresAlignedRuntimeResult(t *testing.T) {
	planHash := "report-failure-plan"
	rep := syntheticFailedReport(planHash)

	_, without := emitAndReadPackage(t, recordemit.RunEmitInput{PlanHash: planHash, Report: rep})
	failurePath := recordpackage.MustRecordContractPath("failure")
	if _, ok := without[failurePath]; !ok {
		t.Fatal("report-derived failure record missing")
	}

	resultRaw := alignedFailedResultJSON()
	_, with := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: rep,
		CADRuntime: &recordemit.CADRuntimeRunEvidence{Result: resultRaw},
	})
	if !bytes.Equal(with[failurePath], without[failurePath]) {
		t.Fatalf("failure record changed when aligned prm.result.json evidence was present\nwithout:\n%s\nwith:\n%s", without[failurePath], with[failurePath])
	}
	if !bytes.Equal(with[recordpackage.RawRuntimeResultContractPath()], resultRaw) {
		t.Fatal("raw aligned runtime result must be preserved byte-for-byte")
	}
	// The aligned failure text is raw evidence only; it is not normalized.
	if bytes.Contains(with[failurePath], []byte("controlled_failure")) {
		t.Fatal("aligned runtime failure was normalized into the failure record")
	}
}

func TestEmitRunPackage_AlignedRuntimeResultOnSuccessfulReportEmitsNoFailureRecord(t *testing.T) {
	planHash := "aligned-result-success-plan"
	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash),
		CADRuntime: &recordemit.CADRuntimeRunEvidence{Result: alignedFailedResultJSON()},
	})
	if _, ok := files[recordpackage.MustRecordContractPath("failure")]; ok {
		t.Fatal("a failure record must not be derived from aligned prm.result.json evidence")
	}
}

func TestEmitRunPackage_MalformedRawRuntimeResultIsNotDecoded(t *testing.T) {
	planHash := "malformed-result-plan"
	malformed := []byte("{ this is not json")
	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash),
		CADRuntime: &recordemit.CADRuntimeRunEvidence{Result: malformed},
	})
	if !bytes.Equal(files[recordpackage.RawRuntimeResultContractPath()], malformed) {
		t.Fatal("raw runtime result must be preserved without interpretation")
	}
}

// The legacy recordmap.MapRuntimeResult contract must not be reachable from
// any production emission path. This guards against wiring it to the active
// aligned prm.result.json (status succeeded/failed) by accident.
func TestProductionCodeDoesNotCallLegacyRuntimeResultMapper(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var offenders []string
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if filepath.ToSlash(filepath.Dir(path)) == filepath.ToSlash(filepath.Join(root, "internal", "engine", "recordmap")) {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(content, []byte("MapRuntimeResult")) {
				offenders = append(offenders, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(offenders) != 0 {
		t.Fatalf("production code outside recordmap references the legacy MapRuntimeResult mapper: %v", offenders)
	}
}

// --- Determinism ---

func TestEmitRunPackage_RepeatedEquivalentEmissionIsByteDeterministic(t *testing.T) {
	planHash := "deterministic-plan"
	material := newRuntimeMaterial(t, true)
	emit := func(artifacts []artifact.Artifact) map[string][]byte {
		_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
			PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash),
			Artifacts: artifacts, CADRuntime: material.evidence(),
		})
		return files
	}
	items := []artifact.Artifact{
		testArtifact("art-b", artifact.ArtifactTypeCSV, "b.csv", digestOf("b")),
		testArtifact("art-a", artifact.ArtifactTypeSTEP, "a.step", digestOf("a")),
	}
	first, second := emit(items), emit(items)
	assertSameFiles(t, first, second)
	if got := len(manifestRecordsOf(t, first, "artifact")); got != 2 {
		t.Fatalf("artifact records = %d, want 2", got)
	}
}

func assertSameFiles(t *testing.T, a, b map[string][]byte) {
	t.Helper()
	if !reflect.DeepEqual(sortedFileNames(a), sortedFileNames(b)) {
		t.Fatalf("package file sets differ: %v vs %v", sortedFileNames(a), sortedFileNames(b))
	}
	for path, content := range a {
		if !bytes.Equal(content, b[path]) {
			t.Fatalf("package file %q differs between equivalent emissions", path)
		}
	}
}

func sortedFileNames(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for path := range files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// Re-emitting into an existing package replaces layout-owned files in place;
// the manifest stays authoritative and indexes exactly the records mapped by
// the latest emission.
func TestEmitRunPackage_ReemissionManifestIndexesOnlyLatestArtifactRecords(t *testing.T) {
	planHash := "reemit-plan"
	runRoot := t.TempDir()
	emit := func(checksum string) map[string][]byte {
		_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
			RunRoot: runRoot, PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash),
			Artifacts: []artifact.Artifact{testArtifact("art-a", artifact.ArtifactTypeSTEP, "a.step", digestOf(checksum))},
		})
		return files
	}

	first := emit("a")
	firstEntries := manifestRecordsOf(t, first, "artifact")
	second := emit("b")
	secondEntries := manifestRecordsOf(t, second, "artifact")
	if len(firstEntries) != 1 || len(secondEntries) != 1 || firstEntries[0].IdentityID == secondEntries[0].IdentityID {
		t.Fatalf("expected distinct single artifact identities, got %#v then %#v", firstEntries, secondEntries)
	}
	if _, ok := second[secondEntries[0].ContractPath]; !ok {
		t.Fatalf("latest artifact record %q was not written", secondEntries[0].ContractPath)
	}

	// Identical re-emission is byte-stable.
	assertSameFiles(t, second, emit("b"))
}
