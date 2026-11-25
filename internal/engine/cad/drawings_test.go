package cad

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// These tests pin the v1.0 technical drawing capture contract from the #83
// audit: entities.drawings as a required registry of CAD-neutral drawings with
// ordered native-bound sheets.

func TestDrawingsZeroDrawingsContract(t *testing.T) {
	contract := mustParse(t, drawingBaseContract([]any{}))
	if contract.Entities.Drawings == nil {
		t.Fatal("expected zero drawings to remain a non-nil empty array")
	}
	if len(contract.Entities.Drawings) != 0 {
		t.Fatalf("expected zero drawings, got %d", len(contract.Entities.Drawings))
	}

	data := canonical(t, contract)
	if !strings.Contains(string(data), `"drawings":[]`) {
		t.Fatalf("expected canonical JSON to emit \"drawings\":[], got %s", data)
	}
}

func TestDrawingsSingleDrawingSingleSheet(t *testing.T) {
	drawing := drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{
		sheetEntry("sht.one", "sheetOne", 1),
	})
	contract := mustParse(t, drawingBaseContract([]any{drawing}))

	if len(contract.Entities.Drawings) != 1 {
		t.Fatalf("expected 1 drawing, got %d", len(contract.Entities.Drawings))
	}
	got := contract.Entities.Drawings[0]
	if got.ID != "drw.one" || got.Name != "one" {
		t.Fatalf("unexpected drawing identity: %#v", got)
	}
	if got.Resource.Topology != "embedded" {
		t.Fatalf("expected embedded topology, got %q", got.Resource.Topology)
	}
	if len(got.Sheets) != 1 || got.Sheets[0].IdentitySource.NativeRef != "Page" {
		t.Fatalf("unexpected sheets: %#v", got.Sheets)
	}
}

func TestDrawingsMultipleDrawings(t *testing.T) {
	contract := mustParse(t, drawingBaseContract([]any{
		drawingEntry("drw.beta", "beta", []string{"cmp.root"}, embeddedResource(), []any{
			sheetEntry("sht.beta", "betaSheet", 1),
		}),
		drawingEntry("drw.alpha", "alpha", []string{"cmp.bracket"}, separateResource("drawings/alpha.FCStd"), []any{
			sheetEntry("sht.alpha", "alphaSheet", 1),
		}),
	}))
	if len(contract.Entities.Drawings) != 2 {
		t.Fatalf("expected 2 drawings, got %d", len(contract.Entities.Drawings))
	}
}

func TestDrawingsCanonicalSorting(t *testing.T) {
	contract := mustParse(t, drawingBaseContract([]any{
		drawingEntry("drw.beta", "beta", []string{"cmp.root", "cmp.bracket"}, embeddedResource(), []any{
			sheetEntry("sht.beta.two", "two", 2),
			sheetEntry("sht.beta.one", "one", 1),
		}),
		drawingEntry("drw.alpha", "alpha", []string{"cmp.bracket"}, embeddedResource(), []any{
			sheetEntry("sht.alpha", "alpha", 1),
		}),
	}))

	data := string(canonical(t, contract))

	if !strings.Contains(data, `"drawings":[{"id":"drw.alpha"`) {
		t.Fatalf("expected drawings sorted by id, got %s", data)
	}
	if strings.Index(data, `"id":"drw.alpha"`) > strings.Index(data, `"id":"drw.beta"`) {
		t.Fatalf("expected drw.alpha before drw.beta, got %s", data)
	}
	if !strings.Contains(data, `"componentIds":["cmp.bracket","cmp.root"]`) {
		t.Fatalf("expected componentIds sorted lexicographically, got %s", data)
	}
	if strings.Index(data, `"id":"sht.beta.one"`) > strings.Index(data, `"id":"sht.beta.two"`) {
		t.Fatalf("expected sheets sorted by order, got %s", data)
	}
}

func TestDrawingsEmbeddedResourceOmitsPath(t *testing.T) {
	for name, resource := range map[string]map[string]any{
		"absent": {"topology": "embedded"},
		"empty":  {"topology": "embedded", "path": ""},
	} {
		t.Run(name, func(t *testing.T) {
			drawing := drawingEntry("drw.embedded", "embedded", []string{"cmp.root"}, resource, []any{
				sheetEntry("sht.embedded", "sheetOne", 1),
			})
			contract := mustParse(t, drawingBaseContract([]any{drawing}))
			data := string(canonical(t, contract))
			if !strings.Contains(data, `"resource":{"topology":"embedded"}`) {
				t.Fatalf("expected embedded resource without path, got %s", data)
			}
		})
	}
}

