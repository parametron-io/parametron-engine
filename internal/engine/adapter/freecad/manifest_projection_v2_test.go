package freecad

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
)

// Issue #16 adapter permanent contract: the FreeCAD runtime export manifest has
// one canonical schema, "1.0". Planner Part / Assembly mutation collections
// project structurally into optional runtime `assemblyMutations` /
// `partMutations` sections that contain ONLY the suppression / visibility /
// deletion families, with exact canonical entry shapes. The sections are
// omitted when no runtime mutations exist. Nested Parameters / Properties
// metadata is filtered out while top-level parameterAssignments remains the
// sole executable native scalar-write surface. Every other schemaVersion,
// including the retired "2.0", is rejected.

func canonicalMutationPayload(mut func(*planner.WriteExportManifestPayload)) planner.WriteExportManifestPayload {
	p := planner.WriteExportManifestPayload{
		SchemaVersion:          planner.ExportManifestSchemaVersion,
		ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
		SourceDocument:         "source/model.FCStd",
		Outputs:                []planner.ExportManifestOutput{},
	}
	if mut != nil {
		mut(&p)
	}
	return p
}

func mustProjectCanonical(t *testing.T, payload planner.WriteExportManifestPayload) (*FreeCADRuntimeExportManifest, []byte) {
	t.Helper()
	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected projection error: %v", err)
	}
	data, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	return manifest, data
}

func topLevelKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	return keys
}

func hasKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// PART F — runtime type contract
// ---------------------------------------------------------------------------

func TestTask11Adapter_RuntimeManifestTypeShape(t *testing.T) {
	rt := reflect.TypeOf(FreeCADRuntimeExportManifest{})
	wantFields := []struct{ name, tag string }{
		{"SchemaVersion", "schemaVersion"},
		{"SourceDocument", "sourceDocument"},
		{"ParameterAssignments", "parameterAssignments"},
		{"AssemblyMutations", "assemblyMutations,omitempty"},
		{"PartMutations", "partMutations,omitempty"},
		{"Outputs", "outputs"},
	}
	if rt.NumField() != len(wantFields) {
		t.Fatalf("FreeCADRuntimeExportManifest field count: got %d want %d", rt.NumField(), len(wantFields))
	}
	for i, want := range wantFields {
		f := rt.Field(i)
		if f.Name != want.name || f.Tag.Get("json") != want.tag {
			t.Fatalf("field %d: got %q/%q want %q/%q", i, f.Name, f.Tag.Get("json"), want.name, want.tag)
		}
	}

	col := reflect.TypeOf(FreeCADRuntimeMutationCollection{})
	wantCol := []struct{ name, tag string }{
		{"Suppression", "suppression,omitempty"},
		{"Visibility", "visibility,omitempty"},
		{"Deletion", "deletion,omitempty"},
	}
	if col.NumField() != len(wantCol) {
		t.Fatalf("FreeCADRuntimeMutationCollection field count: got %d want %d", col.NumField(), len(wantCol))
	}
	for i, want := range wantCol {
		f := col.Field(i)
		if f.Name != want.name || f.Tag.Get("json") != want.tag {
			t.Fatalf("collection field %d: got %q/%q want %q/%q", i, f.Name, f.Tag.Get("json"), want.name, want.tag)
		}
	}
	for _, forbidden := range []string{"Parameters", "Properties"} {
		if _, ok := col.FieldByName(forbidden); ok {
			t.Fatalf("runtime mutation collection must not carry %q", forbidden)
		}
	}

	sup := reflect.TypeOf(FreeCADRuntimeSuppressionMutation{})
	if sup.NumField() != 2 || sup.Field(0).Tag.Get("json") != "object" || sup.Field(1).Tag.Get("json") != "suppressed" {
		t.Fatalf("suppression shape changed: %#v", sup)
	}
	vis := reflect.TypeOf(FreeCADRuntimeVisibilityMutation{})
	if vis.NumField() != 2 || vis.Field(0).Tag.Get("json") != "object" || vis.Field(1).Tag.Get("json") != "visible" {
		t.Fatalf("visibility shape changed: %#v", vis)
	}
	del := reflect.TypeOf(FreeCADRuntimeDeletionMutation{})
	if del.NumField() != 1 || del.Field(0).Tag.Get("json") != "object" {
		t.Fatalf("deletion shape changed: %#v", del)
	}
	for _, forbidden := range []string{"Force", "Cascade", "SemanticID", "TargetKind", "Scope", "DependencyPolicy"} {
		if _, ok := del.FieldByName(forbidden); ok {
			t.Fatalf("runtime deletion must not carry %q", forbidden)
		}
	}
}

