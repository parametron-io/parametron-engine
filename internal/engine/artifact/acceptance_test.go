package artifact

import (
	"errors"
	"testing"
)

func TestAcceptForRegistration(t *testing.T) {
	t.Parallel()

	base := Artifact{
		Class:     ArtifactClassExecutionOutput,
		Type:      ArtifactTypeSTEP,
		JobID:     "job-1",
		ProductID: "widget",
		StepID:    "2",
		Filename:  "widget.step",
	}

	tests := []struct {
		name    string
		item    Artifact
		ctx     AcceptanceContext
		wantErr string
	}{
		{
			name: "execution output accepted on success",
			item: base,
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeSucceeded,
				VerificationOutcome: VerificationOutcomeUnknown,
			},
		},
		{
			name:    "execution output rejected on zero value context",
			item:    base,
			ctx:     AcceptanceContext{},
			wantErr: `artifact registration rejected for class "execution_output": final runtime outcome must be "succeeded"`,
		},
		{
			name: "execution output rejected on explicit unknown final outcome",
			item: base,
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeUnknown,
				VerificationOutcome: VerificationOutcomeUnknown,
			},
			wantErr: `artifact registration rejected for class "execution_output": final runtime outcome must be "succeeded"`,
		},
		{
			name: "execution output rejected on final outcome failed",
			item: base,
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeFailed,
				VerificationOutcome: VerificationOutcomePassed,
			},
			wantErr: `artifact registration rejected for class "execution_output": final runtime outcome must be "succeeded"`,
		},
		{
			name: "execution output rejected on final outcome canceled",
			item: base,
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeCanceled,
				VerificationOutcome: VerificationOutcomePassed,
			},
			wantErr: `artifact registration rejected for class "execution_output": final runtime outcome must be "succeeded"`,
		},
		{
			name: "execution output rejected on final outcome timed out",
			item: base,
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeTimedOut,
				VerificationOutcome: VerificationOutcomePassed,
			},
			wantErr: `artifact registration rejected for class "execution_output": final runtime outcome must be "succeeded"`,
		},
		{
			name: "verified artifact accepted on explicit verification pass",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeSucceeded,
				VerificationOutcome: VerificationOutcomePassed,
			},
		},
		{
			name: "verified artifact rejected on zero value context",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx:     AcceptanceContext{},
			wantErr: `artifact registration rejected for class "verified_artifact": final runtime outcome must be "succeeded"`,
		},
		{
			name: "verified artifact rejected on explicit unknown final outcome",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeUnknown,
				VerificationOutcome: VerificationOutcomePassed,
			},
			wantErr: `artifact registration rejected for class "verified_artifact": final runtime outcome must be "succeeded"`,
		},
		{
			name: "verified artifact rejected on verification failure",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeSucceeded,
				VerificationOutcome: VerificationOutcomeFailed,
			},
			wantErr: `artifact registration rejected for class "verified_artifact": explicit engine verification pass is required`,
		},
		{
			name: "verified artifact rejected when verification missing",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeSucceeded,
				VerificationOutcome: VerificationOutcomeUnknown,
			},
			wantErr: `artifact registration rejected for class "verified_artifact": explicit engine verification pass is required`,
		},
		{
			name: "verified artifact rejected when verification skipped",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeSucceeded,
				VerificationOutcome: VerificationOutcomeSkipped,
			},
			wantErr: `artifact registration rejected for class "verified_artifact": explicit engine verification pass is required`,
		},
		{
			name: "verified artifact rejected when verification not run",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeSucceeded,
				VerificationOutcome: VerificationOutcomeNotRun,
			},
			wantErr: `artifact registration rejected for class "verified_artifact": explicit engine verification pass is required`,
		},
		{
			name: "verified artifact rejected when final outcome failed",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeFailed,
				VerificationOutcome: VerificationOutcomePassed,
			},
			wantErr: `artifact registration rejected for class "verified_artifact": final runtime outcome must be "succeeded"`,
		},
		{
			name: "verified artifact rejected when final outcome canceled",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeCanceled,
				VerificationOutcome: VerificationOutcomePassed,
			},
			wantErr: `artifact registration rejected for class "verified_artifact": final runtime outcome must be "succeeded"`,
		},
		{
			name: "verified artifact rejected when final outcome timed out",
			item: func() Artifact {
				item := base
				item.Class = ArtifactClassVerified
				return item
			}(),
			ctx: AcceptanceContext{
				FinalOutcome:        FinalOutcomeTimedOut,
				VerificationOutcome: VerificationOutcomePassed,
			},
			wantErr: `artifact registration rejected for class "verified_artifact": final runtime outcome must be "succeeded"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AcceptForRegistration(tt.item, tt.ctx)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("AcceptForRegistration returned error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected AcceptForRegistration to fail")
			}
			if got := err.Error(); got != tt.wantErr {
				t.Fatalf("error = %q, want %q", got, tt.wantErr)
			}
			if !errors.Is(err, ErrArtifactAcceptanceRejected) {
				t.Fatalf("expected errors.Is(..., ErrArtifactAcceptanceRejected) to succeed, got %v", err)
			}
			var acceptanceErr *AcceptanceError
			if !errors.As(err, &acceptanceErr) {
				t.Fatalf("expected AcceptanceError, got %T", err)
			}
		})
	}
}

func TestAcceptForRegistration_RejectionIsDeterministic(t *testing.T) {
	t.Parallel()

	item := Artifact{
		Class:     ArtifactClassVerified,
		Type:      ArtifactTypeSTEP,
		JobID:     "job-1",
		ProductID: "widget",
		StepID:    "2",
		Filename:  "widget.step",
	}
	ctx := AcceptanceContext{
		FinalOutcome:        FinalOutcomeSucceeded,
		VerificationOutcome: VerificationOutcomeSkipped,
	}

	first := AcceptForRegistration(item, ctx)
	second := AcceptForRegistration(item, ctx)
	if first == nil || second == nil {
		t.Fatal("expected repeated AcceptForRegistration calls to fail")
	}
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic rejection messages, got %q and %q", first.Error(), second.Error())
	}
	if !errors.Is(first, ErrArtifactAcceptanceRejected) || !errors.Is(second, ErrArtifactAcceptanceRejected) {
		t.Fatalf("expected deterministic rejection classification, got %v and %v", first, second)
	}
}

func TestAcceptBatchForRegistration_FailsAtInvalidVerifiedArtifact(t *testing.T) {
	t.Parallel()

	items := []Artifact{
		{
			Class:     ArtifactClassExecutionOutput,
			Type:      ArtifactTypeCSV,
			ProductID: "widget",
			StepID:    "0",
			Filename:  "widget.csv",
		},
		{
			Class:     ArtifactClassVerified,
			Type:      ArtifactTypeSTEP,
			ProductID: "widget",
			StepID:    "2",
			Filename:  "widget.step",
		},
	}

	err := AcceptBatchForRegistration(items, AcceptanceContext{
		FinalOutcome:        FinalOutcomeSucceeded,
		VerificationOutcome: VerificationOutcomeNotRun,
	})
	if err == nil {
		t.Fatal("expected AcceptBatchForRegistration to fail")
	}

	var batchErr *BatchAcceptanceError
	if !errors.As(err, &batchErr) {
		t.Fatalf("expected BatchAcceptanceError, got %T", err)
	}
	if batchErr.Index != 1 {
		t.Fatalf("batch error index = %d, want 1", batchErr.Index)
	}
	if got, want := batchErr.Err.Error(), `artifact registration rejected for class "verified_artifact": explicit engine verification pass is required`; got != want {
		t.Fatalf("batch error = %q, want %q", got, want)
	}
}

func TestAcceptForRegistration_RejectsUnsupportedClassDeterministically(t *testing.T) {
	t.Parallel()

	item := Artifact{
		Class:     "mystery",
		Type:      ArtifactTypeSTEP,
		JobID:     "job-1",
		ProductID: "widget",
		StepID:    "2",
		Filename:  "widget.step",
	}

	first := AcceptForRegistration(item, AcceptanceContext{
		FinalOutcome:        FinalOutcomeSucceeded,
		VerificationOutcome: VerificationOutcomePassed,
	})
	second := AcceptForRegistration(item, AcceptanceContext{
		FinalOutcome:        FinalOutcomeSucceeded,
		VerificationOutcome: VerificationOutcomePassed,
	})

	if first == nil || second == nil {
		t.Fatal("expected repeated calls to reject unsupported class")
	}
	if got, want := first.Error(), `artifact registration rejected for class "mystery": artifact class is not supported`; got != want {
		t.Fatalf("first error = %q, want %q", got, want)
	}
	if second.Error() != first.Error() {
		t.Fatalf("error messages must be stable, got %q and %q", first.Error(), second.Error())
	}
	if !errors.Is(first, ErrArtifactAcceptanceRejected) || !errors.Is(second, ErrArtifactAcceptanceRejected) {
		t.Fatalf("expected errors.Is to match sentinel for both errors")
	}
}

func TestAcceptBatchForRegistration_RejectionIsByteStableAcrossRepeatedCalls(t *testing.T) {
	t.Parallel()

	items := []Artifact{
		{
			Class:     ArtifactClassExecutionOutput,
			Type:      ArtifactTypeCSV,
			ProductID: "widget",
			StepID:    "0",
			Filename:  "widget.csv",
		},
		{
			Class:     ArtifactClassVerified,
			Type:      ArtifactTypeSTEP,
			ProductID: "widget",
			StepID:    "2",
			Filename:  "widget.step",
		},
	}
	ctx := AcceptanceContext{
		FinalOutcome:        FinalOutcomeSucceeded,
		VerificationOutcome: VerificationOutcomeSkipped,
	}

	first := AcceptBatchForRegistration(items, ctx)
	second := AcceptBatchForRegistration(items, ctx)
	if first == nil || second == nil {
		t.Fatal("expected repeated batch acceptance calls to fail")
	}

	var firstBatchErr *BatchAcceptanceError
	if !errors.As(first, &firstBatchErr) {
		t.Fatalf("expected first error to be BatchAcceptanceError, got %T", first)
	}
	var secondBatchErr *BatchAcceptanceError
	if !errors.As(second, &secondBatchErr) {
		t.Fatalf("expected second error to be BatchAcceptanceError, got %T", second)
	}
	if firstBatchErr.Index != 1 || secondBatchErr.Index != 1 {
		t.Fatalf("batch error indices must be stable, got %d and %d", firstBatchErr.Index, secondBatchErr.Index)
	}
	if first.Error() != second.Error() {
		t.Fatalf("batch error messages must be byte-stable, got %q and %q", first.Error(), second.Error())
	}
}
