package freecad

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"parametron/internal/engine/artifact"
)

const (
	FreeCADRuntimeResultSchemaVersion = "1.0"

	FreeCADRuntimeResultStatusSucceeded FreeCADRuntimeResultStatus = "succeeded"
	FreeCADRuntimeResultStatusFailed    FreeCADRuntimeResultStatus = "failed"
)

type FreeCADRuntimeResultStatus string

var (
	ErrFreeCADRuntimeResultIO         = errors.New("FreeCAD runtime result I/O error")
	ErrFreeCADRuntimeResultDecode     = errors.New("FreeCAD runtime result decode error")
	ErrFreeCADRuntimeResultValidation = errors.New("FreeCAD runtime result validation error")
)

type FreeCADRuntimeResultFileError struct {
	Path string
	Err  error
}

func (e *FreeCADRuntimeResultFileError) Error() string {
	if e == nil {
		return ErrFreeCADRuntimeResultIO.Error()
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *FreeCADRuntimeResultFileError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrFreeCADRuntimeResultIO}
	}
	return []error{ErrFreeCADRuntimeResultIO, e.Err}
}

type FreeCADRuntimeResultDecodeError struct {
	Err error
}

func (e *FreeCADRuntimeResultDecodeError) Error() string {
	if e == nil || e.Err == nil {
		return ErrFreeCADRuntimeResultDecode.Error()
	}
	return fmt.Sprintf("%s: %v", ErrFreeCADRuntimeResultDecode, e.Err)
}

func (e *FreeCADRuntimeResultDecodeError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrFreeCADRuntimeResultDecode}
	}
	return []error{ErrFreeCADRuntimeResultDecode, e.Err}
}

type FreeCADRuntimeResultValidationError struct {
	Message string
}

func (e *FreeCADRuntimeResultValidationError) Error() string {
	if e == nil || e.Message == "" {
		return ErrFreeCADRuntimeResultValidation.Error()
	}
	return fmt.Sprintf("%s: %s", ErrFreeCADRuntimeResultValidation, e.Message)
}

func (e *FreeCADRuntimeResultValidationError) Unwrap() error {
	return ErrFreeCADRuntimeResultValidation
}

// FreeCADRuntimeResult is the normalized aligned FreeCAD result.json contract.
type FreeCADRuntimeResult struct {
	SchemaVersion string
	Status        FreeCADRuntimeResultStatus
	Artifacts     []FreeCADRuntimeResultArtifact
	Failure       *FreeCADRuntimeResultFailure
}

// FreeCADRuntimeResultArtifact is one success artifact declaration.
type FreeCADRuntimeResultArtifact struct {
	ID     string
	Format string
	Path   string
}

// FreeCADRuntimeResultFailure carries aligned runtime-native failure facts.
// Stage == nil represents JSON null.
type FreeCADRuntimeResultFailure struct {
	Boundary string
	Category string
	Code     string
	Message  string
	Stage    *string
}

// ParseFreeCADRuntimeResult decodes and validates aligned FreeCAD result.json bytes.
func ParseFreeCADRuntimeResult(data []byte) (*FreeCADRuntimeResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, &FreeCADRuntimeResultDecodeError{Err: err}
	}
	if err := ensureNoTrailingFreeCADRuntimeResultJSON(decoder); err != nil {
		return nil, &FreeCADRuntimeResultDecodeError{Err: err}
	}

	root, ok := raw.(map[string]any)
	if !ok {
		return nil, freeCADRuntimeResultValidationErrorf("root must be a JSON object")
	}

	result, err := parseFreeCADRuntimeResultObject(root)
	if err != nil {
		return nil, err
	}
	if err := ValidateFreeCADRuntimeResult(result); err != nil {
		return nil, err
	}
	return copyFreeCADRuntimeResult(result), nil
}

// LoadFreeCADRuntimeResult reads and parses aligned FreeCAD result.json from path.
func LoadFreeCADRuntimeResult(path string) (*FreeCADRuntimeResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FreeCADRuntimeResultFileError{Path: path, Err: err}
	}
	return ParseFreeCADRuntimeResult(data)
}

