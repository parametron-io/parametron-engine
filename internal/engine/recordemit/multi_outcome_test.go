package recordemit_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"parametron/internal/engine/integrity"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
)

const multiOutcomeResultDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

type multiOutcomeFixture struct {
	outcome recordemit.RunOutcomeEvidence

	observed     []byte
	verification []byte
	result       []byte
	occurrence   []byte
	fact         []byte
	integrity    []byte
	traversal    []byte
}

func newMultiOutcomeFixture(t *testing.T, product, job, persistedResult string, factValue string, reverseOccurrence bool) multiOutcomeFixture {
	t.Helper()
	material := newRuntimeMaterial(t, false)
	cad := material.evidence()
	cad.JobID = job
	cad.ProductKey = product
	cad.StepRef = "2"
	cad.Attempt = 1
	cad.AttemptID = product + "-attempt-1"

	occurrence, occurrenceEvidence := occurrenceEmissionFixture(t)
	if reverseOccurrence {
		for left, right := 0, len(occurrenceEvidence.Members)-1; left < right; left, right = left+1, right-1 {
			occurrenceEvidence.Members[left], occurrenceEvidence.Members[right] = occurrenceEvidence.Members[right], occurrenceEvidence.Members[left]
		}
		for left, right := 0, len(occurrenceEvidence.Group)-1; left < right; left, right = left+1, right-1 {
			occurrenceEvidence.Group[left], occurrenceEvidence.Group[right] = occurrenceEvidence.Group[right], occurrenceEvidence.Group[left]
		}
	}
	occurrenceRaw, err := json.MarshalIndent(occurrenceEvidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	occurrence.Input.Content = append(occurrenceRaw, '\n')
	cad.Occurrence = occurrence

	fact := factEmissionEvidenceInput(t)
	factEvidence := factEmissionEvidence()
	factEvidence.Facts[0].Value = json.RawMessage(factValue)
	factRaw, err := json.MarshalIndent(factEvidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fact.Input.Content = append(factRaw, '\n')
	cad.Fact = fact

	cad.Integrity = &integrity.OperationIntegrity{Resources: []integrity.ResourceOperation{{
		LogicalID:                 "alpha",
		MaterializedCoordinate:    "source/Alpha.FCStd",
		MaterializedDigest:        integrity.Digest(emitSourceDigest),
		MaterializationStatus:     integrity.StatusVerified,
		RuntimeBaselineDigest:     integrity.Digest(emitSourceDigest),
		RuntimeBaselineStatus:     integrity.StatusVerified,
		PersistedResultCoordinate: "source/Alpha.FCStd",
		PersistedResultDigest:     integrity.Digest(persistedResult),
		ResultStatus:              integrity.StatusVerified,
	}}}
	cad.IntegrityEvidence = []byte(fmt.Sprintf("{\"schemaVersion\":\"1.0\",\"product\":%q}\n", product))

	traversal := referenceTraversalJSON(validReferenceTraversalEdgeJSON())
	return multiOutcomeFixture{
		outcome: recordemit.RunOutcomeEvidence{
			JobID: job, ProductKey: product, StepRef: "2", Attempt: 1, AttemptID: cad.AttemptID,
			CADRuntime: cad,
			ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{
				Resources: traversalInventory(t), ExpectedSourceDocument: "Widget.FCStd", Content: traversal,
				JobID: job, ProductKey: product, StepRef: "2", Attempt: 1, AttemptID: cad.AttemptID,
			},
		},
		observed:     append([]byte(nil), cad.Observed...),
		verification: append([]byte(nil), cad.Verification...),
		result:       append([]byte(nil), cad.Result...),
		occurrence:   append([]byte(nil), cad.Occurrence.Input.Content...),
		fact:         append([]byte(nil), cad.Fact.Input.Content...),
		integrity:    append([]byte(nil), cad.IntegrityEvidence...),
		traversal:    traversal,
	}
}

func multiOutcomeInput(planHash string, outcomes ...recordemit.RunOutcomeEvidence) recordemit.RunEmitInput {
	return recordemit.RunEmitInput{
		PlanHash:        planHash,
		Report:          syntheticSuccessReport(planHash),
		Metadata:        syntheticMetadata(planHash),
		SourceIntegrity: multiOutcomeSourceIntegrity(),
		Outcomes:        outcomes,
	}
}

func multiOutcomeSourceIntegrity() *integrity.ProjectIntegrity {
	return &integrity.ProjectIntegrity{
		Lock: integrity.LockStateVerified,
		Resources: []integrity.ResourceSource{{
			LogicalID:        "alpha",
			SourceCoordinate: "models/alpha.FCStd",
			SourceDigest:     integrity.Digest(emitSourceDigest),
			LockState:        integrity.LockStateVerified,
			LockedDigest:     integrity.Digest(emitSourceDigest),
		}},
	}
}

func TestEmitRunPackage_MultiOutcome_EmitsEveryOutcomeScopedFamily(t *testing.T) {
	planHash := "multi-outcome-plan"
	first := newMultiOutcomeFixture(t, "widget-a", "job-a", emitResultDigest, "42", false)
	second := newMultiOutcomeFixture(t, "widget-b", "job-b", multiOutcomeResultDigest, "43", true)

	runRoot := t.TempDir()
	input := multiOutcomeInput(planHash, first.outcome, second.outcome)
	input.RunRoot = runRoot
	if err := recordemit.EmitRunPackage(input); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}
	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	files := readEmitPackageFiles(t, packageRoot)

	observations := decodeOutcomeObservations(t, files)
	if len(observations) != 2 {
		t.Fatalf("observation records = %d, want 2", len(observations))
	}
	byProduct := map[string]recordcontract.ObservationRecord{}
	for _, record := range observations {
		byProduct[record.Provenance.Linkage.ProductKey] = record
	}
	wantFactValues := map[string]string{"widget-a": "42", "widget-b": "43"}
	for product, wantValue := range wantFactValues {
		record, ok := byProduct[product]
		if !ok {
			t.Fatalf("missing observation record for %s", product)
		}
		if record.Observation.Occurrences == nil || len(record.Observation.Occurrences.Quantities) != 1 || record.Observation.Occurrences.Quantities[0].Quantity != 2 {
			t.Fatalf("%s quantity is not scoped to its own two-occurrence scope: %#v", product, record.Observation.Occurrences)
		}
		var values []string
		for _, fact := range record.Observation.Facts {
			if fact.Kind == recordcontract.ObservationKindFact && fact.Property == "Length" && len(fact.Subject.Context) == 0 {
				values = append(values, fact.Value.Raw)
			}
		}
		if !slices.Contains(values, wantValue) {
			t.Fatalf("%s facts = %#v, want its own Length=%s", product, values, wantValue)
		}
		resource := recordProvenanceIntegrity(t, record.Provenance)
		if resource.PersistedResultDigestSHA256 == "" || resource.PersistedResultDigestSHA256 == emitSourceDigest {
			t.Fatalf("%s lost its configured-result identity: %#v", product, resource)
		}
	}

	if byProduct["widget-a"].Provenance.Integrity.Resources[0].PersistedResultDigestSHA256 == byProduct["widget-b"].Provenance.Integrity.Resources[0].PersistedResultDigestSHA256 {
		t.Fatal("configured-result identity collapsed across outcomes")
	}
	if byProduct["widget-a"].Provenance.Integrity.Resources[0].SourceDigestSHA256 != byProduct["widget-b"].Provenance.Integrity.Resources[0].SourceDigestSHA256 {
		t.Fatal("shared source identity was not preserved")
	}
	if byProduct["widget-a"].Identity == byProduct["widget-b"].Identity {
		t.Fatal("semantically equal observation records from distinct outcomes collided")
	}

	verifications := decodeOutcomeVerifications(t, files)
	if len(verifications) != 2 {
		t.Fatalf("verification records = %d, want 2", len(verifications))
	}
	references := decodeOutcomeReferences(t, files)
	if len(references) != 2 {
		t.Fatalf("reference records = %d, want 2", len(references))
	}
	if references[0].Identity == references[1].Identity {
		t.Fatal("reference records from distinct outcomes collided")
	}

	for _, fixture := range []multiOutcomeFixture{first, second} {
		product := fixture.outcome.ProductKey
		observation := byProduct[product]
		observedPath := provenanceEvidenceRef(t, observation.Provenance, "observed")
		factPath := provenanceEvidenceRef(t, observation.Provenance, recordcontract.FactEvidenceSourceKind)
		occurrencePath := provenanceEvidenceRef(t, observation.Provenance, "occurrence-evidence")
		integrityPath := provenanceEvidenceRef(t, observation.Provenance, "integrity-evidence")
		assertOutcomeRawFile(t, files, observedPath, fixture.observed)
		assertOutcomeRawFile(t, files, factPath, fixture.fact)
		assertOutcomeRawFile(t, files, occurrencePath, fixture.occurrence)
		assertOutcomeRawFile(t, files, integrityPath, fixture.integrity)

		verification := findVerificationByProduct(t, verifications, product)
		assertOutcomeRawFile(t, files, provenanceEvidenceRef(t, verification.Provenance, "verification"), fixture.verification)
		reference := findReferenceByProduct(t, references, product)
		assertOutcomeRawFile(t, files, provenanceEvidenceRef(t, reference.Provenance, "reference-traversal"), fixture.traversal)

	}

	// Raw result evidence has no normalized record slot; both outcome files must
	// still exist under their own scopes and preserve the exact bytes.
	resultPaths := scopedRawPathsWithSuffix(files, "/runtime/prm.result.json")
	if len(resultPaths) != 2 {
		t.Fatalf("outcome-scoped result paths = %v, want 2", resultPaths)
	}
	resultContents := [][]byte{files[resultPaths[0]], files[resultPaths[1]]}
	for _, fixture := range []multiOutcomeFixture{first, second} {
		found := false
		for _, content := range resultContents {
			if bytes.Equal(content, fixture.result) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s result bytes missing from outcome-scoped result evidence", fixture.outcome.ProductKey)
		}
	}

	assertExecutionIntegrityIsSourceOnly(t, files)
	assertScopedRawEvidenceCount(t, files, 2)
}

func TestEmitRunPackage_MultiOutcome_DeterministicAcrossInputOrder(t *testing.T) {
	planHash := "multi-outcome-determinism"
	first := newMultiOutcomeFixture(t, "widget-a", "job-a", emitResultDigest, "42", false)
	second := newMultiOutcomeFixture(t, "widget-b", "job-b", multiOutcomeResultDigest, "43", true)

	emit := func(outcomes ...recordemit.RunOutcomeEvidence) map[string][]byte {
		input := multiOutcomeInput(planHash, outcomes...)
		input.RunRoot = t.TempDir()
		if err := recordemit.EmitRunPackage(input); err != nil {
			t.Fatalf("EmitRunPackage returned error: %v", err)
		}
		return readEmitPackageFiles(t, filepath.Join(input.RunRoot, recordpackage.PackageDirectoryName))
	}

	firstFiles := emit(first.outcome, second.outcome)
	secondFiles := emit(second.outcome, first.outcome)
	if !reflect.DeepEqual(sortedPackageKeys(firstFiles), sortedPackageKeys(secondFiles)) {
		t.Fatalf("package paths differ across outcome input order:\nfirst:  %v\nsecond: %v", sortedPackageKeys(firstFiles), sortedPackageKeys(secondFiles))
	}
	for path, content := range firstFiles {
		if !bytes.Equal(content, secondFiles[path]) {
			t.Fatalf("package file %q differs across equivalent outcome ordering", path)
		}
	}

	firstManifest := readEmitManifestBytes(t, firstFiles)
	secondManifest := readEmitManifestBytes(t, secondFiles)
	if !bytes.Equal(firstManifest, secondManifest) {
		t.Fatal("manifest is not deterministic across equivalent outcome ordering")
	}
}

func TestEmitRunPackage_MultiOutcome_DistinctOutcomesKeepDistinctIdentity(t *testing.T) {
	planHash := "multi-outcome-identical"
	first := newMultiOutcomeFixture(t, "widget-a", "job-a", emitResultDigest, "42", false)
	second := newMultiOutcomeFixture(t, "widget-b", "job-b", emitResultDigest, "42", false)

	input := multiOutcomeInput(planHash, first.outcome, second.outcome)
	input.RunRoot = t.TempDir()
	if err := recordemit.EmitRunPackage(input); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}
	files := readEmitPackageFiles(t, filepath.Join(input.RunRoot, recordpackage.PackageDirectoryName))

	observations := decodeOutcomeObservations(t, files)
	if len(observations) != 2 {
		t.Fatalf("observation records = %d, want 2", len(observations))
	}
	if observations[0].Identity == observations[1].Identity {
		t.Fatal("observation records from distinct outcomes collided")
	}
	references := decodeOutcomeReferences(t, files)
	if len(references) != 2 || references[0].Identity == references[1].Identity {
		t.Fatal("reference records from distinct outcomes collided")
	}
}

