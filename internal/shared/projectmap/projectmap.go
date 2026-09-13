package projectmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

const (
	FileName = "parametron.project.json"
	Version  = "1.0"
)

const projectMappingVersionSyntaxMessage = `version must be a schema version in "major.minor" numeric form`

var (
	ErrIO         = errors.New("project mapping I/O error")
	ErrDecode     = errors.New("project mapping decode error")
	ErrValidation = errors.New("project mapping validation error")
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

type Project struct {
	Version   string            `json:"version"`
	ProjectID string            `json:"projectId"`
	DSL       string            `json:"dsl"`
	Resources Resources         `json:"resources"`
	Tables    map[string]string `json:"tables,omitempty"`
}

type Resources struct {
	Models map[string]string `json:"models"`
}

type projectMappingVersion struct {
	major int
	minor int
	raw   string
}

func Read(data []byte) (*Project, error) {
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

	project, err := parseProject(root)
	if err != nil {
		return nil, err
	}
	return project, nil
}

func Load(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{Path: path, Err: err}
	}
	return Read(data)
}

func Validate(project *Project) error {
	_, err := normalized(project)
	return err
}

func parseProject(root map[string]any) (*Project, error) {
	var problems []string

	if err := ensureAllowedKeys(root, "root", "version", "projectId", "dsl", "resources", "tables"); err != nil {
		problems = append(problems, err...)
	}

	version, errs := requiredVersionField(root)
	problems = append(problems, errs...)
	if errs == nil {
		if _, err := parseProjectMappingVersion(version); err != nil {
			problems = append(problems, err.Error())
		}
	}

	projectID, errs := requiredStringField(root, "projectId")
	problems = append(problems, errs...)

	dsl, errs := requiredStringField(root, "dsl")
	problems = append(problems, errs...)

	resourcesObject, errs := requiredObjectField(root, "resources")
	problems = append(problems, errs...)

	models := map[string]string(nil)
	if resourcesObject != nil {
		if err := ensureAllowedKeys(resourcesObject, "resources", "models"); err != nil {
			problems = append(problems, err...)
		}
		modelsObject, modelErrs := requiredObjectField(resourcesObject, "models")
		problems = append(problems, modelErrs...)
		if modelsObject != nil {
			var valueErrs []string
			models, valueErrs = parseStringMap(modelsObject, "resources.models")
			problems = append(problems, valueErrs...)
		}
	}

	tables := map[string]string(nil)
	if rawTables, ok := root["tables"]; ok {
		tablesObject, ok := rawTables.(map[string]any)
		if !ok {
			problems = append(problems, "tables must be an object")
		} else {
			var valueErrs []string
			tables, valueErrs = parseStringMap(tablesObject, "tables")
			problems = append(problems, valueErrs...)
		}
	}

	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}

	project := &Project{
		Version:   version,
		ProjectID: projectID,
		DSL:       dsl,
		Resources: Resources{Models: models},
		Tables:    tables,
	}

	normalizedProject, err := normalized(project)
	if err != nil {
		return nil, err
	}
	return normalizedProject, nil
}

func normalized(project *Project) (*Project, error) {
	if project == nil {
		return nil, &ValidationError{Problems: []string{"project mapping must not be nil"}}
	}

	var problems []string

	if err := validateProjectMappingVersion(project.Version); err != nil {
		problems = append(problems, err.Error())
		return nil, &ValidationError{Problems: problems}
	}

	if err := validateIDField("projectId", project.ProjectID); err != nil {
		problems = append(problems, err.Error())
	}

	dslPath, err := normalizeRelativePath(project.DSL)
	if err != nil {
		problems = append(problems, fmt.Sprintf("dsl %s", err.Error()))
	}

	if project.Resources.Models == nil {
		problems = append(problems, "resources.models is required")
	}

	modelIDs := make([]string, 0, len(project.Resources.Models))
	for id := range project.Resources.Models {
		modelIDs = append(modelIDs, id)
	}
	sort.Strings(modelIDs)

	normalizedModels := make(map[string]string, len(project.Resources.Models))
	for _, id := range modelIDs {
		if err := validateLogicalID("resources.models", id); err != nil {
			problems = append(problems, err.Error())
			continue
		}

		cleanedPath, pathErr := normalizeRelativePath(project.Resources.Models[id])
		if pathErr != nil {
			problems = append(problems, fmt.Sprintf("resources.models[%q] %s", id, pathErr.Error()))
			continue
		}
		normalizedModels[id] = cleanedPath
	}

	tableIDs := make([]string, 0, len(project.Tables))
	for id := range project.Tables {
		tableIDs = append(tableIDs, id)
	}
	sort.Strings(tableIDs)

	normalizedTables := make(map[string]string, len(project.Tables))
	for _, id := range tableIDs {
		if err := validateLogicalID("tables", id); err != nil {
			problems = append(problems, err.Error())
			continue
		}

		cleanedPath, pathErr := normalizeRelativePath(project.Tables[id])
		if pathErr != nil {
			problems = append(problems, fmt.Sprintf("tables[%q] %s", id, pathErr.Error()))
			continue
		}
		normalizedTables[id] = cleanedPath
	}

	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}

	return &Project{
		Version:   project.Version,
		ProjectID: project.ProjectID,
		DSL:       dslPath,
		Resources: Resources{Models: normalizedModels},
		Tables:    normalizedTables,
	}, nil
}

