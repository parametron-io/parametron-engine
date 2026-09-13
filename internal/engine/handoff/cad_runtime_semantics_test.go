package handoff

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/job"
)

func cadRuntimeSteps(adapter, result string) []planner.Step {
	return []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv", Headers: []string{"width"}, Values: []interface{}{10.0}}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: planner.ExportManifestFilename, SchemaVersion: planner.ExportManifestSchemaVersion, Adapter: adapter, Product: planner.ExportManifestProduct{ID: "widget"}, Values: map[string]interface{}{"width": 10.0}, Outputs: []planner.ExportManifestOutput{{Type: "step", Filename: "widget.step", Object: "Body"}, {Type: "csv", Filename: "declared.csv"}}}},
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: adapter, ManifestFilename: planner.ExportManifestFilename, ResultFilename: result}},
	}
}
func cadRuntimeJob(t *testing.T) *job.Job {
	t.Helper()
	j, err := job.New("widget", cadRuntimeSteps("freecad", "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func cadRuntimePackage(t *testing.T) *Package {
	t.Helper()
	p, err := FromJob(cadRuntimeJob(t))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// cadRuntimeNativeOnlySteps builds a CADRuntime handoff step sequence with a
// zero-length manifest Outputs slice, mirroring authoring-only
// outputs=["none"] planning.
func cadRuntimeNativeOnlySteps(adapter, result string) []planner.Step {
	return []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv", Headers: []string{"width"}, Values: []interface{}{10.0}}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: planner.ExportManifestFilename, SchemaVersion: planner.ExportManifestSchemaVersion, Adapter: adapter, Product: planner.ExportManifestProduct{ID: "widget"}, Values: map[string]interface{}{"width": 10.0}, Outputs: []planner.ExportManifestOutput{}}},
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: adapter, ManifestFilename: planner.ExportManifestFilename, ResultFilename: result}},
	}
}

