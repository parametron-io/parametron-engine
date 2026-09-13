package freecad

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
)

// Phase 5 Task 13 Stage 2 permanent adapter closure: explicit non-leakage and
// combined-compatibility regressions that the mature Task 11 suite covers
// only indirectly (via directly-constructed planner fixtures whose types
// cannot carry semantic identity in the first place, so they cannot prove a
// negative about a real semantic pipeline).
//
// task13ForbiddenRuntimeKeys / task13ForbiddenRuntimeSubstrings are collected
// from the real Task 9/10/11 pipeline output, not guessed: they are the exact
// semantic identifier / routing field names and values that a defective
// projector could leak.

// jsonKeySet walks a decoded JSON value and collects every object key it
// contains, at any depth, so leakage assertions inspect actual decoded keys
// rather than raw substrings (a native Object name may coincidentally
// contain words like "Feature").
func jsonKeySet(t *testing.T, value any, into map[string]bool) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for k, sub := range v {
			into[k] = true
			jsonKeySet(t, sub, into)
		}
	case []any:
		for _, sub := range v {
			jsonKeySet(t, sub, into)
		}
	}
}

func decodeRuntimeManifestJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("invalid runtime manifest JSON: %v\n%s", err, data)
	}
	return decoded
}

// mutationFamilyEntries decodes top[section][family] as a slice of decoded
// entry objects, or nil if the section/family is absent.
func mutationFamilyEntries(t *testing.T, top map[string]any, section, family string) []map[string]any {
	t.Helper()
	sectionValue, ok := top[section]
	if !ok {
		return nil
	}
	sectionMap, ok := sectionValue.(map[string]any)
	if !ok {
		t.Fatalf("section %q is not an object: %#v", section, sectionValue)
	}
	familyValue, ok := sectionMap[family]
	if !ok {
		return nil
	}
	familySlice, ok := familyValue.([]any)
	if !ok {
		t.Fatalf("family %q is not an array: %#v", family, familyValue)
	}
	entries := make([]map[string]any, 0, len(familySlice))
	for _, raw := range familySlice {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("family %q entry is not an object: %#v", family, raw)
		}
		entries = append(entries, entry)
	}
	return entries
}

