package semanticmap

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"parametron/internal/engine/semantic"
)

const (
	MetadataValueKindString  = "string"
	MetadataValueKindBoolean = "boolean"
	MetadataValueKindNumber  = "number"
	MetadataValueKindInteger = "integer"
)

const (
	metadataResolutionCodeInvalidRequest        = "metadata_resolution_invalid_request"
	metadataResolutionCodeInvalidSemanticMap    = "metadata_resolution_invalid_semantic_map"
	metadataResolutionCodeTargetNotFound        = "metadata_resolution_target_not_found"
	metadataResolutionCodeTargetAmbiguous       = "metadata_resolution_target_ambiguous"
	metadataResolutionCodeTargetResolution      = "metadata_resolution_target_resolution"
	metadataResolutionCodeReferenceNotFound     = "metadata_resolution_reference_not_found"
	metadataResolutionCodeReferenceAmbiguous    = "metadata_resolution_reference_ambiguous"
	metadataResolutionCodeNotWritable           = "metadata_resolution_not_writable"
	metadataResolutionCodeNotObservable         = "metadata_resolution_not_observable"
	metadataResolutionCodeUnsupportedValueType  = "metadata_resolution_unsupported_value_type"
	metadataResolutionCodeNativeOwnerInvalid    = "metadata_resolution_native_owner_invalid"
	metadataResolutionCodeNativePropertyInvalid = "metadata_resolution_native_property_invalid"
	metadataResolutionCodeValueKindMissing      = "metadata_resolution_value_kind_missing"
	metadataResolutionCodeValueKindUnsupported  = "metadata_resolution_value_kind_unsupported"
	metadataResolutionCodeValueTypeMismatch     = "metadata_resolution_value_type_mismatch"
)

// TargetMetadataResolutionRequest is the Engine-owned input for resolving one
// target-scoped metadata declaration to its exact native owner, property, and
// value kind.
type TargetMetadataResolutionRequest struct {
	Model           *semantic.Model
	IdentityLinkage *IdentityLinkage
	Contract        *SemanticMap
	TargetName      string
	MetadataKey     string
}

// ResolvedTargetMetadata is the semantic-to-native metadata resolution result.
// It retains the semantic target and captured metadata for provenance while
// carrying only concrete native information for runtime projection.
type ResolvedTargetMetadata struct {
	Target         semantic.ResolvedSemanticTarget
	Metadata       semantic.Metadata
	NativeObject   string
	NativeProperty string
	ValueKind      string
}

// MetadataResolutionError is a deterministic target-scoped metadata resolution
// failure. Code classifies the failure without changing the underlying cause.
type MetadataResolutionError struct {
	Code        string
	Message     string
	TargetName  string
	MetadataKey string
	MetadataID  string
	Err         error
}

func (e *MetadataResolutionError) Error() string {
	if e == nil {
		return "semantic map metadata resolution error"
	}
	message := e.Message
	if e.Err != nil && !strings.Contains(message, e.Err.Error()) {
		if message != "" {
			message += ": "
		}
		message += e.Err.Error()
	}
	if strings.TrimSpace(message) == "" {
		return "semantic map metadata resolution error"
	}
	if e.Code == "" {
		return "semantic map metadata resolution error: " + message
	}
	return fmt.Sprintf("semantic map metadata resolution error [%s]: %s", e.Code, message)
}

