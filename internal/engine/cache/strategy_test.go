package cache

import (
	"strings"
	"testing"
)

// TestDefaultStrategy_SameInputSameKey verifies that the same context produces the same key.
func TestDefaultStrategy_SameInputSameKey(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	ctx := KeyContext{
		PlanHash: "abc123def456",
	}

	key1 := strategy.ComputeKey(CacheGeometry, ctx)
	key2 := strategy.ComputeKey(CacheGeometry, ctx)

	if key1 != key2 {
		t.Errorf("expected same key for same context, got %q and %q", key1, key2)
	}
}

// TestDefaultStrategy_DifferentPlanHashDifferentKey verifies that different plan hashes produce different keys.
func TestDefaultStrategy_DifferentPlanHashDifferentKey(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	ctx1 := KeyContext{
		PlanHash: "abc123def456",
	}
	ctx2 := KeyContext{
		PlanHash: "xyz789uvw012",
	}

	key1 := strategy.ComputeKey(CacheGeometry, ctx1)
	key2 := strategy.ComputeKey(CacheGeometry, ctx2)

	if key1 == key2 {
		t.Errorf("expected different keys for different plan hashes, got %q for both", key1)
	}
}

// TestDefaultStrategy_LayerSpecificKeys verifies that different layers can use the same key.
// Phase 5 behavior: all layers use the same key (planHash).
func TestDefaultStrategy_LayerSpecificKeys(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	ctx := KeyContext{
		PlanHash: "shared_hash_123",
	}

	geoKey := strategy.ComputeKey(CacheGeometry, ctx)
	artKey := strategy.ComputeKey(CacheArtifact, ctx)
	metaKey := strategy.ComputeKey(CacheMetadata, ctx)

	// In Phase 5, all layers should have the same key.
	if geoKey != artKey || artKey != metaKey {
		t.Errorf("expected same key for all layers in Phase 5, got geo=%q, art=%q, meta=%q", geoKey, artKey, metaKey)
	}
}

// TestDefaultStrategy_ShorthandMethods verifies the convenience methods work correctly.
func TestDefaultStrategy_ShorthandMethods(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	ctx := KeyContext{
		PlanHash: "test_hash_456",
	}

	geoKey := strategy.ComputeGeometryKey(ctx)
	artKey := strategy.ComputeArtifactKey(ctx)
	metaKey := strategy.ComputeMetadataKey(ctx)

	// All shorthand methods should return the same key in Phase 5.
	if geoKey != artKey || artKey != metaKey {
		t.Errorf("expected shorthand methods to return same key in Phase 5, got geo=%q, art=%q, meta=%q", geoKey, artKey, metaKey)
	}

	// Verify they match ComputeKey.
	if geoKey != strategy.ComputeKey(CacheGeometry, ctx) {
		t.Error("ComputeGeometryKey should match ComputeKey for geometry layer")
	}
	if artKey != strategy.ComputeKey(CacheArtifact, ctx) {
		t.Error("ComputeArtifactKey should match ComputeKey for artifact layer")
	}
	if metaKey != strategy.ComputeKey(CacheMetadata, ctx) {
		t.Error("ComputeMetadataKey should match ComputeKey for metadata layer")
	}
}

// TestDefaultStrategy_KeySanitization verifies that keys are properly sanitized.
func TestDefaultStrategy_KeySanitization(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	testCases := []struct {
		name     string
		planHash string
		expected string
	}{
		{
			name:     "valid alphanumeric",
			planHash: "abc123DEF456",
			expected: "abc123DEF456",
		},
		{
			name:     "with special characters",
			planHash: "abc/123\\def:ghi",
			expected: "abc_123_def_ghi",
		},
		{
			name:     "with spaces",
			planHash: "abc 123 def",
			expected: "abc_123_def",
		},
		{
			name:     "with dots",
			planHash: "abc.tar.gz",
			expected: "abc_tar_gz",
		},
		{
			name:     "mixed valid and invalid",
			planHash: "valid-part_invalid!part",
			expected: "valid-part_invalid_part",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := KeyContext{
				PlanHash: tc.planHash,
			}

			key := strategy.ComputeKey(CacheGeometry, ctx)

			if key != tc.expected {
				t.Errorf("expected sanitized key %q, got %q", tc.expected, key)
			}

			// Verify key only contains allowed characters.
			for _, c := range key {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
					t.Errorf("key contains invalid character: %q", c)
				}
			}
		})
	}
}

