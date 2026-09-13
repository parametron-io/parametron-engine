package cad

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

const SchemaVersion = "1.0"

type CADContract struct {
	SchemaVersion  string         `json:"schemaVersion"`
	CaptureID      string         `json:"captureId"`
	Adapter        NamedVersion   `json:"adapter"`
	CADSystem      NamedVersion   `json:"cadSystem"`
	SourceDocument SourceDocument `json:"sourceDocument"`
	RootProduct    RootProduct    `json:"rootProduct"`
	Annotations    Annotations    `json:"annotations"`
	Entities       Entities       `json:"entities"`
	Structure      Structure      `json:"structure"`
}

type NamedVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type SourceDocument struct {
	LogicalID   string `json:"logicalId"`
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
}

type RootProduct struct {
	ID string `json:"id"`
}

type Annotations struct {
	Description string `json:"description"`
	Comment     string `json:"comment"`
	Purpose     string `json:"purpose"`
}

type Entities struct {
	Components      []Component      `json:"components"`
	Features        []Feature        `json:"features"`
	Relationships   []Relationship   `json:"relationships"`
	ParameterGroups []ParameterGroup `json:"parameterGroups"`
	Parameters      []Parameter      `json:"parameters"`
	Metadata        []Metadata       `json:"metadata"`
}

type IdentitySource struct {
	Kind           string `json:"kind"`
	Path           string `json:"path,omitempty"`
	OwnerScopedKey string `json:"ownerScopedKey,omitempty"`
	GroupScopedKey string `json:"groupScopedKey,omitempty"`
	NativeRef      string `json:"nativeRef,omitempty"`
}

type Targetability struct {
	Suppress   bool `json:"suppress"`
	Unsuppress bool `json:"unsuppress"`
	Hide       bool `json:"hide"`
	Unhide     bool `json:"unhide"`
	Delete     bool `json:"delete"`
}

type SourceFile struct {
	Path string `json:"path"`
}

type Component struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Name           string          `json:"name"`
	DisplayName    string          `json:"displayName"`
	CADType        string          `json:"cadType"`
	Quantity       int             `json:"quantity"`
	Material       string          `json:"material"`
	IdentitySource *IdentitySource `json:"identitySource,omitempty"`
	StabilityClass string          `json:"stabilityClass,omitempty"`
	SourceFile     *SourceFile     `json:"sourceFile,omitempty"`
	Targetability  Targetability   `json:"targetability"`
	Annotations    Annotations     `json:"annotations"`
}

type Feature struct {
	ID             string          `json:"id"`
	ComponentID    string          `json:"componentId"`
	Name           string          `json:"name"`
	DisplayName    string          `json:"displayName"`
	CADType        string          `json:"cadType"`
	IdentitySource *IdentitySource `json:"identitySource,omitempty"`
	StabilityClass string          `json:"stabilityClass,omitempty"`
	Targetability  Targetability   `json:"targetability"`
	Annotations    Annotations     `json:"annotations"`
}

type Relationship struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	ComponentID    string          `json:"componentId"`
	Name           string          `json:"name"`
	CADType        string          `json:"cadType"`
	Endpoints      []string        `json:"endpoints"`
	IdentitySource *IdentitySource `json:"identitySource,omitempty"`
	StabilityClass string          `json:"stabilityClass,omitempty"`
	Targetability  Targetability   `json:"targetability"`
	Annotations    Annotations     `json:"annotations"`
}

type ParameterGroup struct {
	ID               string          `json:"id"`
	OwnerComponentID string          `json:"ownerComponentId"`
	Name             string          `json:"name"`
	DisplayName      string          `json:"displayName"`
	GroupKind        string          `json:"groupKind"`
	CADType          string          `json:"cadType"`
	Observable       bool            `json:"observable"`
	Writable         bool            `json:"writable"`
	IdentitySource   *IdentitySource `json:"identitySource,omitempty"`
	StabilityClass   string          `json:"stabilityClass,omitempty"`
	Annotations      Annotations     `json:"annotations"`
}

type Parameter struct {
	ID             string          `json:"id"`
	OwnerKind      string          `json:"ownerKind"`
	OwnerID        string          `json:"ownerId"`
	ComponentID    string          `json:"componentId"`
	Name           string          `json:"name"`
	DisplayName    string          `json:"displayName"`
	CADType        string          `json:"cadType"`
	ValueType      string          `json:"valueType"`
	Observable     bool            `json:"observable"`
	Writable       bool            `json:"writable"`
	CurrentValue   ScalarValue     `json:"currentValue,omitempty"`
	Unit           string          `json:"unit,omitempty"`
	IdentitySource *IdentitySource `json:"identitySource,omitempty"`
	StabilityClass string          `json:"stabilityClass,omitempty"`
	Annotations    Annotations     `json:"annotations"`
}

