//go:build integration
// +build integration

package cache

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCacheWithStrategy_GeometryHit tests cache hit with strategy-based key generation.
func TestCacheWithStrategy_GeometryHit(t *testing.T) {
	chdirToTemp(t)

	// Create a cache with default strategy
	cache := NewCache(cacheDir)

	// Create a KeyContext
	ctx := NewKeyContext("test-plan-hash-123").
		WithProductID("widget").
		WithStepID("0").
		Build()

	// Mark geometry as done using context
	if err := cache.MarkDoneWithContext(CacheGeometry, ctx); err != nil {
		t.Fatalf("MarkDoneWithContext failed: %v", err)
	}

	// Check geometry exists using context
	exists, err := cache.ExistsWithContext(CacheGeometry, ctx)
	if err != nil {
		t.Fatalf("ExistsWithContext failed: %v", err)
	}
	if !exists {
		t.Error("Expected geometry cache hit")
	}

	// Verify the marker file exists at the expected path
	expectedKey := cache.Strategy().ComputeKey(CacheGeometry, ctx)
	expectedPath := filepath.Join(cacheDir, string(CacheGeometry), expectedKey+".done")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("Expected marker file at %s: %v", expectedPath, err)
	}
}

// TestCacheWithStrategy_LayerIsolation tests that different layers can be cached independently.
func TestCacheWithStrategy_LayerIsolation(t *testing.T) {
	chdirToTemp(t)

	cache := NewCache(cacheDir)

	ctx := NewKeyContext("shared-plan-hash").
		WithProductID("gear").
		Build()

	// Mark only geometry and artifact
	if err := cache.MarkDoneWithContext(CacheGeometry, ctx); err != nil {
		t.Fatalf("MarkDoneWithContext(geometry) failed: %v", err)
	}
	if err := cache.MarkDoneWithContext(CacheArtifact, ctx); err != nil {
		t.Fatalf("MarkDoneWithContext(artifact) failed: %v", err)
	}

	// Geometry should exist
	geoExists, _ := cache.ExistsWithContext(CacheGeometry, ctx)
	if !geoExists {
		t.Error("Expected geometry layer to exist")
	}

	// Artifact should exist
	artExists, _ := cache.ExistsWithContext(CacheArtifact, ctx)
	if !artExists {
		t.Error("Expected artifact layer to exist")
	}

	// Metadata should NOT exist
	metaExists, _ := cache.ExistsWithContext(CacheMetadata, ctx)
	if metaExists {
		t.Error("Expected metadata layer to NOT exist")
	}

	// Now mark metadata
	if err := cache.MarkDoneWithContext(CacheMetadata, ctx); err != nil {
		t.Fatalf("MarkDoneWithContext(metadata) failed: %v", err)
	}

	// Every layer should now be present under its strategy-computed key.
	for _, layer := range allLayers {
		exists, err := cache.ExistsWithContext(layer, ctx)
		if err != nil {
			t.Fatalf("ExistsWithContext(%s) failed: %v", layer, err)
		}
		if !exists {
			t.Errorf("Expected %s layer to exist after all layers marked", layer)
		}
	}
}

// TestCacheWithStrategy_DifferentContexts tests that different contexts produce different keys.
func TestCacheWithStrategy_DifferentContexts(t *testing.T) {
	chdirToTemp(t)

	cache := NewCache(cacheDir)
	strategy := cache.Strategy()

	// Different plan hashes should produce different keys
	ctx1 := NewKeyContext("plan-hash-1").Build()
	ctx2 := NewKeyContext("plan-hash-2").Build()

	key1 := strategy.ComputeKey(CacheGeometry, ctx1)
	key2 := strategy.ComputeKey(CacheGeometry, ctx2)

	if key1 == key2 {
		t.Error("Expected different keys for different plan hashes")
	}

	// Mark only ctx1
	if err := cache.MarkDoneWithContext(CacheGeometry, ctx1); err != nil {
		t.Fatalf("MarkDoneWithContext(ctx1) failed: %v", err)
	}

	// ctx1 should exist
	exists1, _ := cache.ExistsWithContext(CacheGeometry, ctx1)
	if !exists1 {
		t.Error("Expected ctx1 to exist")
	}

	// ctx2 should NOT exist
	exists2, _ := cache.ExistsWithContext(CacheGeometry, ctx2)
	if exists2 {
		t.Error("Expected ctx2 to NOT exist")
	}
}

// TestCacheWithStrategy_CustomStrategy tests using a custom strategy.
type customTestStrategy struct {
	prefix string
}

