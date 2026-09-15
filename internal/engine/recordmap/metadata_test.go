package recordmap_test

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/metadata"
	"parametron/internal/engine/projectinput"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
)

const metadataEvidenceRef = "raw/prm.metadata.json"

func TestMapMetadataPublicAPISmokeNormalizesValidProvenance(t *testing.T) {
	tempDir := t.TempDir()
	entriesBefore, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir before mapping returned error: %v", err)
	}

	md := validMetadata()
	md.PlanHash = " " + digestA() + " "
	md.ProjectInputs = &projectinput.CapturedResources{
		DSL: projectinput.CapturedResource{
			ResolvedPath: " project/main.pmtn ",
			Signature:    digestB(),
			Kind:         projectinput.ResourceKindDSL,
		},
	}

	first, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md})
	if err != nil {
		t.Fatalf("MapMetadata returned error: %v", err)
	}
	if err := recordcontract.ValidateProvenance(first.Provenance); err != nil {
		t.Fatalf("ValidateProvenance(mapped) returned error: %v", err)
	}
	if !reflect.DeepEqual(first.Inputs, first.Provenance.Inputs) {
		t.Fatalf("output Inputs = %#v, want provenance Inputs %#v", first.Inputs, first.Provenance.Inputs)
	}

	second, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md})
	if err != nil {
		t.Fatalf("MapMetadata repeated call returned error: %v", err)
	}
	if !reflect.DeepEqual(first.Inputs, second.Inputs) {
		t.Fatalf("MapMetadata inputs are not deterministic:\nfirst:  %#v\nsecond: %#v", first.Inputs, second.Inputs)
	}

	entriesAfter, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir after mapping returned error: %v", err)
	}
	if !reflect.DeepEqual(entriesBefore, entriesAfter) {
		t.Fatalf("MapMetadata wrote files in temp dir: before=%#v after=%#v", entriesBefore, entriesAfter)
	}
	if hasInputKind(first.Inputs, "artifact") || hasInputKind(first.Inputs, "record") {
		t.Fatalf("metadata mapping produced artifact/record input surfaces: %#v", first.Inputs)
	}
}

func TestMapMetadataRejectsInvalidSchemaVersion(t *testing.T) {
	tests := []struct {
		name          string
		schemaVersion string
	}{
		{name: "blank", schemaVersion: " "},
		{name: "unsupported", schemaVersion: "2.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := validMetadata()
			md.SchemaVersion = tt.schemaVersion
			_, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md})
			assertInvalidMetadataMapping(t, err)
		})
	}
}

func TestMapMetadataPlanProvenance(t *testing.T) {
	t.Run("metadata plan hash fills empty caller hash", func(t *testing.T) {
		md := validMetadata()
		md.PlanHash = " " + digestA() + " "

		got, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md})
		if err != nil {
			t.Fatalf("MapMetadata returned error: %v", err)
		}
		if got.Provenance.Plan.PlanHash != digestA() {
			t.Fatalf("PlanHash = %q, want trimmed metadata hash", got.Provenance.Plan.PlanHash)
		}
		if got.Provenance.Plan.PlanID != "" {
			t.Fatalf("PlanID = %q, want empty", got.Provenance.Plan.PlanID)
		}
	})

	t.Run("caller plan hash is preserved", func(t *testing.T) {
		md := validMetadata()
		md.PlanHash = digestA()
		caller := recordcontract.Provenance{Plan: recordcontract.PlanProvenance{PlanHash: " " + digestB() + " "}}

		got, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
		if err != nil {
			t.Fatalf("MapMetadata returned error: %v", err)
		}
		if got.Provenance.Plan.PlanHash != digestB() {
			t.Fatalf("PlanHash = %q, want caller hash %q", got.Provenance.Plan.PlanHash, digestB())
		}
		if got.Provenance.Plan.PlanID != "" {
			t.Fatalf("PlanID = %q, want empty", got.Provenance.Plan.PlanID)
		}
	})
}

