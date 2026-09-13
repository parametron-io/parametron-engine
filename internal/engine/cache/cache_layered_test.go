//go:build integration
// +build integration

package cache

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// A) Cache Key Contracts (Deterministic Behavior)
// ============================================================================

// TestDeterministicKeys_SameDSL: When the same DSL is executed twice,
// all layer keys should be identical (deterministic)
func TestDeterministicKeys_SameDSL(t *testing.T) {
	chdirToTemp(t)
	planHash := "dsl-v1-abc123"

	// First run: mark all layers
	for _, layer := range allLayers {
		if err := MarkDoneLayer(layer, planHash); err != nil {
			t.Fatalf("MarkDoneLayer failed: %v", err)
		}
	}

	// Check that all layers use the same key
	layers := []CacheLayer{CacheGeometry, CacheArtifact, CacheMetadata}
	for _, layer := range layers {
		path := filepath.Join(cacheDir, string(layer), planHash+".done")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Layer %s marker not found at expected path: %s", layer, path)
		}
	}

	// Second run: should hit the cache
	for _, layer := range allLayers {
		exists, err := ExistsLayer(layer, planHash)
		if err != nil {
			t.Fatalf("ExistsLayer failed: %v", err)
		}
		if !exists {
			t.Error("Expected cache hit for same DSL plan hash")
		}
	}
}

// TestLayerIsolation_GeometryOnlyChanged: Changing only parameters causes
// a geometry miss, but this test is limited by the current API
// Note: To be extended when separate keys are implemented in Phase 6
func TestLayerIsolation_Conceptual(t *testing.T) {
	chdirToTemp(t)

	// Current implementation: all layers depend on planHash
	// Future: geometryKey, artifactKey, and metadataKey will be separate

	planHashV1 := "plan-v1"
	planHashV2 := "plan-v2" // Different parameters

	// Create cache entries for V1
	for _, layer := range allLayers {
		if err := MarkDoneLayer(layer, planHashV1); err != nil {
			t.Fatalf("MarkDoneLayer v1 failed: %v", err)
		}
	}

	// Expect a cache miss for V2 (different hash)
	for _, layer := range allLayers {
		exists, err := ExistsLayer(layer, planHashV2)
		if err != nil {
			t.Fatalf("ExistsLayer v2 failed: %v", err)
		}
		if exists {
			t.Error("Expected cache miss for different plan hash")
		}
	}

	// V1 should still hit
	for _, layer := range allLayers {
		exists, err := ExistsLayer(layer, planHashV1)
		if err != nil {
			t.Fatalf("ExistsLayer v1 failed: %v", err)
		}
		if !exists {
			t.Error("Expected cache hit for original plan hash")
		}
	}
}

// ============================================================================
// B) Invalidation / Change Matrix
// ============================================================================

// TestInvalidation_AllLayers: A parameter change affects all layers
// (in the current implementation, a planHash change causes all layers to miss)
func TestInvalidation_AllLayersOnPlanChange(t *testing.T) {
	chdirToTemp(t)
	oldHash := "plan-old"
	newHash := "plan-new"

	// Create cache entries for the old plan
	for _, layer := range []CacheLayer{CacheGeometry, CacheArtifact, CacheMetadata} {
		if err := MarkDoneLayer(layer, oldHash); err != nil {
			t.Fatalf("MarkDoneLayer %s failed: %v", layer, err)
		}
	}

	// Each layer should miss for the new plan
	for _, layer := range []CacheLayer{CacheGeometry, CacheArtifact, CacheMetadata} {
		exists, err := ExistsLayer(layer, newHash)
		if err != nil {
			t.Fatalf("ExistsLayer %s failed: %v", layer, err)
		}
		if exists {
			t.Errorf("Expected %s miss for new plan hash", layer)
		}
	}
}

// TestPartialCache_HitOnlySomeLayers: Some layers exist, others are missing
func TestPartialCache_HitOnlySomeLayers(t *testing.T) {
	chdirToTemp(t)
	hash := "partial-test"

	// Mark only geometry and artifact
	if err := MarkDoneLayer(CacheGeometry, hash); err != nil {
		t.Fatalf("MarkDoneLayer geometry failed: %v", err)
	}
	if err := MarkDoneLayer(CacheArtifact, hash); err != nil {
		t.Fatalf("MarkDoneLayer artifact failed: %v", err)
	}

	// The full run misses because metadata is missing
	exists, err := ExistsLayer(CacheMetadata, hash)
	if err != nil {
		t.Fatalf("ExistsLayer failed: %v", err)
	}
	if exists {
		t.Error("Expected full run miss when metadata layer is missing")
	}

	// But individual layer checks should be correct
	geoExists, _ := ExistsLayer(CacheGeometry, hash)
	artExists, _ := ExistsLayer(CacheArtifact, hash)
	metaExists, _ := ExistsLayer(CacheMetadata, hash)

	if !geoExists {
		t.Error("Expected geometry layer to exist")
	}
	if !artExists {
		t.Error("Expected artifact layer to exist")
	}
	if metaExists {
		t.Error("Expected metadata layer to NOT exist")
	}

	// Now add metadata
	if err := MarkDoneLayer(CacheMetadata, hash); err != nil {
		t.Fatalf("MarkDoneLayer metadata failed: %v", err)
	}

	// The full run should now hit
	for _, layer := range allLayers {
		exists, err := ExistsLayer(layer, hash)
		if err != nil {
			t.Fatalf("ExistsLayer after complete failed: %v", err)
		}
		if !exists {
			t.Error("Expected full run hit after all layers marked")
		}
	}
}

