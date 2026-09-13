package recordmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/recordcontract"
)

func TestMapArtifactStoreRecordsMapsArtifactRecords(t *testing.T) {
	artifacts := []artifact.Artifact{
		artifactStoreTestArtifact("artifact-step", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeSTEP, "files/model.step", "model.step", "model/step", 128, artifactMapDigestA(), "export"),
		artifactStoreTestArtifact("artifact-json", artifact.ArtifactClassVerified, artifact.ArtifactTypeJSON, "files/verify.json", "verify.json", "application/json", 256, artifactMapDigestB(), "verify"),
		artifactStoreTestArtifact("artifact-log", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeLog, "files/run.log", "run.log", "text/plain", 64, artifactMapDigestC(), "observe"),
	}

	output, err := MapArtifactStoreRecords(ArtifactMappingInput{
		Artifacts:       artifacts,
		RecordKeyPrefix: "run-123",
		Provenance:      artifactMapProvenance(),
	})
	if err != nil {
		t.Fatalf("MapArtifactStoreRecords returned error: %v", err)
	}
	if len(output.ArtifactRecords) != len(artifacts) {
		t.Fatalf("len(ArtifactRecords) = %d, want %d", len(output.ArtifactRecords), len(artifacts))
	}

	byFilename := make(map[string]recordcontract.ArtifactRecord)
	for _, record := range output.ArtifactRecords {
		if err := recordcontract.ValidateArtifactRecord(record); err != nil {
			t.Fatalf("ValidateArtifactRecord(%s) returned error: %v", record.RecordKey, err)
		}
		if record.Family != recordcontract.FamilyArtifact {
			t.Fatalf("Family = %q, want %q", record.Family, recordcontract.FamilyArtifact)
		}
		if record.Version != recordcontract.CurrentVersion {
			t.Fatalf("Version = %q, want %q", record.Version, recordcontract.CurrentVersion)
		}
		if record.Artifact.Evidence.SourceKind != artifactStoreManifestEvidenceKind {
			t.Fatalf("Artifact.Evidence.SourceKind = %q, want %q", record.Artifact.Evidence.SourceKind, artifactStoreManifestEvidenceKind)
		}
		if record.Artifact.Evidence.SourceRef != artifactStoreManifestEvidenceRef {
			t.Fatalf("Artifact.Evidence.SourceRef = %q, want %q", record.Artifact.Evidence.SourceRef, artifactStoreManifestEvidenceRef)
		}
		if !hasArtifactMapProvenanceEvidence(record.Provenance, artifactStoreManifestEvidenceKind, artifactStoreManifestEvidenceRef) {
			t.Fatalf("Provenance evidence does not include %s %s: %#v", artifactStoreManifestEvidenceKind, artifactStoreManifestEvidenceRef, record.Provenance.Evidence)
		}
		if record.Artifact.Path == artifactStoreManifestEvidenceRef || record.Artifact.Filename == artifactStoreManifestEvidenceRef {
			t.Fatalf("raw manifest was mapped as durable artifact locator: %#v", record.Artifact)
		}
		byFilename[record.Artifact.Filename] = record
	}

	assertArtifactMapRecordSurface(t, byFilename["model.step"], recordcontract.ArtifactClassExecutionOutput, recordcontract.ArtifactTypeSTEP, artifacts[0])
	assertArtifactMapRecordSurface(t, byFilename["verify.json"], recordcontract.ArtifactClassVerified, recordcontract.ArtifactTypeJSON, artifacts[1])
	assertArtifactMapRecordSurface(t, byFilename["run.log"], recordcontract.ArtifactClassExecutionOutput, recordcontract.ArtifactTypeLog, artifacts[2])
}

