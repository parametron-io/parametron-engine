package verification

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/observed"
)

const SchemaVersion = "1.0"

var (
	ErrIO                         = errors.New("verification I/O error")
	ErrDecode                     = errors.New("verification decode error")
	ErrValidation                 = errors.New("verification validation error")
	ErrContractInvalid            = errors.New("verification contract invalid")
	ErrObservedInvalid            = errors.New("observed artifact invalid")
	ErrComponentMismatch          = errors.New("component mismatch")
	ErrParameterMismatch          = errors.New("parameter mismatch")
	ErrMetadataMismatch           = errors.New("metadata mismatch")
	ErrReferenceMismatch          = errors.New("reference mismatch")
	ErrRequiredObservationMissing = errors.New("required observation missing")
	ErrObservationMissing         = ErrRequiredObservationMissing
	ErrInternal                   = errors.New("internal verification error")
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

type VerifyError struct {
	Class   FailureClass
	Message string
	Err     error
}

func (e *VerifyError) Error() string {
	if e == nil || e.Message == "" {
		if e == nil || e.Err == nil {
			return "verification failed"
		}
		return e.Err.Error()
	}
	return e.Message
}

func (e *VerifyError) Unwrap() []error {
	if e == nil {
		return nil
	}
	if e.Err == nil {
		switch e.Class {
		case FailureClassContractInvalid:
			return []error{ErrContractInvalid}
		case FailureClassObservedInvalid:
			return []error{ErrObservedInvalid}
		case FailureClassComponentMismatch:
			return []error{ErrComponentMismatch}
		case FailureClassParameterMismatch:
			return []error{ErrParameterMismatch}
		case FailureClassMetadataMismatch:
			return []error{ErrMetadataMismatch}
		case FailureClassReferenceMismatch:
			return []error{ErrReferenceMismatch}
		case FailureClassRequiredObservationMissing:
			return []error{ErrObservationMissing}
		case FailureClassInternalError:
			return []error{ErrInternal}
		default:
			return nil
		}
	}

	switch e.Class {
	case FailureClassContractInvalid:
		return []error{ErrContractInvalid, e.Err}
	case FailureClassObservedInvalid:
		return []error{ErrObservedInvalid, e.Err}
	case FailureClassComponentMismatch:
		return []error{ErrComponentMismatch, e.Err}
	case FailureClassParameterMismatch:
		return []error{ErrParameterMismatch, e.Err}
	case FailureClassMetadataMismatch:
		return []error{ErrMetadataMismatch, e.Err}
	case FailureClassReferenceMismatch:
		return []error{ErrReferenceMismatch, e.Err}
	case FailureClassRequiredObservationMissing:
		return []error{ErrObservationMissing, e.Err}
	case FailureClassInternalError:
		return []error{ErrInternal, e.Err}
	default:
		return []error{e.Err}
	}
}

type Contract struct {
	SchemaVersion      string             `json:"schemaVersion"`
	Observe            Observe            `json:"observe"`
	ObservationContext ObservationContext `json:"observationContext,omitempty"`
	Expected           Expected           `json:"expected"`
	Checks             Checks             `json:"checks"`
}

type Observe struct {
	Components bool `json:"components"`
	Parameters bool `json:"parameters"`
	Metadata   bool `json:"metadata"`
	References bool `json:"references"`
}

type Expected struct {
	Components []ExpectedComponent `json:"components"`
	Parameters []ExpectedParameter `json:"parameters"`
	Metadata   []ExpectedMetadata  `json:"metadata"`
	References []ExpectedReference `json:"references"`
}

type ExpectedComponent struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	ParentID string `json:"parentId"`
}

type ExpectedParameter struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
}

type ObservationContext struct {
	Parameters []ObservedParameterBinding `json:"parameters,omitempty"`
}

type ObservedParameterBinding struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GroupName string `json:"groupName"`
}

type ExpectedMetadata struct {
	ID        string `json:"id,omitempty"`
	Key       string `json:"key"`
	OwnerID   string `json:"ownerId"`
	Value     string `json:"value"`
	ValueKind string `json:"valueKind,omitempty"`
}

type ExpectedReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type Checks struct {
	Components Check `json:"components"`
	Parameters Check `json:"parameters"`
	Metadata   Check `json:"metadata"`
	References Check `json:"references"`
}

type Check struct {
	Enabled bool `json:"enabled"`
}

type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
)

type CategoryStatus string

const (
	CategoryStatusPass    CategoryStatus = "pass"
	CategoryStatusFail    CategoryStatus = "fail"
	CategoryStatusSkipped CategoryStatus = "skipped"
)

type FailureClass string

const (
	FailureClassNone                       FailureClass = ""
	FailureClassContractInvalid            FailureClass = "verification_contract_invalid"
	FailureClassObservedInvalid            FailureClass = "observed_artifact_invalid"
	FailureClassComponentMismatch          FailureClass = "component_mismatch"
	FailureClassParameterMismatch          FailureClass = "parameter_mismatch"
	FailureClassMetadataMismatch           FailureClass = "metadata_mismatch"
	FailureClassReferenceMismatch          FailureClass = "reference_mismatch"
	FailureClassRequiredObservationMissing FailureClass = "required_observation_missing"
	FailureClassInternalError              FailureClass = "internal_verification_error"
)

type Result struct {
	Status     Status          `json:"status"`
	Failure    FailureClass    `json:"failure,omitempty"`
	Message    string          `json:"message"`
	Categories CategoryResults `json:"categories"`
}

type CategoryResults struct {
	Components CategoryResult `json:"components"`
	Parameters CategoryResult `json:"parameters"`
	Metadata   CategoryResult `json:"metadata"`
	References CategoryResult `json:"references"`
}

type CategoryResult struct {
	Enabled bool           `json:"enabled"`
	Status  CategoryStatus `json:"status"`
	Message string         `json:"message"`
	class   FailureClass   `json:"-"`
}

const (
	componentVerificationDisabledMessage = "component verification is disabled"
	componentsNotEvaluatedMessage        = "component verification was not evaluated"
	parameterVerificationDisabledMessage = "parameter verification is disabled in schema version 1.0 baseline"
	metadataNotEvaluatedMessage          = "metadata verification was not evaluated"
	referencesNotEvaluatedMessage        = "reference verification was not evaluated"
)

func Parse(data []byte) (*Contract, error) {
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
	contract, err := parseContractObject(root)
	if err != nil {
		return nil, err
	}
	if err := Validate(contract); err != nil {
		return nil, err
	}
	return contract, nil
}

func LoadFile(path string) (*Contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{Path: path, Err: err}
	}
	return Parse(data)
}

