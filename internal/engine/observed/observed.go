package observed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const SchemaVersion = "1.0"

var (
	ErrIO         = errors.New("observed I/O error")
	ErrDecode     = errors.New("observed decode error")
	ErrValidation = errors.New("observed validation error")

	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string {
	if e == nil {
		return ErrIO.Error()
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *FileError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrIO}
	}
	return []error{ErrIO, e.Err}
}

type DecodeError struct {
	Err error
}

func (e *DecodeError) Error() string {
	if e == nil || e.Err == nil {
		return ErrDecode.Error()
	}
	return fmt.Sprintf("%s: %v", ErrDecode, e.Err)
}

func (e *DecodeError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrDecode}
	}
	return []error{ErrDecode, e.Err}
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil || e.Message == "" {
		return ErrValidation.Error()
	}
	return fmt.Sprintf("%s: %s", ErrValidation, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

type Observed struct {
	SchemaVersion string      `json:"schemaVersion"`
	WorkingCopy   WorkingCopy `json:"workingCopy"`
	Observation   Observation `json:"observation"`
}

type WorkingCopy struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Observation struct {
	Parameters  []Parameter             `json:"parameters"`
	Metadata    []Metadata              `json:"metadata"`
	References  []Reference             `json:"references"`
	Components  []Component             `json:"components"`
	TargetState *TargetStateObservation `json:"targetState,omitempty"`
}

const (
	TargetDestinationAssembly = "assembly"
	TargetDestinationPart     = "part"

	BooleanEvidenceStatusObserved      = "observed"
	BooleanEvidenceStatusTargetMissing = "target_missing"
	BooleanEvidenceStatusUnavailable   = "unavailable"

	ExistenceEvidenceStatusExists      = "exists"
	ExistenceEvidenceStatusAbsent      = "absent"
	ExistenceEvidenceStatusUnavailable = "unavailable"
)

type TargetStateObservation struct {
	Suppression []BooleanTargetEvidence   `json:"suppression"`
	Visibility  []BooleanTargetEvidence   `json:"visibility"`
	Existence   []ExistenceTargetEvidence `json:"existence"`
}

type BooleanTargetEvidence struct {
	Destination string `json:"destination"`
	Object      string `json:"object"`
	Status      string `json:"status"`
	Value       *bool  `json:"value,omitempty"`
}

type ExistenceTargetEvidence struct {
	Destination string `json:"destination"`
	Object      string `json:"object"`
	Status      string `json:"status"`
}

type Parameter struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GroupID   string `json:"-"`
	Value     Value  `json:"value"`
	ValueKind string `json:"valueKind"`
}

type Metadata struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	OwnerID   string `json:"-"`
	Value     Value  `json:"value"`
	ValueKind string `json:"valueKind"`
}

type Reference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

const (
	ComponentKindAssembly = "assembly"
	ComponentKindPart     = "part"
)

type Component struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	ParentID string `json:"-"`
}

type Value struct {
	raw []byte
	set bool
}

func NewValue(raw json.RawMessage) (Value, error) {
	canonical, err := canonicalizeScalarJSON(raw)
	if err != nil {
		return Value{}, err
	}
	return Value{raw: canonical, set: true}, nil
}

func (v Value) Raw() []byte {
	if !v.set {
		return nil
	}
	return append([]byte(nil), v.raw...)
}

func (v Value) IsZero() bool {
	return !v.set
}

func (v Value) MarshalJSON() ([]byte, error) {
	if !v.set {
		return []byte("null"), nil
	}
	return append([]byte(nil), v.raw...), nil
}

func (v *Value) UnmarshalJSON(data []byte) error {
	canonical, err := canonicalizeScalarJSON(data)
	if err != nil {
		return err
	}
	v.raw = canonical
	v.set = true
	return nil
}

func Parse(data []byte) (*Observed, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, &DecodeError{Err: err}
	}
	if err := ensureNoTrailingJSON(decoder); err != nil {
		return nil, &DecodeError{Err: err}
	}

	root, ok := raw.(map[string]any)
	if !ok {
		return nil, &ValidationError{Message: "root must be a JSON object"}
	}

	observed, err := parseObservedObject(root)
	if err != nil {
		return nil, err
	}
	if err := validateObserved(observed); err != nil {
		return nil, err
	}
	return observed, nil
}

func LoadFile(path string) (*Observed, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{Path: path, Err: err}
	}
	return Parse(data)
}

func Validate(observed *Observed) error {
	return validateObserved(observed)
}

func CanonicalJSON(observed *Observed) ([]byte, error) {
	if err := Validate(observed); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.WriteByte('{')
	writeJSONString(&out, "schemaVersion")
	out.WriteByte(':')
	writeJSONString(&out, observed.SchemaVersion)
	out.WriteByte(',')
	writeJSONString(&out, "workingCopy")
	out.WriteByte(':')
	writeWorkingCopy(&out, observed.WorkingCopy)
	out.WriteByte(',')
	writeJSONString(&out, "observation")
	out.WriteByte(':')
	writeObservation(&out, observed.Observation)
	out.WriteByte('}')
	return out.Bytes(), nil
}

