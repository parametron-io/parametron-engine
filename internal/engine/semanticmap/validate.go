package semanticmap

import (
	"errors"
	"fmt"
	"strings"

	"parametron/internal/engine/semantic"
)

var (
	supportedValueKinds = map[string]struct{}{
		"number":  {},
		"integer": {},
		"boolean": {},
		"string":  {},
	}
	supportedResolutionPrecedence = map[string]struct{}{
		"explicit_parameter_mapping":  {},
		"capture_native_type_mapping": {},
	}
	knownOutputManifestTypes = map[string]struct{}{
		"step": {},
		"csv":  {},
		"pdf":  {},
	}
	knownMutationCollections = map[string]struct{}{
		"assemblyMutations.parameters":  {},
		"assemblyMutations.properties":  {},
		"assemblyMutations.suppression": {},
		"assemblyMutations.visibility":  {},
		"assemblyMutations.deletion":    {},
		"partMutations.parameters":      {},
		"partMutations.properties":      {},
		"partMutations.suppression":     {},
		"partMutations.visibility":      {},
		"partMutations.deletion":        {},
	}
	knownSemanticTargets = map[string]struct{}{
		"assembly":  {},
		"part":      {},
		"feature":   {},
		"parameter": {},
		"metadata":  {},
		"drawing":   {},
	}
	supportedMetadataValueKinds = map[string]struct{}{
		"capture.nativeType": {},
	}
)