// ---------------------------------------------------------------------------
// PART G — canonical schema 1.0 serialization
// ---------------------------------------------------------------------------

func TestTask11Adapter_MutationFreeTopLevelShape(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		SchemaVersion:  planner.ExportManifestSchemaVersion,
		SourceDocument: "source/model.FCStd",
		Outputs:        []planner.ExportManifestOutput{},
	}
	manifest, data := mustProjectCanonical(t, payload)

	if manifest.SchemaVersion != "1.0" {
		t.Fatalf("schemaVersion: %q", manifest.SchemaVersion)
	}
	keys := topLevelKeys(t, data)
	want := []string{"schemaVersion", "sourceDocument", "parameterAssignments", "outputs"}
	if len(keys) != len(want) {
		t.Fatalf("mutation-free top-level keys: got %v want %v", keys, want)
	}
	for _, k := range want {
		if !hasKey(keys, k) {
			t.Fatalf("mutation-free manifest missing key %q (got %v)", k, keys)
		}
	}
	if hasKey(keys, "assemblyMutations") || hasKey(keys, "partMutations") {
		t.Fatalf("mutation-free manifest leaked a mutation section: %v", keys)
	}
	if manifest.AssemblyMutations != nil || manifest.PartMutations != nil {
		t.Fatalf("mutation-free manifest struct carries mutation sections: %#v", manifest)
	}
	if !strings.Contains(string(data), `"parameterAssignments": []`) {
		t.Fatalf("parameterAssignments not an empty array: %s", data)
	}
	if !strings.Contains(string(data), `"outputs": []`) {
		t.Fatalf("outputs not an empty array: %s", data)
	}
}

