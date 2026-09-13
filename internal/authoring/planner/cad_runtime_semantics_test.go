package planner

import (
	"reflect"
	"testing"
)

var _ StepPayload = RunCADRuntimePayload{}

func cadRuntimeTestPlan(payload RunCADRuntimePayload) *ExecutionPlan {
	return &ExecutionPlan{Steps: []Step{
		{Type: StepWriteCSV, Payload: WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv"}},
		{Type: StepWriteExportManifest, Payload: WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: ExportManifestFilename}},
		{Type: StepRunCADRuntime, Payload: payload},
	}}
}

func testCADRuntimePayload() RunCADRuntimePayload {
	return RunCADRuntimePayload{ProductKey: "widget", Adapter: "freecad", ManifestFilename: ExportManifestFilename, ResultFilename: "result.json"}
}

func TestRunCADRuntimePayload_ImplementsStepPayload(t *testing.T) {
	if StepRunCADRuntime != "RunCADRuntime" {
		t.Fatalf("unexpected step type %q", StepRunCADRuntime)
	}
	want := testCADRuntimePayload()
	var payload StepPayload = want
	if got, ok := payload.(RunCADRuntimePayload); !ok || got != want {
		t.Fatalf("payload changed: %#v", payload)
	}
}

func TestCreatePlan_EmitsRunCADRuntimeAfterCutover(t *testing.T) {
	for _, tc := range []struct{ name, dsl string }{
		{"normal", `product Widget { adapter = "freecad" source_model = "widget.FCStd" outputs = ["step"] param label: string = "demo" }`},
		{"file_pattern", `profile Prod { file_pattern = "{product}_{param:label}" } use profile Prod product Widget { adapter = "freecad" source_model = "widget.FCStd" outputs = ["step"] param label: string = "demo" }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, plan := parseValidateAndPlan(t, tc.dsl)
			want := []StepType{StepWriteCSV, StepWriteExportManifest, StepRunCADRuntime}
			if len(plan.Steps) != len(want) {
				t.Fatalf("steps=%v", plan.Steps)
			}
			for i, typ := range want {
				if plan.Steps[i].Type != typ {
					t.Fatalf("step %d=%q want %q", i, plan.Steps[i].Type, typ)
				}
			}
			runtime, ok := plan.Steps[2].Payload.(RunCADRuntimePayload)
			if !ok || runtime.ProductKey != "Widget" || runtime.Adapter != "freecad" || runtime.ManifestFilename != ExportManifestFilename || runtime.ResultFilename != FreeCADRuntimeResultFilename {
				t.Fatalf("aligned runtime changed: %#v", plan.Steps[2].Payload)
			}
		})
	}
}

func TestComputePlanHash_RunCADRuntimeDeterministic(t *testing.T) {
	plan := cadRuntimeTestPlan(testCADRuntimePayload())
	before := *plan
	before.Steps = append([]Step(nil), plan.Steps...)
	first, err := ComputePlanHash(plan, nil)
	if err != nil || first == "" {
		t.Fatalf("hash=%q err=%v", first, err)
	}
	for i := 0; i < 10; i++ {
		got, err := ComputePlanHash(cadRuntimeTestPlan(testCADRuntimePayload()), nil)
		if err != nil || got != first {
			t.Fatalf("iteration %d hash=%q err=%v", i, got, err)
		}
	}
	if !reflect.DeepEqual(plan, &before) {
		t.Fatal("hashing mutated plan")
	}
}

func TestComputePlanHash_RunCADRuntimePayloadFieldsAffectHash(t *testing.T) {
	base := testCADRuntimePayload()
	baseHash, _ := ComputePlanHash(cadRuntimeTestPlan(base), nil)
	tests := map[string]RunCADRuntimePayload{}
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
	p.ResultFilename = "other-result.json"
	tests["ResultFilename"] = p
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ComputePlanHash(cadRuntimeTestPlan(payload), nil)
			if err != nil || got == baseHash {
				t.Fatalf("hash=%q err=%v", got, err)
			}
		})
	}
}

func TestComputePlanHash_RunCADRuntimeStepOrderAndPresenceAffectHash(t *testing.T) {
	base := cadRuntimeTestPlan(testCADRuntimePayload())
	baseHash, _ := ComputePlanHash(base, nil)
	variants := []*ExecutionPlan{
		{Steps: []Step{base.Steps[0], base.Steps[2], base.Steps[1]}},
		{Steps: append([]Step(nil), base.Steps[:2]...)},
		{Steps: append(append([]Step(nil), base.Steps...), Step{Type: StepWriteCSV, Payload: WriteCSVPayload{ProductKey: "widget", Filename: "extra.csv"}})},
	}
	for i, v := range variants {
		got, err := ComputePlanHash(v, nil)
		if err != nil || got == baseHash {
			t.Fatalf("variant %d hash=%q err=%v", i, got, err)
		}
	}
}

func TestComputePlanHash_PlanRemainsDeterministicWithoutRunCADRuntime(t *testing.T) {
	plan := &ExecutionPlan{Steps: []Step{{Type: StepWriteCSV, Payload: WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv"}}, {Type: StepWriteExportManifest, Payload: WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: ExportManifestFilename}}}}
	before := append([]Step(nil), plan.Steps...)
	first, _ := ComputePlanHash(plan, nil)
	second, _ := ComputePlanHash(plan, nil)
	if first == "" || first != second || !reflect.DeepEqual(before, plan.Steps) {
		t.Fatal("hash is not stable or mutated input")
	}
	for _, s := range plan.Steps {
		if s.Type == StepRunCADRuntime {
			t.Fatal("plan contains CAD runtime")
		}
	}
}

func TestDeriveProductIDFromStep_RunCADRuntime(t *testing.T) {
	if got := deriveProductIDFromStep(Step{Type: StepRunCADRuntime, Payload: testCADRuntimePayload()}); got != "widget" {
		t.Fatalf("got %q", got)
	}
	p := testCADRuntimePayload()
	p.ProductKey = ""
	p.ManifestFilename = "other-product.json"
	p.ResultFilename = "widget-result.json"
	if got := deriveProductIDFromStep(Step{Type: StepRunCADRuntime, Payload: p}); got != "" {
		t.Fatalf("filename fallback: %q", got)
	}
}

func TestComputeStepCacheKeyContext_RunCADRuntime(t *testing.T) {
	plan := cadRuntimeTestPlan(testCADRuntimePayload())
	a, err := ComputeStepCacheKeyContext(plan, nil, 2)
	b, _ := ComputeStepCacheKeyContext(plan, nil, 2)
	if err != nil || a.ProductID != "widget" || a.StepID != "2" || a.PlanHash == "" || !reflect.DeepEqual(a, b) {
		t.Fatalf("context=%#v err=%v", a, err)
	}
	p := testCADRuntimePayload()
	p.ProductKey = ""
	p.ManifestFilename = "other.json"
	p.ResultFilename = "widget.json"
	plan = cadRuntimeTestPlan(p)
	ctx, err := ComputeStepCacheKeyContext(plan, nil, 2)
	if err != nil || ctx.ProductID != "" {
		t.Fatalf("context=%#v err=%v", ctx, err)
	}
}