func Validate(contract *SemanticMap) error {
	problems := validate(contract)
	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

func validate(contract *SemanticMap) []string {
	if contract == nil {
		return []string{"semantic map must not be nil"}
	}

	var problems []string

	if err := ValidateSchemaVersion(contract.SchemaVersion); err != nil {
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			problems = append(problems, err.Error())
		} else {
			problems = append(problems, validationErr.Messages()...)
		}
	}
	problems = append(problems, validateRequiredID("mappingId", contract.MappingID)...)
	problems = append(problems, validateRequiredID("adapter", contract.Adapter)...)

	if contract.CADSystem == nil {
		problems = append(problems, "cadSystem is required")
	} else if strings.TrimSpace(contract.CADSystem.Name) == "" {
		problems = append(problems, "cadSystem.name is required")
	} else if contract.CADSystem.Name != strings.TrimSpace(contract.CADSystem.Name) {
		problems = append(problems, "cadSystem.name must not have leading or trailing whitespace")
	}
	if contract.CADSystem != nil && contract.CADSystem.MinVersion != "" && contract.CADSystem.MinVersion != strings.TrimSpace(contract.CADSystem.MinVersion) {
		problems = append(problems, "cadSystem.minVersion must not have leading or trailing whitespace")
	}

	if contract.SemanticTypes == nil {
		problems = append(problems, "semanticTypes is required")
	} else if contract.SemanticTypes.ParameterTypes == nil {
		problems = append(problems, "semanticTypes.parameterTypes is required")
	} else if len(contract.SemanticTypes.ParameterTypes) == 0 {
		problems = append(problems, "semanticTypes.parameterTypes must not be empty")
	} else {
		for _, key := range sortedKeys(contract.SemanticTypes.ParameterTypes) {
			entry := contract.SemanticTypes.ParameterTypes[key]
			if strings.TrimSpace(key) == "" {
				problems = append(problems, "semanticTypes.parameterTypes key must not be empty")
			} else if key != strings.TrimSpace(key) {
				problems = append(problems, fmt.Sprintf("semanticTypes.parameterTypes key %q must not have leading or trailing whitespace", key))
			}
			if strings.TrimSpace(entry.ValueKind) == "" {
				problems = append(problems, fmt.Sprintf("semanticTypes.parameterTypes[%q].valueKind is required", key))
			} else if _, ok := supportedValueKinds[entry.ValueKind]; !ok {
				problems = append(problems, fmt.Sprintf("semanticTypes.parameterTypes[%q].valueKind %q is not supported", key, entry.ValueKind))
			}
			if strings.TrimSpace(entry.DefaultUnit) == "" {
				problems = append(problems, fmt.Sprintf("semanticTypes.parameterTypes[%q].defaultUnit is required", key))
			}
			if strings.TrimSpace(entry.Coercion) == "" {
				problems = append(problems, fmt.Sprintf("semanticTypes.parameterTypes[%q].coercion is required", key))
			} else if !semantic.IsSupportedCoercionRule(entry.Coercion) {
				problems = append(problems, fmt.Sprintf("semanticTypes.parameterTypes[%q].coercion %q is not supported", key, entry.Coercion))
			}
		}
	}
	if contract.SemanticTypes != nil {
		problems = append(problems, validateResolutionPolicy(contract.SemanticTypes.ResolutionPolicy)...)
	}

	problems = append(problems, validateOverridePolicy(contract.OverridePolicy)...)

	if contract.CaptureToSemantic == nil {
		problems = append(problems, "captureToSemantic is required")
	} else {
		if contract.CaptureToSemantic.ParameterGroups == nil {
			problems = append(problems, "captureToSemantic.parameterGroups is required")
		}
		if contract.CaptureToSemantic.Parameters == nil {
			problems = append(problems, "captureToSemantic.parameters is required")
		}
		if contract.CaptureToSemantic.Components == nil {
			problems = append(problems, "captureToSemantic.components is required")
		}
		if contract.CaptureToSemantic.Metadata == nil {
			problems = append(problems, "captureToSemantic.metadata is required")
		}
	}

	seenParameterGroups := map[string]int{}
	if contract.CaptureToSemantic != nil {
		for index, entry := range contract.CaptureToSemantic.ParameterGroups {
			location := fmt.Sprintf("captureToSemantic.parameterGroups[%d]", index)
			problems = append(problems, validateRequiredTrimmedField(location+".captureKind", entry.CaptureKind)...)
			problems = append(problems, validateRequiredTrimmedField(location+".cadContainerType", entry.CADContainerType)...)
			problems = append(problems, validateRequiredTrimmedField(location+".semanticKind", entry.SemanticKind)...)
			key := entry.CaptureKind + "\x00" + entry.CADContainerType + "\x00" + entry.SemanticKind
			if prev, ok := seenParameterGroups[key]; ok {
				problems = append(problems, fmt.Sprintf("%s duplicates natural key of captureToSemantic.parameterGroups[%d]", location, prev))
			} else {
				seenParameterGroups[key] = index
			}
		}
	}

	seenParameters := map[string]int{}
	if contract.CaptureToSemantic != nil {
		for index, entry := range contract.CaptureToSemantic.Parameters {
			location := fmt.Sprintf("captureToSemantic.parameters[%d]", index)
			problems = append(problems, validateRequiredTrimmedField(location+".captureKind", entry.CaptureKind)...)
			problems = append(problems, validateRequiredTrimmedField(location+".semanticKind", entry.SemanticKind)...)
			if entry.Identity != "capture.id" {
				problems = append(problems, location+`.identity must equal "capture.id"`)
			}
			if entry.Name != "capture.name" {
				problems = append(problems, location+`.name must equal "capture.name"`)
			}
			if entry.Group != "capture.groupId" {
				problems = append(problems, location+`.group must equal "capture.groupId"`)
			}
			if entry.SemanticType.From != "capture.nativeType" {
				problems = append(problems, location+`.semanticType.from must equal "capture.nativeType"`)
			}
			if entry.SemanticType.Map == nil || len(entry.SemanticType.Map) == 0 {
				problems = append(problems, location+".semanticType.map must not be empty")
			} else {
				for _, nativeKey := range sortedKeys(entry.SemanticType.Map) {
					semanticTypeName := entry.SemanticType.Map[nativeKey]
					if strings.TrimSpace(nativeKey) == "" {
						problems = append(problems, location+".semanticType.map key must not be empty")
					} else if nativeKey != strings.TrimSpace(nativeKey) {
						problems = append(problems, fmt.Sprintf("%s.semanticType.map key %q must not have leading or trailing whitespace", location, nativeKey))
					}
					if strings.TrimSpace(semanticTypeName) == "" {
						problems = append(problems, fmt.Sprintf("%s.semanticType.map[%q] must reference a non-empty semanticTypes.parameterTypes key", location, nativeKey))
						continue
					}
					if semanticTypeName != strings.TrimSpace(semanticTypeName) {
						problems = append(problems, fmt.Sprintf("%s.semanticType.map[%q] must not have leading or trailing whitespace", location, nativeKey))
						continue
					}
					if contract.SemanticTypes == nil || contract.SemanticTypes.ParameterTypes == nil {
						continue
					}
					if _, ok := contract.SemanticTypes.ParameterTypes[semanticTypeName]; !ok {
						problems = append(problems, fmt.Sprintf("%s.semanticType.map[%q] references unknown semanticTypes.parameterTypes key %q", location, nativeKey, semanticTypeName))
					}
				}
			}
			key := entry.CaptureKind + "\x00" + entry.SemanticKind
			if prev, ok := seenParameters[key]; ok {
				problems = append(problems, fmt.Sprintf("%s duplicates natural key of captureToSemantic.parameters[%d]", location, prev))
			} else {
				seenParameters[key] = index
			}
		}
	}

	seenComponents := map[string]int{}
	if contract.CaptureToSemantic != nil {
		for index, entry := range contract.CaptureToSemantic.Components {
			location := fmt.Sprintf("captureToSemantic.components[%d]", index)
			problems = append(problems, validateRequiredTrimmedField(location+".captureKind", entry.CaptureKind)...)
			if entry.Identity != "capture.id" {
				problems = append(problems, location+`.identity must equal "capture.id"`)
			}
			if entry.Targetability != "capture.targetability" {
				problems = append(problems, location+`.targetability must equal "capture.targetability"`)
			}
			if entry.SemanticKinds == nil || len(entry.SemanticKinds) == 0 {
				problems = append(problems, location+".semanticKinds must not be empty")
			}
			for _, nativeKind := range sortedKeys(entry.SemanticKinds) {
				semanticTarget := entry.SemanticKinds[nativeKind]
				if strings.TrimSpace(nativeKind) == "" {
					problems = append(problems, location+".semanticKinds key must not be empty")
				} else if nativeKind != strings.TrimSpace(nativeKind) {
					problems = append(problems, fmt.Sprintf("%s.semanticKinds key %q must not have leading or trailing whitespace", location, nativeKind))
				}
				if strings.TrimSpace(semanticTarget) == "" {
					problems = append(problems, fmt.Sprintf("%s.semanticKinds[%q] is required", location, nativeKind))
				} else if semanticTarget != strings.TrimSpace(semanticTarget) {
					problems = append(problems, fmt.Sprintf("%s.semanticKinds[%q] must not have leading or trailing whitespace", location, nativeKind))
				} else if _, ok := knownSemanticTargets[semanticTarget]; !ok {
					problems = append(problems, fmt.Sprintf("%s.semanticKinds[%q] %q is not supported", location, nativeKind, semanticTarget))
				}
			}
			key := entry.CaptureKind
			if prev, ok := seenComponents[key]; ok {
				problems = append(problems, fmt.Sprintf("%s duplicates natural key of captureToSemantic.components[%d]", location, prev))
			} else {
				seenComponents[key] = index
			}
		}
	}

	seenMetadata := map[string]int{}
	if contract.CaptureToSemantic != nil {
		for index, entry := range contract.CaptureToSemantic.Metadata {
			location := fmt.Sprintf("captureToSemantic.metadata[%d]", index)
			problems = append(problems, validateRequiredTrimmedField(location+".captureKind", entry.CaptureKind)...)
			problems = append(problems, validateRequiredTrimmedField(location+".semanticKind", entry.SemanticKind)...)
			if entry.Identity != "capture.id" {
				problems = append(problems, location+`.identity must equal "capture.id"`)
			}
			if entry.Key != "capture.key" {
				problems = append(problems, location+`.key must equal "capture.key"`)
			}
			if strings.TrimSpace(entry.ValueKind) == "" {
				problems = append(problems, location+".valueKind is required")
			} else if entry.ValueKind != strings.TrimSpace(entry.ValueKind) {
				problems = append(problems, location+".valueKind must not have leading or trailing whitespace")
			} else if _, ok := supportedMetadataValueKinds[entry.ValueKind]; !ok {
				problems = append(problems, fmt.Sprintf("%s.valueKind %q is not supported", location, entry.ValueKind))
			}
			key := entry.CaptureKind + "\x00" + entry.SemanticKind
			if prev, ok := seenMetadata[key]; ok {
				problems = append(problems, fmt.Sprintf("%s duplicates natural key of captureToSemantic.metadata[%d]", location, prev))
			} else {
				seenMetadata[key] = index
			}
		}
	}

	problems = append(problems, validateSemanticToManifest(contract.SemanticToManifest)...)

	if contract.OperationCapabilities == nil {
		problems = append(problems, "operationCapabilities is required")
	} else {
		for _, key := range sortedKeys(contract.OperationCapabilities) {
			entry := contract.OperationCapabilities[key]
			if strings.TrimSpace(key) == "" {
				problems = append(problems, "operationCapabilities key must not be empty")
			} else if key != strings.TrimSpace(key) {
				problems = append(problems, fmt.Sprintf("operationCapabilities key %q must not have leading or trailing whitespace", key))
			} else if _, ok := supportedOperationNames[key]; !ok {
				problems = append(problems, fmt.Sprintf("operationCapabilities[%q] is not supported", key))
			}
			if entry.RequiresTargetability != "" && entry.RequiresTargetability != strings.TrimSpace(entry.RequiresTargetability) {
				problems = append(problems, fmt.Sprintf("operationCapabilities[%q].requiresTargetability must not have leading or trailing whitespace", key))
			}
			if len(entry.AllowedSemanticTargets) == 0 {
				problems = append(problems, fmt.Sprintf("operationCapabilities[%q].allowedSemanticTargets must not be empty", key))
				continue
			}
			seenTargets := map[string]int{}
			for index, target := range entry.AllowedSemanticTargets {
				if strings.TrimSpace(target) == "" {
					problems = append(problems, fmt.Sprintf("operationCapabilities[%q].allowedSemanticTargets[%d] is required", key, index))
					continue
				}
				if target != strings.TrimSpace(target) {
					problems = append(problems, fmt.Sprintf("operationCapabilities[%q].allowedSemanticTargets[%d] must not have leading or trailing whitespace", key, index))
					continue
				}
				if _, ok := knownSemanticTargets[target]; !ok {
					problems = append(problems, fmt.Sprintf("operationCapabilities[%q].allowedSemanticTargets[%d] %q is not supported", key, index, target))
					continue
				}
				if prev, ok := seenTargets[target]; ok {
					problems = append(problems, fmt.Sprintf("operationCapabilities[%q].allowedSemanticTargets[%d] duplicates operationCapabilities[%q].allowedSemanticTargets[%d]", key, index, key, prev))
					continue
				}
				seenTargets[target] = index
			}
		}
	}

	problems = append(problems, validateDeterminismPolicy(contract)...)

	return problems
}

