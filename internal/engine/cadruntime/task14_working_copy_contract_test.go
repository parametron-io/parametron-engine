package cadruntime

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"parametron/internal/engine/observed"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

// Task 14 permanent regression coverage for the working-copy observation
// contract: the exact meaning of working_copy_path (the Task 7 attempt
// root, not the prepared source document), and the fact that Engine
// verification accepts evidence shaped like the real FreeCAD sibling
// wrapper while rejecting every other working-copy reference shape.
//
// These tests exercise the real Task 8/10/11 production functions
// (ComposeFreeCADRuntimeObservationRequest and
// InvokeAndVerifyFreeCADRuntime) rather than reimplementing their logic;
// see freecad_observation_request_test.go and
// freecad_runtime_verification_test.go for the exhaustive Task 8/11
// contract coverage this file does not repeat.

func TestTask14ObservationRequest_WorkingCopyPathIsAttemptRoot(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	attempt := prepareObservationSource(t, req, []byte("attempt-root-evidence"))

	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	reference := got.Contract.Expected.References[0]
	if reference.Kind != "working_copy_path" || reference.Name != attempt.Layout.WorkingCopyDir {
		t.Fatalf("working_copy_path=%#v want attempt root %q", reference, attempt.Layout.WorkingCopyDir)
	}
	if reference.Name == attempt.Layout.SourceDocumentPath {
		t.Fatal("working_copy_path must not equal the prepared source file")
	}
	if reference.Name == req.Manifest.Inputs.SourceModel {
		t.Fatal("working_copy_path must not equal the host source model")
	}
}

func TestTask14ObservationRequest_PreservesPreparedSourceEvidence(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepared := []byte("prepared-source-evidence")
	attempt := prepareObservationSource(t, req, prepared)

	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	if got.Manifest.Manifest.SourceDocument != filepath.ToSlash(attempt.Layout.SourceDocument) {
		t.Fatalf("source-document intent=%q want=%q", got.Manifest.Manifest.SourceDocument, attempt.Layout.SourceDocument)
	}
	if len(got.Contract.Expected.Metadata) != 1 || got.Contract.Expected.Metadata[0].Key != "working_copy_sha256" {
		t.Fatalf("prepared-source digest metadata missing: %#v", got.Contract.Expected.Metadata)
	}
	wantIDs := map[string]bool{}
	for _, p := range req.Manifest.Verification.ExpectedParameters {
		wantIDs[p.ID] = true
	}
	if len(got.Contract.Expected.Parameters) != len(wantIDs) {
		t.Fatalf("expected parameter count=%d want=%d", len(got.Contract.Expected.Parameters), len(wantIDs))
	}
	for _, p := range got.Contract.Expected.Parameters {
		if !wantIDs[p.ID] {
			t.Fatalf("unexpected requested category id=%q", p.ID)
		}
	}
}

func TestTask14Verification_AcceptsSiblingWorkingCopyRootEvidence(t *testing.T) {
	req := verify11MultiParamRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatalf("expected sibling-compatible evidence to verify, got: %v", err)
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusPass {
		t.Fatalf("verification=%#v", run.Verification)
	}
	if run.Runtime.Observed.WorkingCopy.Path != layout.WorkingCopyDir {
		t.Fatalf("observed working-copy path=%q want attempt root %q", run.Runtime.Observed.WorkingCopy.Path, layout.WorkingCopyDir)
	}
	if fake.calls != 1 {
		t.Fatalf("expected exactly one runtime invocation, got %d", fake.calls)
	}
}

func TestTask14Verification_RejectsWrongWorkingCopyReferences(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	cases := map[string]string{
		"prepared source":                    layout.SourceDocumentPath,
		"parent root":                        filepath.Dir(layout.WorkingCopyDir),
		"another attempt":                    layout.WorkingCopyDir + "-attempt-000002",
		"relative path":                      "relative/attempt",
		"noncanonical equivalent":            layout.WorkingCopyDir + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(layout.WorkingCopyDir),
		"sibling path outside attempt root": filepath.Join(filepath.Dir(layout.WorkingCopyDir), "unrelated-sibling"),
	}
	for name, reference := range cases {
		t.Run(name, func(t *testing.T) {
			build := func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
				obs := verify11BuildPassObserved(t, req, layout, got, digest)
				obs.Observation.References[0].Name = reference
				return obs
			}
			fake := verify11Capability(t, build)
			run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
			if err == nil {
				t.Fatalf("reference %q unexpectedly passed: %#v", reference, run.Verification)
			}
			if fake.calls != 1 {
				t.Fatalf("calls=%d", fake.calls)
			}
		})
	}
}

func TestTask14ObservationRequest_IsDeterministic(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepareObservationSource(t, req, []byte("determinism-evidence"))

	first, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComposeFreeCADRuntimeObservationRequest(cloneFreeCADObservationRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path {
		t.Fatalf("path differs: %q vs %q", first.Path, second.Path)
	}
	if !reflect.DeepEqual(first.Contract, second.Contract) {
		t.Fatalf("contract differs:\n%#v\n%#v", first.Contract, second.Contract)
	}
	if !bytes.Equal(first.JSON, second.JSON) {
		t.Fatalf("JSON differs:\n%s\n%s", first.JSON, second.JSON)
	}
}