// Schema 1.0 accepts and serializes every runtime mutation family; the mutation
// sections are optional fields of the canonical schema, not a separate schema.
// This replaces the retired expectation that 1.0 rejects runtime families.
func TestTask11Adapter_Schema1AcceptsRuntimeFamilies(t *testing.T) {
	families := map[string]struct {
		apply  func(*planner.ExportManifestMutationCollection)
		family string
		want   string
	}{
		"suppression": {
			apply: func(c *planner.ExportManifestMutationCollection) {
				c.Suppression = []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}
			},
			family: "suppression", want: `{"object":"Pad","suppressed":true}`,
		},
		"visibility": {
			apply: func(c *planner.ExportManifestMutationCollection) {
				c.Visibility = []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}}
			},
			family: "visibility", want: `{"object":"Body","visible":false}`,
		},
		"deletion": {
			apply: func(c *planner.ExportManifestMutationCollection) {
				c.Deletion = []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}}
			},
			family: "deletion", want: `{"object":"Chamfer"}`,
		},
	}
	for name, tc := range families {
		for _, mode := range []planner.ExportManifestProjectionMode{planner.ExportManifestProjectionModeFreeCADRuntimeNative, ""} {
			t.Run(name+"/mode="+string(mode), func(t *testing.T) {
				col := &planner.ExportManifestMutationCollection{}
				tc.apply(col)
				payload := planner.WriteExportManifestPayload{
					SchemaVersion:          planner.ExportManifestSchemaVersion,
					ManifestProjectionMode: mode,
					SourceDocument:         "source/model.FCStd",
					PartMutations:          col,
					Outputs:                []planner.ExportManifestOutput{},
				}
				manifest, data := mustProjectCanonical(t, payload)
				if manifest.SchemaVersion != "1.0" {
					t.Fatalf("schemaVersion: %q", manifest.SchemaVersion)
				}
				if manifest.PartMutations == nil || manifest.AssemblyMutations != nil {
					t.Fatalf("mutation intent dropped or misrouted: %#v", manifest)
				}
				var top map[string]json.RawMessage
				if err := json.Unmarshal(data, &top); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if string(top["schemaVersion"]) != `"1.0"` {
					t.Fatalf("schemaVersion: %s", top["schemaVersion"])
				}
				var section map[string]json.RawMessage
				if err := json.Unmarshal(top["partMutations"], &section); err != nil {
					t.Fatalf("invalid section: %v", err)
				}
				if len(section) != 1 {
					t.Fatalf("expected only the %q family, got %s", tc.family, top["partMutations"])
				}
				var entries []json.RawMessage
				if err := json.Unmarshal(section[tc.family], &entries); err != nil || len(entries) != 1 {
					t.Fatalf("family %q entries: %v (%s)", tc.family, err, section[tc.family])
				}
				var compact strings.Builder
				var v any
				_ = json.Unmarshal(entries[0], &v)
				b, _ := json.Marshal(v)
				compact.Write(b)
				if compact.String() != tc.want {
					t.Fatalf("entry: got %s want %s", compact.String(), tc.want)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// PART H / I — schema 1.0 mutation serialization + omitempty
// ---------------------------------------------------------------------------

func TestTask11Adapter_CanonicalMutationEntryShapes(t *testing.T) {
	cases := []struct {
		name    string
		apply   func(*planner.WriteExportManifestPayload)
		section string // "partMutations" or "assemblyMutations"
		family  string
		want    map[string]any
	}{
		{
			name: "part suppress",
			apply: func(p *planner.WriteExportManifestPayload) {
				p.PartMutations = &planner.ExportManifestMutationCollection{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}}
			},
			section: "partMutations", family: "suppression",
			want: map[string]any{"object": "Pad", "suppressed": true},
		},
		{
			name: "part unsuppress serializes explicit false",
			apply: func(p *planner.WriteExportManifestPayload) {
				p.PartMutations = &planner.ExportManifestMutationCollection{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: false}}}
			},
			section: "partMutations", family: "suppression",
			want: map[string]any{"object": "Pad", "suppressed": false},
		},
		{
			name: "part hide serializes explicit false",
			apply: func(p *planner.WriteExportManifestPayload) {
				p.PartMutations = &planner.ExportManifestMutationCollection{Visibility: []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}}}
			},
			section: "partMutations", family: "visibility",
			want: map[string]any{"object": "Body", "visible": false},
		},
		{
			name: "part unhide",
			apply: func(p *planner.WriteExportManifestPayload) {
				p.PartMutations = &planner.ExportManifestMutationCollection{Visibility: []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: true}}}
			},
			section: "partMutations", family: "visibility",
			want: map[string]any{"object": "Body", "visible": true},
		},
		{
			name: "part delete has object only",
			apply: func(p *planner.WriteExportManifestPayload) {
				p.PartMutations = &planner.ExportManifestMutationCollection{Deletion: []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}}}
			},
			section: "partMutations", family: "deletion",
			want: map[string]any{"object": "Chamfer"},
		},
		{
			name: "assembly suppress",
			apply: func(p *planner.WriteExportManifestPayload) {
				p.AssemblyMutations = &planner.ExportManifestMutationCollection{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "SubAsm", Suppressed: true}}}
			},
			section: "assemblyMutations", family: "suppression",
			want: map[string]any{"object": "SubAsm", "suppressed": true},
		},
		{
			name: "assembly visibility",
			apply: func(p *planner.WriteExportManifestPayload) {
				p.AssemblyMutations = &planner.ExportManifestMutationCollection{Visibility: []planner.ExportManifestVisibilityMutation{{Object: "SubAsm", Visible: false}}}
			},
			section: "assemblyMutations", family: "visibility",
			want: map[string]any{"object": "SubAsm", "visible": false},
		},
		{
			name: "assembly deletion",
			apply: func(p *planner.WriteExportManifestPayload) {
				p.AssemblyMutations = &planner.ExportManifestMutationCollection{Deletion: []planner.ExportManifestDeletionMutation{{Object: "SubAsm"}}}
			},
			section: "assemblyMutations", family: "deletion",
			want: map[string]any{"object": "SubAsm"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, data := mustProjectCanonical(t, canonicalMutationPayload(tc.apply))

			var top map[string]json.RawMessage
			if err := json.Unmarshal(data, &top); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if top["schemaVersion"] == nil || string(top["schemaVersion"]) != `"1.0"` {
				t.Fatalf("schemaVersion not 1.0: %s", top["schemaVersion"])
			}
			raw, ok := top[tc.section]
			if !ok {
				t.Fatalf("missing section %q: %s", tc.section, data)
			}
			var section map[string]json.RawMessage
			if err := json.Unmarshal(raw, &section); err != nil {
				t.Fatalf("invalid section JSON: %v", err)
			}
			// only the named family present
			for k := range section {
				if k != tc.family {
					t.Fatalf("section %q carries unexpected family %q: %s", tc.section, k, raw)
				}
			}
			var entries []map[string]any
			if err := json.Unmarshal(section[tc.family], &entries); err != nil {
				t.Fatalf("invalid family JSON: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("expected exactly one entry, got %v", entries)
			}
			if !reflect.DeepEqual(entries[0], tc.want) {
				t.Fatalf("entry mismatch: got %#v want %#v", entries[0], tc.want)
			}
		})
	}
}

