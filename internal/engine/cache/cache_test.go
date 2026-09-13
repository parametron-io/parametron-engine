package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var allLayers = []CacheLayer{CacheGeometry, CacheArtifact, CacheMetadata}

func chdirToTemp(t *testing.T) string {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "cache_test_*")
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

func TestExistsNonExistingHash(t *testing.T) {
	chdirToTemp(t)
	hash := "does-not-exist"

	for _, layer := range allLayers {
		ok, err := ExistsLayer(layer, hash)
		if err != nil {
			t.Fatalf("ExistsLayer returned error: %v", err)
		}
		if ok {
			t.Fatal("expected non-existing hash to return false")
		}
	}
}

// Test that completing one layer leaves the others missing until written.
func TestLayerCompletion(t *testing.T) {
	chdirToTemp(t)
	hash := "partial-hash"

	// Mark only one layer as done
	if err := MarkDoneLayer(CacheGeometry, hash); err != nil {
		t.Fatalf("MarkDoneLayer(geometry) returned error: %v", err)
	}

	// Metadata should remain missing.
	ok, err := ExistsLayer(CacheMetadata, hash)
	if err != nil {
		t.Fatalf("ExistsLayer returned error: %v", err)
	}
	if ok {
		t.Fatal("expected metadata to remain missing with only geometry marked")
	}

	// Mark remaining layers
	if err := MarkDoneLayer(CacheArtifact, hash); err != nil {
		t.Fatalf("MarkDoneLayer(artifact) returned error: %v", err)
	}
	if err := MarkDoneLayer(CacheMetadata, hash); err != nil {
		t.Fatalf("MarkDoneLayer(metadata) returned error: %v", err)
	}

	// Now every layer should exist
	for _, layer := range allLayers {
		ok, err := ExistsLayer(layer, hash)
		if err != nil {
			t.Fatalf("ExistsLayer returned error: %v", err)
		}
		if !ok {
			t.Fatal("expected hash to exist after all layers marked")
		}
	}
}

// Test layer-specific ExistsLayer and MarkDoneLayer
func TestLayerSpecificOperations(t *testing.T) {
	chdirToTemp(t)
	hash := "layer-test"

	layers := []CacheLayer{CacheGeometry, CacheArtifact, CacheMetadata}

	for _, layer := range layers {
		// Initially should not exist
		ok, err := ExistsLayer(layer, hash)
		if err != nil {
			t.Fatalf("ExistsLayer(%s) returned error: %v", layer, err)
		}
		if ok {
			t.Fatalf("expected layer %s to not exist initially", layer)
		}

		// Mark layer as done
		if err := MarkDoneLayer(layer, hash); err != nil {
			t.Fatalf("MarkDoneLayer(%s) returned error: %v", layer, err)
		}

		// Verify layer file exists at correct path
		expectedPath := filepath.Join(cacheDir, string(layer), hash+".done")
		if _, err := os.Stat(expectedPath); err != nil {
			t.Fatalf("expected layer file at %s: %v", expectedPath, err)
		}

		// Now should exist
		ok, err = ExistsLayer(layer, hash)
		if err != nil {
			t.Fatalf("ExistsLayer(%s) returned error: %v", layer, err)
		}
		if !ok {
			t.Fatalf("expected layer %s to exist after MarkDoneLayer", layer)
		}
	}
}

// Test layer-specific marker paths
func TestLayerSpecificMarkerPaths(t *testing.T) {
	chdirToTemp(t)
	hash := "path-test"

	// Create all markers
	for _, layer := range allLayers {
		if err := MarkDoneLayer(layer, hash); err != nil {
			t.Fatalf("MarkDoneLayer returned error: %v", err)
		}
	}

	// Verify correct directory structure: .cache/<layer>/<hash>.done
	expectedPaths := map[CacheLayer]string{
		CacheGeometry: filepath.Join(cacheDir, "geometry", hash+".done"),
		CacheArtifact: filepath.Join(cacheDir, "artifact", hash+".done"),
		CacheMetadata: filepath.Join(cacheDir, "metadata", hash+".done"),
	}

	for layer, expectedPath := range expectedPaths {
		if _, err := os.Stat(expectedPath); err != nil {
			t.Fatalf("expected %s layer file at %s: %v", layer, expectedPath, err)
		}
	}

	// Verify no files at legacy path
	legacyPath := filepath.Join(cacheDir, hash+".done")
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("expected no file at legacy path %s", legacyPath)
	}
}

