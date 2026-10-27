package freecad

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/projectinput"
	"parametron/internal/engine/resourcebinding"
)

// No traversal or reference evidence is supplied: membership comes only from
// declarations. Arbitrary source bytes also keep this test outside native CAD.
func declaredAttemptRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADRuntimeManifestRequest(t)
	root := t.TempDir()
	for name, content := range map[string]string{
		"project.dsl":      "dsl v1.0\nproduct Widget { param width: number = 1 }\n",
		"prm.project.json": `{"version":"1.0","projectId":"project","dsl":"project.dsl","resources":{"models":{"selected":"a/part.FCStd","alias":"a/part.FCStd","additional":"b/part.FCStd","unused":"standalone.FCStd"}}}`,
		"a/part.FCStd":     "primary\x00bytes", "b/part.FCStd": "additional bytes", "standalone.FCStd": "zero-reference bytes",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	project, err := projectinput.ResolveProject(root)
	if err != nil {
		t.Fatal(err)
	}
	req.Resources, err = resourcebinding.Select(project.ModelInventory, project.ProjectID, "selected", root)
	if err != nil {
		t.Fatal(err)
	}
	req.Manifest.Inputs.SourceModel = req.Resources.SelectedSourcePath()
	return req
}

func TestDeclaredMaterializationRelocationRetriesAndAliases(t *testing.T) {
	var stable []resourcebinding.Binding
	for relocation := 0; relocation < 2; relocation++ {
		req := declaredAttemptRequest(t)
		var previousRoot string
		for retry := 1; retry <= 2; retry++ {
			req.Attempt = retry
			computed, err := ComputeFreeCADRuntimeAttempt(req)
			if err != nil {
				t.Fatal(err)
			}
			for _, b := range computed.Resources.Bindings() {
				if b.MaterializedCoordinate != "" {
					t.Fatal("computed attempt claims completed materialization")
				}
			}
			if _, err := os.Stat(computed.Layout.WorkingCopyDir); !os.IsNotExist(err) {
				t.Fatalf("computation wrote workspace: %v", err)
			}
			attempt, err := PrepareFreeCADRuntimeAttempt(req)
			if err != nil {
				t.Fatal(err)
			}
			bindings := attempt.Resources.Bindings()
			if len(bindings) != 4 {
				t.Fatalf("inventory lost declarations: %v", bindings)
			}
			if stable != nil && !reflect.DeepEqual(stable, bindings) {
				t.Fatalf("relative join changed across relocation/retry: %v / %v", stable, bindings)
			}
			stable = bindings
			if attempt.Layout.WorkingCopyDir == previousRoot {
				t.Fatal("retry reused workspace")
			}
			previousRoot = attempt.Layout.WorkingCopyDir
			seen := map[string]bool{}
			for _, b := range bindings {
				want := fmt.Sprintf("resources/%x/%s", sha256.Sum256([]byte(b.LogicalID)), b.SourceCoordinate)
				if b.LogicalID == "selected" {
					want = "source/part.FCStd"
				}
				if b.MaterializedCoordinate != want || resourcebinding.ValidateCoordinate(want) != nil || seen[want] {
					t.Fatalf("invalid destination join: %v", b)
				}
				seen[want] = true
				source, err := req.Resources.SourcePath(b)
				if err != nil {
					t.Fatal(err)
				}
				original, err := os.ReadFile(source)
				if err != nil {
					t.Fatal(err)
				}
				destination := filepath.Join(attempt.Layout.WorkingCopyDir, filepath.FromSlash(want))
				staged, err := os.ReadFile(destination)
				if err != nil || !bytes.Equal(original, staged) {
					t.Fatalf("copy mismatch: %v %v", b, err)
				}
				joined, err := attempt.Resources.LookupCoordinate(want, true)
				if err != nil || joined != b {
					t.Fatalf("inverse join: %v %v", joined, err)
				}
				if err := os.WriteFile(destination, []byte("runtime mutation"), 0o644); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(source)
				if err != nil || !bytes.Equal(original, after) {
					t.Fatal("staging aliases project source")
				}
			}
			if _, err := attempt.Resources.LookupCoordinate("a/part.FCStd", false); !errors.Is(err, resourcebinding.ErrInvalid) {
				t.Fatalf("arbitrary alias selected: %v", err)
			}
		}
	}
}

