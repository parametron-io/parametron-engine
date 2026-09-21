package recordmap_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"parametron/internal/engine/executor"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
)

// These tests exercise MapCADRuntimeFailure with generic Engine failure
// semantics only. The vocabulary below is deliberately not FreeCAD's: the
// mapper must serve any CAD adapter once its native failure has been
// translated into executor.CADRuntimeFailureOutcome.

const cadRuntimeResultEvidenceRef = "raw/runtime/prm.result.json"

func genericCADFailure() executor.CADRuntimeFailureOutcome {
	return executor.CADRuntimeFailureOutcome{
		RuntimeNative: true,
		Class:         "adapter",
		SemanticStage: "adapter",
		// Native details a future adapter might report; the mapper must not
		// surface them in normalized fields.
		Boundary: "solidworks",
		Category: "com_bridge",
		Stage:    "open_document",
		Code:     "sw_open_failed",
		Message:  "document could not be opened",
	}
}

func genericCADFailureInput() recordmap.CADRuntimeFailureMappingInput {
	return recordmap.CADRuntimeFailureMappingInput{
		Failure:              genericCADFailure(),
		RecordKey:            "engine-run:plan-1",
		Linkage:              recordcontract.FailureLinkage{JobID: "job-1", ProductKey: "widget", StepRef: "2"},
		EvidenceDigestSHA256: digestA(),
	}
}

func runtimeResultEvidenceDigest(refs []recordcontract.EvidenceReference, kind, ref string) (string, bool) {
	for _, item := range refs {
		if item.Kind == kind && item.Ref == ref {
			return item.DigestSHA256, true
		}
	}
	return "", false
}

func TestMapCADRuntimeFailureMapsGenericSemantics(t *testing.T) {
	record, err := recordmap.MapCADRuntimeFailure(genericCADFailureInput())
	if err != nil {
		t.Fatalf("MapCADRuntimeFailure: %v", err)
	}
	if err := recordcontract.ValidateFailureRecord(record); err != nil {
		t.Fatalf("mapped record invalid: %v", err)
	}
	if record.Family != recordcontract.FamilyFailure || record.Version != recordcontract.CurrentVersion {
		t.Fatalf("family/version = %q/%q", record.Family, record.Version)
	}
	if record.RecordKey != "engine-run:plan-1:failure" {
		t.Fatalf("RecordKey = %q", record.RecordKey)
	}
	f := record.Failure
	if f.Class != recordcontract.FailureClassAdapter || f.Stage != recordcontract.FailureStageAdapter ||
		f.Severity != recordcontract.FailureSeverityError {
		t.Fatalf("class/stage/severity = %q/%q/%q", f.Class, f.Stage, f.Severity)
	}
	if f.Code != "sw_open_failed" || f.Message != "document could not be opened" {
		t.Fatalf("code/message = %q/%q", f.Code, f.Message)
	}
	if want := (recordcontract.FailureLinkage{JobID: "job-1", ProductKey: "widget", StepRef: "2"}); f.Linkage != want {
		t.Fatalf("linkage = %+v, want %+v", f.Linkage, want)
	}
	wantEvidence := []recordcontract.FailureEvidence{{SourceKind: "runtime-result", SourceRef: cadRuntimeResultEvidenceRef, DigestSHA256: digestA()}}
	if !reflect.DeepEqual(f.Evidence, wantEvidence) {
		t.Fatalf("failure evidence = %#v, want %#v", f.Evidence, wantEvidence)
	}
	if got, ok := runtimeResultEvidenceDigest(record.Provenance.Evidence, "runtime-result", cadRuntimeResultEvidenceRef); !ok || got != digestA() {
		t.Fatalf("provenance evidence = %#v", record.Provenance.Evidence)
	}
	// Native boundary/category/stage stay in the raw runtime result; they are
	// not stuffed into normalized fields.
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, native := range []string{"solidworks", "com_bridge", "open_document"} {
		if bytes.Contains(encoded, []byte(native)) {
			t.Fatalf("normalized record leaks native detail %q: %s", native, encoded)
		}
	}
}