type Metadata struct {
	ID             string          `json:"id"`
	OwnerKind      string          `json:"ownerKind"`
	OwnerID        string          `json:"ownerId"`
	ComponentID    string          `json:"componentId"`
	Key            string          `json:"key"`
	DisplayName    string          `json:"displayName"`
	CADType        string          `json:"cadType"`
	ValueType      string          `json:"valueType"`
	Observable     bool            `json:"observable"`
	Writable       bool            `json:"writable"`
	CurrentValue   ScalarValue     `json:"currentValue,omitempty"`
	IdentitySource *IdentitySource `json:"identitySource,omitempty"`
	StabilityClass string          `json:"stabilityClass,omitempty"`
	Annotations    Annotations     `json:"annotations"`
}

type Structure struct {
	RootComponentID string          `json:"rootComponentId"`
	Nodes           []StructureNode `json:"nodes"`
}

type StructureNode struct {
	ComponentID       string   `json:"componentId"`
	ParentComponentID string   `json:"parentComponentId"`
	Children          []string `json:"children"`
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

func Parse(data []byte) (*CADContract, error) {
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
		return nil, &ValidationError{Problems: []string{"root must be a JSON object"}}
	}

	contract, err := parseContract(root)
	if err != nil {
		return nil, err
	}
	if err := Validate(contract); err != nil {
		return nil, err
	}
	return contract, nil
}

func Read(r io.Reader) (*CADContract, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: reader must not be nil", ErrIO)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIO, err)
	}
	return Parse(data)
}

func Load(path string) (*CADContract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{Path: path, Err: err}
	}
	return Parse(data)
}

func parseContract(root map[string]any) (*CADContract, error) {
	var problems []string

	problems = append(problems, ensureAllowedKeys(root, "root",
		"schemaVersion", "captureId", "adapter", "cadSystem", "sourceDocument", "rootProduct", "annotations", "entities", "structure")...)

	schemaVersion, errs := requiredStringField(root, "schemaVersion")
	problems = append(problems, errs...)
	if len(errs) == 0 && schemaVersion != SchemaVersion {
		problems = append(problems, fmt.Sprintf("schemaVersion must be %q", SchemaVersion))
	}

	captureID, errs := requiredStringField(root, "captureId")
	problems = append(problems, errs...)

	adapter, errs := parseNamedVersionField(root, "adapter")
	problems = append(problems, errs...)

	cadSystem, errs := parseNamedVersionField(root, "cadSystem")
	problems = append(problems, errs...)

	sourceDocument, errs := parseSourceDocumentField(root, "sourceDocument")
	problems = append(problems, errs...)

	rootProduct, errs := parseRootProductField(root, "rootProduct")
	problems = append(problems, errs...)

	annotations, errs := parseAnnotationsField(root, "annotations")
	problems = append(problems, errs...)

	entities, errs := parseEntitiesField(root, "entities")
	problems = append(problems, errs...)

	structure, errs := parseStructureField(root, "structure")
	problems = append(problems, errs...)

	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}

	return &CADContract{
		SchemaVersion:  schemaVersion,
		CaptureID:      captureID,
		Adapter:        adapter,
		CADSystem:      cadSystem,
		SourceDocument: sourceDocument,
		RootProduct:    rootProduct,
		Annotations:    annotations,
		Entities:       entities,
		Structure:      structure,
	}, nil
}

func parseNamedVersionField(root map[string]any, field string) (NamedVersion, []string) {
	object, errs := requiredObjectField(root, field)
	if object == nil {
		return NamedVersion{}, errs
	}
	errs = append(errs, ensureAllowedKeys(object, field, "name", "version")...)
	name, more := requiredStringField(object, "name")
	errs = append(errs, prefixProblems(field, more)...)
	version, more := requiredStringAllowEmpty(object, "version")
	errs = append(errs, prefixProblems(field, more)...)
	return NamedVersion{Name: name, Version: version}, errs
}

func parseSourceDocumentField(root map[string]any, field string) (SourceDocument, []string) {
	object, errs := requiredObjectField(root, field)
	if object == nil {
		return SourceDocument{}, errs
	}
	errs = append(errs, ensureAllowedKeys(object, field, "logicalId", "path", "fingerprint")...)
	logicalID, more := requiredStringField(object, "logicalId")
	errs = append(errs, prefixProblems(field, more)...)
	pathValue, more := requiredStringField(object, "path")
	errs = append(errs, prefixProblems(field, more)...)
	fingerprint, more := requiredStringField(object, "fingerprint")
	errs = append(errs, prefixProblems(field, more)...)
	return SourceDocument{
		LogicalID:   logicalID,
		Path:        pathValue,
		Fingerprint: fingerprint,
	}, errs
}

