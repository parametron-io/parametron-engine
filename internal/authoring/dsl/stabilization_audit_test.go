package dsl_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/testsupport/exampletables"
)

func TestStabilizationAudit_SmokeAndStressParseValidatePlan(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	smokeFiles, err := filepath.Glob(filepath.Join(repoRoot, "testdata", "dsl", "smoke", "*.dsl"))
	if err != nil {
		t.Fatalf("failed to list smoke files: %v", err)
	}
	sort.Strings(smokeFiles)
	smokeFiles = append(smokeFiles, filepath.Join(repoRoot, "testdata", "dsl", "stress.dsl"))

	for _, file := range smokeFiles {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			ast, err := dsl.Parse(file)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if err := dsl.ValidateWithTables(ast, exampletables.SmokeBreakTables()); err != nil {
				t.Fatalf("validate failed: %v", err)
			}
			if _, err := planner.CreatePlanWithTables(ast, map[string]string{}, exampletables.SmokeBreakTables()); err != nil {
				t.Fatalf("plan failed: %v", err)
			}
		})
	}
}

func TestStabilizationAudit_PlanHashDeterministicForFixedInput(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	fixedDSL := filepath.Join(repoRoot, "testdata", "dsl", "smoke", "smoke_ir_roundtrip.dsl")

	ast, err := dsl.Parse(fixedDSL)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	plan, err := planner.CreatePlan(ast, map[string]string{})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}

	first, err := planner.ComputePlanHash(plan, ast)
	if err != nil {
		t.Fatalf("compute hash failed: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, err := planner.ComputePlanHash(plan, ast)
		if err != nil {
			t.Fatalf("compute hash run %d failed: %v", i+1, err)
		}
		if got != first {
			t.Fatalf("non-deterministic plan hash on run %d: got %s want %s", i+1, got, first)
		}
	}
}

func repoRootFromCaller(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate caller file")
	}
	return repoRootFromPath(t, thisFile)
}

func repoRootFromPath(t *testing.T, path string) string {
	t.Helper()

	dir := filepath.Dir(path)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("failed to resolve repo root from %s: go.mod not found", path)
		}
		dir = parent
	}
}
