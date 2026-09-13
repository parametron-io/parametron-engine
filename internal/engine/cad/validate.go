package cad

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func Validate(contract *CADContract) error {
	problems := validateContract(contract)
	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

func validateContract(contract *CADContract) []string {
	if contract == nil {
		return []string{"cad contract must not be nil"}
	}

	var problems []string

	if contract.SchemaVersion != SchemaVersion {
		problems = append(problems, fmt.Sprintf("schemaVersion must be %q", SchemaVersion))
	}
	if strings.TrimSpace(contract.CaptureID) == "" {
		problems = append(problems, "captureId is required")
	} else if contract.CaptureID != strings.TrimSpace(contract.CaptureID) {
		problems = append(problems, "captureId must not have leading or trailing whitespace")
	}

	problems = append(problems, validateNamedVersion("adapter", contract.Adapter)...)
	problems = append(problems, validateNamedVersion("cadSystem", contract.CADSystem)...)
	problems = append(problems, validateSourceDocument(contract.SourceDocument)...)
	problems = append(problems, validateAnnotations("annotations", contract.Annotations)...)

	if strings.TrimSpace(contract.RootProduct.ID) == "" {
		problems = append(problems, "rootProduct.id is required")
	}

	if contract.Entities.Components == nil {
		problems = append(problems, "entities.components is required")
	}
	if contract.Entities.Features == nil {
		problems = append(problems, "entities.features is required")
	}
	if contract.Entities.Relationships == nil {
		problems = append(problems, "entities.relationships is required")
	}
	if contract.Entities.ParameterGroups == nil {
		problems = append(problems, "entities.parameterGroups is required")
	}
	if contract.Entities.Parameters == nil {
		problems = append(problems, "entities.parameters is required")
	}
	if contract.Entities.Metadata == nil {
		problems = append(problems, "entities.metadata is required")
	}
	if contract.Structure.Nodes == nil {
		problems = append(problems, "structure.nodes is required")
	}
	if strings.TrimSpace(contract.Structure.RootComponentID) == "" {
		problems = append(problems, "structure.rootComponentId is required")
	}
	if len(contract.Structure.Nodes) == 0 {
		problems = append(problems, "structure.nodes must not be empty")
	}

	componentIDs := make(map[string]int, len(contract.Entities.Components))
	featureIDs := make(map[string]int, len(contract.Entities.Features))
	relationshipIDs := make(map[string]int, len(contract.Entities.Relationships))
	groupIDs := make(map[string]int, len(contract.Entities.ParameterGroups))
	parameterIDs := make(map[string]int, len(contract.Entities.Parameters))
	metadataIDs := make(map[string]int, len(contract.Entities.Metadata))
	allIDs := map[string]string{}

	addStableID := func(location, id string) {
		if strings.TrimSpace(id) == "" {
			return
		}
		if prev, ok := allIDs[id]; ok {
			problems = append(problems, fmt.Sprintf("%s.id duplicates stable ID %q already declared at %s.id", location, id, prev))
			return
		}
		allIDs[id] = location
	}

	for index, component := range contract.Entities.Components {
		location := fmt.Sprintf("entities.components[%d]", index)
		problems = append(problems, validateComponent(location, component)...)
		addStableID(location, component.ID)
		if component.ID != "" {
			componentIDs[component.ID] = index
		}
	}
	for index, feature := range contract.Entities.Features {
		location := fmt.Sprintf("entities.features[%d]", index)
		problems = append(problems, validateFeature(location, feature)...)
		addStableID(location, feature.ID)
		if feature.ID != "" {
			featureIDs[feature.ID] = index
		}
	}
	for index, relationship := range contract.Entities.Relationships {
		location := fmt.Sprintf("entities.relationships[%d]", index)
		problems = append(problems, validateRelationship(location, relationship)...)
		addStableID(location, relationship.ID)
		if relationship.ID != "" {
			relationshipIDs[relationship.ID] = index
		}
	}
	for index, group := range contract.Entities.ParameterGroups {
		location := fmt.Sprintf("entities.parameterGroups[%d]", index)
		problems = append(problems, validateParameterGroup(location, group)...)
		addStableID(location, group.ID)
		if group.ID != "" {
			groupIDs[group.ID] = index
		}
	}
	for index, parameter := range contract.Entities.Parameters {
		location := fmt.Sprintf("entities.parameters[%d]", index)
		problems = append(problems, validateParameter(location, parameter)...)
		addStableID(location, parameter.ID)
		if parameter.ID != "" {
			parameterIDs[parameter.ID] = index
		}
	}
	for index, entry := range contract.Entities.Metadata {
		location := fmt.Sprintf("entities.metadata[%d]", index)
		problems = append(problems, validateMetadata(location, entry)...)
		addStableID(location, entry.ID)
		if entry.ID != "" {
			metadataIDs[entry.ID] = index
		}
	}

	if contract.RootProduct.ID != "" {
		if _, ok := componentIDs[contract.RootProduct.ID]; !ok {
			problems = append(problems, fmt.Sprintf("rootProduct.id %q must resolve to an existing component", contract.RootProduct.ID))
		}
	}
	if contract.Structure.RootComponentID != "" {
		componentIndex, ok := componentIDs[contract.Structure.RootComponentID]
		if !ok {
			problems = append(problems, fmt.Sprintf("structure.rootComponentId %q must resolve to an existing component", contract.Structure.RootComponentID))
		} else if contract.Entities.Components[componentIndex].Kind != "assembly" {
			problems = append(problems, fmt.Sprintf("structure.rootComponentId %q must resolve to an assembly component", contract.Structure.RootComponentID))
		}
	}
	if contract.RootProduct.ID != "" && contract.Structure.RootComponentID != "" && contract.RootProduct.ID != contract.Structure.RootComponentID {
		problems = append(problems, "structure.rootComponentId must equal rootProduct.id")
	}

	for index, feature := range contract.Entities.Features {
		location := fmt.Sprintf("entities.features[%d]", index)
		if _, ok := componentIDs[feature.ComponentID]; !ok && feature.ComponentID != "" {
			problems = append(problems, fmt.Sprintf("%s.componentId %q must resolve to an existing component", location, feature.ComponentID))
		}
	}
	for index, relationship := range contract.Entities.Relationships {
		location := fmt.Sprintf("entities.relationships[%d]", index)
		if _, ok := componentIDs[relationship.ComponentID]; !ok && relationship.ComponentID != "" {
			problems = append(problems, fmt.Sprintf("%s.componentId %q must resolve to an existing component", location, relationship.ComponentID))
		}
		seenEndpoints := map[string]int{}
		for endpointIndex, endpoint := range relationship.Endpoints {
			if _, ok := componentIDs[endpoint]; !ok {
				if _, ok := featureIDs[endpoint]; !ok && endpoint != "" {
					problems = append(problems, fmt.Sprintf("%s.endpoints[%d] %q must resolve to an existing component or feature", location, endpointIndex, endpoint))
				}
			}
			if prev, ok := seenEndpoints[endpoint]; ok {
				problems = append(problems, fmt.Sprintf("%s.endpoints[%d] duplicates endpoint %q already declared at %s.endpoints[%d]", location, endpointIndex, endpoint, location, prev))
			} else {
				seenEndpoints[endpoint] = endpointIndex
			}
		}
	}
	for index, group := range contract.Entities.ParameterGroups {
		location := fmt.Sprintf("entities.parameterGroups[%d]", index)
		if _, ok := componentIDs[group.OwnerComponentID]; !ok && group.OwnerComponentID != "" {
			problems = append(problems, fmt.Sprintf("%s.ownerComponentId %q must resolve to an existing component", location, group.OwnerComponentID))
		}
	}
	for index, parameter := range contract.Entities.Parameters {
		location := fmt.Sprintf("entities.parameters[%d]", index)
		if _, ok := componentIDs[parameter.ComponentID]; !ok && parameter.ComponentID != "" {
			problems = append(problems, fmt.Sprintf("%s.componentId %q must resolve to an existing component", location, parameter.ComponentID))
		}
		switch parameter.OwnerKind {
		case "group":
			groupIndex, ok := groupIDs[parameter.OwnerID]
			if !ok && parameter.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing parameter group", location, parameter.OwnerID))
			} else if ok && contract.Entities.ParameterGroups[groupIndex].OwnerComponentID != parameter.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must match owner parameter group component %q", location, parameter.ComponentID, contract.Entities.ParameterGroups[groupIndex].OwnerComponentID))
			}
		case "component":
			if _, ok := componentIDs[parameter.OwnerID]; !ok && parameter.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing component", location, parameter.OwnerID))
			} else if parameter.OwnerID != "" && parameter.OwnerID != parameter.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must equal ownerId for component-owned parameters", location, parameter.ComponentID))
			}
		case "feature":
			featureIndex, ok := featureIDs[parameter.OwnerID]
			if !ok && parameter.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing feature", location, parameter.OwnerID))
			} else if ok && contract.Entities.Features[featureIndex].ComponentID != parameter.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must match owner feature component %q", location, parameter.ComponentID, contract.Entities.Features[featureIndex].ComponentID))
			}
		}
		if parameter.CurrentValue.set {
			problems = append(problems, validateScalarMatchesType(location+".currentValue", parameter.ValueType, parameter.CurrentValue)...)
		}
	}
	for index, entry := range contract.Entities.Metadata {
		location := fmt.Sprintf("entities.metadata[%d]", index)
		if _, ok := componentIDs[entry.ComponentID]; !ok && entry.ComponentID != "" {
			problems = append(problems, fmt.Sprintf("%s.componentId %q must resolve to an existing component", location, entry.ComponentID))
		}
		switch entry.OwnerKind {
		case "component":
			if _, ok := componentIDs[entry.OwnerID]; !ok && entry.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing component", location, entry.OwnerID))
			} else if entry.OwnerID != "" && entry.OwnerID != entry.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must equal ownerId for component-owned metadata", location, entry.ComponentID))
			}
		case "feature":
			featureIndex, ok := featureIDs[entry.OwnerID]
			if !ok && entry.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing feature", location, entry.OwnerID))
			} else if ok && contract.Entities.Features[featureIndex].ComponentID != entry.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must match owner feature component %q", location, entry.ComponentID, contract.Entities.Features[featureIndex].ComponentID))
			}
		case "relationship":
			relationshipIndex, ok := relationshipIDs[entry.OwnerID]
			if !ok && entry.OwnerID != "" {
				problems = append(problems, fmt.Sprintf("%s.ownerId %q must resolve to an existing relationship", location, entry.OwnerID))
			} else if ok && contract.Entities.Relationships[relationshipIndex].ComponentID != entry.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must match owner relationship component %q", location, entry.ComponentID, contract.Entities.Relationships[relationshipIndex].ComponentID))
			}
		}
		if entry.CurrentValue.set {
			problems = append(problems, validateScalarMatchesType(location+".currentValue", entry.ValueType, entry.CurrentValue)...)
		}
	}

	problems = append(problems, validateOwnerScopedIdentitySources(contract)...)
	problems = append(problems, validateStructure(contract, componentIDs)...)

	return problems
}