func Validate(contract *Contract) error {
	if contract == nil {
		return &ValidationError{Message: "verification contract must not be nil"}
	}
	if contract.SchemaVersion != SchemaVersion {
		return &ValidationError{Message: fmt.Sprintf("schemaVersion must be %q", SchemaVersion)}
	}
	if contract.Expected.Parameters == nil {
		return &ValidationError{Message: "expected.parameters is required"}
	}
	if contract.Expected.Components == nil {
		return &ValidationError{Message: "expected.components is required"}
	}
	if contract.Expected.Metadata == nil {
		return &ValidationError{Message: "expected.metadata is required"}
	}
	if contract.Expected.References == nil {
		return &ValidationError{Message: "expected.references is required"}
	}
	if contract.Checks.Components.Enabled && !contract.Observe.Components {
		return &ValidationError{Message: "checks.components.enabled requires observe.components to be true"}
	}
	if contract.Checks.Parameters.Enabled {
		if !contract.Observe.Parameters {
			return &ValidationError{Message: "checks.parameters.enabled requires observe.parameters to be true"}
		}
	}
	if contract.Checks.Metadata.Enabled && !contract.Observe.Metadata {
		return &ValidationError{Message: "checks.metadata.enabled requires observe.metadata to be true"}
	}
	if contract.Checks.References.Enabled && !contract.Observe.References {
		return &ValidationError{Message: "checks.references.enabled requires observe.references to be true"}
	}
	for index, component := range contract.Expected.Components {
		if strings.TrimSpace(component.ID) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.components[%d].id is required", index)}
		}
		if !isSupportedExpectedComponentKind(component.Kind) {
			return &ValidationError{Message: fmt.Sprintf("expected.components[%d].kind must be one of %q or %q", index, observed.ComponentKindAssembly, observed.ComponentKindPart)}
		}
		if strings.TrimSpace(component.Name) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.components[%d].name is required", index)}
		}
		if component.ParentID != "" && strings.TrimSpace(component.ParentID) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.components[%d].parentId must not be blank", index)}
		}
	}
	for index, parameter := range contract.Expected.Parameters {
		if strings.TrimSpace(parameter.ID) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.parameters[%d].id is required", index)}
		}
		if parameter.Type != "number" {
			return &ValidationError{Message: fmt.Sprintf("expected.parameters[%d].type must be %q", index, "number")}
		}
		if parameter.Unit != "mm" {
			return &ValidationError{Message: fmt.Sprintf("expected.parameters[%d].unit must be %q", index, "mm")}
		}
	}
	for index, entry := range contract.Expected.Metadata {
		if entry.ID != "" && strings.TrimSpace(entry.ID) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].id must not be blank", index)}
		}
		if strings.TrimSpace(entry.Key) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].key is required", index)}
		}
		if entry.OwnerID != "" && strings.TrimSpace(entry.OwnerID) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].ownerId must not be blank", index)}
		}
		if strings.TrimSpace(entry.Value) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].value is required", index)}
		}
		if entry.ID != "" && !isSupportedExpectedMetadataValueKind(entry.ValueKind) {
			return &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].valueKind must be one of %q, %q, %q, or %q when id is present", index, "number", "integer", "string", "boolean")}
		}
	}
	for index, entry := range contract.Expected.References {
		if strings.TrimSpace(entry.Kind) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.references[%d].kind is required", index)}
		}
		if strings.TrimSpace(entry.Name) == "" {
			return &ValidationError{Message: fmt.Sprintf("expected.references[%d].name is required", index)}
		}
		if !filepath.IsAbs(entry.Name) {
			return &ValidationError{Message: fmt.Sprintf("expected.references[%d].name must be an absolute path", index)}
		}
	}
	for index, parameter := range contract.ObservationContext.Parameters {
		if strings.TrimSpace(parameter.ID) == "" {
			return &ValidationError{Message: fmt.Sprintf("observationContext.parameters[%d].id is required", index)}
		}
		if strings.TrimSpace(parameter.Name) == "" {
			return &ValidationError{Message: fmt.Sprintf("observationContext.parameters[%d].name is required", index)}
		}
		if parameter.GroupName != "" && strings.TrimSpace(parameter.GroupName) == "" {
			return &ValidationError{Message: fmt.Sprintf("observationContext.parameters[%d].groupName must not be blank", index)}
		}
	}

	seenComponents := make(map[string]int, len(contract.Expected.Components))
	for index, component := range contract.Expected.Components {
		if previous, exists := seenComponents[component.ID]; exists {
			return &ValidationError{Message: fmt.Sprintf("expected.components[%d].id %q duplicates expected.components[%d].id", index, component.ID, previous)}
		}
		seenComponents[component.ID] = index
	}
	seenParameters := make(map[string]int, len(contract.Expected.Parameters))
	for index, parameter := range contract.Expected.Parameters {
		if previous, exists := seenParameters[parameter.ID]; exists {
			return &ValidationError{Message: fmt.Sprintf("expected.parameters[%d].id %q duplicates expected.parameters[%d].id", index, parameter.ID, previous)}
		}
		seenParameters[parameter.ID] = index
	}
	seenMetadataKeys := make(map[string]int, len(contract.Expected.Metadata))
	seenMetadataIDs := make(map[string]int, len(contract.Expected.Metadata))
	for index, entry := range contract.Expected.Metadata {
		if entry.ID != "" {
			if previous, exists := seenMetadataIDs[entry.ID]; exists {
				return &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].id %q duplicates expected.metadata[%d].id", index, entry.ID, previous)}
			}
			seenMetadataIDs[entry.ID] = index
			continue
		}
		if previous, exists := seenMetadataKeys[entry.Key]; exists {
			return &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].key %q duplicates expected.metadata[%d].key", index, entry.Key, previous)}
		}
		seenMetadataKeys[entry.Key] = index
	}
	seenReferences := make(map[string]int, len(contract.Expected.References))
	for index, entry := range contract.Expected.References {
		if previous, exists := seenReferences[entry.Kind]; exists {
			return &ValidationError{Message: fmt.Sprintf("expected.references[%d].kind %q duplicates expected.references[%d].kind", index, entry.Kind, previous)}
		}
		seenReferences[entry.Kind] = index
	}
	seenObservationContextParameters := make(map[string]int, len(contract.ObservationContext.Parameters))
	for index, parameter := range contract.ObservationContext.Parameters {
		if previous, exists := seenObservationContextParameters[parameter.ID]; exists {
			return &ValidationError{Message: fmt.Sprintf("observationContext.parameters[%d].id %q duplicates observationContext.parameters[%d].id", index, parameter.ID, previous)}
		}
		seenObservationContextParameters[parameter.ID] = index
	}

	return nil
}

