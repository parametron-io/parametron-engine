package cadruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
)

const (
	FreeCADReferenceTraversalRequestFilename      = "prm.reference-traversal-request.json"
	FreeCADReferenceTraversalRequestSchemaVersion = "2.0"
)

var supportedReferenceMechanisms = map[string]bool{
	"App::PropertyLink": false, "App::PropertyLinkChild": false,
	"App::PropertyLinkGlobal": false, "App::PropertyLinkHidden": false,
	"App::PropertyLinkList": true, "App::PropertyLinkListChild": true,
	"App::PropertyLinkListGlobal": true, "App::PropertyLinkListHidden": true,
	"App::PropertyLinkSub": false, "App::PropertyLinkSubChild": false,
	"App::PropertyLinkSubGlobal": false, "App::PropertyLinkSubHidden": false,
	"App::PropertyLinkSubList": true, "App::PropertyLinkSubListChild": true,
	"App::PropertyLinkSubListGlobal": true, "App::PropertyLinkSubListHidden": true,
	"App::PropertyXLink": false, "App::PropertyXLinkList": true,
	"App::PropertyXLinkSub": false, "App::PropertyXLinkSubHidden": false,
	"App::PropertyXLinkSubList": true,
}

type FreeCADReferenceTraversalExternalTarget struct {
	SourceObjectName   string `json:"sourceObjectName"`
	SourceProperty     string `json:"sourceProperty"`
	ReferenceMechanism string `json:"referenceMechanism"`
	TargetObjectName   string `json:"targetObjectName"`
	TargetDocumentPath string `json:"targetDocumentPath"`
}

type FreeCADReferenceTraversalRequest struct {
	SchemaVersion   string                                    `json:"schemaVersion"`
	ExternalTargets []FreeCADReferenceTraversalExternalTarget `json:"externalTargets"`
}

type FreeCADReferenceTraversalRequestMaterialization struct {
	Contract FreeCADReferenceTraversalRequest
	JSON     []byte
	Path     string
}

type FreeCADReferenceTraversalRequestError struct {
	Field string
	Path  string
	Err   error
}

func (e *FreeCADReferenceTraversalRequestError) Error() string {
	if e == nil {
		return ""
	}
	message := "freecad reference traversal request"
	if e.Field != "" {
		message += ": " + e.Field
	}
	if e.Path != "" {
		message += ": path=" + e.Path
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}
func (e *FreeCADReferenceTraversalRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ComposeFreeCADReferenceTraversalRequest(req adapter.CADRuntimeOrchestrationRequest) (FreeCADReferenceTraversalRequestMaterialization, error) {
	manifest, err := freecad.ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		return FreeCADReferenceTraversalRequestMaterialization{}, &FreeCADReferenceTraversalRequestError{Err: err}
	}
	requestPath := filepath.Join(manifest.Attempt.Layout.WorkingCopyDir, FreeCADReferenceTraversalRequestFilename)
	entries := make([]FreeCADReferenceTraversalExternalTarget, len(req.ExternalTargets))
	for i, raw := range req.ExternalTargets {
		entry := FreeCADReferenceTraversalExternalTarget(raw)
		if field, err := validateTraversalTarget(entry); err != nil {
			return FreeCADReferenceTraversalRequestMaterialization{}, &FreeCADReferenceTraversalRequestError{Field: fmt.Sprintf("ExternalTargets[%d].%s", i, field), Path: requestPath, Err: err}
		}
		entries[i] = entry
	}
	sort.Slice(entries, func(i, j int) bool { return traversalTargetKey(entries[i]) < traversalTargetKey(entries[j]) })
	if field, err := validateTraversalTargetConflicts(entries); err != nil {
		return FreeCADReferenceTraversalRequestMaterialization{}, &FreeCADReferenceTraversalRequestError{Field: field, Path: requestPath, Err: err}
	}
	contract := FreeCADReferenceTraversalRequest{SchemaVersion: FreeCADReferenceTraversalRequestSchemaVersion, ExternalTargets: entries}
	data, err := json.Marshal(contract)
	if err != nil {
		return FreeCADReferenceTraversalRequestMaterialization{}, &FreeCADReferenceTraversalRequestError{Path: requestPath, Err: err}
	}
	data = append(data, '\n')
	return FreeCADReferenceTraversalRequestMaterialization{Contract: contract, JSON: data, Path: requestPath}, nil
}

func WriteFreeCADReferenceTraversalRequest(req adapter.CADRuntimeOrchestrationRequest) (FreeCADReferenceTraversalRequestMaterialization, error) {
	materialized, err := ComposeFreeCADReferenceTraversalRequest(req)
	if err != nil {
		return materialized, err
	}
	dir := filepath.Dir(materialized.Path)
	tmp, err := os.CreateTemp(dir, ".reference-traversal-request-*")
	if err != nil {
		return FreeCADReferenceTraversalRequestMaterialization{}, &FreeCADReferenceTraversalRequestError{Path: materialized.Path, Err: err}
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err = tmp.Write(materialized.JSON); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpPath, materialized.Path)
	}
	if err != nil {
		return FreeCADReferenceTraversalRequestMaterialization{}, &FreeCADReferenceTraversalRequestError{Path: materialized.Path, Err: err}
	}
	ok = true
	return materialized, nil
}