// ============================================================================
// C) Concurrency Safety
// ============================================================================

// TestConcurrentLayerWrites: Concurrent writes to the same layer
func TestConcurrentLayerWrites(t *testing.T) {
	chdirToTemp(t)
	hash := "concurrent-layer"
	layer := CacheGeometry

	// Write concurrently with 10 goroutines
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			if err := MarkDoneLayer(layer, hash); err != nil {
				t.Errorf("Concurrent MarkDoneLayer failed: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to finish
	for i := 0; i < 10; i++ {
		<-done
	}

	// The file should not be corrupted
	exists, err := ExistsLayer(layer, hash)
	if err != nil {
		t.Fatalf("ExistsLayer failed: %v", err)
	}
	if !exists {
		t.Error("Expected layer to exist after concurrent writes")
	}
}

// TestConcurrentFullRunWrites: Concurrent full-run cache writes
func TestConcurrentFullRunWrites(t *testing.T) {
	chdirToTemp(t)
	hash := "concurrent-full"

	// Write full runs concurrently with 10 goroutines
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for _, layer := range allLayers {
				if err := MarkDoneLayer(layer, hash); err != nil {
					t.Errorf("Concurrent MarkDoneLayer failed: %v", err)
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines to finish
	for i := 0; i < 10; i++ {
		<-done
	}

	// All layers should be intact
	for _, layer := range allLayers {
		exists, err := ExistsLayer(layer, hash)
		if err != nil {
			t.Fatalf("ExistsLayer failed: %v", err)
		}
		if !exists {
			t.Error("Expected full run cache to exist after concurrent writes")
		}
	}

	// Check each layer individually
	for _, layer := range []CacheLayer{CacheGeometry, CacheArtifact, CacheMetadata} {
		layerExists, _ := ExistsLayer(layer, hash)
		if !layerExists {
			t.Errorf("Expected %s layer to exist", layer)
		}
	}
}

// ============================================================================
// D) Partial Failure Behavior (Simulation)
// ============================================================================

// TestCacheCommitSemantics: Unsuccessful runs must not be cached
// This test verifies correct use of the cache API
func TestCacheCommitSemantics(t *testing.T) {
	chdirToTemp(t)
	hash := "commit-test"

	// Simulation: The run started but failed
	// Partial information must not be cached

	// The cache should initially be empty
	for _, layer := range allLayers {
		exists, _ := ExistsLayer(layer, hash)
		if exists {
			t.Error("Cache should be empty initially")
		}
	}

	// MarkDoneLayer is called after a successful run
	for _, layer := range allLayers {
		if err := MarkDoneLayer(layer, hash); err != nil {
			t.Fatalf("MarkDoneLayer failed: %v", err)
		}
	}

	// The cache should now be populated
	for _, layer := range allLayers {
		exists, _ := ExistsLayer(layer, hash)
		if !exists {
			t.Error("Cache should be populated after successful run")
		}
	}
}

// TestCacheLayerIsolation: Failure in one layer should not affect others
// Note: This should be tested at the scheduler/executor level, not the cache API level
// Here we only verify that the API allows it
func TestCacheLayerIsolation_API(t *testing.T) {
	chdirToTemp(t)
	hash := "isolated-layers"

	// Mark only some layers (failed-run simulation)
	if err := MarkDoneLayer(CacheGeometry, hash); err != nil {
		t.Fatalf("MarkDoneLayer geometry failed: %v", err)
	}

	// Artifact failed; metadata was never attempted
	// The geometry cache should remain separate

	geoExists, _ := ExistsLayer(CacheGeometry, hash)
	artExists, _ := ExistsLayer(CacheArtifact, hash)
	metaExists, _ := ExistsLayer(CacheMetadata, hash)

	if !geoExists {
		t.Error("Geometry layer should be cached despite other failures")
	}
	if artExists || metaExists {
		t.Error("Artifact and metadata should NOT be cached")
	}
}

// ============================================================================
// Helper functions
// ============================================================================

func chdirToTemp(t *testing.T) string {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "cache_layered_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
		_ = os.RemoveAll(tmpDir)
	})

	return tmpDir
}
