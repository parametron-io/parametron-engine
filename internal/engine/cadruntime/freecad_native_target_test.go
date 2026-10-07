package cadruntime

import (
	"context"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
	"parametron/internal/engine/verification"
)

func TestInvokeAndVerifyFreeCADRuntime_CapturedNativeTargetCorrelation(t *testing.T) {
	capture, err := cad.Load("../semantic/testdata/native-target/prm.cad.json")
	if err != nil {
		t.Fatal(err)
	}
	model, err := semantic.BuildFromCapture(capture)
	if err != nil {
		t.Fatal(err)
	}
	target, err := semantic.ResolveSemanticTargetByExactName(model, "mounting_bracket")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := semantic.LowerTargetActionMutationIntent(target, "suppress")
	if err != nil {
		t.Fatal(err)
	}
	routing, err := semanticmap.RouteTargetMutations(&semanticmap.TargetMutationRoutingRequest{Model: model, IdentityLinkage: &semanticmap.IdentityLinkage{}, Mutations: []semantic.MutationIntent{*intent}})
	if err != nil {
		t.Fatal(err)
	}
	req := tsRuntimeRequest(t)
	req.Manifest.AssemblyMutations.Suppression[0].Object = routing.Assembly[0].Object
	if req.Manifest.AssemblyMutations.Suppression[0].Object != "Body01" {
		t.Fatal("semantic identity leaked into mutation")
	}
	layout := task10Layout(t, req)
	fake := verify11Capability(t, tsBuild(req, layout, func(e *observed.TargetStateObservation) { e.Suppression[0].Object = "Body01" }))
	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	contract := run.Runtime.ObservationRequest.Contract
	if contract.ObservationContext.TargetState.Suppression[0].Object != "Body01" || contract.Expected.TargetState.Suppression[0].Object != "Body01" {
		t.Fatalf("observation/expected correlation: %+v", contract)
	}
	if run.Verification.Status != verification.StatusPass || run.Runtime.Observed.Observation.TargetState.Suppression[0].Object != "Body01" {
		t.Fatalf("native evidence did not correlate: %+v", run)
	}
}