func validateNamedVersion(location string, value NamedVersion) []string {
	var problems []string
	if strings.TrimSpace(value.Name) == "" {
		problems = append(problems, location+".name is required")
	}
	if value.Version != "" && strings.TrimSpace(value.Version) == "" {
		problems = append(problems, location+".version must not be blank")
	}
	return problems
}

func validateSourceDocument(doc SourceDocument) []string {
	var problems []string
	if strings.TrimSpace(doc.LogicalID) == "" {
		problems = append(problems, "sourceDocument.logicalId is required")
	}
	if strings.TrimSpace(doc.Path) == "" {
		problems = append(problems, "sourceDocument.path is required")
	}
	if strings.TrimSpace(doc.Fingerprint) == "" {
		problems = append(problems, "sourceDocument.fingerprint is required")
	}
	return problems
}

func validateAnnotations(location string, annotations Annotations) []string {
	_ = location
	_ = annotations
	return nil
}

func validateComponent(location string, component Component) []string {
	var problems []string
	problems = append(problems, requireText(location+".id", component.ID)...)
	if component.Kind != "assembly" && component.Kind != "part" {
		problems = append(problems, location+".kind must be one of \"assembly\" or \"part\"")
	}
	problems = append(problems, requireText(location+".name", component.Name)...)
	if component.CADType == "" || strings.TrimSpace(component.CADType) == "" {
		problems = append(problems, location+".cadType is required")
	}
	if component.Quantity <= 0 {
		problems = append(problems, location+".quantity must be a positive integer")
	}
	problems = append(problems, validateOptionalSourceFile(location, component.SourceFile)...)
	problems = append(problems, validateIdentitySource(location, "component", component.IdentitySource)...)
	problems = append(problems, validateStabilityClass(location, component.StabilityClass)...)
	problems = append(problems, validateTargetability(location+".targetability", component.Targetability)...)
	problems = append(problems, validateAnnotationPresence(location+".annotations", component.Annotations)...)
	return problems
}

