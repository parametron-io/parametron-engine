package planner

import (
	"os"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/cache"
)

// TestComputeKeyContext_ProfileAware verifies that profile changes affect the context.
func TestComputeKeyContext_ProfileAware(t *testing.T) {
	dslContentA := `
profile Build {
    output_dir = "out/build"
    metadata_enabled = true
}
use profile Build

product Test {
    param width: number = 100
}
`
	dslContentB := `
profile Build {
    output_dir = "out/build-v2"
    metadata_enabled = true
}
use profile Build

product Test {
    param width: number = 100
}
`

	astA, planA := parseValidateAndPlanForContext(t, dslContentA)
	astB, planB := parseValidateAndPlanForContext(t, dslContentB)

	ctxA, err := ComputeCacheKeyContext(planA, astA)
	if err != nil {
		t.Fatalf("ComputeCacheKeyContext A failed: %v", err)
	}

	ctxB, err := ComputeCacheKeyContext(planB, astB)
	if err != nil {
		t.Fatalf("ComputeCacheKeyContext B failed: %v", err)
	}

	// Plan hashes should be different due to different profile settings
	if ctxA.PlanHash == ctxB.PlanHash {
		t.Error("Expected different plan hashes for different profile settings")
	}

	// Profile signatures should be different
	if ctxA.ProfileHash == ctxB.ProfileHash {
		t.Error("Expected different profile hashes for different profile settings")
	}
}

// TestComputeKeyContext_Deterministic verifies that same plan + profile = same context.
func TestComputeKeyContext_Deterministic(t *testing.T) {
	dslContent := `
profile Build {
    output_dir = "out/build"
    metadata_enabled = true
}
use profile Build

product Test {
    param width: number = 100
    param height: number = 50
}
`

	ast, plan := parseValidateAndPlanForContext(t, dslContent)

	// Compute context multiple times
	var contexts []cache.KeyContext
	for i := 0; i < 10; i++ {
		ctx, err := ComputeCacheKeyContext(plan, ast)
		if err != nil {
			t.Fatalf("ComputeCacheKeyContext iteration %d failed: %v", i, err)
		}
		contexts = append(contexts, ctx)
	}

	// All contexts should be identical
	firstCtx := contexts[0]
	for i, ctx := range contexts[1:] {
		if ctx.PlanHash != firstCtx.PlanHash {
			t.Errorf("Iteration %d: plan hash mismatch: %s vs %s", i+1, ctx.PlanHash, firstCtx.PlanHash)
		}
		if ctx.ProfileHash != firstCtx.ProfileHash {
			t.Errorf("Iteration %d: profile hash mismatch: %s vs %s", i+1, ctx.ProfileHash, firstCtx.ProfileHash)
		}
	}
}

// TestComputeKeyContext_NoProfile verifies context creation without profile.
func TestComputeKeyContext_NoProfile(t *testing.T) {
	dslContent := `
product Test {
    param width: number = 100
}
`

	ast, plan := parseValidateAndPlanForContext(t, dslContent)

	ctx, err := ComputeCacheKeyContext(plan, ast)
	if err != nil {
		t.Fatalf("ComputeCacheKeyContext failed: %v", err)
	}

	// Plan hash should be set
	if ctx.PlanHash == "" {
		t.Error("Expected non-empty plan hash")
	}

	// Profile hash should be empty (no profile)
	if ctx.ProfileHash != "" {
		t.Errorf("Expected empty profile hash without profile, got %q", ctx.ProfileHash)
	}
}

