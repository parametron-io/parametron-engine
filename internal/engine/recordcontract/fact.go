package recordcontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FactRequestSchemaVersion is the independent version of the fact request sidecar.
const FactRequestSchemaVersion = "1.0"

// FactEvidenceSchemaVersion is the independent version of the fact evidence sidecar.
const FactEvidenceSchemaVersion = "1.0"

// FactKindBoundProperty is the only accepted bounded fact kind. A bound
// property fact observes one explicitly named native property under an
// explicitly declared resource-qualified subject. No heuristic property
// discovery, material inference, or physical mass semantics are implied.
const FactKindBoundProperty = "bound_property"

// Bounded fact value kinds. The value kind is cross-checked against the actual
// JSON scalar type at intake and again during record validation.
const (
	FactValueKindNumber  = "number"
	FactValueKindInteger = "integer"
	FactValueKindString  = "string"
	FactValueKindBoolean = "boolean"
)

// Explicit fact availability states. Omission is never a substitute state.
const (
	FactAvailabilityObserved    = "observed"
	FactAvailabilityUnavailable = "unavailable"
	FactAvailabilityUnsupported = "unsupported"
)

// Explicit per-binding requirement states.
const (
	FactRequirementOptional = "optional"
	FactRequirementRequired = "required"
)

// Canonical fact evidence provenance inside observation facts.
const (
	FactEvidenceSourceKind = "fact-evidence"
	FactEvidenceSourceRef  = "raw/fact/prm.fact-evidence.json"
)

const (
	factMaxBindings       = 32
	factMaxContextFrames  = 16
	factMaxPropertyLength = 128
	factMaxUnitLength     = 32
)

var (
	ErrInvalidFact             = errors.New("invalid fact contract")
	ErrRequiredFactUnavailable = errors.New("required fact observation unavailable or unsupported")
)

// FactBinding is the explicit Engine-owned binding of one bounded engineering
// fact. The native property is named by the request; the Engine never infers
// native facts from property names. Context carries #50 occurrence frames only
// when the fact is genuinely occurrence-specific.
type FactBinding struct {
	Kind        string                   `json:"kind"`
	Subject     OccurrenceEntity         `json:"subject"`
	Context     []OccurrenceContextFrame `json:"context"`
	Property    string                   `json:"property"`
	ValueKind   string                   `json:"valueKind"`
	Unit        string                   `json:"unit,omitempty"`
	Requirement string                   `json:"requirement"`
}

// FactRequest is explicit opt-in sidecar intent. Nil intent requests no fact
// observation and forbids unsolicited fact evidence.
type FactRequest struct {
	SchemaVersion string        `json:"schemaVersion"`
	Bindings      []FactBinding `json:"bindings"`
}

func factInvalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidFact, message)
}

func factExactText(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}

func validateFactUnit(unit string, valueKind string) error {
	if unit == "" {
		return nil // an unset unit is an explicit unitless fact, never an implicit unit
	}
	// An opaque unit token is bounded explicit text; no unit algebra or
	// conversion is performed or implied.
	if !factExactText(unit) || strings.ContainsAny(unit, "\n\r\t") || len(unit) > factMaxUnitLength {
		return factInvalid("invalid fact unit token")
	}
	if valueKind == FactValueKindString || valueKind == FactValueKindBoolean {
		return factInvalid("unit is incompatible with non-numeric fact values")
	}
	return nil
}

// KnownFactKind reports whether kind belongs to the closed fact kind vocabulary.
func KnownFactKind(kind string) bool {
	return kind == FactKindBoundProperty
}

// KnownFactAvailability reports whether state belongs to the closed fact
// availability vocabulary.
func KnownFactAvailability(state string) bool {
	switch state {
	case FactAvailabilityObserved, FactAvailabilityUnavailable, FactAvailabilityUnsupported:
		return true
	default:
		return false
	}
}

// factNormalizedContext returns a non-nil context slice for stable keying.
func factNormalizedContext(context []OccurrenceContextFrame) []OccurrenceContextFrame {
	if context == nil {
		return []OccurrenceContextFrame{}
	}
	return context
}

// ValidateFactBinding checks one explicit binding deterministically.
func ValidateFactBinding(binding FactBinding) error {
	if !KnownFactKind(binding.Kind) {
		return factInvalid("unsupported fact kind")
	}
	if err := validateOccurrenceEntity(binding.Subject); err != nil {
		return err
	}
	if len(binding.Context) > factMaxContextFrames {
		return factInvalid("fact context exceeds the bounded frame count")
	}
	// Reuse the occurrence scope validator so context frames keep the single
	// #50 validation semantics, including cyclicity and bound checks.
	if err := ValidateOccurrenceScope(OccurrenceScope{
		Parent:    binding.Subject,
		Context:   factNormalizedContext(binding.Context),
		Mechanism: OccurrenceDirectMemberMechanism,
	}); err != nil {
		return err
	}
	if !factExactText(binding.Property) || len(binding.Property) > factMaxPropertyLength {
		return factInvalid("invalid native property binding")
	}
	switch binding.ValueKind {
	case FactValueKindNumber, FactValueKindInteger, FactValueKindString, FactValueKindBoolean:
	default:
		return factInvalid("unsupported fact value kind")
	}
	if err := validateFactUnit(binding.Unit, binding.ValueKind); err != nil {
		return err
	}
	switch binding.Requirement {
	case FactRequirementOptional, FactRequirementRequired:
	default:
		return factInvalid("invalid fact requirement")
	}
	return nil
}

