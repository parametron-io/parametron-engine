package semanticmap

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/semantic"
)

func TestDeterminismFlagsEnabledAreRejectedAcrossValidationAndProjectionLinkageBoundary(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SemanticMap)
		want   string
	}{
		{
			name: "implicit_fallback",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowImplicitFallback = boolPtr(true)
			},
			want: "determinism.allowImplicitFallback must be false",
		},
		{
			name: "case_insensitive_match",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowCaseInsensitiveMatch = boolPtr(true)
			},
			want: "determinism.allowCaseInsensitiveMatch must be false",
		},
		{
			name: "display_name_identity",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowDisplayNameIdentity = boolPtr(true)
			},
			want: "determinism.allowDisplayNameIdentity must be false",
		},
		{
			name: "adapter_inference",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowAdapterInference = boolPtr(true)
			},
			want: "determinism.allowAdapterInference must be false",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
			tc.mutate(contract)

			assertValidationErrorProblems(t, Validate(contract), []string{tc.want})

			review := ReviewProjectionContract(contract)
			if review.Ready {
				t.Fatal("expected projection review to reject determinism violation")
			}
			if !reflect.DeepEqual(review.Problems, []string{tc.want}) {
				t.Fatalf("unexpected review problems\nwant: %#v\ngot:  %#v", []string{tc.want}, review.Problems)
			}

			assertValidationErrorProblems(t, ValidateProjectionContract(contract), []string{tc.want})
			assertValidationErrorProblems(t, validateProjectionLinkageContract(contract), []string{tc.want})

			assertIdentityBoundaryRejectsDeterminism(t, contract, tc.want)
			assertParameterProjectionRejectsDeterminism(t, contract, tc.want)
			assertOutputProjectionRejectsDeterminism(t, contract, tc.want)
			assertMutationProjectionRejectsDeterminism(t, contract, tc.want)
		})
	}
}

func TestDeterminismFlagsMissingAreRejectedWithoutDefaultingAcrossBoundary(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SemanticMap)
		want   string
	}{
		{
			name: "implicit_fallback",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowImplicitFallback = nil
			},
			want: "determinism.allowImplicitFallback is required",
		},
		{
			name: "case_insensitive_match",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowCaseInsensitiveMatch = nil
			},
			want: "determinism.allowCaseInsensitiveMatch is required",
		},
		{
			name: "display_name_identity",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowDisplayNameIdentity = nil
			},
			want: "determinism.allowDisplayNameIdentity is required",
		},
		{
			name: "adapter_inference",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowAdapterInference = nil
			},
			want: "determinism.allowAdapterInference is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
			tc.mutate(contract)

			assertValidationErrorProblems(t, Validate(contract), []string{tc.want})
			assertValidationErrorProblems(t, validateProjectionLinkageContract(contract), []string{tc.want})

			review := ReviewProjectionContract(contract)
			if review.Ready {
				t.Fatal("expected projection review to reject missing determinism flag")
			}
			if !reflect.DeepEqual(review.Problems, []string{tc.want}) {
				t.Fatalf("unexpected review problems\nwant: %#v\ngot:  %#v", []string{tc.want}, review.Problems)
			}

			assertIdentityBoundaryRejectsDeterminism(t, contract, tc.want)
			assertParameterProjectionRejectsDeterminism(t, contract, tc.want)
			assertOutputProjectionRejectsDeterminism(t, contract, tc.want)
			assertMutationProjectionRejectsDeterminism(t, contract, tc.want)
		})
	}
}

func TestDeterminismBoundaryProducesStableProblemOrdering(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.Determinism.AllowImplicitFallback = nil
	contract.Determinism.AllowCaseInsensitiveMatch = boolPtr(true)
	contract.Determinism.AllowDisplayNameIdentity = nil
	contract.Determinism.AllowAdapterInference = boolPtr(true)

	want := []string{
		"determinism.allowImplicitFallback is required",
		"determinism.allowCaseInsensitiveMatch must be false",
		"determinism.allowDisplayNameIdentity is required",
		"determinism.allowAdapterInference must be false",
	}

	first := validateProjectionLinkageContract(contract)
	second := validateProjectionLinkageContract(contract)
	assertValidationErrorProblems(t, first, want)
	assertValidationErrorProblems(t, second, want)
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic boundary error text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
	}

	reviewFirst := ReviewProjectionContract(contract)
	reviewSecond := ReviewProjectionContract(contract)
	if !reflect.DeepEqual(reviewFirst.Problems, want) {
		t.Fatalf("unexpected first review problems\nwant: %#v\ngot:  %#v", want, reviewFirst.Problems)
	}
	if !reflect.DeepEqual(reviewSecond.Problems, want) {
		t.Fatalf("unexpected second review problems\nwant: %#v\ngot:  %#v", want, reviewSecond.Problems)
	}
}