func validateSemanticToManifest(mapping *SemanticToManifest) []string {
	if mapping == nil {
		return []string{"semanticToManifest is required"}
	}

	var problems []string

	problems = append(problems, validateParameterManifestMapping(mapping.Parameters)...)

	if mapping.Outputs == nil {
		problems = append(problems, "semanticToManifest.outputs is required")
	} else {
		for _, key := range sortedKeys(mapping.Outputs) {
			entry := mapping.Outputs[key]
			location := fmt.Sprintf("semanticToManifest.outputs[%q]", key)
			if strings.TrimSpace(key) == "" {
				problems = append(problems, "semanticToManifest.outputs key must not be empty")
			} else if key != strings.TrimSpace(key) {
				problems = append(problems, fmt.Sprintf("semanticToManifest.outputs key %q must not have leading or trailing whitespace", key))
			}
			if strings.TrimSpace(entry.ManifestType) == "" {
				problems = append(problems, location+".manifestType is required")
			} else if entry.ManifestType != strings.TrimSpace(entry.ManifestType) {
				problems = append(problems, location+".manifestType must not have leading or trailing whitespace")
			} else if _, ok := knownOutputManifestTypes[entry.ManifestType]; !ok {
				problems = append(problems, fmt.Sprintf("%s.manifestType %q is not supported", location, entry.ManifestType))
			}

			switch entry.ManifestType {
			case "step":
				problems = append(problems, validateExactTrimmedField(location+".targetField", entry.TargetField, "object")...)
				problems = append(problems, validateExactTrimmedField(location+".targetSource", entry.TargetSource, "semantic.output.target.manifestName")...)
				problems = append(problems, validateForbiddenField(location+".scopeField", entry.ScopeField)...)
				problems = append(problems, validateForbiddenMapField(location+".scopeMap", entry.ScopeMap)...)
			case "csv":
				problems = append(problems, validateExactTrimmedField(location+".targetField", entry.TargetField, "target")...)
				problems = append(problems, validateExactTrimmedField(location+".targetSource", entry.TargetSource, "semantic.output.target.manifestName")...)
				problems = append(problems, validateExactTrimmedField(location+".scopeField", entry.ScopeField, "rollup")...)
				if entry.ScopeMap == nil {
					problems = append(problems, location+".scopeMap is required")
				} else if len(entry.ScopeMap) == 0 {
					problems = append(problems, location+".scopeMap must not be empty")
				} else {
					for _, scopeKey := range sortedKeys(entry.ScopeMap) {
						scopeValue := entry.ScopeMap[scopeKey]
						if strings.TrimSpace(scopeKey) == "" {
							problems = append(problems, location+".scopeMap key must not be empty")
						} else if scopeKey != strings.TrimSpace(scopeKey) {
							problems = append(problems, fmt.Sprintf("%s.scopeMap key %q must not have leading or trailing whitespace", location, scopeKey))
						}
						if strings.TrimSpace(scopeValue) == "" {
							problems = append(problems, fmt.Sprintf("%s.scopeMap[%q] is required", location, scopeKey))
						} else if scopeValue != strings.TrimSpace(scopeValue) {
							problems = append(problems, fmt.Sprintf("%s.scopeMap[%q] must not have leading or trailing whitespace", location, scopeKey))
						}
					}
				}
			case "pdf":
				problems = append(problems, validateForbiddenField(location+".targetField", entry.TargetField)...)
				problems = append(problems, validateForbiddenField(location+".targetSource", entry.TargetSource)...)
				problems = append(problems, validateForbiddenField(location+".scopeField", entry.ScopeField)...)
				problems = append(problems, validateForbiddenMapField(location+".scopeMap", entry.ScopeMap)...)
			}
		}
	}

	if mapping.Mutations == nil {
		problems = append(problems, "semanticToManifest.mutations is required")
	} else {
		for _, key := range sortedKeys(mapping.Mutations) {
			entry := mapping.Mutations[key]
			location := fmt.Sprintf("semanticToManifest.mutations[%q]", key)
			if strings.TrimSpace(key) == "" {
				problems = append(problems, "semanticToManifest.mutations key must not be empty")
			} else if key != strings.TrimSpace(key) {
				problems = append(problems, fmt.Sprintf("semanticToManifest.mutations key %q must not have leading or trailing whitespace", key))
			}
			if strings.TrimSpace(entry.ManifestCollection) == "" {
				problems = append(problems, location+".manifestCollection is required")
			} else if entry.ManifestCollection != strings.TrimSpace(entry.ManifestCollection) {
				problems = append(problems, location+".manifestCollection must not have leading or trailing whitespace")
			} else if _, ok := knownMutationCollections[entry.ManifestCollection]; !ok {
				problems = append(problems, fmt.Sprintf("%s.manifestCollection %q is not supported", location, entry.ManifestCollection))
				continue
			}

			switch {
			case strings.HasSuffix(entry.ManifestCollection, ".suppression"):
				problems = append(problems, validateExactTrimmedField(location+".targetField", entry.TargetField, "object")...)
				problems = append(problems, validateExactTrimmedField(location+".valueField", entry.ValueField, "suppressed")...)
				if entry.Value == nil {
					problems = append(problems, location+".value is required")
				}
				problems = append(problems, validateForbiddenField(location+".propertyField", entry.PropertyField)...)
			case strings.HasSuffix(entry.ManifestCollection, ".visibility"):
				problems = append(problems, validateExactTrimmedField(location+".targetField", entry.TargetField, "object")...)
				problems = append(problems, validateExactTrimmedField(location+".valueField", entry.ValueField, "visible")...)
				if entry.Value == nil {
					problems = append(problems, location+".value is required")
				}
				problems = append(problems, validateForbiddenField(location+".propertyField", entry.PropertyField)...)
			case strings.HasSuffix(entry.ManifestCollection, ".deletion"):
				problems = append(problems, validateExactTrimmedField(location+".targetField", entry.TargetField, "object")...)
				problems = append(problems, validateForbiddenField(location+".valueField", entry.ValueField)...)
				if entry.Value != nil {
					problems = append(problems, location+".value must not be set")
				}
				problems = append(problems, validateForbiddenField(location+".propertyField", entry.PropertyField)...)
			case strings.HasSuffix(entry.ManifestCollection, ".properties"):
				problems = append(problems, validateExactTrimmedField(location+".targetField", entry.TargetField, "object")...)
				problems = append(problems, validateExactTrimmedField(location+".propertyField", entry.PropertyField, "property")...)
				problems = append(problems, validateExactTrimmedField(location+".valueField", entry.ValueField, "value")...)
				if entry.Value != nil {
					problems = append(problems, location+".value must not be set")
				}
			case strings.HasSuffix(entry.ManifestCollection, ".parameters"):
				problems = append(problems, validateExactTrimmedField(location+".targetField", entry.TargetField, "object")...)
				problems = append(problems, validateExactTrimmedField(location+".propertyField", entry.PropertyField, "property")...)
				problems = append(problems, validateExactTrimmedField(location+".valueField", entry.ValueField, "valueParam")...)
				if entry.Value != nil {
					problems = append(problems, location+".value must not be set")
				}
			}
		}
	}

	return problems
}