func parseRootProductField(root map[string]any, field string) (RootProduct, []string) {
	object, errs := requiredObjectField(root, field)
	if object == nil {
		return RootProduct{}, errs
	}
	errs = append(errs, ensureAllowedKeys(object, field, "id")...)
	id, more := requiredStringField(object, "id")
	errs = append(errs, prefixProblems(field, more)...)
	return RootProduct{ID: id}, errs
}

func parseAnnotationsField(root map[string]any, field string) (Annotations, []string) {
	object, errs := requiredObjectField(root, field)
	if object == nil {
		return Annotations{}, errs
	}
	annotations, more := parseAnnotationsObject(object, field)
	errs = append(errs, more...)
	return annotations, errs
}

func parseAnnotationsObject(root map[string]any, location string) (Annotations, []string) {
	var errs []string
	errs = append(errs, ensureAllowedKeys(root, location, "description", "comment", "purpose")...)
	description, more := requiredStringAllowEmpty(root, "description")
	errs = append(errs, prefixProblems(location, more)...)
	comment, more := requiredStringAllowEmpty(root, "comment")
	errs = append(errs, prefixProblems(location, more)...)
	purpose, more := requiredStringAllowEmpty(root, "purpose")
	errs = append(errs, prefixProblems(location, more)...)
	return Annotations{Description: description, Comment: comment, Purpose: purpose}, errs
}

func parseEntitiesField(root map[string]any, field string) (Entities, []string) {
	object, errs := requiredObjectField(root, field)
	if object == nil {
		return Entities{}, errs
	}
	errs = append(errs, ensureAllowedKeys(object, field,
		"components", "features", "relationships", "parameterGroups", "parameters", "metadata")...)
	components, more := parseComponentsField(object, field+".components")
	errs = append(errs, more...)
	features, more := parseFeaturesField(object, field+".features")
	errs = append(errs, more...)
	relationships, more := parseRelationshipsField(object, field+".relationships")
	errs = append(errs, more...)
	parameterGroups, more := parseParameterGroupsField(object, field+".parameterGroups")
	errs = append(errs, more...)
	parameters, more := parseParametersField(object, field+".parameters")
	errs = append(errs, more...)
	metadata, more := parseMetadataField(object, field+".metadata")
	errs = append(errs, more...)
	return Entities{
		Components:      components,
		Features:        features,
		Relationships:   relationships,
		ParameterGroups: parameterGroups,
		Parameters:      parameters,
		Metadata:        metadata,
	}, errs
}

func parseStructureField(root map[string]any, field string) (Structure, []string) {
	object, errs := requiredObjectField(root, field)
	if object == nil {
		return Structure{}, errs
	}
	errs = append(errs, ensureAllowedKeys(object, field, "rootComponentId", "nodes")...)
	rootComponentID, more := requiredStringField(object, "rootComponentId")
	errs = append(errs, prefixProblems(field, more)...)

	items, more := requiredArrayField(object, "nodes")
	errs = append(errs, prefixProblems(field, more)...)
	nodes := make([]StructureNode, 0, len(items))
	for index, item := range items {
		location := fmt.Sprintf("%s.nodes[%d]", field, index)
		nodeObject, ok := item.(map[string]any)
		if !ok {
			errs = append(errs, location+" must be an object")
			continue
		}
		node, nodeErrs := parseStructureNode(nodeObject, location)
		errs = append(errs, nodeErrs...)
		nodes = append(nodes, node)
	}

	return Structure{RootComponentID: rootComponentID, Nodes: nodes}, errs
}

func parseStructureNode(root map[string]any, location string) (StructureNode, []string) {
	var errs []string
	errs = append(errs, ensureAllowedKeys(root, location, "componentId", "parentComponentId", "children")...)
	componentID, more := requiredStringField(root, "componentId")
	errs = append(errs, prefixProblems(location, more)...)
	parentComponentID, more := requiredStringAllowEmpty(root, "parentComponentId")
	errs = append(errs, prefixProblems(location, more)...)
	childItems, more := requiredArrayField(root, "children")
	errs = append(errs, prefixProblems(location, more)...)
	children := make([]string, 0, len(childItems))
	for index, item := range childItems {
		text, ok := item.(string)
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.children[%d] must be a string", location, index))
			continue
		}
		if strings.TrimSpace(text) == "" {
			errs = append(errs, fmt.Sprintf("%s.children[%d] is required", location, index))
			continue
		}
		children = append(children, text)
	}
	return StructureNode{ComponentID: componentID, ParentComponentID: parentComponentID, Children: children}, errs
}

