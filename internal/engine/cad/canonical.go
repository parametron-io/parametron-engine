package cad

import (
	"encoding/json"
	"slices"
)

func CanonicalJSON(contract *CADContract) ([]byte, error) {
	if err := Validate(contract); err != nil {
		return nil, err
	}

	normalized := cloneContract(contract)
	sortForCanonical(normalized)
	return json.Marshal(normalized)
}

func cloneContract(in *CADContract) *CADContract {
	if in == nil {
		return nil
	}

	out := *in
	out.Entities.Components = cloneComponents(in.Entities.Components)
	out.Entities.Features = cloneFeatures(in.Entities.Features)
	out.Entities.Relationships = cloneRelationships(in.Entities.Relationships)
	out.Entities.ParameterGroups = cloneParameterGroups(in.Entities.ParameterGroups)
	out.Entities.Parameters = cloneParameters(in.Entities.Parameters)
	out.Entities.Metadata = cloneMetadata(in.Entities.Metadata)
	out.Structure.Nodes = cloneStructureNodes(in.Structure.Nodes)

	for i := range out.Entities.Components {
		if in.Entities.Components[i].IdentitySource != nil {
			copyValue := *in.Entities.Components[i].IdentitySource
			out.Entities.Components[i].IdentitySource = &copyValue
		}
		if in.Entities.Components[i].SourceFile != nil {
			copyValue := *in.Entities.Components[i].SourceFile
			out.Entities.Components[i].SourceFile = &copyValue
		}
	}
	for i := range out.Entities.Features {
		if in.Entities.Features[i].IdentitySource != nil {
			copyValue := *in.Entities.Features[i].IdentitySource
			out.Entities.Features[i].IdentitySource = &copyValue
		}
	}
	for i := range out.Entities.Relationships {
		if in.Entities.Relationships[i].Endpoints == nil {
			out.Entities.Relationships[i].Endpoints = nil
		} else {
			out.Entities.Relationships[i].Endpoints = append([]string{}, in.Entities.Relationships[i].Endpoints...)
		}
		if in.Entities.Relationships[i].IdentitySource != nil {
			copyValue := *in.Entities.Relationships[i].IdentitySource
			out.Entities.Relationships[i].IdentitySource = &copyValue
		}
	}
	for i := range out.Entities.ParameterGroups {
		if in.Entities.ParameterGroups[i].IdentitySource != nil {
			copyValue := *in.Entities.ParameterGroups[i].IdentitySource
			out.Entities.ParameterGroups[i].IdentitySource = &copyValue
		}
	}
	for i := range out.Entities.Parameters {
		if in.Entities.Parameters[i].IdentitySource != nil {
			copyValue := *in.Entities.Parameters[i].IdentitySource
			out.Entities.Parameters[i].IdentitySource = &copyValue
		}
	}
	for i := range out.Entities.Metadata {
		if in.Entities.Metadata[i].IdentitySource != nil {
			copyValue := *in.Entities.Metadata[i].IdentitySource
			out.Entities.Metadata[i].IdentitySource = &copyValue
		}
	}
	for i := range out.Structure.Nodes {
		if in.Structure.Nodes[i].Children == nil {
			out.Structure.Nodes[i].Children = nil
		} else {
			out.Structure.Nodes[i].Children = append([]string{}, in.Structure.Nodes[i].Children...)
		}
	}

	return &out
}

func cloneComponents(in []Component) []Component {
	if in == nil {
		return nil
	}
	out := make([]Component, len(in))
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

func cloneRelationships(in []Relationship) []Relationship {
	if in == nil {
		return nil
	}
	out := make([]Relationship, len(in))
	copy(out, in)
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

func cloneParameters(in []Parameter) []Parameter {
	if in == nil {
		return nil
	}
	out := make([]Parameter, len(in))
	copy(out, in)
	return out
}

func cloneMetadata(in []Metadata) []Metadata {
	if in == nil {
		return nil
	}
	out := make([]Metadata, len(in))
	copy(out, in)
	return out
}

func cloneStructureNodes(in []StructureNode) []StructureNode {
	if in == nil {
		return nil
	}
	out := make([]StructureNode, len(in))
	copy(out, in)
	for i := range out {
		if in[i].Children == nil {
			out[i].Children = nil
			continue
		}
		out[i].Children = append([]string{}, in[i].Children...)
	}
	return out
}

func sortForCanonical(contract *CADContract) {
	slices.SortFunc(contract.Entities.Components, func(a, b Component) int { return compareStrings(a.ID, b.ID) })
	slices.SortFunc(contract.Entities.Features, func(a, b Feature) int { return compareStrings(a.ID, b.ID) })
	slices.SortFunc(contract.Entities.Relationships, func(a, b Relationship) int { return compareStrings(a.ID, b.ID) })
	slices.SortFunc(contract.Entities.ParameterGroups, func(a, b ParameterGroup) int { return compareStrings(a.ID, b.ID) })
	slices.SortFunc(contract.Entities.Parameters, func(a, b Parameter) int { return compareStrings(a.ID, b.ID) })
	slices.SortFunc(contract.Entities.Metadata, func(a, b Metadata) int { return compareStrings(a.ID, b.ID) })
	slices.SortFunc(contract.Structure.Nodes, func(a, b StructureNode) int { return compareStrings(a.ComponentID, b.ComponentID) })
	for i := range contract.Structure.Nodes {
		slices.Sort(contract.Structure.Nodes[i].Children)
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
