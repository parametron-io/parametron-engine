package semanticmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	FileName      = "parametron.semantic-map.json"
	SchemaVersion = "1.0"
)

type SemanticMap struct {
	SchemaVersion         string                `json:"schemaVersion"`
	MappingID             string                `json:"mappingId"`
	Adapter               string                `json:"adapter"`
	CADSystem             *CADSystem            `json:"cadSystem"`
	SemanticTypes         *SemanticTypes        `json:"semanticTypes"`
	CaptureToSemantic     *CaptureToSemantic    `json:"captureToSemantic"`
	SemanticToManifest    *SemanticToManifest   `json:"semanticToManifest"`
	OverridePolicy        *OverridePolicy       `json:"overridePolicy"`
	OperationCapabilities map[string]Capability `json:"operationCapabilities"`
	Determinism           *Determinism          `json:"determinism"`
}

type CADSystem struct {
	Name       string `json:"name"`
	MinVersion string `json:"minVersion,omitempty"`
}

type SemanticTypes struct {
	ParameterTypes   map[string]ParameterType `json:"parameterTypes"`
	ResolutionPolicy *ResolutionPolicy        `json:"resolutionPolicy"`
}

type OverridePolicy struct {
	AllowAdapterOverrides *bool `json:"allowAdapterOverrides"`
	AllowProjectOverrides *bool `json:"allowProjectOverrides"`
	AllowUserOverrides    *bool `json:"allowUserOverrides"`
}

type ParameterType struct {
	ValueKind   string `json:"valueKind"`
	DefaultUnit string `json:"defaultUnit"`
	Coercion    string `json:"coercion"`
}

type ResolutionPolicy struct {
	Precedence           []string `json:"precedence"`
	AllowEngineDefault   *bool    `json:"allowEngineDefault"`
	AllowEngineInference *bool    `json:"allowEngineInference"`
}

type CaptureToSemantic struct {
	ParameterGroups []ParameterGroupMapping `json:"parameterGroups"`
	Parameters      []ParameterMapping      `json:"parameters"`
	Components      []ComponentMapping      `json:"components"`
	Metadata        []MetadataMapping       `json:"metadata"`
}

type ParameterGroupMapping struct {
	CaptureKind      string `json:"captureKind"`
	CADContainerType string `json:"cadContainerType"`
	SemanticKind     string `json:"semanticKind"`
}

type ParameterMapping struct {
	CaptureKind  string                   `json:"captureKind"`
	SemanticKind string                   `json:"semanticKind"`
	Identity     string                   `json:"identity"`
	Name         string                   `json:"name"`
	Group        string                   `json:"group"`
	SemanticType ParameterSemanticTypeRef `json:"semanticType"`
}

type ParameterSemanticTypeRef struct {
	From string            `json:"from"`
	Map  map[string]string `json:"map"`
}

type ComponentMapping struct {
	CaptureKind   string            `json:"captureKind"`
	SemanticKinds map[string]string `json:"semanticKinds"`
	Identity      string            `json:"identity"`
	Targetability string            `json:"targetability"`
}

type MetadataMapping struct {
	CaptureKind  string `json:"captureKind"`
	SemanticKind string `json:"semanticKind"`
	Identity     string `json:"identity"`
	Key          string `json:"key"`
	ValueKind    string `json:"valueKind"`
}

type SemanticToManifest struct {
	Parameters *ParameterManifestMapping        `json:"parameters"`
	Outputs    map[string]OutputManifestMapping `json:"outputs"`
	Mutations  map[string]MutationMapping       `json:"mutations"`
}

type ParameterManifestMapping struct {
	Target string `json:"target"`
	Name   string `json:"name"`
	Value  string `json:"value"`
	Type   string `json:"type"`
	Unit   string `json:"unit"`
}

type OutputManifestMapping struct {
	ManifestType string            `json:"manifestType"`
	TargetField  string            `json:"targetField,omitempty"`
	TargetSource string            `json:"targetSource,omitempty"`
	ScopeField   string            `json:"scopeField,omitempty"`
	ScopeMap     map[string]string `json:"scopeMap,omitempty"`
}

type MutationMapping struct {
	ManifestCollection string `json:"manifestCollection"`
	TargetField        string `json:"targetField"`
	ValueField         string `json:"valueField,omitempty"`
	Value              *bool  `json:"value,omitempty"`
	PropertyField      string `json:"propertyField,omitempty"`
}

type Capability struct {
	RequiresTargetability  string   `json:"requiresTargetability,omitempty"`
	RequiresWritable       bool     `json:"requiresWritable,omitempty"`
	AllowedSemanticTargets []string `json:"allowedSemanticTargets"`
}

type Determinism struct {
	AllowImplicitFallback     *bool `json:"allowImplicitFallback"`
	AllowCaseInsensitiveMatch *bool `json:"allowCaseInsensitiveMatch"`
	AllowDisplayNameIdentity  *bool `json:"allowDisplayNameIdentity"`
	AllowAdapterInference     *bool `json:"allowAdapterInference"`
}

func Parse(data []byte) (*SemanticMap, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, &DecodeError{Err: err}
	}
	if err := ensureNoTrailingJSON(decoder); err != nil {
		return nil, &DecodeError{Err: err}
	}
	if _, ok := raw.(map[string]any); !ok {
		return nil, &ValidationError{Problems: []string{"root must be a JSON object"}}
	}

	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var contract SemanticMap
	if err := decoder.Decode(&contract); err != nil {
		return nil, &DecodeError{Err: err}
	}
	if err := ensureNoTrailingJSON(decoder); err != nil {
		return nil, &DecodeError{Err: err}
	}

	if err := Validate(&contract); err != nil {
		return nil, err
	}
	return &contract, nil
}

func Read(r io.Reader) (*SemanticMap, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: reader must not be nil", ErrIO)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIO, err)
	}
	return Parse(data)
}

func Load(path string) (*SemanticMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{Path: path, Err: err}
	}
	return Parse(data)
}

func ensureNoTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("unexpected trailing JSON content")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