func WriteFile(path string, observed *Observed) error {
	data, err := CanonicalJSON(observed)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &FileError{Path: path, Err: fmt.Errorf("create parent directory: %w", err)}
	}

	tmpFile, err := os.CreateTemp(dir, ".observed-tmp-*")
	if err != nil {
		return &FileError{Path: path, Err: fmt.Errorf("create temporary observed file: %w", err)}
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: fmt.Errorf("write temporary observed file: %w", err)}
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: fmt.Errorf("close temporary observed file: %w", err)}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: fmt.Errorf("finalize observed file: %w", err)}
	}
	return nil
}

func parseObservedObject(root map[string]any) (*Observed, error) {
	if err := ensureAllowedKeys(root, "root", "schemaVersion", "workingCopy", "observation"); err != nil {
		return nil, err
	}

	schemaVersion, err := requiredStringField(root, "schemaVersion")
	if err != nil {
		return nil, err
	}
	if schemaVersion != SchemaVersion {
		return nil, &ValidationError{Message: fmt.Sprintf("schemaVersion must be %q", SchemaVersion)}
	}

	workingCopyValue, ok := root["workingCopy"]
	if !ok {
		return nil, &ValidationError{Message: "workingCopy is required"}
	}
	workingCopyObject, ok := workingCopyValue.(map[string]any)
	if !ok {
		return nil, &ValidationError{Message: "workingCopy must be an object"}
	}
	workingCopy, err := parseWorkingCopyObject(workingCopyObject)
	if err != nil {
		return nil, err
	}

	observationValue, ok := root["observation"]
	if !ok {
		return nil, &ValidationError{Message: "observation is required"}
	}
	observationObject, ok := observationValue.(map[string]any)
	if !ok {
		return nil, &ValidationError{Message: "observation must be an object"}
	}
	observation, err := parseObservationObject(observationObject)
	if err != nil {
		return nil, err
	}

	return &Observed{
		SchemaVersion: schemaVersion,
		WorkingCopy:   workingCopy,
		Observation:   observation,
	}, nil
}

func parseWorkingCopyObject(root map[string]any) (WorkingCopy, error) {
	if err := ensureAllowedKeys(root, "workingCopy", "path", "sha256"); err != nil {
		return WorkingCopy{}, err
	}

	path, err := requiredStringField(root, "path")
	if err != nil {
		return WorkingCopy{}, wrapFieldError("workingCopy", err)
	}
	sha256Value, err := requiredStringField(root, "sha256")
	if err != nil {
		return WorkingCopy{}, wrapFieldError("workingCopy", err)
	}

	return WorkingCopy{
		Path:   path,
		SHA256: sha256Value,
	}, nil
}

func parseObservationObject(root map[string]any) (Observation, error) {
	if err := ensureAllowedKeys(root, "observation", "parameters", "metadata", "references", "components", "targetState"); err != nil {
		return Observation{}, err
	}

	parameters, err := parseParametersField(root, "parameters")
	if err != nil {
		return Observation{}, err
	}
	metadataEntries, err := parseMetadataField(root, "metadata")
	if err != nil {
		return Observation{}, err
	}
	references, err := parseReferencesField(root, "references")
	if err != nil {
		return Observation{}, err
	}
	components, err := parseComponentsField(root, "components")
	if err != nil {
		return Observation{}, err
	}

	observation := Observation{
		Parameters: parameters,
		Metadata:   metadataEntries,
		References: references,
		Components: components,
	}
	if _, ok := root["targetState"]; ok {
		object, ok := root["targetState"].(map[string]any)
		if !ok {
			return Observation{}, &ValidationError{Message: "observation.targetState must be an object"}
		}
		targetState, err := parseTargetStateObservation(object)
		if err != nil {
			return Observation{}, err
		}
		observation.TargetState = targetState
	}
	return observation, nil
}

