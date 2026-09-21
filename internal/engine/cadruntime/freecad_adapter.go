package cadruntime

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

// FreeCADRuntimeAdapter preserves legacy adapter execution while providing the
// Engine-owned aligned FreeCAD orchestration and verified-run snapshot.
type FreeCADRuntimeAdapter struct {
	delegate   adapter.Adapter
	capability runtimecap.Capability

	mu            sync.RWMutex
	orchestrateMu sync.Mutex
	verified      FreeCADRuntimeVerifiedRun
	available     bool
}

func NewFreeCADRuntimeAdapter(delegate adapter.Adapter, capability runtimecap.Capability) (*FreeCADRuntimeAdapter, error) {
	if nilInterface(delegate) {
		return nil, fmt.Errorf("FreeCAD runtime adapter delegate is required")
	}
	if nilInterface(capability) {
		return nil, fmt.Errorf("FreeCAD runtime capability is required")
	}
	return &FreeCADRuntimeAdapter{delegate: delegate, capability: capability}, nil
}

func (a *FreeCADRuntimeAdapter) Run(ctx context.Context, step planner.Step) error {
	return a.delegate.Run(ctx, step)
}

func (a *FreeCADRuntimeAdapter) OrchestrateCADRuntime(ctx context.Context, req adapter.CADRuntimeOrchestrationRequest) error {
	a.orchestrateMu.Lock()
	defer a.orchestrateMu.Unlock()
	a.mu.Lock()
	a.verified = FreeCADRuntimeVerifiedRun{}
	a.available = false
	a.mu.Unlock()

	run, err := InvokeAndVerifyFreeCADRuntime(ctx, a.capability, req)
	a.mu.Lock()
	a.verified = deepCopyVerifiedRun(run)
	a.available = true
	a.mu.Unlock()
	return err
}