func TestEmitRunPackage_MultiOutcome_RejectsIneligibleAndAmbiguousOutcomes(t *testing.T) {
	planHash := "multi-outcome-rejections"
	first := newMultiOutcomeFixture(t, "widget-a", "job-a", emitResultDigest, "42", false)

	t.Run("runtime failure is not a successful observation outcome", func(t *testing.T) {
		failed := newMultiOutcomeFixture(t, "widget-b", "job-b", multiOutcomeResultDigest, "43", true)
		failed.outcome.CADRuntime.Failure = runtimeNativeFailure()
		input := multiOutcomeInput(planHash, first.outcome, failed.outcome)
		input.RunRoot = t.TempDir()
		err := recordemit.EmitRunPackage(input)
		if err == nil || !strings.Contains(err.Error(), "not eligible") {
			t.Fatalf("EmitRunPackage error = %v, want ineligible outcome rejection", err)
		}
	})

	t.Run("duplicate outcome identity", func(t *testing.T) {
		duplicate := newMultiOutcomeFixture(t, "widget-a", "job-a", multiOutcomeResultDigest, "43", true)
		duplicate.outcome.AttemptID = first.outcome.AttemptID
		input := multiOutcomeInput(planHash, first.outcome, duplicate.outcome)
		input.RunRoot = t.TempDir()
		err := recordemit.EmitRunPackage(input)
		if err == nil || !strings.Contains(err.Error(), "duplicate outcome identity") {
			t.Fatalf("EmitRunPackage error = %v, want duplicate outcome identity rejection", err)
		}
	})
}