func parseComponentsField(root map[string]any, location string) ([]Component, []string) {
	items, errs := requiredArrayField(root, "components")
	errs = prefixProblems("entities", errs)
	components := make([]Component, 0, len(items))
	for index, item := range items {
		entryLocation := fmt.Sprintf("entities.components[%d]", index)
		object, ok := item.(map[string]any)
		if !ok {
			errs = append(errs, entryLocation+" must be an object")
			continue
		}
		component, more := parseComponent(object, entryLocation)
		errs = append(errs, more...)
		components = append(components, component)
	}
	return components, errs
}

func parseComponent(root map[string]any, location string) (Component, []string) {
	var errs []string
	errs = append(errs, ensureAllowedKeys(root, location,
		"id", "kind", "name", "displayName", "cadType", "quantity", "material", "identitySource", "stabilityClass", "sourceFile", "targetability", "annotations")...)
	id, more := requiredStringField(root, "id")
	errs = append(errs, prefixProblems(location, more)...)
	kind, more := requiredStringField(root, "kind")
	errs = append(errs, prefixProblems(location, more)...)
	name, more := requiredStringField(root, "name")
	errs = append(errs, prefixProblems(location, more)...)
	displayName, more := requiredStringAllowEmpty(root, "displayName")
	errs = append(errs, prefixProblems(location, more)...)
	cadType, more := requiredStringField(root, "cadType")
	errs = append(errs, prefixProblems(location, more)...)
	quantity, more := requiredIntField(root, "quantity")
	errs = append(errs, prefixProblems(location, more)...)
	material, more := requiredStringAllowEmpty(root, "material")
	errs = append(errs, prefixProblems(location, more)...)
	identitySource, more := parseOptionalIdentitySource(root, location)
	errs = append(errs, more...)
	stabilityClass, more := optionalStringField(root, "stabilityClass")
	errs = append(errs, prefixProblems(location, more)...)
	sourceFile, more := parseOptionalSourceFile(root, location)
	errs = append(errs, more...)
	targetability, more := parseTargetabilityField(root, location)
	errs = append(errs, more...)
	annotations, more := parseAnnotationsField(root, "annotations")
	errs = append(errs, prefixProblems(location, trimPrefixOnProblems("annotations.", more))...)
	return Component{
		ID:             id,
		Kind:           kind,
		Name:           name,
		DisplayName:    displayName,
		CADType:        cadType,
		Quantity:       quantity,
		Material:       material,
		IdentitySource: identitySource,
		StabilityClass: stabilityClass,
		SourceFile:     sourceFile,
		Targetability:  targetability,
		Annotations:    annotations,
	}, errs
}

func parseFeaturesField(root map[string]any, location string) ([]Feature, []string) {
	items, errs := requiredArrayField(root, "features")
	errs = prefixProblems("entities", errs)
	features := make([]Feature, 0, len(items))
	for index, item := range items {
		entryLocation := fmt.Sprintf("entities.features[%d]", index)
		object, ok := item.(map[string]any)
		if !ok {
			errs = append(errs, entryLocation+" must be an object")
			continue
		}
		feature, more := parseFeature(object, entryLocation)
		errs = append(errs, more...)
		features = append(features, feature)
	}
	return features, errs
}

func parseFeature(root map[string]any, location string) (Feature, []string) {
	var errs []string
	errs = append(errs, ensureAllowedKeys(root, location,
		"id", "componentId", "name", "displayName", "cadType", "identitySource", "stabilityClass", "targetability", "annotations")...)
	id, more := requiredStringField(root, "id")
	errs = append(errs, prefixProblems(location, more)...)
	componentID, more := requiredStringField(root, "componentId")
	errs = append(errs, prefixProblems(location, more)...)
	name, more := requiredStringField(root, "name")
	errs = append(errs, prefixProblems(location, more)...)
	displayName, more := requiredStringAllowEmpty(root, "displayName")
	errs = append(errs, prefixProblems(location, more)...)
	cadType, more := requiredStringField(root, "cadType")
	errs = append(errs, prefixProblems(location, more)...)
	identitySource, more := parseOptionalIdentitySource(root, location)
	errs = append(errs, more...)
	stabilityClass, more := optionalStringField(root, "stabilityClass")
	errs = append(errs, prefixProblems(location, more)...)
	targetability, more := parseTargetabilityField(root, location)
	errs = append(errs, more...)
	annotations, more := parseAnnotationsField(root, "annotations")
	errs = append(errs, prefixProblems(location, trimPrefixOnProblems("annotations.", more))...)
	return Feature{
		ID:             id,
		ComponentID:    componentID,
		Name:           name,
		DisplayName:    displayName,
		CADType:        cadType,
		IdentitySource: identitySource,
		StabilityClass: stabilityClass,
		Targetability:  targetability,
		Annotations:    annotations,
	}, errs
}

