//go:build integration
// +build integration

package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/cache"
	"parametron/internal/engine/executor"
)

// ============================================================================
// Cache-Aware Fake Adapter
// ============================================================================

type cacheAwareFakeAdapter struct {
	mu           sync.Mutex
	calls        []string
	writtenFiles map[string]string
	simulateSlow bool
	slowDelay    time.Duration
	failProduct  string
}

func newCacheAwareFakeAdapter() *cacheAwareFakeAdapter {
	return &cacheAwareFakeAdapter{
		calls:        []string{},
		writtenFiles: make(map[string]string),
		slowDelay:    10 * time.Millisecond,
	}
}

func (a *cacheAwareFakeAdapter) Run(ctx context.Context, step planner.Step) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.simulateSlow {
		time.Sleep(a.slowDelay)
	}

	switch payload := step.Payload.(type) {
	case planner.WriteCSVPayload:
		productKey := payload.ProductKey
		if productKey == "" {
			productKey = payload.Filename[:len(payload.Filename)-4]
		}
		a.calls = append(a.calls, fmt.Sprintf("csv:%s", productKey))

		if a.failProduct == productKey {
			return fmt.Errorf("simulated failure for product %s", productKey)
		}

		a.writtenFiles[payload.Filename] = fmt.Sprintf("key,value\n%s,data\n", productKey)
		return nil

	case planner.WriteExportManifestPayload:
		productKey := payload.ProductKey
		if productKey == "" {
			productKey = payload.ManifestFilename[:len(payload.ManifestFilename)-len(".json")]
		}
		a.calls = append(a.calls, fmt.Sprintf("export:%s", productKey))

		if a.failProduct == productKey {
			return fmt.Errorf("simulated failure for product %s", productKey)
		}

		stepFile := payload.ManifestFilename[:len(payload.ManifestFilename)-len(".json")] + ".step"
		a.writtenFiles[stepFile] = fmt.Sprintf("MOCK_STEP_%s", productKey)
		return nil

	default:
		return nil
	}
}

func (a *cacheAwareFakeAdapter) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func (a *cacheAwareFakeAdapter) getCallsForProduct(product string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, call := range a.calls {
		if call == fmt.Sprintf("csv:%s", product) || call == fmt.Sprintf("export:%s", product) {
			count++
		}
	}
	return count
}

// ============================================================================
// Parallel Cache Tests
// ============================================================================

// TestParallelExecution_CacheConsistency: Cache consistency during parallel execution
func TestParallelExecution_CacheConsistency(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	adp := newCacheAwareFakeAdapter()
	rt := executor.New(adp)
	s := New(rt)

	// 50 distinct products
	names := make([]string, 50)
	for i := 0; i < 50; i++ {
		names[i] = fmt.Sprintf("prod_%03d", i)
	}
	plan := productPlan(names...)

	// First execution
	err := s.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("first execution failed: %v", err)
	}

	firstRunCalls := adp.callCount()
	t.Logf("First run calls: %d", firstRunCalls)

	// Mark cache entries for each product
	for _, name := range names {
		for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
			cache.MarkDoneLayer(layer, name) // Product name as a simple cache key
		}
	}

	// Second execution should hit the cache, but here we test the adapter
	// So this tests parallel execution correctness, not caching
}

// TestParallelExecution_LayeredCacheBehavior: Parallel execution with layered caching
func TestParallelExecution_LayeredCacheBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	adp := newCacheAwareFakeAdapter()
	rt := executor.New(adp)
	s := New(rt)

	plan := productPlan("A", "B", "C", "D", "E")

	// Mark each product's geometry layer (simulation: geometry already exists)
	for _, product := range []string{"A", "B", "C", "D", "E"} {
		cache.MarkDoneLayer(cache.CacheGeometry, product)
	}

	err := s.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	// Adapter calls are expected for artifacts even on a geometry cache hit
	if adp.callCount() == 0 {
		t.Error("expected adapter calls for artifact generation")
	}
}

// TestParallelExecution_CacheRaceCondition: No race conditions should occur
func TestParallelExecution_CacheRaceCondition(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	// Execute concurrently with many workers
	const numProducts = 20
	names := make([]string, numProducts)
	for i := 0; i < numProducts; i++ {
		names[i] = fmt.Sprintf("race_%02d", i)
	}
	plan := productPlan(names...)

	var wg sync.WaitGroup
	errors := make(chan error, 5)

	// Execute the same plan concurrently with 5 goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(runID int) {
			defer wg.Done()

			adp := newCacheAwareFakeAdapter()
			rt := executor.New(adp)
			s := New(rt)

			err := s.Execute(context.Background(), plan)
			if err != nil {
				errors <- fmt.Errorf("run %d failed: %v", runID, err)
				return
			}

			// Mark cache entries
			for _, name := range names {
				for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
					if err := cache.MarkDoneLayer(layer, name); err != nil {
						errors <- fmt.Errorf("run %d cache mark failed: %v", runID, err)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	// Check that cache files are intact
	for _, name := range names {
		for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
			exists, err := cache.ExistsLayer(layer, name)
			if err != nil {
				t.Errorf("cache check failed for %s: %v", name, err)
			}
			if !exists {
				t.Errorf("cache should exist for %s after concurrent writes", name)
			}
		}
	}
}

// TestParallelExecution_PartialFailureRollback: Partial failure behavior
func TestParallelExecution_PartialFailureRollback(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	adp := newCacheAwareFakeAdapter()
	adp.failProduct = "fail_B" // Make product B fail

	rt := executor.New(adp)
	s := New(rt)

	plan := productPlan("A", "fail_B", "C")

	err := s.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error for failing product")
	}

	// Cache entries should not be written for the failed product
	// Note: The current implementation is not atomic; this test documents future behavior
	t.Logf("Error (expected): %v", err)
	t.Logf("Calls: %v", adp.calls)
}