// ValidateFactRequest validates the explicit sidecar request contract.
func ValidateFactRequest(request FactRequest) error {
	if request.SchemaVersion != FactRequestSchemaVersion {
		return factInvalid("unsupported fact request schema version")
	}
	if len(request.Bindings) == 0 {
		return factInvalid("at least one fact binding is required")
	}
	if len(request.Bindings) > factMaxBindings {
		return factInvalid("fact bindings exceed the bounded request size")
	}
	seen := make(map[string]struct{}, len(request.Bindings))
	for _, binding := range request.Bindings {
		if err := ValidateFactBinding(binding); err != nil {
			return err
		}
		key, err := FactBindingKey(binding)
		if err != nil {
			return err
		}
		if _, exists := seen[key]; exists {
			return factInvalid("duplicate or conflicting fact binding")
		}
		seen[key] = struct{}{}
	}
	return nil
}

// factBindingKeyMaterial excludes the semantic annotation and observed value
// material, so conflicting evidence cannot hide behind different duplicate keys.
type factBindingKeyMaterial struct {
	Kind     string                   `json:"kind"`
	Subject  OccurrenceEntity         `json:"subject"`
	Context  []OccurrenceContextFrame `json:"context"`
	Property string                   `json:"property"`
}

func factBindingKeyMaterialFrom(binding FactBinding) factBindingKeyMaterial {
	subject := binding.Subject
	subject.SemanticID = ""
	return factBindingKeyMaterial{
		Kind:     binding.Kind,
		Subject:  subject,
		Context:  factNormalizedContext(binding.Context),
		Property: binding.Property,
	}
}

// FactBindingKey derives the deterministic fact slot key. Equivalent requests
// key identically; the semantic annotation is not fact identity material.
func FactBindingKey(binding FactBinding) (string, error) {
	data, err := json.Marshal(factBindingKeyMaterialFrom(binding))
	if err != nil {
		return "", fmt.Errorf("%w: marshal fact binding key: %v", ErrInvalidFact, err)
	}
	return string(data), nil
}

// CloneFactBinding copies caller-owned binding material without normalizing intent.
func CloneFactBinding(binding FactBinding) FactBinding {
	out := binding
	out.Context = append([]OccurrenceContextFrame{}, factNormalizedContext(binding.Context)...)
	return out
}

// CloneFactRequest copies caller-owned context without normalizing intent.
func CloneFactRequest(request *FactRequest) *FactRequest {
	if request == nil {
		return nil
	}
	out := *request
	out.Bindings = make([]FactBinding, len(request.Bindings))
	for i, binding := range request.Bindings {
		out.Bindings[i] = CloneFactBinding(binding)
	}
	return &out
}

// canonicalFactNumber canonicalizes a finite numeric lexeme deterministically:
// 42, 42.0 and 4.2e1 all normalize to the same canonical form. NaN and
// infinities are rejected and never coerced.
func canonicalFactNumber(raw string) (string, error) {
	lexeme := strings.TrimSpace(raw)
	if lexeme == "" {
		return "", factInvalid("numeric fact value is required")
	}
	if strings.EqualFold(lexeme, "nan") || strings.EqualFold(lexeme, "inf") ||
		strings.EqualFold(lexeme, "infinity") || strings.EqualFold(lexeme, "+inf") ||
		strings.EqualFold(lexeme, "+infinity") || strings.EqualFold(lexeme, "-inf") ||
		strings.EqualFold(lexeme, "-infinity") {
		return "", factInvalid("non-finite numeric fact value")
	}
	value, err := strconv.ParseFloat(lexeme, 64)
	if err != nil {
		return "", factInvalid("malformed numeric fact value")
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", factInvalid("non-finite numeric fact value")
	}
	return strconv.FormatFloat(value, 'f', -1, 64), nil
}

// CanonicalFactScalar cross-checks valueKind against the actual JSON scalar
// lexeme and returns the deterministic canonical raw value. Zero is a valid
// observed value; null and non-scalar material are rejected.
func CanonicalFactScalar(valueKind string, raw string) (string, error) {
	switch valueKind {
	case FactValueKindNumber:
		return canonicalFactNumber(raw)
	case FactValueKindInteger:
		canonical, err := canonicalFactNumber(raw)
		if err != nil {
			return "", err
		}
		value, err := strconv.ParseFloat(canonical, 64)
		if err != nil {
			return "", factInvalid("malformed integer fact value")
		}
		if value != math.Trunc(value) {
			return "", factInvalid("non-integral fact value")
		}
		return canonical, nil
	case FactValueKindString:
		if raw == "" {
			return "", factInvalid("string fact value is required")
		}
		var decoded string
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return "", factInvalid("string fact value must be a JSON string")
		}
		encoded, err := json.Marshal(decoded)
		if err != nil {
			return "", factInvalid("malformed string fact value")
		}
		if string(encoded) != raw {
			return "", factInvalid("string fact value is not canonical")
		}
		return raw, nil
	case FactValueKindBoolean:
		if raw == "true" || raw == "false" {
			return raw, nil
		}
		return "", factInvalid("boolean fact value must be true or false")
	default:
		return "", factInvalid("unsupported fact value kind")
	}
}
