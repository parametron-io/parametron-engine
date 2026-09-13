package recordcontract

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestBuildArtifactRecordDerivesIdentity(t *testing.T) {
	record, err := BuildArtifactRecord(validArtifactRecordInput())
	if err != nil {
		t.Fatalf("BuildArtifactRecord(valid) returned error: %v", err)
	}

	if record.Family != FamilyArtifact {
		t.Fatalf("Family = %q, want %q", record.Family, FamilyArtifact)
	}
	if record.Version != CurrentVersion {
		t.Fatalf("Version = %q, want %q", record.Version, CurrentVersion)
	}
	if record.RecordKey != "artifact-record-1" {
		t.Fatalf("RecordKey = %q, want trimmed artifact-record-1", record.RecordKey)
	}
	if record.Provenance.SourceRevision.RevisionID != "rev-1" {
		t.Fatalf("Provenance.SourceRevision.RevisionID = %q, want normalized rev-1", record.Provenance.SourceRevision.RevisionID)
	}
	if record.Identity.ID == "" || !isLowerHex64(record.Identity.ID) {
		t.Fatalf("Identity.ID = %q, want non-empty lowercase SHA-256 hex", record.Identity.ID)
	}
	if record.Identity.Algorithm != IdentityAlgorithmSHA256CanonicalV1 {
		t.Fatalf("Identity.Algorithm = %q, want %q", record.Identity.Algorithm, IdentityAlgorithmSHA256CanonicalV1)
	}
	if record.Identity.Family != FamilyArtifact {
		t.Fatalf("Identity.Family = %q, want %q", record.Identity.Family, FamilyArtifact)
	}
	if record.Identity.Version != CurrentVersion {
		t.Fatalf("Identity.Version = %q, want %q", record.Identity.Version, CurrentVersion)
	}
	if err := ValidateArtifactRecord(record); err != nil {
		t.Fatalf("ValidateArtifactRecord(built) returned error: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ArtifactRecordInput)
	}{
		{name: "record key", mutate: func(input *ArtifactRecordInput) { input.RecordKey = "artifact-record-2" }},
		{name: "provenance", mutate: func(input *ArtifactRecordInput) { input.Provenance.SourceRevision.RevisionID = "rev-2" }},
		{name: "artifact payload", mutate: func(input *ArtifactRecordInput) { input.Artifact.Filename = "model-v2.step" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := validArtifactRecordInput()
			tc.mutate(&changed)

			got, err := BuildArtifactRecord(changed)
			if err != nil {
				t.Fatalf("BuildArtifactRecord(changed) returned error: %v", err)
			}
			if got.Identity.ID == record.Identity.ID {
				t.Fatalf("identity ID did not change after %s changed: %q", tc.name, got.Identity.ID)
			}
		})
	}
}

func TestNormalizeArtifactRecordDeterministic(t *testing.T) {
	first, err := BuildArtifactRecord(validArtifactRecordInput())
	if err != nil {
		t.Fatalf("BuildArtifactRecord(first) returned error: %v", err)
	}
	second, err := BuildArtifactRecord(equivalentArtifactRecordInput())
	if err != nil {
		t.Fatalf("BuildArtifactRecord(second) returned error: %v", err)
	}

	if first.Identity != second.Identity {
		t.Fatalf("equivalent identities differ:\nfirst:  %#v\nsecond: %#v", first.Identity, second.Identity)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent records differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}

	normalized := NormalizeArtifactRecord(ArtifactRecord{
		Family:   Family(" \t" + string(FamilyArtifact) + "\n"),
		Version:  " " + CurrentVersion + "\t",
		RecordKey: "\nartifact-record-1 ",
		Identity: first.Identity,
		Provenance: equivalentArtifactRecordInput().Provenance,
		Artifact: equivalentArtifactRecordInput().Artifact,
	})

	if !reflect.DeepEqual(normalized.Provenance, first.Provenance) {
		t.Fatalf("NormalizeArtifactRecord provenance = %#v, want %#v", normalized.Provenance, first.Provenance)
	}
	if !reflect.DeepEqual(normalized.Artifact, first.Artifact) {
		t.Fatalf("NormalizeArtifactRecord artifact = %#v, want %#v", normalized.Artifact, first.Artifact)
	}
}

func TestArtifactRecordIdentityChangesWithSemanticMaterial(t *testing.T) {
	base := validArtifactRecordInput()
	baseRecord, err := BuildArtifactRecord(base)
	if err != nil {
		t.Fatalf("BuildArtifactRecord(base) returned error: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ArtifactRecordInput)
	}{
		{name: "artifact class", mutate: func(input *ArtifactRecordInput) { input.Artifact.Class = ArtifactClassVerified }},
		{name: "artifact type", mutate: func(input *ArtifactRecordInput) { input.Artifact.Type = ArtifactTypeCSV }},
		{name: "checksum", mutate: func(input *ArtifactRecordInput) { input.Artifact.ChecksumSHA256 = digestB() }},
		{name: "size bytes", mutate: func(input *ArtifactRecordInput) { input.Artifact.SizeBytes = 2048 }},
		{name: "declared output id", mutate: func(input *ArtifactRecordInput) { input.Artifact.DeclaredOutput.OutputID = "mesh-output" }},
		{name: "declared output format", mutate: func(input *ArtifactRecordInput) { input.Artifact.DeclaredOutput.Format = "csv" }},
		{name: "job linkage", mutate: func(input *ArtifactRecordInput) { input.Artifact.Linkage.JobID = "job-b" }},
		{name: "product linkage", mutate: func(input *ArtifactRecordInput) { input.Artifact.Linkage.ProductKey = "product-b" }},
		{name: "step linkage", mutate: func(input *ArtifactRecordInput) { input.Artifact.Linkage.StepRef = "verify" }},
		{name: "evidence source", mutate: func(input *ArtifactRecordInput) { input.Artifact.Evidence.SourceRef = "logs/export.log" }},
		{name: "evidence digest", mutate: func(input *ArtifactRecordInput) { input.Artifact.Evidence.DigestSHA256 = digestC() }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := base
			changed.Provenance.Inputs = append([]ProvenanceInput(nil), base.Provenance.Inputs...)
			changed.Provenance.Evidence = append([]EvidenceReference(nil), base.Provenance.Evidence...)
			tc.mutate(&changed)

			got, err := BuildArtifactRecord(changed)
			if err != nil {
				t.Fatalf("BuildArtifactRecord(changed) returned error: %v", err)
			}
			if got.Identity.ID == baseRecord.Identity.ID {
				t.Fatalf("identity ID did not change after %s changed: %q", tc.name, got.Identity.ID)
			}
		})
	}
}

func TestValidateArtifactRecordAcceptsSupportedContract(t *testing.T) {
	for _, class := range []ArtifactClass{ArtifactClassExecutionOutput, ArtifactClassVerified} {
		t.Run(string(class), func(t *testing.T) {
			input := validArtifactRecordInput()
			input.Artifact.Class = class
			record := mustBuildArtifactRecord(t, input)
			if err := ValidateArtifactRecord(record); err != nil {
				t.Fatalf("ValidateArtifactRecord(%s) returned error: %v", class, err)
			}
		})
	}

	for _, artifactType := range []ArtifactType{ArtifactTypeSTEP, ArtifactTypeCSV, ArtifactTypePDF, ArtifactTypeJSON, ArtifactTypeLog} {
		t.Run(string(artifactType), func(t *testing.T) {
			input := validArtifactRecordInput()
			input.Artifact.Type = artifactType
			record := mustBuildArtifactRecord(t, input)
			if err := ValidateArtifactRecord(record); err != nil {
				t.Fatalf("ValidateArtifactRecord(%s) returned error: %v", artifactType, err)
			}
		})
	}

	input := validArtifactRecordInput()
	input.Artifact.Type = ArtifactTypeUnknown
	input.Artifact.ChecksumSHA256 = digestA()
	record := mustBuildArtifactRecord(t, input)
	if err := ValidateArtifactRecord(record); err != nil {
		t.Fatalf("ValidateArtifactRecord(unknown with checksum) returned error: %v", err)
	}
}

func TestValidateArtifactRecordRejectsInvalidContract(t *testing.T) {
	valid := mustBuildArtifactRecord(t, validArtifactRecordInput())

	tests := []struct {
		name   string
		mutate func(*ArtifactRecord)
	}{
		{name: "wrong family", mutate: func(record *ArtifactRecord) { record.Family = FamilyExecution }},
		{name: "unsupported version", mutate: func(record *ArtifactRecord) { record.Version = "1.1" }},
		{name: "blank record key", mutate: func(record *ArtifactRecord) { record.RecordKey = " " }},
		{name: "invalid provenance", mutate: func(record *ArtifactRecord) { record.Provenance.Inputs[0].DigestSHA256 = upperDigestA() }},
		{name: "missing identity", mutate: func(record *ArtifactRecord) { record.Identity = Identity{} }},
		{name: "identity with wrong family", mutate: func(record *ArtifactRecord) { record.Identity.Family = FamilyExecution }},
		{name: "identity with wrong algorithm", mutate: func(record *ArtifactRecord) { record.Identity.Algorithm = "sha256-other" }},
		{name: "identity mismatch after payload mutation", mutate: func(record *ArtifactRecord) { record.Artifact.SizeBytes++ }},
		{name: "blank artifact class", mutate: func(record *ArtifactRecord) { record.Artifact.Class = " " }},
		{name: "unknown artifact class", mutate: func(record *ArtifactRecord) { record.Artifact.Class = "preview" }},
		{name: "blank artifact type", mutate: func(record *ArtifactRecord) { record.Artifact.Type = "\t" }},
		{name: "unsupported artifact type", mutate: func(record *ArtifactRecord) { record.Artifact.Type = "mesh" }},
		{name: "negative size", mutate: func(record *ArtifactRecord) { record.Artifact.SizeBytes = -1 }},
		{name: "malformed checksum", mutate: func(record *ArtifactRecord) { record.Artifact.ChecksumSHA256 = upperDigestA() }},
		{name: "malformed evidence digest", mutate: func(record *ArtifactRecord) { record.Artifact.Evidence.DigestSHA256 = "abc123" }},
		{name: "missing locator and evidence material", mutate: func(record *ArtifactRecord) {
			record.Artifact.URI = ""
			record.Artifact.Path = ""
			record.Artifact.Filename = ""
			record.Artifact.ChecksumSHA256 = ""
			record.Artifact.Evidence.SourceRef = ""
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneArtifactRecord(valid)
			tc.mutate(&record)

			if err := ValidateArtifactRecord(record); !errors.Is(err, ErrInvalidArtifactRecord) {
				t.Fatalf("ValidateArtifactRecord error = %v, want ErrInvalidArtifactRecord", err)
			}
		})
	}
}

func TestValidateArtifactRecordUnknownTypeRequiresUsefulEvidence(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ArtifactRecordInput)
		wantValid bool
	}{
		{name: "checksum", mutate: func(input *ArtifactRecordInput) {
			input.Artifact.ChecksumSHA256 = digestA()
		}, wantValid: true},
		{name: "evidence digest", mutate: func(input *ArtifactRecordInput) {
			input.Artifact.Evidence.DigestSHA256 = digestB()
		}, wantValid: true},
		{name: "uri with positive size", mutate: func(input *ArtifactRecordInput) {
			input.Artifact.URI = "parametron://artifacts/model"
			input.Artifact.SizeBytes = 1
		}, wantValid: true},
		{name: "path with positive size", mutate: func(input *ArtifactRecordInput) {
			input.Artifact.Path = "out/model.bin"
			input.Artifact.SizeBytes = 1
		}, wantValid: true},
		{name: "filename with positive size", mutate: func(input *ArtifactRecordInput) {
			input.Artifact.Filename = "model.bin"
			input.Artifact.SizeBytes = 1
		}, wantValid: true},
		{name: "locator without positive size", mutate: func(input *ArtifactRecordInput) {
			input.Artifact.URI = "parametron://artifacts/model"
			input.Artifact.Path = ""
			input.Artifact.Filename = ""
			input.Artifact.SizeBytes = 0
		}},
		{name: "source ref without checksum digest or positive sized locator", mutate: func(input *ArtifactRecordInput) {
			input.Artifact.URI = ""
			input.Artifact.Path = ""
			input.Artifact.Filename = ""
			input.Artifact.ChecksumSHA256 = ""
			input.Artifact.Evidence.SourceRef = "runtime-observation"
			input.Artifact.Evidence.DigestSHA256 = ""
			input.Artifact.SizeBytes = 0
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validArtifactRecordInput()
			input.Artifact.Type = ArtifactTypeUnknown
			input.Artifact.ChecksumSHA256 = ""
			input.Artifact.Evidence.DigestSHA256 = ""
			tc.mutate(&input)

			record, err := BuildArtifactRecord(input)
			if tc.wantValid {
				if err != nil {
					t.Fatalf("BuildArtifactRecord(valid unknown type) returned error: %v", err)
				}
				if err := ValidateArtifactRecord(record); err != nil {
					t.Fatalf("ValidateArtifactRecord(valid unknown type) returned error: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidArtifactRecord) {
				t.Fatalf("BuildArtifactRecord error = %v, want ErrInvalidArtifactRecord", err)
			}
		})
	}
}

func TestArtifactRecordValidationErrorsAreInspectable(t *testing.T) {
	record := mustBuildArtifactRecord(t, validArtifactRecordInput())

	tests := []struct {
		name       string
		record     ArtifactRecord
		wantErrors []error
	}{
		{
			name: "direct artifact validation failure",
			record: withArtifactRecord(record, func(record *ArtifactRecord) {
				record.Artifact.Type = "mesh"
			}),
			wantErrors: []error{ErrInvalidArtifactRecord},
		},
		{
			name: "invalid provenance",
			record: withArtifactRecord(record, func(record *ArtifactRecord) {
				record.Provenance.Inputs[0].DigestSHA256 = "abc123"
			}),
			wantErrors: []error{ErrInvalidArtifactRecord, ErrInvalidProvenance},
		},
		{
			name: "invalid identity",
			record: withArtifactRecord(record, func(record *ArtifactRecord) {
				record.Identity.ID = "not-a-digest"
			}),
			wantErrors: []error{ErrInvalidArtifactRecord, ErrInvalidIdentity},
		},
		{
			name: "invalid version",
			record: withArtifactRecord(record, func(record *ArtifactRecord) {
				record.Version = "2.0"
			}),
			wantErrors: []error{ErrInvalidArtifactRecord, ErrInvalidVersion},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArtifactRecord(tc.record)
			if err == nil {
				t.Fatal("ValidateArtifactRecord returned nil error, want validation failure")
			}
			for _, want := range tc.wantErrors {
				if !errors.Is(err, want) {
					t.Fatalf("ValidateArtifactRecord error = %v, want errors.Is(..., %v)", err, want)
				}
			}
		})
	}
}