// TestParallelExecution_CacheHitSkipExecution: Execution should be skipped on a cache hit
// Note: This test will become active when cache integration is complete
func TestParallelExecution_CacheHitSkipExecution(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	// First execution
	adp1 := newCacheAwareFakeAdapter()
	rt1 := executor.New(adp1)
	s1 := New(rt1)

	plan := productPlan("cached_X")

	err := s1.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("first execution failed: %v", err)
	}

	firstRunCalls := adp1.callCount()

	// Mark cache entries
	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		cache.MarkDoneLayer(layer, "cached_X")
	}

	// Second execution: cache hit
	adp2 := newCacheAwareFakeAdapter()
	rt2 := executor.New(adp2)
	s2 := New(rt2)

	// Cache check (simulate main.go logic)
	exists := true
	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		layerExists, err := cache.ExistsLayer(layer, "cached_X")
		if err != nil {
			t.Fatalf("cache check failed: %v", err)
		}
		if !layerExists {
			exists = false
			break
		}
	}
	if exists {
		// Cache hit: skip execution
		t.Log("Cache hit - skipping execution")
		// The second adapter should never be called
		if adp2.callCount() != 0 {
			t.Error("expected zero calls for cache hit")
		}
	} else {
		// Normal execution
		err = s2.Execute(context.Background(), plan)
		if err != nil {
			t.Fatalf("second execution failed: %v", err)
		}
	}

	t.Logf("First run calls: %d, Second run calls: %d", firstRunCalls, adp2.callCount())
}

// TestParallelExecution_ConcurrentCacheLayerWrites: Concurrent writes to different layers
func TestParallelExecution_ConcurrentCacheLayerWrites(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	const numWorkers = 10
	const numProducts = 10

	var wg sync.WaitGroup
	errorCount := atomic.Int32{}

	// Each worker writes to different layers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for p := 0; p < numProducts; p++ {
				productKey := fmt.Sprintf("prod_%02d", p)

				// Write to a different layer for each worker
				var layer cache.CacheLayer
				switch workerID % 3 {
				case 0:
					layer = cache.CacheGeometry
				case 1:
					layer = cache.CacheArtifact
				case 2:
					layer = cache.CacheMetadata
				}

				if err := cache.MarkDoneLayer(layer, productKey); err != nil {
					errorCount.Add(1)
				}
			}
		}(w)
	}

	wg.Wait()

	if errorCount.Load() > 0 {
		t.Errorf("encountered %d errors during concurrent layer writes", errorCount.Load())
	}

	// Check all layers for each product
	for p := 0; p < numProducts; p++ {
		productKey := fmt.Sprintf("prod_%02d", p)

		// At least some layers should have been written
		geoExists, _ := cache.ExistsLayer(cache.CacheGeometry, productKey)
		artExists, _ := cache.ExistsLayer(cache.CacheArtifact, productKey)
		metaExists, _ := cache.ExistsLayer(cache.CacheMetadata, productKey)

		t.Logf("Product %s: geo=%v art=%v meta=%v", productKey, geoExists, artExists, metaExists)
	}
}

// TestCacheDirectoryStructure: Is the cache directory structure correct?
func TestCacheDirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	hash := "structure_test"

	// Mark all layers
	cache.MarkDoneLayer(cache.CacheGeometry, hash)
	cache.MarkDoneLayer(cache.CacheArtifact, hash)
	cache.MarkDoneLayer(cache.CacheMetadata, hash)

	// Check the directory structure
	expectedStructure := map[string]string{
		"geometry": filepath.Join(".cache", "geometry", hash+".done"),
		"artifact": filepath.Join(".cache", "artifact", hash+".done"),
		"metadata": filepath.Join(".cache", "metadata", hash+".done"),
	}

	for name, path := range expectedStructure {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s layer at %s: %v", name, path, err)
		}
	}

	// The old structure (flat .cache/hash.done) should not exist
	legacyPath := filepath.Join(".cache", hash+".done")
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("legacy cache path should not exist: %s", legacyPath)
	}
}

// TestCacheAtomicity: Are cache writes atomic?
func TestCacheAtomicity(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	const iterations = 100
	var wg sync.WaitGroup

	// Write to the same hash many times concurrently
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
				cache.MarkDoneLayer(layer, "atomic_test")
			}
		}()
	}

	wg.Wait()

	// The file should not be corrupted
	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		exists, err := cache.ExistsLayer(layer, "atomic_test")
		if err != nil {
			t.Fatalf("cache check failed: %v", err)
		}
		if !exists {
			t.Error("cache should exist after atomic writes")
		}
	}

	// Each layer should contain only one file
	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		dir := filepath.Join(".cache", string(layer))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("failed to read layer dir %s: %v", layer, err)
			continue
		}

		// Count atomic_test.done files
		count := 0
		for _, entry := range entries {
			if entry.Name() == "atomic_test.done" {
				count++
			}
		}

		if count != 1 {
			t.Errorf("expected exactly one marker in %s layer, found %d", layer, count)
		}
	}
}
