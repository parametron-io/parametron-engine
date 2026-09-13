package recordcontract

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeProvenanceDeterministicForEquivalentUnorderedInput(t *testing.T) {
	input := unorderedProvenance()

	first := NormalizeProvenance(input)
	second := NormalizeProvenance(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("NormalizeProvenance repeated calls differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}

	equivalent := Provenance{
		SourceRevision: SourceRevisionProvenance{
			RevisionID:   "\t" + first.SourceRevision.RevisionID,
			AssetID:      first.SourceRevision.AssetID + "\n",
			DigestSHA256: " " + first.SourceRevision.DigestSHA256 + " ",
		},
		Inputs: []ProvenanceInput{
			{
				Kind:         first.Inputs[1].Kind + "\n",
				Identity:     " " + first.Inputs[1].Identity,
				DigestSHA256: "\t" + first.Inputs[1].DigestSHA256 + " ",
			},
			{
				Kind:         " " + first.Inputs[0].Kind,
				Identity:     first.Inputs[0].Identity + " ",
				DigestSHA256: first.Inputs[0].DigestSHA256 + "\n",
			},
		},
		Plan: PlanProvenance{
			PlanID:   " " + first.Plan.PlanID,
			PlanHash: first.Plan.PlanHash + "\t",
		},
		Linkage: LinkageProvenance{
			ProductKey: "\n" + first.Linkage.ProductKey,
			JobID:      first.Linkage.JobID + " ",
			StepRef:    " " + first.Linkage.StepRef + " ",
		},
		Evidence: []EvidenceReference{
			{
				Kind:         first.Evidence[1].Kind + " ",
				Ref:          "\t" + first.Evidence[1].Ref,
				DigestSHA256: first.Evidence[1].DigestSHA256 + "\n",
			},
			{
				Kind:         " " + first.Evidence[0].Kind,
				Ref:          first.Evidence[0].Ref + "\t",
				DigestSHA256: " " + first.Evidence[0].DigestSHA256,
			},
		},
		Runtime: RuntimeProvenance{
			ToolID:    " " + first.Runtime.ToolID,
			RuntimeID: first.Runtime.RuntimeID + "\n",
			Adapter:   "\t" + first.Runtime.Adapter + " ",
		},
	}

	if got := NormalizeProvenance(equivalent); !reflect.DeepEqual(got, first) {
		t.Fatalf("NormalizeProvenance(equivalent) = %#v, want %#v", got, first)
	}

	if first.Inputs[0].Kind != "A-kind" || first.Inputs[1].Kind != "B-kind" {
		t.Fatalf("inputs not sorted by stable keys: %#v", first.Inputs)
	}
	if first.Evidence[0].Kind != "artifact" || first.Evidence[1].Kind != "log" {
		t.Fatalf("evidence not sorted by stable keys: %#v", first.Evidence)
	}
	if first.SourceRevision.RevisionID != "Rev-Case-Sensitive" {
		t.Fatalf("RevisionID = %q, want case preserved", first.SourceRevision.RevisionID)
	}
	if first.Inputs[0].Identity != "Identity-Upper" {
		t.Fatalf("Identity = %q, want case preserved", first.Inputs[0].Identity)
	}

	empty := NormalizeProvenance(Provenance{})
	if empty.Inputs == nil {
		t.Fatal("NormalizeProvenance empty Inputs = nil, want empty non-nil slice")
	}
	if empty.Evidence == nil {
		t.Fatal("NormalizeProvenance empty Evidence = nil, want empty non-nil slice")
	}
	if len(empty.Inputs) != 0 || len(empty.Evidence) != 0 {
		t.Fatalf("NormalizeProvenance empty repeated collections = %d inputs, %d evidence; want 0, 0", len(empty.Inputs), len(empty.Evidence))
	}
}

func TestNormalizeProvenanceCopySafety(t *testing.T) {
	original := unorderedProvenance()
	normalized := NormalizeProvenance(original)

	original.Inputs[0] = ProvenanceInput{Kind: "mutated", Identity: "mutated", DigestSHA256: digestA()}
	original.Evidence[0] = EvidenceReference{Kind: "mutated", Ref: "mutated", DigestSHA256: digestB()}
	if normalized.Inputs[0].Kind != "A-kind" {
		t.Fatalf("normalized Inputs changed after original mutation: %#v", normalized.Inputs)
	}
	if normalized.Evidence[0].Kind != "artifact" {
		t.Fatalf("normalized Evidence changed after original mutation: %#v", normalized.Evidence)
	}

	first := NormalizeProvenance(unorderedProvenance())
	second := NormalizeProvenance(unorderedProvenance())
	first.Inputs[0].Kind = "changed"
	first.Evidence[0].Ref = "changed"
	if second.Inputs[0].Kind != "A-kind" {
		t.Fatalf("second normalized Inputs changed after first mutation: %#v", second.Inputs)
	}
	if second.Evidence[0].Ref != "artifact/ref" {
		t.Fatalf("second normalized Evidence changed after first mutation: %#v", second.Evidence)
	}

	third := NormalizeProvenance(unorderedProvenance())
	if third.Inputs[0].Kind != "A-kind" || third.Evidence[0].Ref != "artifact/ref" {
		t.Fatalf("repeated NormalizeProvenance leaked caller mutation: %#v", third)
	}
}

func TestValidateProvenanceAcceptsValidMinimalProvenance(t *testing.T) {
	if err := ValidateProvenance(Provenance{}); err != nil {
		t.Fatalf("ValidateProvenance(empty optional groups) returned error: %v", err)
	}

	minimal := Provenance{
		SourceRevision: SourceRevisionProvenance{RevisionID: "rev-1"},
		Inputs: []ProvenanceInput{
			{Kind: "dsl", Identity: "model-1"},
		},
		Plan: PlanProvenance{PlanID: "plan-1"},
		Evidence: []EvidenceReference{
			{Kind: "log", Ref: "logs/run.log"},
		},
	}
	if err := ValidateProvenance(minimal); err != nil {
		t.Fatalf("ValidateProvenance(valid minimal present groups) returned error: %v", err)
	}
}

func TestValidateProvenanceRejectsMalformedPresentItems(t *testing.T) {
	tests := []struct {
		name string
		in   Provenance
	}{
		{
			name: "source revision digest without revision or asset",
			in: Provenance{
				SourceRevision: SourceRevisionProvenance{DigestSHA256: digestA()},
			},
		},
		{
			name: "input missing kind",
			in: Provenance{
				Inputs: []ProvenanceInput{{Kind: " ", Identity: "input-1"}},
			},
		},
		{
			name: "input missing identity",
			in: Provenance{
				Inputs: []ProvenanceInput{{Kind: "dsl", Identity: "\n"}},
			},
		},
		{
			name: "evidence missing kind",
			in: Provenance{
				Evidence: []EvidenceReference{{Kind: " ", Ref: "logs/run.log"}},
			},
		},
		{
			name: "evidence missing ref",
			in: Provenance{
				Evidence: []EvidenceReference{{Kind: "log", Ref: "\t"}},
			},
		},
		{
			name: "uppercase digest",
			in: Provenance{
				Inputs: []ProvenanceInput{{Kind: "dsl", Identity: "input-1", DigestSHA256: upperDigestA()}},
			},
		},
		{
			name: "short digest",
			in: Provenance{
				Inputs: []ProvenanceInput{{Kind: "dsl", Identity: "input-1", DigestSHA256: "abc123"}},
			},
		},
		{
			name: "long digest",
			in: Provenance{
				Evidence: []EvidenceReference{{Kind: "log", Ref: "logs/run.log", DigestSHA256: digestA() + "0"}},
			},
		},
		{
			name: "non hex digest",
			in: Provenance{
				SourceRevision: SourceRevisionProvenance{RevisionID: "rev-1", DigestSHA256: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateProvenance(tc.in); !errors.Is(err, ErrInvalidProvenance) {
				t.Fatalf("ValidateProvenance error = %v, want ErrInvalidProvenance", err)
			}
		})
	}
}

func unorderedProvenance() Provenance {
	return Provenance{
		SourceRevision: SourceRevisionProvenance{
			RevisionID:   " Rev-Case-Sensitive ",
			AssetID:      "\tAsset-Case-Sensitive\n",
			DigestSHA256: " " + digestA() + " ",
		},
		Inputs: []ProvenanceInput{
			{Kind: " B-kind ", Identity: "identity-lower", DigestSHA256: digestB()},
			{Kind: "A-kind", Identity: " Identity-Upper ", DigestSHA256: "\t" + digestA()},
		},
		Plan: PlanProvenance{
			PlanID:   " Plan-Case-Sensitive ",
			PlanHash: "\tPlanHash-Case-Sensitive\n",
		},
		Linkage: LinkageProvenance{
			ProductKey: " Product-Case-Sensitive ",
			JobID:      "\tJob-Case-Sensitive",
			StepRef:    "Step-Case-Sensitive\n",
		},
		Evidence: []EvidenceReference{
			{Kind: " log ", Ref: "logs/run.log", DigestSHA256: digestD()},
			{Kind: "artifact", Ref: " artifact/ref ", DigestSHA256: digestC()},
		},
		Runtime: RuntimeProvenance{
			ToolID:    " FreeCAD ",
			RuntimeID: "\tRuntime-Case-Sensitive",
			Adapter:   "Adapter-Case-Sensitive\n",
		},
	}
}

func digestA() string {
	return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}

func digestB() string {
	return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
}

func digestC() string {
	return "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
}

func digestD() string {
	return "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
}

func digestE() string {
	return "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
}

func digestF() string {
	return "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
}

func upperDigestA() string {
	return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}
