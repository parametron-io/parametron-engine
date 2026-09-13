package job

import (
	"errors"
	"reflect"
	"testing"

	"parametron/internal/authoring/planner"
)

func TestFromPlan_SingleProductPlanBuildsJob(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "widget",
					Filename:   "widget.csv",
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       "widget",
					ManifestFilename: planner.ExportManifestFilename,
					Product:          planner.ExportManifestProduct{ID: "widget"},
				},
			},
			{
				Type: planner.StepRunCADRuntime,
				Payload: planner.RunCADRuntimePayload{
					ProductKey:       "widget",
					Adapter:          "freecad",
					ManifestFilename: planner.ExportManifestFilename,
					ResultFilename:   planner.FreeCADRuntimeResultFilename,
				},
			},
		},
	}

	j, err := FromPlan(plan)
	if err != nil {
		t.Fatalf("FromPlan returned error: %v", err)
	}

	if j.ProductKey != "widget" {
		t.Fatalf("expected product key %q, got %q", "widget", j.ProductKey)
	}
	if j.ID == "" {
		t.Fatal("expected FromPlan to assign a non-empty job ID")
	}

	gotSteps := j.Steps()
	if len(gotSteps) != len(plan.Steps) {
		t.Fatalf("expected %d steps, got %d", len(plan.Steps), len(gotSteps))
	}

	for i := range plan.Steps {
		if gotSteps[i].Type != plan.Steps[i].Type {
			t.Fatalf("step order changed at index %d: got %q want %q", i, gotSteps[i].Type, plan.Steps[i].Type)
		}
	}
}

func TestNew_AssignsDeterministicID(t *testing.T) {
	steps := []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "widget.csv",
				Headers:    []string{"width"},
				Values:     []interface{}{42},
			},
		},
	}

	first, err := New("widget", steps)
	if err != nil {
		t.Fatalf("first New returned error: %v", err)
	}
	if first.ID == "" {
		t.Fatal("expected New to assign a non-empty job ID")
	}

	second, err := New("widget", steps)
	if err != nil {
		t.Fatalf("second New returned error: %v", err)
	}
	if second.ID == "" {
		t.Fatal("expected repeated New to assign a non-empty job ID")
	}
	if first.ID != second.ID {
		t.Fatalf("expected deterministic IDs, got %q and %q", first.ID, second.ID)
	}
}

func TestFromPlan_MixedProductsFails(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "alpha",
					Filename:   "shared.csv",
				},
			},
			{
				Type: planner.StepRunCADRuntime,
				Payload: planner.RunCADRuntimePayload{
					ProductKey:       "beta",
					Adapter:          "freecad",
					ManifestFilename: planner.ExportManifestFilename,
					ResultFilename:   planner.FreeCADRuntimeResultFilename,
				},
			},
		},
	}

	_, err := FromPlan(plan)
	if !errors.Is(err, ErrMixedProductKeys) {
		t.Fatalf("expected ErrMixedProductKeys, got %v", err)
	}
}

func TestFromPlan_MissingProductIdentityFails(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ManifestFilename: planner.ExportManifestFilename,
				},
			},
		},
	}

	_, err := FromPlan(plan)
	if !errors.Is(err, ErrMissingProductKey) {
		t.Fatalf("expected ErrMissingProductKey, got %v", err)
	}
}

func TestSplitPlan_UsesDeterministicFallbackForMissingProductIdentity(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ManifestFilename: planner.ExportManifestFilename,
				},
			},
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "wheel",
					Filename:   "wheel.csv",
				},
			},
			{
				Type: planner.StepRunCADRuntime,
				Payload: planner.RunCADRuntimePayload{
					ProductKey:       "wheel",
					Adapter:          "freecad",
					ManifestFilename: planner.ExportManifestFilename,
					ResultFilename:   planner.FreeCADRuntimeResultFilename,
				},
			},
		},
	}

	jobs, err := SplitPlan(plan)
	if err != nil {
		t.Fatalf("SplitPlan returned error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[0].ProductKey != "product_0" {
		t.Fatalf("expected fallback product key %q, got %q", "product_0", jobs[0].ProductKey)
	}
	if jobs[1].ProductKey != "wheel" {
		t.Fatalf("expected product key %q, got %q", "wheel", jobs[1].ProductKey)
	}
	if len(jobs[1].Steps()) != 2 {
		t.Fatalf("expected grouped wheel job to contain 2 steps, got %d", len(jobs[1].Steps()))
	}
}

