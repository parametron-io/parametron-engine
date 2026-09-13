package projectlock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"parametron/internal/engine/projectinput"
	"parametron/internal/shared/projectmap"
)

const (
	FileName = "parametron.lock.json"
	Version  = "1.0"
)

var (
	ErrIO         = errors.New("project lock I/O error")
	ErrDecode     = errors.New("project lock decode error")
	ErrValidation = errors.New("project lock validation error")
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
	Problems []string
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Problems) == 0 {
		return ErrValidation.Error()
	}
	return fmt.Sprintf("%s: %s", ErrValidation, strings.Join(e.Problems, "; "))
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

func (e *ValidationError) Messages() []string {
	if e == nil {
		return nil
	}
	out := make([]string, len(e.Problems))
	copy(out, e.Problems)
	return out
}

type LockFile struct {
	Version   string                      `json:"version"`
	ProjectID string                      `json:"projectId"`
	DSL       LockedDSLEntry              `json:"dsl"`
	Resources LockedResources             `json:"resources"`
	Tables    map[string]LockedTableEntry `json:"tables,omitempty"`
}

type LockedDSLEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type LockedModelEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type LockedTableEntry struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
}

type LockedResources struct {
	Models map[string]LockedModelEntry `json:"models"`
}

func Read(data []byte) (*LockFile, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))

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

	lock, err := parseLockFile(root)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func Load(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{Path: path, Err: err}
	}
	return Read(data)
}

func Validate(lock *LockFile) error {
	_, err := normalized(lock)
	return err
}

func Write(path string, lock *LockFile) error {
	normalizedLock, err := normalized(lock)
	if err != nil {
		return err
	}

	data, err := marshal(normalizedLock)
	if err != nil {
		return &FileError{Path: path, Err: err}
	}

	parentDir := filepath.Dir(path)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return &FileError{Path: path, Err: err}
	}

	tmpFile, err := os.CreateTemp(parentDir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return &FileError{Path: path, Err: err}
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: err}
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: err}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return &FileError{Path: path, Err: err}
	}

	return nil
}

func Build(project *projectmap.Project, _ string, captured *projectinput.CapturedResources) (*LockFile, error) {
	var problems []string
	if project == nil {
		problems = append(problems, "project mapping must not be nil")
	}
	if captured == nil {
		problems = append(problems, "captured resources must not be nil")
	}
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}

	projectID, err := validateAndNormalizeID("projectId", project.ProjectID)
	if err != nil {
		return nil, &ValidationError{Problems: []string{err.Error()}}
	}

	dslPath, err := normalizeRelativePath(project.DSL)
	if err != nil {
		return nil, &ValidationError{Problems: []string{fmt.Sprintf("dsl.path %s", err.Error())}}
	}

	modelIDs := sortedMapKeys(project.Resources.Models)
	modelEntries := make(map[string]LockedModelEntry, len(modelIDs))
	capturedModels := make(map[string]projectinput.CapturedResource, len(captured.Models))

	for _, resource := range captured.Models {
		if _, exists := capturedModels[resource.LogicalID]; exists {
			return nil, &ValidationError{Problems: []string{fmt.Sprintf("captured resources contain duplicate model logical ID %q", resource.LogicalID)}}
		}
		capturedModels[resource.LogicalID] = resource
	}

	for _, logicalID := range modelIDs {
		if err := validateLogicalID("resources.models", logicalID); err != nil {
			return nil, &ValidationError{Problems: []string{err.Error()}}
		}

		resource, ok := capturedModels[logicalID]
		if !ok {
			return nil, &ValidationError{Problems: []string{fmt.Sprintf("captured resources missing model logical ID %q", logicalID)}}
		}

		modelPath, err := normalizeRelativePath(project.Resources.Models[logicalID])
		if err != nil {
			return nil, &ValidationError{Problems: []string{fmt.Sprintf("resources.models[%q].path %s", logicalID, err.Error())}}
		}

		modelEntries[logicalID] = LockedModelEntry{
			Path:   modelPath,
			SHA256: resource.Signature,
		}
	}

	extraModelIDs := extraCapturedLogicalIDs(captured.Models, project.Resources.Models)
	if len(extraModelIDs) > 0 {
		return nil, &ValidationError{Problems: []string{fmt.Sprintf("captured resources have extra model logical IDs: %s", quotedList(extraModelIDs))}}
	}

	tableIDs := sortedMapKeys(project.Tables)
	tableEntries := make(map[string]LockedTableEntry, len(tableIDs))
	capturedTables := make(map[string]projectinput.CapturedResource, len(captured.Tables))

	for _, resource := range captured.Tables {
		if _, exists := capturedTables[resource.LogicalID]; exists {
			return nil, &ValidationError{Problems: []string{fmt.Sprintf("captured resources contain duplicate table logical ID %q", resource.LogicalID)}}
		}
		capturedTables[resource.LogicalID] = resource
	}

	for _, logicalID := range tableIDs {
		if err := validateLogicalID("tables", logicalID); err != nil {
			return nil, &ValidationError{Problems: []string{err.Error()}}
		}

		resource, ok := capturedTables[logicalID]
		if !ok {
			return nil, &ValidationError{Problems: []string{fmt.Sprintf("captured resources missing table logical ID %q", logicalID)}}
		}

		tablePath, err := normalizeRelativePath(project.Tables[logicalID])
		if err != nil {
			return nil, &ValidationError{Problems: []string{fmt.Sprintf("tables[%q].path %s", logicalID, err.Error())}}
		}

		tableEntries[logicalID] = LockedTableEntry{
			Path:        tablePath,
			Fingerprint: resource.Signature,
		}
	}

	extraTableIDs := extraCapturedLogicalIDs(captured.Tables, project.Tables)
	if len(extraTableIDs) > 0 {
		return nil, &ValidationError{Problems: []string{fmt.Sprintf("captured resources have extra table logical IDs: %s", quotedList(extraTableIDs))}}
	}

	lock := &LockFile{
		Version:   Version,
		ProjectID: projectID,
		DSL: LockedDSLEntry{
			Path:   dslPath,
			SHA256: captured.DSL.Signature,
		},
		Resources: LockedResources{
			Models: modelEntries,
		},
	}
	if len(tableEntries) > 0 {
		lock.Tables = tableEntries
	}

	return normalized(lock)
}