// TestComputeKeyContextWithIDs verifies context creation with IDs.
func TestComputeKeyContextWithIDs(t *testing.T) {
	dslContent := `
profile Build {
    output_dir = "out"
}
use profile Build

product Widget {
    param width: number = 100
}
`

	ast, plan := parseValidateAndPlanForContext(t, dslContent)

	ctx, err := ComputeCacheKeyContextWithIDs(plan, ast, "widget-product", "step-001")
	if err != nil {
		t.Fatalf("ComputeCacheKeyContextWithIDs failed: %v", err)
	}

	if ctx.ProductID != "widget-product" {
		t.Errorf("Expected ProductID 'widget-product', got %q", ctx.ProductID)
	}

	if ctx.StepID != "step-001" {
		t.Errorf("Expected StepID 'step-001', got %q", ctx.StepID)
	}

	// Plan hash and profile hash should still be set
	if ctx.PlanHash == "" {
		t.Error("Expected non-empty plan hash")
	}
}

// TestComputeStepCacheKeyContext verifies per-step context creation.
func TestComputeStepCacheKeyContext(t *testing.T) {
	dslContent := `
profile Build {
    output_dir = "out"
}
use profile Build

product Widget {
    param width: number = 100
}
product Gear {
    param diameter: number = 50
}
`

	ast, plan := parseValidateAndPlanForContext(t, dslContent)

	// Should have 4 steps: CSV+Manifest for each product (no adapter runner by default).
	if len(plan.Steps) != 4 {
		t.Fatalf("Expected 4 steps, got %d", len(plan.Steps))
	}

	// Test step 0 (Widget CSV)
	ctx0, err := ComputeStepCacheKeyContext(plan, ast, 0)
	if err != nil {
		t.Fatalf("ComputeStepCacheKeyContext(0) failed: %v", err)
	}
	if ctx0.StepID != "0" {
		t.Errorf("Expected StepID '0', got %q", ctx0.StepID)
	}
	if ctx0.ProductID != "Widget" {
		t.Errorf("Expected ProductID 'Widget', got %q", ctx0.ProductID)
	}

	// Test step 1 (Widget Manifest)
	ctx1, err := ComputeStepCacheKeyContext(plan, ast, 1)
	if err != nil {
		t.Fatalf("ComputeStepCacheKeyContext(1) failed: %v", err)
	}
	if ctx1.StepID != "1" {
		t.Errorf("Expected StepID '1', got %q", ctx1.StepID)
	}
	if ctx1.ProductID != "Widget" {
		t.Errorf("Expected ProductID 'Widget', got %q", ctx1.ProductID)
	}

	// Test step 2 (Gear CSV)
	ctx2, err := ComputeStepCacheKeyContext(plan, ast, 2)
	if err != nil {
		t.Fatalf("ComputeStepCacheKeyContext(2) failed: %v", err)
	}
	if ctx2.StepID != "2" {
		t.Errorf("Expected StepID '2', got %q", ctx2.StepID)
	}
	if ctx2.ProductID != "Gear" {
		t.Errorf("Expected ProductID 'Gear', got %q", ctx2.ProductID)
	}

	// Test step 3 (Gear Manifest)
	ctx3, err := ComputeStepCacheKeyContext(plan, ast, 3)
	if err != nil {
		t.Fatalf("ComputeStepCacheKeyContext(3) failed: %v", err)
	}
	if ctx3.StepID != "3" {
		t.Errorf("Expected StepID '3', got %q", ctx3.StepID)
	}
	if ctx3.ProductID != "Gear" {
		t.Errorf("Expected ProductID 'Gear', got %q", ctx3.ProductID)
	}
}

// TestComputeStepCacheKeyContext_InvalidIndex verifies error handling for invalid indices.
func TestComputeStepCacheKeyContext_InvalidIndex(t *testing.T) {
	dslContent := `
product Test {
    param width: number = 100
}
`

	ast, plan := parseValidateAndPlanForContext(t, dslContent)

	// Test negative index
	_, err := ComputeStepCacheKeyContext(plan, ast, -1)
	if err == nil {
		t.Error("Expected error for negative index")
	}

	// Test out of bounds index
	_, err = ComputeStepCacheKeyContext(plan, ast, 100)
	if err == nil {
		t.Error("Expected error for out of bounds index")
	}
}