func TestMapCADRuntimeFailureMapsEachSemanticClassAndStage(t *testing.T) {
	for _, tc := range []struct {
		class, stage string
		wantClass    recordcontract.FailureClass
		wantStage    recordcontract.FailureStage
	}{
		{"validation", "validation", recordcontract.FailureClassValidation, recordcontract.FailureStageValidation},
		{"adapter", "adapter", recordcontract.FailureClassAdapter, recordcontract.FailureStageAdapter},
		{"export", "export", recordcontract.FailureClassExport, recordcontract.FailureStageExport},
		{"runtime", "runtime", recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime},
	} {
		t.Run(tc.class, func(t *testing.T) {
			in := genericCADFailureInput()
			in.Failure.Class, in.Failure.SemanticStage = " "+tc.class+" ", " "+tc.stage+" "
			record, err := recordmap.MapCADRuntimeFailure(in)
			if err != nil {
				t.Fatal(err)
			}
			if record.Failure.Class != tc.wantClass || record.Failure.Stage != tc.wantStage {
				t.Fatalf("class/stage = %q/%q", record.Failure.Class, record.Failure.Stage)
			}
		})
	}
}

func TestMapCADRuntimeFailureIsDeterministicAndDoesNotMutateInput(t *testing.T) {
	in := genericCADFailureInput()
	in.Provenance = recordcontract.Provenance{
		Inputs:   []recordcontract.ProvenanceInput{{Kind: "dsl", Identity: "model.pmtn", DigestSHA256: digestB()}},
		Evidence: []recordcontract.EvidenceReference{{Kind: "report", Ref: "raw/prm.report.json"}},
	}
	before := in
	before.Provenance = recordcontract.Provenance{
		Inputs:   append([]recordcontract.ProvenanceInput(nil), in.Provenance.Inputs...),
		Evidence: append([]recordcontract.EvidenceReference(nil), in.Provenance.Evidence...),
	}

	first, err := recordmap.MapCADRuntimeFailure(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := recordmap.MapCADRuntimeFailure(in)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if !bytes.Equal(a, b) || first.Identity.ID == "" || first.Identity.ID != second.Identity.ID {
		t.Fatalf("mapping is not deterministic:\n%s\n%s", a, b)
	}
	if !reflect.DeepEqual(in, before) {
		t.Fatalf("caller input mutated: %#v, want %#v", in, before)
	}
	// The report evidence the caller supplied is kept next to the runtime result.
	if _, ok := runtimeResultEvidenceDigest(first.Provenance.Evidence, "report", "raw/prm.report.json"); !ok {
		t.Fatalf("provenance dropped caller evidence: %#v", first.Provenance.Evidence)
	}

	// Identity follows content: a different digest, code or linkage is a
	// different record.
	for name, mutate := range map[string]func(*recordmap.CADRuntimeFailureMappingInput){
		"digest":  func(in *recordmap.CADRuntimeFailureMappingInput) { in.EvidenceDigestSHA256 = digestB() },
		"code":    func(in *recordmap.CADRuntimeFailureMappingInput) { in.Failure.Code = "other" },
		"linkage": func(in *recordmap.CADRuntimeFailureMappingInput) { in.Linkage.JobID = "job-2" },
	} {
		other := genericCADFailureInput()
		mutate(&other)
		got, err := recordmap.MapCADRuntimeFailure(other)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Identity.ID == first.Identity.ID {
			t.Fatalf("%s change did not change the record identity", name)
		}
	}
}

func TestMapCADRuntimeFailureMergesRuntimeResultProvenanceDigest(t *testing.T) {
	in := genericCADFailureInput()
	in.Provenance = recordcontract.Provenance{Evidence: []recordcontract.EvidenceReference{
		{Kind: "runtime-result", Ref: cadRuntimeResultEvidenceRef},
	}}
	record, err := recordmap.MapCADRuntimeFailure(in)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := runtimeResultEvidenceDigest(record.Provenance.Evidence, "runtime-result", cadRuntimeResultEvidenceRef); !ok || got != digestA() {
		t.Fatalf("existing runtime-result reference not enriched: %#v", record.Provenance.Evidence)
	}
	if len(record.Provenance.Evidence) != 1 {
		t.Fatalf("runtime-result evidence duplicated: %#v", record.Provenance.Evidence)
	}

	in.Provenance.Evidence[0].DigestSHA256 = digestB()
	if _, err := recordmap.MapCADRuntimeFailure(in); !errors.Is(err, recordmap.ErrInvalidCADRuntimeFailureMapping) {
		t.Fatalf("conflicting digest error = %v, want ErrInvalidCADRuntimeFailureMapping", err)
	}
}

func TestMapCADRuntimeFailureFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*recordmap.CADRuntimeFailureMappingInput)
	}{
		{"not runtime native", func(in *recordmap.CADRuntimeFailureMappingInput) { in.Failure.RuntimeNative = false }},
		{"missing class", func(in *recordmap.CADRuntimeFailureMappingInput) { in.Failure.Class = " " }},
		{"missing semantic stage", func(in *recordmap.CADRuntimeFailureMappingInput) { in.Failure.SemanticStage = "" }},
		{"missing code", func(in *recordmap.CADRuntimeFailureMappingInput) { in.Failure.Code = "" }},
		{"missing message", func(in *recordmap.CADRuntimeFailureMappingInput) { in.Failure.Message = "  " }},
		{"missing record key", func(in *recordmap.CADRuntimeFailureMappingInput) { in.RecordKey = " " }},
		{"unknown class", func(in *recordmap.CADRuntimeFailureMappingInput) { in.Failure.Class = "solidworks" }},
		{"unknown stage", func(in *recordmap.CADRuntimeFailureMappingInput) { in.Failure.SemanticStage = "com_bridge" }},
		{"short digest", func(in *recordmap.CADRuntimeFailureMappingInput) { in.EvidenceDigestSHA256 = "abc" }},
		{"uppercase digest", func(in *recordmap.CADRuntimeFailureMappingInput) {
			in.EvidenceDigestSHA256 = strings.ToUpper(digestA())
		}},
		{"non-hex digest", func(in *recordmap.CADRuntimeFailureMappingInput) {
			in.EvidenceDigestSHA256 = strings.Repeat("z", 64)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := genericCADFailureInput()
			tc.mutate(&in)
			record, err := recordmap.MapCADRuntimeFailure(in)
			if !errors.Is(err, recordmap.ErrInvalidCADRuntimeFailureMapping) {
				t.Fatalf("error = %v, want ErrInvalidCADRuntimeFailureMapping", err)
			}
			if !reflect.DeepEqual(record, recordcontract.FailureRecord{}) {
				t.Fatalf("record returned alongside error: %#v", record)
			}
		})
	}
}