func TestTask11Adapter_MixedPartAndAssemblyUnderSchema1(t *testing.T) {
	payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}},
		}
		p.AssemblyMutations = &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "SubAsm", Suppressed: false}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "SubAsm", Visible: true}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "OldAsm"}},
		}
	})
	manifest, data := mustProjectCanonical(t, payload)
	if manifest.SchemaVersion != "1.0" {
		t.Fatalf("schemaVersion: %q", manifest.SchemaVersion)
	}
	wantCol := &FreeCADRuntimeMutationCollection{
		Suppression: []FreeCADRuntimeSuppressionMutation{{Object: "Pad", Suppressed: true}},
		Visibility:  []FreeCADRuntimeVisibilityMutation{{Object: "Body", Visible: false}},
		Deletion:    []FreeCADRuntimeDeletionMutation{{Object: "Chamfer"}},
	}
	if !reflect.DeepEqual(manifest.PartMutations, wantCol) {
		t.Fatalf("part mutations: got %#v want %#v", manifest.PartMutations, wantCol)
	}
	wantAsm := &FreeCADRuntimeMutationCollection{
		Suppression: []FreeCADRuntimeSuppressionMutation{{Object: "SubAsm", Suppressed: false}},
		Visibility:  []FreeCADRuntimeVisibilityMutation{{Object: "SubAsm", Visible: true}},
		Deletion:    []FreeCADRuntimeDeletionMutation{{Object: "OldAsm"}},
	}
	if !reflect.DeepEqual(manifest.AssemblyMutations, wantAsm) {
		t.Fatalf("assembly mutations: got %#v want %#v", manifest.AssemblyMutations, wantAsm)
	}
	keys := topLevelKeys(t, data)
	for _, k := range []string{"schemaVersion", "sourceDocument", "parameterAssignments", "assemblyMutations", "partMutations", "outputs"} {
		if !hasKey(keys, k) {
			t.Fatalf("missing top-level key %q: %v", k, keys)
		}
	}
	if len(keys) != 6 {
		t.Fatalf("unexpected top-level keys: %v", keys)
	}
}