func TestDrawingsSeparateResourceRoundTrip(t *testing.T) {
	drawing := drawingEntry("drw.separate", "separate", []string{"cmp.root"}, separateResource("drawings/separate.FCStd"), []any{
		sheetEntry("sht.separate", "sheetOne", 1),
	})
	contract := mustParse(t, drawingBaseContract([]any{drawing}))
	data := string(canonical(t, contract))
	if !strings.Contains(data, `"resource":{"topology":"separate","path":"drawings/separate.FCStd"}`) {
		t.Fatalf("expected separate resource path in canonical JSON, got %s", data)
	}
}

func TestDrawingsReorderedEquivalentInputIsByteIdentical(t *testing.T) {
	first := drawingBaseContract([]any{
		drawingEntry("drw.beta", "beta", []string{"cmp.root", "cmp.bracket"}, embeddedResource(), []any{
			sheetEntry("sht.beta.two", "two", 2),
			sheetEntry("sht.beta.one", "one", 1),
		}),
		drawingEntry("drw.alpha", "alpha", []string{"cmp.bracket"}, separateResource("drawings/alpha.FCStd"), []any{
			sheetEntry("sht.alpha.one", "first", 1),
		}),
	})
	second := drawingBaseContract([]any{
		drawingEntry("drw.alpha", "alpha", []string{"cmp.bracket"}, separateResource("drawings/alpha.FCStd"), []any{
			sheetEntry("sht.alpha.one", "first", 1),
		}),
		drawingEntry("drw.beta", "beta", []string{"cmp.bracket", "cmp.root"}, embeddedResource(), []any{
			sheetEntry("sht.beta.one", "one", 1),
			sheetEntry("sht.beta.two", "two", 2),
		}),
	})

	left := canonical(t, mustParse(t, first))
	right := canonical(t, mustParse(t, second))
	if !bytes.Equal(left, right) {
		t.Fatalf("reordered equivalent input produced different canonical JSON\nleft:  %s\nright: %s", left, right)
	}

	repeated := canonical(t, mustParse(t, first))
	if !bytes.Equal(left, repeated) {
		t.Fatalf("repeated canonical JSON was not byte-stable\nfirst:    %s\nrepeated: %s", left, repeated)
	}

	roundTrip := canonical(t, mustParseBytes(t, left))
	if !bytes.Equal(left, roundTrip) {
		t.Fatalf("canonical JSON changed across round trip\nfirst:    %s\nroundTrip: %s", left, roundTrip)
	}
}

