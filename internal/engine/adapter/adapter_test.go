package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
)

type mockAdapter struct {
	receivedCtx  context.Context
	receivedStep planner.Step
	returnErr    error
}

var _ Adapter = (*mockAdapter)(nil)

func (m *mockAdapter) Run(ctx context.Context, step planner.Step) error {
	m.receivedCtx = ctx
	m.receivedStep = step
	return m.returnErr
}

func invokeRun(adp Adapter, ctx context.Context, step planner.Step) error {
	return adp.Run(ctx, step)
}

func TestAdapterContract_ContextPropagation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	step := planner.Step{Type: planner.StepWriteCSV}
	mock := &mockAdapter{}

	if err := invokeRun(mock, ctx, step); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if mock.receivedCtx != ctx {
		t.Fatal("mock did not receive the same context instance")
	}
}

func TestAdapterContract_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	step := planner.Step{Type: planner.StepRunCADRuntime}
	expectedErr := errors.New("mock run error")
	mock := &mockAdapter{returnErr: expectedErr}

	err := invokeRun(mock, ctx, step)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
}

func TestAdapterContract_Success(t *testing.T) {
	ctx := context.Background()
	step := planner.Step{Type: planner.StepWriteCSV}
	mock := &mockAdapter{}

	if err := invokeRun(mock, ctx, step); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestAdapterContract_DoesNotReferenceVerificationAuthority(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file path")
	}
	packageDir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("failed to read adapter package dir: %v", err)
	}

	forbidden := []string{
		"prm.observed.json",
		"verification_contract_invalid",
		"observed_artifact_invalid",
		"metadata_mismatch",
		"reference_mismatch",
		"required_observation_missing",
		"internal_verification_error",
		"verification.Verify(",
	}

	var scanned []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned = append(scanned, name)
		data, err := os.ReadFile(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		text := string(data)
		for _, token := range forbidden {
			if strings.Contains(text, token) {
				t.Fatalf("adapter package must stay execution-only; found forbidden token %q in %s", token, name)
			}
		}
	}
	slices.Sort(scanned)
	if len(scanned) == 0 {
		t.Fatal("expected adapter package source files to scan")
	}
}
