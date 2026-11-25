package semantic

import (
	"fmt"
	"slices"
	"strings"

	"parametron/internal/engine/resourcebinding"
)

func validateModel(model *Model) []string {
	if model == nil {
		return []string{"semantic model must not be nil"}
	}

	var problems []string
	if strings.TrimSpace(model.SchemaVersion) == "" {
		problems = append(problems, "schemaVersion is required")
	} else if model.SchemaVersion != SchemaVersion {
		problems = append(problems, fmt.Sprintf("schemaVersion must be %q", SchemaVersion))
	}
	if strings.TrimSpace(model.RootComponentID) == "" {
		problems = append(problems, "rootComponentId is required")
	}

	componentIDs, componentProblems := collectIDs("components", len(model.Components), func(i int) string {
		return model.Components[i].ID
	})
	featureIDs, featureProblems := collectIDs("features", len(model.Features), func(i int) string {
		return model.Features[i].ID
	})
	groupIDs, groupProblems := collectIDs("parameterGroups", len(model.ParameterGroups), func(i int) string {
		return model.ParameterGroups[i].ID
	})
	parameterIDs, parameterProblems := collectIDs("parameters", len(model.Parameters), func(i int) string {
		return model.Parameters[i].ID
	})
	metadataIDs, metadataProblems := collectIDs("metadata", len(model.Metadata), func(i int) string {
		return model.Metadata[i].ID
	})
	drawingIDs, drawingProblems := collectIDs("drawings", len(model.Drawings), func(i int) string {
		return model.Drawings[i].ID
	})
	sheetIDs, sheetProblems := collectSheetIDs(model)

	problems = append(problems, componentProblems...)
	problems = append(problems, featureProblems...)
	problems = append(problems, groupProblems...)
	problems = append(problems, parameterProblems...)
	problems = append(problems, metadataProblems...)
	problems = append(problems, drawingProblems...)
	problems = append(problems, sheetProblems...)
	problems = append(problems, collectCrossCollectionIDConflicts(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs, drawingIDs)...)
	problems = append(problems, collectSheetIDConflicts(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs, drawingIDs, sheetIDs)...)

	if model.RootComponentID != "" {
		if _, ok := componentIDs[model.RootComponentID]; !ok {
			problems = append(problems, fmt.Sprintf("rootComponentId %q must resolve to an existing component", model.RootComponentID))
		}
	}

	for i, component := range model.Components {
		location := fmt.Sprintf("components[%d]", i)
		if component.ParentID != "" {
			if _, ok := componentIDs[component.ParentID]; !ok {
				problems = append(problems, fmt.Sprintf("%s.parentId %q must resolve to an existing component", location, component.ParentID))
			}
		}
		for childIndex, childID := range component.ChildrenIDs {
			if _, ok := componentIDs[childID]; !ok {
				problems = append(problems, fmt.Sprintf("%s.childrenIds[%d] %q must resolve to an existing component", location, childIndex, childID))
			}
		}
	}

	for i, group := range model.ParameterGroups {
		location := fmt.Sprintf("parameterGroups[%d]", i)
		if group.OwnerComponentID != "" {
			if _, ok := componentIDs[group.OwnerComponentID]; !ok {
				problems = append(problems, fmt.Sprintf("%s.ownerComponentId %q must resolve to an existing component", location, group.OwnerComponentID))
			}
		}
	}

	for i, feature := range model.Features {
		location := fmt.Sprintf("features[%d]", i)
		if feature.ComponentID != "" {
			if _, ok := componentIDs[feature.ComponentID]; !ok {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must resolve to an existing component", location, feature.ComponentID))
			}
		}
	}

	for i, parameter := range model.Parameters {
		location := fmt.Sprintf("parameters[%d]", i)
		if parameter.ComponentID != "" {
			if _, ok := componentIDs[parameter.ComponentID]; !ok {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must resolve to an existing component", location, parameter.ComponentID))
			}
		}
		if parameter.GroupID != "" {
			groupIndex, ok := groupIDs[parameter.GroupID]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s.groupId %q must resolve to an existing parameter group", location, parameter.GroupID))
			} else if model.ParameterGroups[groupIndex].OwnerComponentID != parameter.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must match group owner component %q", location, parameter.ComponentID, model.ParameterGroups[groupIndex].OwnerComponentID))
			}
		}
		switch parameter.OwnerKind {
		case OwnerKindComponent:
			if parameter.OwnerID != "" {
				if _, ok := componentIDs[parameter.OwnerID]; !ok {
					problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing component", location, parameter.OwnerID))
				}
			}
		case OwnerKindGroup:
			groupIndex, ok := groupIDs[parameter.OwnerID]
			if !ok && parameter.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing parameter group", location, parameter.OwnerID))
			} else if ok && model.ParameterGroups[groupIndex].OwnerComponentID != parameter.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must match owner parameter group component %q", location, parameter.ComponentID, model.ParameterGroups[groupIndex].OwnerComponentID))
			}
		case OwnerKindFeature:
			featureIndex, ok := featureIDs[parameter.OwnerID]
			if !ok && parameter.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing feature", location, parameter.OwnerID))
			} else if ok && model.Features[featureIndex].ComponentID != parameter.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must match owner feature component %q", location, parameter.ComponentID, model.Features[featureIndex].ComponentID))
			}
		}
	}

	for i, entry := range model.Metadata {
		location := fmt.Sprintf("metadata[%d]", i)
		if entry.ComponentID != "" {
			if _, ok := componentIDs[entry.ComponentID]; !ok {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must resolve to an existing component", location, entry.ComponentID))
			}
		}
		switch entry.OwnerKind {
		case OwnerKindComponent:
			if entry.OwnerID != "" {
				if _, ok := componentIDs[entry.OwnerID]; !ok {
					problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing component", location, entry.OwnerID))
				}
			}
		case OwnerKindFeature:
			featureIndex, ok := featureIDs[entry.OwnerID]
			if !ok && entry.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing feature", location, entry.OwnerID))
			} else if ok && model.Features[featureIndex].ComponentID != entry.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must match owner feature component %q", location, entry.ComponentID, model.Features[featureIndex].ComponentID))
			}
		}
	}

	drawingNames := map[string]int{}
	for i, drawing := range model.Drawings {
		location := fmt.Sprintf("drawings[%d]", i)
		problems = append(problems, validateDrawing(location, drawing, componentIDs)...)
		if strings.TrimSpace(drawing.Name) != "" {
			if previous, ok := drawingNames[drawing.Name]; ok {
				problems = append(problems, fmt.Sprintf("%s.name %q duplicates drawing name already declared at drawings[%d].name", location, drawing.Name, previous))
			} else {
				drawingNames[drawing.Name] = i
			}
		}
	}

	for i, intent := range model.ProductIntents {
		location := fmt.Sprintf("productIntents[%d]", i)
		if strings.TrimSpace(intent.Name) == "" {
			problems = append(problems, location+".name is required")
		}
		if isSemanticCaptureBackedAdapter(intent.Adapter) && intent.SourceModelLogicalID != model.SourceDocumentLogicalID {
			problems = append(problems, fmt.Sprintf("%s.sourceModelLogicalId %q must match sourceDocumentLogicalId %q", location, intent.SourceModelLogicalID, model.SourceDocumentLogicalID))
		}
		for outputIndex, format := range intent.RequestedOutputFormats {
			if strings.TrimSpace(format) == "" {
				problems = append(problems, fmt.Sprintf("%s.requestedOutputFormats[%d] must not be empty", location, outputIndex))
			}
		}
		for outputIndex, output := range intent.Outputs {
			outputLocation := fmt.Sprintf("%s.outputs[%d]", location, outputIndex)
			if strings.TrimSpace(output.OutputType) == "" {
				problems = append(problems, outputLocation+".outputType is required")
			}
			switch output.OutputType {
			case "pdf":
				if output.TargetEntityKind != "" || output.TargetSemanticID != "" || output.Scope != "" {
					problems = append(problems, outputLocation+" must not declare target or scope for pdf outputs")
				}
			case "step":
				if strings.TrimSpace(output.TargetEntityKind) == "" {
					problems = append(problems, outputLocation+".targetEntityKind is required for step outputs")
				}
				if strings.TrimSpace(output.TargetSemanticID) == "" {
					problems = append(problems, outputLocation+".targetSemanticId is required for step outputs")
				}
			case "csv":
				if strings.TrimSpace(output.TargetEntityKind) == "" {
					problems = append(problems, outputLocation+".targetEntityKind is required for csv outputs")
				}
				if strings.TrimSpace(output.TargetSemanticID) == "" {
					problems = append(problems, outputLocation+".targetSemanticId is required for csv outputs")
				}
				if strings.TrimSpace(output.Scope) == "" {
					problems = append(problems, outputLocation+".scope is required for csv outputs")
				}
			}
			if output.TargetSemanticID != "" && !semanticEntityExists(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs, output.TargetSemanticID) {
				problems = append(problems, fmt.Sprintf("%s.targetSemanticId %q must resolve to an existing semantic entity", outputLocation, output.TargetSemanticID))
			}
		}
		for exportIndex, export := range intent.ExportedParameters {
			exportLocation := fmt.Sprintf("%s.exportedParameters[%d]", location, exportIndex)
			if strings.TrimSpace(export.DSLParameterName) == "" {
				problems = append(problems, exportLocation+".dslParameterName is required")
			}
			if strings.TrimSpace(export.SemanticParameterID) == "" {
				problems = append(problems, exportLocation+".semanticParameterId is required")
				continue
			}
			if _, ok := parameterIDs[export.SemanticParameterID]; !ok {
				problems = append(problems, fmt.Sprintf("%s.semanticParameterId %q must resolve to an existing parameter", exportLocation, export.SemanticParameterID))
			}
		}
		for mutationIndex, mutation := range intent.Mutations {
			mutationLocation := fmt.Sprintf("%s.mutations[%d]", location, mutationIndex)
			if strings.TrimSpace(mutation.OperationKind) == "" {
				problems = append(problems, mutationLocation+".operationKind is required")
			}
			if strings.TrimSpace(mutation.TargetEntityKind) == "" {
				problems = append(problems, mutationLocation+".targetEntityKind is required")
			}
			if strings.TrimSpace(mutation.TargetSemanticID) == "" {
				problems = append(problems, mutationLocation+".targetSemanticId is required")
			} else if !semanticEntityExists(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs, mutation.TargetSemanticID) {
				problems = append(problems, fmt.Sprintf("%s.targetSemanticId %q must resolve to an existing semantic entity", mutationLocation, mutation.TargetSemanticID))
			}
			if strings.TrimSpace(mutation.Scope) == "" {
				problems = append(problems, mutationLocation+".scope is required")
			}
			if strings.TrimSpace(mutation.ValueSource.Kind) == "" {
				problems = append(problems, mutationLocation+".valueSource.kind is required")
			}
			if mutation.ValueSource.SemanticParameterID != "" {
				if _, ok := parameterIDs[mutation.ValueSource.SemanticParameterID]; !ok {
					problems = append(problems, fmt.Sprintf("%s.valueSource.semanticParameterId %q must resolve to an existing parameter", mutationLocation, mutation.ValueSource.SemanticParameterID))
				}
			}
		}
	}

	slices.Sort(problems)
	return problems
}