func CanonicalJSON(contract *Contract) ([]byte, error) {
	if err := Validate(contract); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.WriteByte('{')
	writeJSONString(&out, "schemaVersion")
	out.WriteByte(':')
	writeJSONString(&out, contract.SchemaVersion)
	out.WriteByte(',')
	writeJSONString(&out, "observe")
	out.WriteByte(':')
	writeObserve(&out, contract.Observe)
	out.WriteByte(',')
	writeJSONString(&out, "observationContext")
	out.WriteByte(':')
	writeObservationContext(&out, contract.ObservationContext)
	out.WriteByte(',')
	writeJSONString(&out, "expected")
	out.WriteByte(':')
	writeExpected(&out, contract.Expected)
	out.WriteByte(',')
	writeJSONString(&out, "checks")
	out.WriteByte(':')
	writeChecks(&out, contract.Checks)
	out.WriteByte('}')
	return out.Bytes(), nil
}

func WriteFile(path string, contract *Contract) error {
	data, err := CanonicalJSON(contract)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &FileError{Path: path, Err: fmt.Errorf("create parent directory: %w", err)}
	}

	tmpFile, err := os.CreateTemp(dir, ".verification-tmp-*")
	if err != nil {
		return &FileError{Path: path, Err: fmt.Errorf("create temporary verification file: %w", err)}
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: fmt.Errorf("write temporary verification file: %w", err)}
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: fmt.Errorf("close temporary verification file: %w", err)}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: fmt.Errorf("finalize verification file: %w", err)}
	}
	return nil
}

// DeriveFromManifestAndWorkingCopy derives a verification contract from the
// exact canonical absolute working-copy path supplied by the caller.
func DeriveFromManifestAndWorkingCopy(manifest planner.WriteExportManifestPayload, workingCopyPath string) (*Contract, error) {
	if workingCopyPath == "" || strings.TrimSpace(workingCopyPath) != workingCopyPath || strings.ContainsRune(workingCopyPath, '\x00') || !filepath.IsAbs(workingCopyPath) || filepath.Clean(workingCopyPath) != workingCopyPath {
		return nil, &ValidationError{Message: "working copy path must be canonical, absolute, and clean"}
	}
	assignments, err := deriveExpectedParametersFromVerificationIntent(manifest.Verification.ExpectedParameters, manifest.ParameterAssignments)
	if err != nil {
		return nil, &ValidationError{Message: err.Error()}
	}
	workingCopyBytes, err := os.ReadFile(workingCopyPath)
	if err != nil {
		return nil, &FileError{Path: workingCopyPath, Err: err}
	}
	sum := sha256.Sum256(workingCopyBytes)

	contract := &Contract{
		SchemaVersion: SchemaVersion,
		Observe: Observe{
			Components: false,
			Parameters: len(assignments) > 0,
			Metadata:   true,
			References: true,
		},
		ObservationContext: ObservationContext{
			Parameters: []ObservedParameterBinding{},
		},
		Expected: Expected{
			Components: []ExpectedComponent{},
			Parameters: assignments,
			Metadata: []ExpectedMetadata{{
				Key:   "working_copy_sha256",
				Value: hex.EncodeToString(sum[:]),
			}},
			References: []ExpectedReference{{
				Kind: "working_copy_path",
				Name: workingCopyPath,
			}},
		},
		Checks: Checks{
			Components: Check{Enabled: false},
			Parameters: Check{Enabled: len(assignments) > 0},
			Metadata:   Check{Enabled: true},
			References: Check{Enabled: true},
		},
	}
	if contract.Checks.Parameters.Enabled {
		contract.ObservationContext.Parameters = deriveObservedParameterBindings(manifest.Verification.ObservationParameterLinks)
	}
	if err := Validate(contract); err != nil {
		return nil, err
	}
	return contract, nil
}

func Verify(contract *Contract, obs *observed.Observed) (*Result, error) {
	if err := Validate(contract); err != nil {
		return failResult(FailureClassContractInvalid, fmt.Sprintf("verification contract invalid: %v", err), defaultCategoryResults(contract), err)
	}
	if err := validateObservedForVerification(obs); err != nil {
		return failResult(FailureClassObservedInvalid, fmt.Sprintf("observed artifact invalid: %v", err), defaultCategoryResults(contract), err)
	}

	categories := CategoryResults{
		Components: componentCategoryResult(contract),
		Parameters: parameterCategoryResult(contract),
		Metadata:   skippedCategoryResult(contract.Checks.Metadata.Enabled, metadataNotEvaluatedMessage),
		References: skippedCategoryResult(contract.Checks.References.Enabled, referencesNotEvaluatedMessage),
	}

	if contract.Checks.Components.Enabled {
		categories.Components = verifyComponents(contract.Expected.Components, obs.Observation.Components)
	}
	if contract.Checks.Parameters.Enabled {
		categories.Parameters = verifyParameters(contract.Expected.Parameters, obs.Observation.Parameters)
	}
	if contract.Checks.Metadata.Enabled {
		categories.Metadata = verifyMetadata(contract.Expected.Metadata, obs.Observation.Metadata)
	}
	if contract.Checks.References.Enabled {
		categories.References = verifyReferences(contract.Expected.References, obs.Observation.References)
	}

	result := aggregateResult(categories)
	if result.Status == StatusFail {
		return result, &VerifyError{Class: result.Failure, Message: result.Message}
	}
	return result, nil
}

func aggregateResult(categories CategoryResults) *Result {
	if failed := firstEnabledFailure(categories); failed != nil {
		failureClass := failed.failureClass()
		message := failed.Message
		if failureClass == FailureClassNone {
			failureClass = FailureClassInternalError
			message = "internal verification error: failed verification category did not declare a failure class"
		}
		return &Result{
			Status:     StatusFail,
			Failure:    failureClass,
			Message:    message,
			Categories: categories,
		}
	}
	return &Result{
		Status:     StatusPass,
		Message:    "verification passed",
		Categories: categories,
	}
}

func firstEnabledFailure(categories CategoryResults) *CategoryResult {
	ordered := []CategoryResult{
		categories.Components,
		categories.Parameters,
		categories.Metadata,
		categories.References,
	}
	for i := range ordered {
		if !ordered[i].Enabled || ordered[i].Status != CategoryStatusFail {
			continue
		}
		return &ordered[i]
	}
	return nil
}

func deriveExpectedParameters(assignments []planner.ExportManifestParameterAssignment) ([]ExpectedParameter, error) {
	_ = assignments
	return deriveExpectedParametersFromVerificationIntent(nil, nil)
}

