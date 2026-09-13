package semantic

import "slices"

func cloneComponents(in []Component) []Component {
	if in == nil {
		return nil
	}
	out := make([]Component, len(in))
	copy(out, in)
	for i := range out {
		if in[i].ChildrenIDs == nil {
			out[i].ChildrenIDs = nil
			continue
		}
		out[i].ChildrenIDs = append([]string(nil), in[i].ChildrenIDs...)
	}
	return out
}

func cloneParameterGroups(in []ParameterGroup) []ParameterGroup {
	if in == nil {
		return nil
	}
	out := make([]ParameterGroup, len(in))
	copy(out, in)
	return out
}

func cloneFeatures(in []Feature) []Feature {
	if in == nil {
		return nil
	}
	out := make([]Feature, len(in))
	copy(out, in)
	return out
}

func cloneParameters(in []Parameter) []Parameter {
	if in == nil {
		return nil
	}
	out := make([]Parameter, len(in))
	copy(out, in)
	for i := range out {
		out[i].CurrentValue.raw = append([]byte(nil), in[i].CurrentValue.raw...)
	}
	return out
}

func cloneMetadata(in []Metadata) []Metadata {
	if in == nil {
		return nil
	}
	out := make([]Metadata, len(in))
	copy(out, in)
	for i := range out {
		out[i].CurrentValue.raw = append([]byte(nil), in[i].CurrentValue.raw...)
	}
	return out
}

func cloneProductIntents(in []ProductIntent) []ProductIntent {
	if in == nil {
		return nil
	}
	out := make([]ProductIntent, len(in))
	copy(out, in)
	for i := range out {
		out[i].RequestedOutputFormats = append([]string(nil), in[i].RequestedOutputFormats...)
		out[i].Outputs = append([]OutputIntent(nil), in[i].Outputs...)
		out[i].ExportedParameters = append([]ExportedParameterIntent(nil), in[i].ExportedParameters...)
		out[i].Mutations = cloneMutationIntents(in[i].Mutations)
	}
	return out
}

func cloneMutationIntents(in []MutationIntent) []MutationIntent {
	if in == nil {
		return nil
	}
	out := make([]MutationIntent, len(in))
	copy(out, in)
	for i := range out {
		if !in[i].ValueSource.Scalar.IsZero() {
			out[i].ValueSource.Scalar.raw = append([]byte(nil), in[i].ValueSource.Scalar.raw...)
		}
		if in[i].ValueSource.BooleanState != nil {
			value := *in[i].ValueSource.BooleanState
			out[i].ValueSource.BooleanState = &value
		}
	}
	return out
}

func sortForCanonical(model *Model) {
	normalizeSlices(model)
	sortComponents(model.Components)
	sortFeatures(model.Features)
	sortParameterGroups(model.ParameterGroups)
	sortParameters(model.Parameters)
	sortMetadata(model.Metadata)
	sortProductIntents(model.ProductIntents)
}

func normalizeSlices(model *Model) {
	if model.Components == nil {
		model.Components = []Component{}
	}
	if model.ParameterGroups == nil {
		model.ParameterGroups = []ParameterGroup{}
	}
	if model.Features == nil {
		model.Features = []Feature{}
	}
	if model.Parameters == nil {
		model.Parameters = []Parameter{}
	}
	if model.Metadata == nil {
		model.Metadata = []Metadata{}
	}
	if model.ProductIntents == nil {
		model.ProductIntents = []ProductIntent{}
	}
	for i := range model.Components {
		if model.Components[i].ChildrenIDs == nil {
			model.Components[i].ChildrenIDs = []string{}
		}
	}
	for i := range model.ProductIntents {
		if model.ProductIntents[i].RequestedOutputFormats == nil {
			model.ProductIntents[i].RequestedOutputFormats = []string{}
		}
		if model.ProductIntents[i].Outputs == nil {
			model.ProductIntents[i].Outputs = []OutputIntent{}
		}
		if model.ProductIntents[i].ExportedParameters == nil {
			model.ProductIntents[i].ExportedParameters = []ExportedParameterIntent{}
		}
		if model.ProductIntents[i].Mutations == nil {
			model.ProductIntents[i].Mutations = []MutationIntent{}
		}
	}
}

func sortComponents(components []Component) {
	slices.SortFunc(components, func(a, b Component) int {
		if result := compareStrings(a.ID, b.ID); result != 0 {
			return result
		}
		return compareStrings(a.ParentID, b.ParentID)
	})
	for i := range components {
		slices.Sort(components[i].ChildrenIDs)
	}
}

func sortParameterGroups(groups []ParameterGroup) {
	slices.SortFunc(groups, func(a, b ParameterGroup) int { return compareStrings(a.ID, b.ID) })
}

func sortFeatures(features []Feature) {
	slices.SortFunc(features, func(a, b Feature) int { return compareStrings(a.ID, b.ID) })
}

func sortParameters(parameters []Parameter) {
	slices.SortFunc(parameters, func(a, b Parameter) int { return compareStrings(a.ID, b.ID) })
}

func sortMetadata(metadata []Metadata) {
	slices.SortFunc(metadata, func(a, b Metadata) int { return compareStrings(a.ID, b.ID) })
}

func sortProductIntents(intents []ProductIntent) {
	slices.SortFunc(intents, func(a, b ProductIntent) int {
		if result := compareStrings(a.Name, b.Name); result != 0 {
			return result
		}
		if result := compareStrings(a.Adapter, b.Adapter); result != 0 {
			return result
		}
		return compareStrings(a.SourceModelLogicalID, b.SourceModelLogicalID)
	})
	for i := range intents {
		slices.Sort(intents[i].RequestedOutputFormats)
		slices.SortFunc(intents[i].Outputs, func(a, b OutputIntent) int {
			if result := compareStrings(a.OutputType, b.OutputType); result != 0 {
				return result
			}
			if result := compareStrings(a.TargetEntityKind, b.TargetEntityKind); result != 0 {
				return result
			}
			if result := compareStrings(a.TargetSemanticID, b.TargetSemanticID); result != 0 {
				return result
			}
			return compareStrings(a.Scope, b.Scope)
		})
		slices.SortFunc(intents[i].ExportedParameters, func(a, b ExportedParameterIntent) int {
			if result := compareStrings(a.DSLParameterName, b.DSLParameterName); result != 0 {
				return result
			}
			if result := compareStrings(a.SemanticParameterID, b.SemanticParameterID); result != 0 {
				return result
			}
			return compareStrings(a.Type, b.Type)
		})
		slices.SortFunc(intents[i].Mutations, func(a, b MutationIntent) int {
			if result := compareStrings(a.OperationKind, b.OperationKind); result != 0 {
				return result
			}
			if result := compareStrings(a.TargetEntityKind, b.TargetEntityKind); result != 0 {
				return result
			}
			if result := compareStrings(a.TargetSemanticID, b.TargetSemanticID); result != 0 {
				return result
			}
			if result := compareStrings(a.TargetField, b.TargetField); result != 0 {
				return result
			}
			if result := compareStrings(a.Scope, b.Scope); result != 0 {
				return result
			}
			if result := compareStrings(a.ValueSource.Kind, b.ValueSource.Kind); result != 0 {
				return result
			}
			if result := compareStrings(a.ValueSource.DSLParameterName, b.ValueSource.DSLParameterName); result != 0 {
				return result
			}
			return compareStrings(a.ValueSource.SemanticParameterID, b.ValueSource.SemanticParameterID)
		})
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