func parseRelationshipsField(root map[string]any, location string) ([]Relationship, []string) {
	items, errs := requiredArrayField(root, "relationships")
	errs = prefixProblems("entities", errs)
	out := make([]Relationship, 0, len(items))
	for index, item := range items {
		entryLocation := fmt.Sprintf("entities.relationships[%d]", index)
		object, ok := item.(map[string]any)
		if !ok {
			errs = append(errs, entryLocation+" must be an object")
			continue
		}
		relationship, more := parseRelationship(object, entryLocation)
		errs = append(errs, more...)
		out = append(out, relationship)
	}
	return out, errs
}

func parseRelationship(root map[string]any, location string) (Relationship, []string) {
	var errs []string
	errs = append(errs, ensureAllowedKeys(root, location,
		"id", "kind", "componentId", "name", "cadType", "endpoints", "identitySource", "stabilityClass", "targetability", "annotations")...)
	id, more := requiredStringField(root, "id")
	errs = append(errs, prefixProblems(location, more)...)
	kind, more := requiredStringField(root, "kind")
	errs = append(errs, prefixProblems(location, more)...)
	componentID, more := requiredStringField(root, "componentId")
	errs = append(errs, prefixProblems(location, more)...)
	name, more := requiredStringField(root, "name")
	errs = append(errs, prefixProblems(location, more)...)
	cadType, more := requiredStringField(root, "cadType")
	errs = append(errs, prefixProblems(location, more)...)
	endpointItems, more := requiredArrayField(root, "endpoints")
	errs = append(errs, prefixProblems(location, more)...)
	endpoints := make([]string, 0, len(endpointItems))
	for index, item := range endpointItems {
		value, ok := item.(string)
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.endpoints[%d] must be a string", location, index))
			continue
		}
		if strings.TrimSpace(value) == "" {
			errs = append(errs, fmt.Sprintf("%s.endpoints[%d] is required", location, index))
			continue
		}
		endpoints = append(endpoints, value)
	}
	identitySource, more := parseOptionalIdentitySource(root, location)
	errs = append(errs, more...)
	stabilityClass, more := optionalStringField(root, "stabilityClass")
	errs = append(errs, prefixProblems(location, more)...)
	targetability, more := parseTargetabilityField(root, location)
	errs = append(errs, more...)
	annotations, more := parseAnnotationsField(root, "annotations")
	errs = append(errs, prefixProblems(location, trimPrefixOnProblems("annotations.", more))...)
	return Relationship{
		ID:             id,
		Kind:           kind,
		ComponentID:    componentID,
		Name:           name,
		CADType:        cadType,
		Endpoints:      endpoints,
		IdentitySource: identitySource,
		StabilityClass: stabilityClass,
		Targetability:  targetability,
		Annotations:    annotations,
	}, errs
}

func parseParameterGroupsField(root map[string]any, location string) ([]ParameterGroup, []string) {
	items, errs := requiredArrayField(root, "parameterGroups")
	errs = prefixProblems("entities", errs)
	out := make([]ParameterGroup, 0, len(items))
	for index, item := range items {
		entryLocation := fmt.Sprintf("entities.parameterGroups[%d]", index)
		object, ok := item.(map[string]any)
		if !ok {
			errs = append(errs, entryLocation+" must be an object")
			continue
		}
		group, more := parseParameterGroup(object, entryLocation)
		errs = append(errs, more...)
		out = append(out, group)
	}
	return out, errs
}