func parseLockFile(root map[string]any) (*LockFile, error) {
	var problems []string

	if err := ensureAllowedKeys(root, "root", "version", "projectId", "dsl", "resources", "tables"); err != nil {
		problems = append(problems, err...)
	}

	version, errs := requiredStringField(root, "version")
	problems = append(problems, errs...)

	projectID, errs := requiredStringField(root, "projectId")
	problems = append(problems, errs...)

	dslObject, errs := requiredObjectField(root, "dsl")
	problems = append(problems, errs...)

	resourcesObject, errs := requiredObjectField(root, "resources")
	problems = append(problems, errs...)

	var dsl LockedDSLEntry
	if dslObject != nil {
		if err := ensureAllowedKeys(dslObject, "dsl", "path", "sha256"); err != nil {
			problems = append(problems, err...)
		}
		pathValue, fieldErrs := requiredStringFieldWithContext(dslObject, "dsl", "path")
		problems = append(problems, fieldErrs...)
		shaValue, fieldErrs := requiredStringFieldWithContext(dslObject, "dsl", "sha256")
		problems = append(problems, fieldErrs...)
		dsl = LockedDSLEntry{
			Path:   pathValue,
			SHA256: shaValue,
		}
	}

	models := map[string]LockedModelEntry(nil)
	if resourcesObject != nil {
		if err := ensureAllowedKeys(resourcesObject, "resources", "models"); err != nil {
			problems = append(problems, err...)
		}
		modelsObject, modelErrs := requiredObjectField(resourcesObject, "models")
		problems = append(problems, modelErrs...)
		if modelsObject != nil {
			var valueErrs []string
			models, valueErrs = parseModels(modelsObject)
			problems = append(problems, valueErrs...)
		}
	}

	tables := map[string]LockedTableEntry(nil)
	if rawTables, ok := root["tables"]; ok {
		tablesObject, ok := rawTables.(map[string]any)
		if !ok {
			problems = append(problems, "tables must be an object")
		} else {
			var valueErrs []string
			tables, valueErrs = parseTables(tablesObject)
			problems = append(problems, valueErrs...)
		}
	}

	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}

	lock := &LockFile{
		Version:   version,
		ProjectID: projectID,
		DSL:       dsl,
		Resources: LockedResources{Models: models},
		Tables:    tables,
	}
	return normalized(lock)
}

func parseModels(object map[string]any) (map[string]LockedModelEntry, []string) {
	out := make(map[string]LockedModelEntry, len(object))
	keys := sortedAnyMapKeys(object)

	var problems []string
	for _, logicalID := range keys {
		context := fmt.Sprintf("resources.models[%q]", logicalID)
		rawEntry := object[logicalID]
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s must be an object", context))
			continue
		}
		if err := ensureAllowedKeys(entry, context, "path", "sha256"); err != nil {
			problems = append(problems, err...)
		}
		pathValue, errs := requiredStringFieldWithContext(entry, context, "path")
		problems = append(problems, errs...)
		shaValue, errs := requiredStringFieldWithContext(entry, context, "sha256")
		problems = append(problems, errs...)
		out[logicalID] = LockedModelEntry{
			Path:   pathValue,
			SHA256: shaValue,
		}
	}

	return out, problems
}