func decodeOutcomeObservations(t *testing.T, files map[string][]byte) []recordcontract.ObservationRecord {
	t.Helper()
	var out []recordcontract.ObservationRecord
	for path, content := range files {
		if !strings.HasPrefix(path, "records/outcomes/") || !strings.HasSuffix(path, "prm.observation-record.json") {
			continue
		}
		var record recordcontract.ObservationRecord
		if err := json.Unmarshal(content, &record); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if err := recordcontract.ValidateObservationRecord(record); err != nil {
			t.Fatalf("validate %s: %v", path, err)
		}
		out = append(out, record)
	}
	if len(out) == 0 {
		t.Fatal("no outcome-scoped observation records")
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Provenance.Linkage.ProductKey < out[j].Provenance.Linkage.ProductKey
	})
	return out
}

func decodeOutcomeVerifications(t *testing.T, files map[string][]byte) []recordcontract.VerificationRecord {
	t.Helper()
	var out []recordcontract.VerificationRecord
	for path, content := range files {
		if !strings.HasPrefix(path, "records/outcomes/") || !strings.HasSuffix(path, "prm.verification-record.json") {
			continue
		}
		var record recordcontract.VerificationRecord
		if err := json.Unmarshal(content, &record); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if err := recordcontract.ValidateVerificationRecord(record); err != nil {
			t.Fatalf("validate %s: %v", path, err)
		}
		out = append(out, record)
	}
	return out
}