func collectIDs(prefix string, count int, idAt func(index int) string) (map[string]int, []string) {
	ids := make(map[string]int, count)
	var problems []string
	for i := 0; i < count; i++ {
		id := idAt(i)
		location := fmt.Sprintf("%s[%d]", prefix, i)
		if strings.TrimSpace(id) == "" {
			problems = append(problems, location+".id is required")
			continue
		}
		if previous, ok := ids[id]; ok {
			problems = append(problems, fmt.Sprintf("%s.id duplicates stable ID %q already declared at %s[%d].id", location, id, prefix, previous))
			continue
		}
		ids[id] = i
	}
	return ids, problems
}

func collectSheetIDs(model *Model) (map[string]string, []string) {
	ids := map[string]string{}
	var problems []string
	for i, drawing := range model.Drawings {
		for j, sheet := range drawing.Sheets {
			location := fmt.Sprintf("drawings[%d].sheets[%d]", i, j)
			if strings.TrimSpace(sheet.ID) == "" {
				problems = append(problems, location+".id is required")
				continue
			}
			if previous, ok := ids[sheet.ID]; ok {
				problems = append(problems, fmt.Sprintf("%s.id duplicates stable ID %q already declared at %s.id", location, sheet.ID, previous))
				continue
			}
			ids[sheet.ID] = location
		}
	}
	return ids, problems
}