func validateFeature(location string, feature Feature) []string {
	var problems []string
	problems = append(problems, requireText(location+".id", feature.ID)...)
	problems = append(problems, requireText(location+".componentId", feature.ComponentID)...)
	problems = append(problems, requireText(location+".name", feature.Name)...)
	problems = append(problems, requireText(location+".cadType", feature.CADType)...)
	problems = append(problems, validateIdentitySource(location, "feature", feature.IdentitySource)...)
	problems = append(problems, validateStabilityClass(location, feature.StabilityClass)...)
	problems = append(problems, validateTargetability(location+".targetability", feature.Targetability)...)
	problems = append(problems, validateAnnotationPresence(location+".annotations", feature.Annotations)...)
	return problems
}

func validateRelationship(location string, relationship Relationship) []string {
	var problems []string
	problems = append(problems, requireText(location+".id", relationship.ID)...)
	if relationship.Kind != "mate" && relationship.Kind != "relationship" {
		problems = append(problems, location+".kind must be one of \"mate\" or \"relationship\"")
	}
	problems = append(problems, requireText(location+".componentId", relationship.ComponentID)...)
	problems = append(problems, requireText(location+".name", relationship.Name)...)
	problems = append(problems, requireText(location+".cadType", relationship.CADType)...)
	if len(relationship.Endpoints) == 0 {
		problems = append(problems, location+".endpoints must not be empty")
	}
	problems = append(problems, validateIdentitySource(location, "relationship", relationship.IdentitySource)...)
	problems = append(problems, validateStabilityClass(location, relationship.StabilityClass)...)
	problems = append(problems, validateTargetability(location+".targetability", relationship.Targetability)...)
	problems = append(problems, validateAnnotationPresence(location+".annotations", relationship.Annotations)...)
	return problems
}

