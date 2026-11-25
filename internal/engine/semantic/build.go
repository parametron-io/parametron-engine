package semantic

import "parametron/internal/engine/cad"

func BuildFromCapture(contract *cad.CADContract) (*Model, error) {
	if err := cad.Validate(contract); err != nil {
		return nil, err
	}

	structureByComponentID := make(map[string]cad.StructureNode, len(contract.Structure.Nodes))
	for _, node := range contract.Structure.Nodes {
		nodeCopy := node
		if node.Children == nil {
			nodeCopy.Children = nil
		} else {
			nodeCopy.Children = append([]string(nil), node.Children...)
		}
		structureByComponentID[node.ComponentID] = nodeCopy
	}

	model := &Model{
		SchemaVersion:           SchemaVersion,
		SourceDocumentLogicalID: contract.SourceDocument.LogicalID,
		Adapter:                 copySystemIdentity(contract.Adapter),
		CADSystem:               copySystemIdentity(contract.CADSystem),
		RootComponentID:         contract.Structure.RootComponentID,
		Components:              make([]Component, 0, len(contract.Entities.Components)),
		Features:                make([]Feature, 0, len(contract.Entities.Features)),
		ParameterGroups:         make([]ParameterGroup, 0, len(contract.Entities.ParameterGroups)),
		Parameters:              make([]Parameter, 0, len(contract.Entities.Parameters)),
		Metadata:                make([]Metadata, 0, len(contract.Entities.Metadata)),
		Drawings:                make([]Drawing, 0, len(contract.Entities.Drawings)),
	}

	for _, component := range contract.Entities.Components {
		node := structureByComponentID[component.ID]
		children := append([]string(nil), node.Children...)
		model.Components = append(model.Components, Component{
			NativeRef:     capturedNativeRef(component.IdentitySource),
			ID:            component.ID,
			Kind:          component.Kind,
			Name:          component.Name,
			DisplayName:   component.DisplayName,
			ParentID:      node.ParentComponentID,
			ChildrenIDs:   children,
			Material:      component.Material,
			Quantity:      component.Quantity,
			Targetability: component.Targetability,
		})
	}

	for _, group := range contract.Entities.ParameterGroups {
		model.ParameterGroups = append(model.ParameterGroups, ParameterGroup{
			ID:               group.ID,
			OwnerComponentID: group.OwnerComponentID,
			Name:             group.Name,
			DisplayName:      group.DisplayName,
			GroupKind:        group.GroupKind,
			NativeType:       group.CADType,
			Observable:       group.Observable,
			Writable:         group.Writable,
		})
	}

	for _, feature := range contract.Entities.Features {
		model.Features = append(model.Features, Feature{
			NativeRef:     capturedNativeRef(feature.IdentitySource),
			ID:            feature.ID,
			ComponentID:   feature.ComponentID,
			Name:          feature.Name,
			DisplayName:   feature.DisplayName,
			NativeType:    feature.CADType,
			Targetability: feature.Targetability,
		})
	}

	for _, parameter := range contract.Entities.Parameters {
		model.Parameters = append(model.Parameters, Parameter{
			ID:           parameter.ID,
			OwnerKind:    parameter.OwnerKind,
			OwnerID:      parameter.OwnerID,
			ComponentID:  parameter.ComponentID,
			GroupID:      parameterGroupID(parameter),
			Name:         parameter.Name,
			DisplayName:  parameter.DisplayName,
			ValueType:    parameter.ValueType,
			NativeType:   parameter.CADType,
			Unit:         parameter.Unit,
			Observable:   parameter.Observable,
			Writable:     parameter.Writable,
			CurrentValue: copyScalarValue(parameter.CurrentValue),
		})
	}

	for _, entry := range contract.Entities.Metadata {
		model.Metadata = append(model.Metadata, Metadata{
			ID:           entry.ID,
			OwnerKind:    entry.OwnerKind,
			OwnerID:      entry.OwnerID,
			ComponentID:  entry.ComponentID,
			Key:          entry.Key,
			DisplayName:  entry.DisplayName,
			ValueType:    entry.ValueType,
			NativeType:   entry.CADType,
			Observable:   entry.Observable,
			Writable:     entry.Writable,
			CurrentValue: copyScalarValue(entry.CurrentValue),
		})
	}

	for _, drawing := range contract.Entities.Drawings {
		sheets := make([]DrawingSheet, 0, len(drawing.Sheets))
		for _, sheet := range drawing.Sheets {
			sheets = append(sheets, DrawingSheet{
				NativeRef:   sheet.IdentitySource.NativeRef,
				ID:          sheet.ID,
				Name:        sheet.Name,
				DisplayName: sheet.DisplayName,
				Order:       sheet.Order,
				NativeType:  sheet.CADType,
			})
		}
		model.Drawings = append(model.Drawings, Drawing{
			NativeRef:    capturedNativeRef(drawing.IdentitySource),
			ID:           drawing.ID,
			Name:         drawing.Name,
			DisplayName:  drawing.DisplayName,
			ComponentIDs: append([]string(nil), drawing.ComponentIDs...),
			Resource: DrawingResource{
				Topology: drawing.Resource.Topology,
				Path:     drawing.Resource.Path,
			},
			Sheets: sheets,
		})
	}

	canonical, err := Canonicalize(model)
	if err != nil {
		return nil, err
	}

	return canonical, nil
}

func capturedNativeRef(identity *cad.IdentitySource) string {
	if identity == nil {
		return ""
	}
	return identity.NativeRef
}

func copySystemIdentity(value cad.NamedVersion) *SystemIdentity {
	if value.Name == "" && value.Version == "" {
		return nil
	}
	return &SystemIdentity{Name: value.Name, Version: value.Version}
}

func parameterGroupID(parameter cad.Parameter) string {
	if parameter.OwnerKind == OwnerKindGroup {
		return parameter.OwnerID
	}
	return ""
}

func copyScalarValue(value cad.ScalarValue) ScalarValue {
	if value.IsZero() {
		return ScalarValue{}
	}
	copyValue, err := NewScalarValue(value.Raw())
	if err != nil {
		return ScalarValue{}
	}
	return copyValue
}
