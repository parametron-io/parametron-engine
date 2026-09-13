package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// KeyContext holds all contextual information needed to generate cache keys.
type KeyContext struct {
	// PlanHash is the base plan hash (profile-aware).
	PlanHash string
	// ProductID is an optional product identifier for product-scoped keys.
	ProductID string
	// StepID is an optional step identifier for step-scoped keys.
	StepID string
	// ModelHash is an optional CAD model hash for cache signatures.
	ModelHash string
	// ProfileHash is the resolved profile settings hash.
	ProfileHash string
	// Inputs are additional inputs affecting cache validity.
	Inputs map[string]interface{}
}

// CacheKeyStrategy defines how cache keys are generated for different layers.
type CacheKeyStrategy interface {
	// ComputeKey generates a deterministic cache key for the given layer and context.
	ComputeKey(layer CacheLayer, ctx KeyContext) string
	// ComputeGeometryKey is a shorthand for geometry layer (optional convenience).
	ComputeGeometryKey(ctx KeyContext) string
	// ComputeArtifactKey is a shorthand for artifact layer (optional convenience).
	ComputeArtifactKey(ctx KeyContext) string
	// ComputeMetadataKey is a shorthand for metadata layer (optional convenience).
	ComputeMetadataKey(ctx KeyContext) string
}

// DefaultCacheKeyStrategy is the backward-compatible implementation.
// All layers use the same plan hash key for backward compatibility.
type DefaultCacheKeyStrategy struct {
	// Currently stateless; version/config may be added in the future.
	// dummy field ensures unique memory addresses for different instances
	_ byte
}

// SignatureV2Strategy is the context-based cache signature strategy.
// Keys are derived deterministically from the full context, with optional model hash.
type SignatureV2Strategy struct{}

// ComputeKey generates a cache key for the given layer and context.
func (s *SignatureV2Strategy) ComputeKey(layer CacheLayer, ctx KeyContext) string {
	trimmedModelHash := strings.TrimSpace(ctx.ModelHash)
	ctxCopy := ctx
	ctxCopy.ModelHash = trimmedModelHash

	key := computeHashFromContext(ctxCopy)
	return sanitizeKey(key)
}

// ComputeGeometryKey generates a cache key for the geometry layer.
func (s *SignatureV2Strategy) ComputeGeometryKey(ctx KeyContext) string {
	return s.ComputeKey(CacheGeometry, ctx)
}

// ComputeArtifactKey generates a cache key for the artifact layer.
func (s *SignatureV2Strategy) ComputeArtifactKey(ctx KeyContext) string {
	return s.ComputeKey(CacheArtifact, ctx)
}

// ComputeMetadataKey generates a cache key for the metadata layer.
func (s *SignatureV2Strategy) ComputeMetadataKey(ctx KeyContext) string {
	return s.ComputeKey(CacheMetadata, ctx)
}

// ComputeKey generates a cache key for the given layer and context.
// All layers share the plan hash key for backward compatibility.
func (s *DefaultCacheKeyStrategy) ComputeKey(layer CacheLayer, ctx KeyContext) string {
	// Use the plan hash without layer-specific prefixes or suffixes.
	key := ctx.PlanHash

	// Validate key format and length.
	key = sanitizeKey(key)

	return key
}

// ComputeGeometryKey generates a cache key for the geometry layer.
func (s *DefaultCacheKeyStrategy) ComputeGeometryKey(ctx KeyContext) string {
	return s.ComputeKey(CacheGeometry, ctx)
}

// ComputeArtifactKey generates a cache key for the artifact layer.
func (s *DefaultCacheKeyStrategy) ComputeArtifactKey(ctx KeyContext) string {
	return s.ComputeKey(CacheArtifact, ctx)
}

// ComputeMetadataKey generates a cache key for the metadata layer.
func (s *DefaultCacheKeyStrategy) ComputeMetadataKey(ctx KeyContext) string {
	return s.ComputeKey(CacheMetadata, ctx)
}

// keySanitizeRegex is compiled once at package initialization for performance.
var keySanitizeRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeKey ensures the key is filesystem-friendly and within length limits.
// Key format: [a-zA-Z0-9_-]+ (filesystem-friendly)
// Max length: 255 characters (ext4 limit consideration).
func sanitizeKey(key string) string {
	// Replace any characters not in [a-zA-Z0-9_-] with underscore.
	key = keySanitizeRegex.ReplaceAllString(key, "_")

	// Truncate if longer than 255 characters.
	if len(key) > 255 {
		// Use the first 255 characters.
		key = key[:255]
	}

	return key
}