func parseTargetStateObservation(root map[string]any) (*TargetStateObservation, error) {
	if err := ensureAllowedKeys(root, "observation.targetState", "suppression", "visibility", "existence"); err != nil {
		return nil, err
	}
	parseBoolean := func(field string) ([]BooleanTargetEvidence, error) {
		value, ok := root[field]
		if !ok {
			return nil, &ValidationError{Message: "observation.targetState." + field + " is required"}
		}
		items, ok := value.([]any)
		if !ok {
			return nil, &ValidationError{Message: "observation.targetState." + field + " must be an array"}
		}
		out := make([]BooleanTargetEvidence, 0, len(items))
		for index, item := range items {
			object, ok := item.(map[string]any)
			location := fmt.Sprintf("observation.targetState.%s[%d]", field, index)
			if !ok {
				return nil, &ValidationError{Message: location + " must be an object"}
			}
			if err := ensureAllowedKeys(object, location, "destination", "object", "status", "value"); err != nil {
				return nil, err
			}
			destination, err := requiredStringField(object, "destination")
			if err != nil {
				return nil, wrapIndexedFieldError("observation.targetState."+field, index, err)
			}
			name, err := requiredStringField(object, "object")
			if err != nil {
				return nil, wrapIndexedFieldError("observation.targetState."+field, index, err)
			}
			status, err := requiredStringField(object, "status")
			if err != nil {
				return nil, wrapIndexedFieldError("observation.targetState."+field, index, err)
			}
			entry := BooleanTargetEvidence{Destination: destination, Object: name, Status: status}
			if raw, exists := object["value"]; exists {
				typed, ok := raw.(bool)
				if !ok {
					return nil, &ValidationError{Message: location + ".value must be a boolean"}
				}
				entry.Value = &typed
			}
			out = append(out, entry)
		}
		return out, nil
	}
	parseExistence := func() ([]ExistenceTargetEvidence, error) {
		value, ok := root["existence"]
		if !ok {
			return nil, &ValidationError{Message: "observation.targetState.existence is required"}
		}
		items, ok := value.([]any)
		if !ok {
			return nil, &ValidationError{Message: "observation.targetState.existence must be an array"}
		}
		out := make([]ExistenceTargetEvidence, 0, len(items))
		for index, item := range items {
			object, ok := item.(map[string]any)
			location := fmt.Sprintf("observation.targetState.existence[%d]", index)
			if !ok {
				return nil, &ValidationError{Message: location + " must be an object"}
			}
			if err := ensureAllowedKeys(object, location, "destination", "object", "status"); err != nil {
				return nil, err
			}
			destination, err := requiredStringField(object, "destination")
			if err != nil {
				return nil, wrapIndexedFieldError("observation.targetState.existence", index, err)
			}
			name, err := requiredStringField(object, "object")
			if err != nil {
				return nil, wrapIndexedFieldError("observation.targetState.existence", index, err)
			}
			status, err := requiredStringField(object, "status")
			if err != nil {
				return nil, wrapIndexedFieldError("observation.targetState.existence", index, err)
			}
			out = append(out, ExistenceTargetEvidence{Destination: destination, Object: name, Status: status})
		}
		return out, nil
	}
	suppression, err := parseBoolean("suppression")
	if err != nil {
		return nil, err
	}
	visibility, err := parseBoolean("visibility")
	if err != nil {
		return nil, err
	}
	existence, err := parseExistence()
	if err != nil {
		return nil, err
	}
	return &TargetStateObservation{Suppression: suppression, Visibility: visibility, Existence: existence}, nil
}

func parseParametersField(root map[string]any, field string) ([]Parameter, error) {
	value, ok := root[field]
	if !ok {
		return make([]Parameter, 0), nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("observation.%s must be an array", field)}
	}
	parameters := make([]Parameter, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("observation.parameters[%d] must be an object", index)}
		}
		parameter, err := parseParameterObject(index, object)
		if err != nil {
			return nil, err
		}
		parameters = append(parameters, parameter)
	}
	return parameters, nil
}

func parseParameterObject(index int, root map[string]any) (Parameter, error) {
	if err := ensureAllowedKeys(root, fmt.Sprintf("observation.parameters[%d]", index), "id", "name", "groupId", "value", "valueKind"); err != nil {
		return Parameter{}, err
	}

	id, err := requiredStringField(root, "id")
	if err != nil {
		return Parameter{}, wrapIndexedFieldError("observation.parameters", index, err)
	}
	name, err := requiredStringField(root, "name")
	if err != nil {
		return Parameter{}, wrapIndexedFieldError("observation.parameters", index, err)
	}
	valueKind, err := requiredStringField(root, "valueKind")
	if err != nil {
		return Parameter{}, wrapIndexedFieldError("observation.parameters", index, err)
	}

	parameter := Parameter{
		ID:        id,
		Name:      name,
		ValueKind: valueKind,
	}
	if rawGroupID, ok := root["groupId"]; ok {
		switch typed := rawGroupID.(type) {
		case nil:
			parameter.GroupID = ""
		case string:
			if strings.TrimSpace(typed) == "" {
				parameter.GroupID = ""
			} else {
				parameter.GroupID = typed
			}
		default:
			return Parameter{}, &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].groupId must be a string or null", index)}
		}
	}

	rawValue, ok := root["value"]
	if !ok {
		return Parameter{}, &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].value is required", index)}
	}
	rawBytes, err := json.Marshal(rawValue)
	if err != nil {
		return Parameter{}, &DecodeError{Err: err}
	}
	value, err := NewValue(rawBytes)
	if err != nil {
		return Parameter{}, &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].value %s", index, err.Error())}
	}
	parameter.Value = value

	return parameter, nil
}

