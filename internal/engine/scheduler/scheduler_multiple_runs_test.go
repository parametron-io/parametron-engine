package scheduler

import (
	"context"
	"testing"

	"parametron/internal/engine/executor"
)

func TestDeterminism_MultipleRuns(t *testing.T) {
	const products = 20
	const expectedCallsPerRun = products * 2

	for i := 0; i < 10; i++ {
		adp := &fakeAdapter{}
		s := New(executor.New(adp))

		err := s.Execute(context.Background(), buildPlan(products))
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}

		if got := adp.callCount(); got != expectedCallsPerRun {
			t.Fatalf("run %d: unexpected call count: got=%d want=%d", i, got, expectedCallsPerRun)
		}
	}
}