func TestMapMetadataEvidenceMapping(t *testing.T) {
	t.Run("appends raw metadata evidence once", func(t *testing.T) {
		caller := recordcontract.Provenance{
			Evidence: []recordcontract.EvidenceReference{
				{Kind: "report", Ref: "raw/report.json", DigestSHA256: digestA()},
			},
		}

		got, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: validMetadata(), Provenance: caller})
		if err != nil {
			t.Fatalf("MapMetadata returned error: %v", err)
		}

		if countEvidence(got.Provenance.Evidence, "metadata", metadataEvidenceRef) != 1 {
			t.Fatalf("metadata evidence count = %d, want 1 in %#v", countEvidence(got.Provenance.Evidence, "metadata", metadataEvidenceRef), got.Provenance.Evidence)
		}
		metadataEvidence := findEvidence(got.Provenance.Evidence, "metadata", metadataEvidenceRef)
		if metadataEvidence == nil {
			t.Fatal("metadata evidence not found")
		}
		if metadataEvidence.DigestSHA256 != "" {
			t.Fatalf("metadata evidence digest = %q, want empty", metadataEvidence.DigestSHA256)
		}
		if !hasEvidence(got.Provenance.Evidence, "report", "raw/report.json", digestA()) {
			t.Fatalf("caller evidence not preserved: %#v", got.Provenance.Evidence)
		}
		if hasInputKind(got.Provenance.Inputs, "metadata") || hasInputKind(got.Provenance.Inputs, "artifact") || hasInputKind(got.Provenance.Inputs, "record") {
			t.Fatalf("raw metadata represented as normalized input/artifact/record surface: %#v", got.Provenance.Inputs)
		}
	})

	t.Run("does not duplicate existing identical metadata evidence", func(t *testing.T) {
		caller := recordcontract.Provenance{
			Evidence: []recordcontract.EvidenceReference{
				{Kind: "metadata", Ref: metadataEvidenceRef},
			},
		}

		got, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: validMetadata(), Provenance: caller})
		if err != nil {
			t.Fatalf("MapMetadata returned error: %v", err)
		}
		if countEvidence(got.Provenance.Evidence, "metadata", metadataEvidenceRef) != 1 {
			t.Fatalf("metadata evidence count = %d, want 1 in %#v", countEvidence(got.Provenance.Evidence, "metadata", metadataEvidenceRef), got.Provenance.Evidence)
		}
	})
}

func TestMapMetadataDSLInputIdentityMapping(t *testing.T) {
	t.Run("project DSL surface uses resolved path and signature", func(t *testing.T) {
		md := validMetadata()
		md.DSLHash = digestA()
		md.ProjectInputs = &projectinput.CapturedResources{
			DSL: projectinput.CapturedResource{
				ResolvedPath: " /workspace/project/main.pmtn ",
				Signature:    " " + digestB() + " ",
				Kind:         projectinput.ResourceKindDSL,
			},
		}

		got, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
		if err != nil {
			t.Fatalf("MapMetadataToInputIdentitySurfaces returned error: %v", err)
		}
		input := requireInput(t, got, "dsl", "/workspace/project/main.pmtn")
		if input.DigestSHA256 != digestB() {
			t.Fatalf("DSL digest = %q, want project signature %q", input.DigestSHA256, digestB())
		}
	})

	t.Run("fallback DSL hash uses generic identity", func(t *testing.T) {
		md := validMetadata()
		md.DSLHash = " " + digestA() + " "

		got, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
		if err != nil {
			t.Fatalf("MapMetadataToInputIdentitySurfaces returned error: %v", err)
		}
		input := requireInput(t, got, "dsl", "dsl")
		if input.DigestSHA256 != digestA() {
			t.Fatalf("DSL digest = %q, want DSLHash %q", input.DigestSHA256, digestA())
		}
	})

	t.Run("invalid project DSL signature is rejected", func(t *testing.T) {
		md := validMetadata()
		md.ProjectInputs = &projectinput.CapturedResources{
			DSL: projectinput.CapturedResource{ResolvedPath: "main.pmtn", Signature: "ABC"},
		}
		_, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
		assertInvalidMetadataMapping(t, err)
	})

	t.Run("invalid fallback DSL hash is rejected", func(t *testing.T) {
		md := validMetadata()
		md.DSLHash = "not-a-digest"
		_, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
		assertInvalidMetadataMapping(t, err)
	})
}