func parseParameterGroup(root map[string]any, location string) (ParameterGroup, []string) {
	var errs []string
	errs = append(errs, ensureAllowedKeys(root, location,
		"id", "ownerComponentId", "name", "displayName", "groupKind", "cadType", "observable", "writable", "identitySource", "stabilityClass", "annotations")...)
	id, more := requiredStringField(root, "id")
	errs = append(errs, prefixProblems(location, more)...)
	ownerComponentID, more := requiredStringField(root, "ownerComponentId")
	errs = append(errs, prefixProblems(location, more)...)
	name, more := requiredStringField(root, "name")
	errs = append(errs, prefixProblems(location, more)...)
	displayName, more := requiredStringAllowEmpty(root, "displayName")
	errs = append(errs, prefixProblems(location, more)...)
	groupKind, more := requiredStringField(root, "groupKind")
	errs = append(errs, prefixProblems(location, more)...)
	cadType, more := requiredStringField(root, "cadType")
	errs = append(errs, prefixProblems(location, more)...)
	observable, more := requiredBoolField(root, "observable")
	errs = append(errs, prefixProblems(location, more)...)
	writable, more := requiredBoolField(root, "writable")
	errs = append(errs, prefixProblems(location, more)...)
	identitySource, more := parseOptionalIdentitySource(root, location)
	errs = append(errs, more...)
	stabilityClass, more := optionalStringField(root, "stabilityClass")
	errs = append(errs, prefixProblems(location, more)...)
	annotations, more := parseAnnotationsField(root, "annotations")
	errs = append(errs, prefixProblems(location, trimPrefixOnProblems("annotations.", more))...)
	return ParameterGroup{
		ID:               id,
		OwnerComponentID: ownerComponentID,
		Name:             name,
		DisplayName:      displayName,
		GroupKind:        groupKind,
		CADType:          cadType,
		Observable:       observable,
		Writable:         writable,
		IdentitySource:   identitySource,
		StabilityClass:   stabilityClass,
		Annotations:      annotations,
	}, errs
}

func parseParametersField(root map[string]any, location string) ([]Parameter, []string) {
	items, errs := requiredArrayField(root, "parameters")
	errs = prefixProblems("entities", errs)
	out := make([]Parameter, 0, len(items))
	for index, item := range items {
		entryLocation := fmt.Sprintf("entities.parameters[%d]", index)
		object, ok := item.(map[string]any)
		if !ok {
			errs = append(errs, entryLocation+" must be an object")
			continue
		}
		parameter, more := parseParameter(object, entryLocation)
		errs = append(errs, more...)
		out = append(out, parameter)
	}
	return out, errs
}

func parseParameter(root map[string]any, location string) (Parameter, []string) {
	var errs []string
	errs = append(errs, ensureAllowedKeys(root, location,
		"id", "ownerKind", "ownerId", "componentId", "name", "displayName", "cadType", "valueType", "observable", "writable", "currentValue", "unit", "identitySource", "stabilityClass", "annotations")...)
	id, more := requiredStringField(root, "id")
	errs = append(errs, prefixProblems(location, more)...)
	ownerKind, more := requiredStringField(root, "ownerKind")
	errs = append(errs, prefixProblems(location, more)...)
	ownerID, more := requiredStringField(root, "ownerId")
	errs = append(errs, prefixProblems(location, more)...)
	componentID, more := requiredStringField(root, "componentId")
	errs = append(errs, prefixProblems(location, more)...)
	name, more := requiredStringField(root, "name")
	errs = append(errs, prefixProblems(location, more)...)
	displayName, more := requiredStringAllowEmpty(root, "displayName")
	errs = append(errs, prefixProblems(location, more)...)
	cadType, more := requiredStringField(root, "cadType")
	errs = append(errs, prefixProblems(location, more)...)
	valueType, more := requiredStringField(root, "valueType")
	errs = append(errs, prefixProblems(location, more)...)
	observable, more := requiredBoolField(root, "observable")
	errs = append(errs, prefixProblems(location, more)...)
	writable, more := requiredBoolField(root, "writable")
	errs = append(errs, prefixProblems(location, more)...)
	currentValue, more := optionalScalarField(root, "currentValue")
	errs = append(errs, prefixProblems(location, more)...)
	unit, more := optionalStringField(root, "unit")
	errs = append(errs, prefixProblems(location, more)...)
	identitySource, more := parseOptionalIdentitySource(root, location)
	errs = append(errs, more...)
	stabilityClass, more := optionalStringField(root, "stabilityClass")
	errs = append(errs, prefixProblems(location, more)...)
	annotations, more := parseAnnotationsField(root, "annotations")
	errs = append(errs, prefixProblems(location, trimPrefixOnProblems("annotations.", more))...)
	return Parameter{
		ID:             id,
		OwnerKind:      ownerKind,
		OwnerID:        ownerID,
		ComponentID:    componentID,
		Name:           name,
		DisplayName:    displayName,
		CADType:        cadType,
		ValueType:      valueType,
		Observable:     observable,
		Writable:       writable,
		CurrentValue:   currentValue,
		Unit:           unit,
		IdentitySource: identitySource,
		StabilityClass: stabilityClass,
		Annotations:    annotations,
	}, errs
}