func validateParameterGroup(location string, group ParameterGroup) []string {
	var problems []string
	problems = append(problems, requireText(location+".id", group.ID)...)
	problems = append(problems, requireText(location+".ownerComponentId", group.OwnerComponentID)...)
	problems = append(problems, requireText(location+".name", group.Name)...)
	if group.GroupKind != "varset" && group.GroupKind != "design_table" && group.GroupKind != "cad_native_group" {
		problems = append(problems, location+".groupKind must be one of \"varset\", \"design_table\", or \"cad_native_group\"")
	}
	problems = append(problems, requireText(location+".cadType", group.CADType)...)
	problems = append(problems, validateIdentitySource(location, "parameterGroup", group.IdentitySource)...)
	problems = append(problems, validateStabilityClass(location, group.StabilityClass)...)
	problems = append(problems, validateAnnotationPresence(location+".annotations", group.Annotations)...)
	return problems
}

func validateParameter(location string, parameter Parameter) []string {
	var problems []string
	problems = append(problems, requireText(location+".id", parameter.ID)...)
	if parameter.OwnerKind != "group" && parameter.OwnerKind != "component" && parameter.OwnerKind != "feature" {
		problems = append(problems, location+".ownerKind must be one of \"group\", \"component\", or \"feature\"")
	}
	problems = append(problems, requireText(location+".ownerId", parameter.OwnerID)...)
	problems = append(problems, requireText(location+".componentId", parameter.ComponentID)...)
	problems = append(problems, requireText(location+".name", parameter.Name)...)
	problems = append(problems, requireText(location+".cadType", parameter.CADType)...)
	if parameter.ValueType != "number" && parameter.ValueType != "string" && parameter.ValueType != "boolean" {
		problems = append(problems, location+".valueType must be one of \"number\", \"string\", or \"boolean\"")
	}
	if parameter.Unit != "" && strings.TrimSpace(parameter.Unit) == "" {
		problems = append(problems, location+".unit must not be blank")
	}
	problems = append(problems, validateIdentitySource(location, "parameter:"+parameter.OwnerKind, parameter.IdentitySource)...)
	problems = append(problems, validateStabilityClass(location, parameter.StabilityClass)...)
	problems = append(problems, validateAnnotationPresence(location+".annotations", parameter.Annotations)...)
	return problems
}