func TestSplitPlan_IsDeterministic(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "beta",
					Filename:   "beta.csv",
				},
			},
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "alpha",
					Filename:   "alpha.csv",
				},
			},
			{
				Type: planner.StepRunCADRuntime,
				Payload: planner.RunCADRuntimePayload{
					ProductKey:       "beta",
					Adapter:          "freecad",
					ManifestFilename: planner.ExportManifestFilename,
					ResultFilename:   planner.FreeCADRuntimeResultFilename,
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       "alpha",
					ManifestFilename: planner.ExportManifestFilename,
					Product:          planner.ExportManifestProduct{ID: "alpha"},
				},
			},
		},
	}

	first, err := SplitPlan(plan)
	if err != nil {
		t.Fatalf("first SplitPlan returned error: %v", err)
	}

	second, err := SplitPlan(plan)
	if err != nil {
		t.Fatalf("second SplitPlan returned error: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("job count mismatch: first=%d second=%d", len(first), len(second))
	}

	for i := range first {
		if first[i].ProductKey != second[i].ProductKey {
			t.Fatalf("product key mismatch at job %d: first=%q second=%q", i, first[i].ProductKey, second[i].ProductKey)
		}
		if first[i].ID == "" || second[i].ID == "" {
			t.Fatalf("expected non-empty job IDs at index %d, got %q and %q", i, first[i].ID, second[i].ID)
		}
		if first[i].ID != second[i].ID {
			t.Fatalf("job ID mismatch at index %d: first=%q second=%q", i, first[i].ID, second[i].ID)
		}

		firstSteps := first[i].Steps()
		secondSteps := second[i].Steps()
		if len(firstSteps) != len(secondSteps) {
			t.Fatalf("step count mismatch at job %d: first=%d second=%d", i, len(firstSteps), len(secondSteps))
		}
		if !reflect.DeepEqual(firstSteps, secondSteps) {
			t.Fatalf("step ordering/content mismatch at job %d:\nfirst=%#v\nsecond=%#v", i, firstSteps, secondSteps)
		}
	}
}

func TestSplitPlan_FallbackKeysRemainStableForIdentityLessSteps(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ManifestFilename: "first.json",
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ManifestFilename: "second.json",
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ManifestFilename: "third.json",
				},
			},
		},
	}

	jobs, err := SplitPlan(plan)
	if err != nil {
		t.Fatalf("SplitPlan returned error: %v", err)
	}

	wantKeys := []string{"product_0", "product_1", "product_2"}
	if len(jobs) != len(wantKeys) {
		t.Fatalf("expected %d jobs, got %d", len(wantKeys), len(jobs))
	}

	for i, want := range wantKeys {
		if jobs[i].ProductKey != want {
			t.Fatalf("fallback key mismatch at index %d: got %q want %q", i, jobs[i].ProductKey, want)
		}
		if jobs[i].ID == "" {
			t.Fatalf("expected fallback job %d to receive a non-empty ID", i)
		}
		if len(jobs[i].Steps()) != 1 {
			t.Fatalf("expected fallback job %d to contain exactly 1 step, got %d", i, len(jobs[i].Steps()))
		}
	}

	repeated, err := SplitPlan(plan)
	if err != nil {
		t.Fatalf("second SplitPlan returned error: %v", err)
	}

	for i := range jobs {
		if jobs[i].ID != repeated[i].ID {
			t.Fatalf("expected stable fallback job ID at index %d, got %q and %q", i, jobs[i].ID, repeated[i].ID)
		}
	}
}

func TestNew_DifferentProductKeyProducesDifferentID(t *testing.T) {
	steps := []planner.Step{
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ManifestFilename: "shared.json",
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product:          planner.ExportManifestProduct{},
			},
		},
	}

	alpha, err := New("alpha", steps)
	if err != nil {
		t.Fatalf("alpha New returned error: %v", err)
	}

	beta, err := New("beta", steps)
	if err != nil {
		t.Fatalf("beta New returned error: %v", err)
	}

	if alpha.ID == beta.ID {
		t.Fatalf("expected different product keys to produce different IDs, got %q", alpha.ID)
	}
}

func TestNew_DifferentStepOrderProducesDifferentID(t *testing.T) {
	stepsA := []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "widget.csv",
			},
		},
		{
			Type: planner.StepRunCADRuntime,
			Payload: planner.RunCADRuntimePayload{
				ProductKey:       "widget",
				Adapter:          "freecad",
				ManifestFilename: planner.ExportManifestFilename,
				ResultFilename:   planner.FreeCADRuntimeResultFilename,
			},
		},
	}

	stepsB := []planner.Step{stepsA[1], stepsA[0]}

	first, err := New("widget", stepsA)
	if err != nil {
		t.Fatalf("first New returned error: %v", err)
	}

	second, err := New("widget", stepsB)
	if err != nil {
		t.Fatalf("second New returned error: %v", err)
	}

	if first.ID == second.ID {
		t.Fatalf("expected different step order to produce different IDs, got %q", first.ID)
	}
}