func parseMetadataField(root map[string]any, location string) ([]Metadata, []string) {
	items, errs := requiredArrayField(root, "metadata")
	errs = prefixProblems("entities", errs)
	out := make([]Metadata, 0, len(items))
	for index, item := range items {
		entryLocation := fmt.Sprintf("entities.metadata[%d]", index)
		object, ok := item.(map[string]any)
		if !ok {
			errs = append(errs, entryLocation+" must be an object")
			continue
		}
		metadata, more := parseMetadataEntry(object, entryLocation)
		errs = append(errs, more...)
		out = append(out, metadata)
	}
	return out, errs
}

func parseMetadataEntry(root map[string]any, location string) (Metadata, []string) {
	var errs []string
	errs = append(errs, ensureAllowedKeys(root, location,
		"id", "ownerKind", "ownerId", "componentId", "key", "displayName", "cadType", "valueType", "observable", "writable", "currentValue", "identitySource", "stabilityClass", "annotations")...)
	id, more := requiredStringField(root, "id")
	errs = append(errs, prefixProblems(location, more)...)
	ownerKind, more := requiredStringField(root, "ownerKind")
	errs = append(errs, prefixProblems(location, more)...)
	ownerID, more := requiredStringField(root, "ownerId")
	errs = append(errs, prefixProblems(location, more)...)
	componentID, more := requiredStringField(root, "componentId")
	errs = append(errs, prefixProblems(location, more)...)
	key, more := requiredStringField(root, "key")
	errs = append(errs, prefixProblems(location, more)...)
	displayName, more := requiredStringAllowEmpty(root, "displayName")
	errs = append(errs, prefixProblems(location, more)...)
	cadType, more := requiredStringField(root, "cadType")
	errs = append(errs, prefixProblems(location, more)...)
	valueType, more := requiredStringField(root, "valueType")
	errs = append(errs, prefixProblems(location, more)...)
	observable, more := requiredBoolField(root, "observable")
	errs = append(errs, prefixProblems(location, more)...)
	writable, more := requiredBoolField(root, "writable")
	errs = append(errs, prefixProblems(location, more)...)
	currentValue, more := optionalScalarField(root, "currentValue")
	errs = append(errs, prefixProblems(location, more)...)
	identitySource, more := parseOptionalIdentitySource(root, location)
	errs = append(errs, more...)
	stabilityClass, more := optionalStringField(root, "stabilityClass")
	errs = append(errs, prefixProblems(location, more)...)
	annotations, more := parseAnnotationsField(root, "annotations")
	errs = append(errs, prefixProblems(location, trimPrefixOnProblems("annotations.", more))...)
	return Metadata{
		ID:             id,
		OwnerKind:      ownerKind,
		OwnerID:        ownerID,
		ComponentID:    componentID,
		Key:            key,
		DisplayName:    displayName,
		CADType:        cadType,
		ValueType:      valueType,
		Observable:     observable,
		Writable:       writable,
		CurrentValue:   currentValue,
		IdentitySource: identitySource,
		StabilityClass: stabilityClass,
		Annotations:    annotations,
	}, errs
}

func parseOptionalIdentitySource(root map[string]any, location string) (*IdentitySource, []string) {
	value, ok := root["identitySource"]
	if !ok {
		return nil, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, []string{location + ".identitySource must be an object"}
	}
	var errs []string
	errs = append(errs, ensureAllowedKeys(object, location+".identitySource",
		"kind", "path", "ownerScopedKey", "groupScopedKey", "nativeRef")...)
	kind, more := requiredStringField(object, "kind")
	errs = append(errs, prefixProblems(location+".identitySource", more)...)
	pathValue, more := optionalStringField(object, "path")
	errs = append(errs, prefixProblems(location+".identitySource", more)...)
	ownerScopedKey, more := optionalStringField(object, "ownerScopedKey")
	errs = append(errs, prefixProblems(location+".identitySource", more)...)
	groupScopedKey, more := optionalStringField(object, "groupScopedKey")
	errs = append(errs, prefixProblems(location+".identitySource", more)...)
	nativeRef, more := optionalStringAllowEmpty(object, "nativeRef")
	errs = append(errs, prefixProblems(location+".identitySource", more)...)
	return &IdentitySource{
		Kind:           kind,
		Path:           pathValue,
		OwnerScopedKey: ownerScopedKey,
		GroupScopedKey: groupScopedKey,
		NativeRef:      nativeRef,
	}, errs
}

func parseOptionalSourceFile(root map[string]any, location string) (*SourceFile, []string) {
	value, ok := root["sourceFile"]
	if !ok {
		return nil, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, []string{location + ".sourceFile must be an object"}
	}
	var errs []string
	errs = append(errs, ensureAllowedKeys(object, location+".sourceFile", "path")...)
	pathValue, more := requiredStringField(object, "path")
	errs = append(errs, prefixProblems(location+".sourceFile", more)...)
	return &SourceFile{Path: pathValue}, errs
}