func TestTask11Adapter_OmitemptyContract(t *testing.T) {
	t.Run("part-only omits assemblyMutations", func(t *testing.T) {
		_, data := mustProjectCanonical(t, canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
			p.PartMutations = &planner.ExportManifestMutationCollection{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}}
		}))
		if hasKey(topLevelKeys(t, data), "assemblyMutations") {
			t.Fatalf("assemblyMutations should be omitted: %s", data)
		}
	})
	t.Run("assembly-only omits partMutations", func(t *testing.T) {
		_, data := mustProjectCanonical(t, canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
			p.AssemblyMutations = &planner.ExportManifestMutationCollection{Deletion: []planner.ExportManifestDeletionMutation{{Object: "SubAsm"}}}
		}))
		if hasKey(topLevelKeys(t, data), "partMutations") {
			t.Fatalf("partMutations should be omitted: %s", data)
		}
	})
	t.Run("present section omits empty families", func(t *testing.T) {
		_, data := mustProjectCanonical(t, canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
			p.PartMutations = &planner.ExportManifestMutationCollection{Visibility: []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: true}}}
		}))
		var top map[string]json.RawMessage
		_ = json.Unmarshal(data, &top)
		var section map[string]json.RawMessage
		if err := json.Unmarshal(top["partMutations"], &section); err != nil {
			t.Fatalf("invalid section: %v", err)
		}
		if _, ok := section["suppression"]; ok {
			t.Fatalf("empty suppression not omitted: %s", top["partMutations"])
		}
		if _, ok := section["deletion"]; ok {
			t.Fatalf("empty deletion not omitted: %s", top["partMutations"])
		}
	})
	t.Run("all-empty collections omit both sections", func(t *testing.T) {
		_, data := mustProjectCanonical(t, canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
			p.PartMutations = &planner.ExportManifestMutationCollection{}
			p.AssemblyMutations = &planner.ExportManifestMutationCollection{}
		}))
		keys := topLevelKeys(t, data)
		if hasKey(keys, "partMutations") || hasKey(keys, "assemblyMutations") {
			t.Fatalf("empty collections must not serialize a section: %v", keys)
		}
	})
}

// ---------------------------------------------------------------------------
// PART J — runtime filtering (Parameters / Properties never reach runtime)
// ---------------------------------------------------------------------------

func TestTask11Adapter_NestedParametersAndPropertiesAreFiltered(t *testing.T) {
	for _, section := range []string{"part", "assembly"} {
		t.Run(section, func(t *testing.T) {
			col := &planner.ExportManifestMutationCollection{
				Parameters:  []planner.ExportManifestParameterMutation{{Object: "Box", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"}},
				Properties:  []planner.ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "primary"}},
				Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
				Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
				Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}},
			}
			payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
				if section == "part" {
					p.PartMutations = col
				} else {
					p.AssemblyMutations = col
				}
			})
			manifest, data := mustProjectCanonical(t, payload)

			runtime := manifest.PartMutations
			key := "partMutations"
			if section == "assembly" {
				runtime = manifest.AssemblyMutations
				key = "assemblyMutations"
			}
			if runtime == nil {
				t.Fatalf("expected a %s section", key)
			}
			if len(runtime.Suppression) != 1 || len(runtime.Visibility) != 1 || len(runtime.Deletion) != 1 {
				t.Fatalf("runtime families not projected: %#v", runtime)
			}

			var top map[string]json.RawMessage
			if err := json.Unmarshal(data, &top); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			var sectionKeys map[string]json.RawMessage
			if err := json.Unmarshal(top[key], &sectionKeys); err != nil {
				t.Fatalf("invalid section: %v", err)
			}
			for _, forbidden := range []string{"parameters", "properties"} {
				if _, ok := sectionKeys[forbidden]; ok {
					t.Fatalf("%s section leaked nested %q: %s", key, forbidden, top[key])
				}
			}
			// scan the whole mutation subtree text for the nested keys
			if strings.Contains(string(top[key]), `"parameters"`) || strings.Contains(string(top[key]), `"properties"`) {
				t.Fatalf("%s subtree text leaked nested metadata: %s", key, top[key])
			}
			// "primary" / "Width" metadata values must not appear in the subtree
			if strings.Contains(string(top[key]), "primary") {
				t.Fatalf("%s subtree leaked a property value: %s", key, top[key])
			}
		})
	}
}

// Filtering does not destroy the internal Parameters metadata needed for
// implicit parameter target resolution: an assignment with no explicit target
// still resolves through the planner mutation Parameters, and the resolved
// Object.Property appears only in top-level parameterAssignments.
func TestTask11Adapter_ParameterTargetFallbackSurvivesFiltering(t *testing.T) {
	payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
		p.ParameterAssignments = []planner.ExportManifestParameterAssignment{
			{Name: "width", Value: 50, Type: "number", Unit: "mm"},
		}
		p.PartMutations = &planner.ExportManifestMutationCollection{
			Parameters:  []planner.ExportManifestParameterMutation{{Object: "Box", Property: "Width", ValueParam: "width"}},
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
		}
	})
	manifest, data := mustProjectCanonical(t, payload)
	if len(manifest.ParameterAssignments) != 1 || manifest.ParameterAssignments[0].Target != "Box.Width" {
		t.Fatalf("parameter target fallback lost: %#v", manifest.ParameterAssignments)
	}
	// The scalar write appears exactly once, at the top level.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.Contains(string(top["parameterAssignments"]), `"Box.Width"`) {
		t.Fatalf("expected resolved target in parameterAssignments: %s", top["parameterAssignments"])
	}
	if strings.Contains(string(top["partMutations"]), "Box.Width") || strings.Contains(string(top["partMutations"]), `"Width"`) {
		t.Fatalf("scalar-write target leaked into partMutations: %s", top["partMutations"])
	}
	if strings.Contains(string(top["partMutations"]), `"parameters"`) {
		t.Fatalf("nested parameters key present: %s", top["partMutations"])
	}
}