func validateMetadata(location string, entry Metadata) []string {
	var problems []string
	problems = append(problems, requireText(location+".id", entry.ID)...)
	if entry.OwnerKind != "component" && entry.OwnerKind != "feature" && entry.OwnerKind != "relationship" {
		problems = append(problems, location+".ownerKind must be one of \"component\", \"feature\", or \"relationship\"")
	}
	problems = append(problems, requireText(location+".ownerId", entry.OwnerID)...)
	problems = append(problems, requireText(location+".componentId", entry.ComponentID)...)
	problems = append(problems, requireText(location+".key", entry.Key)...)
	problems = append(problems, requireText(location+".cadType", entry.CADType)...)
	if entry.ValueType != "number" && entry.ValueType != "string" && entry.ValueType != "boolean" {
		problems = append(problems, location+".valueType must be one of \"number\", \"string\", or \"boolean\"")
	}
	problems = append(problems, validateIdentitySource(location, "metadata", entry.IdentitySource)...)
	problems = append(problems, validateStabilityClass(location, entry.StabilityClass)...)
	problems = append(problems, validateAnnotationPresence(location+".annotations", entry.Annotations)...)
	return problems
}

func validateOptionalSourceFile(location string, sourceFile *SourceFile) []string {
	if sourceFile == nil {
		return nil
	}
	return requireText(location+".sourceFile.path", sourceFile.Path)
}

func validateIdentitySource(location, kind string, identity *IdentitySource) []string {
	if identity == nil {
		return nil
	}
	var problems []string
	if strings.TrimSpace(identity.Kind) == "" {
		problems = append(problems, location+".identitySource.kind is required")
		return problems
	}
	switch kind {
	case "component":
		if identity.Kind != "parent_scoped_path" {
			problems = append(problems, location+".identitySource.kind must be \"parent_scoped_path\"")
		}
		if strings.TrimSpace(identity.Path) == "" {
			problems = append(problems, location+".identitySource.path is required")
		}
	case "feature", "parameterGroup", "metadata", "relationship":
		if identity.Kind != "owner_scoped_key" && kind != "relationship" {
			problems = append(problems, location+".identitySource.kind must be \"owner_scoped_key\"")
		}
		if kind != "relationship" && strings.TrimSpace(identity.OwnerScopedKey) == "" {
			problems = append(problems, location+".identitySource.ownerScopedKey is required")
		}
	case "parameter:group":
		if identity.Kind != "owner_scoped_key" {
			problems = append(problems, location+".identitySource.kind must be \"owner_scoped_key\"")
		}
		if strings.TrimSpace(identity.OwnerScopedKey) == "" {
			problems = append(problems, location+".identitySource.ownerScopedKey is required")
		}
		if strings.TrimSpace(identity.GroupScopedKey) == "" {
			problems = append(problems, location+".identitySource.groupScopedKey is required")
		}
	case "parameter:component", "parameter:feature":
		if identity.Kind != "owner_scoped_key" {
			problems = append(problems, location+".identitySource.kind must be \"owner_scoped_key\"")
		}
		if strings.TrimSpace(identity.OwnerScopedKey) == "" {
			problems = append(problems, location+".identitySource.ownerScopedKey is required")
		}
	}
	return problems
}

func validateStabilityClass(location, value string) []string {
	if value == "" {
		return nil
	}
	switch value {
	case "stable", "conditionally_stable", "unstable":
		return nil
	default:
		return []string{location + ".stabilityClass must be one of \"stable\", \"conditionally_stable\", or \"unstable\""}
	}
}

func validateTargetability(location string, targetability Targetability) []string {
	_ = location
	_ = targetability
	return nil
}

func validateAnnotationPresence(location string, annotations Annotations) []string {
	_ = location
	_ = annotations
	return nil
}

