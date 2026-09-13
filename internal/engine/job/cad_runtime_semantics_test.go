package job

import (
	"errors"
	"reflect"
	"testing"

	"parametron/internal/authoring/planner"
)

func runtimePayload(product string) planner.RunCADRuntimePayload {
	return planner.RunCADRuntimePayload{ProductKey: product, Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json"}
}
func runtimeStep(product string) planner.Step {
	return planner.Step{Type: planner.StepRunCADRuntime, Payload: runtimePayload(product)}
}
func csv(product string) planner.Step {
	return planner.Step{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: product, Filename: product + ".csv"}}
}
func manifest(product string) planner.Step {
	return planner.Step{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: product, ManifestFilename: planner.ExportManifestFilename, Product: planner.ExportManifestProduct{ID: product}}}
}

func TestProductKeyFromStep_RunCADRuntime(t *testing.T) {
	if got := ProductKeyFromStep(runtimeStep("widget")); got != "widget" {
		t.Fatal(got)
	}
	p := runtimePayload("")
	p.ManifestFilename = "widget.json"
	p.ResultFilename = "widget-result.json"
	if got := ProductKeyFromStep(planner.Step{Type: planner.StepRunCADRuntime, Payload: p}); got != "" {
		t.Fatalf("fallback %q", got)
	}
}
func TestNew_RunCADRuntimeAllowsExplicitJobProductKey(t *testing.T) {
	j, err := New("widget", []planner.Step{runtimeStep("")})
	if err != nil || j.ProductKey != "widget" || j.ID == "" {
		t.Fatalf("job=%#v err=%v", j, err)
	}
}
func TestNew_RunCADRuntimeRejectsMissingProductIdentity(t *testing.T) {
	_, err := New("", []planner.Step{runtimeStep("")})
	if !errors.Is(err, ErrMissingProductKey) {
		t.Fatal(err)
	}
}
func TestNew_RunCADRuntimeRejectsMixedProductKeys(t *testing.T) {
	_, err := New("widget", []planner.Step{runtimeStep("bracket")})
	if !errors.Is(err, ErrMixedProductKeys) {
		t.Fatal(err)
	}
}

func TestSplitPlan_GroupsRunCADRuntimeWithMatchingProduct(t *testing.T) {
	jobs, err := SplitPlan(&planner.ExecutionPlan{Steps: []planner.Step{csv("widget"), manifest("widget"), runtimeStep("widget"), csv("bracket"), manifest("bracket"), runtimeStep("bracket")}})
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs=%#v err=%v", jobs, err)
	}
	for i, want := range []string{"widget", "bracket"} {
		if jobs[i].ProductKey != want || jobs[i].ID == "" || len(jobs[i].Steps()) != 3 {
			t.Fatalf("job %d=%#v", i, jobs[i])
		}
		got := jobs[i].Steps()
		if got[0].Type != planner.StepWriteCSV || got[1].Type != planner.StepWriteExportManifest || got[2].Type != planner.StepRunCADRuntime {
			t.Fatal(got)
		}
	}
}
func TestSplitPlan_RunCADRuntimeWithoutProductKeyUsesStandaloneFallback(t *testing.T) {
	jobs, err := SplitPlan(&planner.ExecutionPlan{Steps: []planner.Step{runtimeStep(""), runtimeStep("")}})
	if err != nil || len(jobs) != 2 || jobs[0].ProductKey != "product_0" || jobs[1].ProductKey != "product_1" {
		t.Fatalf("jobs=%#v err=%v", jobs, err)
	}
}
func TestNew_RunCADRuntimeIdentityIsDeterministic(t *testing.T) {
	steps := []planner.Step{csv("widget"), manifest("widget"), runtimeStep("widget")}
	a, _ := New("widget", steps)
	b, _ := New("widget", steps)
	if a.ID == "" || a.ID != b.ID || !reflect.DeepEqual(a.Steps(), steps) {
		t.Fatal("non-deterministic job")
	}
}

func TestNew_RunCADRuntimePayloadFieldsAffectJobID(t *testing.T) {
	base := runtimePayload("widget")
	baseline, _ := New("widget", []planner.Step{{Type: planner.StepRunCADRuntime, Payload: base}})
	tests := map[string]planner.RunCADRuntimePayload{}
	p := base
	p.ProductKey = "bracket"
	tests["ProductKey"] = p
	p = base
	p.Adapter = "solidworks"
	tests["Adapter"] = p
	p = base
	p.ManifestFilename = "other.json"
	tests["ManifestFilename"] = p
	p = base
	p.ResultFilename = "other.json"
	tests["ResultFilename"] = p
	for n, p := range tests {
		t.Run(n, func(t *testing.T) {
			key := "widget"
			if p.ProductKey != "widget" {
				key = p.ProductKey
			}
			j, err := New(key, []planner.Step{{Type: planner.StepRunCADRuntime, Payload: p}})
			if err != nil || j.ID == baseline.ID {
				t.Fatalf("job=%#v err=%v", j, err)
			}
		})
	}
}
func TestNew_RunCADRuntimeOrderAndPresenceAffectJobID(t *testing.T) {
	run := runtimeStep("widget")
	c := csv("widget")
	variants := [][]planner.Step{{c, run}, {run, c}, {c}, {c, run, run}, {c, manifest("widget")}}
	ids := map[string]bool{}
	for _, steps := range variants {
		j, err := New("widget", steps)
		if err != nil {
			t.Fatal(err)
		}
		if ids[j.ID] {
			t.Fatalf("duplicate ID for %#v", steps)
		}
		ids[j.ID] = true
	}
}
func TestCanonicalPayload_AcceptsRunCADRuntime(t *testing.T) {
	want := runtimePayload("widget")
	got, err := canonicalPayload(want)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