func TestProjectOutputs_TargetSemanticIDMatchingRemainsCaseSensitive(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithOutputs(semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "CMP.ASSEMBLY", TargetNameSource: "identity_linkage"}),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err == nil {
		t.Fatal("expected exact-case target semantic ID failure")
	}
	if !errors.Is(err, ErrProjectionNonProjectableOutput) {
		t.Fatalf("expected non-projectable output sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), `missing target for assembly "CMP.ASSEMBLY"`) {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestProjectOutputs_IdentityLinkageResolutionRemainsCaseSensitive(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)
	for index := range linkage.Entries {
		if linkage.Entries[index].EntityKind == identityKindComponent && linkage.Entries[index].SemanticID == "cmp.assembly" {
			linkage.Entries[index].SemanticID = "CMP.ASSEMBLY"
			break
		}
	}

	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithOutputs(semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"}),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err == nil {
		t.Fatal("expected exact-case identity linkage failure")
	}
	if !errors.Is(err, ErrProjectionMissingIdentityLinkage) {
		t.Fatalf("expected missing identity linkage sentinel, got %v", err)
	}
}

func TestProjectMutations_MissingIdentityLinkageDoesNotFallbackToNamesOrNativeMetadata(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)
	filtered := linkage.Entries[:0]
	for _, entry := range linkage.Entries {
		if entry.EntityKind == identityKindMetadata && entry.SemanticID == "meta.a" {
			continue
		}
		filtered = append(filtered, entry)
	}
	linkage.Entries = filtered

	_, err := ProjectMutations(&MutationProjectionRequest{
		Model: model,
		Intent: outputMutationProjectionIntentWithMutations(
			semantic.MutationIntent{
				OperationKind:    "set_property",
				TargetEntityKind: "metadata",
				TargetSemanticID: "meta.a",
				Scope:            "assembly",
				ValueSource:      semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "part_number_a"},
			},
		),
		ResolvedValues:  map[string]any{"part_number_a": "A-001"},
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err == nil {
		t.Fatal("expected missing metadata identity linkage failure")
	}
	if !errors.Is(err, ErrProjectionMissingIdentityLinkage) {
		t.Fatalf("expected missing identity linkage sentinel, got %v", err)
	}
}

func assertValidationErrorProblems(t *testing.T, err error, want []string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func assertIdentityBoundaryRejectsDeterminism(t *testing.T, contract *SemanticMap, want string) {
	t.Helper()
	first, firstErr := LinkCaptureIdentities(contract, smokeSemanticModel(t))
	second, secondErr := LinkCaptureIdentities(contract, smokeSemanticModel(t))
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected repeated identity linkage failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no linkage on determinism failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrIdentityInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", firstErr)
	}
	assertProblemCarrierContainsExactly(t, firstErr, []string{want})
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic identity error text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func assertParameterProjectionRejectsDeterminism(t *testing.T, contract *SemanticMap, want string) {
	t.Helper()
	request := &ParameterProjectionRequest{
		Model:          projectionModel("Length", "par.root.length", "length"),
		Intent:         projectionIntent("length", "par.root.length"),
		ResolvedValues: map[string]any{"length": 35.0},
		Contract:       contract,
	}
	first, firstErr := ProjectParameterAssignments(request)
	second, secondErr := ProjectParameterAssignments(request)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected repeated parameter projection failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no assignments on determinism failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrProjectionInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", firstErr)
	}
	assertProblemCarrierContainsExactly(t, firstErr, []string{want})
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic parameter projection error text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func assertOutputProjectionRejectsDeterminism(t *testing.T, contract *SemanticMap, want string) {
	t.Helper()
	request := &OutputProjectionRequest{
		Model:    outputMutationProjectionModel(),
		Intent:   outputMutationProjectionIntentWithOutputs(semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"}),
		Contract: contract,
	}
	first, firstErr := ProjectOutputs(request)
	second, secondErr := ProjectOutputs(request)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected repeated output projection failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no outputs on determinism failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrProjectionInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", firstErr)
	}
	assertProblemCarrierContainsExactly(t, firstErr, []string{want})
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic output projection error text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func assertMutationProjectionRejectsDeterminism(t *testing.T, contract *SemanticMap, want string) {
	t.Helper()
	request := &MutationProjectionRequest{
		Model: outputMutationProjectionModel(),
		Intent: outputMutationProjectionIntentWithMutations(
			semantic.MutationIntent{OperationKind: "suppress", TargetEntityKind: "feature", TargetSemanticID: "feat.assembly", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}},
		),
		ResolvedValues: map[string]any{},
		Contract:       contract,
	}
	first, firstErr := ProjectMutations(request)
	second, secondErr := ProjectMutations(request)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected repeated mutation projection failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no mutations on determinism failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrProjectionInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", firstErr)
	}
	assertProblemCarrierContainsExactly(t, firstErr, []string{want})
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic mutation projection error text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func assertProblemCarrierContainsExactly(t *testing.T, err error, want []string) {
	t.Helper()

	type problemLister interface {
		ProblemList() []string
	}

	var lister problemLister
	if !errors.As(err, &lister) {
		t.Fatalf("expected error exposing ProblemList, got %T", err)
	}
	if !reflect.DeepEqual(lister.ProblemList(), want) {
		t.Fatalf("unexpected problem list\nwant: %#v\ngot:  %#v", want, lister.ProblemList())
	}
}