func validateDrawing(location string, drawing Drawing, componentIDs map[string]int) []string {
	var problems []string
	if strings.TrimSpace(drawing.Name) == "" {
		problems = append(problems, location+".name is required")
	}
	problems = append(problems, validateDrawingResource(location, drawing.Resource)...)

	if len(drawing.ComponentIDs) == 0 {
		problems = append(problems, location+".componentIds must not be empty")
	}
	seenComponents := map[string]int{}
	for index, componentID := range drawing.ComponentIDs {
		if strings.TrimSpace(componentID) == "" {
			problems = append(problems, fmt.Sprintf("%s.componentIds[%d] is required", location, index))
			continue
		}
		if previous, ok := seenComponents[componentID]; ok {
			problems = append(problems, fmt.Sprintf("%s.componentIds[%d] duplicates component ID %q already declared at %s.componentIds[%d]", location, index, componentID, location, previous))
		} else {
			seenComponents[componentID] = index
		}
		if _, ok := componentIDs[componentID]; !ok {
			problems = append(problems, fmt.Sprintf("%s.componentIds[%d] %q must resolve to an existing component", location, index, componentID))
		}
	}

	if len(drawing.Sheets) == 0 {
		problems = append(problems, location+".sheets must not be empty")
	}
	sheetNames := map[string]int{}
	sheetOrders := map[int]int{}
	for index, sheet := range drawing.Sheets {
		sheetLocation := fmt.Sprintf("%s.sheets[%d]", location, index)
		problems = append(problems, validateDrawingSheet(sheetLocation, sheet)...)
		if strings.TrimSpace(sheet.Name) != "" {
			if previous, ok := sheetNames[sheet.Name]; ok {
				problems = append(problems, fmt.Sprintf("%s.name %q duplicates sheet name already declared at %s.sheets[%d].name", sheetLocation, sheet.Name, location, previous))
			} else {
				sheetNames[sheet.Name] = index
			}
		}
		if sheet.Order >= 1 {
			if previous, ok := sheetOrders[sheet.Order]; ok {
				problems = append(problems, fmt.Sprintf("%s.order %d duplicates sheet order already declared at %s.sheets[%d].order", sheetLocation, sheet.Order, location, previous))
			} else {
				sheetOrders[sheet.Order] = index
			}
		}
	}

	return problems
}

