package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCacheEntryValidation(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		content      string
		wantExists   bool
		wantFileGone bool
		layer        CacheLayer
	}{
		{
			name:         "valid cache entry",
			key:          "valid-key",
			content:      `{"version":1,"key":"valid-key","layer":"geometry"}`,
			wantExists:   true,
			wantFileGone: false,
			layer:        CacheGeometry,
		},
		{
			name:         "tampered key",
			key:          "valid-key",
			content:      `{"version":1,"key":"tampered-key","layer":"geometry"}`,
			wantExists:   false,
			wantFileGone: true,
			layer:        CacheGeometry,
		},
		{
			name:         "wrong layer",
			key:          "valid-key",
			content:      `{"version":1,"key":"valid-key","layer":"artifact"}`,
			wantExists:   false,
			wantFileGone: true,
			layer:        CacheGeometry,
		},
		{
			name:         "empty file (legacy)",
			key:          "valid-key",
			content:      "",
			wantExists:   false,
			wantFileGone: true,
			layer:        CacheGeometry,
		},
		{
			name:         "corrupted json",
			key:          "valid-key",
			content:      "not-json",
			wantExists:   false,
			wantFileGone: true,
			layer:        CacheGeometry,
		},
		{
			name:         "garbage content",
			key:          "valid-key",
			content:      "garbage data here",
			wantExists:   false,
			wantFileGone: true,
			layer:        CacheGeometry,
		},
		{
			name:         "valid artifact layer entry",
			key:          "artifact-key-123",
			content:      `{"version":1,"key":"artifact-key-123","layer":"artifact"}`,
			wantExists:   true,
			wantFileGone: false,
			layer:        CacheArtifact,
		},
		{
			name:         "valid metadata layer entry",
			key:          "metadata-key-456",
			content:      `{"version":1,"key":"metadata-key-456","layer":"metadata"}`,
			wantExists:   true,
			wantFileGone: false,
			layer:        CacheMetadata,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cache := NewCache(tmpDir)

			// Create the marker file with test content
			markerPath := filepath.Join(tmpDir, string(tt.layer), tt.key+".done")
			if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
				t.Fatalf("failed to create marker directory: %v", err)
			}

			if tt.content != "" {
				if err := os.WriteFile(markerPath, []byte(tt.content), 0644); err != nil {
					t.Fatalf("failed to write marker file: %v", err)
				}
			} else {
				// Create empty file for legacy test
				f, err := os.Create(markerPath)
				if err != nil {
					t.Fatalf("failed to create empty marker file: %v", err)
				}
				f.Close()
			}

			// Check if the cache entry exists
			exists, err := cache.ExistsLayer(tt.layer, tt.key)
			if err != nil {
				t.Fatalf("ExistsLayer returned error: %v", err)
			}
			if exists != tt.wantExists {
				t.Errorf("ExistsLayer() = %v, want %v", exists, tt.wantExists)
			}

			// Check if the file was removed (for invalid entries)
			_, statErr := os.Stat(markerPath)
			fileExists := !os.IsNotExist(statErr)

			if tt.wantFileGone && fileExists {
				t.Errorf("expected marker file to be removed, but it still exists")
			}
			if !tt.wantFileGone && !fileExists {
				t.Errorf("expected marker file to exist, but it was removed")
			}
		})
	}
}

func TestCachePoisoningAttack(t *testing.T) {
	// Simulate a cache poisoning attack where an attacker creates
	// a marker file with a fake hash
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	attackerKey := "attacker-controlled-hash"
	layer := CacheGeometry

	// Attacker creates a fake marker file
	markerPath := filepath.Join(tmpDir, string(layer), attackerKey+".done")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("failed to create marker directory: %v", err)
	}

	// Attacker writes garbage or tries to impersonate a valid entry
	fakeContent := `{"version":1,"key":"different-hash","layer":"geometry"}`
	if err := os.WriteFile(markerPath, []byte(fakeContent), 0644); err != nil {
		t.Fatalf("failed to write fake marker file: %v", err)
	}

	// System should detect the mismatch and treat as cache miss
	exists, err := cache.ExistsLayer(layer, attackerKey)
	if err != nil {
		t.Fatalf("ExistsLayer returned error: %v", err)
	}
	if exists {
		t.Error("cache poisoning attack succeeded - fake entry was accepted")
	}

	// The fake file should be removed
	_, statErr := os.Stat(markerPath)
	if !os.IsNotExist(statErr) {
		t.Error("fake marker file was not removed after detection")
	}
}