func validateParameterManifestMapping(mapping *ParameterManifestMapping) []string {
	if mapping == nil {
		return []string{"semanticToManifest.parameters is required"}
	}

	var problems []string

	problems = append(problems, validateExactTrimmedField("semanticToManifest.parameters.target", mapping.Target, "parameterAssignments")...)
	problems = append(problems, validateExactTrimmedField("semanticToManifest.parameters.name", mapping.Name, "semantic.parameter.name")...)
	problems = append(problems, validateExactTrimmedField("semanticToManifest.parameters.value", mapping.Value, "resolved.value")...)
	problems = append(problems, validateExactTrimmedField("semanticToManifest.parameters.type", mapping.Type, "number")...)
	problems = append(problems, validateExactTrimmedField("semanticToManifest.parameters.unit", mapping.Unit, "resolved.unit")...)

	return problems
}

func validateOverridePolicy(policy *OverridePolicy) []string {
	if policy == nil {
		return []string{"overridePolicy is required"}
	}

	var problems []string
	problems = append(problems, validateRequiredFalseBool("overridePolicy.allowAdapterOverrides", policy.AllowAdapterOverrides)...)
	problems = append(problems, validateRequiredFalseBool("overridePolicy.allowProjectOverrides", policy.AllowProjectOverrides)...)
	problems = append(problems, validateRequiredFalseBool("overridePolicy.allowUserOverrides", policy.AllowUserOverrides)...)
	return problems
}

