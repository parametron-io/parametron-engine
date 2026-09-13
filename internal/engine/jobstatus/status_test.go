package jobstatus

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/job"
)

func TestNewQueuedStatusFromJob(t *testing.T) {
	j := testJob(t, "widget")
	now := time.Date(2026, 3, 16, 12, 30, 0, 0, time.FixedZone("UTC+3", 3*60*60))

	status, err := New(j, now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if status.SchemaVersion != SchemaVersion {
		t.Fatalf("schemaVersion = %q", status.SchemaVersion)
	}
	if status.JobID != j.ID {
		t.Fatalf("jobId = %q", status.JobID)
	}
	if status.ProductKey != j.ProductKey {
		t.Fatalf("productKey = %q", status.ProductKey)
	}
	if status.State != StateQueued {
		t.Fatalf("state = %q", status.State)
	}
	if !status.CreatedAt.Equal(now.UTC()) {
		t.Fatalf("createdAt = %v, want %v", status.CreatedAt, now.UTC())
	}
	if !status.UpdatedAt.Equal(now.UTC()) {
		t.Fatalf("updatedAt = %v, want %v", status.UpdatedAt, now.UTC())
	}
	if status.StartedAt != nil {
		t.Fatal("startedAt should be nil")
	}
	if status.EndedAt != nil {
		t.Fatal("endedAt should be nil")
	}
	if status.Error != nil {
		t.Fatal("error should be nil")
	}
}

func TestNewRejectsInvalidJob(t *testing.T) {
	now := time.Now()

	if _, err := New(nil, now); !errors.Is(err, ErrNilJob) {
		t.Fatalf("expected ErrNilJob, got %v", err)
	}

	if _, err := New(&job.Job{ProductKey: "widget"}, now); !errors.Is(err, ErrMissingJobID) {
		t.Fatalf("expected ErrMissingJobID, got %v", err)
	}

	if _, err := New(&job.Job{ID: "job-1"}, now); !errors.Is(err, ErrMissingProductKey) {
		t.Fatalf("expected ErrMissingProductKey, got %v", err)
	}
}

func TestAllowedTransitions(t *testing.T) {
	base := time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC)

	t.Run("queued to running", func(t *testing.T) {
		status := newStatus(t, base)

		if err := status.Start(base.Add(time.Minute)); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if status.State != StateRunning {
			t.Fatalf("state = %q", status.State)
		}
		if status.StartedAt == nil || !status.StartedAt.Equal(base.Add(time.Minute)) {
			t.Fatalf("startedAt = %v", status.StartedAt)
		}
		if !status.UpdatedAt.Equal(base.Add(time.Minute)) {
			t.Fatalf("updatedAt = %v", status.UpdatedAt)
		}
	})

	t.Run("queued to canceled", func(t *testing.T) {
		status := newStatus(t, base)

		if err := status.Cancel(base.Add(time.Minute), nil); err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		assertTerminalCanceled(t, status, base.Add(time.Minute))
		if status.StartedAt != nil {
			t.Fatalf("startedAt = %v, want nil", status.StartedAt)
		}
	})

	t.Run("running to succeeded", func(t *testing.T) {
		status := startedStatus(t, base)

		if err := status.Succeed(base.Add(2 * time.Minute)); err != nil {
			t.Fatalf("Succeed() error = %v", err)
		}
		if status.State != StateSucceeded {
			t.Fatalf("state = %q", status.State)
		}
		if status.EndedAt == nil || !status.EndedAt.Equal(base.Add(2*time.Minute)) {
			t.Fatalf("endedAt = %v", status.EndedAt)
		}
		if status.Error != nil {
			t.Fatalf("error = %#v, want nil", status.Error)
		}
	})

	t.Run("running to failed", func(t *testing.T) {
		status := startedStatus(t, base)
		execErr := &executor.ExecutionError{
			ProductID:  "widget",
			StepID:     "2",
			Err:        errors.New("adapter exploded"),
			RetryCount: 3,
		}

		if err := status.Fail(base.Add(2*time.Minute), execErr); err != nil {
			t.Fatalf("Fail() error = %v", err)
		}
		if status.State != StateFailed {
			t.Fatalf("state = %q", status.State)
		}
		if status.EndedAt == nil || !status.EndedAt.Equal(base.Add(2*time.Minute)) {
			t.Fatalf("endedAt = %v", status.EndedAt)
		}
		if status.Error == nil || status.Error.Message != "adapter exploded" {
			t.Fatalf("error = %#v", status.Error)
		}
	})

	t.Run("running to canceled", func(t *testing.T) {
		status := startedStatus(t, base)

		if err := status.Cancel(base.Add(2*time.Minute), errors.New("manual stop")); err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		assertTerminalCanceled(t, status, base.Add(2*time.Minute))
		if status.StartedAt == nil || !status.StartedAt.Equal(base.Add(time.Minute)) {
			t.Fatalf("startedAt = %v", status.StartedAt)
		}
	})
}