func parseMetadataField(root map[string]any, field string) ([]Metadata, error) {
	value, ok := root[field]
	if !ok {
		return make([]Metadata, 0), nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("observation.%s must be an array", field)}
	}
	metadataEntries := make([]Metadata, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("observation.metadata[%d] must be an object", index)}
		}
		metadataEntry, err := parseMetadataObject(index, object)
		if err != nil {
			return nil, err
		}
		metadataEntries = append(metadataEntries, metadataEntry)
	}
	return metadataEntries, nil
}

func parseMetadataObject(index int, root map[string]any) (Metadata, error) {
	if err := ensureAllowedKeys(root, fmt.Sprintf("observation.metadata[%d]", index), "id", "key", "ownerId", "value", "valueKind"); err != nil {
		return Metadata{}, err
	}

	id, err := requiredStringField(root, "id")
	if err != nil {
		return Metadata{}, wrapIndexedFieldError("observation.metadata", index, err)
	}
	key, err := requiredStringField(root, "key")
	if err != nil {
		return Metadata{}, wrapIndexedFieldError("observation.metadata", index, err)
	}
	valueKind, err := requiredStringField(root, "valueKind")
	if err != nil {
		return Metadata{}, wrapIndexedFieldError("observation.metadata", index, err)
	}

	entry := Metadata{
		ID:        id,
		Key:       key,
		ValueKind: valueKind,
	}
	if rawOwnerID, ok := root["ownerId"]; ok {
		switch typed := rawOwnerID.(type) {
		case nil:
			entry.OwnerID = ""
		case string:
			if strings.TrimSpace(typed) == "" {
				return Metadata{}, &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].ownerId must not be blank", index)}
			}
			entry.OwnerID = typed
		default:
			return Metadata{}, &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].ownerId must be a string or null", index)}
		}
	}

	rawValue, ok := root["value"]
	if !ok {
		return Metadata{}, &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].value is required", index)}
	}
	rawBytes, err := json.Marshal(rawValue)
	if err != nil {
		return Metadata{}, &DecodeError{Err: err}
	}
	value, err := NewValue(rawBytes)
	if err != nil {
		return Metadata{}, &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].value %s", index, err.Error())}
	}
	entry.Value = value

	return entry, nil
}

func parseReferencesField(root map[string]any, field string) ([]Reference, error) {
	value, ok := root[field]
	if !ok {
		return make([]Reference, 0), nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("observation.%s must be an array", field)}
	}
	references := make([]Reference, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("observation.references[%d] must be an object", index)}
		}
		if err := ensureAllowedKeys(object, fmt.Sprintf("observation.references[%d]", index), "kind", "name"); err != nil {
			return nil, err
		}
		kind, err := requiredStringField(object, "kind")
		if err != nil {
			return nil, wrapIndexedFieldError("observation.references", index, err)
		}
		name, err := requiredStringField(object, "name")
		if err != nil {
			return nil, wrapIndexedFieldError("observation.references", index, err)
		}
		references = append(references, Reference{Kind: kind, Name: name})
	}
	return references, nil
}

func parseComponentsField(root map[string]any, field string) ([]Component, error) {
	value, ok := root[field]
	if !ok {
		return make([]Component, 0), nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("observation.%s must be an array", field)}
	}
	components := make([]Component, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("observation.components[%d] must be an object", index)}
		}
		component, err := parseComponentObject(index, object)
		if err != nil {
			return nil, err
		}
		components = append(components, component)
	}
	return components, nil
}

func parseComponentObject(index int, root map[string]any) (Component, error) {
	if err := ensureAllowedKeys(root, fmt.Sprintf("observation.components[%d]", index), "id", "kind", "name", "parentId"); err != nil {
		return Component{}, err
	}

	id, err := requiredStringField(root, "id")
	if err != nil {
		return Component{}, wrapIndexedFieldError("observation.components", index, err)
	}
	kind, err := requiredStringField(root, "kind")
	if err != nil {
		return Component{}, wrapIndexedFieldError("observation.components", index, err)
	}
	name, err := requiredStringField(root, "name")
	if err != nil {
		return Component{}, wrapIndexedFieldError("observation.components", index, err)
	}

	component := Component{
		ID:   id,
		Kind: kind,
		Name: name,
	}
	if rawParentID, ok := root["parentId"]; ok {
		switch typed := rawParentID.(type) {
		case nil:
			component.ParentID = ""
		case string:
			component.ParentID = typed
		default:
			return Component{}, &ValidationError{Message: fmt.Sprintf("observation.components[%d].parentId must be a string or null", index)}
		}
	}

	return component, nil
}