// ---------------------------------------------------------------------------
// PART K — schema validation
// ---------------------------------------------------------------------------

func TestTask11Adapter_SchemaValidationMatrix(t *testing.T) {
	suppression := func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}}
	}
	visibility := func(p *planner.WriteExportManifestPayload) {
		p.AssemblyMutations = &planner.ExportManifestMutationCollection{Visibility: []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}}}
	}
	deletion := func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{Deletion: []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}}}
	}
	paramsOnly := func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{Parameters: []planner.ExportManifestParameterMutation{{Object: "Box", Property: "Width", ValueParam: "width"}}}
	}
	propsOnly := func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{Properties: []planner.ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "primary"}}}
	}

	base := func(schema string, apply func(*planner.WriteExportManifestPayload)) planner.WriteExportManifestPayload {
		p := planner.WriteExportManifestPayload{
			SchemaVersion:          schema,
			ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
			SourceDocument:         "source/model.FCStd",
			Outputs:                []planner.ExportManifestOutput{},
		}
		if apply != nil {
			apply(&p)
		}
		return p
	}

	cases := []struct {
		name    string
		payload planner.WriteExportManifestPayload
		wantErr string
	}{
		{"1.0 no mutation", base("1.0", nil), ""},
		{"1.0 suppression accepted", base("1.0", suppression), ""},
		{"1.0 visibility accepted", base("1.0", visibility), ""},
		{"1.0 deletion accepted", base("1.0", deletion), ""},
		{"1.0 internal parameters only accepted", base("1.0", paramsOnly), ""},
		{"1.0 internal properties only accepted", base("1.0", propsOnly), ""},
		{"2.0 no mutation rejected", base("2.0", nil), `"2.0" is not supported`},
		{"2.0 suppression rejected", base("2.0", suppression), `"2.0" is not supported`},
		{"2.0 visibility rejected", base("2.0", visibility), `"2.0" is not supported`},
		{"2.0 deletion rejected", base("2.0", deletion), `"2.0" is not supported`},
		{"unsupported version 3.0", base("3.0", nil), `"3.0" is not supported`},
		{"unsupported version 3.0 with mutation", base("3.0", suppression), `"3.0" is not supported`},
		{"unsupported version 0.9", base("0.9", nil), `"0.9" is not supported`},
		{"empty version", base("", nil), "is required"},
		{"empty version with mutation", base("", suppression), "is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFreeCADRuntimeExportManifestSchema(tc.payload)
			projected, projectErr := ProjectFreeCADRuntimeExportManifest(tc.payload)
			if tc.wantErr == "" {
				if err != nil || projectErr != nil {
					t.Fatalf("expected acceptance, got validate=%v project=%v", err, projectErr)
				}
				if projected.SchemaVersion != "1.0" {
					t.Fatalf("projected schemaVersion: %q", projected.SchemaVersion)
				}
				return
			}
			for _, got := range []error{err, projectErr} {
				if got == nil || !strings.Contains(got.Error(), tc.wantErr) {
					t.Fatalf("error %v does not contain %q", got, tc.wantErr)
				}
				var pe *FreeCADRuntimeManifestProjectionError
				if !errors.As(got, &pe) || pe.Field != "schemaVersion" {
					t.Fatalf("expected typed schemaVersion projection error, got %#v", got)
				}
			}
			if projected != nil {
				t.Fatalf("rejected payload produced a manifest: %#v", projected)
			}
		})
	}
}

