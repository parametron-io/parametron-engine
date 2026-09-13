package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/testsupport/exampletables"
)

type breakExpectation struct {
	Expect    string `json:"expect"`
	Stage     string `json:"stage,omitempty"`
	Class     string `json:"class,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type breakOutcome struct {
	Expect string `json:"expect"`
	Stage  string `json:"stage,omitempty"`
	Class  string `json:"class,omitempty"`
	Error  string `json:"error,omitempty"`
}

const (
	breakWorkerEnabledEnv  = "PARAMETRON_BREAK_WORKER"
	breakWorkerFileEnv     = "PARAMETRON_BREAK_FILE"
	breakWorkerResultEnv   = "PARAMETRON_BREAK_RESULT"
	breakWorkerProgressEnv = "PARAMETRON_BREAK_PROGRESS"
	defaultCaseTimeout     = 5 * time.Second
)

func TestBreakMatrix(t *testing.T) {
	if os.Getenv(breakWorkerEnabledEnv) == "1" {
		t.Skip("worker mode")
	}

	repoRoot := repoRootFromCaller(t)
	breakDir := filepath.Join(repoRoot, "testdata", "dsl", "break")
	expectationPath := filepath.Join(breakDir, "_expectations.json")

	expectations := loadBreakExpectations(t, expectationPath)
	files := listBreakDSLFiles(t, breakDir)

	var mismatches []string
	seen := make(map[string]struct{}, len(files))

	for _, file := range files {
		base := filepath.Base(file)
		seen[base] = struct{}{}
		expected, ok := expectations[base]
		if !ok {
			actual := runBreakCaseInWorker(t, file, breakExpectation{Expect: "pass"})
			template := expectationTemplate(base, actual)
			mismatches = append(mismatches, fmt.Sprintf("missing expectation for %s\nsuggested entry:\n%s", base, template))
			continue
		}

		actual := runBreakCaseInWorker(t, file, expected)
		if expected.Expect != actual.Expect {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: expected result=%s, got result=%s\nsuggested entry:\n%s",
				base, expected.Expect, actual.Expect, expectationTemplate(base, actual),
			))
			continue
		}

		if expected.Expect == "fail" || expected.Expect == "timeout" {
			if expected.Stage != actual.Stage {
				mismatches = append(mismatches, fmt.Sprintf(
					"%s: expected stage=%s, got stage=%s\nsuggested entry:\n%s",
					base, expected.Stage, actual.Stage, expectationTemplate(base, actual),
				))
			}
			if expected.Class != actual.Class {
				mismatches = append(mismatches, fmt.Sprintf(
					"%s: expected class=%s, got class=%s\nsuggested entry:\n%s",
					base, expected.Class, actual.Class, expectationTemplate(base, actual),
				))
			}
		}
	}

	for fileName := range expectations {
		if _, ok := seen[fileName]; ok {
			continue
		}
		mismatches = append(mismatches, fmt.Sprintf("stale expectation entry: %s (file not found in testdata/dsl/break)", fileName))
	}

	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		t.Fatalf("break matrix mismatches (%d):\n%s", len(mismatches), strings.Join(mismatches, "\n\n"))
	}
}

func TestBreakMatrixWorker(t *testing.T) {
	if os.Getenv(breakWorkerEnabledEnv) != "1" {
		t.Skip("not in worker mode")
	}

	filePath := os.Getenv(breakWorkerFileEnv)
	resultPath := os.Getenv(breakWorkerResultEnv)
	progressPath := os.Getenv(breakWorkerProgressEnv)
	if filePath == "" || resultPath == "" || progressPath == "" {
		t.Fatalf("worker env is incomplete")
	}

	writeProgress := func(stage string) {
		if err := os.WriteFile(progressPath, []byte(stage), 0o644); err != nil {
			t.Fatalf("failed to write progress %q: %v", stage, err)
		}
	}

	var ast *dsl.AST
	writeProgress("parse")
	ast, err := dsl.Parse(filePath)
	if err != nil {
		writeBreakOutcome(t, resultPath, breakOutcome{
			Expect: "fail",
			Stage:  "parse",
			Class:  classifyBreakError("parse", err),
			Error:  err.Error(),
		})
		return
	}

	writeProgress("validate")
	if err := dsl.ValidateWithTables(ast, exampletables.SmokeBreakTables()); err != nil {
		writeBreakOutcome(t, resultPath, breakOutcome{
			Expect: "fail",
			Stage:  "validate",
			Class:  classifyBreakError("validate", err),
			Error:  err.Error(),
		})
		return
	}

	writeProgress("plan")
	if _, err := planner.CreatePlanWithTables(ast, map[string]string{}, exampletables.SmokeBreakTables()); err != nil {
		writeBreakOutcome(t, resultPath, breakOutcome{
			Expect: "fail",
			Stage:  "plan",
			Class:  classifyBreakError("plan", err),
			Error:  err.Error(),
		})
		return
	}

	writeProgress("done")
	writeBreakOutcome(t, resultPath, breakOutcome{Expect: "pass"})
}

func runBreakCaseInWorker(t *testing.T, filePath string, expected breakExpectation) breakOutcome {
	t.Helper()

	resultFile := filepath.Join(t.TempDir(), "break-result.json")
	progressFile := filepath.Join(t.TempDir(), "break-progress.txt")
	if err := os.WriteFile(progressFile, []byte("init"), 0o644); err != nil {
		t.Fatalf("failed to initialize progress file: %v", err)
	}

	timeout := defaultCaseTimeout
	if expected.Expect == "timeout" {
		if expected.TimeoutMS <= 0 {
			t.Fatalf("timeout expectation for %s must set timeout_ms", filepath.Base(filePath))
		}
		timeout = time.Duration(expected.TimeoutMS) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBreakMatrixWorker$")
	cmd.Env = append(os.Environ(),
		breakWorkerEnabledEnv+"=1",
		breakWorkerFileEnv+"="+filePath,
		breakWorkerResultEnv+"="+resultFile,
		breakWorkerProgressEnv+"="+progressFile,
	)

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		stage := readProgressStage(progressFile)
		return breakOutcome{
			Expect: "timeout",
			Stage:  stage,
			Class:  stage + "_timeout",
			Error:  strings.TrimSpace(string(output)),
		}
	}
	if err != nil {
		t.Fatalf("worker failed for %s: %v\nworker output:\n%s", filepath.Base(filePath), err, strings.TrimSpace(string(output)))
	}

	data, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("worker did not produce result for %s: %v", filepath.Base(filePath), err)
	}

	var outcome breakOutcome
	if err := json.Unmarshal(data, &outcome); err != nil {
		t.Fatalf("invalid worker result JSON for %s: %v\nraw: %s", filepath.Base(filePath), err, string(data))
	}
	return outcome
}

func classifyBreakError(stage string, err error) string {
	msg := strings.ToLower(err.Error())
	switch stage {
	case "parse":
		switch {
		case strings.Contains(msg, "lex error: unclosed block comment"):
			return "lex_unclosed_block_comment"
		case strings.Contains(msg, "unsupported dsl version"):
			return "parse_version_unsupported"
		case strings.Contains(msg, "reserved keyword"):
			return "parse_reserved_keyword"
		case strings.Contains(msg, "unexpected token in expression: '1e"):
			return "parse_numeric_literal_invalid"
		default:
			return "parse_syntax_error"
		}
	case "validate":
		switch {
		case strings.Contains(msg, "circular dependency") || strings.Contains(msg, "circular reference"):
			return "validation_circular_dependency"
		case strings.Contains(msg, "constants cannot depend on runtime values"):
			return "validation_constant_runtime_dependency"
		case strings.Contains(msg, "duplicate product name") || strings.Contains(msg, "duplicate parameter name") || strings.Contains(msg, "constant already defined"):
			return "validation_namespace_collision"
		case strings.Contains(msg, "invalid enum value"):
			return "validation_enum_value_invalid"
		case strings.Contains(msg, "right side of comparison") || strings.Contains(msg, "left side of comparison") || strings.Contains(msg, "has type"):
			return "validation_type_mismatch"
		case strings.Contains(msg, "unsafe output_dir") || strings.Contains(msg, "path contains directory traversal"):
			return "validation_security_path_traversal"
		case strings.Contains(msg, "undefined constant"):
			return "validation_undefined_constant"
		case strings.Contains(msg, "undefined parameter reference") || strings.Contains(msg, "undefined binding reference"):
			return "validation_undefined_identifier"
		case strings.Contains(msg, "invalid control character"):
			return "validation_string_control_character"
		default:
			return "validation_error"
		}
	case "plan":
		switch {
		case strings.Contains(msg, "division by zero"):
			return "plan_division_by_zero"
		case strings.Contains(msg, "file_pattern"):
			return "plan_file_pattern_invalid"
		default:
			return "plan_error"
		}
	default:
		return "unknown_error"
	}
}

func loadBreakExpectations(t *testing.T, path string) map[string]breakExpectation {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read expectations file %s: %v", path, err)
	}

	out := make(map[string]breakExpectation)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("failed to parse expectations JSON %s: %v", path, err)
	}
	return out
}

func listBreakDSLFiles(t *testing.T, breakDir string) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(breakDir, "*.dsl"))
	if err != nil {
		t.Fatalf("failed to enumerate break DSL files: %v", err)
	}
	sort.Strings(files)
	return files
}

func readProgressStage(progressPath string) string {
	data, err := os.ReadFile(progressPath)
	if err != nil {
		return "unknown"
	}
	stage := strings.TrimSpace(string(data))
	if stage == "" {
		return "unknown"
	}
	return stage
}

func writeBreakOutcome(t *testing.T, resultPath string, outcome breakOutcome) {
	t.Helper()

	data, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("failed to encode outcome: %v", err)
	}
	if err := os.WriteFile(resultPath, data, 0o644); err != nil {
		t.Fatalf("failed to write outcome: %v", err)
	}
}

func expectationTemplate(fileName string, actual breakOutcome) string {
	template := map[string]any{
		"expect": actual.Expect,
	}
	if actual.Expect == "fail" || actual.Expect == "timeout" {
		template["stage"] = actual.Stage
		template["class"] = actual.Class
	}
	raw, _ := json.MarshalIndent(template, "", "  ")
	return fmt.Sprintf("\"%s\": %s", fileName, string(raw))
}

func repoRootFromCaller(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}