func TestRejectedTransitions(t *testing.T) {
	base := time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		make func(*testing.T) *Status
		act  func(*Status) error
		from State
		to   State
	}{
		{
			name: "queued to succeeded",
			make: func(t *testing.T) *Status { return newStatus(t, base) },
			act:  func(s *Status) error { return s.Succeed(base.Add(time.Minute)) },
			from: StateQueued,
			to:   StateSucceeded,
		},
		{
			name: "queued to failed",
			make: func(t *testing.T) *Status { return newStatus(t, base) },
			act:  func(s *Status) error { return s.Fail(base.Add(time.Minute), errors.New("boom")) },
			from: StateQueued,
			to:   StateFailed,
		},
		{
			name: "running to queued",
			make: func(t *testing.T) *Status { return startedStatus(t, base) },
			act:  func(s *Status) error { return s.transition(StateQueued, base.Add(2*time.Minute), nil) },
			from: StateRunning,
			to:   StateQueued,
		},
		{
			name: "succeeded terminal",
			make: func(t *testing.T) *Status {
				s := startedStatus(t, base)
				if err := s.Succeed(base.Add(2 * time.Minute)); err != nil {
					t.Fatalf("Succeed() error = %v", err)
				}
				return s
			},
			act:  func(s *Status) error { return s.Cancel(base.Add(3*time.Minute), nil) },
			from: StateSucceeded,
			to:   StateCanceled,
		},
		{
			name: "failed terminal",
			make: func(t *testing.T) *Status {
				s := startedStatus(t, base)
				if err := s.Fail(base.Add(2*time.Minute), errors.New("boom")); err != nil {
					t.Fatalf("Fail() error = %v", err)
				}
				return s
			},
			act:  func(s *Status) error { return s.Succeed(base.Add(3 * time.Minute)) },
			from: StateFailed,
			to:   StateSucceeded,
		},
		{
			name: "canceled terminal",
			make: func(t *testing.T) *Status {
				s := newStatus(t, base)
				if err := s.Cancel(base.Add(time.Minute), nil); err != nil {
					t.Fatalf("Cancel() error = %v", err)
				}
				return s
			},
			act:  func(s *Status) error { return s.Start(base.Add(2 * time.Minute)) },
			from: StateCanceled,
			to:   StateRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := tt.make(t)
			err := tt.act(status)
			var transitionErr *InvalidTransitionError
			if !errors.As(err, &transitionErr) {
				t.Fatalf("expected InvalidTransitionError, got %v", err)
			}
			if transitionErr.From != tt.from || transitionErr.To != tt.to {
				t.Fatalf("transition = %q -> %q, want %q -> %q", transitionErr.From, transitionErr.To, tt.from, tt.to)
			}
		})
	}
}

func TestStart_CannotStartTwice(t *testing.T) {
	base := time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC)
	status := newStatus(t, base)

	if err := status.Start(base.Add(time.Minute)); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}

	before := cloneStatus(t, status)
	err := status.Start(base.Add(2 * time.Minute))
	var transitionErr *InvalidTransitionError
	if !errors.As(err, &transitionErr) {
		t.Fatalf("expected InvalidTransitionError, got %v", err)
	}
	if transitionErr.From != StateRunning || transitionErr.To != StateRunning {
		t.Fatalf("transition = %q -> %q, want %q -> %q", transitionErr.From, transitionErr.To, StateRunning, StateRunning)
	}
	if !reflect.DeepEqual(*status, before) {
		t.Fatalf("status mutated after failed Start(): %#v", status)
	}
}