func validateTraversalTarget(e FreeCADReferenceTraversalExternalTarget) (string, error) {
	fields := []struct{ name, value string }{{"sourceObjectName", e.SourceObjectName}, {"sourceProperty", e.SourceProperty}, {"referenceMechanism", e.ReferenceMechanism}, {"targetObjectName", e.TargetObjectName}}
	for _, f := range fields {
		if f.value == "" || strings.TrimSpace(f.value) != f.value || strings.ContainsRune(f.value, '\x00') {
			return f.name, fmt.Errorf("must be an exact non-empty string")
		}
	}
	if _, ok := supportedReferenceMechanisms[e.ReferenceMechanism]; !ok {
		return "referenceMechanism", fmt.Errorf("unsupported mechanism %q", e.ReferenceMechanism)
	}
	p := e.TargetDocumentPath
	if p == "" || strings.TrimSpace(p) != p || strings.ContainsRune(p, '\x00') || strings.Contains(p, "\\") || path.IsAbs(p) || (len(p) >= 2 && p[1] == ':') || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return "targetDocumentPath", fmt.Errorf("must be a canonical contract-relative slash-separated path")
	}
	return "", nil
}

func traversalTargetKey(e FreeCADReferenceTraversalExternalTarget) string {
	return strings.Join([]string{e.SourceObjectName, e.SourceProperty, e.ReferenceMechanism, e.TargetObjectName, e.TargetDocumentPath}, "\x00")
}
func traversalPropertyKey(e FreeCADReferenceTraversalExternalTarget) string {
	return strings.Join([]string{e.SourceObjectName, e.SourceProperty, e.ReferenceMechanism}, "\x00")
}
func traversalLookupKey(e FreeCADReferenceTraversalExternalTarget) string {
	return traversalPropertyKey(e) + "\x00" + e.TargetObjectName
}

func validateTraversalTargetConflicts(entries []FreeCADReferenceTraversalExternalTarget) (string, error) {
	lookup := map[string]FreeCADReferenceTraversalExternalTarget{}
	propertyTargets := map[string]map[string]struct{}{}
	for i, e := range entries {
		key := traversalLookupKey(e)
		if prior, ok := lookup[key]; ok {
			if prior.TargetDocumentPath == e.TargetDocumentPath {
				return fmt.Sprintf("ExternalTargets[%d]", i), fmt.Errorf("exact duplicate entry")
			}
			return fmt.Sprintf("ExternalTargets[%d]", i), fmt.Errorf("lookup coordinate maps to conflicting target identities")
		}
		lookup[key] = e
		propertyKey := traversalPropertyKey(e)
		targets := propertyTargets[propertyKey]
		if targets == nil {
			targets = map[string]struct{}{}
			propertyTargets[propertyKey] = targets
		}
		if _, exists := targets[e.TargetObjectName]; exists {
			return fmt.Sprintf("ExternalTargets[%d]", i), fmt.Errorf("duplicate target object name for source property coordinate")
		}
		targets[e.TargetObjectName] = struct{}{}
		if !supportedReferenceMechanisms[e.ReferenceMechanism] && len(targets) > 1 {
			return fmt.Sprintf("ExternalTargets[%d]", i), fmt.Errorf("single-target mechanism has multiple targets for source property coordinate")
		}
	}
	return "", nil
}

// DecodeFreeCADReferenceTraversalRequest strictly parses the current schema version.
func DecodeFreeCADReferenceTraversalRequest(data []byte) (FreeCADReferenceTraversalRequest, error) {
	var header struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return FreeCADReferenceTraversalRequest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if header.SchemaVersion != FreeCADReferenceTraversalRequestSchemaVersion {
		return FreeCADReferenceTraversalRequest{}, fmt.Errorf("unsupported schemaVersion %q", header.SchemaVersion)
	}
	var result FreeCADReferenceTraversalRequest
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := requireTraversalJSONEOF(decoder); err != nil {
		return result, err
	}
	if result.ExternalTargets == nil {
		return result, fmt.Errorf("externalTargets is required")
	}
	for i, entry := range result.ExternalTargets {
		if field, err := validateTraversalTarget(entry); err != nil {
			return result, fmt.Errorf("externalTargets[%d].%s: %w", i, field, err)
		}
	}
	if _, err := validateTraversalTargetConflicts(result.ExternalTargets); err != nil {
		return result, err
	}
	return result, nil
}

func requireTraversalJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