func validateDeterminismPolicy(contract *SemanticMap) []string {
	if contract == nil {
		return nil
	}
	return validateDeterminism(contract.Determinism)
}

func enforceDeterminismPolicy(contract *SemanticMap) error {
	problems := validateDeterminismPolicy(contract)
	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

func validateDeterminism(policy *Determinism) []string {
	if policy == nil {
		return []string{"determinism is required"}
	}

	var problems []string
	problems = append(problems, validateRequiredFalseBool("determinism.allowImplicitFallback", policy.AllowImplicitFallback)...)
	problems = append(problems, validateRequiredFalseBool("determinism.allowCaseInsensitiveMatch", policy.AllowCaseInsensitiveMatch)...)
	problems = append(problems, validateRequiredFalseBool("determinism.allowDisplayNameIdentity", policy.AllowDisplayNameIdentity)...)
	problems = append(problems, validateRequiredFalseBool("determinism.allowAdapterInference", policy.AllowAdapterInference)...)
	return problems
}

func validateResolutionPolicy(policy *ResolutionPolicy) []string {
	if policy == nil {
		return []string{"semanticTypes.resolutionPolicy is required"}
	}

	var problems []string

	if policy.Precedence == nil {
		problems = append(problems, "semanticTypes.resolutionPolicy.precedence is required")
	} else if len(policy.Precedence) == 0 {
		problems = append(problems, "semanticTypes.resolutionPolicy.precedence must not be empty")
	} else {
		seen := map[string]int{}
		for index, value := range policy.Precedence {
			if _, ok := supportedResolutionPrecedence[value]; !ok {
				problems = append(problems, fmt.Sprintf("semanticTypes.resolutionPolicy.precedence[%d] %q is not supported", index, value))
				continue
			}
			if prev, ok := seen[value]; ok {
				problems = append(problems, fmt.Sprintf("semanticTypes.resolutionPolicy.precedence[%d] duplicates semanticTypes.resolutionPolicy.precedence[%d]", index, prev))
				continue
			}
			seen[value] = index
		}
	}

	if policy.AllowEngineDefault == nil {
		problems = append(problems, "semanticTypes.resolutionPolicy.allowEngineDefault is required")
	} else if *policy.AllowEngineDefault {
		problems = append(problems, "semanticTypes.resolutionPolicy.allowEngineDefault must be false")
	}

	if policy.AllowEngineInference == nil {
		problems = append(problems, "semanticTypes.resolutionPolicy.allowEngineInference is required")
	} else if *policy.AllowEngineInference {
		problems = append(problems, "semanticTypes.resolutionPolicy.allowEngineInference must be false")
	}

	return problems
}

func validateRequiredID(field, value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{field + " is required"}
	}
	if value != strings.TrimSpace(value) {
		return []string{field + " must not have leading or trailing whitespace"}
	}
	return nil
}

