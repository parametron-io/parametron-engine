//go:build race
// +build race

package cache

import (
	"fmt"
	"sync"
	"testing"
)

// TestStrategy_ConcurrentKeyComputation tests concurrent key generation under race detector.
func TestStrategy_ConcurrentKeyComputation(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}
	ctx := NewKeyContext("plan-abc123").
		WithProductID("widget").
		WithStepID("step-1").
		Build()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(layer CacheLayer) {
			defer wg.Done()
			key := strategy.ComputeKey(layer, ctx)
			if key == "" {
				errors <- fmt.Errorf("empty key generated")
			}
		}(CacheLayer(fmt.Sprintf("layer-%d", i%3)))
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestStrategy_ConcurrentContextBuilding tests building contexts concurrently.
func TestStrategy_ConcurrentContextBuilding(t *testing.T) {
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := NewKeyContext(fmt.Sprintf("plan-%d", idx)).
				WithProductID(fmt.Sprintf("product-%d", idx)).
				WithStepID(fmt.Sprintf("step-%d", idx)).
				Build()
			if ctx.PlanHash == "" {
				errors <- fmt.Errorf("empty plan hash")
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestStrategy_ConcurrentGlobalStrategyAccess tests concurrent access to global strategy.
func TestStrategy_ConcurrentGlobalStrategyAccess(t *testing.T) {
	// Save and restore original
	original := GetDefaultStrategy()
	defer SetDefaultStrategy(original)

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Half writers, half readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			SetDefaultStrategy(&DefaultCacheKeyStrategy{})
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = GetDefaultStrategy()
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestCache_ConcurrentStrategyAccess tests concurrent cache operations with strategy.
func TestCache_ConcurrentStrategyAccess(t *testing.T) {
	chdirToTemp(t)

	cache := NewCache(cacheDir)
	ctx := NewKeyContext("race-test").Build()

	var wg sync.WaitGroup

	// Concurrent reads of strategy
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cache.Strategy()
		}()
	}

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cache.MarkDoneWithContext(CacheGeometry, ctx)
		}()
	}

	wg.Wait()
}

// TestCache_ConcurrentExistsAndMarkDoneWithContext tests concurrent cache checks and writes.
func TestCache_ConcurrentExistsAndMarkDoneWithContext(t *testing.T) {
	chdirToTemp(t)

	cache := NewCache(cacheDir)
	ctx := NewKeyContext("concurrent-test").Build()

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_, _ = cache.ExistsWithContext(CacheGeometry, ctx)
			} else {
				_ = cache.MarkDoneWithContext(CacheGeometry, ctx)
			}
		}(i)
	}

	wg.Wait()
}