func decodeOutcomeReferences(t *testing.T, files map[string][]byte) []recordcontract.ReferenceRecord {
	t.Helper()
	var out []recordcontract.ReferenceRecord
	for path, content := range files {
		if !strings.HasPrefix(path, "records/outcomes/") || !strings.HasSuffix(path, "prm.reference-record.json") {
			continue
		}
		var record recordcontract.ReferenceRecord
		if err := json.Unmarshal(content, &record); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if err := recordcontract.ValidateReferenceRecord(record); err != nil {
			t.Fatalf("validate %s: %v", path, err)
		}
		out = append(out, record)
	}
	return out
}

func findVerificationByProduct(t *testing.T, records []recordcontract.VerificationRecord, product string) recordcontract.VerificationRecord {
	t.Helper()
	for _, record := range records {
		if record.Verification.Linkage.ProductKey == product {
			return record
		}
	}
	t.Fatalf("missing verification record for %s", product)
	return recordcontract.VerificationRecord{}
}

func findReferenceByProduct(t *testing.T, records []recordcontract.ReferenceRecord, product string) recordcontract.ReferenceRecord {
	t.Helper()
	for _, record := range records {
		if record.Provenance.Linkage.ProductKey == product {
			return record
		}
	}
	t.Fatalf("missing reference record for %s", product)
	return recordcontract.ReferenceRecord{}
}