// computeHashFromContext creates a deterministic hash from KeyContext fields.
// This is a helper function for future use when we need more complex key generation.
func computeHashFromContext(ctx KeyContext) string {
	// Combine all relevant fields.
	data := struct {
		PlanHash    string                 `json:"planHash"`
		ProductID   string                 `json:"productId,omitempty"`
		StepID      string                 `json:"stepId,omitempty"`
		ModelHash   string                 `json:"modelHash,omitempty"`
		ProfileHash string                 `json:"profileHash,omitempty"`
		Inputs      map[string]interface{} `json:"inputs,omitempty"`
	}{
		PlanHash:    ctx.PlanHash,
		ProductID:   ctx.ProductID,
		StepID:      ctx.StepID,
		ModelHash:   ctx.ModelHash,
		ProfileHash: ctx.ProfileHash,
		Inputs:      ctx.Inputs,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		// Fallback to plan hash if serialization fails.
		return ctx.PlanHash
	}

	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:])
}

// KeyContextBuilder provides a fluent API for building KeyContext.
type KeyContextBuilder struct {
	ctx KeyContext
}

// NewKeyContext creates a new KeyContextBuilder with the required plan hash.
func NewKeyContext(planHash string) *KeyContextBuilder {
	return &KeyContextBuilder{
		ctx: KeyContext{
			PlanHash: planHash,
			Inputs:   make(map[string]interface{}),
		},
	}
}

// WithProductID sets the product ID.
func (b *KeyContextBuilder) WithProductID(productID string) *KeyContextBuilder {
	b.ctx.ProductID = productID
	return b
}

// WithStepID sets the step ID.
func (b *KeyContextBuilder) WithStepID(stepID string) *KeyContextBuilder {
	b.ctx.StepID = stepID
	return b
}

// WithModelHash sets the model hash.
func (b *KeyContextBuilder) WithModelHash(modelHash string) *KeyContextBuilder {
	b.ctx.ModelHash = modelHash
	return b
}

// WithProfileHash sets the profile hash.
func (b *KeyContextBuilder) WithProfileHash(profileHash string) *KeyContextBuilder {
	b.ctx.ProfileHash = profileHash
	return b
}

// WithInput adds a single input key-value pair.
func (b *KeyContextBuilder) WithInput(key string, value interface{}) *KeyContextBuilder {
	if b.ctx.Inputs == nil {
		b.ctx.Inputs = make(map[string]interface{})
	}
	b.ctx.Inputs[key] = value
	return b
}

// WithInputs sets all inputs at once.
func (b *KeyContextBuilder) WithInputs(inputs map[string]interface{}) *KeyContextBuilder {
	b.ctx.Inputs = inputs
	return b
}

// Build returns the constructed KeyContext.
func (b *KeyContextBuilder) Build() KeyContext {
	return b.ctx
}

// Global default strategy instance.
var (
	globalDefaultStrategy   CacheKeyStrategy = &DefaultCacheKeyStrategy{}
	globalDefaultStrategyMu sync.RWMutex
)

// SetDefaultStrategy sets the global default cache key strategy.
// This is used by the cache package when no strategy is explicitly provided.
func SetDefaultStrategy(strategy CacheKeyStrategy) {
	globalDefaultStrategyMu.Lock()
	defer globalDefaultStrategyMu.Unlock()
	globalDefaultStrategy = strategy
}

// GetDefaultStrategy returns the current global default cache key strategy.
func GetDefaultStrategy() CacheKeyStrategy {
	globalDefaultStrategyMu.RLock()
	defer globalDefaultStrategyMu.RUnlock()
	return globalDefaultStrategy
}

// ResetDefaultStrategy resets the global default strategy to DefaultCacheKeyStrategy.
// This is primarily useful for testing.
func ResetDefaultStrategy() {
	globalDefaultStrategyMu.Lock()
	defer globalDefaultStrategyMu.Unlock()
	globalDefaultStrategy = &DefaultCacheKeyStrategy{}
}

// String returns a string representation of KeyContext for debugging.
func (ctx KeyContext) String() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("PlanHash=%s", ctx.PlanHash))
	if ctx.ProductID != "" {
		parts = append(parts, fmt.Sprintf("ProductID=%s", ctx.ProductID))
	}
	if ctx.StepID != "" {
		parts = append(parts, fmt.Sprintf("StepID=%s", ctx.StepID))
	}
	if ctx.ModelHash != "" {
		parts = append(parts, fmt.Sprintf("ModelHash=%s", ctx.ModelHash))
	}
	if ctx.ProfileHash != "" {
		parts = append(parts, fmt.Sprintf("ProfileHash=%s", ctx.ProfileHash))
	}
	if len(ctx.Inputs) > 0 {
		parts = append(parts, fmt.Sprintf("Inputs=%d", len(ctx.Inputs)))
	}
	return "KeyContext{" + strings.Join(parts, ", ") + "}"
}