// A rejected schema is never silently converted to 1.0, with or without
// mutations, and the projector never rewrites an accepted schema either.
func TestTask11Adapter_ProjectorDoesNotSilentlyConvertSchema(t *testing.T) {
	for _, schema := range []string{"2.0", "3.0", ""} {
		payload := planner.WriteExportManifestPayload{
			SchemaVersion:          schema,
			ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
			SourceDocument:         "source/model.FCStd",
			PartMutations: &planner.ExportManifestMutationCollection{
				Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
			},
			Outputs: []planner.ExportManifestOutput{},
		}
		manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
		if err == nil || manifest != nil {
			t.Fatalf("schema %q: expected rejection, got %#v / %v", schema, manifest, err)
		}
	}
}

// The projector never selects a schema from mutation content: the same
// mutation-bearing payload with a non-canonical schema is rejected, and with the
// canonical schema is projected as 1.0.
func TestTask11Adapter_SchemaIsNotSelectedByMutationPresence(t *testing.T) {
	mutated := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{
			Deletion: []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}},
		}
	})
	plain := canonicalMutationPayload(nil)
	for name, payload := range map[string]planner.WriteExportManifestPayload{"mutation-bearing": mutated, "mutation-free": plain} {
		manifest, _ := mustProjectCanonical(t, payload)
		if manifest.SchemaVersion != "1.0" {
			t.Fatalf("%s: schemaVersion %q", name, manifest.SchemaVersion)
		}
	}
}

// ---------------------------------------------------------------------------
// PART P — object / destination preservation
// ---------------------------------------------------------------------------

func TestTask11Adapter_ObjectSurvivesVerbatim(t *testing.T) {
	const oddName = "PartDesign::Odd_Name-7"
	_, data := mustProjectCanonical(t, canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: oddName, Suppressed: true}},
		}
	}))
	if !strings.Contains(string(data), `"object": "`+oddName+`"`) {
		t.Fatalf("native object not preserved verbatim: %s", data)
	}
}

// ---------------------------------------------------------------------------
// PART R — determinism
// ---------------------------------------------------------------------------

func TestTask11Adapter_MutationBearingBytesAreDeterministic(t *testing.T) {
	payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}, {Object: "Rib", Suppressed: false}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}},
		}
		p.AssemblyMutations = &planner.ExportManifestMutationCollection{
			Visibility: []planner.ExportManifestVisibilityMutation{{Object: "SubAsm", Visible: true}},
		}
	})
	_, first := mustProjectCanonical(t, payload)
	if !strings.Contains(string(first), `"schemaVersion": "1.0"`) {
		t.Fatalf("bytes do not carry canonical schema: %s", first)
	}
	for i := 0; i < 10; i++ {
		_, got := mustProjectCanonical(t, payload)
		if string(got) != string(first) {
			t.Fatalf("iteration %d: bytes drifted:\n%s\nvs\n%s", i, got, first)
		}
	}
}

func TestTask11Adapter_RejectionDiagnosticsAreDeterministic(t *testing.T) {
	schema2WithSuppression := planner.WriteExportManifestPayload{
		SchemaVersion:          "2.0",
		ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
		SourceDocument:         "source/model.FCStd",
		PartMutations: &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
		},
		Outputs: []planner.ExportManifestOutput{},
	}
	unsupported := planner.WriteExportManifestPayload{
		SchemaVersion:          "3.0",
		ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
		SourceDocument:         "source/model.FCStd",
		Outputs:                []planner.ExportManifestOutput{},
	}
	for _, tc := range []struct {
		name    string
		payload planner.WriteExportManifestPayload
	}{
		{"schema-2 rejection", schema2WithSuppression},
		{"unsupported schema rejection", unsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, first := ProjectFreeCADRuntimeExportManifest(tc.payload)
			if first == nil {
				t.Fatal("expected an error")
			}
			for i := 0; i < 10; i++ {
				_, err := ProjectFreeCADRuntimeExportManifest(tc.payload)
				if err == nil || err.Error() != first.Error() {
					t.Fatalf("iteration %d: diagnostic drift: %v vs %v", i, err, first)
				}
			}
		})
	}
}
