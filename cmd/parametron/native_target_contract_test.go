package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reuse the separated capture without rewriting canonical lifecycle fixtures.
func writeNativeTargetContractFixture(t *testing.T, action string) string {
	t.Helper()
	dir := writeContractPackageFixture(t, nil)
	original, _ := decodeJSONFile(t, filepath.Join(dir, "prm.cad.json"))
	data, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), "internal/engine/semantic/testdata/native-target/prm.cad.json"))
	if err != nil {
		t.Fatal(err)
	}
	var capture map[string]any
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	capture["sourceDocument"] = original["sourceDocument"]
	capture["entities"].(map[string]any)["parameters"] = original["entities"].(map[string]any)["parameters"]
	data, err = json.Marshal(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prm.cad.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "project.dsl")
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "    param width: number = 42", "    param width: number = 42\n    target mounting_bracket: action = "+action, 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestContractPackage_SeparatedNativeTarget(t *testing.T) {
	for _, action := range []string{"suppress", "hide"} {
		t.Run(action, func(t *testing.T) {
			contractPackageEnv(t)
			dir := writeNativeTargetContractFixture(t, action)
			want := contractRequestExpectation{}
			response := scriptedTargetState{}
			facts := map[string]string{}
			if action == "suppress" {
				want.assemblyMutations = `{"suppression":[{"object":"Body01","suppressed":true}]}`
				want.targetStateRequest = `{"suppression":[{"destination":"assembly","object":"Body01"}],"visibility":[],"existence":[]}`
				response.Suppression = []scriptedBooleanEvidence{observedBoolean("assembly", "Body01", true)}
				facts["assembly/Body01/suppression"] = `{"status":"observed","value":true}`
			} else {
				want.assemblyMutations = `{"visibility":[{"object":"Body01","visible":false}]}`
				want.targetStateRequest = `{"suppression":[],"visibility":[{"destination":"assembly","object":"Body01"}],"existence":[]}`
				response.Visibility = []scriptedBooleanEvidence{observedBoolean("assembly", "Body01", false)}
				facts["assembly/Body01/visibility"] = `{"status":"observed","value":false}`
			}
			evidence := scripted(t, response)
			run := runContractPackage(t, dir, filepath.Join(t.TempDir(), "out"), "success", evidence)
			if len(run.invocations) != 1 {
				t.Fatalf("runtime calls=%d, err=%v", len(run.invocations), run.err)
			}
			attempt := assertContractRequest(t, dir, run, run.invocations[0], want)
			assertContractSuccessPackage(t, run, attempt, *evidence, facts)
		})
	}
}
