package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/projectinput"
	"parametron/internal/engine/resourcebinding"
)

func TestSchedulerResourceContextsSnapshotAndProductIsolation(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"project.dsl": `dsl v1.0
product First { adapter = "freecad" source_model = "first" outputs = ["step"] param label: string = "first" }
product Second { adapter = "freecad" source_model = "second" outputs = ["step"] param label: string = "second" }
`,
		"prm.project.json": `{"version":"1.0","projectId":"p","dsl":"project.dsl","resources":{"models":{"first":"first.FCStd","second":"second.FCStd","unused":"unused.FCStd"}}}`,
		"first.FCStd":      "first", "second.FCStd": "second", "unused.FCStd": "unused",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	project, err := projectinput.ResolveProject(root)
	if err != nil {
		t.Fatal(err)
	}
	ast, err := dsl.Parse(project.DSLPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatal(err)
	}
	if err := projectinput.ApplyModelMappings(ast, project); err != nil {
		t.Fatal(err)
	}
	plan, err := planner.CreatePlan(ast, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	contexts := map[string]*resourcebinding.Context{}
	want := map[string]string{}
	// The product keys are planner-owned; correlate each manifest to its source.
	for _, step := range plan.Steps {
		if manifest, ok := step.Payload.(planner.WriteExportManifestPayload); ok {
			id := "first"
			product := "First"
			if manifest.Inputs.SourceModel == filepath.Join(root, "second.FCStd") {
				id = "second"
				product = "Second"
			}
			contexts[manifest.ProductKey] = project.ModelSelections[product]
			want[manifest.ProductKey] = id
		}
	}
	if len(contexts) != 2 {
		t.Fatalf("expected two product contexts: %v", contexts)
	}
	fake := &schedulerFakeCADAdapter{}
	s := NewWithProductDirs(t.TempDir(), func(dir string) *executor.Executor {
		return executor.NewWithCADRuntimeOrchestration(fake, schedulerFakeResolver{}, dir)
	}).WithResourceContexts(contexts)
	for key := range contexts {
		contexts[key] = nil
		delete(contexts, key)
	}
	if err := s.Execute(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	requests := fake.orchestrateRequests()
	if len(requests) != 2 {
		t.Fatalf("requests: %d", len(requests))
	}
	for _, req := range requests {
		if req.Resources == nil || req.Resources.Selected().LogicalID != want[req.ProductKey] || req.Resources.SelectedSourcePath() != req.Manifest.Inputs.SourceModel {
			t.Fatalf("product selection leaked: %v", req)
		}
		if !reflect.DeepEqual(req.Resources.Inventory().Bindings(), project.ModelInventory.Bindings()) {
			t.Fatal("scheduler/executor dropped unselected declarations")
		}
		bindings := req.Resources.Inventory().Bindings()
		bindings[0].LogicalID = "corrupted"
		if !reflect.DeepEqual(req.Resources.Inventory().Bindings(), project.ModelInventory.Bindings()) {
			t.Fatal("request inventory exposed mutable storage")
		}
	}
	after, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("context propagation mutated planner payload")
	}
}