func TestSucceed_CannotSucceedTwice(t *testing.T) {
	base := time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC)
	status := startedStatus(t, base)

	if err := status.Succeed(base.Add(2 * time.Minute)); err != nil {
		t.Fatalf("first Succeed() error = %v", err)
	}

	before := cloneStatus(t, status)
	err := status.Succeed(base.Add(3 * time.Minute))
	var transitionErr *InvalidTransitionError
	if !errors.As(err, &transitionErr) {
		t.Fatalf("expected InvalidTransitionError, got %v", err)
	}
	if transitionErr.From != StateSucceeded || transitionErr.To != StateSucceeded {
		t.Fatalf("transition = %q -> %q, want %q -> %q", transitionErr.From, transitionErr.To, StateSucceeded, StateSucceeded)
	}
	if !reflect.DeepEqual(*status, before) {
		t.Fatalf("status mutated after failed Succeed(): %#v", status)
	}
}

func TestTerminalState_InvalidTransitionDoesNotMutateStatus(t *testing.T) {
	base := time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC)
	status := startedStatus(t, base)

	if err := status.Succeed(base.Add(2 * time.Minute)); err != nil {
		t.Fatalf("Succeed() error = %v", err)
	}

	before := cloneStatus(t, status)
	err := status.Cancel(base.Add(3*time.Minute), errors.New("too late"))
	var transitionErr *InvalidTransitionError
	if !errors.As(err, &transitionErr) {
		t.Fatalf("expected InvalidTransitionError, got %v", err)
	}
	if transitionErr.From != StateSucceeded || transitionErr.To != StateCanceled {
		t.Fatalf("transition = %q -> %q, want %q -> %q", transitionErr.From, transitionErr.To, StateSucceeded, StateCanceled)
	}
	if !reflect.DeepEqual(*status, before) {
		t.Fatalf("status mutated after failed terminal transition:\nbefore=%#v\nafter=%#v", before, *status)
	}
	if status.State != StateSucceeded {
		t.Fatalf("state = %q, want %q", status.State, StateSucceeded)
	}
	if !status.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("updatedAt = %v, want %v", status.UpdatedAt, before.UpdatedAt)
	}
	if status.EndedAt == nil || before.EndedAt == nil || !status.EndedAt.Equal(*before.EndedAt) {
		t.Fatalf("endedAt = %v, want %v", status.EndedAt, before.EndedAt)
	}
	if status.Error != nil {
		t.Fatalf("error = %#v, want nil", status.Error)
	}
}