func TestArtifactRecordNormalizationIsCopySafe(t *testing.T) {
	input := validArtifactRecordInput()
	original := input
	original.Provenance.Inputs = append([]ProvenanceInput(nil), input.Provenance.Inputs...)
	original.Provenance.Evidence = append([]EvidenceReference(nil), input.Provenance.Evidence...)

	built, err := BuildArtifactRecord(input)
	if err != nil {
		t.Fatalf("BuildArtifactRecord(valid) returned error: %v", err)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("BuildArtifactRecord mutated caller input:\ngot:  %#v\nwant: %#v", input, original)
	}

	input.Provenance.Inputs[0].Identity = "mutated-input"
	input.Provenance.Evidence[0].Ref = "mutated-evidence"
	input.Artifact.DeclaredOutput.OutputID = "mutated-output"
	input.Artifact.Linkage.JobID = "mutated-job"
	input.Artifact.Evidence.SourceRef = "mutated-source"
	input.Artifact.Filename = "mutated.step"
	if built.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("built provenance inputs changed after input mutation: %#v", built.Provenance.Inputs)
	}
	if built.Provenance.Evidence[0].Ref != "out/model.step" {
		t.Fatalf("built provenance evidence changed after input mutation: %#v", built.Provenance.Evidence)
	}
	if built.Artifact.DeclaredOutput.OutputID != "model-output" {
		t.Fatalf("built declared output changed after input mutation: %#v", built.Artifact.DeclaredOutput)
	}
	if built.Artifact.Linkage.JobID != "job-a" {
		t.Fatalf("built linkage changed after input mutation: %#v", built.Artifact.Linkage)
	}
	if built.Artifact.Evidence.SourceRef != "runtime://artifact/model.step" {
		t.Fatalf("built artifact evidence changed after input mutation: %#v", built.Artifact.Evidence)
	}
	if built.Artifact.Filename != "model.step" {
		t.Fatalf("built artifact summary changed after input mutation: %#v", built.Artifact)
	}

	normalizedInput := ArtifactRecord{
		Provenance: Provenance{
			Inputs:   []ProvenanceInput{{Kind: "dsl", Identity: "model-a"}},
			Evidence: []EvidenceReference{{Kind: "artifact", Ref: "out/model.step"}},
		},
		Artifact: ArtifactSummary{
			Class:    ArtifactClassExecutionOutput,
			Type:     ArtifactTypeSTEP,
			Filename: " model.step ",
			DeclaredOutput: ArtifactDeclaredOutput{
				OutputID: " model-output ",
			},
			Linkage: ArtifactLinkage{
				JobID: " job-a ",
			},
			Evidence: ArtifactEvidence{
				SourceRef: " runtime://artifact/model.step ",
			},
		},
	}
	normalized := NormalizeArtifactRecord(normalizedInput)
	normalizedInput.Provenance.Inputs[0].Identity = "mutated-input"
	normalizedInput.Provenance.Evidence[0].Ref = "mutated-evidence"
	normalizedInput.Artifact.DeclaredOutput.OutputID = "mutated-output"
	normalizedInput.Artifact.Linkage.JobID = "mutated-job"
	normalizedInput.Artifact.Evidence.SourceRef = "mutated-source"
	normalizedInput.Artifact.Filename = "mutated.step"
	if normalized.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("normalized provenance inputs changed after input mutation: %#v", normalized.Provenance.Inputs)
	}
	if normalized.Provenance.Evidence[0].Ref != "out/model.step" {
		t.Fatalf("normalized provenance evidence changed after input mutation: %#v", normalized.Provenance.Evidence)
	}
	if normalized.Artifact.DeclaredOutput.OutputID != "model-output" {
		t.Fatalf("normalized declared output changed after input mutation: %#v", normalized.Artifact.DeclaredOutput)
	}
	if normalized.Artifact.Linkage.JobID != "job-a" {
		t.Fatalf("normalized linkage changed after input mutation: %#v", normalized.Artifact.Linkage)
	}
	if normalized.Artifact.Evidence.SourceRef != "runtime://artifact/model.step" {
		t.Fatalf("normalized artifact evidence changed after input mutation: %#v", normalized.Artifact.Evidence)
	}
	if normalized.Artifact.Filename != "model.step" {
		t.Fatalf("normalized artifact summary changed after input mutation: %#v", normalized.Artifact)
	}
}

