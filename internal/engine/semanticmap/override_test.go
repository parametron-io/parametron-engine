package semanticmap

import (
	"errors"
	"reflect"
	"testing"
)

func TestOverridePolicyHelpersAreNilSafe(t *testing.T) {
	var policy *OverridePolicy

	if policy.OverridesAllowed() {
		t.Fatal("expected OverridesAllowed to be false")
	}
	if policy.AllowsAdapterOverrides() {
		t.Fatal("expected AllowsAdapterOverrides to be false")
	}
	if policy.AllowsProjectOverrides() {
		t.Fatal("expected AllowsProjectOverrides to be false")
	}
	if policy.AllowsUserOverrides() {
		t.Fatal("expected AllowsUserOverrides to be false")
	}
}

func TestOverridePolicyHelpersReportDisabledV1Policy(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	if contract.OverridePolicy.OverridesAllowed() {
		t.Fatal("expected OverridesAllowed to be false")
	}
	if contract.OverridePolicy.AllowsAdapterOverrides() {
		t.Fatal("expected AllowsAdapterOverrides to be false")
	}
	if contract.OverridePolicy.AllowsProjectOverrides() {
		t.Fatal("expected AllowsProjectOverrides to be false")
	}
	if contract.OverridePolicy.AllowsUserOverrides() {
		t.Fatal("expected AllowsUserOverrides to be false")
	}
}

func TestOverridePolicyHelpersReportIndividualFlagsEvenWhenInvalid(t *testing.T) {
	policy := &OverridePolicy{
		AllowAdapterOverrides: boolPtr(true),
		AllowProjectOverrides: boolPtr(false),
		AllowUserOverrides:    boolPtr(true),
	}

	if !policy.OverridesAllowed() {
		t.Fatal("expected OverridesAllowed to be true")
	}
	if !policy.AllowsAdapterOverrides() {
		t.Fatal("expected AllowsAdapterOverrides to be true")
	}
	if policy.AllowsProjectOverrides() {
		t.Fatal("expected AllowsProjectOverrides to be false")
	}
	if !policy.AllowsUserOverrides() {
		t.Fatal("expected AllowsUserOverrides to be true")
	}
}

func TestValidateOverridePolicyAcceptsAllFalsePolicy(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	if err := ValidateOverridePolicy(contract.OverridePolicy); err != nil {
		t.Fatalf("ValidateOverridePolicy returned error: %v", err)
	}
}

func TestValidateOverridePolicyRejectsMissingPolicy(t *testing.T) {
	err := ValidateOverridePolicy(nil)
	assertFixtureError(t, err, ErrValidation, "overridePolicy is required")
}

func TestValidateOverridePolicyRejectsMissingIndividualFlagsDeterministically(t *testing.T) {
	cases := []struct {
		name string
		edit func(policy *OverridePolicy)
		want []string
	}{
		{
			name: "adapter",
			edit: func(policy *OverridePolicy) { policy.AllowAdapterOverrides = nil },
			want: []string{"overridePolicy.allowAdapterOverrides is required"},
		},
		{
			name: "project",
			edit: func(policy *OverridePolicy) { policy.AllowProjectOverrides = nil },
			want: []string{"overridePolicy.allowProjectOverrides is required"},
		},
		{
			name: "user",
			edit: func(policy *OverridePolicy) { policy.AllowUserOverrides = nil },
			want: []string{"overridePolicy.allowUserOverrides is required"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract := loadFixture(t, "smoke/freecad-default")
			tc.edit(contract.OverridePolicy)

			err := ValidateOverridePolicy(contract.OverridePolicy)
			if err == nil {
				t.Fatal("expected ValidateOverridePolicy to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tc.want) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tc.want, validationErr.Messages())
			}
		})
	}
}

func TestValidateOverridePolicyRejectsTrueIndividualFlagsDeterministically(t *testing.T) {
	cases := []struct {
		name string
		edit func(policy *OverridePolicy)
		want []string
	}{
		{
			name: "adapter",
			edit: func(policy *OverridePolicy) { policy.AllowAdapterOverrides = boolPtr(true) },
			want: []string{"overridePolicy.allowAdapterOverrides must be false"},
		},
		{
			name: "project",
			edit: func(policy *OverridePolicy) { policy.AllowProjectOverrides = boolPtr(true) },
			want: []string{"overridePolicy.allowProjectOverrides must be false"},
		},
		{
			name: "user",
			edit: func(policy *OverridePolicy) { policy.AllowUserOverrides = boolPtr(true) },
			want: []string{"overridePolicy.allowUserOverrides must be false"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract := loadFixture(t, "smoke/freecad-default")
			tc.edit(contract.OverridePolicy)

			err := ValidateOverridePolicy(contract.OverridePolicy)
			if err == nil {
				t.Fatal("expected ValidateOverridePolicy to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tc.want) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tc.want, validationErr.Messages())
			}
		})
	}
}

func TestValidateOverridePolicyOrdersMultipleProblemsDeterministically(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	contract.OverridePolicy.AllowAdapterOverrides = boolPtr(true)
	contract.OverridePolicy.AllowProjectOverrides = nil
	contract.OverridePolicy.AllowUserOverrides = boolPtr(true)

	first := ValidateOverridePolicy(contract.OverridePolicy)
	second := ValidateOverridePolicy(contract.OverridePolicy)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}
	if !errors.Is(first, ErrValidation) || !errors.Is(second, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v and %v", first, second)
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected first ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected second ValidationError, got %T", second)
	}

	want := []string{
		"overridePolicy.allowAdapterOverrides must be false",
		"overridePolicy.allowProjectOverrides is required",
		"overridePolicy.allowUserOverrides must be false",
	}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
	}
}