// TestDefaultStrategy_KeyLengthLimit verifies that keys are truncated to 255 characters.
func TestDefaultStrategy_KeyLengthLimit(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	// Create a very long plan hash (> 255 chars).
	longHash := strings.Repeat("a", 300)

	ctx := KeyContext{
		PlanHash: longHash,
	}

	key := strategy.ComputeKey(CacheGeometry, ctx)

	if len(key) != 255 {
		t.Errorf("expected key length of 255, got %d", len(key))
	}
}

// TestKeyContext_Builder verifies the KeyContext builder helper functions.
func TestKeyContext_Builder(t *testing.T) {
	// Test basic builder.
	ctx := NewKeyContext("plan_hash_123").
		Build()

	if ctx.PlanHash != "plan_hash_123" {
		t.Errorf("expected plan hash 'plan_hash_123', got %q", ctx.PlanHash)
	}

	// Test full builder chain.
	ctx = NewKeyContext("plan_hash_456").
		WithProductID("product_abc").
		WithStepID("step_001").
		WithModelHash("model_xyz").
		WithProfileHash("profile_def").
		WithInput("key1", "value1").
		WithInput("key2", 42).
		Build()

	if ctx.PlanHash != "plan_hash_456" {
		t.Errorf("expected plan hash 'plan_hash_456', got %q", ctx.PlanHash)
	}
	if ctx.ProductID != "product_abc" {
		t.Errorf("expected product ID 'product_abc', got %q", ctx.ProductID)
	}
	if ctx.StepID != "step_001" {
		t.Errorf("expected step ID 'step_001', got %q", ctx.StepID)
	}
	if ctx.ModelHash != "model_xyz" {
		t.Errorf("expected model hash 'model_xyz', got %q", ctx.ModelHash)
	}
	if ctx.ProfileHash != "profile_def" {
		t.Errorf("expected profile hash 'profile_def', got %q", ctx.ProfileHash)
	}
	if len(ctx.Inputs) != 2 {
		t.Errorf("expected 2 inputs, got %d", len(ctx.Inputs))
	}
	if ctx.Inputs["key1"] != "value1" {
		t.Errorf("expected input key1='value1', got %v", ctx.Inputs["key1"])
	}
	if ctx.Inputs["key2"] != 42 {
		t.Errorf("expected input key2=42, got %v", ctx.Inputs["key2"])
	}
}

// TestKeyContext_BuilderWithInputs verifies the WithInputs method.
func TestKeyContext_BuilderWithInputs(t *testing.T) {
	inputs := map[string]interface{}{
		"param1": "value1",
		"param2": 123,
		"param3": true,
	}

	ctx := NewKeyContext("plan_hash").
		WithInputs(inputs).
		Build()

	if len(ctx.Inputs) != 3 {
		t.Errorf("expected 3 inputs, got %d", len(ctx.Inputs))
	}
	if ctx.Inputs["param1"] != "value1" {
		t.Errorf("expected param1='value1', got %v", ctx.Inputs["param1"])
	}
	if ctx.Inputs["param2"] != 123 {
		t.Errorf("expected param2=123, got %v", ctx.Inputs["param2"])
	}
	if ctx.Inputs["param3"] != true {
		t.Errorf("expected param3=true, got %v", ctx.Inputs["param3"])
	}
}

// TestKeyContext_String verifies the String() method for debugging.
func TestKeyContext_String(t *testing.T) {
	ctx := NewKeyContext("abc123").
		WithProductID("widget").
		WithStepID("step1").
		Build()

	s := ctx.String()

	if !strings.Contains(s, "PlanHash=abc123") {
		t.Error("String() should contain PlanHash")
	}
	if !strings.Contains(s, "ProductID=widget") {
		t.Error("String() should contain ProductID")
	}
	if !strings.Contains(s, "StepID=step1") {
		t.Error("String() should contain StepID")
	}
}

// TestKeyContext_StringMinimal verifies String() with minimal context.
func TestKeyContext_StringMinimal(t *testing.T) {
	ctx := NewKeyContext("hash_only").Build()

	s := ctx.String()

	if s != "KeyContext{PlanHash=hash_only}" {
		t.Errorf("expected minimal string representation, got %q", s)
	}
}

// TestGlobalDefaultStrategy verifies the global default strategy getter/setter.
func TestGlobalDefaultStrategy(t *testing.T) {
	// Save original to restore later.
	original := GetDefaultStrategy()
	defer SetDefaultStrategy(original)

	// Test default is DefaultCacheKeyStrategy.
	defaultStrategy := GetDefaultStrategy()
	if defaultStrategy == nil {
		t.Fatal("expected non-nil default strategy")
	}

	// Test that we can set a custom strategy.
	customStrategy := &DefaultCacheKeyStrategy{}
	SetDefaultStrategy(customStrategy)

	if GetDefaultStrategy() != customStrategy {
		t.Error("expected custom strategy to be set")
	}

	// Test ResetDefaultStrategy.
	ResetDefaultStrategy()
	if GetDefaultStrategy() == customStrategy {
		t.Error("expected strategy to be reset to default")
	}
}

