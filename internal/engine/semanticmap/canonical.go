package semanticmap

import (
	"encoding/json"
	"slices"
)

func CanonicalJSON(contract *SemanticMap) ([]byte, error) {
	if err := Validate(contract); err != nil {
		return nil, err
	}

	normalized := cloneSemanticMap(contract)
	sortForCanonical(normalized)

	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, &DecodeError{Err: err}
	}
	return append(data, '\n'), nil
}

func cloneSemanticMap(in *SemanticMap) *SemanticMap {
	if in == nil {
		return nil
	}

	out := *in
	out.CADSystem = cloneCADSystem(in.CADSystem)
	out.SemanticTypes = cloneSemanticTypes(in.SemanticTypes)
	out.CaptureToSemantic = cloneCaptureToSemantic(in.CaptureToSemantic)
	out.SemanticToManifest = cloneSemanticToManifest(in.SemanticToManifest)
	out.OverridePolicy = cloneOverridePolicy(in.OverridePolicy)
	out.OperationCapabilities = cloneMap(in.OperationCapabilities)
	out.Determinism = cloneDeterminism(in.Determinism)
	for key, entry := range out.OperationCapabilities {
		entry.AllowedSemanticTargets = slices.Clone(entry.AllowedSemanticTargets)
		out.OperationCapabilities[key] = entry
	}

	return &out
}

func cloneCADSystem(in *CADSystem) *CADSystem {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneSemanticTypes(in *SemanticTypes) *SemanticTypes {
	if in == nil {
		return nil
	}

	out := *in
	out.ParameterTypes = cloneMap(in.ParameterTypes)
	out.ResolutionPolicy = cloneResolutionPolicy(in.ResolutionPolicy)
	return &out
}

func cloneCaptureToSemantic(in *CaptureToSemantic) *CaptureToSemantic {
	if in == nil {
		return nil
	}

	out := *in
	out.ParameterGroups = slices.Clone(in.ParameterGroups)
	out.Parameters = slices.Clone(in.Parameters)
	out.Components = slices.Clone(in.Components)
	out.Metadata = slices.Clone(in.Metadata)

	for i := range out.Parameters {
		out.Parameters[i].SemanticType.Map = cloneMap(in.Parameters[i].SemanticType.Map)
	}
	for i := range out.Components {
		out.Components[i].SemanticKinds = cloneMap(in.Components[i].SemanticKinds)
	}

	return &out
}

func cloneSemanticToManifest(in *SemanticToManifest) *SemanticToManifest {
	if in == nil {
		return nil
	}

	out := *in
	out.Parameters = cloneParameterManifestMapping(in.Parameters)
	out.Outputs = cloneMap(in.Outputs)
	out.Mutations = cloneMap(in.Mutations)
	for key, entry := range out.Outputs {
		entry.ScopeMap = cloneMap(entry.ScopeMap)
		out.Outputs[key] = entry
	}
	return &out
}

func cloneParameterManifestMapping(in *ParameterManifestMapping) *ParameterManifestMapping {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneResolutionPolicy(in *ResolutionPolicy) *ResolutionPolicy {
	if in == nil {
		return nil
	}

	out := *in
	out.Precedence = slices.Clone(in.Precedence)
	out.AllowEngineDefault = cloneBoolPointer(in.AllowEngineDefault)
	out.AllowEngineInference = cloneBoolPointer(in.AllowEngineInference)
	return &out
}

func cloneOverridePolicy(in *OverridePolicy) *OverridePolicy {
	if in == nil {
		return nil
	}

	out := *in
	out.AllowAdapterOverrides = cloneBoolPointer(in.AllowAdapterOverrides)
	out.AllowProjectOverrides = cloneBoolPointer(in.AllowProjectOverrides)
	out.AllowUserOverrides = cloneBoolPointer(in.AllowUserOverrides)
	return &out
}

func cloneDeterminism(in *Determinism) *Determinism {
	if in == nil {
		return nil
	}

	out := *in
	out.AllowImplicitFallback = cloneBoolPointer(in.AllowImplicitFallback)
	out.AllowCaseInsensitiveMatch = cloneBoolPointer(in.AllowCaseInsensitiveMatch)
	out.AllowDisplayNameIdentity = cloneBoolPointer(in.AllowDisplayNameIdentity)
	out.AllowAdapterInference = cloneBoolPointer(in.AllowAdapterInference)
	return &out
}

func cloneBoolPointer(in *bool) *bool {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneMap[T any](in map[string]T) map[string]T {
	if in == nil {
		return nil
	}
	out := make(map[string]T, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortForCanonical(contract *SemanticMap) {
	if contract.CaptureToSemantic != nil {
		slices.SortFunc(contract.CaptureToSemantic.ParameterGroups, func(a, b ParameterGroupMapping) int {
			return compareStrings(a.CaptureKind+"\x00"+a.CADContainerType+"\x00"+a.SemanticKind, b.CaptureKind+"\x00"+b.CADContainerType+"\x00"+b.SemanticKind)
		})
		slices.SortFunc(contract.CaptureToSemantic.Parameters, func(a, b ParameterMapping) int {
			return compareStrings(a.CaptureKind+"\x00"+a.SemanticKind, b.CaptureKind+"\x00"+b.SemanticKind)
		})
		slices.SortFunc(contract.CaptureToSemantic.Components, func(a, b ComponentMapping) int {
			return compareStrings(a.CaptureKind, b.CaptureKind)
		})
		slices.SortFunc(contract.CaptureToSemantic.Metadata, func(a, b MetadataMapping) int {
			return compareStrings(a.CaptureKind+"\x00"+a.SemanticKind, b.CaptureKind+"\x00"+b.SemanticKind)
		})
	}
	for key, entry := range contract.OperationCapabilities {
		slices.Sort(entry.AllowedSemanticTargets)
		contract.OperationCapabilities[key] = entry
	}
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