func deriveExpectedParametersFromVerificationIntent(expectedParameters []planner.VerificationExpectedParameter, _ []planner.ExportManifestParameterAssignment) ([]ExpectedParameter, error) {
	if len(expectedParameters) == 0 {
		return []ExpectedParameter{}, nil
	}

	expected := make([]ExpectedParameter, len(expectedParameters))
	for index, parameter := range expectedParameters {
		if strings.TrimSpace(parameter.ID) == "" {
			return nil, fmt.Errorf("verification.expectedParameters[%d].id is required", index)
		}
		if parameter.Type != "number" {
			return nil, fmt.Errorf("verification.expectedParameters[%d].type must be %q", index, "number")
		}
		if parameter.Unit != "mm" {
			return nil, fmt.Errorf("verification.expectedParameters[%d].unit must be %q", index, "mm")
		}
		expected[index] = ExpectedParameter{
			ID:    parameter.ID,
			Name:  parameter.Name,
			Type:  parameter.Type,
			Unit:  parameter.Unit,
			Value: parameter.Value,
		}
	}

	slices.SortFunc(expected, func(left, right ExpectedParameter) int {
		if left.ID != right.ID {
			return strings.Compare(left.ID, right.ID)
		}
		if left.Name != right.Name {
			return strings.Compare(left.Name, right.Name)
		}
		if left.Value != right.Value {
			if left.Value < right.Value {
				return -1
			}
			return 1
		}
		if left.Type != right.Type {
			return strings.Compare(left.Type, right.Type)
		}
		return strings.Compare(left.Unit, right.Unit)
	})

	for index := 1; index < len(expected); index++ {
		if expected[index-1].ID == expected[index].ID {
			return nil, fmt.Errorf("verification.expectedParameters[%d].id %q duplicates verification.expectedParameters[%d].id", index, expected[index].ID, index-1)
		}
	}

	return expected, nil
}

func deriveObservedParameterBindings(bindings []planner.VerificationObservationParameterLink) []ObservedParameterBinding {
	if len(bindings) == 0 {
		return []ObservedParameterBinding{}
	}
	out := make([]ObservedParameterBinding, 0, len(bindings))
	for _, binding := range bindings {
		if strings.TrimSpace(binding.ID) == "" || strings.TrimSpace(binding.Name) == "" {
			continue
		}
		out = append(out, ObservedParameterBinding{
			ID:        binding.ID,
			Name:      binding.Name,
			GroupName: binding.GroupName,
		})
	}
	slices.SortFunc(out, compareObservedParameterBindings)
	return out
}

func verifyComponents(expected []ExpectedComponent, actual []observed.Component) CategoryResult {
	for _, entry := range expected {
		count := 0
		var matched observed.Component
		for _, observedEntry := range actual {
			if observedEntry.ID != entry.ID {
				continue
			}
			count++
			matched = observedEntry
		}
		if count == 0 {
			return failedCategoryResult(true, FailureClassRequiredObservationMissing, fmt.Sprintf("required observed component %q is missing", entry.ID))
		}
		if count > 1 {
			return failedCategoryResult(true, FailureClassComponentMismatch, fmt.Sprintf("observed component %q is not unique", entry.ID))
		}
		if matched.Kind != entry.Kind {
			return failedCategoryResult(true, FailureClassComponentMismatch, fmt.Sprintf("observed component %q kind mismatch: want %q, got %q", entry.ID, entry.Kind, matched.Kind))
		}
		if matched.Name != entry.Name {
			return failedCategoryResult(true, FailureClassComponentMismatch, fmt.Sprintf("observed component %q name mismatch: want %q, got %q", entry.ID, entry.Name, matched.Name))
		}
		if matched.ParentID != entry.ParentID {
			return failedCategoryResult(true, FailureClassComponentMismatch, fmt.Sprintf("observed component %q parentId mismatch: want %q, got %q", entry.ID, expectedOptionalIdentity(entry.ParentID), expectedOptionalIdentity(matched.ParentID)))
		}
	}
	return passedCategoryResult(true, fmt.Sprintf("verified %d components", len(expected)))
}

func verifyMetadata(expected []ExpectedMetadata, actual []observed.Metadata) CategoryResult {
	for _, entry := range expected {
		if entry.ID != "" {
			count := 0
			var matched observed.Metadata
			for _, observedEntry := range actual {
				if observedEntry.ID != entry.ID {
					continue
				}
				count++
				matched = observedEntry
			}
			if count == 0 {
				return failedCategoryResult(true, FailureClassRequiredObservationMissing, fmt.Sprintf("required observed metadata %q is missing", entry.ID))
			}
			if count > 1 {
				return failedCategoryResult(true, FailureClassMetadataMismatch, fmt.Sprintf("observed metadata %q is not unique", entry.ID))
			}
			if matched.Key != entry.Key {
				return failedCategoryResult(true, FailureClassMetadataMismatch, fmt.Sprintf("observed metadata %q key mismatch: want %q, got %q", entry.ID, entry.Key, matched.Key))
			}
			if matched.OwnerID != entry.OwnerID {
				return failedCategoryResult(true, FailureClassMetadataMismatch, fmt.Sprintf("observed metadata %q ownerId mismatch: want %q, got %q", entry.ID, expectedOptionalIdentity(entry.OwnerID), expectedOptionalIdentity(matched.OwnerID)))
			}
			if matched.ValueKind != entry.ValueKind {
				return failedCategoryResult(true, FailureClassMetadataMismatch, fmt.Sprintf("observed metadata %q valueKind mismatch: want %q, got %q", entry.ID, entry.ValueKind, matched.ValueKind))
			}
			value := observedMetadataIdentityComparableValue(matched)
			if value != entry.Value {
				return failedCategoryResult(true, FailureClassMetadataMismatch, fmt.Sprintf("observed metadata %q mismatch: want %q, got %q", entry.ID, entry.Value, value))
			}
			continue
		}

		count := 0
		value := ""
		for _, observedEntry := range actual {
			if observedEntry.Key != entry.Key {
				continue
			}
			count++
			value = observedMetadataComparableValue(observedEntry)
		}
		if count == 0 {
			return failedCategoryResult(true, FailureClassRequiredObservationMissing, fmt.Sprintf("required observed metadata %q is missing", entry.Key))
		}
		if count > 1 {
			return failedCategoryResult(true, FailureClassMetadataMismatch, fmt.Sprintf("observed metadata %q is not unique", entry.Key))
		}
		if value != entry.Value {
			return failedCategoryResult(true, FailureClassMetadataMismatch, fmt.Sprintf("observed metadata %q mismatch: want %q, got %q", entry.Key, entry.Value, value))
		}
	}
	return passedCategoryResult(true, fmt.Sprintf("verified %d metadata entries", len(expected)))
}

func verifyParameters(expected []ExpectedParameter, actual []observed.Parameter) CategoryResult {
	for _, entry := range expected {
		count := 0
		var matched observed.Parameter
		for _, observedEntry := range actual {
			if observedEntry.ID != entry.ID {
				continue
			}
			count++
			matched = observedEntry
		}
		if count == 0 {
			return failedCategoryResult(true, FailureClassRequiredObservationMissing, fmt.Sprintf("required observed parameter %q is missing", entry.ID))
		}
		if count > 1 {
			return failedCategoryResult(true, FailureClassParameterMismatch, fmt.Sprintf("observed parameter %q is not unique", entry.ID))
		}
		if matched.ValueKind != "number" {
			return failedCategoryResult(true, FailureClassParameterMismatch, fmt.Sprintf("observed parameter %q valueKind mismatch: want %q, got %q", entry.ID, "number", matched.ValueKind))
		}
		wantValue := strconv.FormatFloat(entry.Value, 'f', -1, 64)
		gotValue := string(matched.Value.Raw())
		if gotValue != wantValue {
			return failedCategoryResult(true, FailureClassParameterMismatch, fmt.Sprintf("observed parameter %q mismatch: want %s, got %s", entry.ID, wantValue, gotValue))
		}
	}
	return passedCategoryResult(true, fmt.Sprintf("verified %d parameters", len(expected)))
}