func requiredVersionField(object map[string]any) (string, []string) {
	value, ok := object["version"]
	if !ok {
		return "", []string{"version is required"}
	}

	text, ok := value.(string)
	if !ok {
		return "", []string{"version must be a string"}
	}

	return text, nil
}

func validateProjectMappingVersion(raw string) error {
	version, err := parseProjectMappingVersion(raw)
	if err != nil {
		return err
	}

	supported, err := parseProjectMappingVersion(Version)
	if err != nil {
		return err
	}

	switch {
	case version.major != supported.major:
		return fmt.Errorf("unsupported project mapping major version %q", raw)
	case version.minor > supported.minor:
		return fmt.Errorf("unsupported newer project mapping version %q", raw)
	case version.minor < supported.minor:
		return fmt.Errorf("unsupported project mapping version %q", raw)
	default:
		return nil
	}
}

func parseProjectMappingVersion(raw string) (projectMappingVersion, error) {
	dot := strings.IndexByte(raw, '.')
	if dot <= 0 || dot != strings.LastIndexByte(raw, '.') || dot == len(raw)-1 {
		return projectMappingVersion{}, errors.New(projectMappingVersionSyntaxMessage)
	}

	majorPart := raw[:dot]
	minorPart := raw[dot+1:]
	if !isNumericVersionPart(majorPart) || !isNumericVersionPart(minorPart) {
		return projectMappingVersion{}, errors.New(projectMappingVersionSyntaxMessage)
	}

	return projectMappingVersion{
		major: parseNumericVersionPart(majorPart),
		minor: parseNumericVersionPart(minorPart),
		raw:   raw,
	}, nil
}

func isNumericVersionPart(part string) bool {
	if part == "" {
		return false
	}
	if len(part) > 1 && part[0] == '0' {
		return false
	}
	for _, ch := range part {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func parseNumericVersionPart(part string) int {
	value := 0
	for _, ch := range part {
		value = value*10 + int(ch-'0')
	}
	return value
}

func validateIDField(field string, value string) error {
	if value == "" || strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not have leading or trailing whitespace", field)
	}
	return nil
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

func normalizeRelativePath(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) == "" {
		return "", errors.New("path must not be empty")
	}
	if raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("path %q must not have leading or trailing whitespace", raw)
	}
	if strings.Contains(raw, "\x00") {
		return "", fmt.Errorf("path %q must not contain a null byte", raw)
	}

	if isSlashAbsolute(raw) {
		return "", fmt.Errorf("path %q must be relative", raw)
	}

	slashPath := strings.ReplaceAll(raw, "\\", "/")
	if isWindowsAbsolute(slashPath) {
		return "", fmt.Errorf("path %q must be relative", raw)
	}

	cleaned := path.Clean(slashPath)
	if cleaned == "." {
		return "", fmt.Errorf("path %q must not resolve to current directory", raw)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path %q must stay within project root", raw)
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

func parseStringMap(object map[string]any, context string) (map[string]string, []string) {
	out := make(map[string]string, len(object))
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var problems []string
	for _, key := range keys {
		value, ok := object[key].(string)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s[%q] must be a string", context, key))
			continue
		}
		out[key] = value
	}
	return out, problems
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