func validateScalarMatchesType(location, valueType string, value ScalarValue) []string {
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value.raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return []string{location + " must be valid JSON scalar"}
	}
	switch valueType {
	case "number":
		if _, ok := decoded.(json.Number); !ok {
			return []string{location + ` must match valueType "number"`}
		}
	case "string":
		if _, ok := decoded.(string); !ok {
			return []string{location + ` must match valueType "string"`}
		}
	case "boolean":
		if _, ok := decoded.(bool); !ok {
			return []string{location + ` must match valueType "boolean"`}
		}
	}
	return nil
}

func validateOwnerScopedIdentitySources(contract *CADContract) []string {
	var problems []string
	type ownerScoped struct {
		key      string
		location string
	}

	componentPaths := map[string]string{}
	for index, component := range contract.Entities.Components {
		if component.IdentitySource == nil || component.IdentitySource.Path == "" {
			continue
		}
		location := fmt.Sprintf("entities.components[%d]", index)
		if prev, ok := componentPaths[component.IdentitySource.Path]; ok {
			problems = append(problems, fmt.Sprintf("%s.identitySource.path %q duplicates parent-scoped identity source already declared at %s.identitySource.path", location, component.IdentitySource.Path, prev))
		} else {
			componentPaths[component.IdentitySource.Path] = location
		}
	}

	ownerScopedSeen := map[string]ownerScoped{}
	addOwnerScoped := func(scope, key, location, field string) {
		if scope == "" || key == "" {
			return
		}
		compound := scope + "\x00" + key
		if prev, ok := ownerScopedSeen[compound]; ok {
			problems = append(problems, fmt.Sprintf("%s.%s %q duplicates owner-scoped identity source already declared at %s.%s", location, field, key, prev.location, field))
			return
		}
		ownerScopedSeen[compound] = ownerScoped{key: key, location: location}
	}

	for index, feature := range contract.Entities.Features {
		if feature.IdentitySource == nil {
			continue
		}
		addOwnerScoped("feature:"+feature.ComponentID, feature.IdentitySource.OwnerScopedKey, fmt.Sprintf("entities.features[%d]", index), "identitySource.ownerScopedKey")
	}
	for index, group := range contract.Entities.ParameterGroups {
		if group.IdentitySource == nil {
			continue
		}
		addOwnerScoped("group:"+group.OwnerComponentID, group.IdentitySource.OwnerScopedKey, fmt.Sprintf("entities.parameterGroups[%d]", index), "identitySource.ownerScopedKey")
	}
	for index, parameter := range contract.Entities.Parameters {
		if parameter.IdentitySource == nil {
			continue
		}
		scope := parameter.OwnerKind + ":" + parameter.OwnerID
		addOwnerScoped(scope, parameter.IdentitySource.OwnerScopedKey, fmt.Sprintf("entities.parameters[%d]", index), "identitySource.ownerScopedKey")
	}
	for index, entry := range contract.Entities.Metadata {
		if entry.IdentitySource == nil {
			continue
		}
		scope := entry.OwnerKind + ":" + entry.OwnerID
		addOwnerScoped(scope, entry.IdentitySource.OwnerScopedKey, fmt.Sprintf("entities.metadata[%d]", index), "identitySource.ownerScopedKey")
	}

	return problems
}

