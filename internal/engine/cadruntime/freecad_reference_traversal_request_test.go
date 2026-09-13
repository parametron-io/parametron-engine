package cadruntime

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/adapter"
)

func traversalTarget(source, property, mechanism, target, document string) adapter.ReferenceTraversalExternalTarget {
	return adapter.ReferenceTraversalExternalTarget{SourceObjectName: source, SourceProperty: property, ReferenceMechanism: mechanism, TargetObjectName: target, TargetDocumentPath: document}
}

func TestComposeFreeCADReferenceTraversalRequest_EmptyAndByteStable(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	first, err := ComposeFreeCADReferenceTraversalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComposeFreeCADReferenceTraversalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"schemaVersion\":\"2.0\",\"externalTargets\":[]}\n"
	if string(first.JSON) != want || !bytes.Equal(first.JSON, second.JSON) {
		t.Fatalf("bytes=%q second=%q want=%q", first.JSON, second.JSON, want)
	}
}

func TestComposeFreeCADReferenceTraversalRequest_CanonicalOrderingAndRelocationSafety(t *testing.T) {
	entries := []adapter.ReferenceTraversalExternalTarget{
		traversalTarget("B", "Parts", "App::PropertyXLinkList", "Wheel", "references/wheel.FCStd"),
		traversalTarget("A", "Parts", "App::PropertyXLinkList", "Body", "references/part.FCStd"),
	}
	var previous []byte
	for i := 0; i < 2; i++ {
		req := validFreeCADObservationRequest(t)
		req.ExternalTargets = append([]adapter.ReferenceTraversalExternalTarget(nil), entries...)
		if i == 1 {
			req.ExternalTargets[0], req.ExternalTargets[1] = req.ExternalTargets[1], req.ExternalTargets[0]
		}
		got, err := ComposeFreeCADReferenceTraversalRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got.JSON), req.ProductDir) || strings.Contains(string(got.JSON), req.Manifest.Inputs.SourceModel) {
			t.Fatalf("runtime path leaked: %s", got.JSON)
		}
		if i > 0 && !bytes.Equal(previous, got.JSON) {
			t.Fatalf("relocation/order changed bytes:\n%s\n%s", previous, got.JSON)
		}
		previous = got.JSON
	}
}

func TestComposeFreeCADReferenceTraversalRequest_Validation(t *testing.T) {
	base := traversalTarget("Assembly", "LinkedParts", "App::PropertyXLinkList", "Body", "references/part.FCStd")
	tests := map[string][]adapter.ReferenceTraversalExternalTarget{
		"absolute path": {func() adapter.ReferenceTraversalExternalTarget {
			x := base
			x.TargetDocumentPath = "/tmp/part.FCStd"
			return x
		}()},
		"windows absolute path": {func() adapter.ReferenceTraversalExternalTarget {
			x := base
			x.TargetDocumentPath = "C:/part.FCStd"
			return x
		}()},
		"traversal path": {func() adapter.ReferenceTraversalExternalTarget {
			x := base
			x.TargetDocumentPath = "../part.FCStd"
			return x
		}()},
		"noncanonical path": {func() adapter.ReferenceTraversalExternalTarget {
			x := base
			x.TargetDocumentPath = "references/../part.FCStd"
			return x
		}()},
		"unsupported": {func() adapter.ReferenceTraversalExternalTarget {
			x := base
			x.ReferenceMechanism = "App::PropertyString"
			return x
		}()},
		"empty coordinate": {func() adapter.ReferenceTraversalExternalTarget { x := base; x.SourceProperty = ""; return x }()},
		"duplicate":        {base, base},
		"coordinate conflict": {base, func() adapter.ReferenceTraversalExternalTarget {
			x := base
			x.TargetDocumentPath = "references/other.FCStd"
			return x
		}()},
		"single cardinality": {traversalTarget("A", "Part", "App::PropertyXLink", "Body", "references/a.FCStd"), traversalTarget("A", "Part", "App::PropertyXLink", "Other", "references/b.FCStd")},
		"list duplicate name": {base, func() adapter.ReferenceTraversalExternalTarget {
			x := base
			x.TargetDocumentPath = "references/other.FCStd"
			return x
		}()},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			req := validFreeCADObservationRequest(t)
			req.ExternalTargets = entries
			_, err := ComposeFreeCADReferenceTraversalRequest(req)
			var typed *FreeCADReferenceTraversalRequestError
			if !errors.As(err, &typed) || typed.Unwrap() == nil {
				t.Fatalf("error=%T %v", err, err)
			}
		})
	}
}

func TestComposeFreeCADReferenceTraversalRequest_ListAllowsDistinctTargets(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.ExternalTargets = []adapter.ReferenceTraversalExternalTarget{
		traversalTarget("A", "Parts", "App::PropertyXLinkList", "Body", "references/a.FCStd"),
		traversalTarget("A", "Parts", "App::PropertyXLinkList", "Wheel", "references/b.FCStd"),
	}
	got, err := ComposeFreeCADReferenceTraversalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Contract.ExternalTargets) != 2 {
		t.Fatalf("targets=%d", len(got.Contract.ExternalTargets))
	}
}

func TestDecodeFreeCADReferenceTraversalRequest_VersionedStrictness(t *testing.T) {
	for _, raw := range []string{`{"schemaVersion":"2.0","externalTargets":[],"extra":true}`, `{"schemaVersion":"2.0"}`, `{"schemaVersion":"2.0","externalTargets":[]} {}`} {
		if _, err := DecodeFreeCADReferenceTraversalRequest([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	v2, err := DecodeFreeCADReferenceTraversalRequest([]byte(`{"schemaVersion":"2.0","externalTargets":[]}`))
	if err != nil || v2.SchemaVersion != "2.0" || !reflect.DeepEqual(v2.ExternalTargets, []FreeCADReferenceTraversalExternalTarget{}) {
		t.Fatalf("v2=%#v err=%v", v2, err)
	}
}

func TestDecodeFreeCADReferenceTraversalRequest_RejectsUnsupportedVersions(t *testing.T) {
	for _, raw := range []string{`{"schemaVersion":"1.0"}`, `{"schemaVersion":"1.0","externalTargets":[]}`, `{"schemaVersion":"3.0","externalTargets":[]}`, `{"externalTargets":[]}`} {
		if _, err := DecodeFreeCADReferenceTraversalRequest([]byte(raw)); err == nil || !strings.Contains(err.Error(), "unsupported schemaVersion") {
			t.Fatalf("input=%s err=%v", raw, err)
		}
	}
}