// TestFromJob_CADRuntimeZeroOutputsIsValid proves the native-only handoff
// scoping contract: Package.CADRuntime != nil paired with
// Manifest.Outputs == [] is a valid handoff package, ExpectedArtifacts
// still contains result.json (the CADRuntime result), no native CAD
// document is added, and derived-output artifacts are simply absent (not
// replaced by a synthetic entry).
func TestFromJob_CADRuntimeZeroOutputsIsValid(t *testing.T) {
	j, err := job.New("widget", cadRuntimeNativeOnlySteps("freecad", "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := FromJob(j)
	if err != nil {
		t.Fatalf("expected native-only CADRuntime handoff to be valid, got error: %v", err)
	}
	if p.CADRuntime == nil {
		t.Fatal("expected CADRuntime to be set")
	}
	if len(p.Manifest.Outputs) != 0 {
		t.Fatalf("expected zero manifest outputs, got %+v", p.Manifest.Outputs)
	}
	wantArtifacts := []string{"widget.csv", planner.ExportManifestFilename, "result.json"}
	if !reflect.DeepEqual(p.ExpectedArtifacts(), wantArtifacts) {
		t.Fatalf("ExpectedArtifacts=%v want %v", p.ExpectedArtifacts(), wantArtifacts)
	}
	for _, artifact := range p.ExpectedArtifacts() {
		if strings.Contains(artifact, ".FCStd") {
			t.Fatalf("expected no native CAD document artifact, found %q", artifact)
		}
	}
}

// TestFromJob_LegacyManifestOnlyZeroOutputsRemainsInvalid proves the
// contrasting scope boundary: a manifest-only package (no CADRuntime) with
// zero manifest outputs remains rejected, exactly as before native-only
// planning existed. Zero outputs are only legal when scoped to an aligned
// CADRuntime handoff.
func TestFromJob_LegacyManifestOnlyZeroOutputsRemainsInvalid(t *testing.T) {
	j, err := job.New("widget", []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv", Headers: []string{"width"}, Values: []interface{}{10.0}}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: planner.ExportManifestFilename, SchemaVersion: planner.ExportManifestSchemaVersion, Product: planner.ExportManifestProduct{ID: "widget"}, Outputs: []planner.ExportManifestOutput{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = FromJob(j)
	if !errors.Is(err, ErrManifestOutputsEmpty) {
		t.Fatalf("expected ErrManifestOutputsEmpty for legacy zero-output package, got %v", err)
	}
}

// TestFromJob_CADRuntimeZeroOutputsJSONRoundTrip proves the empty manifest
// outputs list and CADRuntime state survive a JSON round trip without
// inventing an output or losing the CADRuntime linkage.
func TestFromJob_CADRuntimeZeroOutputsJSONRoundTrip(t *testing.T) {
	p := &Package{}
	j, err := job.New("widget", cadRuntimeNativeOnlySteps("freecad", "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := FromJob(j)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"outputs":[]`)) {
		t.Fatalf("expected round-tripped JSON to preserve empty outputs array, got: %s", data)
	}
	if err := json.Unmarshal(data, p); err != nil {
		t.Fatal(err)
	}
	if p.CADRuntime == nil || *p.CADRuntime != *original.CADRuntime {
		t.Fatalf("CADRuntime lost/changed across round trip: got %#v want %#v", p.CADRuntime, original.CADRuntime)
	}
	if len(p.Manifest.Outputs) != 0 {
		t.Fatalf("expected zero outputs after round trip, got %+v", p.Manifest.Outputs)
	}
}

func TestFromJob_CADRuntimeSuccess(t *testing.T) {
	p := cadRuntimePackage(t)
	want := planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json"}
	if p.CADRuntime == nil || *p.CADRuntime != want || !p.RequiresCADRuntime() || p.RuntimeResultFilename() != "result.json" || p.JobID == "" || p.ProductKey != "widget" {
		t.Fatalf("package=%#v", p)
	}
	wantTypes := []planner.StepType{planner.StepWriteCSV, planner.StepWriteExportManifest, planner.StepRunCADRuntime}
	for i, s := range p.Steps {
		if s.Type != wantTypes[i] {
			t.Fatal(p.Steps)
		}
		count := 0
		for _, set := range []bool{s.CSV != nil, s.Manifest != nil, s.CADRuntime != nil} {
			if set {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("snapshot=%#v", s)
		}
	}
}
func TestPackage_CADRuntimeHelpers(t *testing.T) {
	var nilPkg *Package
	if nilPkg.RequiresCADRuntime() || nilPkg.RuntimeResultFilename() != "" {
		t.Fatal("nil helpers")
	}
	manifest := &Package{}
	if manifest.RequiresCADRuntime() || manifest.RuntimeResultFilename() != "" {
		t.Fatal("manifest helpers")
	}
	aligned := &Package{CADRuntime: &planner.RunCADRuntimePayload{ResultFilename: "result.json"}}
	if !aligned.RequiresCADRuntime() || aligned.RuntimeResultFilename() != "result.json" {
		t.Fatal("runtime helpers")
	}
}

func TestPackagePlan_ReconstructsRunCADRuntimeInOriginalOrder(t *testing.T) {
	p := cadRuntimePackage(t)
	want := &planner.ExecutionPlan{Steps: cadRuntimeSteps("freecad", "result.json")}
	a := p.Plan()
	b := p.Plan()
	if !reflect.DeepEqual(a, want) || !reflect.DeepEqual(a, b) {
		t.Fatalf("plan=%#v", a)
	}
	a.Steps[0].Payload = planner.WriteCSVPayload{Filename: "mutated.csv"}
	if !reflect.DeepEqual(b, p.Plan()) {
		t.Fatal("Plan aliases package")
	}
}
func TestPackageCADRuntime_JSONRoundTrip(t *testing.T) {
	p := cadRuntimePackage(t)
	first, err := json.Marshal(p)
	if err != nil || !bytes.Contains(first, []byte(`"cadRuntime"`)) || bytes.Contains(first, []byte(`"runner"`)) {
		t.Fatalf("json=%s err=%v", first, err)
	}
	var got Package
	if err := json.Unmarshal(first, &got); err != nil {
		t.Fatal(err)
	}
	if got.CADRuntime == nil || *got.CADRuntime != *p.CADRuntime || len(got.Steps) != len(p.Steps) || !got.RequiresCADRuntime() || got.RuntimeResultFilename() != "result.json" || !reflect.DeepEqual(p.ExpectedArtifacts(), got.ExpectedArtifacts()) || len(got.Plan().Steps) != 3 || got.Plan().Steps[2].Type != planner.StepRunCADRuntime {
		t.Fatalf("round trip=%#v", got)
	}
	second, _ := json.Marshal(&got)
	if !bytes.Equal(first, second) {
		t.Fatal("JSON not deterministic")
	}
}
func TestFromJob_CADRuntimeCopiesCallerOwnedState(t *testing.T) {
	steps := cadRuntimeSteps("freecad", "result.json")
	csv := steps[0].Payload.(planner.WriteCSVPayload)
	manifest := steps[1].Payload.(planner.WriteExportManifestPayload)
	j, err := job.New("widget", steps)
	if err != nil {
		t.Fatal(err)
	}
	p, err := FromJob(j)
	if err != nil {
		t.Fatal(err)
	}
	csv.Headers[0] = "changed"
	manifest.Values["width"] = 99
	manifest.Outputs[0].Filename = "changed.step"
	steps[2].Payload = planner.RunCADRuntimePayload{ResultFilename: "changed.json"}
	reconstructed := p.Plan()
	reconstructed.Steps[2].Payload = planner.RunCADRuntimePayload{ResultFilename: "also-changed.json"}
	if p.CSV.Headers[0] != "width" || p.Manifest.Values["width"] != 10.0 || p.Manifest.Outputs[0].Filename != "widget.step" || p.CADRuntime.ResultFilename != "result.json" || !reflect.DeepEqual(p.ExpectedArtifacts(), []string{"widget.csv", planner.ExportManifestFilename, "result.json", "widget.step", "declared.csv"}) {
		t.Fatalf("aliased package=%#v", p)
	}
}

func TestFromJob_RejectsDuplicateCADRuntime(t *testing.T) {
	steps := append(cadRuntimeSteps("freecad", "result.json"), cadRuntimeSteps("freecad", "other.json")[2])
	j, _ := job.New("widget", steps)
	_, err := FromJob(j)
	if !errors.Is(err, ErrDuplicateCADRuntime) {
		t.Fatal(err)
	}
}
func TestFromJob_RejectsCADRuntimeProductMismatch(t *testing.T) {
	steps := cadRuntimeSteps("freecad", "result.json")
	p := steps[2].Payload.(planner.RunCADRuntimePayload)
	p.ProductKey = "bracket"
	steps[2].Payload = p
	j, err := job.New("widget", steps)
	if !errors.Is(err, job.ErrMixedProductKeys) {
		t.Fatal("job should enforce mismatch")
	}
	j, _ = job.New("bracket", []planner.Step{steps[2]})
	_ = j // direct package validation covers the same exported sentinel
	pkg := cadRuntimePackage(t)
	pkg.CADRuntime.ProductKey = "bracket"
	if err := validatePackage(pkg); !errors.Is(err, ErrMismatchedProductKey) {
		t.Fatal(err)
	}
}

func TestFromJob_ValidatesCADRuntimeAdapter(t *testing.T) {
	for _, bad := range []string{"", " ", " freecad", "freecad "} {
		t.Run(bad, func(t *testing.T) {
			steps := cadRuntimeSteps("freecad", "result.json")
			p := steps[2].Payload.(planner.RunCADRuntimePayload)
			p.Adapter = bad
			steps[2].Payload = p
			j, _ := job.New("widget", steps)
			_, err := FromJob(j)
			if !errors.Is(err, ErrCADRuntimeAdapterEmpty) {
				t.Fatal(err)
			}
		})
	}
	steps := cadRuntimeSteps("freecad", "result.json")
	p := steps[2].Payload.(planner.RunCADRuntimePayload)
	p.Adapter = "solidworks"
	steps[2].Payload = p
	j, _ := job.New("widget", steps)
	_, err := FromJob(j)
	if !errors.Is(err, ErrCADRuntimeAdapterMismatch) {
		t.Fatal(err)
	}
	j, _ = job.New("widget", cadRuntimeSteps("solidworks", "result.json"))
	if _, err = FromJob(j); err != nil {
		t.Fatal(err)
	}
}
func TestFromJob_ValidatesCADRuntimeManifestFilename(t *testing.T) {
	tests := []struct {
		name string
		want error
	}{{"", ErrCADRuntimeManifestFilenameEmpty}, {" ", ErrCADRuntimeManifestFilenameEmpty}, {"other.json", ErrCADRuntimeManifestMismatch}, {"dir/manifest.json", ErrCADRuntimeManifestFilenameEmpty}, {"../manifest.json", ErrCADRuntimeManifestFilenameEmpty}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			steps := cadRuntimeSteps("freecad", "result.json")
			p := steps[2].Payload.(planner.RunCADRuntimePayload)
			p.ManifestFilename = tc.name
			steps[2].Payload = p
			j, _ := job.New("widget", steps)
			_, err := FromJob(j)
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
	if _, err := FromJob(cadRuntimeJob(t)); err != nil {
		t.Fatal(err)
	}
}
func TestFromJob_ValidatesCADRuntimeResultFilename(t *testing.T) {
	invalid := []string{"", " ", ".", "..", "../result.json", "dir/result.json", `dir\result.json`, "/result.json", "result.json\n", "result.json\t", "result\x00.json", " result.json", "result.json ", `C:\result.json`}
	for _, name := range invalid {
		t.Run(strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			j, _ := job.New("widget", cadRuntimeSteps("freecad", name))
			_, err := FromJob(j)
			want := ErrCADRuntimeResultFilenameInvalid
			if name == "" {
				want = ErrCADRuntimeResultFilenameEmpty
			}
			if !errors.Is(err, want) {
				t.Fatalf("name=%q err=%v", name, err)
			}
		})
	}
	for _, name := range []string{"result.json", "cad-result.json", "runtime_result.v1.json"} {
		j, _ := job.New("widget", cadRuntimeSteps("freecad", name))
		if _, err := FromJob(j); err != nil {
			t.Fatalf("name=%q err=%v", name, err)
		}
	}
}
func TestFromJob_RejectsCADRuntimeResultFilenameCollisions(t *testing.T) {
	for _, name := range []string{"widget.csv", planner.ExportManifestFilename, "widget.step", "declared.csv"} {
		j, _ := job.New("widget", cadRuntimeSteps("freecad", name))
		_, err := FromJob(j)
		if !errors.Is(err, ErrCADRuntimeFilenameConflict) {
			t.Fatalf("name=%q err=%v", name, err)
		}
	}
}
func TestPackageExpectedArtifacts_CADRuntimeOrdering(t *testing.T) {
	p := cadRuntimePackage(t)
	want := []string{"widget.csv", planner.ExportManifestFilename, "result.json", "widget.step", "declared.csv"}
	a := p.ExpectedArtifacts()
	b := p.ExpectedArtifacts()
	if !reflect.DeepEqual(a, want) || !reflect.DeepEqual(a, b) {
		t.Fatal(a)
	}
	a[0] = "changed"
	if !reflect.DeepEqual(p.ExpectedArtifacts(), want) {
		t.Fatal("artifact slice aliases")
	}
	legacy := p
	legacy.CADRuntime = nil
	if got := legacy.ExpectedArtifacts(); !reflect.DeepEqual(got, []string{"widget.csv", planner.ExportManifestFilename, "widget.step", "declared.csv"}) {
		t.Fatal(got)
	}
}
func TestValidateSnapshotShape_RunCADRuntime(t *testing.T) {
	payload := &planner.RunCADRuntimePayload{}
	csv := &planner.WriteCSVPayload{}
	manifest := &planner.WriteExportManifestPayload{}
	valid := StepSnapshot{Type: planner.StepRunCADRuntime, CADRuntime: payload}
	if err := validateSnapshotShape(valid); err != nil {
		t.Fatal(err)
	}
	invalid := []StepSnapshot{{Type: planner.StepRunCADRuntime}, {Type: planner.StepRunCADRuntime, CADRuntime: payload, CSV: csv}, {Type: planner.StepRunCADRuntime, CADRuntime: payload, Manifest: manifest}, {Type: planner.StepWriteCSV, CADRuntime: payload}, {Type: planner.StepRunCADRuntime, CSV: csv}}
	for _, s := range invalid {
		if err := validateSnapshotShape(s); !errors.Is(err, ErrUnsupportedStep) {
			t.Fatalf("snapshot=%#v err=%v", s, err)
		}
	}
}

func TestFromJob_ManifestOnlyBehaviorUnchanged(t *testing.T) {
	steps := cadRuntimeSteps("freecad", "result.json")[:2]
	j, _ := job.New("widget", steps)
	p, err := FromJob(j)
	if err != nil || p.CADRuntime != nil || !reflect.DeepEqual(p.Plan(), &planner.ExecutionPlan{Steps: steps}) || !reflect.DeepEqual(p.ExpectedArtifacts(), []string{"widget.csv", planner.ExportManifestFilename, "widget.step", "declared.csv"}) {
		t.Fatalf("pkg=%#v err=%v", p, err)
	}
}
func TestFromJob_CADRuntimeDeterministic(t *testing.T) {
	a := cadRuntimePackage(t)
	b := cadRuntimePackage(t)
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if a.JobID != b.JobID || !reflect.DeepEqual(a, b) || !bytes.Equal(aj, bj) || !reflect.DeepEqual(a.ExpectedArtifacts(), b.ExpectedArtifacts()) || !reflect.DeepEqual(a.Plan(), b.Plan()) {
		t.Fatal("handoff is not deterministic")
	}
}
