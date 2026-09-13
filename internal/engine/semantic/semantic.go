package semantic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"parametron/internal/engine/cad"
)

const SchemaVersion = "1.0"

const (
	OwnerKindComponent = "component"
	OwnerKindGroup     = "group"
	OwnerKindFeature   = "feature"
)

type SystemIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Model struct {
	SchemaVersion           string           `json:"schemaVersion"`
	SourceDocumentLogicalID string           `json:"sourceDocumentLogicalId"`
	Adapter                 *SystemIdentity  `json:"adapter,omitempty"`
	CADSystem               *SystemIdentity  `json:"cadSystem,omitempty"`
	RootComponentID         string           `json:"rootComponentId"`
	Components              []Component      `json:"components"`
	Features                []Feature        `json:"features"`
	ParameterGroups         []ParameterGroup `json:"parameterGroups"`
	Parameters              []Parameter      `json:"parameters"`
	Metadata                []Metadata       `json:"metadata"`
	ProductIntents          []ProductIntent  `json:"productIntents,omitempty"`
}

type Component struct {
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"displayName"`
	ParentID      string            `json:"parentId,omitempty"`
	ChildrenIDs   []string          `json:"childrenIds"`
	Material      string            `json:"material"`
	Quantity      int               `json:"quantity"`
	Targetability cad.Targetability `json:"targetability"`
}

type Feature struct {
	ID            string            `json:"id"`
	ComponentID   string            `json:"componentId"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"displayName"`
	NativeType    string            `json:"nativeType"`
	Targetability cad.Targetability `json:"targetability"`
}

type ParameterGroup struct {
	ID               string `json:"id"`
	OwnerComponentID string `json:"ownerComponentId"`
	Name             string `json:"name"`
	DisplayName      string `json:"displayName"`
	GroupKind        string `json:"groupKind"`
	NativeType       string `json:"nativeType"`
	Observable       bool   `json:"observable"`
	Writable         bool   `json:"writable"`
}

type Parameter struct {
	ID           string      `json:"id"`
	OwnerKind    string      `json:"ownerKind"`
	OwnerID      string      `json:"ownerId"`
	ComponentID  string      `json:"componentId"`
	GroupID      string      `json:"groupId,omitempty"`
	Name         string      `json:"name"`
	DisplayName  string      `json:"displayName"`
	ValueType    string      `json:"valueType"`
	NativeType   string      `json:"nativeType"`
	Unit         string      `json:"unit,omitempty"`
	Observable   bool        `json:"observable"`
	Writable     bool        `json:"writable"`
	CurrentValue ScalarValue `json:"currentValue,omitempty"`
}

type Metadata struct {
	ID           string      `json:"id"`
	OwnerKind    string      `json:"ownerKind"`
	OwnerID      string      `json:"ownerId"`
	ComponentID  string      `json:"componentId"`
	Key          string      `json:"key"`
	DisplayName  string      `json:"displayName"`
	ValueType    string      `json:"valueType"`
	NativeType   string      `json:"nativeType"`
	Observable   bool        `json:"observable"`
	Writable     bool        `json:"writable"`
	CurrentValue ScalarValue `json:"currentValue,omitempty"`
}

type ProductIntent struct {
	Name                   string                    `json:"name"`
	Adapter                string                    `json:"adapter,omitempty"`
	SourceModelLogicalID   string                    `json:"sourceModelLogicalId,omitempty"`
	RequestedOutputFormats []string                  `json:"requestedOutputFormats"`
	Outputs                []OutputIntent            `json:"outputs"`
	ExportedParameters     []ExportedParameterIntent `json:"exportedParameters"`
	Mutations              []MutationIntent          `json:"mutations"`
}

type ExportedParameterIntent struct {
	DSLParameterName    string `json:"dslParameterName"`
	SemanticParameterID string `json:"semanticParameterId"`
	Type                string `json:"type,omitempty"`
}

type OutputIntent struct {
	OutputType       string `json:"outputType"`
	TargetEntityKind string `json:"targetEntityKind,omitempty"`
	TargetSemanticID string `json:"targetSemanticId,omitempty"`
	TargetNameSource string `json:"targetNameSource,omitempty"`
	Scope            string `json:"scope,omitempty"`
}

type MutationIntent struct {
	OperationKind    string              `json:"operationKind"`
	TargetEntityKind string              `json:"targetEntityKind"`
	TargetSemanticID string              `json:"targetSemanticId"`
	TargetField      string              `json:"targetField,omitempty"`
	Scope            string              `json:"scope"`
	ValueSource      MutationValueSource `json:"valueSource"`
}

type MutationValueSource struct {
	Kind                string      `json:"kind"`
	DSLParameterName    string      `json:"dslParameterName,omitempty"`
	SemanticParameterID string      `json:"semanticParameterId,omitempty"`
	Scalar              ScalarValue `json:"scalar,omitempty"`
	BooleanState        *bool       `json:"booleanState,omitempty"`
}

type ScalarValue struct {
	raw []byte
	set bool
}

func NewScalarValue(data []byte) (ScalarValue, error) {
	canonical, err := canonicalizeJSONScalar(data)
	if err != nil {
		return ScalarValue{}, err
	}
	return ScalarValue{raw: canonical, set: true}, nil
}

func (v ScalarValue) Raw() []byte {
	if !v.set {
		return nil
	}
	return append([]byte(nil), v.raw...)
}

func (v ScalarValue) IsZero() bool {
	return !v.set
}

func (v ScalarValue) MarshalJSON() ([]byte, error) {
	if !v.set {
		return []byte("null"), nil
	}
	return append([]byte(nil), v.raw...), nil
}

func (v *ScalarValue) UnmarshalJSON(data []byte) error {
	canonical, err := canonicalizeJSONScalar(data)
	if err != nil {
		return err
	}
	v.raw = canonical
	v.set = true
	return nil
}

func Validate(model *Model) error {
	problems := validateModel(model)
	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

func CanonicalJSON(model *Model) ([]byte, error) {
	canonical, err := Canonicalize(model)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Canonicalize(model *Model) (*Model, error) {
	if err := Validate(model); err != nil {
		return nil, err
	}
	out := Clone(model)
	sortForCanonical(out)
	return out, nil
}

func Clone(model *Model) *Model {
	if model == nil {
		return nil
	}

	out := *model
	if model.Adapter != nil {
		copyValue := *model.Adapter
		out.Adapter = &copyValue
	}
	if model.CADSystem != nil {
		copyValue := *model.CADSystem
		out.CADSystem = &copyValue
	}
	out.Components = cloneComponents(model.Components)
	out.Features = cloneFeatures(model.Features)
	out.ParameterGroups = cloneParameterGroups(model.ParameterGroups)
	out.Parameters = cloneParameters(model.Parameters)
	out.Metadata = cloneMetadata(model.Metadata)
	out.ProductIntents = cloneProductIntents(model.ProductIntents)
	return &out
}

func (m *Model) CanonicalComponents() []Component {
	if m == nil {
		return nil
	}
	out := cloneComponents(m.Components)
	sortComponents(out)
	return out
}

func (m *Model) CanonicalParameterGroups() []ParameterGroup {
	if m == nil {
		return nil
	}
	out := cloneParameterGroups(m.ParameterGroups)
	sortParameterGroups(out)
	return out
}

func (m *Model) CanonicalFeatures() []Feature {
	if m == nil {
		return nil
	}
	out := cloneFeatures(m.Features)
	sortFeatures(out)
	return out
}

func (m *Model) CanonicalParameters() []Parameter {
	if m == nil {
		return nil
	}
	out := cloneParameters(m.Parameters)
	sortParameters(out)
	return out
}

func (m *Model) CanonicalMetadata() []Metadata {
	if m == nil {
		return nil
	}
	out := cloneMetadata(m.Metadata)
	sortMetadata(out)
	return out
}

func (m *Model) CanonicalProductIntents() []ProductIntent {
	if m == nil {
		return nil
	}
	out := cloneProductIntents(m.ProductIntents)
	sortProductIntents(out)
	return out
}

func canonicalizeJSONScalar(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("must be valid JSON scalar: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("must be valid JSON scalar: unexpected trailing JSON value")
		}
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