func TestDeclaredMaterializationRejectsUnsafeDestinationsBeforeWrites(t *testing.T) {
	for _, mode := range []string{"destination symlink", "ancestor symlink", "working root symlink", "file ancestor", "destination directory", "source hardlink", "missing additional", "nonregular additional", "escaped additional", "reserved collision", "reserved ancestor", "reserved output directory"} {
		t.Run(mode, func(t *testing.T) {
			req := declaredAttemptRequest(t)
			if mode == "reserved collision" {
				req.CADRuntime.ResultFilename = req.CADRuntime.ManifestFilename
			}
			if mode == "reserved ancestor" {
				req.Manifest.ManifestFilename = "source"
				req.CADRuntime.ManifestFilename = "source"
			}
			if mode == "reserved output directory" {
				req.CADRuntime.ResultFilename = "outputs"
			}
			attempt, err := ComputeFreeCADRuntimeAttempt(req)
			if err != nil {
				t.Fatal(err)
			}
			copies, _, err := planDeclaredResourceCopies(req.Resources, attempt)
			reserved := mode == "reserved collision" || mode == "reserved ancestor" || mode == "reserved output directory"
			if reserved {
				if err == nil {
					t.Fatal("reserved filenames collided")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			victim := filepath.Join(outside, "victim")
			if err := os.WriteFile(victim, []byte("unchanged"), 0o644); err != nil {
				t.Fatal(err)
			}
			var additional declaredResourceCopy
			for _, copy := range copies {
				if copy.binding.LogicalID == "additional" {
					additional = copy
				}
			}
			makeParent := func(path string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "destination symlink":
				makeParent(additional.destination)
				err = os.Symlink(victim, additional.destination)
			case "ancestor symlink":
				path := filepath.Join(attempt.Layout.WorkingCopyDir, "resources")
				makeParent(path)
				err = os.Symlink(outside, path)
			case "working root symlink":
				makeParent(attempt.Layout.WorkingRoot)
				err = os.Symlink(outside, attempt.Layout.WorkingRoot)
			case "file ancestor":
				path := filepath.Join(attempt.Layout.WorkingCopyDir, "resources")
				makeParent(path)
				err = os.WriteFile(path, []byte("file"), 0o644)
			case "destination directory":
				err = os.MkdirAll(additional.destination, 0o755)
			case "source hardlink":
				makeParent(additional.destination)
				err = os.Link(additional.source, additional.destination)
			case "missing additional":
				err = os.Remove(additional.source)
			case "nonregular additional":
				if err = os.Remove(additional.source); err == nil {
					err = os.Mkdir(additional.source, 0o755)
				}
			case "escaped additional":
				if err = os.Remove(additional.source); err == nil {
					err = os.Symlink(victim, additional.source)
				}
			}
			if err != nil && !reserved {
				t.Fatal(err)
			}
			before, err := os.ReadFile(req.Resources.SelectedSourcePath())
			if err != nil {
				t.Fatal(err)
			}
			_, err = PrepareFreeCADRuntimeAttempt(req)
			var failure *FreeCADRuntimeAttemptError
			if !errors.As(err, &failure) || failure.Stage != FreeCADRuntimeAttemptStageSourcePreparation {
				t.Fatalf("wrong failure stage: %v", err)
			}
			if _, err := os.Lstat(attempt.Layout.SourceDocumentPath); !os.IsNotExist(err) {
				t.Fatalf("primary written before complete preflight: %v", err)
			}
			after, err := os.ReadFile(req.Resources.SelectedSourcePath())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("project source modified")
			}
			unchanged, err := os.ReadFile(victim)
			if err != nil || string(unchanged) != "unchanged" {
				t.Fatal("outside destination modified")
			}
		})
	}
}

func TestResourceContextDoesNotChangeAttemptIdentityOrWireRequest(t *testing.T) {
	req := declaredAttemptRequest(t)
	manifestWith, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	with, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	wireWith, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Resources = nil
	manifestWithout, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	without, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	wireWithout, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if with.Identity != without.Identity || with.Layout != without.Layout || !bytes.Equal(wireWith, wireWithout) || !bytes.Equal(manifestWith.JSON, manifestWithout.JSON) {
		t.Fatal("resource context changed execution identity, runtime manifest, or serialized orchestration request")
	}
}

func TestDeclaredSelectionMustMatchManifestSource(t *testing.T) {
	req := declaredAttemptRequest(t)
	req.Manifest.Inputs.SourceModel = filepath.Join(t.TempDir(), "part.FCStd")
	_, err := PrepareFreeCADRuntimeAttempt(req)
	var failure *FreeCADRuntimeAttemptError
	if !errors.As(err, &failure) || failure.Stage != FreeCADRuntimeAttemptStageRequestValidation || failure.Field != "Resources" {
		t.Fatalf("selection mismatch: %v", err)
	}
	if _, err := os.Stat(req.ProductDir); !os.IsNotExist(err) {
		t.Fatalf("invalid selection wrote product directory: %v", err)
	}
}