func observedMetadataComparableValue(entry observed.Metadata) string {
	raw := entry.Value.Raw()
	if len(raw) == 0 {
		return ""
	}
	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		return stringValue
	}
	return strconv.Quote(string(raw))
}

func observedMetadataIdentityComparableValue(entry observed.Metadata) string {
	raw := entry.Value.Raw()
	if len(raw) == 0 {
		return ""
	}
	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		return stringValue
	}
	return string(raw)
}

func verifyReferences(expected []ExpectedReference, actual []observed.Reference) CategoryResult {
	for _, entry := range expected {
		count := 0
		name := ""
		for _, observedEntry := range actual {
			if observedEntry.Kind != entry.Kind {
				continue
			}
			count++
			name = observedEntry.Name
		}
		if count == 0 {
			return failedCategoryResult(true, FailureClassRequiredObservationMissing, fmt.Sprintf("required observed reference %q is missing", entry.Kind))
		}
		if count > 1 {
			return failedCategoryResult(true, FailureClassReferenceMismatch, fmt.Sprintf("observed reference %q is not unique", entry.Kind))
		}
		if name != entry.Name {
			return failedCategoryResult(true, FailureClassReferenceMismatch, fmt.Sprintf("observed reference %q mismatch: want %q, got %q", entry.Kind, entry.Name, name))
		}
	}
	return passedCategoryResult(true, fmt.Sprintf("verified %d reference entries", len(expected)))
}

func failResult(class FailureClass, message string, categories CategoryResults, cause error) (*Result, error) {
	return &Result{
		Status:     StatusFail,
		Failure:    class,
		Message:    message,
		Categories: categories,
	}, &VerifyError{Class: class, Message: message, Err: cause}
}

func validateObservedForVerification(obs *observed.Observed) error {
	if obs == nil {
		return observed.Validate(obs)
	}

	clone := *obs
	clone.Observation.Parameters = slices.Clone(obs.Observation.Parameters)
	clone.Observation.Metadata = slices.Clone(obs.Observation.Metadata)
	clone.Observation.References = slices.Clone(obs.Observation.References)
	clone.Observation.Components = []observed.Component{}

	if err := observed.Validate(&clone); err != nil {
		return err
	}
	if obs.Observation.Components == nil {
		return &observed.ValidationError{Message: "observation.components is required"}
	}
	for index, component := range obs.Observation.Components {
		if strings.TrimSpace(component.ID) == "" {
			return &observed.ValidationError{Message: fmt.Sprintf("observation.components[%d].id is required", index)}
		}
		if !isSupportedExpectedComponentKind(component.Kind) {
			return &observed.ValidationError{Message: fmt.Sprintf("observation.components[%d].kind must be one of %q or %q", index, observed.ComponentKindAssembly, observed.ComponentKindPart)}
		}
		if strings.TrimSpace(component.Name) == "" {
			return &observed.ValidationError{Message: fmt.Sprintf("observation.components[%d].name is required", index)}
		}
		if component.ParentID != "" && strings.TrimSpace(component.ParentID) == "" {
			return &observed.ValidationError{Message: fmt.Sprintf("observation.components[%d].parentId must not be blank", index)}
		}
	}
	return nil
}

func defaultCategoryResults(contract *Contract) CategoryResults {
	results := CategoryResults{
		Components: skippedCategoryResult(false, componentsNotEvaluatedMessage),
		Parameters: parameterCategoryResult(nil),
		Metadata:   skippedCategoryResult(false, metadataNotEvaluatedMessage),
		References: skippedCategoryResult(false, referencesNotEvaluatedMessage),
	}
	if contract != nil {
		results.Components = componentCategoryResult(contract)
		results.Parameters = parameterCategoryResult(contract)
		results.Metadata = skippedCategoryResult(contract.Checks.Metadata.Enabled, metadataNotEvaluatedMessage)
		results.References = skippedCategoryResult(contract.Checks.References.Enabled, referencesNotEvaluatedMessage)
	}
	return results
}

func componentCategoryResult(contract *Contract) CategoryResult {
	enabled := false
	if contract != nil {
		enabled = contract.Checks.Components.Enabled
	}
	if enabled {
		return skippedCategoryResult(true, componentsNotEvaluatedMessage)
	}
	return skippedCategoryResult(enabled, componentVerificationDisabledMessage)
}

func parameterCategoryResult(contract *Contract) CategoryResult {
	enabled := false
	if contract != nil {
		enabled = contract.Checks.Parameters.Enabled
	}
	if enabled {
		return skippedCategoryResult(true, "parameter verification was not evaluated")
	}
	return skippedCategoryResult(enabled, parameterVerificationDisabledMessage)
}

func skippedCategoryResult(enabled bool, message string) CategoryResult {
	return CategoryResult{
		Enabled: enabled,
		Status:  CategoryStatusSkipped,
		Message: message,
	}
}

func passedCategoryResult(enabled bool, message string) CategoryResult {
	return CategoryResult{
		Enabled: enabled,
		Status:  CategoryStatusPass,
		Message: message,
	}
}

func failedCategoryResult(enabled bool, class FailureClass, message string) CategoryResult {
	return CategoryResult{
		Enabled: enabled,
		Status:  CategoryStatusFail,
		Message: message,
		class:   class,
	}
}

func (r CategoryResult) failureClass() FailureClass {
	return r.FailureClass()
}

// FailureClass returns the category-specific verification failure classification when present.
func (r CategoryResult) FailureClass() FailureClass {
	return r.class
}

