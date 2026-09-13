package scheduler

import (
	"context"
	"fmt"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
)

func buildPlan(productCount int) *planner.ExecutionPlan {
	names := make([]string, 0, productCount)
	for i := 0; i < productCount; i++ {
		names = append(names, fmt.Sprintf("iso-%03d", i))
	}
	return productPlan(names...)
}

func TestScheduler_AllProductStepsExecuted(t *testing.T) {
	t.Parallel()

	plan := buildPlan(100)

	adp := &fakeAdapter{}
	s := New(executor.New(adp))

	if err := s.Execute(context.Background(), plan); err != nil {
		t.Fatal(err)
	}

	// 100 product * 2 step per product
	const expectedCalls = 200
	if got := adp.callCount(); got != expectedCalls {
		t.Fatalf("unexpected total calls, got=%d want=%d", got, expectedCalls)
	}
}