// TestKeyContextBuilder_Planner verifies the planner's KeyContextBuilder.
func TestKeyContextBuilder_Planner(t *testing.T) {
	dslContent := `
profile Build {
    output_dir = "out"
}
use profile Build

product Widget {
    param width: number = 100
}
`

	ast, plan := parseValidateAndPlanForContext(t, dslContent)

	builder, err := NewKeyContextBuilder(plan, ast)
	if err != nil {
		t.Fatalf("NewKeyContextBuilder failed: %v", err)
	}

	ctx := builder.
		WithProductID("custom-product").
		WithStepIndex(5).
		WithInput("extra", "value").
		Build()

	if ctx.ProductID != "custom-product" {
		t.Errorf("Expected ProductID 'custom-product', got %q", ctx.ProductID)
	}
	if ctx.StepID != "5" {
		t.Errorf("Expected StepID '5', got %q", ctx.StepID)
	}
	if ctx.Inputs["extra"] != "value" {
		t.Errorf("Expected input 'extra'='value', got %v", ctx.Inputs["extra"])
	}
	if ctx.PlanHash == "" {
		t.Error("Expected non-empty plan hash")
	}
}

// TestDeriveProductIDFromStep verifies product ID extraction from steps.
func TestDeriveProductIDFromStep(t *testing.T) {
	tests := []struct {
		name     string
		step     Step
		expected string
	}{
		{
			name: "WriteCSV with ProductKey",
			step: Step{
				Type:    StepWriteCSV,
				Payload: WriteCSVPayload{ProductKey: "widget-123", Filename: "output.csv"},
			},
			expected: "widget-123",
		},
		{
			name: "WriteCSV without ProductKey",
			step: Step{
				Type:    StepWriteCSV,
				Payload: WriteCSVPayload{Filename: "gear.csv"},
			},
			expected: "gear",
		},
		{
			name: "WriteCSV with path in filename",
			step: Step{
				Type:    StepWriteCSV,
				Payload: WriteCSVPayload{Filename: "data.output.csv"},
			},
			expected: "data.output",
		},
		{
			name: "WriteExportManifest with ProductKey",
			step: Step{
				Type: StepWriteExportManifest,
				Payload: WriteExportManifestPayload{
					ProductKey: "widget-789",
					Product:    ExportManifestProduct{ID: "Widget"},
				},
			},
			expected: "widget-789",
		},
		{
			name: "WriteExportManifest without ProductKey",
			step: Step{
				Type: StepWriteExportManifest,
				Payload: WriteExportManifestPayload{
					Product: ExportManifestProduct{ID: "Widget"},
				},
			},
			expected: "Widget",
		},
		{
			name:     "Unknown step type",
			step:     Step{Type: "Unknown"},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := deriveProductIDFromStep(tc.step)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestKeyContext_ContextEquality verifies that contexts with same inputs are equal.
func TestKeyContext_ContextEquality(t *testing.T) {
	dslContent := `
product Test {
    param width: number = 100
}
`

	ast, plan := parseValidateAndPlanForContext(t, dslContent)

	ctx1, err := ComputeCacheKeyContextWithIDs(plan, ast, "product-1", "step-0")
	if err != nil {
		t.Fatalf("ComputeCacheKeyContextWithIDs failed: %v", err)
	}

	ctx2, err := ComputeCacheKeyContextWithIDs(plan, ast, "product-1", "step-0")
	if err != nil {
		t.Fatalf("ComputeCacheKeyContextWithIDs (second) failed: %v", err)
	}

	// Same inputs should produce same context values
	if ctx1.PlanHash != ctx2.PlanHash {
		t.Error("Expected same plan hash for same inputs")
	}
	if ctx1.ProductID != ctx2.ProductID {
		t.Error("Expected same product ID")
	}
	if ctx1.StepID != ctx2.StepID {
		t.Error("Expected same step ID")
	}
}

// Helper function for tests
func parseValidateAndPlanForContext(t *testing.T, dslContent string) (*dsl.AST, *ExecutionPlan) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "hash_context_test_*.dsl")
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
