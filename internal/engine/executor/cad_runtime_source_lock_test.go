package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"parametron/internal/engine/integrity"
	"parametron/internal/engine/runtimecap"
)

const (
	executorSourceDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	executorLockedDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func executorProjectIntegrity(scope integrity.LockScope) *integrity.ProjectIntegrity {
	return integrity.EvaluateLock(scope, []integrity.ResourceSource{{
		LogicalID:        "widget",
		SourceCoordinate: "models/widget.FCStd",
		SourceDigest:     integrity.Digest(executorSourceDigest),
	}})
}

func TestExecutorSourceLockRejectsInvalidSourceBeforeOrchestration(t *testing.T) {
	tests := []struct {
		name  string
		scope integrity.LockScope
		code  string
	}{
		{
			name:  "source bytes changed after lock creation",
			scope: integrity.LockScope{Present: true, Models: []integrity.LockEntry{{LogicalID: "widget", Digest: executorLockedDigest}}},
			code:  integrity.CodeLockSourceMismatch,
		},
		{
			name:  "malformed locked digest",
			scope: integrity.LockScope{Present: true, Models: []integrity.LockEntry{{LogicalID: "widget", Digest: "not-a-digest"}}},
			code:  integrity.CodeLockDigestMalformed,
		},
		{
			name:  "declared resource missing from lock scope",
			scope: integrity.LockScope{Present: true, Models: []integrity.LockEntry{{LogicalID: "other", Digest: executorSourceDigest}}},
			code:  integrity.CodeLockResourceMissing,
		},
		{
			name:  "unreadable project lock",
			scope: integrity.LockScope{Present: true, Invalid: true},
			code:  integrity.CodeLockUnavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &cadTestAdapter{}
			resolver := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
			e, _ := newCADExecutor(t, adapter, resolver)
			e.SetSourceIntegrity(executorProjectIntegrity(tc.scope))
			pkg := alignedPackage(t, "freecad")

			if err := e.ExecutePackage(context.Background(), pkg); err == nil {
				t.Fatal("ExecutePackage accepted a run with invalid declared source identity")
			} else {
				var consumption *CADRuntimeConsumptionError
				if !errors.As(err, &consumption) {
					t.Fatalf("error = %v, want a typed consumption error", err)
				}
				if consumption.Stage != cadRuntimeDispatchStageSourceLock {
					t.Fatalf("consumption stage = %q, want %q", consumption.Stage, cadRuntimeDispatchStageSourceLock)
				}
				if consumption.Category != "integrity" || consumption.Boundary != "engine" {
					t.Fatalf("consumption classification = %q/%q, want engine/integrity", consumption.Boundary, consumption.Category)
				}
				if consumption.Code != tc.code {
					t.Fatalf("consumption code = %q, want %q", consumption.Code, tc.code)
				}
				for _, collapsed := range []string{"metadata_mismatch", "runtime", "verification"} {
					if consumption.Category == collapsed || consumption.Code == collapsed {
						t.Fatalf("integrity failure collapsed into %q", collapsed)
					}
				}
			}

			if len(adapter.requests) != 0 {
				t.Fatalf("runtime was invoked despite a rejected source lock: %d attempts", len(adapter.requests))
			}
			if len(resolver.calls) != 1 {
				t.Fatalf("executable resolution calls = %d, want one before the source-lock check", len(resolver.calls))
			}
			states := e.States()
			for _, state := range states {
				if state.Attempts != 0 {
					t.Fatalf("source-lock rejection started a step: %#v", state)
				}
			}
		})
	}
}

func TestExecutorSourceLockVerifiedSourceProceeds(t *testing.T) {
	adapter := &cadTestAdapter{}
	resolver := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, adapter, resolver)
	e.SetSourceIntegrity(executorProjectIntegrity(integrity.LockScope{
		Present: true,
		Models:  []integrity.LockEntry{{LogicalID: "widget", Digest: executorSourceDigest}},
	}))
	pkg := alignedPackage(t, "freecad")

	if err := e.ExecutePackage(context.Background(), pkg); err != nil {
		t.Fatalf("ExecutePackage returned error for a verified lock: %v", err)
	}
	if len(adapter.requests) != 1 {
		t.Fatalf("orchestration attempts = %d, want one", len(adapter.requests))
	}
}

func TestExecutorUnlockedWorkflowProceedsWithoutVerificationClaim(t *testing.T) {
	adapter := &cadTestAdapter{}
	resolver := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, adapter, resolver)
	e.SetSourceIntegrity(executorProjectIntegrity(integrity.LockScope{}))
	pkg := alignedPackage(t, "freecad")

	if err := e.ExecutePackage(context.Background(), pkg); err != nil {
		t.Fatalf("ExecutePackage returned error for an unlocked workflow: %v", err)
	}
	if len(adapter.requests) != 1 {
		t.Fatalf("orchestration attempts = %d, want one", len(adapter.requests))
	}
}

func TestExecutorSourceLockFailureIdentityIsPathFree(t *testing.T) {
	adapter := &cadTestAdapter{}
	resolver := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, adapter, resolver)
	e.SetSourceIntegrity(executorProjectIntegrity(integrity.LockScope{
		Present: true,
		Models:  []integrity.LockEntry{{LogicalID: "widget", Digest: executorLockedDigest}},
	}))
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err == nil {
		t.Fatal("ExecutePackage accepted changed source bytes")
	} else {
		if strings.Contains(err.Error(), "/opt/runtime") || strings.Contains(err.Error(), "working") {
			t.Fatalf("source-lock failure leaked host path material: %v", err)
		}
		if !strings.Contains(err.Error(), integrity.CodeLockSourceMismatch) {
			t.Fatalf("source-lock failure omitted its stable code: %v", err)
		}
	}
}
