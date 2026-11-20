package semantic

import (
	"errors"
	"testing"
)

func TestResolveSemanticTargetByExactName_ClassifiedErrors(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{
		{ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", DisplayName: "Pad"},
		{ID: "feat.padOther", ComponentID: "cmp.root", Name: "Pad", DisplayName: "Pad Other"},
	})

	t.Run("not found", func(t *testing.T) {
		_, err := ResolveSemanticTargetByExactName(model, "Missing")
		if !errors.Is(err, ErrSemanticTargetNotFound) {
			t.Fatalf("expected ErrSemanticTargetNotFound, got %v", err)
		}
		var typed *TargetResolutionError
		if !errors.As(err, &typed) {
			t.Fatalf("expected *TargetResolutionError, got %T", err)
		}
		if typed.Ambiguous {
			t.Fatalf("expected non-ambiguous typed error, got %#v", typed)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		_, err := ResolveSemanticTargetByExactName(model, "Pad")
		if !errors.Is(err, ErrSemanticTargetAmbiguous) {
			t.Fatalf("expected ErrSemanticTargetAmbiguous, got %v", err)
		}
		var typed *TargetResolutionError
		if !errors.As(err, &typed) {
			t.Fatalf("expected *TargetResolutionError, got %T", err)
		}
		if !typed.Ambiguous {
			t.Fatalf("expected ambiguous typed error, got %#v", typed)
		}
		if len(typed.Candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %#v", typed.Candidates)
		}
	})
}