func TestMarkDoneLayer_CreatesValidEntry(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	key := "test-key-123"
	layer := CacheArtifact

	// Mark the layer as done
	if err := cache.MarkDoneLayer(layer, key); err != nil {
		t.Fatalf("MarkDoneLayer failed: %v", err)
	}

	// Verify the entry exists and is valid
	exists, err := cache.ExistsLayer(layer, key)
	if err != nil {
		t.Fatalf("ExistsLayer returned error: %v", err)
	}
	if !exists {
		t.Error("valid cache entry was not recognized")
	}

	// Verify the file contains valid JSON with expected fields
	markerPath := filepath.Join(tmpDir, string(layer), key+".done")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}

	content := string(data)
	if content == "" {
		t.Error("marker file is empty")
	}

	// Check for expected JSON fields
	expectedFields := []string{`"version"`, `"key"`, `"layer"`, `"timestamp"`}
	for _, field := range expectedFields {
		if !contains(content, field) {
			t.Errorf("marker file missing field %s", field)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestAtomicWrite(t *testing.T) {
	// Verify that MarkDoneLayer uses atomic write (temp + rename)
	// by checking that we never see a partially written file
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	key := "atomic-test-key"
	layer := CacheGeometry

	// The temp file should not exist after successful write
	markerPath := filepath.Join(tmpDir, string(layer), key+".done")
	tempPath := markerPath + ".tmp"

	if err := cache.MarkDoneLayer(layer, key); err != nil {
		t.Fatalf("MarkDoneLayer failed: %v", err)
	}

	// Temp file should not exist
	_, err := os.Stat(tempPath)
	if !os.IsNotExist(err) {
		t.Error("temporary marker file was not cleaned up")
	}

	// Final file should exist
	_, err = os.Stat(markerPath)
	if os.IsNotExist(err) {
		t.Error("final marker file does not exist")
	}
}

func TestLegacyCacheMigration(t *testing.T) {
	// Test that legacy (empty) marker files are treated as cache miss
	// and are cleaned up
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	key := "legacy-key"
	layer := CacheGeometry

	// Create a legacy empty marker file
	markerPath := filepath.Join(tmpDir, string(layer), key+".done")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("failed to create marker directory: %v", err)
	}

	f, err := os.Create(markerPath)
	if err != nil {
		t.Fatalf("failed to create legacy marker file: %v", err)
	}
	f.Close()

	// Should be treated as cache miss
	exists, err := cache.ExistsLayer(layer, key)
	if err != nil {
		t.Fatalf("ExistsLayer returned error: %v", err)
	}
	if exists {
		t.Error("legacy empty marker file was treated as valid cache hit")
	}

	// Legacy file should be removed
	_, statErr := os.Stat(markerPath)
	if !os.IsNotExist(statErr) {
		t.Error("legacy marker file was not cleaned up")
	}

	// Now write a proper entry
	if err := cache.MarkDoneLayer(layer, key); err != nil {
		t.Fatalf("MarkDoneLayer failed: %v", err)
	}

	// Should now be a valid cache hit
	exists, err = cache.ExistsLayer(layer, key)
	if err != nil {
		t.Fatalf("ExistsLayer returned error: %v", err)
	}
	if !exists {
		t.Error("new valid cache entry was not recognized after legacy cleanup")
	}
}

func TestConcurrentCacheSecurity(t *testing.T) {
	// Test concurrent access doesn't create security issues
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	const workers = 32
	key := "concurrent-security-test"
	layer := CacheGeometry

	// Create an invalid marker file
	markerPath := filepath.Join(tmpDir, string(layer), key+".done")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		t.Fatalf("failed to create marker directory: %v", err)
	}

	invalidContent := `{"version":1,"key":"wrong-key","layer":"geometry"}`
	if err := os.WriteFile(markerPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("failed to write invalid marker: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	// Concurrently check and mark as done
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			if idx%2 == 0 {
				if err := cache.MarkDoneLayer(layer, key); err != nil {
					errCh <- err
				}
				return
			}

			if _, err := cache.ExistsLayer(layer, key); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent operation failed: %v", err)
		}
	}

	// Final state should be valid
	exists, err := cache.ExistsLayer(layer, key)
	if err != nil {
		t.Fatalf("ExistsLayer returned error: %v", err)
	}
	if !exists {
		t.Error("final cache state is not valid after concurrent operations")
	}

	// Verify the file contains valid JSON
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Errorf("final marker file contains invalid JSON: %v", err)
	}

	if entry.Key != key {
		t.Errorf("marker file has wrong key: got %q, want %q", entry.Key, key)
	}
}