func TestMapMetadataModelInputIdentityMapping(t *testing.T) {
	md := validMetadata()
	md.ProjectInputs = &projectinput.CapturedResources{
		Models: []projectinput.CapturedResource{
			{ResolvedPath: "models/bracket.step", Signature: digestB(), Kind: projectinput.ResourceKindModel},
			{LogicalID: "housing", ResolvedPath: "models/housing.step", Signature: digestA(), Kind: projectinput.ResourceKindModel},
		},
	}

	got, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces returned error: %v", err)
	}
	if input := requireInput(t, got, "model", "housing"); input.DigestSHA256 != digestA() {
		t.Fatalf("logical model digest = %q, want %q", input.DigestSHA256, digestA())
	}
	if input := requireInput(t, got, "model", "models/bracket.step"); input.DigestSHA256 != digestB() {
		t.Fatalf("path model digest = %q, want %q", input.DigestSHA256, digestB())
	}

	reversed := validMetadata()
	reversed.ProjectInputs = &projectinput.CapturedResources{
		Models: []projectinput.CapturedResource{md.ProjectInputs.Models[1], md.ProjectInputs.Models[0]},
	}
	gotReversed, err := recordmap.MapMetadataToInputIdentitySurfaces(reversed)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces reversed returned error: %v", err)
	}
	if !reflect.DeepEqual(got, gotReversed) {
		t.Fatalf("model inputs not deterministic across input order:\nfirst:    %#v\nreversed: %#v", got, gotReversed)
	}

	t.Run("missing identity is rejected", func(t *testing.T) {
		md := validMetadata()
		md.ProjectInputs = &projectinput.CapturedResources{
			Models: []projectinput.CapturedResource{{Signature: digestA(), Kind: projectinput.ResourceKindModel}},
		}
		_, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
		assertInvalidMetadataMapping(t, err)
	})

	t.Run("invalid signature is rejected", func(t *testing.T) {
		md := validMetadata()
		md.ProjectInputs = &projectinput.CapturedResources{
			Models: []projectinput.CapturedResource{{LogicalID: "bad", Signature: strings.ToUpper(digestA())}},
		}
		_, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
		assertInvalidMetadataMapping(t, err)
	})
}

func TestMapMetadataTableInputIdentityMapping(t *testing.T) {
	md := validMetadata()
	md.ProjectInputs = &projectinput.CapturedResources{
		Tables: []projectinput.CapturedResource{
			{LogicalID: "fasteners", ResolvedPath: "tables/fasteners.csv", Signature: digestA(), Kind: projectinput.ResourceKindTable},
			{ResolvedPath: "tables/materials.csv", Signature: digestB(), Kind: projectinput.ResourceKindTable},
		},
	}
	md.Tables = []metadata.TableInputMetadata{
		{LogicalID: "legacy", Name: "legacy-name", Fingerprint: digestC()},
		{Name: "adhoc", Fingerprint: digestD()},
		{LogicalID: "fasteners", Name: "duplicate-project", Fingerprint: digestA()},
	}

	got, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces returned error: %v", err)
	}
	for _, want := range []recordcontract.ProvenanceInput{
		{Kind: "table", Identity: "fasteners", DigestSHA256: digestA()},
		{Kind: "table", Identity: "tables/materials.csv", DigestSHA256: digestB()},
		{Kind: "table", Identity: "legacy", DigestSHA256: digestC()},
		{Kind: "table", Identity: "adhoc", DigestSHA256: digestD()},
	} {
		if input := requireInput(t, got, want.Kind, want.Identity); input.DigestSHA256 != want.DigestSHA256 {
			t.Fatalf("%s/%s digest = %q, want %q", want.Kind, want.Identity, input.DigestSHA256, want.DigestSHA256)
		}
	}
	if countInput(got, "table", "fasteners") != 1 {
		t.Fatalf("duplicate table logical ID was not deduplicated: %#v", got)
	}

	reversed := validMetadata()
	reversed.ProjectInputs = &projectinput.CapturedResources{
		Tables: []projectinput.CapturedResource{md.ProjectInputs.Tables[1], md.ProjectInputs.Tables[0]},
	}
	reversed.Tables = []metadata.TableInputMetadata{md.Tables[1], md.Tables[2], md.Tables[0]}
	gotReversed, err := recordmap.MapMetadataToInputIdentitySurfaces(reversed)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces reversed returned error: %v", err)
	}
	if !reflect.DeepEqual(got, gotReversed) {
		t.Fatalf("table inputs not deterministic across input order:\nfirst:    %#v\nreversed: %#v", got, gotReversed)
	}
}