func TestMapArtifactStoreManifestUsesSameArtifactRecordSurface(t *testing.T) {
	artifacts := []artifact.Artifact{
		artifactStoreTestArtifact("artifact-step", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeSTEP, "files/model.step", "model.step", "model/step", 128, artifactMapDigestA(), "export"),
		artifactStoreTestArtifact("artifact-json", artifact.ArtifactClassVerified, artifact.ArtifactTypeJSON, "files/verify.json", "verify.json", "application/json", 256, artifactMapDigestB(), "verify"),
	}
	provenance := artifactMapProvenance()

	fromRecords, err := MapArtifactStoreRecords(ArtifactMappingInput{
		Artifacts:       artifacts,
		RecordKeyPrefix: "run-123",
		Provenance:      provenance,
	})
	if err != nil {
		t.Fatalf("MapArtifactStoreRecords returned error: %v", err)
	}

	fromManifest, err := MapArtifactStoreManifest(ArtifactManifestMappingInput{
		Manifest:        artifact.Manifest{Artifacts: artifacts},
		RecordKeyPrefix: "run-123",
		Provenance:      provenance,
	})
	if err != nil {
		t.Fatalf("MapArtifactStoreManifest returned error: %v", err)
	}
	if !reflect.DeepEqual(fromManifest.ArtifactRecords, fromRecords.ArtifactRecords) {
		t.Fatalf("manifest records differ from direct records:\nmanifest: %#v\ndirect:   %#v", fromManifest.ArtifactRecords, fromRecords.ArtifactRecords)
	}

	directRecords, err := MapArtifactStoreManifestToArtifactRecords(ArtifactManifestMappingInput{
		Manifest:        artifact.Manifest{Artifacts: artifacts},
		RecordKeyPrefix: "run-123",
		Provenance:      provenance,
	})
	if err != nil {
		t.Fatalf("MapArtifactStoreManifestToArtifactRecords returned error: %v", err)
	}
	if !reflect.DeepEqual(directRecords, fromManifest.ArtifactRecords) {
		t.Fatalf("helper records differ from mapped output:\nhelper: %#v\noutput: %#v", directRecords, fromManifest.ArtifactRecords)
	}
	for _, record := range directRecords {
		if err := recordcontract.ValidateArtifactRecord(record); err != nil {
			t.Fatalf("ValidateArtifactRecord(%s) returned error: %v", record.RecordKey, err)
		}
	}
}