func parseContractObject(root map[string]any) (*Contract, error) {
	if err := ensureAllowedKeys(root, "root", "schemaVersion", "observe", "observationContext", "expected", "checks"); err != nil {
		return nil, err
	}
	schemaVersion, err := requiredStringField(root, "schemaVersion")
	if err != nil {
		return nil, err
	}
	observeObject, err := requiredObjectField(root, "observe")
	if err != nil {
		return nil, err
	}
	expectedObject, err := requiredObjectField(root, "expected")
	if err != nil {
		return nil, err
	}
	checksObject, err := requiredObjectField(root, "checks")
	if err != nil {
		return nil, err
	}

	observe, err := parseObserveObject(observeObject)
	if err != nil {
		return nil, err
	}
	expected, err := parseExpectedObject(expectedObject)
	if err != nil {
		return nil, err
	}
	observationContext := ObservationContext{Parameters: []ObservedParameterBinding{}}
	if observationContextValue, ok := root["observationContext"]; ok {
		observationContextObject, ok := observationContextValue.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: "observationContext must be an object"}
		}
		observationContext, err = parseObservationContextObject(observationContextObject)
		if err != nil {
			return nil, err
		}
	}
	checks, err := parseChecksObject(checksObject)
	if err != nil {
		return nil, err
	}

	return &Contract{
		SchemaVersion:      schemaVersion,
		Observe:            observe,
		ObservationContext: observationContext,
		Expected:           expected,
		Checks:             checks,
	}, nil
}

func parseObserveObject(root map[string]any) (Observe, error) {
	if err := ensureAllowedKeys(root, "observe", "components", "parameters", "metadata", "references"); err != nil {
		return Observe{}, err
	}
	components, err := requiredBoolField(root, "components")
	if err != nil {
		return Observe{}, wrapFieldError("observe", err)
	}
	parameters, err := requiredBoolField(root, "parameters")
	if err != nil {
		return Observe{}, wrapFieldError("observe", err)
	}
	metadata, err := requiredBoolField(root, "metadata")
	if err != nil {
		return Observe{}, wrapFieldError("observe", err)
	}
	references, err := requiredBoolField(root, "references")
	if err != nil {
		return Observe{}, wrapFieldError("observe", err)
	}
	return Observe{
		Components: components,
		Parameters: parameters,
		Metadata:   metadata,
		References: references,
	}, nil
}

func parseExpectedObject(root map[string]any) (Expected, error) {
	if err := ensureAllowedKeys(root, "expected", "components", "parameters", "metadata", "references"); err != nil {
		return Expected{}, err
	}
	components, err := parseExpectedComponentsField(root)
	if err != nil {
		return Expected{}, err
	}
	parameters, err := parseExpectedParametersField(root)
	if err != nil {
		return Expected{}, err
	}
	metadataEntries, err := parseExpectedMetadataField(root)
	if err != nil {
		return Expected{}, err
	}
	references, err := parseExpectedReferencesField(root)
	if err != nil {
		return Expected{}, err
	}
	return Expected{
		Components: components,
		Parameters: parameters,
		Metadata:   metadataEntries,
		References: references,
	}, nil
}

func parseObservationContextObject(root map[string]any) (ObservationContext, error) {
	if err := ensureAllowedKeys(root, "observationContext", "parameters"); err != nil {
		return ObservationContext{}, err
	}
	items, err := requiredArrayField(root, "parameters")
	if err != nil {
		return ObservationContext{}, wrapFieldError("observationContext", err)
	}
	parameters := make([]ObservedParameterBinding, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return ObservationContext{}, &ValidationError{Message: fmt.Sprintf("observationContext.parameters[%d] must be an object", index)}
		}
		if err := ensureAllowedKeys(object, fmt.Sprintf("observationContext.parameters[%d]", index), "id", "name", "groupName"); err != nil {
			return ObservationContext{}, err
		}
		id, err := requiredStringField(object, "id")
		if err != nil {
			return ObservationContext{}, wrapIndexedFieldError("observationContext.parameters", index, err)
		}
		name, err := requiredStringField(object, "name")
		if err != nil {
			return ObservationContext{}, wrapIndexedFieldError("observationContext.parameters", index, err)
		}
		groupName := ""
		if rawGroupName, ok := object["groupName"]; ok {
			switch typed := rawGroupName.(type) {
			case nil:
				groupName = ""
			case string:
				groupName = typed
			default:
				return ObservationContext{}, &ValidationError{Message: fmt.Sprintf("observationContext.parameters[%d].groupName must be a string or null", index)}
			}
		}
		parameters = append(parameters, ObservedParameterBinding{
			ID:        id,
			Name:      name,
			GroupName: groupName,
		})
	}
	return ObservationContext{Parameters: parameters}, nil
}

func parseChecksObject(root map[string]any) (Checks, error) {
	if err := ensureAllowedKeys(root, "checks", "components", "parameters", "metadata", "references"); err != nil {
		return Checks{}, err
	}
	components, err := parseCheckField(root, "components")
	if err != nil {
		return Checks{}, err
	}
	parameters, err := parseCheckField(root, "parameters")
	if err != nil {
		return Checks{}, err
	}
	metadata, err := parseCheckField(root, "metadata")
	if err != nil {
		return Checks{}, err
	}
	references, err := parseCheckField(root, "references")
	if err != nil {
		return Checks{}, err
	}
	return Checks{
		Components: components,
		Parameters: parameters,
		Metadata:   metadata,
		References: references,
	}, nil
}

func parseExpectedComponentsField(root map[string]any) ([]ExpectedComponent, error) {
	items, err := requiredArrayField(root, "components")
	if err != nil {
		return nil, wrapFieldError("expected", err)
	}
	components := make([]ExpectedComponent, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("expected.components[%d] must be an object", index)}
		}
		if err := ensureAllowedKeys(object, fmt.Sprintf("expected.components[%d]", index), "id", "kind", "name", "parentId"); err != nil {
			return nil, err
		}
		id, err := requiredStringField(object, "id")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.components", index, err)
		}
		kind, err := requiredStringField(object, "kind")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.components", index, err)
		}
		name, err := requiredStringField(object, "name")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.components", index, err)
		}
		component := ExpectedComponent{ID: id, Kind: kind, Name: name}
		if rawParentID, ok := object["parentId"]; ok {
			switch typed := rawParentID.(type) {
			case nil:
				component.ParentID = ""
			case string:
				component.ParentID = typed
			default:
				return nil, &ValidationError{Message: fmt.Sprintf("expected.components[%d].parentId must be a string or null", index)}
			}
		}
		components = append(components, component)
	}
	return components, nil
}

func parseExpectedParametersField(root map[string]any) ([]ExpectedParameter, error) {
	items, err := requiredArrayField(root, "parameters")
	if err != nil {
		return nil, wrapFieldError("expected", err)
	}
	parameters := make([]ExpectedParameter, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("expected.parameters[%d] must be an object", index)}
		}
		if err := ensureAllowedKeys(object, fmt.Sprintf("expected.parameters[%d]", index), "id", "name", "type", "unit", "value"); err != nil {
			return nil, err
		}
		id, err := requiredStringField(object, "id")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.parameters", index, err)
		}
		name := ""
		if rawName, ok := object["name"]; ok {
			typed, ok := rawName.(string)
			if !ok {
				return nil, &ValidationError{Message: fmt.Sprintf("expected.parameters[%d].name must be a string", index)}
			}
			name = typed
		}
		typeValue, err := requiredStringField(object, "type")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.parameters", index, err)
		}
		unit, err := requiredStringField(object, "unit")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.parameters", index, err)
		}
		value, err := requiredFloatField(object, "value")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.parameters", index, err)
		}
		parameters = append(parameters, ExpectedParameter{
			ID:    id,
			Name:  name,
			Type:  typeValue,
			Unit:  unit,
			Value: value,
		})
	}
	return parameters, nil
}