func TestMapMetadataTableInputRejectsInvalidCases(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.Metadata
	}{
		{
			name: "project table duplicate logical ID conflicting digests",
			md: metadataWithProjectTables(
				projectinput.CapturedResource{LogicalID: "fasteners", Signature: digestA()},
				projectinput.CapturedResource{LogicalID: "fasteners", Signature: digestB()},
			),
		},
		{
			name: "project table invalid signature",
			md:   metadataWithProjectTables(projectinput.CapturedResource{LogicalID: "bad", Signature: "xyz"}),
		},
		{
			name: "project table missing identity",
			md:   metadataWithProjectTables(projectinput.CapturedResource{Signature: digestA()}),
		},
		{
			name: "metadata table duplicate logical ID conflicting fingerprints",
			md: metadataWithTables(
				metadata.TableInputMetadata{LogicalID: "fasteners", Fingerprint: digestA()},
				metadata.TableInputMetadata{LogicalID: "fasteners", Fingerprint: digestB()},
			),
		},
		{
			name: "metadata table invalid fingerprint",
			md:   metadataWithTables(metadata.TableInputMetadata{LogicalID: "bad", Fingerprint: strings.ToUpper(digestA())}),
		},
		{
			name: "metadata table missing identity",
			md:   metadataWithTables(metadata.TableInputMetadata{Fingerprint: digestA()}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := recordmap.MapMetadataToInputIdentitySurfaces(tt.md)
			assertInvalidMetadataMapping(t, err)
		})
	}
}

func TestMapMetadataProfileInputIdentityMapping(t *testing.T) {
	md := validMetadata()
	md.Profile = metadata.ProfileMetadata{
		Name: "prod-profile",
		ResolvedSettings: map[string]any{
			"material": "steel",
			"count":    float64(2),
		},
	}

	first, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces returned error: %v", err)
	}
	profileInput := requireInput(t, first, "profile", "prod-profile")
	if profileInput.DigestSHA256 == "" {
		t.Fatal("profile digest is empty, want deterministic digest")
	}

	second, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces repeated returned error: %v", err)
	}
	if got := requireInput(t, second, "profile", "prod-profile"); got.DigestSHA256 != profileInput.DigestSHA256 {
		t.Fatalf("profile digest changed across equivalent calls: %q vs %q", got.DigestSHA256, profileInput.DigestSHA256)
	}

	changed := md
	changed.Profile = metadata.ProfileMetadata{
		Name: "prod-profile",
		ResolvedSettings: map[string]any{
			"material": "aluminum",
			"count":    float64(2),
		},
	}
	changedInputs, err := recordmap.MapMetadataToInputIdentitySurfaces(changed)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces changed returned error: %v", err)
	}
	if got := requireInput(t, changedInputs, "profile", "prod-profile"); got.DigestSHA256 == profileInput.DigestSHA256 {
		t.Fatalf("profile digest did not change after material settings changed: %q", got.DigestSHA256)
	}

	unnamed := validMetadata()
	unnamed.Profile = metadata.ProfileMetadata{ResolvedSettings: map[string]any{"mode": "draft"}}
	unnamedInputs, err := recordmap.MapMetadataToInputIdentitySurfaces(unnamed)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces unnamed returned error: %v", err)
	}
	requireInput(t, unnamedInputs, "profile", "profile")

	zeroInputs, err := recordmap.MapMetadataToInputIdentitySurfaces(validMetadata())
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces zero profile returned error: %v", err)
	}
	if hasInputKind(zeroInputs, "profile") {
		t.Fatalf("zero profile produced profile input: %#v", zeroInputs)
	}
}