func TestMapArtifactStoreRecordsDeterministicIndependentOfInputOrder(t *testing.T) {
	firstOrder := []artifact.Artifact{
		artifactStoreTestArtifact("artifact-csv", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeCSV, "files/table.csv", "table.csv", "text/csv", 96, artifactMapDigestA(), "table"),
		artifactStoreTestArtifact("artifact-json", artifact.ArtifactClassVerified, artifact.ArtifactTypeJSON, "files/verify.json", "verify.json", "application/json", 256, artifactMapDigestB(), "verify"),
		artifactStoreTestArtifact("artifact-step", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeSTEP, "files/model.step", "model.step", "model/step", 128, artifactMapDigestC(), "export"),
	}
	secondOrder := []artifact.Artifact{firstOrder[2], firstOrder[0], firstOrder[1]}

	first := mustMapArtifactStoreRecords(t, firstOrder)
	second := mustMapArtifactStoreRecords(t, secondOrder)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("records differ across input order:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	firstJSON := mustMarshalArtifactMapRecords(t, first)
	secondJSON := mustMarshalArtifactMapRecords(t, second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("JSON differs across input order:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}

	for i := range first {
		if first[i].RecordKey != second[i].RecordKey {
			t.Fatalf("record key at %d differs: %q vs %q", i, first[i].RecordKey, second[i].RecordKey)
		}
		if first[i].Identity != second[i].Identity {
			t.Fatalf("identity at %d differs: %#v vs %#v", i, first[i].Identity, second[i].Identity)
		}
	}
}

func TestMapArtifactStoreRecordsEmptyInputsReturnNoRecords(t *testing.T) {
	output, err := MapArtifactStoreRecords(ArtifactMappingInput{
		Artifacts:       nil,
		RecordKeyPrefix: "run-123",
		Provenance:      artifactMapProvenance(),
	})
	if err != nil {
		t.Fatalf("MapArtifactStoreRecords(empty) returned error: %v", err)
	}
	if len(output.ArtifactRecords) != 0 {
		t.Fatalf("len(ArtifactRecords) = %d, want 0", len(output.ArtifactRecords))
	}

	manifestOutput, err := MapArtifactStoreManifest(ArtifactManifestMappingInput{
		Manifest:        artifact.Manifest{},
		RecordKeyPrefix: "run-123",
		Provenance:      artifactMapProvenance(),
	})
	if err != nil {
		t.Fatalf("MapArtifactStoreManifest(empty) returned error: %v", err)
	}
	if len(manifestOutput.ArtifactRecords) != 0 {
		t.Fatalf("len(manifest ArtifactRecords) = %d, want 0", len(manifestOutput.ArtifactRecords))
	}
}

func TestMapArtifactStoreRecordsInvalidInputWrapsErrInvalidArtifactMapping(t *testing.T) {
	validArtifact := artifactStoreTestArtifact("artifact-step", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeSTEP, "files/model.step", "model.step", "model/step", 128, artifactMapDigestA(), "export")

	tests := []struct {
		name   string
		input  ArtifactMappingInput
		mutate func(*artifact.Artifact)
	}{
		{
			name: "blank record key prefix",
			input: ArtifactMappingInput{
				Artifacts:       []artifact.Artifact{validArtifact},
				RecordKeyPrefix: " ",
				Provenance:      artifactMapProvenance(),
			},
		},
		{name: "invalid lowercase checksum shape", mutate: func(item *artifact.Artifact) { item.ChecksumSHA256 = strings.Repeat("g", 64) }},
		{name: "uppercase checksum", mutate: func(item *artifact.Artifact) { item.ChecksumSHA256 = strings.ToUpper(artifactMapDigestA()) }},
		{name: "short checksum", mutate: func(item *artifact.Artifact) { item.ChecksumSHA256 = "abc123" }},
		{name: "non-hex checksum", mutate: func(item *artifact.Artifact) { item.ChecksumSHA256 = strings.Repeat("z", 64) }},
		{name: "negative size", mutate: func(item *artifact.Artifact) { item.SizeBytes = -1 }},
		{name: "unsupported artifact class", mutate: func(item *artifact.Artifact) { item.Class = artifact.ArtifactClass("preview") }},
		{name: "unsupported artifact type", mutate: func(item *artifact.Artifact) { item.Type = artifact.ArtifactType("mesh") }},
		{name: "recordcontract validation failure", mutate: func(item *artifact.Artifact) {
			item.ID = ""
			item.Type = ""
			item.Path = ""
			item.Filename = ""
			item.SizeBytes = 0
			item.ChecksumSHA256 = ""
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.input
			if input.RecordKeyPrefix == "" {
				item := validArtifact
				if tc.mutate != nil {
					tc.mutate(&item)
				}
				input = ArtifactMappingInput{
					Artifacts:       []artifact.Artifact{item},
					RecordKeyPrefix: "run-123",
					Provenance:      artifactMapProvenance(),
				}
			}

			_, err := MapArtifactStoreRecords(input)
			if !errors.Is(err, ErrInvalidArtifactMapping) {
				t.Fatalf("MapArtifactStoreRecords error = %v, want errors.Is(..., ErrInvalidArtifactMapping)", err)
			}
		})
	}
}

func TestMapArtifactStoreRecordsProvenancePreservedAndCopySafe(t *testing.T) {
	provenance := artifactMapProvenance()
	provenance.Evidence = append(provenance.Evidence, recordcontract.EvidenceReference{
		Kind:         artifactStoreManifestEvidenceKind,
		Ref:          artifactStoreManifestEvidenceRef,
		DigestSHA256: "",
	})
	originalInputs := append([]recordcontract.ProvenanceInput(nil), provenance.Inputs...)
	originalEvidence := append([]recordcontract.EvidenceReference(nil), provenance.Evidence...)
	artifacts := []artifact.Artifact{
		artifactStoreTestArtifact("artifact-step", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeSTEP, "files/model.step", "model.step", "model/step", 128, artifactMapDigestA(), "export"),
	}

	first := mustMapArtifactStoreRecordsWithProvenance(t, artifacts, provenance)
	if !reflect.DeepEqual(provenance.Inputs, originalInputs) {
		t.Fatalf("caller provenance inputs mutated:\ngot:  %#v\nwant: %#v", provenance.Inputs, originalInputs)
	}
	if !reflect.DeepEqual(provenance.Evidence, originalEvidence) {
		t.Fatalf("caller provenance evidence mutated:\ngot:  %#v\nwant: %#v", provenance.Evidence, originalEvidence)
	}

	record := first[0]
	if record.Provenance.SourceRevision != recordcontract.NormalizeProvenance(provenance).SourceRevision {
		t.Fatalf("SourceRevision = %#v, want %#v", record.Provenance.SourceRevision, recordcontract.NormalizeProvenance(provenance).SourceRevision)
	}
	if record.Provenance.Plan != recordcontract.NormalizeProvenance(provenance).Plan {
		t.Fatalf("Plan = %#v, want %#v", record.Provenance.Plan, recordcontract.NormalizeProvenance(provenance).Plan)
	}
	if record.Provenance.Linkage != recordcontract.NormalizeProvenance(provenance).Linkage {
		t.Fatalf("Linkage = %#v, want %#v", record.Provenance.Linkage, recordcontract.NormalizeProvenance(provenance).Linkage)
	}
	if artifactMapEvidenceCount(record.Provenance.Evidence, artifactStoreManifestEvidenceKind, artifactStoreManifestEvidenceRef) != 1 {
		t.Fatalf("manifest evidence count = %d, want 1: %#v", artifactMapEvidenceCount(record.Provenance.Evidence, artifactStoreManifestEvidenceKind, artifactStoreManifestEvidenceRef), record.Provenance.Evidence)
	}

	provenance.Inputs[0].Identity = "mutated-input"
	provenance.Evidence[0].Ref = "mutated-evidence"
	if first[0].Provenance.Inputs[0].Identity == "mutated-input" {
		t.Fatalf("mapped record shares caller provenance input backing storage")
	}
	if first[0].Provenance.Evidence[0].Ref == "mutated-evidence" {
		t.Fatalf("mapped record shares caller provenance evidence backing storage")
	}

	second := mustMapArtifactStoreRecordsWithProvenance(t, artifacts, artifactMapProvenance())
	first[0].Provenance.Inputs[0].Identity = "mutated-record"
	first[0].Provenance.Evidence[0].Ref = "mutated-record-evidence"
	if second[0].Provenance.Inputs[0].Identity == "mutated-record" {
		t.Fatalf("separately mapped output shares provenance input backing storage")
	}
	if second[0].Provenance.Evidence[0].Ref == "mutated-record-evidence" {
		t.Fatalf("separately mapped output shares provenance evidence backing storage")
	}
}

func TestMapArtifactStoreRecordsRecordKeyFallbackBehavior(t *testing.T) {
	withID := artifactStoreTestArtifact("explicit-artifact-id", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeSTEP, "files/model.step", "model.step", "model/step", 128, artifactMapDigestA(), "export")
	withoutID := artifactStoreTestArtifact("", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeSTEP, "files/model.step", "model.step", "model/step", 128, artifactMapDigestA(), "export")
	withoutIDDifferentTime := withoutID
	withoutIDDifferentTime.CreatedAt = withoutID.CreatedAt.Add(24 * time.Hour)

	explicit := mustMapArtifactStoreRecords(t, []artifact.Artifact{withID})
	fallback := mustMapArtifactStoreRecords(t, []artifact.Artifact{withoutID})
	fallbackDifferentTime := mustMapArtifactStoreRecords(t, []artifact.Artifact{withoutIDDifferentTime})
	fallbackDifferentOrder := mustMapArtifactStoreRecords(t, []artifact.Artifact{
		artifactStoreTestArtifact("artifact-json", artifact.ArtifactClassVerified, artifact.ArtifactTypeJSON, "files/verify.json", "verify.json", "application/json", 256, artifactMapDigestB(), "verify"),
		withoutID,
	})

	if !strings.Contains(explicit[0].RecordKey, ":artifact:explicit-artifact-id") {
		t.Fatalf("explicit ID record key = %q, want explicit artifact ID", explicit[0].RecordKey)
	}
	if fallback[0].RecordKey == explicit[0].RecordKey {
		t.Fatalf("fallback record key unexpectedly matched explicit ID key: %q", fallback[0].RecordKey)
	}
	if err := recordcontract.ValidateArtifactRecord(fallback[0]); err != nil {
		t.Fatalf("ValidateArtifactRecord(fallback) returned error: %v", err)
	}
	if fallback[0].RecordKey != fallbackDifferentTime[0].RecordKey {
		t.Fatalf("fallback record key changed with timestamp: %q vs %q", fallback[0].RecordKey, fallbackDifferentTime[0].RecordKey)
	}
	if fallback[0].Identity != fallbackDifferentTime[0].Identity {
		t.Fatalf("fallback identity changed with timestamp: %#v vs %#v", fallback[0].Identity, fallbackDifferentTime[0].Identity)
	}

	foundFallback := false
	for _, record := range fallbackDifferentOrder {
		if record.Artifact.Filename == withoutID.Filename {
			foundFallback = true
			if record.RecordKey != fallback[0].RecordKey {
				t.Fatalf("fallback record key changed with input order: %q vs %q", record.RecordKey, fallback[0].RecordKey)
			}
			if record.Identity != fallback[0].Identity {
				t.Fatalf("fallback identity changed with input order: %#v vs %#v", record.Identity, fallback[0].Identity)
			}
		}
	}
	if !foundFallback {
		t.Fatalf("did not find fallback artifact in mixed output: %#v", fallbackDifferentOrder)
	}
}

func TestMapArtifactStoreRecordsEvidenceBehavior(t *testing.T) {
	provenance := artifactMapProvenance()
	provenance.Evidence = append(provenance.Evidence, recordcontract.EvidenceReference{
		Kind: artifactStoreManifestEvidenceKind,
		Ref:  artifactStoreManifestEvidenceRef,
	})
	records := mustMapArtifactStoreRecordsWithProvenance(t, []artifact.Artifact{
		artifactStoreTestArtifact("artifact-json", artifact.ArtifactClassVerified, artifact.ArtifactTypeJSON, "files/verify.json", "verify.json", "application/json", 256, artifactMapDigestB(), "verify"),
	}, provenance)

	record := records[0]
	if record.Artifact.Evidence.SourceKind != artifactStoreManifestEvidenceKind {
		t.Fatalf("Artifact.Evidence.SourceKind = %q, want %q", record.Artifact.Evidence.SourceKind, artifactStoreManifestEvidenceKind)
	}
	if record.Artifact.Evidence.SourceRef != artifactStoreManifestEvidenceRef {
		t.Fatalf("Artifact.Evidence.SourceRef = %q, want %q", record.Artifact.Evidence.SourceRef, artifactStoreManifestEvidenceRef)
	}
	if artifactMapEvidenceCount(record.Provenance.Evidence, artifactStoreManifestEvidenceKind, artifactStoreManifestEvidenceRef) != 1 {
		t.Fatalf("manifest provenance evidence count = %d, want 1: %#v", artifactMapEvidenceCount(record.Provenance.Evidence, artifactStoreManifestEvidenceKind, artifactStoreManifestEvidenceRef), record.Provenance.Evidence)
	}
	if record.Artifact.Evidence.DigestSHA256 != "" {
		t.Fatalf("Artifact.Evidence.DigestSHA256 = %q, want empty because mapper should not hash files", record.Artifact.Evidence.DigestSHA256)
	}
}

func TestMapArtifactStoreRecordsConvenienceHelperParity(t *testing.T) {
	artifacts := []artifact.Artifact{
		artifactStoreTestArtifact("artifact-step", artifact.ArtifactClassExecutionOutput, artifact.ArtifactTypeSTEP, "files/model.step", "model.step", "model/step", 128, artifactMapDigestA(), "export"),
		artifactStoreTestArtifact("artifact-json", artifact.ArtifactClassVerified, artifact.ArtifactTypeJSON, "files/verify.json", "verify.json", "application/json", 256, artifactMapDigestB(), "verify"),
	}
	recordsInput := ArtifactMappingInput{
		Artifacts:       artifacts,
		RecordKeyPrefix: "run-123",
		Provenance:      artifactMapProvenance(),
	}
	output, err := MapArtifactStoreRecords(recordsInput)
	if err != nil {
		t.Fatalf("MapArtifactStoreRecords returned error: %v", err)
	}
	records, err := MapArtifactStoreRecordsToArtifactRecords(recordsInput)
	if err != nil {
		t.Fatalf("MapArtifactStoreRecordsToArtifactRecords returned error: %v", err)
	}
	if !reflect.DeepEqual(records, output.ArtifactRecords) {
		t.Fatalf("records helper differs from output:\nhelper: %#v\noutput: %#v", records, output.ArtifactRecords)
	}

	manifestInput := ArtifactManifestMappingInput{
		Manifest:        artifact.Manifest{Artifacts: artifacts},
		RecordKeyPrefix: "run-123",
		Provenance:      artifactMapProvenance(),
	}
	manifestOutput, err := MapArtifactStoreManifest(manifestInput)
	if err != nil {
		t.Fatalf("MapArtifactStoreManifest returned error: %v", err)
	}
	manifestRecords, err := MapArtifactStoreManifestToArtifactRecords(manifestInput)
	if err != nil {
		t.Fatalf("MapArtifactStoreManifestToArtifactRecords returned error: %v", err)
	}
	if !reflect.DeepEqual(manifestRecords, manifestOutput.ArtifactRecords) {
		t.Fatalf("manifest helper differs from output:\nhelper: %#v\noutput: %#v", manifestRecords, manifestOutput.ArtifactRecords)
	}
}

func assertArtifactMapRecordSurface(t *testing.T, record recordcontract.ArtifactRecord, wantClass recordcontract.ArtifactClass, wantType recordcontract.ArtifactType, source artifact.Artifact) {
	t.Helper()

	if record.Artifact.Class != wantClass {
		t.Fatalf("%s class = %q, want %q", source.Filename, record.Artifact.Class, wantClass)
	}
	if record.Artifact.Type != wantType {
		t.Fatalf("%s type = %q, want %q", source.Filename, record.Artifact.Type, wantType)
	}
	if record.Artifact.Path != source.Path {
		t.Fatalf("%s path = %q, want %q", source.Filename, record.Artifact.Path, source.Path)
	}
	if record.Artifact.Filename != source.Filename {
		t.Fatalf("%s filename = %q, want %q", source.Filename, record.Artifact.Filename, source.Filename)
	}
	if record.Artifact.MimeType != source.MimeType {
		t.Fatalf("%s MIME type = %q, want %q", source.Filename, record.Artifact.MimeType, source.MimeType)
	}
	if record.Artifact.SizeBytes != source.SizeBytes {
		t.Fatalf("%s sizeBytes = %d, want %d", source.Filename, record.Artifact.SizeBytes, source.SizeBytes)
	}
	if record.Artifact.ChecksumSHA256 != source.ChecksumSHA256 {
		t.Fatalf("%s checksum = %q, want %q", source.Filename, record.Artifact.ChecksumSHA256, source.ChecksumSHA256)
	}
	if record.Artifact.Linkage.JobID != source.JobID {
		t.Fatalf("%s job linkage = %q, want %q", source.Filename, record.Artifact.Linkage.JobID, source.JobID)
	}
	if record.Artifact.Linkage.ProductKey != source.ProductID {
		t.Fatalf("%s product linkage = %q, want %q", source.Filename, record.Artifact.Linkage.ProductKey, source.ProductID)
	}
	if record.Artifact.Linkage.StepRef != source.StepID {
		t.Fatalf("%s step linkage = %q, want %q", source.Filename, record.Artifact.Linkage.StepRef, source.StepID)
	}
}

func mustMapArtifactStoreRecords(t *testing.T, artifacts []artifact.Artifact) []recordcontract.ArtifactRecord {
	t.Helper()
	return mustMapArtifactStoreRecordsWithProvenance(t, artifacts, artifactMapProvenance())
}

func mustMapArtifactStoreRecordsWithProvenance(t *testing.T, artifacts []artifact.Artifact, provenance recordcontract.Provenance) []recordcontract.ArtifactRecord {
	t.Helper()

	output, err := MapArtifactStoreRecords(ArtifactMappingInput{
		Artifacts:       artifacts,
		RecordKeyPrefix: "run-123",
		Provenance:      provenance,
	})
	if err != nil {
		t.Fatalf("MapArtifactStoreRecords returned error: %v", err)
	}
	for _, record := range output.ArtifactRecords {
		if err := recordcontract.ValidateArtifactRecord(record); err != nil {
			t.Fatalf("ValidateArtifactRecord(%s) returned error: %v", record.RecordKey, err)
		}
	}
	return output.ArtifactRecords
}

func mustMarshalArtifactMapRecords(t *testing.T, records []recordcontract.ArtifactRecord) []byte {
	t.Helper()

	data, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("json.Marshal(records) returned error: %v", err)
	}
	return data
}

func artifactStoreTestArtifact(id string, class artifact.ArtifactClass, typ artifact.ArtifactType, path, filename, mimeType string, sizeBytes int64, checksum, stepID string) artifact.Artifact {
	return artifact.Artifact{
		ID:             id,
		Class:          class,
		Type:           typ,
		Path:           path,
		Filename:       filename,
		MimeType:       mimeType,
		SizeBytes:      sizeBytes,
		ChecksumSHA256: checksum,
		JobID:          "job-123",
		ProductID:      "product-abc",
		StepID:         stepID,
		CreatedAt:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func artifactMapProvenance() recordcontract.Provenance {
	return recordcontract.Provenance{
		SourceRevision: recordcontract.SourceRevisionProvenance{
			RevisionID:   "rev-123",
			AssetID:      "asset-abc",
			DigestSHA256: artifactMapDigestD(),
		},
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: "dsl", Identity: "input.parametron", DigestSHA256: artifactMapDigestA()},
		},
		Plan: recordcontract.PlanProvenance{
			PlanID:   "plan-123",
			PlanHash: "plan-hash-abc",
		},
		Linkage: recordcontract.LinkageProvenance{
			ProductKey: "product-abc",
			JobID:      "job-123",
			StepRef:    "export",
		},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: "runtime-log", Ref: "raw/runtime/run.log", DigestSHA256: artifactMapDigestB()},
		},
		Runtime: recordcontract.RuntimeProvenance{
			ToolID:    "parametron-engine",
			RuntimeID: "local-headless",
			Adapter:   "freecad",
		},
	}
}

func hasArtifactMapProvenanceEvidence(provenance recordcontract.Provenance, kind, ref string) bool {
	return artifactMapEvidenceCount(provenance.Evidence, kind, ref) > 0
}

func artifactMapEvidenceCount(evidence []recordcontract.EvidenceReference, kind, ref string) int {
	count := 0
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref {
			count++
		}
	}
	return count
}

func artifactMapDigestA() string {
	return strings.Repeat("a", 64)
}

func artifactMapDigestB() string {
	return strings.Repeat("b", 64)
}

func artifactMapDigestC() string {
	return strings.Repeat("c", 64)
}

func artifactMapDigestD() string {
	return strings.Repeat("d", 64)
}