func (s *customTestStrategy) ComputeKey(layer CacheLayer, ctx KeyContext) string {
	return s.prefix + ":" + ctx.PlanHash + ":" + string(layer)
}

func (s *customTestStrategy) ComputeGeometryKey(ctx KeyContext) string {
	return s.ComputeKey(CacheGeometry, ctx)
}

func (s *customTestStrategy) ComputeArtifactKey(ctx KeyContext) string {
	return s.ComputeKey(CacheArtifact, ctx)
}

func (s *customTestStrategy) ComputeMetadataKey(ctx KeyContext) string {
	return s.ComputeKey(CacheMetadata, ctx)
}

func TestCacheWithStrategy_CustomStrategy(t *testing.T) {
	chdirToTemp(t)

	customStrategy := &customTestStrategy{prefix: "custom"}
	cache := NewCacheWithStrategy(cacheDir, customStrategy)

	// Verify the strategy is set
	if cache.Strategy() != customStrategy {
		t.Error("Expected custom strategy to be set")
	}

	ctx := NewKeyContext("test-hash").Build()

	// The key should include the custom prefix and layer
	key := cache.Strategy().ComputeKey(CacheGeometry, ctx)
	expectedKey := "custom:test-hash:geometry"
	if key != expectedKey {
		t.Errorf("Expected key %q, got %q", expectedKey, key)
	}

	// Mark and check using the custom strategy
	if err := cache.MarkDoneWithContext(CacheGeometry, ctx); err != nil {
		t.Fatalf("MarkDoneWithContext failed: %v", err)
	}

	// Verify file exists at expected path with custom key format
	expectedPath := filepath.Join(cacheDir, "geometry", expectedKey+".done")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("Expected marker file at %s: %v", expectedPath, err)
	}
}

// TestCacheWithStrategy_SetStrategy tests setting strategy after creation.
func TestCacheWithStrategy_SetStrategy(t *testing.T) {
	chdirToTemp(t)

	cache := NewCache(cacheDir)

	// Initially should have default strategy
	if _, ok := cache.Strategy().(*DefaultCacheKeyStrategy); !ok {
		t.Error("Expected default strategy initially")
	}

	// Set custom strategy
	customStrategy := &customTestStrategy{prefix: "v2"}
	cache.SetStrategy(customStrategy)

	if cache.Strategy() != customStrategy {
		t.Error("Expected custom strategy after SetStrategy")
	}
}

// TestCacheWithStrategy_NilStrategyHandling tests that nil strategy falls back to default.
func TestCacheWithStrategy_NilStrategyHandling(t *testing.T) {
	chdirToTemp(t)

	// Create cache with nil strategy (should use default)
	cache := NewCacheWithStrategy(cacheDir, nil)

	if cache.Strategy() == nil {
		t.Fatal("Expected non-nil strategy when nil passed to constructor")
	}

	if _, ok := cache.Strategy().(*DefaultCacheKeyStrategy); !ok {
		t.Error("Expected DefaultCacheKeyStrategy when nil passed")
	}

	// Test SetStrategy with nil
	cache.SetStrategy(nil)
	if _, ok := cache.Strategy().(*DefaultCacheKeyStrategy); !ok {
		t.Error("Expected DefaultKeyStrategy after SetStrategy(nil)")
	}
}

// TestCacheWithStrategy_ConcurrentAccess tests thread-safety with strategy.
func TestCacheWithStrategy_ConcurrentAccess(t *testing.T) {
	chdirToTemp(t)

	cache := NewCache(cacheDir)
	ctx := NewKeyContext("concurrent-test").Build()

	done := make(chan bool, 30)

	// Concurrent writes to different layers
	for i := 0; i < 10; i++ {
		go func() {
			if err := cache.MarkDoneWithContext(CacheGeometry, ctx); err != nil {
				t.Errorf("Concurrent geometry write failed: %v", err)
			}
			done <- true
		}()
		go func() {
			if err := cache.MarkDoneWithContext(CacheArtifact, ctx); err != nil {
				t.Errorf("Concurrent artifact write failed: %v", err)
			}
			done <- true
		}()
		go func() {
			if err := cache.MarkDoneWithContext(CacheMetadata, ctx); err != nil {
				t.Errorf("Concurrent metadata write failed: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 30; i++ {
		<-done
	}

	// All layers should exist
	for _, layer := range allLayers {
		exists, err := cache.ExistsWithContext(layer, ctx)
		if err != nil {
			t.Fatalf("ExistsWithContext(%s) failed: %v", layer, err)
		}
		if !exists {
			t.Errorf("Expected %s layer to exist after concurrent writes", layer)
		}
	}
}