func (a *FreeCADRuntimeAdapter) CADRuntimeVerifiedRun() (FreeCADRuntimeVerifiedRun, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.available {
		return FreeCADRuntimeVerifiedRun{}, false
	}
	return deepCopyVerifiedRun(a.verified), true
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func deepCopyVerifiedRun(in FreeCADRuntimeVerifiedRun) FreeCADRuntimeVerifiedRun {
	out := in
	out.Runtime.ObservationRequest = copyObservationRequestMaterialization(in.Runtime.ObservationRequest)
	if in.Runtime.Process != nil {
		process := *in.Runtime.Process
		process.Command.Args = append([]string(nil), in.Runtime.Process.Command.Args...)
		out.Runtime.Process = &process
	}
	out.Runtime.Result = copyRuntimeResult(in.Runtime.Result)
	out.Runtime.Observed = copyObserved(in.Runtime.Observed)
	out.Runtime.ReferenceTraversalJSON = append([]byte(nil), in.Runtime.ReferenceTraversalJSON...)
	out.Runtime.Artifacts = append([]FreeCADRuntimeValidatedArtifact(nil), in.Runtime.Artifacts...)
	if in.Verification != nil {
		result := *in.Verification
		out.Verification = &result
	}
	return out
}

func copyObservationRequestMaterialization(in FreeCADRuntimeObservationRequestMaterialization) FreeCADRuntimeObservationRequestMaterialization {
	out := in
	out.JSON = append([]byte(nil), in.JSON...)
	out.Manifest.JSON = append([]byte(nil), in.Manifest.JSON...)
	out.Manifest.Manifest.ParameterAssignments = append([]freecad.FreeCADRuntimeParameterAssignment(nil), in.Manifest.Manifest.ParameterAssignments...)
	for i := range out.Manifest.Manifest.ParameterAssignments {
		out.Manifest.Manifest.ParameterAssignments[i].Value = copyAny(in.Manifest.Manifest.ParameterAssignments[i].Value)
	}
	out.Manifest.Manifest.Outputs = append([]freecad.FreeCADRuntimeManifestOutput(nil), in.Manifest.Manifest.Outputs...)
	out.Contract = copyVerificationContract(in.Contract)
	return out
}

func copyVerificationContract(in verification.Contract) verification.Contract {
	out := in
	out.ObservationContext.Parameters = append([]verification.ObservedParameterBinding(nil), in.ObservationContext.Parameters...)
	if in.ObservationContext.TargetState != nil {
		targetState := *in.ObservationContext.TargetState
		targetState.Suppression = append([]verification.TargetIdentity(nil), in.ObservationContext.TargetState.Suppression...)
		targetState.Visibility = append([]verification.TargetIdentity(nil), in.ObservationContext.TargetState.Visibility...)
		targetState.Existence = append([]verification.TargetIdentity(nil), in.ObservationContext.TargetState.Existence...)
		out.ObservationContext.TargetState = &targetState
	}
	out.Expected.Components = append([]verification.ExpectedComponent(nil), in.Expected.Components...)
	out.Expected.Parameters = append([]verification.ExpectedParameter(nil), in.Expected.Parameters...)
	out.Expected.Metadata = append([]verification.ExpectedMetadata(nil), in.Expected.Metadata...)
	out.Expected.References = append([]verification.ExpectedReference(nil), in.Expected.References...)
	if in.Expected.TargetState != nil {
		targetState := *in.Expected.TargetState
		targetState.Suppression = append([]verification.ExpectedBooleanTargetState(nil), in.Expected.TargetState.Suppression...)
		targetState.Visibility = append([]verification.ExpectedBooleanTargetState(nil), in.Expected.TargetState.Visibility...)
		targetState.Existence = append([]verification.ExpectedExistenceTargetState(nil), in.Expected.TargetState.Existence...)
		out.Expected.TargetState = &targetState
	}
	return out
}

func copyRuntimeResult(in *freecad.FreeCADRuntimeResult) *freecad.FreeCADRuntimeResult {
	if in == nil {
		return nil
	}
	out := *in
	out.Artifacts = append([]freecad.FreeCADRuntimeResultArtifact(nil), in.Artifacts...)
	if in.Failure != nil {
		failure := *in.Failure
		if in.Failure.Stage != nil {
			stage := *in.Failure.Stage
			failure.Stage = &stage
		}
		out.Failure = &failure
	}
	return &out
}

func copyObserved(in *observed.Observed) *observed.Observed {
	if in == nil {
		return nil
	}
	out := *in
	out.Observation.Parameters = append([]observed.Parameter(nil), in.Observation.Parameters...)
	for i := range out.Observation.Parameters {
		out.Observation.Parameters[i].Value = copyObservedValue(in.Observation.Parameters[i].Value)
	}
	out.Observation.Metadata = append([]observed.Metadata(nil), in.Observation.Metadata...)
	for i := range out.Observation.Metadata {
		out.Observation.Metadata[i].Value = copyObservedValue(in.Observation.Metadata[i].Value)
	}
	out.Observation.References = append([]observed.Reference(nil), in.Observation.References...)
	out.Observation.Components = append([]observed.Component(nil), in.Observation.Components...)
	if in.Observation.TargetState != nil {
		targetState := *in.Observation.TargetState
		targetState.Suppression = copyBooleanTargetEvidence(in.Observation.TargetState.Suppression)
		targetState.Visibility = copyBooleanTargetEvidence(in.Observation.TargetState.Visibility)
		targetState.Existence = append([]observed.ExistenceTargetEvidence(nil), in.Observation.TargetState.Existence...)
		out.Observation.TargetState = &targetState
	}
	return &out
}

func copyBooleanTargetEvidence(in []observed.BooleanTargetEvidence) []observed.BooleanTargetEvidence {
	out := append([]observed.BooleanTargetEvidence(nil), in...)
	for index := range out {
		if in[index].Value != nil {
			value := *in[index].Value
			out[index].Value = &value
		}
	}
	return out
}

func copyObservedValue(in observed.Value) observed.Value {
	if in.IsZero() {
		return observed.Value{}
	}
	out, err := observed.NewValue(in.Raw())
	if err != nil {
		panic("copy validated observed value: " + err.Error())
	}
	return out
}

func copyAny(in any) any {
	switch value := in.(type) {
	case []byte:
		return append([]byte(nil), value...)
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = copyAny(value[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = copyAny(item)
		}
		return out
	default:
		return value
	}
}