func TestMapMetadataRuntimeProvenanceMapping(t *testing.T) {
	t.Run("caller runtime fields are preserved", func(t *testing.T) {
		md := validMetadata()
		md.Toolchain = metadata.ToolchainMetadata{ParametronVersion: "v1.2.3", GoVersion: "go1.25.0"}
		caller := recordcontract.Provenance{
			Runtime: recordcontract.RuntimeProvenance{
				ToolID:    "custom-tool",
				RuntimeID: "runtime-123",
				Adapter:   "freecad",
			},
		}

		got, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
		if err != nil {
			t.Fatalf("MapMetadata returned error: %v", err)
		}
		if got.Provenance.Runtime != caller.Runtime {
			t.Fatalf("Runtime = %#v, want caller runtime %#v", got.Provenance.Runtime, caller.Runtime)
		}
	})

	tests := []struct {
		name      string
		toolchain metadata.ToolchainMetadata
		wantTool  string
		wantID    string
	}{
		{
			name:      "parametron and go version",
			toolchain: metadata.ToolchainMetadata{ParametronVersion: " v1.2.3 ", GoVersion: " go1.25.0 "},
			wantTool:  "parametron",
			wantID:    "parametron=v1.2.3;go=go1.25.0",
		},
		{
			name:      "parametron version only",
			toolchain: metadata.ToolchainMetadata{ParametronVersion: "v1.2.3"},
			wantTool:  "parametron",
			wantID:    "parametron=v1.2.3",
		},
		{
			name:      "go version only",
			toolchain: metadata.ToolchainMetadata{GoVersion: "go1.25.0"},
			wantTool:  "parametron",
			wantID:    "go=go1.25.0",
		},
		{
			name:      "no version fields",
			toolchain: metadata.ToolchainMetadata{},
			wantTool:  "",
			wantID:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := validMetadata()
			md.Toolchain = tt.toolchain
			got, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md})
			if err != nil {
				t.Fatalf("MapMetadata returned error: %v", err)
			}
			if got.Provenance.Runtime.ToolID != tt.wantTool {
				t.Fatalf("Runtime.ToolID = %q, want %q", got.Provenance.Runtime.ToolID, tt.wantTool)
			}
			if got.Provenance.Runtime.RuntimeID != tt.wantID {
				t.Fatalf("Runtime.RuntimeID = %q, want %q", got.Provenance.Runtime.RuntimeID, tt.wantID)
			}
			if got.Provenance.Runtime.Adapter != "" {
				t.Fatalf("Runtime.Adapter = %q, want empty", got.Provenance.Runtime.Adapter)
			}
		})
	}
}

func TestMapMetadataPreservesAndMergesCallerProvenance(t *testing.T) {
	md := validMetadata()
	md.DSLHash = digestA()
	md.ProjectInputs = &projectinput.CapturedResources{
		Models: []projectinput.CapturedResource{
			{LogicalID: "housing", Signature: digestB()},
		},
	}
	caller := recordcontract.Provenance{
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: "caller", Identity: "input", DigestSHA256: digestC()},
			{Kind: "model", Identity: "housing", DigestSHA256: digestB()},
		},
		Plan:    recordcontract.PlanProvenance{PlanHash: digestD()},
		Runtime: recordcontract.RuntimeProvenance{ToolID: "caller-tool", RuntimeID: "caller-runtime", Adapter: "caller-adapter"},
	}
	callerBefore := cloneProvenance(caller)

	got, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
	if err != nil {
		t.Fatalf("MapMetadata returned error: %v", err)
	}

	if got.Provenance.Plan.PlanHash != digestD() {
		t.Fatalf("caller plan hash not preserved: %#v", got.Provenance.Plan)
	}
	if got.Provenance.Runtime != caller.Runtime {
		t.Fatalf("caller runtime not preserved: %#v", got.Provenance.Runtime)
	}
	requireInput(t, got.Provenance.Inputs, "caller", "input")
	requireInput(t, got.Provenance.Inputs, "dsl", "dsl")
	if countInput(got.Provenance.Inputs, "model", "housing") != 1 {
		t.Fatalf("duplicate model input not deduplicated: %#v", got.Provenance.Inputs)
	}
	if !reflect.DeepEqual(caller, callerBefore) {
		t.Fatalf("caller provenance was mutated:\nbefore: %#v\nafter:  %#v", callerBefore, caller)
	}

	t.Run("caller metadata input conflict fails", func(t *testing.T) {
		md := validMetadata()
		md.DSLHash = digestA()
		caller := recordcontract.Provenance{
			Inputs: []recordcontract.ProvenanceInput{{Kind: "dsl", Identity: "dsl", DigestSHA256: digestB()}},
		}

		_, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
		assertInvalidMetadataMapping(t, err)
	})
}

