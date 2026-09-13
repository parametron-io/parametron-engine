package planner

import (
	"fmt"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/cache"
)

// ComputeCacheKeyContext creates a KeyContext from an ExecutionPlan and AST.
// This function:
//   - Computes the plan hash
//   - Computes the profile hash using BuildProfileResolvedSignature
//   - Fills the KeyContext struct with all relevant information
func ComputeCacheKeyContext(plan *ExecutionPlan, ast *dsl.AST) (cache.KeyContext, error) {
	// Compute the base plan hash (profile-aware).
	planHash, err := ComputePlanHash(plan, ast)
	if err != nil {
		return cache.KeyContext{}, fmt.Errorf("failed to compute plan hash: %w", err)
	}

	// Build the KeyContext starting with the plan hash.
	ctx := cache.NewKeyContext(planHash)

	// Compute and add profile hash if a profile is active.
	if ast != nil && ast.ActiveProfileName != nil {
		profileSettings, err := ResolveActiveProfileSettings(ast)
		if err != nil {
			return cache.KeyContext{}, fmt.Errorf("failed to resolve profile settings: %w", err)
		}

		profileSignature, err := BuildProfileResolvedSignature(*ast.ActiveProfileName, profileSettings)
		if err != nil {
			return cache.KeyContext{}, fmt.Errorf("failed to build profile signature: %w", err)
		}

		ctx.WithProfileHash(profileSignature)
	}

	return ctx.Build(), nil
}

// ComputeCacheKeyContextWithIDs creates a KeyContext with product and step identifiers.
// This is useful when generating cache keys for specific execution steps.
func ComputeCacheKeyContextWithIDs(plan *ExecutionPlan, ast *dsl.AST, productID, stepID string) (cache.KeyContext, error) {
	ctx, err := ComputeCacheKeyContext(plan, ast)
	if err != nil {
		return cache.KeyContext{}, err
	}

	// Add product and step IDs.
	ctx.ProductID = productID
	ctx.StepID = stepID

	return ctx, nil
}

// ComputeStepCacheKeyContext creates a KeyContext for a specific step in the execution plan.
// This extracts product information from the step and includes it in the context.
func ComputeStepCacheKeyContext(plan *ExecutionPlan, ast *dsl.AST, stepIndex int) (cache.KeyContext, error) {
	if stepIndex < 0 || stepIndex >= len(plan.Steps) {
		return cache.KeyContext{}, fmt.Errorf("step index %d out of range [0, %d)", stepIndex, len(plan.Steps))
	}

	step := plan.Steps[stepIndex]

	// Derive product ID from step payload.
	productID := deriveProductIDFromStep(step)
	stepID := fmt.Sprintf("%d", stepIndex)

	return ComputeCacheKeyContextWithIDs(plan, ast, productID, stepID)
}

// deriveProductIDFromStep extracts the product ID from a step's payload.
func deriveProductIDFromStep(step Step) string {
	switch payload := step.Payload.(type) {
	case WriteCSVPayload:
		if payload.ProductKey != "" {
			return payload.ProductKey
		}
		// Extract from filename by removing .csv extension.
		if len(payload.Filename) > 4 && payload.Filename[len(payload.Filename)-4:] == ".csv" {
			return payload.Filename[:len(payload.Filename)-4]
		}
		return payload.Filename
	case RunCADRuntimePayload:
		return payload.ProductKey
	case WriteExportManifestPayload:
		if payload.ProductKey != "" {
			return payload.ProductKey
		}
		return payload.Product.ID
	default:
		return ""
	}
}

// KeyContextBuilder provides a fluent interface for building KeyContext with planner-specific options.
// This wraps the cache.KeyContextBuilder with additional planner-aware functionality.
type KeyContextBuilder struct {
	base *cache.KeyContextBuilder
	ctx  cache.KeyContext
}

// NewKeyContextBuilder creates a new KeyContextBuilder starting with the plan hash.
func NewKeyContextBuilder(plan *ExecutionPlan, ast *dsl.AST) (*KeyContextBuilder, error) {
	planHash, err := ComputePlanHash(plan, ast)
	if err != nil {
		return nil, fmt.Errorf("failed to compute plan hash: %w", err)
	}

	return &KeyContextBuilder{
		base: cache.NewKeyContext(planHash),
		ctx:  cache.NewKeyContext(planHash).Build(),
	}, nil
}

// WithProductID sets the product identifier.
func (b *KeyContextBuilder) WithProductID(productID string) *KeyContextBuilder {
	b.base.WithProductID(productID)
	return b
}

// WithStepID sets the step identifier.
func (b *KeyContextBuilder) WithStepID(stepID string) *KeyContextBuilder {
	b.base.WithStepID(stepID)
	return b
}

// WithStepIndex sets the step identifier from an index.
func (b *KeyContextBuilder) WithStepIndex(stepIndex int) *KeyContextBuilder {
	b.base.WithStepID(fmt.Sprintf("%d", stepIndex))
	return b
}

// WithModelHash sets the CAD model hash in the cache key context.
func (b *KeyContextBuilder) WithModelHash(modelHash string) *KeyContextBuilder {
	b.base.WithModelHash(modelHash)
	return b
}

// WithInput adds a single input key-value pair.
func (b *KeyContextBuilder) WithInput(key string, value interface{}) *KeyContextBuilder {
	b.base.WithInput(key, value)
	return b
}

// Build returns the constructed KeyContext.
func (b *KeyContextBuilder) Build() cache.KeyContext {
	return b.base.Build()
}