func validateObserved(observed *Observed) error {
	if observed == nil {
		return &ValidationError{Message: "observed must not be nil"}
	}
	if observed.SchemaVersion != SchemaVersion {
		return &ValidationError{Message: fmt.Sprintf("schemaVersion must be %q", SchemaVersion)}
	}
	if strings.TrimSpace(observed.WorkingCopy.Path) == "" {
		return &ValidationError{Message: "workingCopy.path is required"}
	}
	if !filepath.IsAbs(observed.WorkingCopy.Path) {
		return &ValidationError{Message: "workingCopy.path must be an absolute path"}
	}
	if !sha256Pattern.MatchString(observed.WorkingCopy.SHA256) {
		return &ValidationError{Message: "workingCopy.sha256 must be a lowercase 64-character hex string"}
	}
	if observed.Observation.Parameters == nil {
		return &ValidationError{Message: "observation.parameters is required"}
	}
	if observed.Observation.Metadata == nil {
		return &ValidationError{Message: "observation.metadata is required"}
	}
	if observed.Observation.References == nil {
		return &ValidationError{Message: "observation.references is required"}
	}
	if observed.Observation.Components == nil {
		return &ValidationError{Message: "observation.components is required"}
	}
	if targetState := observed.Observation.TargetState; targetState != nil {
		if targetState.Suppression == nil || targetState.Visibility == nil || targetState.Existence == nil {
			return &ValidationError{Message: "observation.targetState collections are required"}
		}
		if err := validateBooleanTargetEvidence("observation.targetState.suppression", targetState.Suppression); err != nil {
			return err
		}
		if err := validateBooleanTargetEvidence("observation.targetState.visibility", targetState.Visibility); err != nil {
			return err
		}
		if err := validateExistenceTargetEvidence(targetState.Existence); err != nil {
			return err
		}
	}
	for index, parameter := range observed.Observation.Parameters {
		if strings.TrimSpace(parameter.ID) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].id is required", index)}
		}
		if strings.TrimSpace(parameter.Name) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].name is required", index)}
		}
		if parameter.GroupID != "" && strings.TrimSpace(parameter.GroupID) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].groupId must not be blank", index)}
		}
		if !isSupportedParameterValueKind(parameter.ValueKind) {
			return &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].valueKind must be one of %q, %q, %q, or %q", index, "number", "integer", "string", "boolean")}
		}
		if !parameter.Value.set {
			return &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].value is required", index)}
		}
		canonical, err := canonicalizeNonNullScalarJSON(parameter.Value.raw)
		if err != nil {
			return &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].value %s", index, err.Error())}
		}
		if !bytes.Equal(canonical, parameter.Value.raw) {
			return &ValidationError{Message: fmt.Sprintf("observation.parameters[%d].value must already be canonical", index)}
		}
	}
	for index, entry := range observed.Observation.Metadata {
		if strings.TrimSpace(entry.ID) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].id is required", index)}
		}
		if strings.TrimSpace(entry.Key) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].key is required", index)}
		}
		if entry.OwnerID != "" && strings.TrimSpace(entry.OwnerID) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].ownerId must not be blank", index)}
		}
		if !isSupportedMetadataValueKind(entry.ValueKind) {
			return &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].valueKind must be one of %q, %q, %q, or %q", index, "number", "integer", "string", "boolean")}
		}
		if !entry.Value.set {
			return &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].value is required", index)}
		}
		canonical, err := canonicalizeNonNullScalarJSON(entry.Value.raw)
		if err != nil {
			return &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].value %s", index, err.Error())}
		}
		if !bytes.Equal(canonical, entry.Value.raw) {
			return &ValidationError{Message: fmt.Sprintf("observation.metadata[%d].value must already be canonical", index)}
		}
	}
	for index, reference := range observed.Observation.References {
		if strings.TrimSpace(reference.Kind) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.references[%d].kind is required", index)}
		}
		if strings.TrimSpace(reference.Name) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.references[%d].name is required", index)}
		}
	}
	seenComponentIDs := make(map[string]int, len(observed.Observation.Components))
	for index, component := range observed.Observation.Components {
		if strings.TrimSpace(component.ID) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.components[%d].id is required", index)}
		}
		if !isSupportedComponentKind(component.Kind) {
			return &ValidationError{Message: fmt.Sprintf("observation.components[%d].kind must be one of %q or %q", index, ComponentKindAssembly, ComponentKindPart)}
		}
		if strings.TrimSpace(component.Name) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.components[%d].name is required", index)}
		}
		if component.ParentID != "" && strings.TrimSpace(component.ParentID) == "" {
			return &ValidationError{Message: fmt.Sprintf("observation.components[%d].parentId must not be blank", index)}
		}
		if firstIndex, exists := seenComponentIDs[component.ID]; exists {
			return &ValidationError{Message: fmt.Sprintf("observation.components[%d].id duplicates observation.components[%d].id %q", index, firstIndex, component.ID)}
		}
		seenComponentIDs[component.ID] = index
	}
	for index, component := range observed.Observation.Components {
		if component.ParentID == "" {
			continue
		}
		if _, exists := seenComponentIDs[component.ParentID]; !exists {
			return &ValidationError{Message: fmt.Sprintf("observation.components[%d].parentId references unknown component %q", index, component.ParentID)}
		}
	}
	return nil
}

