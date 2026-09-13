package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"parametron/internal/authoring/dsl"
)

func TestComputePlanHash_DeterministicAcrossRunsWithProfile(t *testing.T) {
	dslContent := `
const OUT_DIR = "out/build"
profile Build {
    output_dir = OUT_DIR
    metadata_enabled = true
}
use profile Build

product Test {
    param width: number = 100
}
`

	ast, plan := parseValidateAndPlan(t, dslContent)

	firstHash, err := ComputePlanHash(plan, ast)
	if err != nil {
		t.Fatalf("failed to compute first hash: %v", err)
	}

	for i := 0; i < 100; i++ {
		hash, err := ComputePlanHash(plan, ast)
		if err != nil {
			t.Fatalf("failed to compute hash at iteration %d: %v", i, err)
		}
		if hash != firstHash {
			t.Fatalf("hash mismatch at iteration %d: expected %s, got %s", i, firstHash, hash)
		}
	}
}

func TestBuildProfileResolvedSignature_DeterministicWithMapInsertionOrder(t *testing.T) {
	settingsA := make(map[string]interface{})
	settingsA["output_dir"] = "out/build"
	settingsA["metadata_enabled"] = true
	settingsA["retries"] = float64(3)

	settingsB := make(map[string]interface{})
	settingsB["retries"] = float64(3)
	settingsB["output_dir"] = "out/build"
	settingsB["metadata_enabled"] = true

	sigA, err := BuildProfileResolvedSignature("Build", settingsA)
	if err != nil {
		t.Fatalf("failed to build first signature: %v", err)
	}
	sigB, err := BuildProfileResolvedSignature("Build", settingsB)
	if err != nil {
		t.Fatalf("failed to build second signature: %v", err)
	}

	if sigA != sigB {
		t.Fatalf("profile signature mismatch across map insertion orders:\nA:\n%s\nB:\n%s", sigA, sigB)
	}

	expected := "profile:Build\nmetadata_enabled=true\noutput_dir=\"out/build\"\nretries=3\n"
	if sigA != expected {
		t.Fatalf("unexpected canonical signature:\nexpected:\n%s\ngot:\n%s", expected, sigA)
	}
}

func TestComputePlanHash_ProfileSettingChangeChangesHash(t *testing.T) {
	dslA := `
profile Build {
    output_dir = "out/build"
    metadata_enabled = true
}
use profile Build

product Test {
    param width: number = 100
}
`
	dslB := `
profile Build {
    output_dir = "out/build-v2"
    metadata_enabled = true
}
use profile Build

product Test {
    param width: number = 100
}
`

	astA, planA := parseValidateAndPlan(t, dslA)
	astB, planB := parseValidateAndPlan(t, dslB)

	if !reflect.DeepEqual(planA, planB) {
		t.Fatalf("expected plan graphs to match; only profile settings should differ")
	}

	hashA, err := ComputePlanHash(planA, astA)
	if err != nil {
		t.Fatalf("failed to compute hash A: %v", err)
	}
	hashB, err := ComputePlanHash(planB, astB)
	if err != nil {
		t.Fatalf("failed to compute hash B: %v", err)
	}

	if hashA == hashB {
		t.Fatalf("expected hash to change when selected profile settings change")
	}
}

func TestComputePlanHash_BackwardCompatibleWithoutProfile(t *testing.T) {
	noProfileDSL := `
product Test {
    param width: number = 100
}
`
	withProfileDSL := `
profile Build {
    output_dir = "out/build"
}
use profile Build

product Test {
    param width: number = 100
}
`

	astNoProfile, planNoProfile := parseValidateAndPlan(t, noProfileDSL)
	legacy := legacyPlanHash(t, planNoProfile)

	hashNoProfile, err := ComputePlanHash(planNoProfile, astNoProfile)
	if err != nil {
		t.Fatalf("failed to compute no-profile hash: %v", err)
	}
	if hashNoProfile != legacy {
		t.Fatalf("no-profile hash should match legacy hash: expected %s, got %s", legacy, hashNoProfile)
	}

	astWithProfile, planWithProfile := parseValidateAndPlan(t, withProfileDSL)
	if !reflect.DeepEqual(planNoProfile, planWithProfile) {
		t.Fatalf("expected plan graphs to be identical between profile/no-profile DSL")
	}

	hashWithProfile, err := ComputePlanHash(planWithProfile, astWithProfile)
	if err != nil {
		t.Fatalf("failed to compute profile hash: %v", err)
	}
	if hashWithProfile == legacy {
		t.Fatalf("expected profile-aware hash to differ from legacy hash")
	}

	hashNoProfileAgain, err := ComputePlanHash(planNoProfile, astNoProfile)
	if err != nil {
		t.Fatalf("failed to recompute no-profile hash: %v", err)
	}
	if hashNoProfileAgain != legacy {
		t.Fatalf("expected no-profile hash to remain equal to legacy after profile hashing")
	}
}

