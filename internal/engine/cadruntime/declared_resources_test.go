package cadruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/resourcebinding"
)

func TestDeclaredResourcePreflightStopsBeforeRuntimeInvocation(t *testing.T) {
	for _, mode := range []string{"missing source", "source escape", "destination ancestor", "working copy root"} {
		t.Run(mode, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			root := filepath.Dir(req.Manifest.Inputs.SourceModel)
			additional := filepath.Join(root, "additional.FCStd")
			if err := os.WriteFile(additional, []byte("additional"), 0o644); err != nil {
				t.Fatal(err)
			}
			inventory, err := resourcebinding.New([]resourcebinding.Binding{
				{ProjectID: "p", Kind: resourcebinding.ResourceKindModel, LogicalID: "selected", SourceCoordinate: filepath.Base(req.Manifest.Inputs.SourceModel)},
				{ProjectID: "p", Kind: resourcebinding.ResourceKindModel, LogicalID: "unused", SourceCoordinate: "additional.FCStd"},
			})
			if err != nil {
				t.Fatal(err)
			}
			req.Resources, err = resourcebinding.Select(inventory, "p", "selected", root)
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := freecad.ComputeFreeCADRuntimeAttempt(req)
			if err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			wantStage := FreeCADRuntimeRunStageRequestMaterialization
			switch mode {
			case "missing source":
				err = os.Remove(additional)
			case "source escape":
				err = os.Remove(additional)
				// The target itself is a regular file outside the accepted project.
				if err == nil {
					err = os.WriteFile(filepath.Join(outside, "additional"), []byte("outside"), 0o644)
				}
				if err == nil {
					err = os.Symlink(filepath.Join(outside, "additional"), additional)
				}
			case "destination ancestor", "working copy root":
				path := filepath.Join(attempt.Layout.WorkingCopyDir, "resources")
				if mode == "working copy root" {
					path = attempt.Layout.WorkingCopyDir
					wantStage = FreeCADRuntimeRunStageOutputPreparation
				}
				if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
					err = os.Symlink(outside, path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			capability := &task10Capability{}
			_, err = InvokeAndValidateFreeCADRuntime(context.Background(), capability, req)
			var runErr *FreeCADRuntimeRunError
			if !errors.As(err, &runErr) || runErr.Stage != wantStage || capability.calls != 0 {
				t.Fatalf("failure boundary: %v, calls=%d", err, capability.calls)
			}
			if _, err := os.Stat(attempt.Layout.SourceDocumentPath); !os.IsNotExist(err) {
				t.Fatalf("primary staged before failed preflight: %v", err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "source escape" && len(entries) != 0 {
				t.Fatalf("redirected destination was written: %v", entries)
			}
		})
	}
}