func validateTargetIdentity(location, destination, object string) error {
	if destination != TargetDestinationAssembly && destination != TargetDestinationPart {
		return &ValidationError{Message: fmt.Sprintf("%s.destination must be one of %q or %q", location, TargetDestinationAssembly, TargetDestinationPart)}
	}
	if strings.TrimSpace(object) == "" || strings.TrimSpace(object) != object || strings.ContainsRune(object, '\x00') {
		return &ValidationError{Message: location + ".object must be a non-blank exact native object name"}
	}
	return nil
}

func validateBooleanTargetEvidence(location string, entries []BooleanTargetEvidence) error {
	seen := make(map[string]int, len(entries))
	for index, entry := range entries {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		if err := validateTargetIdentity(itemLocation, entry.Destination, entry.Object); err != nil {
			return err
		}
		switch entry.Status {
		case BooleanEvidenceStatusObserved:
			if entry.Value == nil {
				return &ValidationError{Message: itemLocation + ".value is required when status is observed"}
			}
		case BooleanEvidenceStatusTargetMissing, BooleanEvidenceStatusUnavailable:
			if entry.Value != nil {
				return &ValidationError{Message: itemLocation + ".value must be absent unless status is observed"}
			}
		default:
			return &ValidationError{Message: fmt.Sprintf("%s.status must be one of %q, %q, or %q", itemLocation, BooleanEvidenceStatusObserved, BooleanEvidenceStatusTargetMissing, BooleanEvidenceStatusUnavailable)}
		}
		key := entry.Destination + "\x00" + entry.Object
		if previous, ok := seen[key]; ok {
			return &ValidationError{Message: fmt.Sprintf("%s duplicates %s[%d]", itemLocation, location, previous)}
		}
		seen[key] = index
	}
	return nil
}

func validateExistenceTargetEvidence(entries []ExistenceTargetEvidence) error {
	seen := make(map[string]int, len(entries))
	for index, entry := range entries {
		location := fmt.Sprintf("observation.targetState.existence[%d]", index)
		if err := validateTargetIdentity(location, entry.Destination, entry.Object); err != nil {
			return err
		}
		if entry.Status != ExistenceEvidenceStatusExists && entry.Status != ExistenceEvidenceStatusAbsent && entry.Status != ExistenceEvidenceStatusUnavailable {
			return &ValidationError{Message: fmt.Sprintf("%s.status must be one of %q, %q, or %q", location, ExistenceEvidenceStatusExists, ExistenceEvidenceStatusAbsent, ExistenceEvidenceStatusUnavailable)}
		}
		key := entry.Destination + "\x00" + entry.Object
		if previous, ok := seen[key]; ok {
			return &ValidationError{Message: fmt.Sprintf("%s duplicates observation.targetState.existence[%d]", location, previous)}
		}
		seen[key] = index
	}
	return nil
}

func writeWorkingCopy(out *bytes.Buffer, workingCopy WorkingCopy) {
	out.WriteByte('{')
	writeJSONString(out, "path")
	out.WriteByte(':')
	writeJSONString(out, workingCopy.Path)
	out.WriteByte(',')
	writeJSONString(out, "sha256")
	out.WriteByte(':')
	writeJSONString(out, workingCopy.SHA256)
	out.WriteByte('}')
}

func writeObservation(out *bytes.Buffer, observation Observation) {
	out.WriteByte('{')
	writeJSONString(out, "parameters")
	out.WriteByte(':')
	writeParameters(out, observation.Parameters)
	out.WriteByte(',')
	writeJSONString(out, "metadata")
	out.WriteByte(':')
	writeMetadataEntries(out, observation.Metadata)
	out.WriteByte(',')
	writeJSONString(out, "references")
	out.WriteByte(':')
	writeReferences(out, observation.References)
	out.WriteByte(',')
	writeJSONString(out, "components")
	out.WriteByte(':')
	writeComponents(out, observation.Components)
	if observation.TargetState != nil {
		out.WriteByte(',')
		writeJSONString(out, "targetState")
		out.WriteByte(':')
		writeTargetStateObservation(out, *observation.TargetState)
	}
	out.WriteByte('}')
}

func writeTargetStateObservation(out *bytes.Buffer, observation TargetStateObservation) {
	out.WriteByte('{')
	writeJSONString(out, "suppression")
	out.WriteByte(':')
	writeBooleanTargetEvidence(out, observation.Suppression)
	out.WriteByte(',')
	writeJSONString(out, "visibility")
	out.WriteByte(':')
	writeBooleanTargetEvidence(out, observation.Visibility)
	out.WriteByte(',')
	writeJSONString(out, "existence")
	out.WriteByte(':')
	writeExistenceTargetEvidence(out, observation.Existence)
	out.WriteByte('}')
}