func TestArtifactRecordJSONContractRootFields(t *testing.T) {
	record := mustBuildArtifactRecord(t, validArtifactRecordInput())

	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal(ArtifactRecord) returned error: %v", err)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		t.Fatalf("json.Unmarshal(root) returned error: %v", err)
	}
	for _, field := range []string{"family", "version", "recordKey", "identity", "provenance", "artifact"} {
		if _, ok := root[field]; !ok {
			t.Fatalf("root JSON field %q missing from %s", field, payload)
		}
	}

	var artifact map[string]json.RawMessage
	if err := json.Unmarshal(root["artifact"], &artifact); err != nil {
		t.Fatalf("json.Unmarshal(artifact) returned error: %v", err)
	}
	for _, field := range []string{
		"class",
		"type",
		"uri",
		"path",
		"filename",
		"mimeType",
		"sizeBytes",
		"checksumSha256",
		"declaredOutput",
		"linkage",
		"evidence",
	} {
		if _, ok := artifact[field]; !ok {
			t.Fatalf("artifact JSON field %q missing from %s", field, payload)
		}
	}
}

func validArtifactRecordInput() ArtifactRecordInput {
	return ArtifactRecordInput{
		RecordKey: " artifact-record-1 ",
		Provenance: Provenance{
			SourceRevision: SourceRevisionProvenance{
				RevisionID:   " rev-1 ",
				AssetID:      " asset-1 ",
				DigestSHA256: digestA(),
			},
			Inputs: []ProvenanceInput{
				{Kind: " parameter-table ", Identity: " params-a ", DigestSHA256: digestC()},
				{Kind: " dsl ", Identity: " model-a ", DigestSHA256: digestD()},
			},
			Plan: PlanProvenance{
				PlanID:   " plan-1 ",
				PlanHash: " plan-hash-1 ",
			},
			Linkage: LinkageProvenance{
				ProductKey: " product-a ",
				JobID:      " job-a ",
				StepRef:    " export ",
			},
			Evidence: []EvidenceReference{
				{Kind: " log ", Ref: " logs/run.log ", DigestSHA256: digestE()},
				{Kind: " artifact ", Ref: " out/model.step ", DigestSHA256: digestF()},
			},
			Runtime: RuntimeProvenance{
				ToolID:    " parametron ",
				RuntimeID: " runtime-1 ",
				Adapter:   " freecad ",
			},
		},
		Artifact: ArtifactSummary{
			Class:          " execution_output ",
			Type:           " step ",
			URI:            " parametron://artifacts/model.step ",
			Path:           " out/model.step ",
			Filename:       " model.step ",
			MimeType:       " model/step ",
			SizeBytes:      1024,
			ChecksumSHA256: " " + digestA() + " ",
			DeclaredOutput: ArtifactDeclaredOutput{
				OutputID:   " model-output ",
				OutputName: " Model STEP ",
				OutputType: " cad-model ",
				Format:     " step ",
				ObjectRef:  " object/model ",
			},
			Linkage: ArtifactLinkage{
				JobID:      " job-a ",
				ProductKey: " product-a ",
				StepRef:    " export ",
			},
			Evidence: ArtifactEvidence{
				SourceKind:   " runtime ",
				SourceRef:    " runtime://artifact/model.step ",
				DigestSHA256: " " + digestB() + " ",
			},
		},
	}
}