// TestDefaultCacheKeyStrategy_ThreadSafety verifies thread safety.
func TestDefaultCacheKeyStrategy_ThreadSafety(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}
	ctx := KeyContext{PlanHash: "concurrent_test"}

	// Run concurrent key generation.
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			key := strategy.ComputeKey(CacheGeometry, ctx)
			if key != "concurrent_test" {
				t.Errorf("unexpected key: %q", key)
			}
			done <- true
		}()
	}

	// Wait for all goroutines.
	for i := 0; i < 100; i++ {
		<-done
	}
}

// TestKeyContextBuilder_ThreadSafety verifies builder thread safety.
func TestKeyContextBuilder_ThreadSafety(t *testing.T) {
	done := make(chan KeyContext, 100)

	for i := 0; i < 100; i++ {
		go func(idx int) {
			ctx := NewKeyContext("base_hash").
				WithProductID(string(rune('A' + idx%26))).
				Build()
			done <- ctx
		}(i)
	}

	// Collect all contexts.
	contexts := make([]KeyContext, 100)
	for i := 0; i < 100; i++ {
		contexts[i] = <-done
	}

	// Verify all have correct base hash.
	for i, ctx := range contexts {
		if ctx.PlanHash != "base_hash" {
			t.Errorf("context %d: expected plan hash 'base_hash', got %q", i, ctx.PlanHash)
		}
	}
}

func TestSignatureV2Strategy_EmptyModelHashUsesContextHash(t *testing.T) {
	strategy := &SignatureV2Strategy{}
	ctx := KeyContext{
		PlanHash:  "plan_hash_123",
		ModelHash: "",
	}

	key := strategy.ComputeKey(CacheGeometry, ctx)
	if key == ctx.PlanHash {
		t.Fatal("expected context-derived key to differ from bare plan hash")
	}
	if key != computeHashFromContext(ctx) {
		t.Fatal("expected Signature V2 context hash")
	}
	if repeated := strategy.ComputeKey(CacheGeometry, ctx); repeated != key {
		t.Fatalf("expected deterministic key, got %q and %q", key, repeated)
	}
	ctx.Inputs = map[string]interface{}{}
	if got := strategy.ComputeKey(CacheGeometry, ctx); got != key {
		t.Fatal("expected nil and empty inputs to produce the same key")
	}
	ctx.ProfileHash = "profile_hash"
	if got := strategy.ComputeKey(CacheGeometry, ctx); got == key {
		t.Fatal("expected changed profile context to change the key")
	}
}

func TestSignatureV2Strategy_WhitespaceModelHashOmitted(t *testing.T) {
	strategy := &SignatureV2Strategy{}
	ctx := KeyContext{
		PlanHash:  "plan_hash_123",
		ModelHash: "   \t\n",
	}

	key := strategy.ComputeKey(CacheGeometry, ctx)
	ctx.ModelHash = ""
	if key != strategy.ComputeKey(CacheGeometry, ctx) || key != computeHashFromContext(ctx) {
		t.Fatalf("expected whitespace model hash to be omitted, got %q", key)
	}
}

func TestSignatureV2Strategy_ModelHashChangesKeyDeterministically(t *testing.T) {
	strategy := &SignatureV2Strategy{}
	ctx := KeyContext{
		PlanHash:    "plan_hash_123",
		ModelHash:   "abc",
		ProfileHash: "profile_hash",
		Inputs: map[string]interface{}{
			"foo": "bar",
		},
	}

	key1 := strategy.ComputeKey(CacheGeometry, ctx)
	key2 := strategy.ComputeKey(CacheGeometry, ctx)
	if key1 != key2 {
		t.Fatalf("expected deterministic key, got %q and %q", key1, key2)
	}
	if key1 == "plan_hash_123" {
		t.Fatalf("expected key to differ from legacy plan hash when model hash is present")
	}
}

func TestSignatureV2Strategy_ContextualInputsForceHashedKeyWithoutModelHash(t *testing.T) {
	strategy := &SignatureV2Strategy{}
	ctx := KeyContext{
		PlanHash:  "plan_hash_123",
		ModelHash: "",
		Inputs: map[string]interface{}{
			"tables": []map[string]string{
				{"logicalId": "fasteners", "fingerprint": "fp-1"},
			},
		},
	}

	key := strategy.ComputeKey(CacheGeometry, ctx)
	if key == "plan_hash_123" {
		t.Fatal("expected non-legacy key when contextual inputs are present")
	}
}