func parseTargetabilityField(root map[string]any, location string) (Targetability, []string) {
	object, errs := requiredObjectField(root, "targetability")
	if object == nil {
		return Targetability{}, prefixProblems(location, errs)
	}
	errs = prefixProblems(location, errs)
	errs = append(errs, ensureAllowedKeys(object, location+".targetability", "suppress", "unsuppress", "hide", "unhide", "delete")...)
	suppress, more := requiredBoolField(object, "suppress")
	errs = append(errs, prefixProblems(location+".targetability", more)...)
	unsuppress, more := requiredBoolField(object, "unsuppress")
	errs = append(errs, prefixProblems(location+".targetability", more)...)
	hide, more := requiredBoolField(object, "hide")
	errs = append(errs, prefixProblems(location+".targetability", more)...)
	unhide, more := requiredBoolField(object, "unhide")
	errs = append(errs, prefixProblems(location+".targetability", more)...)
	deleteValue, more := requiredBoolField(object, "delete")
	errs = append(errs, prefixProblems(location+".targetability", more)...)
	return Targetability{
		Suppress:   suppress,
		Unsuppress: unsuppress,
		Hide:       hide,
		Unhide:     unhide,
		Delete:     deleteValue,
	}, errs
}

func ensureNoTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func ensureAllowedKeys(object map[string]any, location string, allowed ...string) []string {
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
	return []string{fmt.Sprintf("%s has unknown field %q", location, unknown[0])}
}

func requiredObjectField(root map[string]any, field string) (map[string]any, []string) {
	value, ok := root[field]
	if !ok {
		return nil, []string{field + " is required"}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, []string{field + " must be an object"}
	}
	return object, nil
}

func requiredArrayField(root map[string]any, field string) ([]any, []string) {
	value, ok := root[field]
	if !ok {
		return nil, []string{field + " is required"}
	}
	items, ok := value.([]any)
	if !ok {
		return nil, []string{field + " must be an array"}
	}
	return items, nil
}

func requiredStringField(root map[string]any, field string) (string, []string) {
	value, ok := root[field]
	if !ok {
		return "", []string{field + " is required"}
	}
	text, ok := value.(string)
	if !ok {
		return "", []string{field + " must be a string"}
	}
	if strings.TrimSpace(text) == "" {
		return "", []string{field + " is required"}
	}
	return text, nil
}

func requiredStringAllowEmpty(root map[string]any, field string) (string, []string) {
	value, ok := root[field]
	if !ok {
		return "", []string{field + " is required"}
	}
	text, ok := value.(string)
	if !ok {
		return "", []string{field + " must be a string"}
	}
	return text, nil
}

func optionalStringField(root map[string]any, field string) (string, []string) {
	value, ok := root[field]
	if !ok {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", []string{field + " must be a string"}
	}
	if strings.TrimSpace(text) == "" {
		return "", []string{field + " is required"}
	}
	return text, nil
}

func optionalStringAllowEmpty(root map[string]any, field string) (string, []string) {
	value, ok := root[field]
	if !ok {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", []string{field + " must be a string"}
	}
	return text, nil
}

func requiredIntField(root map[string]any, field string) (int, []string) {
	value, ok := root[field]
	if !ok {
		return 0, []string{field + " is required"}
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, []string{field + " must be an integer"}
	}
	intValue, err := number.Int64()
	if err != nil {
		return 0, []string{field + " must be an integer"}
	}
	return int(intValue), nil
}

func requiredBoolField(root map[string]any, field string) (bool, []string) {
	value, ok := root[field]
	if !ok {
		return false, []string{field + " is required"}
	}
	boolean, ok := value.(bool)
	if !ok {
		return false, []string{field + " must be a boolean"}
	}
	return boolean, nil
}

func optionalScalarField(root map[string]any, field string) (ScalarValue, []string) {
	value, ok := root[field]
	if !ok {
		return ScalarValue{}, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ScalarValue{}, []string{field + " must be valid JSON scalar"}
	}
	scalar, err := NewScalarValue(data)
	if err != nil {
		return ScalarValue{}, []string{field + " " + err.Error()}
	}
	return scalar, nil
}

func prefixProblems(prefix string, problems []string) []string {
	if len(problems) == 0 {
		return nil
	}
	out := make([]string, len(problems))
	for i, problem := range problems {
		out[i] = prefix + "." + problem
	}
	return out
}

func trimPrefixOnProblems(prefix string, problems []string) []string {
	if len(problems) == 0 {
		return nil
	}
	out := make([]string, len(problems))
	for i, problem := range problems {
		out[i] = strings.TrimPrefix(problem, prefix)
	}
	return out
}
