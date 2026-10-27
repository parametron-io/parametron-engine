package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/resourcebinding"
)

func addDeclaredModels(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "prm.project.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var project map[string]any
	if err := json.Unmarshal(raw, &project); err != nil {
		t.Fatal(err)
	}
	models := project["resources"].(map[string]any)["models"].(map[string]any)
	models["alias"] = "input/box.FCStd"
	models["unused"] = "standalone/box.FCStd"
	if err := os.Mkdir(filepath.Join(root, "standalone"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "standalone", "box.FCStd"), []byte("unused zero-reference source"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContractPackage_DeclaredResourceContextThroughNormalExecution(t *testing.T) {
	for _, mode := range []string{"success", "failure"} {
		t.Run(mode, func(t *testing.T) {
			contractPackageEnv(t)
			root := writeContractPackageFixture(t, nil)
			addDeclaredModels(t, root)
			run := runContractPackage(t, root, filepath.Join(t.TempDir(), "out"), mode, nil)
			if (run.err == nil) != (mode == "success") {
				t.Fatalf("unexpected execution result: %v", run.err)
			}
			if len(run.result.Execution.Jobs) != 1 {
				t.Fatalf("jobs: %v", run.result.Execution.Jobs)
			}
			outcome := run.result.Execution.Jobs[0].CADRuntimeOutcome
			if outcome == nil {
				t.Fatal("normal execution dropped outcome")
			}
			if mode == "failure" && len(run.invocations) < 2 {
				t.Fatal("failure did not exercise retries")
			}
			var selected *resourcebinding.Context
			for _, context := range run.planned.ResourceContexts {
				selected = context
			}
			if selected == nil || selected.Selected().LogicalID != "box_model" {
				t.Fatal("CLI lost explicit selected logical ID to alias")
			}
			declared := selected.Inventory().Bindings()
			if len(declared) != 3 {
				t.Fatalf("declared inventory: %v", declared)
			}
			for _, binding := range declared {
				if binding.MaterializedCoordinate != "" {
					t.Fatal("planned state claims completed staging")
				}
			}
			materialized := outcome.Resources.Bindings()
			if len(materialized) != 3 {
				t.Fatalf("outcome lost finite inventory: %v", materialized)
			}
			for n, binding := range materialized {
				coordinate := binding.MaterializedCoordinate
				if resourcebinding.ValidateCoordinate(coordinate) != nil {
					t.Fatalf("invalid completed coordinate: %v", binding)
				}
				binding.MaterializedCoordinate = ""
				if binding != declared[n] {
					t.Fatalf("join changed: %v / %v", binding, declared[n])
				}
				if binding.LogicalID == "box_model" && coordinate != "source/box.FCStd" {
					t.Fatal("single-source placement changed")
				}
				original, err := selected.SourcePath(binding)
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile(original)
				if err != nil {
					t.Fatal(err)
				}
				for _, invocation := range run.invocations {
					got, err := os.ReadFile(filepath.Join(filepath.Dir(invocation.flag(t, "--manifest")), filepath.FromSlash(coordinate)))
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("normal attempt omitted resource %q: %v", binding.LogicalID, err)
					}
				}
			}
			materialized[0].LogicalID = "caller mutation"
			if reflect.DeepEqual(materialized, outcome.Resources.Bindings()) {
				t.Fatal("outcome inventory exposed mutable storage")
			}
			seen := map[string]bool{}
			for _, invocation := range run.invocations {
				if seen[filepath.Dir(invocation.flag(t, "--manifest"))] {
					t.Fatal("retry reused workspace")
				}
				seen[filepath.Dir(invocation.flag(t, "--manifest"))] = true
			}
		})
	}
}

func TestContractPackage_DifferentProductsSelectDifferentDeclaredModels(t *testing.T) {
	contractPackageEnv(t)
	root := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, root)
	// This fixture exercises project resource selection without capture-backed
	// entity intent, whose existing contract is scoped to one source document.
	for _, name := range []string{"prm.cad.json", "prm.semantic-map.json"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "project.dsl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), "param width: number = 42", `param label: string = "box"`))
	raw = append(raw, []byte(`
product Other {
    adapter = "freecad"
    source_model = "unused"
    outputs = ["step"]
    param label: string = "other"
}
`)...)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	run := runContractPackage(t, root, filepath.Join(t.TempDir(), "out"), "success", nil)
	if run.err != nil {
		t.Fatal(run.err)
	}
	if len(run.result.Execution.Jobs) != 2 || len(run.invocations) != 2 {
		t.Fatalf("expected two executions: %v", run.result.Execution)
	}
	seen := map[string]bool{}
	for _, job := range run.result.Execution.Jobs {
		selection := run.planned.ResourceContexts[job.Job.ProductKey]
		if selection == nil || job.CADRuntimeOutcome == nil {
			t.Fatal("product lost resource context or outcome")
		}
		id := selection.Selected().LogicalID
		if seen[id] {
			t.Fatal("products share the wrong selection")
		}
		seen[id] = true
		binding, err := job.CADRuntimeOutcome.Resources.LookupCoordinate("source/box.FCStd", true)
		if err != nil || binding.LogicalID != id || binding.SourceCoordinate != selection.Selected().SourceCoordinate {
			t.Fatalf("selected materialization mismatch: %v %v", binding, err)
		}
		if len(job.CADRuntimeOutcome.Resources.Bindings()) != 3 {
			t.Fatal("product lost other declared resources")
		}
	}
	if !seen["box_model"] || !seen["unused"] {
		t.Fatalf("selected logical IDs: %v", seen)
	}
}