func TestTimestampSemanticsAndUTCSerialization(t *testing.T) {
	createdAt := time.Date(2026, 3, 16, 11, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	startedAt := createdAt.Add(2 * time.Minute)
	endedAt := createdAt.Add(5 * time.Minute)
	status := newStatus(t, createdAt)

	if !status.CreatedAt.Equal(createdAt.UTC()) || status.CreatedAt.Location() != time.UTC {
		t.Fatalf("createdAt = %v", status.CreatedAt)
	}
	if !status.UpdatedAt.Equal(createdAt.UTC()) || status.UpdatedAt.Location() != time.UTC {
		t.Fatalf("updatedAt = %v", status.UpdatedAt)
	}

	if err := status.Start(startedAt); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if status.StartedAt == nil || status.StartedAt.Location() != time.UTC {
		t.Fatalf("startedAt = %v", status.StartedAt)
	}
	if !status.UpdatedAt.Equal(startedAt.UTC()) {
		t.Fatalf("updatedAt = %v", status.UpdatedAt)
	}

	if err := status.Succeed(endedAt); err != nil {
		t.Fatalf("Succeed() error = %v", err)
	}
	if status.EndedAt == nil || status.EndedAt.Location() != time.UTC {
		t.Fatalf("endedAt = %v", status.EndedAt)
	}
	if !status.UpdatedAt.Equal(endedAt.UTC()) {
		t.Fatalf("updatedAt = %v", status.UpdatedAt)
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	jsonText := string(data)
	if strings.Contains(jsonText, "+03:00") {
		t.Fatalf("expected UTC timestamps, got %s", jsonText)
	}
	if !strings.Contains(jsonText, "\"createdAt\":\"2026-03-16T08:00:00Z\"") {
		t.Fatalf("expected UTC createdAt, got %s", jsonText)
	}
}

func TestFailureRepresentation(t *testing.T) {
	t.Run("execution error preserves structured fields", func(t *testing.T) {
		status := startedStatus(t, time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC))
		execErr := &executor.ExecutionError{
			ProductID:  "widget",
			StepID:     "7",
			Err:        errors.New("runner timeout"),
			RetryCount: 2,
			Timeout:    true,
		}

		if err := status.Fail(time.Date(2026, 3, 16, 9, 5, 0, 0, time.UTC), execErr); err != nil {
			t.Fatalf("Fail() error = %v", err)
		}

		if status.Error == nil {
			t.Fatal("error should be set")
		}
		if status.Error.Message != "runner timeout" {
			t.Fatalf("message = %q", status.Error.Message)
		}
		if status.Error.ProductID != "widget" || status.Error.StepID != "7" {
			t.Fatalf("error = %#v", status.Error)
		}
		if status.Error.RetryCount != 2 || !status.Error.Timeout || status.Error.Canceled {
			t.Fatalf("error = %#v", status.Error)
		}
	})

	t.Run("generic fail error builds minimal failure", func(t *testing.T) {
		status := startedStatus(t, time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC))

		if err := status.Fail(time.Date(2026, 3, 16, 9, 2, 0, 0, time.UTC), errors.New("boom")); err != nil {
			t.Fatalf("Fail() error = %v", err)
		}

		if status.Error == nil {
			t.Fatal("error should be set")
		}
		if status.Error.Message != "boom" || status.Error.ProductID != status.ProductKey {
			t.Fatalf("error = %#v", status.Error)
		}
		if status.Error.StepID != "" || status.Error.RetryCount != 0 || status.Error.Timeout || status.Error.Canceled {
			t.Fatalf("error = %#v", status.Error)
		}
	})

	t.Run("cancel preserves canceled and timeout flags", func(t *testing.T) {
		status := startedStatus(t, time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC))
		execErr := &executor.ExecutionError{
			ProductID:  "widget",
			StepID:     "4",
			Err:        errors.New("step canceled after timeout"),
			RetryCount: 1,
			Timeout:    true,
		}

		if err := status.Cancel(time.Date(2026, 3, 16, 9, 3, 0, 0, time.UTC), execErr); err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}

		if status.Error == nil || !status.Error.Canceled || !status.Error.Timeout {
			t.Fatalf("error = %#v", status.Error)
		}
	})

	t.Run("success omits error", func(t *testing.T) {
		status := startedStatus(t, time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC))
		if err := status.Succeed(time.Date(2026, 3, 16, 9, 1, 0, 0, time.UTC)); err != nil {
			t.Fatalf("Succeed() error = %v", err)
		}
		if status.Error != nil {
			t.Fatalf("error = %#v, want nil", status.Error)
		}
	})
}

func TestJSONDeterminismAndOmitEmpty(t *testing.T) {
	now := time.Date(2026, 3, 16, 8, 0, 0, 0, time.UTC)
	status1 := newStatus(t, now)
	status2 := newStatus(t, now)

	data1, err := json.Marshal(status1)
	if err != nil {
		t.Fatalf("json.Marshal(status1) error = %v", err)
	}
	data2, err := json.Marshal(status2)
	if err != nil {
		t.Fatalf("json.Marshal(status2) error = %v", err)
	}
	if string(data1) != string(data2) {
		t.Fatalf("marshal bytes differ:\n%s\n%s", data1, data2)
	}
	text := string(data1)
	if strings.Contains(text, "startedAt") || strings.Contains(text, "endedAt") || strings.Contains(text, "\"error\"") {
		t.Fatalf("expected omitempty fields to be absent, got %s", text)
	}

	if err := status1.Start(now.Add(time.Minute)); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	data3, err := json.Marshal(status1)
	if err != nil {
		t.Fatalf("json.Marshal(status1 started) error = %v", err)
	}
	if !strings.Contains(string(data3), "\"startedAt\"") || strings.Contains(string(data3), "\"endedAt\"") {
		t.Fatalf("unexpected started-state json %s", data3)
	}
}