func parseExpectedMetadataField(root map[string]any) ([]ExpectedMetadata, error) {
	items, err := requiredArrayField(root, "metadata")
	if err != nil {
		return nil, wrapFieldError("expected", err)
	}
	entries := make([]ExpectedMetadata, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("expected.metadata[%d] must be an object", index)}
		}
		if err := ensureAllowedKeys(object, fmt.Sprintf("expected.metadata[%d]", index), "id", "key", "ownerId", "value", "valueKind"); err != nil {
			return nil, err
		}
		id := ""
		if rawID, ok := object["id"]; ok {
			typed, ok := rawID.(string)
			if !ok {
				return nil, &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].id must be a string", index)}
			}
			id = typed
		}
		key, err := requiredStringField(object, "key")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.metadata", index, err)
		}
		ownerID := ""
		if rawOwnerID, ok := object["ownerId"]; ok {
			switch typed := rawOwnerID.(type) {
			case nil:
				ownerID = ""
			case string:
				ownerID = typed
			default:
				return nil, &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].ownerId must be a string or null", index)}
			}
		}
		value, err := requiredStringField(object, "value")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.metadata", index, err)
		}
		valueKind := ""
		if rawValueKind, ok := object["valueKind"]; ok {
			typed, ok := rawValueKind.(string)
			if !ok {
				return nil, &ValidationError{Message: fmt.Sprintf("expected.metadata[%d].valueKind must be a string", index)}
			}
			valueKind = typed
		}
		entries = append(entries, ExpectedMetadata{ID: id, Key: key, OwnerID: ownerID, Value: value, ValueKind: valueKind})
	}
	return entries, nil
}

func parseExpectedReferencesField(root map[string]any) ([]ExpectedReference, error) {
	items, err := requiredArrayField(root, "references")
	if err != nil {
		return nil, wrapFieldError("expected", err)
	}
	entries := make([]ExpectedReference, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("expected.references[%d] must be an object", index)}
		}
		if err := ensureAllowedKeys(object, fmt.Sprintf("expected.references[%d]", index), "kind", "name"); err != nil {
			return nil, err
		}
		kind, err := requiredStringField(object, "kind")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.references", index, err)
		}
		name, err := requiredStringField(object, "name")
		if err != nil {
			return nil, wrapIndexedFieldError("expected.references", index, err)
		}
		entries = append(entries, ExpectedReference{Kind: kind, Name: name})
	}
	return entries, nil
}

func parseCheckField(root map[string]any, field string) (Check, error) {
	object, err := requiredObjectField(root, field)
	if err != nil {
		return Check{}, wrapFieldError("checks", err)
	}
	if err := ensureAllowedKeys(object, "checks."+field, "enabled"); err != nil {
		return Check{}, err
	}
	enabled, err := requiredBoolField(object, "enabled")
	if err != nil {
		return Check{}, wrapFieldError("checks."+field, err)
	}
	return Check{Enabled: enabled}, nil
}

func writeObserve(out *bytes.Buffer, observe Observe) {
	out.WriteByte('{')
	writeJSONString(out, "components")
	out.WriteByte(':')
	writeBool(out, observe.Components)
	out.WriteByte(',')
	writeJSONString(out, "parameters")
	out.WriteByte(':')
	writeBool(out, observe.Parameters)
	out.WriteByte(',')
	writeJSONString(out, "metadata")
	out.WriteByte(':')
	writeBool(out, observe.Metadata)
	out.WriteByte(',')
	writeJSONString(out, "references")
	out.WriteByte(':')
	writeBool(out, observe.References)
	out.WriteByte('}')
}

func writeObservationContext(out *bytes.Buffer, context ObservationContext) {
	ordered := append([]ObservedParameterBinding(nil), context.Parameters...)
	slices.SortFunc(ordered, compareObservedParameterBindings)

	out.WriteByte('{')
	writeJSONString(out, "parameters")
	out.WriteByte(':')
	out.WriteByte('[')
	for index, binding := range ordered {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(out, "id")
		out.WriteByte(':')
		writeJSONString(out, binding.ID)
		out.WriteByte(',')
		writeJSONString(out, "name")
		out.WriteByte(':')
		writeJSONString(out, binding.Name)
		out.WriteByte(',')
		writeJSONString(out, "groupName")
		out.WriteByte(':')
		if binding.GroupName == "" {
			out.WriteString("null")
		} else {
			writeJSONString(out, binding.GroupName)
		}
		out.WriteByte('}')
	}
	out.WriteByte(']')
	out.WriteByte('}')
}