func validateDrawingResource(location string, resource DrawingResource) []string {
	var problems []string
	switch resource.Topology {
	case "embedded":
		if resource.Path != "" {
			problems = append(problems, location+".resource.path must be empty when topology is \"embedded\"")
		}
	case "separate":
		if strings.TrimSpace(resource.Path) == "" {
			problems = append(problems, location+".resource.path is required when topology is \"separate\"")
		} else if err := resourcebinding.ValidateCoordinate(resource.Path); err != nil {
			problems = append(problems, fmt.Sprintf("%s.resource.path %v", location, err))
		}
	default:
		problems = append(problems, location+".resource.topology must be one of \"embedded\" or \"separate\"")
	}
	return problems
}

func validateDrawingSheet(location string, sheet DrawingSheet) []string {
	var problems []string
	if strings.TrimSpace(sheet.Name) == "" {
		problems = append(problems, location+".name is required")
	}
	if strings.TrimSpace(sheet.NativeType) == "" {
		problems = append(problems, location+".nativeType is required")
	}
	if sheet.Order < 1 {
		problems = append(problems, location+".order must be a positive integer")
	}
	return problems
}

func collectCrossCollectionIDConflicts(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs, drawingIDs map[string]int) []string {
	type collection struct {
		name string
		ids  map[string]int
	}

	collections := []collection{
		{name: "components", ids: componentIDs},
		{name: "features", ids: featureIDs},
		{name: "parameterGroups", ids: groupIDs},
		{name: "parameters", ids: parameterIDs},
		{name: "metadata", ids: metadataIDs},
		{name: "drawings", ids: drawingIDs},
	}

	var problems []string
	for i := 0; i < len(collections); i++ {
		for j := i + 1; j < len(collections); j++ {
			left := collections[i]
			right := collections[j]
			for id, leftIndex := range left.ids {
				rightIndex, ok := right.ids[id]
				if !ok {
					continue
				}
				problems = append(problems, fmt.Sprintf("%s[%d].id duplicates stable ID %q already declared at %s[%d].id", right.name, rightIndex, id, left.name, leftIndex))
			}
		}
	}
	return problems
}

func collectSheetIDConflicts(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs, drawingIDs map[string]int, sheetIDs map[string]string) []string {
	type collection struct {
		name string
		ids  map[string]int
	}
	collections := []collection{
		{name: "components", ids: componentIDs},
		{name: "features", ids: featureIDs},
		{name: "parameterGroups", ids: groupIDs},
		{name: "parameters", ids: parameterIDs},
		{name: "metadata", ids: metadataIDs},
		{name: "drawings", ids: drawingIDs},
	}

	var problems []string
	for id, location := range sheetIDs {
		for _, collection := range collections {
			index, ok := collection.ids[id]
			if !ok {
				continue
			}
			problems = append(problems, fmt.Sprintf("%s.id duplicates stable ID %q already declared at %s[%d].id", location, id, collection.name, index))
		}
	}
	return problems
}

func semanticEntityExists(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs map[string]int, id string) bool {
	if _, ok := componentIDs[id]; ok {
		return true
	}
	if _, ok := featureIDs[id]; ok {
		return true
	}
	if _, ok := groupIDs[id]; ok {
		return true
	}
	if _, ok := parameterIDs[id]; ok {
		return true
	}
	if _, ok := metadataIDs[id]; ok {
		return true
	}
	return false
}
