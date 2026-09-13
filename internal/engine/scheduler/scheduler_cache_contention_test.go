//go:build stress
// +build stress

package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
)

func buildCacheHeavyPlan(productCount int) *planner.ExecutionPlan {
	names := make([]string, 0, productCount)
	for i := 0; i < productCount; i++ {
		names = append(names, fmt.Sprintf("cache-%03d", i))
	}
	return productPlan(names...)
}

func TestParallelExecution_WithCacheContention(t *testing.T) {
	t.Parallel()

	plan := buildCacheHeavyPlan(200)

	var mu sync.Mutex
	cache := map[string]int{}

	adp := &fakeAdapter{
		runFn: func(_ context.Context, _ int, step planner.Step) error {
			var key string
			switch payload := step.Payload.(type) {
			case planner.WriteCSVPayload:
				key = strings.TrimSuffix(payload.Filename, ".csv")
			case planner.WriteExportManifestPayload:
				key = strings.TrimSuffix(payload.ManifestFilename, ".json")
			default:
				key = "unknown"
			}

			// Simulate a hot shared cache with lock contention.
			mu.Lock()
			cache[key]++
			mu.Unlock()

			time.Sleep(1 * time.Millisecond)
			return nil
		},
	}

	s := New(executor.New(adp))
	if err := s.Execute(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each product key should be touched twice: WriteCSV + WriteExportManifest.
	mu.Lock()
	defer mu.Unlock()
	if len(cache) != 200 {
		t.Fatalf("unexpected cache key count, got=%d want=%d", len(cache), 200)
	}
	for k, v := range cache {
		if v != 2 {
			t.Fatalf("unexpected cache count for %s: got=%d want=2", k, v)
		}
	}
}