func TestJSON_RoundTripDeterministic(t *testing.T) {
	base := time.Date(2026, 3, 16, 8, 0, 0, 0, time.UTC)
	status := startedStatus(t, base)
	execErr := &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "4",
		Err:        errors.New("runner timed out"),
		RetryCount: 1,
		Timeout:    true,
	}
	if err := status.Fail(base.Add(2*time.Minute), execErr); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	first, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal(first) error = %v", err)
	}

	var roundTripped Status
	if err := json.Unmarshal(first, &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	second, err := json.Marshal(&roundTripped)
	if err != nil {
		t.Fatalf("json.Marshal(second) error = %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("round-trip bytes differ:\nfirst=%s\nsecond=%s", first, second)
	}
	if roundTripped.StartedAt == nil || roundTripped.EndedAt == nil || roundTripped.Error == nil {
		t.Fatalf("round-tripped optional fields missing: %#v", roundTripped)
	}
}

func TestSchemaContractAndFieldNames(t *testing.T) {
	status := newStatus(t, time.Date(2026, 3, 16, 8, 0, 0, 0, time.UTC))

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	expected := `{"schemaVersion":"1.0","jobId":"` + status.JobID + `","productKey":"widget","state":"queued","createdAt":"2026-03-16T08:00:00Z","updatedAt":"2026-03-16T08:00:00Z"}`
	if string(data) != expected {
		t.Fatalf("json = %s, want %s", data, expected)
	}
}

func TestTerminalStateInvariants(t *testing.T) {
	base := time.Date(2026, 3, 16, 8, 0, 0, 0, time.UTC)

	success := startedStatus(t, base)
	if err := success.Succeed(base.Add(2 * time.Minute)); err != nil {
		t.Fatalf("Succeed() error = %v", err)
	}
	if success.EndedAt == nil || success.Error != nil {
		t.Fatalf("success = %#v", success)
	}

	failed := startedStatus(t, base)
	if err := failed.Fail(base.Add(2*time.Minute), errors.New("boom")); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if failed.EndedAt == nil || failed.Error == nil {
		t.Fatalf("failed = %#v", failed)
	}

	canceled := newStatus(t, base)
	if err := canceled.Cancel(base.Add(time.Minute), nil); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceled.EndedAt == nil || canceled.Error == nil || !canceled.Error.Canceled {
		t.Fatalf("canceled = %#v", canceled)
	}
}

func newStatus(t *testing.T, now time.Time) *Status {
	t.Helper()

	status, err := New(testJob(t, "widget"), now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return status
}

func startedStatus(t *testing.T, createdAt time.Time) *Status {
	t.Helper()

	status := newStatus(t, createdAt)
	if err := status.Start(createdAt.Add(time.Minute)); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return status
}

func assertTerminalCanceled(t *testing.T, status *Status, endedAt time.Time) {
	t.Helper()

	if status.State != StateCanceled {
		t.Fatalf("state = %q", status.State)
	}
	if status.EndedAt == nil || !status.EndedAt.Equal(endedAt.UTC()) {
		t.Fatalf("endedAt = %v", status.EndedAt)
	}
	if status.Error == nil || !status.Error.Canceled {
		t.Fatalf("error = %#v", status.Error)
	}
	if !status.UpdatedAt.Equal(endedAt.UTC()) {
		t.Fatalf("updatedAt = %v", status.UpdatedAt)
	}
}

func testJob(t *testing.T, productKey string) *job.Job {
	t.Helper()

	j, err := job.New(productKey, []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: productKey,
				Filename:   productKey + ".csv",
			},
		},
	})
	if err != nil {
		t.Fatalf("job.New() error = %v", err)
	}
	return j
}

func cloneStatus(t *testing.T, status *Status) Status {
	t.Helper()

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var cloned Status
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	return cloned
}