func writeExpected(out *bytes.Buffer, expected Expected) {
	orderedComponents := append([]ExpectedComponent(nil), expected.Components...)
	slices.SortFunc(orderedComponents, compareExpectedComponents)
	orderedParameters := append([]ExpectedParameter(nil), expected.Parameters...)
	slices.SortFunc(orderedParameters, compareExpectedParameters)
	orderedMetadata := append([]ExpectedMetadata(nil), expected.Metadata...)
	slices.SortFunc(orderedMetadata, compareExpectedMetadata)

	out.WriteByte('{')
	writeJSONString(out, "components")
	out.WriteByte(':')
	out.WriteByte('[')
	for index, component := range orderedComponents {
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
	out.WriteByte(',')
	writeJSONString(out, "parameters")
	out.WriteByte(':')
	out.WriteByte('[')
	for index, parameter := range orderedParameters {
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
		writeJSONString(out, "type")
		out.WriteByte(':')
		writeJSONString(out, parameter.Type)
		out.WriteByte(',')
		writeJSONString(out, "unit")
		out.WriteByte(':')
		writeJSONString(out, parameter.Unit)
		out.WriteByte(',')
		writeJSONString(out, "value")
		out.WriteByte(':')
		writeFloat(out, parameter.Value)
		out.WriteByte('}')
	}
	out.WriteByte(']')
	out.WriteByte(',')
	writeJSONString(out, "metadata")
	out.WriteByte(':')
	out.WriteByte('[')
	for index, entry := range orderedMetadata {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		if entry.ID != "" {
			writeJSONString(out, "id")
			out.WriteByte(':')
			writeJSONString(out, entry.ID)
			out.WriteByte(',')
		}
		writeJSONString(out, "key")
		out.WriteByte(':')
		writeJSONString(out, entry.Key)
		out.WriteByte(',')
		if entry.ID != "" {
			writeJSONString(out, "ownerId")
			out.WriteByte(':')
			if entry.OwnerID == "" {
				out.WriteString("null")
			} else {
				writeJSONString(out, entry.OwnerID)
			}
			out.WriteByte(',')
		}
		writeJSONString(out, "value")
		out.WriteByte(':')
		writeJSONString(out, entry.Value)
		if entry.ID != "" {
			out.WriteByte(',')
			writeJSONString(out, "valueKind")
			out.WriteByte(':')
			writeJSONString(out, entry.ValueKind)
		}
		out.WriteByte('}')
	}
	out.WriteByte(']')
	out.WriteByte(',')
	writeJSONString(out, "references")
	out.WriteByte(':')
	out.WriteByte('[')
	for index, entry := range expected.References {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(out, "kind")
		out.WriteByte(':')
		writeJSONString(out, entry.Kind)
		out.WriteByte(',')
		writeJSONString(out, "name")
		out.WriteByte(':')
		writeJSONString(out, entry.Name)
		out.WriteByte('}')
	}
	out.WriteByte(']')
	out.WriteByte('}')
}

func writeChecks(out *bytes.Buffer, checks Checks) {
	out.WriteByte('{')
	writeJSONString(out, "components")
	out.WriteByte(':')
	writeCheck(out, checks.Components)
	out.WriteByte(',')
	writeJSONString(out, "parameters")
	out.WriteByte(':')
	writeCheck(out, checks.Parameters)
	out.WriteByte(',')
	writeJSONString(out, "metadata")
	out.WriteByte(':')
	writeCheck(out, checks.Metadata)
	out.WriteByte(',')
	writeJSONString(out, "references")
	out.WriteByte(':')
	writeCheck(out, checks.References)
	out.WriteByte('}')
}

func writeCheck(out *bytes.Buffer, check Check) {
	out.WriteByte('{')
	writeJSONString(out, "enabled")
	out.WriteByte(':')
	writeBool(out, check.Enabled)
	out.WriteByte('}')
}

func writeJSONString(out *bytes.Buffer, value string) {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	out.Write(data)
}

func writeBool(out *bytes.Buffer, value bool) {
	if value {
		out.WriteString("true")
		return
	}
	out.WriteString("false")
}

func writeFloat(out *bytes.Buffer, value float64) {
	out.WriteString(strconv.FormatFloat(value, 'f', -1, 64))
}

func compareExpectedParameters(left, right ExpectedParameter) int {
	if left.ID != right.ID {
		return strings.Compare(left.ID, right.ID)
	}
	if left.Name != right.Name {
		return strings.Compare(left.Name, right.Name)
	}
	if left.Type != right.Type {
		return strings.Compare(left.Type, right.Type)
	}
	if left.Unit != right.Unit {
		return strings.Compare(left.Unit, right.Unit)
	}
	if left.Value < right.Value {
		return -1
	}
	if left.Value > right.Value {
		return 1
	}
	return 0
}

func compareExpectedComponents(left, right ExpectedComponent) int {
	if left.ID != right.ID {
		return strings.Compare(left.ID, right.ID)
	}
	if left.Kind != right.Kind {
		return strings.Compare(left.Kind, right.Kind)
	}
	if left.Name != right.Name {
		return strings.Compare(left.Name, right.Name)
	}
	return strings.Compare(left.ParentID, right.ParentID)
}

func compareExpectedMetadata(left, right ExpectedMetadata) int {
	if left.ID != right.ID {
		return strings.Compare(left.ID, right.ID)
	}
	if left.Key != right.Key {
		return strings.Compare(left.Key, right.Key)
	}
	if left.OwnerID != right.OwnerID {
		return strings.Compare(left.OwnerID, right.OwnerID)
	}
	if left.Value != right.Value {
		return strings.Compare(left.Value, right.Value)
	}
	return strings.Compare(left.ValueKind, right.ValueKind)
}

func isSupportedExpectedComponentKind(kind string) bool {
	switch kind {
	case observed.ComponentKindAssembly, observed.ComponentKindPart:
		return true
	default:
		return false
	}
}

func isSupportedExpectedMetadataValueKind(kind string) bool {
	switch kind {
	case "number", "integer", "string", "boolean":
		return true
	default:
		return false
	}
}

func expectedOptionalIdentity(value string) string {
	if value == "" {
		return "null"
	}
	return value
}

func compareObservedParameterBindings(left, right ObservedParameterBinding) int {
	if left.ID != right.ID {
		return strings.Compare(left.ID, right.ID)
	}
	if left.Name != right.Name {
		return strings.Compare(left.Name, right.Name)
	}
	return strings.Compare(left.GroupName, right.GroupName)
}

func ensureAllowedKeys(root map[string]any, context string, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range root {
		if _, ok := allowedSet[key]; ok {
			continue
		}
		if context == "root" {
			return &ValidationError{Message: fmt.Sprintf("unknown field %q", key)}
		}
		return &ValidationError{Message: fmt.Sprintf("%s contains unknown field %q", context, key)}
	}
	return nil
}

func requiredStringField(root map[string]any, field string) (string, error) {
	value, ok := root[field]
	if !ok {
		return "", &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	text, ok := value.(string)
	if !ok {
		return "", &ValidationError{Message: fmt.Sprintf("%s must be a string", field)}
	}
	if strings.TrimSpace(text) == "" {
		return "", &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	return text, nil
}

func requiredObjectField(root map[string]any, field string) (map[string]any, error) {
	value, ok := root[field]
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("%s must be an object", field)}
	}
	return object, nil
}

func requiredArrayField(root map[string]any, field string) ([]any, error) {
	value, ok := root[field]
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	items, ok := value.([]any)
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("%s must be an array", field)}
	}
	return items, nil
}

func requiredBoolField(root map[string]any, field string) (bool, error) {
	value, ok := root[field]
	if !ok {
		return false, &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	flag, ok := value.(bool)
	if !ok {
		return false, &ValidationError{Message: fmt.Sprintf("%s must be a boolean", field)}
	}
	return flag, nil
}

func requiredFloatField(root map[string]any, field string) (float64, error) {
	value, ok := root[field]
	if !ok {
		return 0, &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	switch typed := value.(type) {
	case json.Number:
		floatValue, err := typed.Float64()
		if err != nil {
			return 0, &ValidationError{Message: fmt.Sprintf("%s must be a number", field)}
		}
		return floatValue, nil
	case float64:
		return typed, nil
	default:
		return 0, &ValidationError{Message: fmt.Sprintf("%s must be a number", field)}
	}
}

func wrapFieldError(context string, err error) error {
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	return &ValidationError{Message: context + "." + validationErr.Message}
}

func wrapIndexedFieldError(context string, index int, err error) error {
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	return &ValidationError{Message: fmt.Sprintf("%s[%d].%s", context, index, validationErr.Message)}
}

func ensureNoTrailingJSON(decoder *json.Decoder) error {
	if decoder == nil {
		return nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