func TestMapCADRuntimeFailureWithoutDigestOmitsDigestOnly(t *testing.T) {
	in := genericCADFailureInput()
	in.EvidenceDigestSHA256 = ""
	record, err := recordmap.MapCADRuntimeFailure(in)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := runtimeResultEvidenceDigest(record.Provenance.Evidence, "runtime-result", cadRuntimeResultEvidenceRef); !ok || got != "" {
		t.Fatalf("provenance evidence = %#v", record.Provenance.Evidence)
	}
	if record.Failure.Evidence[0].DigestSHA256 != "" {
		t.Fatalf("failure evidence = %#v", record.Failure.Evidence)
	}
}

func TestMapCADRuntimeFailureEvidenceRefIsPackageRawRuntimeResultPath(t *testing.T) {
	if recordpackage.RawRuntimeResultContractPath() != cadRuntimeResultEvidenceRef {
		t.Fatalf("raw runtime result path = %q, want %q", recordpackage.RawRuntimeResultContractPath(), cadRuntimeResultEvidenceRef)
	}
}

// --- architectural guards ---

func productionGoFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse recordmap production sources: %v", err)
	}
	files := map[string]*ast.File{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			files[name] = file
		}
	}
	if len(files) == 0 {
		t.Fatal("no recordmap production sources found")
	}
	return files
}

// The mapper package must not import any adapter package: adapter-native
// results are translated by the runtime consumption boundary before they reach
// record mapping.
func TestRecordmapProductionImportsNoAdapterPackages(t *testing.T) {
	for name, file := range productionGoFiles(t) {
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(path, "/adapter") || strings.Contains(strings.ToLower(path), "freecad") || strings.HasSuffix(path, "/cadruntime") {
				t.Errorf("%s imports adapter-specific package %q", name, path)
			}
		}
	}
}

