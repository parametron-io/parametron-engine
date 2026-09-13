package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const cacheDir = ".cache"

// CacheLayer represents a specific cache layer in the system.
type CacheLayer string

const (
	// CacheGeometry is the cache layer for geometry operations.
	CacheGeometry CacheLayer = "geometry"
	// CacheArtifact is the cache layer for artifact generation.
	CacheArtifact CacheLayer = "artifact"
	// CacheMetadata is the cache layer for metadata generation.
	CacheMetadata CacheLayer = "metadata"
)

// cacheEntry represents the metadata stored in a cache marker file.
type cacheEntry struct {
	Version   int       `json:"version"`
	Key       string    `json:"key"`
	Layer     string    `json:"layer"`
	Timestamp time.Time `json:"timestamp"`
}

// Cache stores plan execution markers on disk.
type Cache struct {
	mu       sync.RWMutex
	dir      string
	strategy CacheKeyStrategy
}

var defaultCache = &Cache{
	dir:      cacheDir,
	strategy: &DefaultCacheKeyStrategy{},
}

// ExistsLayer checks if a specific cache layer is marked done for the given key.
func ExistsLayer(layer CacheLayer, key string) (bool, error) {
	return defaultCache.ExistsLayer(layer, key)
}

// MarkDoneLayer marks a specific cache layer as done for the given key.
func MarkDoneLayer(layer CacheLayer, key string) error {
	return defaultCache.MarkDoneLayer(layer, key)
}

// ExistsLayer checks if a specific cache layer is marked done for the given key.
// It validates that the cache marker file contains valid metadata matching the
// expected key and layer, protecting against cache poisoning attacks.
// Invalid/corrupted files are removed during validation.
func (c *Cache) ExistsLayer(layer CacheLayer, key string) (bool, error) {
	// Use write lock because we may delete invalid cache files
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.existsLayerLocked(layer, key)
}

// existsLayerLocked checks if a layer exists (caller must hold lock).
func (c *Cache) existsLayerLocked(layer CacheLayer, key string) (bool, error) {
	path := c.layerMarkerPath(layer, key)

	// Read the marker file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	// Parse and validate the cache entry
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		// Corrupted or legacy (empty) file - remove and treat as miss
		_ = os.Remove(path)
		return false, nil
	}

	// Validate that the entry matches expected key and layer
	if entry.Key != key || entry.Layer != string(layer) {
		// Tampered file - remove and treat as miss
		_ = os.Remove(path)
		return false, nil
	}

	return true, nil
}

// MarkDoneLayer marks a specific cache layer as done for the given key.
func (c *Cache) MarkDoneLayer(layer CacheLayer, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.markDoneLayerLocked(layer, key)
}

// markDoneLayerLocked marks a layer as done (caller must hold lock).
// It writes JSON metadata to the marker file using atomic write (temp + rename)
// to prevent corruption during concurrent writes.
func (c *Cache) markDoneLayerLocked(layer CacheLayer, key string) error {
	layerDir := filepath.Join(c.dir, string(layer))
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache layer directory %s: %w", layer, err)
	}

	// Create cache entry with metadata
	entry := cacheEntry{
		Version:   1,
		Key:       key,
		Layer:     string(layer),
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %w", err)
	}

	// Atomic write: write to temp file, then rename to final path
	path := c.layerMarkerPath(layer, key)
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache marker temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize cache marker file: %w", err)
	}

	return nil
}

// layerMarkerPath returns the path for a layer marker file.
func (c *Cache) layerMarkerPath(layer CacheLayer, key string) string {
	return filepath.Join(c.dir, string(layer), key+".done")
}

// SetStrategy sets the cache key strategy for this cache instance.
// If strategy is nil, uses the global default strategy.
func (c *Cache) SetStrategy(strategy CacheKeyStrategy) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if strategy == nil {
		c.strategy = GetDefaultStrategy()
	} else {
		c.strategy = strategy
	}
}

// Strategy returns the current cache key strategy for this cache instance.
func (c *Cache) Strategy() CacheKeyStrategy {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.strategy == nil {
		// Note: We can't set the strategy while holding RLock.
		// This is intentional - if strategy is nil, return default without storing.
		// The next write operation (SetStrategy) will properly initialize it.
		return GetDefaultStrategy()
	}
	return c.strategy
}

// NewCacheWithStrategy creates a new Cache instance with a custom strategy.
// The directory is created if it doesn't exist.
func NewCacheWithStrategy(dir string, strategy CacheKeyStrategy) *Cache {
	if strategy == nil {
		strategy = GetDefaultStrategy()
	}
	return &Cache{
		dir:      dir,
		strategy: strategy,
	}
}

// NewCache creates a new Cache instance with the default strategy.
// The directory is created if it doesn't exist.
func NewCache(dir string) *Cache {
	return &Cache{
		dir:      dir,
		strategy: GetDefaultStrategy(),
	}
}

// ExistsWithContext checks if the given layer is cached using the provided context.
// The cache key is computed using the cache's strategy.
func (c *Cache) ExistsWithContext(layer CacheLayer, ctx KeyContext) (bool, error) {
	key := c.Strategy().ComputeKey(layer, ctx)
	return c.ExistsLayer(layer, key)
}

// MarkDoneWithContext marks the given layer as done using the provided context.
// The cache key is computed using the cache's strategy.
func (c *Cache) MarkDoneWithContext(layer CacheLayer, ctx KeyContext) error {
	key := c.Strategy().ComputeKey(layer, ctx)
	return c.MarkDoneLayer(layer, key)
}

// ExistsWithContext checks if the given layer is cached using the provided context.
// Uses the global default cache and strategy.
func ExistsWithContext(layer CacheLayer, ctx KeyContext) (bool, error) {
	return defaultCache.ExistsWithContext(layer, ctx)
}

// MarkDoneWithContext marks the given layer as done using the provided context.
// Uses the global default cache and strategy.
func MarkDoneWithContext(layer CacheLayer, ctx KeyContext) error {
	return defaultCache.MarkDoneWithContext(layer, ctx)
}