func TestDrawingsBreakContract(t *testing.T) {
	cases := []struct {
		name  string
		build func() map[string]any
		want  string
	}{
		{
			name:  "missing-drawings",
			build: func() map[string]any { return drawingContract(nil, false) },
			want:  "entities.drawings is required",
		},
		{
			name:  "null-drawings",
			build: func() map[string]any { return drawingContract(nil, true) },
			want:  "entities.drawings must be an array",
		},
		{
			name: "duplicate-drawing-id",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.dup", "a", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.a", "a", 1)}),
					drawingEntry("drw.dup", "b", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.b", "b", 1)}),
				})
			},
			want: `duplicates stable ID "drw.dup"`,
		},
		{
			name: "duplicate-sheet-id-across-entities",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("cmp.root", "one", 1)}),
				})
			},
			want: `duplicates stable ID "cmp.root"`,
		},
		{
			name: "duplicate-sheet-id-across-drawings",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.shared", "one", 1)}),
					drawingEntry("drw.two", "two", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.shared", "two", 1)}),
				})
			},
			want: `duplicates stable ID "sht.shared"`,
		},
		{
			name: "duplicate-component-id",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root", "cmp.root"}, embeddedResource(), []any{sheetEntry("sht.one", "one", 1)}),
				})
			},
			want: `duplicates component ID "cmp.root"`,
		},
		{
			name: "duplicate-sheet-order",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{
						sheetEntry("sht.one", "one", 1),
						sheetEntry("sht.two", "two", 1),
					}),
				})
			},
			want: "duplicates sheet order",
		},
		{
			name: "duplicate-drawing-name",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "shared", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.one", "one", 1)}),
					drawingEntry("drw.two", "shared", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.two", "two", 1)}),
				})
			},
			want: `name "shared" duplicates drawing name`,
		},
		{
			name: "duplicate-sheet-name-within-drawing",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{
						sheetEntry("sht.one", "same", 1),
						sheetEntry("sht.two", "same", 2),
					}),
				})
			},
			want: `name "same" duplicates sheet name`,
		},
		{
			name: "empty-component-ids",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{}, embeddedResource(), []any{sheetEntry("sht.one", "one", 1)}),
				})
			},
			want: "componentIds must not be empty",
		},
		{
			name: "missing-component-ids",
			build: func() map[string]any {
				drawing := sheetOnlyDrawing("drw.one", "one")
				return singleDrawingContract([]any{drawing})
			},
			want: "componentIds is required",
		},
		{
			name: "unresolved-component-id",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.missing"}, embeddedResource(), []any{sheetEntry("sht.one", "one", 1)}),
				})
			},
			want: `"cmp.missing" must resolve to an existing component`,
		},
		{
			name: "empty-sheets",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{}),
				})
			},
			want: "sheets must not be empty",
		},
		{
			name: "missing-sheets",
			build: func() map[string]any {
				drawing := drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{})
				delete(drawing, "sheets")
				return singleDrawingContract([]any{drawing})
			},
			want: "sheets is required",
		},
		{
			name: "missing-sheet-identity-source",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheetWithoutIdentitySource("sht.one", "one", 1)}),
				})
			},
			want: "identitySource is required",
		},
		{
			name: "blank-sheet-identity-source-kind",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{
						sheetWithIdentitySource("sht.one", "one", 1, map[string]any{"kind": "", "nativeRef": "Page"}),
					}),
				})
			},
			want: "identitySource.kind is required",
		},
		{
			name: "wrong-sheet-identity-source-kind",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{
						sheetWithIdentitySource("sht.one", "one", 1, map[string]any{"kind": "owner_scoped_key", "nativeRef": "Page"}),
					}),
				})
			},
			want: `identitySource.kind must be "native_ref"`,
		},
		{
			name: "blank-sheet-native-ref",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{
						sheetWithIdentitySource("sht.one", "one", 1, map[string]any{"kind": "native_ref", "nativeRef": ""}),
					}),
				})
			},
			want: "identitySource.nativeRef is required",
		},
		{
			name: "zero-sheet-order",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.one", "one", 0)}),
				})
			},
			want: "order must be a positive integer",
		},
		{
			name: "embedded-with-path",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, map[string]any{"topology": "embedded", "path": "drawings/x.FCStd"}, []any{sheetEntry("sht.one", "one", 1)}),
				})
			},
			want: `resource.path must be empty when topology is "embedded"`,
		},
		{
			name: "separate-without-path",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, map[string]any{"topology": "separate"}, []any{sheetEntry("sht.one", "one", 1)}),
				})
			},
			want: `resource.path is required when topology is "separate"`,
		},
		{
			name:  "separate-absolute-path",
			build: func() map[string]any { return separatePathContract("/drawings/x.FCStd") },
			want:  "invalid canonical relative coordinate",
		},
		{
			name:  "separate-traversal-path",
			build: func() map[string]any { return separatePathContract("../drawings/x.FCStd") },
			want:  "invalid canonical relative coordinate",
		},
		{
			name:  "separate-backslash-path",
			build: func() map[string]any { return separatePathContract(`drawings\x.FCStd`) },
			want:  "invalid canonical relative coordinate",
		},
		{
			name:  "separate-non-clean-path",
			build: func() map[string]any { return separatePathContract("drawings//x.FCStd") },
			want:  "invalid canonical relative coordinate",
		},
		{
			name: "unknown-drawing-field",
			build: func() map[string]any {
				drawing := drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.one", "one", 1)})
				drawing["extra"] = true
				return singleDrawingContract([]any{drawing})
			},
			want: `entities.drawings[0] has unknown field "extra"`,
		},
		{
			name: "unknown-resource-field",
			build: func() map[string]any {
				resource := embeddedResource()
				resource["extra"] = true
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, resource, []any{sheetEntry("sht.one", "one", 1)}),
				})
			},
			want: `entities.drawings[0].resource has unknown field "extra"`,
		},
		{
			name: "unknown-sheet-field",
			build: func() map[string]any {
				sheet := sheetEntry("sht.one", "one", 1)
				sheet["extra"] = true
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheet}),
				})
			},
			want: `entities.drawings[0].sheets[0] has unknown field "extra"`,
		},
		{
			name: "invalid-drawing-identity-source-shape",
			build: func() map[string]any {
				drawing := drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.one", "one", 1)})
				drawing["identitySource"] = "not-an-object"
				return singleDrawingContract([]any{drawing})
			},
			want: "identitySource must be an object",
		},
		{
			name: "invalid-drawing-identity-source-kind",
			build: func() map[string]any {
				drawing := drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.one", "one", 1)})
				drawing["identitySource"] = map[string]any{"kind": ""}
				return singleDrawingContract([]any{drawing})
			},
			want: "identitySource.kind is required",
		},
		{
			name: "invalid-drawing-identity-source-native-ref",
			build: func() map[string]any {
				drawing := drawingEntry("drw.one", "one", []string{"cmp.root"}, embeddedResource(), []any{sheetEntry("sht.one", "one", 1)})
				drawing["identitySource"] = map[string]any{"kind": "native_ref"}
				return singleDrawingContract([]any{drawing})
			},
			want: "identitySource.nativeRef is required",
		},
		{
			name: "unknown-resource-topology",
			build: func() map[string]any {
				return singleDrawingContract([]any{
					drawingEntry("drw.one", "one", []string{"cmp.root"}, map[string]any{"topology": "linked"}, []any{sheetEntry("sht.one", "one", 1)}),
				})
			},
			want: `resource.topology must be one of "embedded" or "separate"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.build())
			if err != nil {
				t.Fatalf("json.Marshal returned error: %v", err)
			}
			_, err = Parse(data)
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func drawingBaseContract(drawings any) map[string]any {
	return drawingContract(drawings, true)
}

func drawingContract(drawings any, includeDrawings bool) map[string]any {
	entities := map[string]any{
		"components":      drawingComponents(),
		"features":        []any{},
		"relationships":   []any{},
		"parameterGroups": []any{},
		"parameters":      []any{},
		"metadata":        []any{},
	}
	if includeDrawings {
		entities["drawings"] = drawings
	}
	return map[string]any{
		"schemaVersion":  "1.0",
		"captureId":      "cap.drawings",
		"adapter":        map[string]any{"name": "freecad", "version": ""},
		"cadSystem":      map[string]any{"name": "FreeCAD", "version": ""},
		"sourceDocument": map[string]any{"logicalId": "m", "path": "input/M.FCStd", "fingerprint": "sha256:x"},
		"rootProduct":    map[string]any{"id": "cmp.root"},
		"annotations":    map[string]any{"description": "", "comment": "", "purpose": ""},
		"entities":       entities,
		"structure": map[string]any{
			"rootComponentId": "cmp.root",
			"nodes": []any{
				map[string]any{"componentId": "cmp.root", "parentComponentId": "", "children": []any{"cmp.bracket"}},
				map[string]any{"componentId": "cmp.bracket", "parentComponentId": "cmp.root", "children": []any{}},
			},
		},
	}
}

func drawingComponents() []any {
	return []any{
		componentEntry("cmp.root", "assembly", "Root"),
		componentEntry("cmp.bracket", "part", "Bracket"),
	}
}

func componentEntry(id, kind, name string) map[string]any {
	cadType := "Part::Feature"
	if kind == "assembly" {
		cadType = "App::Part"
	}
	return map[string]any{
		"id":             id,
		"kind":           kind,
		"name":           name,
		"displayName":    name,
		"cadType":        cadType,
		"quantity":       1,
		"material":       "",
		"identitySource": map[string]any{"kind": "parent_scoped_path", "path": id},
		"targetability":  map[string]any{"suppress": false, "unsuppress": false, "hide": true, "unhide": true, "delete": false},
		"annotations":    map[string]any{"description": "", "comment": "", "purpose": ""},
	}
}

func drawingEntry(id, name string, componentIDs any, resource map[string]any, sheets any) map[string]any {
	return map[string]any{
		"id":           id,
		"name":         name,
		"displayName":  "",
		"componentIds": componentIDs,
		"resource":     resource,
		"sheets":       sheets,
	}
}

func sheetEntry(id, name string, order int) map[string]any {
	return sheetWithIdentitySource(id, name, order, map[string]any{"kind": "native_ref", "nativeRef": "Page"})
}

func sheetWithIdentitySource(id, name string, order int, identitySource map[string]any) map[string]any {
	return map[string]any{
		"id":             id,
		"name":           name,
		"displayName":    "",
		"order":          order,
		"cadType":        "TechDraw::DrawPage",
		"identitySource": identitySource,
	}
}

func sheetWithoutIdentitySource(id, name string, order int) map[string]any {
	sheet := sheetEntry(id, name, order)
	delete(sheet, "identitySource")
	return sheet
}

func embeddedResource() map[string]any {
	return map[string]any{"topology": "embedded"}
}

func separateResource(path string) map[string]any {
	return map[string]any{"topology": "separate", "path": path}
}

func separatePathContract(path string) map[string]any {
	return singleDrawingContract([]any{
		drawingEntry("drw.one", "one", []string{"cmp.root"}, separateResource(path), []any{sheetEntry("sht.one", "one", 1)}),
	})
}

func singleDrawingContract(drawings []any) map[string]any {
	return drawingBaseContract(drawings)
}

// sheetOnlyDrawing intentionally omits componentIds so the parser reports the
// required field while the rest of the drawing shape stays valid.
func sheetOnlyDrawing(id, name string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        name,
		"displayName": "",
		"resource":    embeddedResource(),
		"sheets":      []any{sheetEntry("sht.one", "one", 1)},
	}
}

func mustParse(t *testing.T, contract map[string]any) *CADContract {
	t.Helper()
	data, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	return parsed
}

func mustParseBytes(t *testing.T, data []byte) *CADContract {
	t.Helper()
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	return parsed
}

func canonical(t *testing.T, contract *CADContract) []byte {
	t.Helper()
	data, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	return data
}