func writeBooleanTargetEvidence(out *bytes.Buffer, entries []BooleanTargetEvidence) {
	ordered := append([]BooleanTargetEvidence(nil), entries...)
	slices.SortFunc(ordered, compareBooleanTargetEvidence)
	out.WriteByte('[')
	for index, entry := range ordered {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(out, "destination")
		out.WriteByte(':')
		writeJSONString(out, entry.Destination)
		out.WriteByte(',')
		writeJSONString(out, "object")
		out.WriteByte(':')
		writeJSONString(out, entry.Object)
		out.WriteByte(',')
		writeJSONString(out, "status")
		out.WriteByte(':')
		writeJSONString(out, entry.Status)
		if entry.Value != nil {
			out.WriteByte(',')
			writeJSONString(out, "value")
			out.WriteByte(':')
			if *entry.Value {
				out.WriteString("true")
			} else {
				out.WriteString("false")
			}
		}
		out.WriteByte('}')
	}
	out.WriteByte(']')
}

func writeExistenceTargetEvidence(out *bytes.Buffer, entries []ExistenceTargetEvidence) {
	ordered := append([]ExistenceTargetEvidence(nil), entries...)
	slices.SortFunc(ordered, compareExistenceTargetEvidence)
	out.WriteByte('[')
	for index, entry := range ordered {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(out, "destination")
		out.WriteByte(':')
		writeJSONString(out, entry.Destination)
		out.WriteByte(',')
		writeJSONString(out, "object")
		out.WriteByte(':')
		writeJSONString(out, entry.Object)
		out.WriteByte(',')
		writeJSONString(out, "status")
		out.WriteByte(':')
		writeJSONString(out, entry.Status)
		out.WriteByte('}')
	}
	out.WriteByte(']')
}

func compareBooleanTargetEvidence(left, right BooleanTargetEvidence) int {
	if cmp := strings.Compare(left.Destination, right.Destination); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.Object, right.Object); cmp != 0 {
		return cmp
	}
	return strings.Compare(left.Status, right.Status)
}

func compareExistenceTargetEvidence(left, right ExistenceTargetEvidence) int {
	if cmp := strings.Compare(left.Destination, right.Destination); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.Object, right.Object); cmp != 0 {
		return cmp
	}
	return strings.Compare(left.Status, right.Status)
}

func writeParameters(out *bytes.Buffer, parameters []Parameter) {
	ordered := append([]Parameter(nil), parameters...)
	slices.SortFunc(ordered, compareParameters)

	out.WriteByte('[')
	for index, parameter := range ordered {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(out, "id")
		out.WriteByte(':')
		writeJSONString(out, parameter.ID)
		out.WriteByte(',')
		writeJSONString(out, "name")
		out.WriteByte(':')
		writeJSONString(out, parameter.Name)
		out.WriteByte(',')
		writeJSONString(out, "groupId")
		out.WriteByte(':')
		if parameter.GroupID == "" {
			out.WriteString("null")
		} else {
			writeJSONString(out, parameter.GroupID)
		}
		out.WriteByte(',')
		writeJSONString(out, "value")
		out.WriteByte(':')
		out.Write(parameter.Value.raw)
		out.WriteByte(',')
		writeJSONString(out, "valueKind")
		out.WriteByte(':')
		writeJSONString(out, parameter.ValueKind)
		out.WriteByte('}')
	}
	out.WriteByte(']')
}

func writeMetadataEntries(out *bytes.Buffer, entries []Metadata) {
	ordered := append([]Metadata(nil), entries...)
	slices.SortFunc(ordered, compareMetadata)

	out.WriteByte('[')
	for index, entry := range ordered {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(out, "id")
		out.WriteByte(':')
		writeJSONString(out, entry.ID)
		out.WriteByte(',')
		writeJSONString(out, "key")
		out.WriteByte(':')
		writeJSONString(out, entry.Key)
		out.WriteByte(',')
		writeJSONString(out, "ownerId")
		out.WriteByte(':')
		if entry.OwnerID == "" {
			out.WriteString("null")
		} else {
			writeJSONString(out, entry.OwnerID)
		}
		out.WriteByte(',')
		writeJSONString(out, "value")
		out.WriteByte(':')
		out.Write(entry.Value.raw)
		out.WriteByte(',')
		writeJSONString(out, "valueKind")
		out.WriteByte(':')
		writeJSONString(out, entry.ValueKind)
		out.WriteByte('}')
	}
	out.WriteByte(']')
}

func writeReferences(out *bytes.Buffer, references []Reference) {
	out.WriteByte('[')
	for index, reference := range references {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(out, "kind")
		out.WriteByte(':')
		writeJSONString(out, reference.Kind)
		out.WriteByte(',')
		writeJSONString(out, "name")
		out.WriteByte(':')
		writeJSONString(out, reference.Name)
		out.WriteByte('}')
	}
	out.WriteByte(']')
}