func TestNew_DifferentStepTypeProducesDifferentID(t *testing.T) {
	writeCSV, err := New("widget", []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "widget.csv",
			},
		},
	})
	if err != nil {
		t.Fatalf("writeCSV New returned error: %v", err)
	}

	runCADRuntime, err := New("widget", []planner.Step{
		{
			Type: planner.StepRunCADRuntime,
			Payload: planner.RunCADRuntimePayload{
				ProductKey:       "widget",
				Adapter:          "freecad",
				ManifestFilename: planner.ExportManifestFilename,
				ResultFilename:   planner.FreeCADRuntimeResultFilename,
			},
		},
	})
	if err != nil {
		t.Fatalf("runCADRuntime New returned error: %v", err)
	}

	if writeCSV.ID == runCADRuntime.ID {
		t.Fatalf("expected different step types to produce different IDs, got %q", writeCSV.ID)
	}
}

func TestNew_DifferentPayloadContentProducesDifferentID(t *testing.T) {
	first, err := New("widget", []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "widget.csv",
				Headers:    []string{"width"},
				Values:     []interface{}{42},
			},
		},
	})
	if err != nil {
		t.Fatalf("first New returned error: %v", err)
	}

	second, err := New("widget", []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "widget.csv",
				Headers:    []string{"width"},
				Values:     []interface{}{43},
			},
		},
	})
	if err != nil {
		t.Fatalf("second New returned error: %v", err)
	}

	if first.ID == second.ID {
		t.Fatalf("expected different payload content to produce different IDs, got %q", first.ID)
	}
}

func TestNew_MapPayloadOrderDoesNotAffectID(t *testing.T) {
	first, err := New("widget", []planner.Step{
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: "widget.json",
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product:          planner.ExportManifestProduct{ID: "widget"},
				Values: map[string]interface{}{
					"alpha": 1,
					"beta": map[string]interface{}{
						"x": true,
						"y": "yes",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("first New returned error: %v", err)
	}

	second, err := New("widget", []planner.Step{
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: "widget.json",
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product:          planner.ExportManifestProduct{ID: "widget"},
				Values: map[string]interface{}{
					"beta": map[string]interface{}{
						"y": "yes",
						"x": true,
					},
					"alpha": 1,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("second New returned error: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected map insertion order not to affect ID, got %q and %q", first.ID, second.ID)
	}
}

func TestNew_ManifestMutationsAffectIDDeterministically(t *testing.T) {
	first, err := New("widget", []planner.Step{
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: "widget.json",
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product:          planner.ExportManifestProduct{ID: "widget"},
				AssemblyMutations: &planner.ExportManifestMutationCollection{
					Properties: []planner.ExportManifestPropertyMutation{
						{Object: "Assembly", Property: "Metadata", Value: map[string]interface{}{"alpha": 1, "beta": true}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("first New returned error: %v", err)
	}

	second, err := New("widget", []planner.Step{
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: "widget.json",
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product:          planner.ExportManifestProduct{ID: "widget"},
				AssemblyMutations: &planner.ExportManifestMutationCollection{
					Properties: []planner.ExportManifestPropertyMutation{
						{Object: "Assembly", Property: "Metadata", Value: map[string]interface{}{"beta": true, "alpha": 1}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("second New returned error: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected mutation map insertion order not to affect ID, got %q and %q", first.ID, second.ID)
	}

	third, err := New("widget", []planner.Step{
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: "widget.json",
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product:          planner.ExportManifestProduct{ID: "widget"},
				AssemblyMutations: &planner.ExportManifestMutationCollection{
					Properties: []planner.ExportManifestPropertyMutation{
						{Object: "Assembly", Property: "Metadata", Value: map[string]interface{}{"alpha": 2, "beta": true}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("third New returned error: %v", err)
	}

	if first.ID == third.ID {
		t.Fatalf("expected mutation content change to affect ID, got %q", first.ID)
	}
}

func TestNew_PreservesStepOrderExactly(t *testing.T) {
	steps := []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "01-widget.csv",
				Headers:    []string{"first"},
				Values:     []interface{}{1},
			},
		},
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: "02-widget.json",
				Product:          planner.ExportManifestProduct{ID: "widget"},
			},
		},
		{
			Type: planner.StepRunCADRuntime,
			Payload: planner.RunCADRuntimePayload{
				ProductKey:       "widget",
				Adapter:          "freecad",
				ManifestFilename: "02-widget.json",
				ResultFilename:   planner.FreeCADRuntimeResultFilename,
			},
		},
	}

	j, err := New("widget", steps)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	gotSteps := j.Steps()
	if !reflect.DeepEqual(gotSteps, steps) {
		t.Fatalf("steps were not preserved in original order:\ngot=%#v\nwant=%#v", gotSteps, steps)
	}
}