func recordProvenanceIntegrity(t *testing.T, provenance recordcontract.Provenance) recordcontract.ResourceProvenance {
	t.Helper()
	if provenance.Integrity == nil || len(provenance.Integrity.Resources) != 1 {
		t.Fatalf("provenance integrity = %#v, want one resource", provenance.Integrity)
	}
	return provenance.Integrity.Resources[0]
}

func provenanceEvidenceRef(t *testing.T, provenance recordcontract.Provenance, kind string) string {
	t.Helper()
	for _, ref := range provenance.Evidence {
		if ref.Kind == kind {
			return ref.Ref
		}
	}
	t.Fatalf("provenance evidence %q missing in %#v", kind, provenance.Evidence)
	return ""
}

func assertOutcomeRawFile(t *testing.T, files map[string][]byte, path string, want []byte) {
	t.Helper()
	if !strings.HasPrefix(path, "raw/outcomes/") {
		t.Fatalf("raw evidence path %q is not outcome-scoped", path)
	}
	content, ok := files[path]
	if !ok {
		t.Fatalf("raw evidence %q missing", path)
	}
	if !bytes.Equal(content, want) {
		t.Fatalf("raw evidence %q bytes differ\nwant: %q\ngot:  %q", path, want, content)
	}
}

func assertExecutionIntegrityIsSourceOnly(t *testing.T, files map[string][]byte) {
	t.Helper()
	var execution recordcontract.ExecutionRecord
	if err := json.Unmarshal(files[recordpackage.MustRecordContractPath("execution")], &execution); err != nil {
		t.Fatalf("decode execution record: %v", err)
	}
	resource := recordProvenanceIntegrity(t, execution.Provenance)
	if resource.SourceDigestSHA256 != emitSourceDigest {
		t.Fatalf("run-level source identity = %q, want %q", resource.SourceDigestSHA256, emitSourceDigest)
	}
	if resource.PersistedResultDigestSHA256 != "" || resource.ResultStatus != recordcontract.IntegrityStatusNotPerformed {
		t.Fatalf("run-level provenance absorbed outcome configured-result identity: %#v", resource)
	}
}

func assertScopedRawEvidenceCount(t *testing.T, files map[string][]byte, wantPerFamily int) {
	t.Helper()
	counts := map[string]int{}
	for path := range files {
		if !strings.HasPrefix(path, "raw/outcomes/") {
			continue
		}
		parts := strings.Split(path, "/")
		if len(parts) < 4 {
			t.Fatalf("unexpected scoped raw path %q", path)
		}
		counts[strings.Join(parts[3:], "/")]++
	}
	for family, count := range counts {
		if count != wantPerFamily {
			t.Fatalf("raw family %q count = %d, want %d: %#v", family, count, wantPerFamily, counts)
		}
	}
}

func readEmitPackageFiles(t *testing.T, packageRoot string) map[string][]byte {
	t.Helper()
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
	return files
}

func readEmitManifestBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	content, ok := files[recordpackage.PackageManifestContractPath()]
	if !ok {
		t.Fatal("package manifest missing")
	}
	return content
}

func sortedPackageKeys(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for path := range files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func scopedRawPathsWithSuffix(files map[string][]byte, suffix string) []string {
	var out []string
	for path := range files {
		if strings.HasPrefix(path, "raw/outcomes/") && strings.HasSuffix(path, suffix) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}
