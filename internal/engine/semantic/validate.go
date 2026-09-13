package semantic

import (
	"fmt"
	"slices"
	"strings"
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

	problems = append(problems, componentProblems...)
	problems = append(problems, featureProblems...)
	problems = append(problems, groupProblems...)
	problems = append(problems, parameterProblems...)
	problems = append(problems, metadataProblems...)
	problems = append(problems, collectCrossCollectionIDConflicts(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs)...)

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

func collectCrossCollectionIDConflicts(componentIDs, featureIDs, groupIDs, parameterIDs, metadataIDs map[string]int) []string {
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