func TestDeterministicFilenameForSameHash(t *testing.T) {
	chdirToTemp(t)
	hash := "samehash"
	expectedPaths := []string{
		filepath.Join(cacheDir, "geometry", hash+".done"),
		filepath.Join(cacheDir, "artifact", hash+".done"),
		filepath.Join(cacheDir, "metadata", hash+".done"),
	}

	for _, layer := range allLayers {
		if err := MarkDoneLayer(layer, hash); err != nil {
			t.Fatalf("first MarkDoneLayer returned error: %v", err)
		}
	}
	for _, path := range expectedPaths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected cache file at %s: %v", path, err)
		}
	}

	for _, layer := range allLayers {
		if err := MarkDoneLayer(layer, hash); err != nil {
			t.Fatalf("second MarkDoneLayer returned error: %v", err)
		}
	}

	// Verify only 3 layer directories exist
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("failed to read cache directory: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected exactly three layer directories, got %d", len(entries))
	}

	// Verify each layer directory has exactly one file
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("expected directory in cache root, got file: %s", entry.Name())
		}
		layerEntries, err := os.ReadDir(filepath.Join(cacheDir, entry.Name()))
		if err != nil {
			t.Fatalf("failed to read layer directory %s: %v", entry.Name(), err)
		}
		if len(layerEntries) != 1 {
			t.Fatalf("expected exactly one file in layer %s, got %d", entry.Name(), len(layerEntries))
		}
		if layerEntries[0].Name() != hash+".done" {
			t.Fatalf("expected cache file name %s.done, got %s", hash, layerEntries[0].Name())
		}
	}
}

func TestConcurrentLayerReadsAndWrites_SameHash(t *testing.T) {
	chdirToTemp(t)
	hash := "concurrent-hash"
	const workers = 64

	var wg sync.WaitGroup
	errCh := make(chan error, workers*len(allLayers))

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			if i%2 == 0 {
				for _, layer := range allLayers {
					if err := MarkDoneLayer(layer, hash); err != nil {
						errCh <- err
					}
				}
				return
			}

			for _, layer := range allLayers {
				if _, err := ExistsLayer(layer, hash); err != nil {
					errCh <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent cache access returned error: %v", err)
		}
	}

	for _, layer := range allLayers {
		if err := MarkDoneLayer(layer, hash); err != nil {
			t.Fatalf("final MarkDoneLayer returned error: %v", err)
		}
	}

	for _, layer := range allLayers {
		ok, err := ExistsLayer(layer, hash)
		if err != nil {
			t.Fatalf("final ExistsLayer returned error: %v", err)
		}
		if !ok {
			t.Fatal("expected hash to exist in cache")
		}
	}

	// Verify each layer has exactly one file
	for _, layer := range allLayers {
		entries, err := os.ReadDir(filepath.Join(cacheDir, string(layer)))
		if err != nil {
			t.Fatalf("failed to read layer directory %s: %v", layer, err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected exactly one file in layer %s, got %d", layer, len(entries))
		}
		if entries[0].Name() != hash+".done" {
			t.Fatalf("expected cache file name %s.done, got %s", hash, entries[0].Name())
		}
	}
}

// Test concurrent access to layer-specific operations
func TestConcurrentLayerSpecificOperations(t *testing.T) {
	chdirToTemp(t)
	const workers = 32

	var wg sync.WaitGroup
	errCh := make(chan error, workers*3)

	// Test concurrent operations on different layers
	layers := []CacheLayer{CacheGeometry, CacheArtifact, CacheMetadata}

	for _, layer := range layers {
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(l CacheLayer, idx int) {
				defer wg.Done()

				hash := "concurrent-layer-test"
				if idx%2 == 0 {
					if err := MarkDoneLayer(l, hash); err != nil {
						errCh <- err
					}
					return
				}

				if _, err := ExistsLayer(l, hash); err != nil {
					errCh <- err
				}
			}(layer, i)
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent layer cache access returned error: %v", err)
		}
	}

	// Verify each layer has the expected file
	for _, layer := range layers {
		expectedPath := filepath.Join(cacheDir, string(layer), "concurrent-layer-test.done")
		if _, err := os.Stat(expectedPath); err != nil {
			t.Fatalf("expected layer file at %s: %v", expectedPath, err)
		}
	}
}