func validateRequiredTrimmedField(field, value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{field + " is required"}
	}
	if value != strings.TrimSpace(value) {
		return []string{field + " must not have leading or trailing whitespace"}
	}
	return nil
}

func validateExactTrimmedField(field string, value string, expected string) []string {
	if problems := validateRequiredTrimmedField(field, value); len(problems) > 0 {
		return problems
	}
	if value != expected {
		return []string{fmt.Sprintf("%s must equal %q", field, expected)}
	}
	return nil
}

func validateForbiddenField(field string, value string) []string {
	if value == "" {
		return nil
	}
	return []string{field + " must not be set"}
}

func validateForbiddenMapField(field string, value map[string]string) []string {
	if len(value) == 0 {
		return nil
	}
	return []string{field + " must not be set"}
}

func validateRequiredFalseBool(field string, value *bool) []string {
	if value == nil {
		return []string{field + " is required"}
	}
	if *value {
		return []string{field + " must be false"}
	}
	return nil
}

func validateOperationCapabilityForTarget(contract *SemanticMap, operation string, semanticTargetKind string, targetability map[string]bool) error {
	var problems []string

	if contract == nil {
		problems = append(problems, "semantic map must not be nil")
	} else {
		capability, ok := contract.OperationCapabilities[operation]
		if !ok {
			problems = append(problems, fmt.Sprintf("operationCapabilities[%q] is not declared", operation))
		} else {
			allowed := false
			for _, target := range capability.AllowedSemanticTargets {
				if target == semanticTargetKind {
					allowed = true
					break
				}
			}
			if !allowed {
				problems = append(problems, fmt.Sprintf("operationCapabilities[%q] does not allow semantic target %q", operation, semanticTargetKind))
			}
			if capability.RequiresTargetability != "" {
				if targetability == nil {
					problems = append(problems, fmt.Sprintf("operationCapabilities[%q] requires targetability %q", operation, capability.RequiresTargetability))
				} else if !targetability[capability.RequiresTargetability] {
					problems = append(problems, fmt.Sprintf("operationCapabilities[%q] requires targetability %q to be true", operation, capability.RequiresTargetability))
				}
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}