func validateStructure(contract *CADContract, componentIDs map[string]int) []string {
	var problems []string
	nodeByComponent := map[string]int{}
	parentOf := map[string]string{}
	rootNodeCount := 0

	for index, node := range contract.Structure.Nodes {
		location := fmt.Sprintf("structure.nodes[%d]", index)
		if strings.TrimSpace(node.ComponentID) == "" {
			problems = append(problems, location+".componentId is required")
			continue
		}
		if prev, ok := nodeByComponent[node.ComponentID]; ok {
			problems = append(problems, fmt.Sprintf("%s.componentId %q duplicates node already declared at structure.nodes[%d].componentId", location, node.ComponentID, prev))
		} else {
			nodeByComponent[node.ComponentID] = index
		}
		if _, ok := componentIDs[node.ComponentID]; !ok {
			problems = append(problems, fmt.Sprintf("%s.componentId %q must resolve to an existing component", location, node.ComponentID))
		}
		if node.ParentComponentID == "" {
			rootNodeCount++
			if node.ComponentID != contract.Structure.RootComponentID && contract.Structure.RootComponentID != "" {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must equal structure.rootComponentId when parentComponentId is empty", location, node.ComponentID))
			}
		} else {
			if _, ok := componentIDs[node.ParentComponentID]; !ok {
				problems = append(problems, fmt.Sprintf("%s.parentComponentId %q must resolve to an existing component", location, node.ParentComponentID))
			}
			if node.ParentComponentID == node.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.parentComponentId must not reference the same component as componentId", location))
			}
			if prev, ok := parentOf[node.ComponentID]; ok && prev != node.ParentComponentID {
				problems = append(problems, fmt.Sprintf("%s.componentId %q must not declare more than one parent", location, node.ComponentID))
			} else {
				parentOf[node.ComponentID] = node.ParentComponentID
			}
		}
		childSeen := map[string]int{}
		for childIndex, childID := range node.Children {
			if _, ok := componentIDs[childID]; !ok && childID != "" {
				problems = append(problems, fmt.Sprintf("%s.children[%d] %q must resolve to an existing component", location, childIndex, childID))
			}
			if childID == node.ComponentID {
				problems = append(problems, fmt.Sprintf("%s.children[%d] must not reference the same component as componentId", location, childIndex))
			}
			if prev, ok := childSeen[childID]; ok {
				problems = append(problems, fmt.Sprintf("%s.children[%d] duplicates child %q already declared at %s.children[%d]", location, childIndex, childID, location, prev))
			} else {
				childSeen[childID] = childIndex
			}
		}
	}

	if len(contract.Structure.Nodes) > 0 && rootNodeCount != 1 {
		problems = append(problems, "structure.nodes must contain exactly one root node with empty parentComponentId")
	}

	for index, node := range contract.Structure.Nodes {
		for _, childID := range node.Children {
			if parent, ok := parentOf[childID]; ok && parent != node.ComponentID {
				problems = append(problems, fmt.Sprintf("structure.nodes[%d].children references child %q whose declared parent is %q", index, childID, parent))
			}
			if _, ok := nodeByComponent[childID]; !ok && childID != "" {
				problems = append(problems, fmt.Sprintf("structure.nodes[%d].children references child %q that has no node entry", index, childID))
			}
		}
	}

	for componentID := range componentIDs {
		if _, ok := nodeByComponent[componentID]; !ok {
			problems = append(problems, fmt.Sprintf("structure.nodes must include component %q", componentID))
		}
	}

	for componentID := range nodeByComponent {
		node := contract.Structure.Nodes[nodeByComponent[componentID]]
		if node.ParentComponentID == "" {
			continue
		}
		if _, ok := parentOf[componentID]; !ok {
			problems = append(problems, fmt.Sprintf("structure component %q must have exactly one parent", componentID))
		}
	}

	visited := map[string]bool{}
	stack := map[string]bool{}
	var dfs func(string) bool
	dfs = func(componentID string) bool {
		if stack[componentID] {
			return true
		}
		if visited[componentID] {
			return false
		}
		visited[componentID] = true
		stack[componentID] = true
		nodeIndex, ok := nodeByComponent[componentID]
		if ok {
			for _, childID := range contract.Structure.Nodes[nodeIndex].Children {
				if dfs(childID) {
					return true
				}
			}
		}
		delete(stack, componentID)
		return false
	}
	if contract.Structure.RootComponentID != "" && dfs(contract.Structure.RootComponentID) {
		problems = append(problems, "structure.nodes must not contain cycles")
	}

	return problems
}

func requireText(location, value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{location + " is required"}
	}
	return nil
}

func canonicalizeJSONScalar(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("must be valid JSON scalar: %w", err)
	}
	if err := ensureNoTrailingJSON(decoder); err != nil {
		return nil, fmt.Errorf("must be valid JSON scalar: %w", err)
	}
	switch typed := value.(type) {
	case nil:
		return nil, fmt.Errorf("must be a JSON scalar")
	case bool:
		if typed {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case string:
		raw, _ := json.Marshal(typed)
		return raw, nil
	case json.Number:
		return []byte(typed.String()), nil
	default:
		return nil, fmt.Errorf("must be a JSON scalar")
	}
}