// ValidateFreeCADRuntimeResult validates a normalized FreeCAD runtime result value.
func ValidateFreeCADRuntimeResult(result *FreeCADRuntimeResult) error {
	if result == nil {
		return freeCADRuntimeResultValidationErrorf("result must not be nil")
	}
	if result.SchemaVersion != FreeCADRuntimeResultSchemaVersion {
		return freeCADRuntimeResultValidationErrorf("schemaVersion must be %q", FreeCADRuntimeResultSchemaVersion)
	}
	if result.Artifacts == nil {
		return freeCADRuntimeResultValidationErrorf("artifacts is required")
	}

	switch result.Status {
	case FreeCADRuntimeResultStatusSucceeded:
		if result.Failure != nil {
			return freeCADRuntimeResultValidationErrorf("succeeded result must not contain failure")
		}
		for i, entry := range result.Artifacts {
			if err := validateFreeCADRuntimeResultArtifact(entry); err != nil {
				return wrapFreeCADRuntimeResultIndexedFieldError("artifacts", i, err)
			}
		}
	case FreeCADRuntimeResultStatusFailed:
		if len(result.Artifacts) != 0 {
			return freeCADRuntimeResultValidationErrorf("failed result must not contain artifacts")
		}
		if result.Failure == nil {
			return freeCADRuntimeResultValidationErrorf("failed result requires failure")
		}
		if err := validateFreeCADRuntimeResultFailure(result.Failure); err != nil {
			return wrapFreeCADRuntimeResultFieldError("failure", err)
		}
	default:
		return freeCADRuntimeResultValidationErrorf("unsupported aligned status %q", result.Status)
	}
	return nil
}

func parseFreeCADRuntimeResultObject(root map[string]any) (*FreeCADRuntimeResult, error) {
	if err := ensureAllowedFreeCADRuntimeResultKeys(root, "result", "schemaVersion", "status", "artifacts", "failure"); err != nil {
		return nil, err
	}

	schemaVersion, err := requiredFreeCADRuntimeResultStringField(root, "schemaVersion")
	if err != nil {
		return nil, err
	}
	if schemaVersion != FreeCADRuntimeResultSchemaVersion {
		return nil, freeCADRuntimeResultValidationErrorf("schemaVersion must be %q", FreeCADRuntimeResultSchemaVersion)
	}

	statusValue, err := requiredFreeCADRuntimeResultStringField(root, "status")
	if err != nil {
		return nil, err
	}
	status := FreeCADRuntimeResultStatus(statusValue)
	switch status {
	case FreeCADRuntimeResultStatusSucceeded, FreeCADRuntimeResultStatusFailed:
	default:
		return nil, freeCADRuntimeResultValidationErrorf("unsupported aligned status %q", statusValue)
	}

	_, hasArtifacts := root["artifacts"]
	_, hasFailure := root["failure"]

	result := &FreeCADRuntimeResult{
		SchemaVersion: schemaVersion,
		Status:        status,
		Artifacts:     []FreeCADRuntimeResultArtifact{},
	}

	switch status {
	case FreeCADRuntimeResultStatusSucceeded:
		if hasFailure {
			return nil, freeCADRuntimeResultValidationErrorf("succeeded result must not contain failure")
		}
		if !hasArtifacts {
			return nil, freeCADRuntimeResultValidationErrorf("succeeded result requires artifacts")
		}
		artifacts, err := parseFreeCADRuntimeResultArtifacts(root["artifacts"])
		if err != nil {
			return nil, err
		}
		result.Artifacts = artifacts
	case FreeCADRuntimeResultStatusFailed:
		if hasArtifacts {
			return nil, freeCADRuntimeResultValidationErrorf("failed result must not contain artifacts")
		}
		if !hasFailure {
			return nil, freeCADRuntimeResultValidationErrorf("failed result requires failure")
		}
		failure, err := parseFreeCADRuntimeResultFailure(root["failure"])
		if err != nil {
			return nil, err
		}
		result.Failure = failure
	}

	return result, nil
}

func parseFreeCADRuntimeResultArtifacts(raw any) ([]FreeCADRuntimeResultArtifact, error) {
	if raw == nil {
		return nil, freeCADRuntimeResultValidationErrorf("artifacts must be an array")
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, freeCADRuntimeResultValidationErrorf("artifacts must be an array")
	}

	artifacts := make([]FreeCADRuntimeResultArtifact, 0, len(items))
	for i, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, freeCADRuntimeResultValidationErrorf("artifacts[%d] must be an object", i)
		}
		artifactEntry, err := parseFreeCADRuntimeResultArtifact(object, i)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifactEntry)
	}
	return artifacts, nil
}