func parseTables(object map[string]any) (map[string]LockedTableEntry, []string) {
	out := make(map[string]LockedTableEntry, len(object))
	keys := sortedAnyMapKeys(object)

	var problems []string
	for _, logicalID := range keys {
		context := fmt.Sprintf("tables[%q]", logicalID)
		rawEntry := object[logicalID]
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s must be an object", context))
			continue
		}
		if err := ensureAllowedKeys(entry, context, "path", "fingerprint"); err != nil {
			problems = append(problems, err...)
		}
		pathValue, errs := requiredStringFieldWithContext(entry, context, "path")
		problems = append(problems, errs...)
		fingerprintValue, errs := requiredStringFieldWithContext(entry, context, "fingerprint")
		problems = append(problems, errs...)
		out[logicalID] = LockedTableEntry{
			Path:        pathValue,
			Fingerprint: fingerprintValue,
		}
	}

	return out, problems
}

func normalized(lock *LockFile) (*LockFile, error) {
	if lock == nil {
		return nil, &ValidationError{Problems: []string{"project lock must not be nil"}}
	}

	var problems []string

	if lock.Version == "" {
		problems = append(problems, "version is required")
	} else if lock.Version != Version {
		problems = append(problems, fmt.Sprintf("version must be %q", Version))
	}

	projectID, err := validateAndNormalizeID("projectId", lock.ProjectID)
	if err != nil {
		problems = append(problems, err.Error())
	}

	dslPath, err := normalizeRelativePath(lock.DSL.Path)
	if err != nil {
		problems = append(problems, fmt.Sprintf("dsl.path %s", err.Error()))
	}
	dslHash, err := normalizeSHA256("dsl.sha256", lock.DSL.SHA256)
	if err != nil {
		problems = append(problems, err.Error())
	}

	if lock.Resources.Models == nil {
		problems = append(problems, "resources.models is required")
	}

	modelIDs := sortedLockedModelIDs(lock.Resources.Models)
	normalizedModels := make(map[string]LockedModelEntry, len(modelIDs))
	for _, logicalID := range modelIDs {
		if err := validateLogicalID("resources.models", logicalID); err != nil {
			problems = append(problems, err.Error())
			continue
		}

		entry := lock.Resources.Models[logicalID]
		modelPath, pathErr := normalizeRelativePath(entry.Path)
		if pathErr != nil {
			problems = append(problems, fmt.Sprintf("resources.models[%q].path %s", logicalID, pathErr.Error()))
		}
		modelHash, hashErr := normalizeSHA256(fmt.Sprintf("resources.models[%q].sha256", logicalID), entry.SHA256)
		if hashErr != nil {
			problems = append(problems, hashErr.Error())
		}
		if pathErr == nil && hashErr == nil {
			normalizedModels[logicalID] = LockedModelEntry{
				Path:   modelPath,
				SHA256: modelHash,
			}
		}
	}

	tableIDs := sortedLockedTableIDs(lock.Tables)
	var normalizedTables map[string]LockedTableEntry
	if len(tableIDs) > 0 {
		normalizedTables = make(map[string]LockedTableEntry, len(tableIDs))
	}
	for _, logicalID := range tableIDs {
		if err := validateLogicalID("tables", logicalID); err != nil {
			problems = append(problems, err.Error())
			continue
		}

		entry := lock.Tables[logicalID]
		tablePath, pathErr := normalizeRelativePath(entry.Path)
		if pathErr != nil {
			problems = append(problems, fmt.Sprintf("tables[%q].path %s", logicalID, pathErr.Error()))
		}
		fingerprint, fingerprintErr := normalizeNonEmptyString(fmt.Sprintf("tables[%q].fingerprint", logicalID), entry.Fingerprint)
		if fingerprintErr != nil {
			problems = append(problems, fingerprintErr.Error())
		}
		if pathErr == nil && fingerprintErr == nil {
			normalizedTables[logicalID] = LockedTableEntry{
				Path:        tablePath,
				Fingerprint: fingerprint,
			}
		}
	}

	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}

	return &LockFile{
		Version:   Version,
		ProjectID: projectID,
		DSL: LockedDSLEntry{
			Path:   dslPath,
			SHA256: dslHash,
		},
		Resources: LockedResources{
			Models: normalizedModels,
		},
		Tables: normalizedTables,
	}, nil
}