func (e *MetadataResolutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ResolveTargetMetadata resolves an exact semantic target, then an exact
// captured metadata key owned by that target. It validates writability,
// observability, captured value type, native owner identity, native property
// identity, and the captured-native value kind before returning. Metadata
// owned by relationships is excluded because the semantic target resolver
// addresses only Features and Components.
func ResolveTargetMetadata(request *TargetMetadataResolutionRequest) (ResolvedTargetMetadata, error) {
	if request == nil {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(metadataResolutionCodeInvalidRequest, "", "", "", "request must not be nil", nil)
	}
	targetName := request.TargetName
	metadataKey := request.MetadataKey
	if request.Model == nil {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(metadataResolutionCodeInvalidRequest, targetName, metadataKey, "", "semantic model must not be nil", nil)
	}
	if request.Contract == nil {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(metadataResolutionCodeInvalidRequest, targetName, metadataKey, "", "semantic map must not be nil", nil)
	}
	if strings.TrimSpace(targetName) == "" {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(metadataResolutionCodeInvalidRequest, targetName, metadataKey, "", "target name must not be empty", nil)
	}
	if strings.TrimSpace(metadataKey) == "" {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(metadataResolutionCodeInvalidRequest, targetName, metadataKey, "", "metadata key must not be empty", nil)
	}
	if err := Validate(request.Contract); err != nil {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(metadataResolutionCodeInvalidSemanticMap, targetName, metadataKey, "", "semantic map is invalid", err)
	}

	target, err := semantic.ResolveSemanticTargetByExactName(request.Model, targetName)
	if err != nil {
		return ResolvedTargetMetadata{}, newTargetResolutionError(err, targetName, metadataKey)
	}

	matches := targetMetadataMatches(request.Model, target, metadataKey)
	switch len(matches) {
	case 0:
		return ResolvedTargetMetadata{}, newMetadataResolutionError(
			metadataResolutionCodeReferenceNotFound,
			targetName,
			metadataKey,
			"",
			fmt.Sprintf("metadata key %q does not resolve to captured metadata owned by semantic target %q", metadataKey, targetName),
			nil,
		)
	case 1:
	default:
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		slices.Sort(ids)
		return ResolvedTargetMetadata{}, newMetadataResolutionError(
			metadataResolutionCodeReferenceAmbiguous,
			targetName,
			metadataKey,
			"",
			fmt.Sprintf("metadata key %q is ambiguous across captured metadata %v for semantic target %q", metadataKey, ids, targetName),
			nil,
		)
	}

	metadata := matches[0]
	if !metadata.Writable {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(
			metadataResolutionCodeNotWritable,
			targetName,
			metadataKey,
			metadata.ID,
			fmt.Sprintf("captured metadata %q is not writable", metadata.ID),
			nil,
		)
	}
	if !metadata.Observable {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(
			metadataResolutionCodeNotObservable,
			targetName,
			metadataKey,
			metadata.ID,
			fmt.Sprintf("captured metadata %q is not observable", metadata.ID),
			nil,
		)
	}
	switch metadata.ValueType {
	case "number", "string", "boolean":
	default:
		return ResolvedTargetMetadata{}, newMetadataResolutionError(
			metadataResolutionCodeUnsupportedValueType,
			targetName,
			metadataKey,
			metadata.ID,
			fmt.Sprintf("captured metadata %q valueType %q is not supported", metadata.ID, metadata.ValueType),
			nil,
		)
	}

	nativeObject, err := ResolveTargetNativeObject(request.Model, request.IdentityLinkage, target.TargetEntityKind, target.SemanticID)
	if err != nil {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(
			metadataResolutionCodeNativeOwnerInvalid,
			targetName,
			metadataKey,
			metadata.ID,
			fmt.Sprintf("cannot resolve native owner for semantic target %q: %v", targetName, err),
			err,
		)
	}

	if err := validateNativeMetadataProperty(metadata.Key); err != nil {
		return ResolvedTargetMetadata{}, newMetadataResolutionError(
			metadataResolutionCodeNativePropertyInvalid,
			targetName,
			metadataKey,
			metadata.ID,
			fmt.Sprintf("cannot resolve native property for captured metadata %q: %v", metadata.ID, err),
			err,
		)
	}

	valueKind, resolutionErr := resolveMetadataValueKind(request.Contract, metadata, targetName, metadataKey)
	if resolutionErr != nil {
		return ResolvedTargetMetadata{}, resolutionErr
	}

	return ResolvedTargetMetadata{
		Target:         target,
		Metadata:       metadata,
		NativeObject:   nativeObject,
		NativeProperty: metadata.Key,
		ValueKind:      valueKind,
	}, nil
}

// ValidateResolvedMetadataValue validates an already-resolved DSL value
// against a resolved metadata value kind. The planner supplies string, bool,
// or float64 values; integer requires an exact integral finite number.
func ValidateResolvedMetadataValue(valueKind string, value any) error {
	switch valueKind {
	case MetadataValueKindString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("metadata value kind %q requires a string, got %T", valueKind, value)
		}
	case MetadataValueKindBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("metadata value kind %q requires a boolean, got %T", valueKind, value)
		}
	case MetadataValueKindNumber, MetadataValueKindInteger:
		number, ok := value.(float64)
		if !ok {
			return fmt.Errorf("metadata value kind %q requires a number, got %T", valueKind, value)
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("metadata value kind %q requires a finite number, got %v", valueKind, number)
		}
		if valueKind == MetadataValueKindInteger && math.Trunc(number) != number {
			return fmt.Errorf("metadata value kind %q requires an exact integral number, got %v", valueKind, number)
		}
	default:
		return fmt.Errorf("unsupported metadata value kind %q", valueKind)
	}
	return nil
}

