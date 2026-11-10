package recordcontract

import (
	"errors"
	"fmt"
	"testing"
)

func occurrenceTestScope() OccurrenceScope {
	return OccurrenceScope{Parent: OccurrenceEntity{Resource: ReferenceResource{ProjectID: "p", Kind: "model", LogicalID: "assembly"}, NativeKind: "object", Name: "Assembly"}, Context: []OccurrenceContextFrame{}, Mechanism: OccurrenceDirectMemberMechanism}
}

func TestOccurrenceIdentitySeparatesMemberParentResourceAndContext(t *testing.T) {
	base := occurrenceTestScope()
	original, err := OccurrenceKey(base, "Link")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*OccurrenceScope){
		"parent":   func(s *OccurrenceScope) { s.Parent.Name = "Other" },
		"resource": func(s *OccurrenceScope) { s.Parent.Resource.LogicalID = "other" },
		"context": func(s *OccurrenceScope) {
			p := s.Parent
			p.Name = "Outer"
			s.Context = []OccurrenceContextFrame{{Parent: p, Member: "Instance"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := occurrenceTestScope()
			mutate(&s)
			got, err := OccurrenceKey(s, "Link")
			if err != nil || got == original {
				t.Fatalf("identity collapsed: %s, %v", got, err)
			}
		})
	}
	other, err := OccurrenceKey(base, "OtherLink")
	if err != nil || other == original {
		t.Fatal("distinct members collapsed")
	}
	base.Parent.SemanticID = "annotation"
	got, err := OccurrenceKey(base, "Link")
	if err != nil || got != original {
		t.Fatal("semantic annotation changed native key")
	}
}

func TestOccurrenceContextBoundsOrderAndCycles(t *testing.T) {
	scope := occurrenceTestScope()
	for i := 0; i < 16; i++ {
		p := scope.Parent
		p.Name = fmt.Sprintf("Outer%d", i)
		scope.Context = append(scope.Context, OccurrenceContextFrame{Parent: p, Member: fmt.Sprintf("Link%d", i)})
	}
	if err := ValidateOccurrenceScope(scope); err != nil {
		t.Fatal(err)
	}
	first, err := OccurrenceKey(scope, "Link")
	if err != nil {
		t.Fatal(err)
	}
	scope.Context[0], scope.Context[1] = scope.Context[1], scope.Context[0]
	second, err := OccurrenceKey(scope, "Link")
	if err != nil || first == second {
		t.Fatal("ordered frames lost")
	}
	for name, mutate := range map[string]func(*OccurrenceScope){
		"seventeen": func(s *OccurrenceScope) {
			p := s.Parent
			p.Name = "Seventeenth"
			s.Context = append(s.Context, OccurrenceContextFrame{Parent: p, Member: "Link"})
		},
		"duplicate parent": func(s *OccurrenceScope) { s.Context[1] = s.Context[0] },
		"cycle to root":    func(s *OccurrenceScope) { s.Context[0].Parent = s.Parent },
		"missing parent":   func(s *OccurrenceScope) { s.Context[0].Parent = OccurrenceEntity{} },
		"malformed member": func(s *OccurrenceScope) { s.Context[0].Member = "A.B." },
		"missing context":  func(s *OccurrenceScope) { s.Context = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := scope
			s.Context = append([]OccurrenceContextFrame{}, scope.Context...)
			mutate(&s)
			if err := ValidateOccurrenceScope(s); !errors.Is(err, ErrInvalidOccurrence) {
				t.Fatalf("invalid context accepted: %v", err)
			}
		})
	}
	// A valid bounded context never authorizes recursive complete quantities.
	summary := OccurrenceSummary{SchemaVersion: "1.0", Scope: scope, Completeness: "complete", Occurrences: []Occurrence{}, Quantities: []OccurrenceQuantity{}, Evidence: ObservationEvidence{SourceKind: "occurrence-evidence", SourceRef: "raw/runtime/prm.occurrence-evidence.json", DigestSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if err := ValidateOccurrenceSummary(summary); !errors.Is(err, ErrInvalidOccurrence) {
		t.Fatalf("recursive complete scope accepted: %v", err)
	}
}

func TestOccurrenceSummaryRejectsFabricatedQuantitiesAndIndependentVersion(t *testing.T) {
	scope := occurrenceTestScope()
	target := scope.Parent
	target.Name = "Body"
	for name, mutate := range map[string]func(*OccurrenceSummary){
		"evidence version used as summary": func(s *OccurrenceSummary) { s.SchemaVersion = "1.1" },
		"fabricated quantity":              func(s *OccurrenceSummary) { s.Quantities[0].Quantity = 2 },
		"duplicate member":                 func(s *OccurrenceSummary) { s.Occurrences = append(s.Occurrences, s.Occurrences[0]) },
		"incomplete numeric":               func(s *OccurrenceSummary) { s.Completeness = "incomplete" },
		"unsupported numeric":              func(s *OccurrenceSummary) { s.Completeness = "unsupported" },
		"unavailable numeric":              func(s *OccurrenceSummary) { s.Completeness = "unavailable"; s.Occurrences = []Occurrence{} },
		"missing quantities":               func(s *OccurrenceSummary) { s.Quantities = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := OccurrenceSummary{SchemaVersion: "1.0", Scope: scope, Completeness: "complete", Occurrences: []Occurrence{{Member: "Link", Target: target}}, Quantities: []OccurrenceQuantity{{Target: target, Quantity: 1}}, Evidence: ObservationEvidence{SourceKind: "occurrence-evidence", SourceRef: "raw/runtime/prm.occurrence-evidence.json", DigestSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
			if err := ValidateOccurrenceSummary(s); err != nil {
				t.Fatal(err)
			}
			mutate(&s)
			if err := ValidateOccurrenceSummary(s); !errors.Is(err, ErrInvalidOccurrence) {
				t.Fatalf("invalid summary accepted: %v", err)
			}
		})
	}
}
