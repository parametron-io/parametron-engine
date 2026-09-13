package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/runtimecap"
)

type orchestrationContractAdapter struct{}

func (*orchestrationContractAdapter) Run(context.Context, planner.Step) error { return nil }
func (*orchestrationContractAdapter) OrchestrateCADRuntime(context.Context, CADRuntimeOrchestrationRequest) error {
	return nil
}

type legacyContractAdapter struct{}

func (*legacyContractAdapter) Run(context.Context, planner.Step) error { return nil }

var _ Adapter = (*orchestrationContractAdapter)(nil)
var _ CADRuntimeOrchestrator = (*orchestrationContractAdapter)(nil)
var _ Adapter = (*legacyContractAdapter)(nil)

func validCADRuntimeRequest() CADRuntimeOrchestrationRequest {
	return CADRuntimeOrchestrationRequest{
		JobID: "job-1", ProductKey: "widget", ProductDir: "/missing/products/widget", StepID: "2", Attempt: 1,
		CSV:        planner.WriteCSVPayload{ProductKey: "widget", Filename: "misleading.csv", Headers: []string{"width"}, Values: []any{10.0}},
		Manifest:   planner.WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: "export_manifest_v1.json", Adapter: "freecad", Values: map[string]any{"width": 10.0}},
		CADRuntime: planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: "freecad", ManifestFilename: "export_manifest_v1.json", ResultFilename: "result.json"},
		Executable: runtimecap.ExecutableSelection{Adapter: "freecad", Command: "/opt/parametron/bin/parametron-freecad", Path: "/opt/parametron/bin/parametron-freecad", Source: runtimecap.ExecutableSelectionSourceConfigured},
	}
}

func requireRequestField(t *testing.T, err error, field string) {
	t.Helper()
	var requestErr *CADRuntimeOrchestrationRequestError
	if !errors.As(err, &requestErr) || requestErr.Field != field {
		t.Fatalf("error=%v requestError=%#v want field %q", err, requestErr, field)
	}
}

func TestCADRuntimeOrchestrator_InterfaceContract(t *testing.T) {
	var legacy any = &legacyContractAdapter{}
	if _, ok := legacy.(CADRuntimeOrchestrator); ok {
		t.Fatal("legacy Adapter unexpectedly implements secondary capability")
	}
}

func TestValidateCADRuntimeOrchestrationRequest_Valid(t *testing.T) {
	req := validCADRuntimeRequest()
	req.ProductDir = filepath.Join(t.TempDir(), "absent-product")
	req.Executable.Path = filepath.Join(t.TempDir(), "absent-runtime")
	req.Executable.Command = req.Executable.Path
	want := req
	if err := ValidateCADRuntimeOrchestrationRequest(req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("validator mutated request: %#v", req)
	}
	if _, err := os.Stat(req.ProductDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("product directory side effect: %v", err)
	}
}

func TestValidateCADRuntimeOrchestrationRequest_RequiredFields(t *testing.T) {
	tests := []struct {
		name, field string
		mutate      func(*CADRuntimeOrchestrationRequest)
	}{
		{"job empty", "JobID", func(r *CADRuntimeOrchestrationRequest) { r.JobID = "" }}, {"job whitespace", "JobID", func(r *CADRuntimeOrchestrationRequest) { r.JobID = " \t" }},
		{"product empty", "ProductKey", func(r *CADRuntimeOrchestrationRequest) { r.ProductKey = "" }}, {"product whitespace", "ProductKey", func(r *CADRuntimeOrchestrationRequest) { r.ProductKey = " " }},
		{"dir empty", "ProductDir", func(r *CADRuntimeOrchestrationRequest) { r.ProductDir = "" }}, {"dir whitespace", "ProductDir", func(r *CADRuntimeOrchestrationRequest) { r.ProductDir = " " }},
		{"step empty", "StepID", func(r *CADRuntimeOrchestrationRequest) { r.StepID = "" }}, {"step whitespace", "StepID", func(r *CADRuntimeOrchestrationRequest) { r.StepID = " " }},
		{"attempt zero", "Attempt", func(r *CADRuntimeOrchestrationRequest) { r.Attempt = 0 }}, {"attempt negative", "Attempt", func(r *CADRuntimeOrchestrationRequest) { r.Attempt = -1 }},
		{"csv", "CSV.Filename", func(r *CADRuntimeOrchestrationRequest) { r.CSV.Filename = "" }}, {"manifest", "Manifest.ManifestFilename", func(r *CADRuntimeOrchestrationRequest) { r.Manifest.ManifestFilename = "" }},
		{"runtime adapter", "CADRuntime.Adapter", func(r *CADRuntimeOrchestrationRequest) { r.CADRuntime.Adapter = "" }}, {"runtime manifest", "CADRuntime.ManifestFilename", func(r *CADRuntimeOrchestrationRequest) { r.CADRuntime.ManifestFilename = "" }},
		{"executable adapter", "Executable.Adapter", func(r *CADRuntimeOrchestrationRequest) { r.Executable.Adapter = "" }}, {"command", "Executable.Command", func(r *CADRuntimeOrchestrationRequest) { r.Executable.Command = "" }}, {"path", "Executable.Path", func(r *CADRuntimeOrchestrationRequest) { r.Executable.Path = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validCADRuntimeRequest()
			tt.mutate(&r)
			requireRequestField(t, ValidateCADRuntimeOrchestrationRequest(r), tt.field)
		})
	}
}