// IsSupportedMetadataValueKind reports whether kind is one of the resolved
// metadata value kinds supported by this phase.
func IsSupportedMetadataValueKind(kind string) bool {
	_, ok := supportedResolvedMetadataValueKinds[kind]
	return ok
}

func newTargetResolutionError(err error, targetName, metadataKey string) *MetadataResolutionError {
	code := metadataResolutionCodeTargetResolution
	var typed *semantic.TargetResolutionError
	if errors.As(err, &typed) {
		if typed.Ambiguous {
			code = metadataResolutionCodeTargetAmbiguous
		} else {
			code = metadataResolutionCodeTargetNotFound
		}
	}
	return newMetadataResolutionError(code, targetName, metadataKey, "", err.Error(), err)
}

func targetMetadataMatches(model *semantic.Model, target semantic.ResolvedSemanticTarget, key string) []semantic.Metadata {
	matches := make([]semantic.Metadata, 0, 1)
	for _, entry := range model.Metadata {
		if entry.Key != key {
			continue
		}
		if entry.OwnerKind != target.SemanticEntityKind || entry.OwnerID != target.SemanticID {
			continue
		}
		matches = append(matches, entry)
	}
	slices.SortFunc(matches, func(a, b semantic.Metadata) int {
		return compareStrings(a.ID, b.ID)
	})
	return matches
}

func validateNativeMetadataProperty(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("must be non-empty")
	}
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("must not have leading or trailing whitespace")
	}
	if strings.ContainsRune(key, '\x00') {
		return fmt.Errorf("must not contain NUL")
	}
	return nil
}

func resolveMetadataValueKind(contract *SemanticMap, metadata semantic.Metadata, targetName, metadataKey string) (string, *MetadataResolutionError) {
	mapping, ok := metadataCaptureMapping(contract)
	if !ok {
		return "", newMetadataResolutionError(
			metadataResolutionCodeValueKindMissing,
			targetName,
			metadataKey,
			metadata.ID,
			"semantic map has no captureToSemantic.metadata mapping",
			nil,
		)
	}
	valueKind, ok := mapping.NativeValueKinds[metadata.NativeType]
	if !ok {
		return "", newMetadataResolutionError(
			metadataResolutionCodeValueKindMissing,
			targetName,
			metadataKey,
			metadata.ID,
			fmt.Sprintf("native type %q for captured metadata %q is not mapped to a metadata value kind", metadata.NativeType, metadata.ID),
			nil,
		)
	}
	if !IsSupportedMetadataValueKind(valueKind) {
		return "", newMetadataResolutionError(
			metadataResolutionCodeValueKindUnsupported,
			targetName,
			metadataKey,
			metadata.ID,
			fmt.Sprintf("native type %q for captured metadata %q maps to unsupported value kind %q", metadata.NativeType, metadata.ID, valueKind),
			nil,
		)
	}
	if err := validateMetadataValueTypeCompatibility(metadata.ValueType, valueKind); err != nil {
		return "", newMetadataResolutionError(
			metadataResolutionCodeValueTypeMismatch,
			targetName,
			metadataKey,
			metadata.ID,
			fmt.Sprintf("captured metadata %q valueType %q is incompatible with native value kind %q", metadata.ID, metadata.ValueType, valueKind),
			err,
		)
	}
	return valueKind, nil
}

func metadataCaptureMapping(contract *SemanticMap) (MetadataMapping, bool) {
	for _, mapping := range contract.CaptureToSemantic.Metadata {
		if mapping.CaptureKind == "metadata" && mapping.SemanticKind == "metadata" {
			return mapping, true
		}
	}
	return MetadataMapping{}, false
}

func validateMetadataValueTypeCompatibility(valueType, valueKind string) error {
	switch valueKind {
	case MetadataValueKindString:
		if valueType != "string" {
			return fmt.Errorf("valueType %q requires string", valueType)
		}
	case MetadataValueKindBoolean:
		if valueType != "boolean" {
			return fmt.Errorf("valueType %q requires boolean", valueType)
		}
	case MetadataValueKindNumber, MetadataValueKindInteger:
		if valueType != "number" {
			return fmt.Errorf("valueType %q requires number", valueType)
		}
	default:
		return fmt.Errorf("unsupported metadata value kind %q", valueKind)
	}
	return nil
}

func newMetadataResolutionError(code, targetName, metadataKey, metadataID, message string, err error) *MetadataResolutionError {
	return &MetadataResolutionError{
		Code:        code,
		Message:     message,
		TargetName:  targetName,
		MetadataKey: metadataKey,
		MetadataID:  metadataID,
		Err:         err,
	}
}