func marshal(lock *LockFile) ([]byte, error) {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func validateAndNormalizeID(field, value string) (string, error) {
	if value == "" || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if value != strings.TrimSpace(value) {
		return "", fmt.Errorf("%s must not have leading or trailing whitespace", field)
	}
	return value, nil
}

func validateLogicalID(context, id string) error {
	if id == "" || strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s logical ID must not be empty", context)
	}
	if id != strings.TrimSpace(id) {
		return fmt.Errorf("%s logical ID %q must not have leading or trailing whitespace", context, id)
	}
	return nil
}

func normalizeSHA256(field, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if value != strings.TrimSpace(value) {
		return "", fmt.Errorf("%s must not have leading or trailing whitespace", field)
	}
	if !isLowerHex(value) {
		return "", fmt.Errorf("%s must be lowercase hexadecimal", field)
	}
	return value, nil
}

func normalizeNonEmptyString(field, value string) (string, error) {
	if value == "" || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if value != strings.TrimSpace(value) {
		return "", fmt.Errorf("%s must not have leading or trailing whitespace", field)
	}
	return value, nil
}

func isLowerHex(value string) bool {
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func normalizeRelativePath(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) == "" {
		return "", errors.New("must not be empty")
	}
	if raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("%q must not have leading or trailing whitespace", raw)
	}
	if strings.Contains(raw, "\x00") {
		return "", fmt.Errorf("%q must not contain a null byte", raw)
	}
	if isSlashAbsolute(raw) {
		return "", fmt.Errorf("%q must be relative", raw)
	}

	slashPath := strings.ReplaceAll(raw, "\\", "/")
	if isWindowsAbsolute(slashPath) {
		return "", fmt.Errorf("%q must be relative", raw)
	}

	cleaned := path.Clean(slashPath)
	if cleaned == "." {
		return "", fmt.Errorf("%q must not resolve to current directory", raw)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%q must stay within project root", raw)
	}
	return cleaned, nil
}

func isSlashAbsolute(value string) bool {
	return strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") || strings.HasPrefix(value, "//") || strings.HasPrefix(value, "\\\\")
}

func isWindowsAbsolute(value string) bool {
	if len(value) < 3 {
		return false
	}
	drive := value[0]
	if (drive < 'A' || drive > 'Z') && (drive < 'a' || drive > 'z') {
		return false
	}
	return value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func requiredStringField(object map[string]any, field string) (string, []string) {
	value, ok := object[field]
	if !ok {
		return "", []string{fmt.Sprintf("%s is required", field)}
	}
	text, ok := value.(string)
	if !ok {
		return "", []string{fmt.Sprintf("%s must be a string", field)}
	}
	if text == "" || strings.TrimSpace(text) == "" {
		return "", []string{fmt.Sprintf("%s is required", field)}
	}
	return text, nil
}

func requiredStringFieldWithContext(object map[string]any, context, field string) (string, []string) {
	value, ok := object[field]
	if !ok {
		return "", []string{fmt.Sprintf("%s.%s is required", context, field)}
	}
	text, ok := value.(string)
	if !ok {
		return "", []string{fmt.Sprintf("%s.%s must be a string", context, field)}
	}
	if text == "" || strings.TrimSpace(text) == "" {
		return "", []string{fmt.Sprintf("%s.%s is required", context, field)}
	}
	return text, nil
}

func requiredObjectField(object map[string]any, field string) (map[string]any, []string) {
	value, ok := object[field]
	if !ok {
		return nil, []string{fmt.Sprintf("%s is required", field)}
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return nil, []string{fmt.Sprintf("%s must be an object", field)}
	}
	return nested, nil
}

func ensureAllowedKeys(object map[string]any, context string, allowedKeys ...string) []string {
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}

	extraKeys := make([]string, 0)
	for key := range object {
		if _, ok := allowed[key]; !ok {
			extraKeys = append(extraKeys, key)
		}
	}
	sort.Strings(extraKeys)

	problems := make([]string, 0, len(extraKeys))
	for _, key := range extraKeys {
		problems = append(problems, fmt.Sprintf("%s has unknown field %q", context, key))
	}
	return problems
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

func sortedAnyMapKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys[V any](object map[string]V) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedLockedModelIDs(object map[string]LockedModelEntry) []string {
	return sortedMapKeys(object)
}

func sortedLockedTableIDs(object map[string]LockedTableEntry) []string {
	return sortedMapKeys(object)
}

func extraCapturedLogicalIDs(captured []projectinput.CapturedResource, expected map[string]string) []string {
	var extras []string
	for _, resource := range captured {
		if _, ok := expected[resource.LogicalID]; !ok {
			extras = append(extras, resource.LogicalID)
		}
	}
	sort.Strings(extras)
	return extras
}

func quotedList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fmt.Sprintf("%q", value)
	}
	return strings.Join(quoted, ", ")
}