func parseFreeCADRuntimeResultArtifact(object map[string]any, index int) (FreeCADRuntimeResultArtifact, error) {
	if err := ensureAllowedFreeCADRuntimeResultKeys(object, fmt.Sprintf("artifacts[%d]", index), "id", "format", "path"); err != nil {
		return FreeCADRuntimeResultArtifact{}, err
	}

	id, err := requiredFreeCADRuntimeResultNonEmptyStringField(object, "id")
	if err != nil {
		return FreeCADRuntimeResultArtifact{}, wrapFreeCADRuntimeResultIndexedFieldError("artifacts", index, err)
	}
	format, err := requiredFreeCADRuntimeResultNonEmptyStringField(object, "format")
	if err != nil {
		return FreeCADRuntimeResultArtifact{}, wrapFreeCADRuntimeResultIndexedFieldError("artifacts", index, err)
	}
	if !isExactFreeCADRuntimeResultArtifactFormat(format) {
		return FreeCADRuntimeResultArtifact{}, freeCADRuntimeResultValidationErrorf(
			"artifacts[%d].format %q is not supported", index, format,
		)
	}
	path, err := requiredFreeCADRuntimeResultNonEmptyStringField(object, "path")
	if err != nil {
		return FreeCADRuntimeResultArtifact{}, wrapFreeCADRuntimeResultIndexedFieldError("artifacts", index, err)
	}

	return FreeCADRuntimeResultArtifact{
		ID:     id,
		Format: format,
		Path:   path,
	}, nil
}

func parseFreeCADRuntimeResultFailure(raw any) (*FreeCADRuntimeResultFailure, error) {
	if raw == nil {
		return nil, freeCADRuntimeResultValidationErrorf("failure must be an object")
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, freeCADRuntimeResultValidationErrorf("failure must be an object")
	}
	if err := ensureAllowedFreeCADRuntimeResultKeys(object, "failure", "boundary", "category", "code", "message", "stage"); err != nil {
		return nil, err
	}

	boundary, err := requiredFreeCADRuntimeResultNonEmptyStringField(object, "boundary")
	if err != nil {
		return nil, wrapFreeCADRuntimeResultFieldError("failure", err)
	}
	category, err := requiredFreeCADRuntimeResultNonEmptyStringField(object, "category")
	if err != nil {
		return nil, wrapFreeCADRuntimeResultFieldError("failure", err)
	}
	code, err := requiredFreeCADRuntimeResultNonEmptyStringField(object, "code")
	if err != nil {
		return nil, wrapFreeCADRuntimeResultFieldError("failure", err)
	}
	message, err := requiredFreeCADRuntimeResultNonEmptyStringField(object, "message")
	if err != nil {
		return nil, wrapFreeCADRuntimeResultFieldError("failure", err)
	}
	stage, err := optionalFreeCADRuntimeResultStageField(object, "stage")
	if err != nil {
		return nil, wrapFreeCADRuntimeResultFieldError("failure", err)
	}

	return &FreeCADRuntimeResultFailure{
		Boundary: boundary,
		Category: category,
		Code:     code,
		Message:  message,
		Stage:    stage,
	}, nil
}

func validateFreeCADRuntimeResultArtifact(entry FreeCADRuntimeResultArtifact) error {
	if entry.ID == "" {
		return freeCADRuntimeResultValidationErrorf("id must be a non-empty string")
	}
	if entry.Format == "" {
		return freeCADRuntimeResultValidationErrorf("format must be a non-empty string")
	}
	if !isExactFreeCADRuntimeResultArtifactFormat(entry.Format) {
		return freeCADRuntimeResultValidationErrorf("format %q is not supported", entry.Format)
	}
	if entry.Path == "" {
		return freeCADRuntimeResultValidationErrorf("path must be a non-empty string")
	}
	return nil
}

