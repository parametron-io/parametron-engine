package semanticmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/semantic"
)

// #86 boundary coverage: v1.0 semantic maps stay unchanged when capture carries
// technical drawings. There is no captureToSemantic.drawings section, no
// drawing identity linkage, and no drawing operation capability target.

func TestSemanticMapRejectsSpeculativeDrawingSections(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t, "smoke/freecad-default"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(raw map[string]any)
	}{
		{
			name: "root drawings section",
			mutate: func(raw map[string]any) {
				raw["drawings"] = []any{}
			},
		},
		{
			name: "captureToSemantic drawings section",
			mutate: func(raw map[string]any) {
				captureToSemantic, ok := raw["captureToSemantic"].(map[string]any)
				if !ok {
					t.Fatal("fixture captureToSemantic is not an object")
				}
				captureToSemantic["drawings"] = []any{}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal returned error: %v", err)
			}
			tc.mutate(raw)
			mutated, err := json.Marshal(raw)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}

			_, err = Parse(mutated)
			if err == nil {
				t.Fatal("expected Parse to reject the speculative drawing section")
			}
			if !errors.Is(err, ErrDecode) {
				t.Fatalf("expected ErrDecode, got %v", err)
			}
			if !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("expected unknown-field decode error, got %v", err)
			}
		})
	}
}

func TestLinkCaptureIdentitiesIgnoresSemanticDrawings(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	if err := Validate(contract); err != nil {
		t.Fatalf("semantic map validation changed: %v", err)
	}
	for operation, capability := range contract.OperationCapabilities {
		for _, target := range capability.AllowedSemanticTargets {
			if target == "drawing" {
				t.Fatalf("unexpected drawing operation capability target in %q", operation)
			}
		}
	}

	base := smokeSemanticModel(t)
	withDrawings := semantic.Clone(base)
	withDrawings.Drawings = []semantic.Drawing{
		{
			NativeRef:    "Drawing",
			ID:           "drw.boundary",
			Name:         "boundary",
			DisplayName:  "Boundary Drawing",
			ComponentIDs: []string{"cmp.root"},
			Resource:     semantic.DrawingResource{Topology: "embedded"},
			Sheets: []semantic.DrawingSheet{
				{NativeRef: "Page", ID: "sht.boundary", Name: "sheet", DisplayName: "", Order: 1, NativeType: "TechDraw::DrawPage"},
			},
		},
	}
	if err := semantic.Validate(withDrawings); err != nil {
		t.Fatalf("semantic model with drawings is invalid: %v", err)
	}

	baseLinkage, err := LinkCaptureIdentities(contract, base)
	if err != nil {
		t.Fatalf("LinkCaptureIdentities(base) returned error: %v", err)
	}
	drawingsLinkage, err := LinkCaptureIdentities(contract, withDrawings)
	if err != nil {
		t.Fatalf("LinkCaptureIdentities(with drawings) returned error: %v", err)
	}
	if !reflect.DeepEqual(baseLinkage, drawingsLinkage) {
		t.Fatalf("semantic drawings changed identity linkage\nbase:     %#v\nwithDrawings: %#v", baseLinkage, drawingsLinkage)
	}
	for _, entry := range drawingsLinkage.Entries {
		if strings.Contains(entry.EntityKind, "drawing") || entry.SemanticID == "drw.boundary" {
			t.Fatalf("semantic drawing was linked as an identity: %#v", entry)
		}
	}

	data, err := json.Marshal(drawingsLinkage)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	for _, forbidden := range []string{"drw.boundary", "sht.boundary", "Boundary Drawing", `"drawing"`} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("semantic drawing leaked into identity linkage: %s", data)
		}
	}
}