func TestValidateCADRuntimeOrchestrationRequest_ProductIdentity(t *testing.T) {
	for _, field := range []string{"CSV.ProductKey", "Manifest.ProductKey", "CADRuntime.ProductKey"} {
		for _, key := range []string{"", "widget", "other"} {
			t.Run(field+"/"+key, func(t *testing.T) {
				r := validCADRuntimeRequest()
				r.CSV.Filename = "other-product.csv"
				switch field {
				case "CSV.ProductKey":
					r.CSV.ProductKey = key
				case "Manifest.ProductKey":
					r.Manifest.ProductKey = key
				default:
					r.CADRuntime.ProductKey = key
				}
				err := ValidateCADRuntimeOrchestrationRequest(r)
				if key == "other" {
					requireRequestField(t, err, field)
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestValidateCADRuntimeOrchestrationRequest_AdapterConsistency(t *testing.T) {
	r := validCADRuntimeRequest()
	r.Manifest.Adapter = "other"
	requireRequestField(t, ValidateCADRuntimeOrchestrationRequest(r), "CADRuntime.Adapter")
	r = validCADRuntimeRequest()
	r.Executable.Adapter = "other"
	requireRequestField(t, ValidateCADRuntimeOrchestrationRequest(r), "CADRuntime.Adapter")
	r = validCADRuntimeRequest()
	r.Manifest.Adapter = "solidworks"
	r.CADRuntime.Adapter = "solidworks"
	r.Executable.Adapter = "solidworks"
	if err := ValidateCADRuntimeOrchestrationRequest(r); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCADRuntimeOrchestrationRequest_ManifestConsistency(t *testing.T) {
	r := validCADRuntimeRequest()
	r.CADRuntime.ManifestFilename = "EXPORT_MANIFEST_V1.JSON"
	requireRequestField(t, ValidateCADRuntimeOrchestrationRequest(r), "CADRuntime.ManifestFilename")
}

func TestValidateCADRuntimeOrchestrationRequest_ExecutableSelection(t *testing.T) {
	tests := []struct {
		name, field string
		mutate      func(*CADRuntimeOrchestrationRequest)
	}{
		{"relative", "Executable.Path", func(r *CADRuntimeOrchestrationRequest) { r.Executable.Path = "bin/runtime" }},
		{"unclean", "Executable.Path", func(r *CADRuntimeOrchestrationRequest) { r.Executable.Path = "/opt/bin/../runtime" }},
		{"unsupported", "Executable.Source", func(r *CADRuntimeOrchestrationRequest) { r.Executable.Source = "registry" }},
		{"empty", "Executable.Source", func(r *CADRuntimeOrchestrationRequest) { r.Executable.Source = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validCADRuntimeRequest()
			tt.mutate(&r)
			requireRequestField(t, ValidateCADRuntimeOrchestrationRequest(r), tt.field)
		})
	}
	for _, source := range []runtimecap.ExecutableSelectionSource{runtimecap.ExecutableSelectionSourceConfigured, runtimecap.ExecutableSelectionSourceDefaultPATH} {
		r := validCADRuntimeRequest()
		r.Executable.Source = source
		if err := ValidateCADRuntimeOrchestrationRequest(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateCADRuntimeOrchestrationRequest_HasNoFilesystemOrProcessSideEffects(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "started")
	product := filepath.Join(root, "product")
	script := filepath.Join(root, "runtime-would-write-"+filepath.Base(marker))
	r := validCADRuntimeRequest()
	r.ProductDir = product
	r.Executable.Path = script
	r.Executable.Command = script
	want := r
	if err := ValidateCADRuntimeOrchestrationRequest(r); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{product, script, marker, filepath.Join(product, r.CSV.Filename), filepath.Join(product, r.Manifest.ManifestFilename)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("side effect at %s: %v", path, err)
		}
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatal("request mutated")
	}
}

func TestCADRuntimeOrchestrationRequestError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("sentinel cause")
	err := &CADRuntimeOrchestrationRequestError{Field: "StepID", Err: cause}
	if !strings.Contains(err.Error(), "StepID") || !errors.Is(err, cause) {
		t.Fatalf("error contract: %v", err)
	}
	var got *CADRuntimeOrchestrationRequestError
	if !errors.As(err, &got) || got != err {
		t.Fatal("errors.As failed")
	}
}