func validateFreeCADRuntimeResultFailure(failure *FreeCADRuntimeResultFailure) error {
	if failure == nil {
		return freeCADRuntimeResultValidationErrorf("failure must not be nil")
	}
	if failure.Boundary == "" {
		return freeCADRuntimeResultValidationErrorf("boundary must be a non-empty string")
	}
	if failure.Category == "" {
		return freeCADRuntimeResultValidationErrorf("category must be a non-empty string")
	}
	if failure.Code == "" {
		return freeCADRuntimeResultValidationErrorf("code must be a non-empty string")
	}
	if failure.Message == "" {
		return freeCADRuntimeResultValidationErrorf("message must be a non-empty string")
	}
	if failure.Stage != nil && *failure.Stage == "" {
		return freeCADRuntimeResultValidationErrorf("stage must be a non-empty string when provided")
	}
	return nil
}

func isExactFreeCADRuntimeResultArtifactFormat(format string) bool {
	switch format {
	case artifact.ExportOutputTypeSTEP, artifact.ExportOutputTypeCSV, artifact.ExportOutputTypePDF:
		return true
	default:
		return false
	}
}

func copyFreeCADRuntimeResult(result *FreeCADRuntimeResult) *FreeCADRuntimeResult {
	if result == nil {
		return nil
	}

	out := &FreeCADRuntimeResult{
		SchemaVersion: result.SchemaVersion,
		Status:        result.Status,
		Artifacts:     make([]FreeCADRuntimeResultArtifact, len(result.Artifacts)),
	}
	copy(out.Artifacts, result.Artifacts)
	if out.Artifacts == nil {
		out.Artifacts = []FreeCADRuntimeResultArtifact{}
	}

	if result.Failure != nil {
		failure := *result.Failure
		if result.Failure.Stage != nil {
			stage := *result.Failure.Stage
			failure.Stage = &stage
		}
		out.Failure = &failure
	}
	return out
}

func ensureNoTrailingFreeCADRuntimeResultJSON(decoder *json.Decoder) error {
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

func ensureAllowedFreeCADRuntimeResultKeys(object map[string]any, location string, allowed ...string) error {
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
	return freeCADRuntimeResultValidationErrorf("%s has unknown field %q", location, unknown[0])
}

func requiredFreeCADRuntimeResultStringField(root map[string]any, field string) (string, error) {
	value, ok := root[field]
	if !ok {
		return "", freeCADRuntimeResultValidationErrorf("%s is required", field)
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", freeCADRuntimeResultValidationErrorf("%s must be a string", field)
	}
	return stringValue, nil
}

func requiredFreeCADRuntimeResultNonEmptyStringField(root map[string]any, field string) (string, error) {
	stringValue, err := requiredFreeCADRuntimeResultStringField(root, field)
	if err != nil {
		return "", err
	}
	if stringValue == "" {
		return "", freeCADRuntimeResultValidationErrorf("%s must be a non-empty string", field)
	}
	return stringValue, nil
}

func optionalFreeCADRuntimeResultStageField(root map[string]any, field string) (*string, error) {
	value, ok := root[field]
	if !ok {
		return nil, freeCADRuntimeResultValidationErrorf("%s is required", field)
	}
	if value == nil {
		return nil, nil
	}
	stringValue, ok := value.(string)
	if !ok {
		return nil, freeCADRuntimeResultValidationErrorf("%s must be a string or null", field)
	}
	if stringValue == "" {
		return nil, freeCADRuntimeResultValidationErrorf("%s must be a non-empty string when provided", field)
	}
	return &stringValue, nil
}

func freeCADRuntimeResultValidationErrorf(format string, args ...any) error {
	return &FreeCADRuntimeResultValidationError{Message: fmt.Sprintf(format, args...)}
}

func wrapFreeCADRuntimeResultFieldError(prefix string, err error) error {
	var validationErr *FreeCADRuntimeResultValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	if validationErr == nil || validationErr.Message == "" {
		return err
	}
	return &FreeCADRuntimeResultValidationError{Message: prefix + "." + validationErr.Message}
}

func wrapFreeCADRuntimeResultIndexedFieldError(prefix string, index int, err error) error {
	var validationErr *FreeCADRuntimeResultValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	if validationErr == nil || validationErr.Message == "" {
		return err
	}
	return &FreeCADRuntimeResultValidationError{Message: fmt.Sprintf("%s[%d].%s", prefix, index, validationErr.Message)}
}
