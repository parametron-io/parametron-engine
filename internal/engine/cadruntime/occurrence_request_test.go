package cadruntime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/resourcebinding"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

func TestOccurrenceRuntimeSidecarAndVerificationBoundary(t *testing.T) {
	for _, mode := range []string{"complete", "optional omitted", "required omitted", "unsupported", "incomplete", "malformed", "scope mismatch", "undeclared resource", "absolute coordinate", "symlink escape", "alternate path", "no request"} {
		t.Run(mode, func(t *testing.T) {
			req := verify11NoParamRequest(t)
			input, e := occurrenceFixture(t, 2)
			inventory, err := resourcebinding.New([]resourcebinding.Binding{{ProjectID: "p", Kind: "model", LogicalID: "assembly", SourceCoordinate: filepath.Base(req.Manifest.Inputs.SourceModel)}})
			if err != nil {
				t.Fatal(err)
			}
			req.Resources, err = resourcebinding.Select(inventory, "p", "assembly", filepath.Dir(req.Manifest.Inputs.SourceModel))
			if err != nil {
				t.Fatal(err)
			}
			req.OccurrenceObservation = input.Request
			if mode == "required omitted" || mode == "unsupported" || mode == "incomplete" || mode == "alternate path" {
				req.OccurrenceObservation.Requirement = "complete"
			}
			if mode == "no request" {
				req.OccurrenceObservation = nil
			}
			e.Parent.Document = "source/Widget.FCStd"
			for i := range e.Members {
				e.Members[i].Target.Entity.Resource.LogicalID = "assembly"
				e.Members[i].Target.Document = "source/Widget.FCStd"
			}
			if mode == "unsupported" || mode == "incomplete" {
				e.Completeness = mode
				e.Diagnostics = []string{"controlled unavailable inspection"}
				e.Members = []OccurrenceRuntimeMember{}
				e.Group = []string{}
				e.InspectionCompleted = false
			}
			if mode == "scope mismatch" {
				e.Scope.Parent.Name = "Other"
			}
			if mode == "undeclared resource" {
				e.Members[0].Target.Entity.Resource.LogicalID = "unknown"
			}
			if mode == "absolute coordinate" {
				e.Members[0].Target.Document = "/tmp/Widget.FCStd"
			}
			raw := append(occurrenceBytes(t, e), '\n')
			if mode == "malformed" {
				raw = []byte(`{"schemaVersion":`)
			}
			layout := task10Layout(t, req)
			fake := verify11Capability(t, verify11PassBuild(req, layout))
			invoke := fake.fn
			fake.fn = func(ctx context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				requestPath := filepath.Join(got.WorkingCopyDir, OccurrenceRequestFilename)
				content, err := os.ReadFile(requestPath)
				if mode == "no request" {
					if !os.IsNotExist(err) {
						t.Fatalf("unrequested sidecar: %v", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					decoded, err := DecodeOccurrenceRequest(content)
					if err != nil || !reflect.DeepEqual(decoded, *req.OccurrenceObservation) {
						t.Fatalf("sidecar intent changed: %+v %v", decoded, err)
					}
				}
				result, err := invoke(ctx, got)
				if err != nil {
					return result, err
				}
				path := filepath.Join(got.OutputDir, OccurrenceEvidenceFilename)
				switch mode {
				case "optional omitted", "required omitted", "no request":
					return result, nil
				case "alternate path":
					path = filepath.Join(got.WorkingCopyDir, OccurrenceEvidenceFilename)
				case "symlink escape":
					outside := filepath.Join(t.TempDir(), OccurrenceEvidenceFilename)
					if err := os.WriteFile(outside, raw, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, path); err != nil {
						t.Fatal(err)
					}
					return result, nil
				}
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				return result, nil
			}
			run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
			switch mode {
			case "complete", "optional omitted", "no request":
				if err != nil || run.Verification == nil || run.Verification.Status != verification.StatusPass {
					t.Fatalf("controlled verification: %v %+v", err, run.Verification)
				}
				accepted, err := LoadOccurrenceRun(req, run.Runtime)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "no request" {
					if accepted != nil {
						t.Fatal("synthesized occurrence intent")
					}
					return
				}
				if !reflect.DeepEqual(accepted.Request, req.OccurrenceObservation) {
					t.Fatal("request not retained")
				}
				summary, err := MapOccurrenceEvidence(OccurrenceMappingInput{Request: accepted.Request, Resources: run.Runtime.ObservationRequest.Manifest.Attempt.Resources, ExpectedSourceDocument: "source/Widget.FCStd", Content: accepted.Content})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "complete" {
					if !bytes.Equal(accepted.Content, raw) || summary.Quantities[0].Quantity != 2 {
						t.Fatal("raw evidence or quantity lost")
					}
				} else if summary.Completeness != "unavailable" || summary.Quantities != nil {
					t.Fatal(summary)
				}
			default:
				if err == nil {
					t.Fatal("invalid or required-incomplete evidence accepted")
				}
				var classified *FreeCADRuntimeVerificationError
				if !errors.As(err, &classified) || classified.Stage != "occurrence_validation" {
					t.Fatalf("wrong boundary: %v", err)
				}
				if mode == "required omitted" || mode == "unsupported" || mode == "incomplete" || mode == "alternate path" {
					if !errors.Is(err, recordcontract.ErrOccurrenceCompletenessRequired) {
						t.Fatalf("required completeness sentinel lost: %v", err)
					}
				}
			}
		})
	}
}