func TestMapMetadataInvalidCasesWrapSentinel(t *testing.T) {
	tests := []struct {
		name string
		in   recordmap.MetadataMappingInput
	}{
		{
			name: "invalid schema",
			in: recordmap.MetadataMappingInput{
				Metadata: metadata.Metadata{SchemaVersion: "2.0"},
			},
		},
		{
			name: "invalid digest",
			in: recordmap.MetadataMappingInput{
				Metadata: metadataWithTables(metadata.TableInputMetadata{LogicalID: "bad", Fingerprint: "bad"}),
			},
		},
		{
			name: "table conflict",
			in: recordmap.MetadataMappingInput{
				Metadata: metadataWithTables(
					metadata.TableInputMetadata{LogicalID: "t", Fingerprint: digestA()},
					metadata.TableInputMetadata{LogicalID: "t", Fingerprint: digestB()},
				),
			},
		},
		{
			name: "caller metadata input conflict",
			in: recordmap.MetadataMappingInput{
				Metadata: func() metadata.Metadata {
					md := validMetadata()
					md.DSLHash = digestA()
					return md
				}(),
				Provenance: recordcontract.Provenance{
					Inputs: []recordcontract.ProvenanceInput{{Kind: "dsl", Identity: "dsl", DigestSHA256: digestB()}},
				},
			},
		},
		{
			name: "final provenance validation failure",
			in: recordmap.MetadataMappingInput{
				Metadata: validMetadata(),
				Provenance: recordcontract.Provenance{
					SourceRevision: recordcontract.SourceRevisionProvenance{DigestSHA256: digestA()},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := recordmap.MapMetadata(tt.in)
			assertInvalidMetadataMapping(t, err)
		})
	}
}

func TestMapMetadataWrapperFunctions(t *testing.T) {
	md := validMetadata()
	md.PlanHash = digestA()
	md.ProjectInputs = &projectinput.CapturedResources{
		Models: []projectinput.CapturedResource{{LogicalID: "housing", Signature: digestB()}},
	}
	caller := recordcontract.Provenance{
		Inputs: []recordcontract.ProvenanceInput{{Kind: "caller", Identity: "input", DigestSHA256: digestC()}},
	}

	full, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
	if err != nil {
		t.Fatalf("MapMetadata returned error: %v", err)
	}
	provenance, err := recordmap.MapMetadataToProvenance(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
	if err != nil {
		t.Fatalf("MapMetadataToProvenance returned error: %v", err)
	}
	if !reflect.DeepEqual(provenance, full.Provenance) {
		t.Fatalf("MapMetadataToProvenance = %#v, want %#v", provenance, full.Provenance)
	}

	metadataOnly, err := recordmap.MapMetadataToInputIdentitySurfaces(md)
	if err != nil {
		t.Fatalf("MapMetadataToInputIdentitySurfaces returned error: %v", err)
	}
	requireInput(t, metadataOnly, "model", "housing")
	if hasInput(metadataOnly, "caller", "input") {
		t.Fatalf("metadata-only wrapper returned caller input: %#v", metadataOnly)
	}

	invalid := md
	invalid.ProjectInputs = &projectinput.CapturedResources{
		Models: []projectinput.CapturedResource{{LogicalID: "bad", Signature: "bad"}},
	}
	_, err = recordmap.MapMetadataToInputIdentitySurfaces(invalid)
	assertInvalidMetadataMapping(t, err)
}

func TestMapMetadataDeterminismAndCopySafety(t *testing.T) {
	md := validMetadata()
	md.DSLHash = digestA()
	md.ProjectInputs = &projectinput.CapturedResources{
		Models: []projectinput.CapturedResource{{LogicalID: "housing", Signature: digestB()}},
		Tables: []projectinput.CapturedResource{{LogicalID: "fasteners", Signature: digestC()}},
	}
	md.Profile = metadata.ProfileMetadata{
		Name:             "profile-a",
		ResolvedSettings: map[string]any{"material": "steel", "revision": float64(1)},
	}
	caller := recordcontract.Provenance{
		Inputs:   []recordcontract.ProvenanceInput{{Kind: "caller", Identity: "input", DigestSHA256: digestD()}},
		Evidence: []recordcontract.EvidenceReference{{Kind: "report", Ref: "raw/report.json", DigestSHA256: digestA()}},
	}
	mdBefore := cloneMetadata(md)
	callerBefore := cloneProvenance(caller)

	first, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
	if err != nil {
		t.Fatalf("MapMetadata returned error: %v", err)
	}
	second, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
	if err != nil {
		t.Fatalf("MapMetadata repeated returned error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("MapMetadata repeated output differs:\nfirst:  %#v\nsecond: %#v", first, second)
	}

	first.Inputs[0] = recordcontract.ProvenanceInput{Kind: "mutated", Identity: "mutated"}
	first.Provenance.Evidence[0] = recordcontract.EvidenceReference{Kind: "mutated", Ref: "mutated"}
	third, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{Metadata: md, Provenance: caller})
	if err != nil {
		t.Fatalf("MapMetadata after returned mutation returned error: %v", err)
	}
	if reflect.DeepEqual(first, third) {
		t.Fatalf("mutating returned output affected later mapper output: %#v", third)
	}
	if hasInput(third.Inputs, "mutated", "mutated") {
		t.Fatalf("later output contains mutated returned input: %#v", third.Inputs)
	}
	if hasEvidence(third.Provenance.Evidence, "mutated", "mutated", "") {
		t.Fatalf("later output contains mutated returned evidence: %#v", third.Provenance.Evidence)
	}
	if !reflect.DeepEqual(md, mdBefore) {
		t.Fatalf("metadata input was mutated:\nbefore: %#v\nafter:  %#v", mdBefore, md)
	}
	if !reflect.DeepEqual(caller, callerBefore) {
		t.Fatalf("caller provenance was mutated:\nbefore: %#v\nafter:  %#v", callerBefore, caller)
	}
}

func validMetadata() metadata.Metadata {
	return metadata.Metadata{SchemaVersion: "1.0"}
}

func metadataWithProjectTables(tables ...projectinput.CapturedResource) metadata.Metadata {
	md := validMetadata()
	md.ProjectInputs = &projectinput.CapturedResources{Tables: tables}
	return md
}

func metadataWithTables(tables ...metadata.TableInputMetadata) metadata.Metadata {
	md := validMetadata()
	md.Tables = tables
	return md
}

func digestA() string { return strings.Repeat("a", 64) }
func digestB() string { return strings.Repeat("b", 64) }
func digestC() string { return strings.Repeat("c", 64) }
func digestD() string { return strings.Repeat("d", 64) }

func assertInvalidMetadataMapping(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, recordmap.ErrInvalidMetadataMapping) {
		t.Fatalf("error = %v, want ErrInvalidMetadataMapping", err)
	}
}

func requireInput(t *testing.T, inputs []recordcontract.ProvenanceInput, kind, identity string) recordcontract.ProvenanceInput {
	t.Helper()
	for _, input := range inputs {
		if input.Kind == kind && input.Identity == identity {
			return input
		}
	}
	t.Fatalf("input %s/%s not found in %#v", kind, identity, inputs)
	return recordcontract.ProvenanceInput{}
}

func hasInput(inputs []recordcontract.ProvenanceInput, kind, identity string) bool {
	for _, input := range inputs {
		if input.Kind == kind && input.Identity == identity {
			return true
		}
	}
	return false
}

func hasInputKind(inputs []recordcontract.ProvenanceInput, kind string) bool {
	for _, input := range inputs {
		if input.Kind == kind {
			return true
		}
	}
	return false
}

func countInput(inputs []recordcontract.ProvenanceInput, kind, identity string) int {
	count := 0
	for _, input := range inputs {
		if input.Kind == kind && input.Identity == identity {
			count++
		}
	}
	return count
}

func findEvidence(evidence []recordcontract.EvidenceReference, kind, ref string) *recordcontract.EvidenceReference {
	for i := range evidence {
		if evidence[i].Kind == kind && evidence[i].Ref == ref {
			return &evidence[i]
		}
	}
	return nil
}

func hasEvidence(evidence []recordcontract.EvidenceReference, kind, ref, digest string) bool {
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref && item.DigestSHA256 == digest {
			return true
		}
	}
	return false
}

func countEvidence(evidence []recordcontract.EvidenceReference, kind, ref string) int {
	count := 0
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref {
			count++
		}
	}
	return count
}

func cloneProvenance(p recordcontract.Provenance) recordcontract.Provenance {
	out := p
	out.Inputs = append([]recordcontract.ProvenanceInput(nil), p.Inputs...)
	out.Evidence = append([]recordcontract.EvidenceReference(nil), p.Evidence...)
	return out
}

func cloneMetadata(md metadata.Metadata) metadata.Metadata {
	out := md
	out.Tables = append([]metadata.TableInputMetadata(nil), md.Tables...)
	if md.ProjectInputs != nil {
		out.ProjectInputs = projectinput.CloneCapturedResources(md.ProjectInputs)
	}
	if md.Profile.ResolvedSettings != nil {
		out.Profile.ResolvedSettings = make(map[string]any, len(md.Profile.ResolvedSettings))
		for k, v := range md.Profile.ResolvedSettings {
			out.Profile.ResolvedSettings[k] = v
		}
	}
	return out
}