// The mapper's input carries only Engine-owned types.
func TestMapCADRuntimeFailureInputCarriesOnlyEngineOwnedTypes(t *testing.T) {
	typ := reflect.TypeOf(recordmap.CADRuntimeFailureMappingInput{})
	for i := 0; i < typ.NumField(); i++ {
		pkg := typ.Field(i).Type.PkgPath()
		if strings.Contains(pkg, "adapter") || strings.Contains(strings.ToLower(pkg), "freecad") {
			t.Errorf("field %s has adapter-owned type %s", typ.Field(i).Name, typ.Field(i).Type)
		}
	}
	outcome := reflect.TypeOf(executor.CADRuntimeFailureOutcome{})
	for i := 0; i < outcome.NumField(); i++ {
		if pkg := outcome.Field(i).Type.PkgPath(); pkg != "" {
			t.Errorf("outcome field %s has non-builtin type %s; the generic outcome must stay adapter-neutral", outcome.Field(i).Name, outcome.Field(i).Type)
		}
	}
}

// The legacy runtime-result mapper was removed; the CAD path is
// MapCADRuntimeFailure. Guard the declarations, not formatting.
func TestLegacyRuntimeResultMapperIsNotDeclaredInProduction(t *testing.T) {
	removed := map[string]bool{
		"MapRuntimeResult":                true,
		"MapRuntimeResultToFailureRecord": true,
		"RuntimeResult":                   true,
		"RuntimeResultError":              true,
		"RuntimeResultMappingInput":       true,
		"ErrInvalidRuntimeResultMapping":  true,
	}
	var found []string
	for name, file := range productionGoFiles(t) {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if removed[d.Name.Name] {
					found = append(found, name+": func "+d.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if removed[s.Name.Name] {
							found = append(found, name+": type "+s.Name.Name)
						}
					case *ast.ValueSpec:
						for _, id := range s.Names {
							if removed[id.Name] {
								found = append(found, name+": var/const "+id.Name)
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Fatalf("legacy runtime-result mapping declarations are back in production: %v", found)
	}
}

func TestMapCADRuntimeFailurePreservesEngineOperationalContext(t *testing.T) {
	// A non-UTC instant: the record carries it normalized to UTC.
	zone := time.FixedZone("UTC+3", 3*60*60)
	occurred := time.Date(2026, 6, 1, 11, 30, 15, 0, zone)
	in := genericCADFailureInput()
	in.RetryCount = 2
	in.OccurredAt = &occurred
	beforeOccurred := occurred

	record, err := recordmap.MapCADRuntimeFailure(in)
	if err != nil {
		t.Fatal(err)
	}
	f := record.Failure
	wantUTC := time.Date(2026, 6, 1, 8, 30, 15, 0, time.UTC)
	if f.RetryCount != 2 || f.OccurredAt == nil || !f.OccurredAt.Equal(wantUTC) || f.OccurredAt.Location() != time.UTC {
		t.Fatalf("retryCount/occurredAt = %d/%v, want 2/%v UTC", f.RetryCount, f.OccurredAt, wantUTC)
	}
	// Semantic fields are untouched by the operational context.
	if f.Class != recordcontract.FailureClassAdapter || f.Stage != recordcontract.FailureStageAdapter ||
		f.Code != "sw_open_failed" || f.Message != "document could not be opened" ||
		f.Linkage.JobID != "job-1" || f.Evidence[0].DigestSHA256 != digestA() {
		t.Fatalf("semantic fields changed: %#v", f)
	}
	if !occurred.Equal(beforeOccurred) || occurred.Location() != zone {
		t.Fatal("caller timestamp mutated")
	}

	again, err := recordmap.MapCADRuntimeFailure(in)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(record)
	b, _ := json.Marshal(again)
	if !bytes.Equal(a, b) {
		t.Fatalf("mapping with operational context is not deterministic:\n%s\n%s", a, b)
	}

	// Absent context stays absent (no invented time or retry count).
	bare, err := recordmap.MapCADRuntimeFailure(genericCADFailureInput())
	if err != nil || bare.Failure.RetryCount != 0 || bare.Failure.OccurredAt != nil {
		t.Fatalf("bare mapping = %#v, err=%v", bare.Failure, err)
	}
	// Negative retry counts fail closed.
	in.RetryCount = -1
	if _, err := recordmap.MapCADRuntimeFailure(in); !errors.Is(err, recordmap.ErrInvalidCADRuntimeFailureMapping) {
		t.Fatalf("negative retry count error = %v", err)
	}
}