func TestComputePlanHash_FilePatternChangeChangesHash(t *testing.T) {
	dslA := `
profile Prod {
    file_pattern = "{product}_{param:width}"
}
use profile Prod

product Test {
    param width: number = 100
}
`
	dslB := `
profile Prod {
    file_pattern = "{profile}_{product}_{param:width}"
}
use profile Prod

product Test {
    param width: number = 100
}
`

	astA, planA := parseValidateAndPlan(t, dslA)
	astB, planB := parseValidateAndPlan(t, dslB)

	hashA, err := ComputePlanHash(planA, astA)
	if err != nil {
		t.Fatalf("failed to compute hash A: %v", err)
	}
	hashB, err := ComputePlanHash(planB, astB)
	if err != nil {
		t.Fatalf("failed to compute hash B: %v", err)
	}

	if hashA == hashB {
		t.Fatalf("expected hash to change when file_pattern changes")
	}
}

func TestComputePlanHash_ProductExecutionDeclarationChangeChangesHash(t *testing.T) {
	dslA := `
product Test {
    adapter = "freecad"
    source_model = "input/a.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`
	dslB := `
product Test {
    adapter = "freecad"
    source_model = "input/b.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`

	astA, planA := parseValidateAndPlan(t, dslA)
	astB, planB := parseValidateAndPlan(t, dslB)

	hashA, err := ComputePlanHash(planA, astA)
	if err != nil {
		t.Fatalf("failed to compute hash A: %v", err)
	}
	hashB, err := ComputePlanHash(planB, astB)
	if err != nil {
		t.Fatalf("failed to compute hash B: %v", err)
	}

	if hashA == hashB {
		t.Fatalf("expected hash to change when product execution declarations change")
	}
}

func TestComputePlanHash_LetExpressionFormDoesNotAffectExecutionIdentity(t *testing.T) {
	dslA := `
product Demo {
    let a = 10
    param x: number = a * 2
}
`
	dslB := `
product Demo {
    let a = 5 + 5
    param x: number = a * 2
}
`

	astA, planA := parseValidateAndPlan(t, dslA)
	astB, planB := parseValidateAndPlan(t, dslB)

	if !reflect.DeepEqual(planA, planB) {
		t.Fatalf("expected identical plans when only internal let expression form changes")
	}

	hashA, err := ComputePlanHash(planA, astA)
	if err != nil {
		t.Fatalf("failed to compute hash A: %v", err)
	}
	hashB, err := ComputePlanHash(planB, astB)
	if err != nil {
		t.Fatalf("failed to compute hash B: %v", err)
	}
	if hashA != hashB {
		t.Fatalf("expected identical hashes when only internal let expression form changes: %s vs %s", hashA, hashB)
	}

	if len(planA.Steps) != 2 || len(planB.Steps) != 2 {
		t.Fatalf("expected both plans to contain csv and export manifest steps")
	}

	writeA, ok := planA.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected first plan CSV payload, got %T", planA.Steps[0].Payload)
	}
	writeB, ok := planB.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected second plan CSV payload, got %T", planB.Steps[0].Payload)
	}
	if !reflect.DeepEqual(writeA, writeB) {
		t.Fatalf("expected identical CSV payloads when only internal let expression form changes")
	}

	manifestA, ok := planA.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected first plan manifest payload, got %T", planA.Steps[1].Payload)
	}
	manifestB, ok := planB.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected second plan manifest payload, got %T", planB.Steps[1].Payload)
	}
	if !reflect.DeepEqual(manifestA.Values, manifestB.Values) {
		t.Fatalf("expected identical exported values when only internal let expression form changes")
	}

	expectedAssignments := []ExportManifestParameterAssignment{
		{Name: "x", Value: 20.0, Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(manifestA.ParameterAssignments, expectedAssignments) {
		t.Fatalf("unexpected parameter assignments for first plan: %+v", manifestA.ParameterAssignments)
	}
	if !reflect.DeepEqual(manifestB.ParameterAssignments, expectedAssignments) {
		t.Fatalf("unexpected parameter assignments for second plan: %+v", manifestB.ParameterAssignments)
	}
}

func TestResolveActiveProfileSettings_ProfileOnlySettings(t *testing.T) {
	dslContent := `
profile Dev {
    output_dir = "out"
    metadata_enabled = true
}
use profile Dev

product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "pdf"]

    param label: string = "box"
}
`

	ast, _ := parseValidateAndPlan(t, dslContent)

	settings, err := ResolveActiveProfileSettings(ast)
	if err != nil {
		t.Fatalf("ResolveActiveProfileSettings failed: %v", err)
	}

	if got := settings["output_dir"]; got != "out" {
		t.Fatalf("expected output_dir=out, got %v", got)
	}
	if got := settings["metadata_enabled"]; got != true {
		t.Fatalf("expected metadata_enabled=true, got %v", got)
	}
}

func parseValidateAndPlan(t *testing.T, dslContent string) (*dsl.AST, *ExecutionPlan) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "planner_hash_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("failed to validate DSL: %v", err)
	}

	plan, err := CreatePlan(ast, map[string]string{})
	if err != nil {
		t.Fatalf("failed to create plan: %v", err)
	}
	return ast, plan
}

func legacyPlanHash(t *testing.T, plan *ExecutionPlan) string {
	t.Helper()

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("failed to marshal plan: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