func writeComponents(out *bytes.Buffer, components []Component) {
	ordered := append([]Component(nil), components...)
	slices.SortFunc(ordered, compareComponents)

	out.WriteByte('[')
	for index, component := range ordered {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(out, "id")
		out.WriteByte(':')
		writeJSONString(out, component.ID)
		out.WriteByte(',')
		writeJSONString(out, "kind")
		out.WriteByte(':')
		writeJSONString(out, component.Kind)
		out.WriteByte(',')
		writeJSONString(out, "name")
		out.WriteByte(':')
		writeJSONString(out, component.Name)
		out.WriteByte(',')
		writeJSONString(out, "parentId")
		out.WriteByte(':')
		if component.ParentID == "" {
			out.WriteString("null")
		} else {
			writeJSONString(out, component.ParentID)
		}
		out.WriteByte('}')
	}
	out.WriteByte(']')
}

func compareComponents(left, right Component) int {
	if cmp := strings.Compare(left.ID, right.ID); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.Kind, right.Kind); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.Name, right.Name); cmp != 0 {
		return cmp
	}
	return strings.Compare(left.ParentID, right.ParentID)
}

func compareParameters(left, right Parameter) int {
	if cmp := strings.Compare(left.ID, right.ID); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.Name, right.Name); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.GroupID, right.GroupID); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.ValueKind, right.ValueKind); cmp != 0 {
		return cmp
	}
	return bytes.Compare(left.Value.raw, right.Value.raw)
}

func compareMetadata(left, right Metadata) int {
	if cmp := strings.Compare(left.ID, right.ID); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.Key, right.Key); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.OwnerID, right.OwnerID); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.ValueKind, right.ValueKind); cmp != 0 {
		return cmp
	}
	return bytes.Compare(left.Value.raw, right.Value.raw)
}

func isSupportedComponentKind(kind string) bool {
	switch kind {
	case ComponentKindAssembly, ComponentKindPart:
		return true
	default:
		return false
	}
}

func isSupportedParameterValueKind(kind string) bool {
	switch kind {
	case "number", "integer", "string", "boolean":
		return true
	default:
		return false
	}
}

func isSupportedMetadataValueKind(kind string) bool {
	switch kind {
	case "number", "integer", "string", "boolean":
		return true
	default:
		return false
	}
}

func canonicalizeScalarJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("must be valid JSON scalar: %w", err)
	}
	if err := ensureNoTrailingJSON(decoder); err != nil {
		return nil, fmt.Errorf("must be valid JSON scalar: %w", err)
	}
	if !isScalar(value) {
		return nil, errors.New("must be a JSON scalar")
	}

	var out bytes.Buffer
	if err := writeCanonicalScalar(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func canonicalizeNonNullScalarJSON(data []byte) ([]byte, error) {
	canonical, err := canonicalizeScalarJSON(data)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(canonical, []byte("null")) {
		return nil, errors.New("must not be null")
	}
	return canonical, nil
}

func isScalar(value any) bool {
	switch value.(type) {
	case nil, bool, string, json.Number:
		return true
	default:
		return false
	}
}

func writeCanonicalScalar(out *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if typed {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		writeJSONString(out, typed)
	case json.Number:
		out.WriteString(typed.String())
	default:
		return fmt.Errorf("unsupported scalar type %T", value)
	}
	return nil
}

func ensureNoTrailingJSON(decoder *json.Decoder) error {
	if decoder == nil {
		return nil
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func ensureAllowedKeys(object map[string]any, location string, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}

	var unknown []string
	for key := range object {
		if _, ok := allowedSet[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	slices.Sort(unknown)
	if len(unknown) == 0 {
		return nil
	}
	return &ValidationError{Message: fmt.Sprintf("%s has unknown field %q", location, unknown[0])}
}

func requiredStringField(root map[string]any, field string) (string, error) {
	value, ok := root[field]
	if !ok {
		return "", &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", &ValidationError{Message: fmt.Sprintf("%s must be a string", field)}
	}
	if strings.TrimSpace(stringValue) == "" {
		return "", &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	return stringValue, nil
}

func wrapFieldError(prefix string, err error) error {
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	if validationErr == nil || validationErr.Message == "" {
		return err
	}
	return &ValidationError{Message: prefix + "." + validationErr.Message}
}

func wrapIndexedFieldError(prefix string, index int, err error) error {
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	if validationErr == nil || validationErr.Message == "" {
		return err
	}
	return &ValidationError{Message: fmt.Sprintf("%s[%d].%s", prefix, index, validationErr.Message)}
}

func writeJSONString(out *bytes.Buffer, value string) {
	encoded, _ := json.Marshal(value)
	out.Write(encoded)
}