func equivalentArtifactRecordInput() ArtifactRecordInput {
	input := validArtifactRecordInput()
	input.RecordKey = "\tartifact-record-1\n"
	input.Provenance.Inputs = []ProvenanceInput{
		{Kind: "dsl", Identity: "model-a", DigestSHA256: " " + digestD() + " "},
		{Kind: "parameter-table", Identity: "params-a", DigestSHA256: digestC()},
	}
	input.Provenance.Evidence = []EvidenceReference{
		{Kind: "artifact", Ref: "out/model.step", DigestSHA256: digestF()},
		{Kind: "log", Ref: "logs/run.log", DigestSHA256: digestE()},
	}
	input.Artifact = ArtifactSummary{
		Class:          "\texecution_output\n",
		Type:           " step ",
		URI:            "\tparametron://artifacts/model.step ",
		Path:           " out/model.step\n",
		Filename:       "\tmodel.step ",
		MimeType:       " model/step\n",
		SizeBytes:      1024,
		ChecksumSHA256: "\t" + digestA() + " ",
		DeclaredOutput: ArtifactDeclaredOutput{
			OutputID:   "\tmodel-output ",
			OutputName: " Model STEP\n",
			OutputType: "\tcad-model ",
			Format:     " step\n",
			ObjectRef:  "\tobject/model ",
		},
		Linkage: ArtifactLinkage{
			JobID:      "\tjob-a ",
			ProductKey: " product-a\n",
			StepRef:    "\texport ",
		},
		Evidence: ArtifactEvidence{
			SourceKind:   "\truntime ",
			SourceRef:    " runtime://artifact/model.step\n",
			DigestSHA256: "\t" + digestB() + " ",
		},
	}
	return input
}

func mustBuildArtifactRecord(t *testing.T, input ArtifactRecordInput) ArtifactRecord {
	t.Helper()
	record, err := BuildArtifactRecord(input)
	if err != nil {
		t.Fatalf("BuildArtifactRecord(%#v) returned error: %v", input, err)
	}
	return record
}

func withArtifactRecord(base ArtifactRecord, mutate func(*ArtifactRecord)) ArtifactRecord {
	base = cloneArtifactRecord(base)
	mutate(&base)
	return base
}

func cloneArtifactRecord(record ArtifactRecord) ArtifactRecord {
	record.Provenance.Inputs = append([]ProvenanceInput(nil), record.Provenance.Inputs...)
	record.Provenance.Evidence = append([]EvidenceReference(nil), record.Provenance.Evidence...)
	return record
}