func entryKeySet(entry map[string]any) []string {
	keys := make([]string, 0, len(entry))
	for k := range entry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Gap 2 -- runtime semantic-ID non-leakage
// ---------------------------------------------------------------------------

// TestTask13Adapter_RuntimeMutationEntriesDoNotSerializeSemanticID drives the
// real Task 9/10/11 pipeline (semantic model with known, deliberately
// distinct SemanticIDs -> DSL target actions -> planner -> adapter
// projection/serialization) and proves the serialized runtime mutation
// subtrees carry only canonical native fields: no semantic identifier field
// name or value survives.
func TestTask13Adapter_RuntimeMutationEntriesDoNotSerializeSemanticID(t *testing.T) {
	payload := mutationDeterminismManifestPayload(t,
		"    target Pad: action = suppress\n    target Slot: action = hide\n    target Leg: action = delete")

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	data, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Sanity: the fixture's private semantic IDs (from mutationDeterminismModel)
	// are distinct from the native Object names they resolve to.
	for _, semanticID := range []string{"feat.pad", "feat.slot", "cmp.leg"} {
		if strings.Contains(string(data), semanticID) {
			t.Fatalf("runtime manifest leaked private semantic identifier %q:\n%s", semanticID, data)
		}
	}

	decoded := decodeRuntimeManifestJSON(t, data)
	allKeys := map[string]bool{}
	jsonKeySet(t, decoded, allKeys)
	for _, forbidden := range []string{"semanticId", "semanticID", "targetSemanticID", "targetSemanticId", "SemanticID", "TargetSemanticID"} {
		if allKeys[forbidden] {
			t.Fatalf("runtime manifest JSON carries forbidden semantic-identity key %q:\n%s", forbidden, data)
		}
	}

	top := map[string]any(decoded)
	suppression := mutationFamilyEntries(t, top, "partMutations", "suppression")
	if len(suppression) != 1 || !equalStringSlices(entryKeySet(suppression[0]), []string{"object", "suppressed"}) {
		t.Fatalf("partMutations.suppression entries not closed to {object,suppressed}: %#v", suppression)
	}
	visibility := mutationFamilyEntries(t, top, "partMutations", "visibility")
	if len(visibility) != 1 || !equalStringSlices(entryKeySet(visibility[0]), []string{"object", "visible"}) {
		t.Fatalf("partMutations.visibility entries not closed to {object,visible}: %#v", visibility)
	}
	deletion := mutationFamilyEntries(t, top, "partMutations", "deletion")
	if len(deletion) != 1 || !equalStringSlices(entryKeySet(deletion[0]), []string{"object"}) {
		t.Fatalf("partMutations.deletion entries not closed to {object}: %#v", deletion)
	}
}

// ---------------------------------------------------------------------------
// Gap 3 -- runtime target-kind non-leakage
// ---------------------------------------------------------------------------

// TestTask13Adapter_RuntimeMutationEntriesDoNotSerializeTargetKind proves the
// same real pipeline never serializes routing/classification metadata
// (Feature/Component semantic entity kind, TargetEntityKind, Scope) into the
// runtime mutation entries -- checked via decoded JSON key sets, not
// substring matching, since a native Object name may coincidentally contain
// a word like "Feature".
func TestTask13Adapter_RuntimeMutationEntriesDoNotSerializeTargetKind(t *testing.T) {
	payload := mutationDeterminismManifestPayload(t,
		"    target Pad: action = suppress\n    target Slot: action = hide\n    target Leg: action = delete")

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	data, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	decoded := decodeRuntimeManifestJSON(t, data)
	allKeys := map[string]bool{}
	jsonKeySet(t, decoded, allKeys)
	for _, forbidden := range []string{"targetKind", "targetEntityKind", "TargetEntityKind", "scope", "Scope", "semanticEntityKind", "SemanticEntityKind"} {
		if allKeys[forbidden] {
			t.Fatalf("runtime manifest JSON carries forbidden target-kind/routing key %q:\n%s", forbidden, data)
		}
	}

	top := map[string]any(decoded)
	for _, family := range []string{"suppression", "visibility", "deletion"} {
		for _, entry := range mutationFamilyEntries(t, top, "partMutations", family) {
			keys := entryKeySet(entry)
			for _, key := range keys {
				if key != "object" && key != "suppressed" && key != "visible" {
					t.Fatalf("partMutations.%s entry key set not closed to the Task 11 runtime contract: %#v", family, entry)
				}
			}
		}
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Gap 4 -- combined parameter/property compatibility
// ---------------------------------------------------------------------------

// TestTask13Adapter_CombinedParameterAndPropertyCompatibility exercises a
// single schema-2.0 payload that carries, simultaneously: a top-level
// parameterAssignments scalar write, internal Parameters metadata, internal
// Properties metadata, and a suppression/visibility/deletion mutation --
// across both Part and Assembly sections -- and proves the runtime
// projection keeps exactly the top-level scalar write and the three runtime
// families, with nested Parameters/Properties metadata filtered out and no
// value written through two surfaces at once.
func TestTask13Adapter_CombinedParameterAndPropertyCompatibility(t *testing.T) {
	const leakedPropertyValue = "combined-compat-do-not-leak"

	payload := v2MutationPayload(func(p *planner.WriteExportManifestPayload) {
		p.ParameterAssignments = []planner.ExportManifestParameterAssignment{
			{Name: "width", Target: "Box.Width", Value: 50, Type: "number", Unit: "mm"},
		}
		p.PartMutations = &planner.ExportManifestMutationCollection{
			Parameters:  []planner.ExportManifestParameterMutation{{Object: "Box", Property: "Height", ValueParam: "height", Type: "number", Unit: "mm"}},
			Properties:  []planner.ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: leakedPropertyValue}},
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}},
		}
		p.AssemblyMutations = &planner.ExportManifestMutationCollection{
			Parameters:  []planner.ExportManifestParameterMutation{{Object: "SubAsm", Property: "Offset", ValueParam: "offset", Type: "number", Unit: "mm"}},
			Properties:  []planner.ExportManifestPropertyMutation{{Object: "SubAsm", Property: "Label", Value: leakedPropertyValue}},
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "SubAsm1", Suppressed: false}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "SubAsm2", Visible: true}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "SubAsm3"}},
		}
	})

	manifest, data := mustProjectV2(t, payload)

	if manifest.SchemaVersion != "2.0" {
		t.Fatalf("expected schemaVersion 2.0, got %q", manifest.SchemaVersion)
	}
	if len(manifest.ParameterAssignments) != 1 {
		t.Fatalf("expected exactly one top-level parameter assignment: %#v", manifest.ParameterAssignments)
	}
	assignment := manifest.ParameterAssignments[0]
	if assignment.Target != "Box.Width" {
		t.Fatalf("expected top-level parameterAssignments target Box.Width, got %#v", assignment)
	}
	if value, ok := assignment.Value.(float64); !ok || value != 50 {
		t.Fatalf("expected top-level parameterAssignments value 50, got %#v", assignment.Value)
	}

	decoded := decodeRuntimeManifestJSON(t, data)
	top := map[string]any(decoded)

	for _, section := range []string{"partMutations", "assemblyMutations"} {
		sectionValue, ok := top[section]
		if !ok {
			t.Fatalf("expected %s section to be present", section)
		}
		sectionMap, ok := sectionValue.(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object: %#v", section, sectionValue)
		}
		for _, forbidden := range []string{"parameters", "properties"} {
			if _, ok := sectionMap[forbidden]; ok {
				t.Fatalf("%s leaked nested %q: %s", section, forbidden, data)
			}
		}
		for _, required := range []string{"suppression", "visibility", "deletion"} {
			if _, ok := sectionMap[required]; !ok {
				t.Fatalf("%s missing required family %q: %s", section, required, data)
			}
		}
	}

	if strings.Contains(string(data), leakedPropertyValue) {
		t.Fatalf("property metadata value leaked into runtime manifest: %s", data)
	}
	if strings.Contains(string(data), `"height"`) || strings.Contains(string(data), `"offset"`) {
		t.Fatalf("internal parameter value-param names leaked into runtime manifest: %s", data)
	}

	// The scalar-write target appears exactly once in the whole document: at
	// the top level, never duplicated into a mutation subtree.
	if got := strings.Count(string(data), "Box.Width"); got != 1 {
		t.Fatalf("expected scalar-write target \"Box.Width\" to appear exactly once, appeared %d times: %s", got, data)
	}
}
