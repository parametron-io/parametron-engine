package handoff

import (
	"reflect"
	"testing"

	"parametron/internal/authoring/planner"
)

// Phase 5 Task 11 handoff permanent contract: cloneWriteExportManifestPayload /
// cloneMutationCollection carry every mutation family — including the Task 11
// Visibility and Deletion families — through the handoff snapshot, defensively
// (mutating the original slices after the clone must not disturb the clone) and
// without inventing empty slices where the source had nil.

func fullMutationCollection() *planner.ExportManifestMutationCollection {
	return &planner.ExportManifestMutationCollection{
		Parameters:  []planner.ExportManifestParameterMutation{{Object: "Box", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"}},
		Properties:  []planner.ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "primary"}},
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
		Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
		Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}},
	}
}

func TestTask11Handoff_CloneMutationCollectionPreservesAllFamilies(t *testing.T) {
	in := fullMutationCollection()
	out := cloneMutationCollection(in)
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("clone dropped or altered a family:\ngot:  %#v\nwant: %#v", out, in)
	}
}

func TestTask11Handoff_ClonedPayloadCarriesBothBucketsWithEveryFamily(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		ProductKey:             "widget",
		ManifestFilename:       planner.ExportManifestFilename,
		ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
		SchemaVersion:          planner.FreeCADRuntimeMutationManifestSchemaVersion,
		Adapter:                "freecad",
		SourceDocument:         "source/model.FCStd",
		AssemblyMutations:      fullMutationCollection(),
		PartMutations:          fullMutationCollection(),
	}
	clone := cloneWriteExportManifestPayload(payload)
	if !reflect.DeepEqual(clone.AssemblyMutations, payload.AssemblyMutations) {
		t.Fatalf("assembly bucket mismatch: %#v", clone.AssemblyMutations)
	}
	if !reflect.DeepEqual(clone.PartMutations, payload.PartMutations) {
		t.Fatalf("part bucket mismatch: %#v", clone.PartMutations)
	}
	if clone.SchemaVersion != planner.FreeCADRuntimeMutationManifestSchemaVersion {
		t.Fatalf("schema version not carried: %q", clone.SchemaVersion)
	}
}

func TestTask11Handoff_CloneIsDefensive(t *testing.T) {
	in := fullMutationCollection()
	out := cloneMutationCollection(in)

	// Mutate every slice in the original after the clone.
	in.Parameters[0].Object = "MUTATED"
	in.Properties[0].Value = "MUTATED"
	in.Suppression[0].Suppressed = false
	in.Visibility[0].Visible = true
	in.Deletion[0].Object = "MUTATED"
	in.Parameters = append(in.Parameters, planner.ExportManifestParameterMutation{Object: "Extra"})
	in.Visibility = append(in.Visibility, planner.ExportManifestVisibilityMutation{Object: "Extra"})
	in.Deletion = append(in.Deletion, planner.ExportManifestDeletionMutation{Object: "Extra"})

	want := fullMutationCollection()
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("clone was disturbed by mutating the original:\ngot:  %#v\nwant: %#v", out, want)
	}
}

func TestTask11Handoff_ClonePreservesNilAndEmptySemantics(t *testing.T) {
	if got := cloneMutationCollection(nil); got != nil {
		t.Fatalf("cloning a nil collection must stay nil, got %#v", got)
	}

	// A collection whose families are all nil clones without inventing
	// non-nil empty slices for the value-typed families.
	empty := &planner.ExportManifestMutationCollection{}
	out := cloneMutationCollection(empty)
	if out == nil {
		t.Fatal("cloning a non-nil empty collection must stay non-nil")
	}
	if out.Parameters != nil || out.Properties != nil || out.Suppression != nil || out.Visibility != nil || out.Deletion != nil {
		t.Fatalf("clone invented empty slices: %#v", out)
	}
}

func TestTask11Handoff_MutationFamiliesRoundTripThroughPackagePlan(t *testing.T) {
	steps := []planner.Step{
		csvStep("widget", "widget.csv"),
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:             "widget",
				ManifestFilename:       planner.ExportManifestFilename,
				ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
				SchemaVersion:          planner.FreeCADRuntimeMutationManifestSchemaVersion,
				PlanHash:               "plan-widget",
				Adapter:                "freecad",
				Product:                planner.ExportManifestProduct{ID: "widget"},
				Values:                 map[string]interface{}{"width": 42.0},
				PartMutations: &planner.ExportManifestMutationCollection{
					Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
					Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
					Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}},
				},
				AssemblyMutations: &planner.ExportManifestMutationCollection{
					Visibility: []planner.ExportManifestVisibilityMutation{{Object: "SubAsm", Visible: true}},
				},
				Outputs: []planner.ExportManifestOutput{{Type: "step", Filename: "widget.step", Object: "Body"}},
			},
		},
		runStep("widget", "widget.csv", planner.ExportManifestFilename),
	}

	pkg := mustPackage(t, mustJob(t, "widget", steps))
	plan := pkg.Plan()
	if !reflect.DeepEqual(plan.Steps, steps) {
		t.Fatalf("mutation families did not round trip through the handoff package:\ngot:  %#v\nwant: %#v", plan.Steps, steps)
	}
}
