package semantic

import (
	"bytes"
	"testing"

	"parametron/internal/engine/cad"
)

func nativeTargetCapture(t *testing.T) *cad.CADContract {
	t.Helper()
	capture, err := cad.Load("testdata/native-target/prm.cad.json")
	if err != nil {
		t.Fatal(err)
	}
	return capture
}

func TestBuildFromCapture_NativeTargetRetentionAndJSONExclusion(t *testing.T) {
	capture := nativeTargetCapture(t)
	model, err := BuildFromCapture(capture)
	if err != nil {
		t.Fatal(err)
	}
	capture.Entities.Components[0].IdentitySource.NativeRef = "changed"
	capture.Entities.Features[0].IdentitySource.NativeRef = "changed"
	canonical, err := Canonicalize(model)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []*Model{model, Clone(model), canonical} {
		c, f := got.CanonicalComponents()[0], got.CanonicalFeatures()[0]
		if c.ID != "cmp.root" || c.Name != "bracket_assembly" || c.DisplayName != "Bracket Assembly" || c.NativeRef != "Assembly01" {
			t.Fatalf("component identities: %+v", c)
		}
		if f.ID != "fea.mountingBracket" || f.Name != "mounting_bracket" || f.DisplayName != "Mounting Bracket" || f.NativeRef != "Body01" {
			t.Fatalf("feature identities: %+v", f)
		}
		data, err := CanonicalJSON(got)
		if err != nil {
			t.Fatal(err)
		}
		for _, excluded := range []string{"nativeRef", "NativeRef", "Body01", "Assembly01"} {
			if bytes.Contains(data, []byte(excluded)) {
				t.Fatalf("internal native mapping leaked into JSON: %s", data)
			}
		}
	}
}

func TestBuildFromCapture_MissingNativeMappingIsRetainedWithoutFallback(t *testing.T) {
	for _, absent := range []bool{false, true} {
		capture := nativeTargetCapture(t)
		if absent {
			capture.Entities.Components[0].IdentitySource = nil
			capture.Entities.Features[0].IdentitySource = nil
		} else {
			capture.Entities.Components[0].IdentitySource.NativeRef = ""
			capture.Entities.Features[0].IdentitySource.NativeRef = ""
		}
		model, err := BuildFromCapture(capture)
		if err != nil {
			t.Fatal(err)
		}
		if model.Components[0].NativeRef != "" || model.Features[0].NativeRef != "" {
			t.Fatal("missing native mapping acquired a fallback")
		}
	}
}

func TestResolveSemanticTarget_NativeIdentityIsNotAnAlias(t *testing.T) {
	model, err := BuildFromCapture(nativeTargetCapture(t))
	if err != nil {
		t.Fatal(err)
	}
	target, err := ResolveSemanticTargetByExactName(model, "mounting_bracket")
	if err != nil || target.SemanticID != "fea.mountingBracket" {
		t.Fatalf("semantic lookup: %+v, %v", target, err)
	}
	for _, name := range []string{"Body01", "fea.mountingBracket", "Mounting Bracket", "Mounting_bracket"} {
		if _, err := ResolveSemanticTargetByExactName(model, name); err == nil {
			t.Fatalf("unexpected alias %q", name)
		}
	}
	// Native selector reuse does not create semantic Name ambiguity.
	other := model.Features[0]
	other.ID = "fea.other"
	other.Name = "other"
	model.Features = append(model.Features, other)
	if _, err := ResolveSemanticTargetByExactName(model, "mounting_bracket"); err != nil {
		t.Fatal(err)
	}
}
