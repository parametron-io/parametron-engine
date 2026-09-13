package semanticmap

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"parametron/internal/engine/semantic"
)

var (
	ErrIdentityMissingSemanticMap = errors.New("semantic map identity linkage missing semantic map")
	ErrIdentityInvalidSemanticMap = errors.New("semantic map identity linkage invalid semantic map")
	ErrIdentityInvalidModel       = errors.New("semantic map identity linkage invalid semantic model")
	ErrIdentityUnlinkable         = errors.New("semantic map identity linkage unlinkable entity")
	ErrIdentityDuplicate          = errors.New("semantic map identity linkage duplicate linkage")
)

const (
	identityCodeMissingSemanticMap = "identity_missing_semantic_map"
	identityCodeInvalidSemanticMap = "identity_invalid_semantic_map"
	identityCodeInvalidModel       = "identity_invalid_semantic_model"
	identityCodeUnlinkable         = "identity_unlinkable"
	identityCodeDuplicate          = "identity_duplicate"

	identityKindComponent      = "component"
	identityKindParameterGroup = "parameter_group"
	identityKindParameter      = "parameter"
	identityKindMetadata       = "metadata"
)

type IdentityLinkage struct {
	Entries []IdentityLink `json:"entries"`
}

type IdentityLink struct {
	EntityKind    string `json:"entityKind"`
	SemanticID    string `json:"semanticId"`
	CaptureID     string `json:"captureId"`
	IdentityField string `json:"identityField"`
	NameField     string `json:"nameField,omitempty"`
	GroupField    string `json:"groupField,omitempty"`
	KeyField      string `json:"keyField,omitempty"`
}

type IdentityError struct {
	Code       string
	Message    string
	EntityKind string
	SemanticID string
	CaptureID  string
	Problems   []string
	Err        error
}

func (e *IdentityError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return ErrIdentity.Error()
	}
	return fmt.Sprintf("%s: %s", ErrIdentity, e.Message)
}

func (e *IdentityError) Unwrap() []error {
	if e == nil {
		return []error{ErrIdentity}
	}

	out := []error{ErrIdentity}
	switch e.Code {
	case identityCodeMissingSemanticMap:
		out = append(out, ErrIdentityMissingSemanticMap)
	case identityCodeInvalidSemanticMap:
		out = append(out, ErrIdentityInvalidSemanticMap)
	case identityCodeInvalidModel:
		out = append(out, ErrIdentityInvalidModel)
	case identityCodeUnlinkable:
		out = append(out, ErrIdentityUnlinkable)
	case identityCodeDuplicate:
		out = append(out, ErrIdentityDuplicate)
	}
	if e.Err != nil {
		out = append(out, e.Err)
	}
	return out
}

func (e *IdentityError) ProblemList() []string {
	if e == nil {
		return nil
	}
	return append([]string(nil), e.Problems...)
}

func LinkCaptureIdentities(contract *SemanticMap, model *semantic.Model) (*IdentityLinkage, error) {
	if contract == nil {
		return nil, &IdentityError{
			Code:    identityCodeMissingSemanticMap,
			Message: "semantic map must not be nil",
		}
	}

	if err := validateProjectionLinkageContract(contract); err != nil {
		problems := projectionProblems(err)
		return nil, &IdentityError{
			Code:     identityCodeInvalidSemanticMap,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	if model == nil {
		return nil, &IdentityError{
			Code:    identityCodeInvalidModel,
			Message: "semantic model must not be nil",
		}
	}

	if duplicate, ok := detectDuplicateIdentityKey(model); ok {
		return nil, &IdentityError{
			Code:       identityCodeDuplicate,
			Message:    fmt.Sprintf("duplicate identity linkage key for %s %q", duplicate.EntityKind, duplicate.SemanticID),
			EntityKind: duplicate.EntityKind,
			SemanticID: duplicate.SemanticID,
			CaptureID:  duplicate.CaptureID,
		}
	}

	if err := semantic.Validate(model); err != nil {
		problems := semanticValidationProblems(err)
		return nil, &IdentityError{
			Code:     identityCodeInvalidModel,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	canonical, err := semantic.Canonicalize(model)
	if err != nil {
		problems := semanticValidationProblems(err)
		return nil, &IdentityError{
			Code:     identityCodeInvalidModel,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	if problem := validateIdentityCompatibility(contract, canonical); problem != nil {
		return nil, problem
	}

	entries := make([]IdentityLink, 0, len(canonical.Components)+len(canonical.ParameterGroups)+len(canonical.Parameters)+len(canonical.Metadata))
	for _, component := range canonical.Components {
		entries = append(entries, IdentityLink{
			EntityKind:    identityKindComponent,
			SemanticID:    component.ID,
			CaptureID:     component.ID,
			IdentityField: "capture.id",
		})
	}
	for _, group := range canonical.ParameterGroups {
		entries = append(entries, IdentityLink{
			EntityKind:    identityKindParameterGroup,
			SemanticID:    group.ID,
			CaptureID:     group.ID,
			IdentityField: "capture.id",
		})
	}
	for _, parameter := range canonical.Parameters {
		entries = append(entries, IdentityLink{
			EntityKind:    identityKindParameter,
			SemanticID:    parameter.ID,
			CaptureID:     parameter.ID,
			IdentityField: "capture.id",
			NameField:     "capture.name",
			GroupField:    "capture.groupId",
		})
	}
	for _, entry := range canonical.Metadata {
		entries = append(entries, IdentityLink{
			EntityKind:    identityKindMetadata,
			SemanticID:    entry.ID,
			CaptureID:     entry.ID,
			IdentityField: "capture.id",
			KeyField:      "capture.key",
		})
	}

	slices.SortFunc(entries, func(a, b IdentityLink) int {
		if result := compareStrings(a.EntityKind, b.EntityKind); result != 0 {
			return result
		}
		if result := compareStrings(a.SemanticID, b.SemanticID); result != 0 {
			return result
		}
		return compareStrings(a.CaptureID, b.CaptureID)
	})

	for i := 1; i < len(entries); i++ {
		if entries[i-1].EntityKind == entries[i].EntityKind &&
			entries[i-1].SemanticID == entries[i].SemanticID &&
			entries[i-1].CaptureID == entries[i].CaptureID {
			return nil, &IdentityError{
				Code:       identityCodeDuplicate,
				Message:    fmt.Sprintf("duplicate identity linkage key for %s %q", entries[i].EntityKind, entries[i].SemanticID),
				EntityKind: entries[i].EntityKind,
				SemanticID: entries[i].SemanticID,
				CaptureID:  entries[i].CaptureID,
			}
		}
	}

	return &IdentityLinkage{Entries: entries}, nil
}

func validateIdentityCompatibility(contract *SemanticMap, model *semantic.Model) *IdentityError {
	if len(model.Components) > 0 {
		for _, component := range model.Components {
			if component.ID == "" {
				return &IdentityError{
					Code:       identityCodeInvalidModel,
					Message:    "component semantic identity must not be empty",
					EntityKind: identityKindComponent,
				}
			}
			if componentIdentitySupported(contract, component.Kind) {
				continue
			}
			return &IdentityError{
				Code:       identityCodeUnlinkable,
				Message:    fmt.Sprintf("component %q kind %q cannot be linked through captureToSemantic.components using capture.id", component.ID, component.Kind),
				EntityKind: identityKindComponent,
				SemanticID: component.ID,
				CaptureID:  component.ID,
			}
		}
	}

	if len(model.ParameterGroups) > 0 && !parameterGroupIdentitySupported(contract) {
		group := model.ParameterGroups[0]
		return &IdentityError{
			Code:       identityCodeUnlinkable,
			Message:    fmt.Sprintf("parameter group %q cannot be linked through captureToSemantic.parameterGroups using capture.id", group.ID),
			EntityKind: identityKindParameterGroup,
			SemanticID: group.ID,
			CaptureID:  group.ID,
		}
	}

	if len(model.Parameters) > 0 && !parameterIdentitySupported(contract) {
		parameter := model.Parameters[0]
		return &IdentityError{
			Code:       identityCodeUnlinkable,
			Message:    fmt.Sprintf("parameter %q cannot be linked through captureToSemantic.parameters using capture.id", parameter.ID),
			EntityKind: identityKindParameter,
			SemanticID: parameter.ID,
			CaptureID:  parameter.ID,
		}
	}

	if len(model.Metadata) > 0 && !metadataIdentitySupported(contract) {
		entry := model.Metadata[0]
		return &IdentityError{
			Code:       identityCodeUnlinkable,
			Message:    fmt.Sprintf("metadata %q cannot be linked through captureToSemantic.metadata using capture.id", entry.ID),
			EntityKind: identityKindMetadata,
			SemanticID: entry.ID,
			CaptureID:  entry.ID,
		}
	}

	return nil
}

func componentIdentitySupported(contract *SemanticMap, semanticKind string) bool {
	for _, entry := range contract.CaptureToSemantic.Components {
		if entry.CaptureKind != identityKindComponent || entry.Identity != "capture.id" {
			continue
		}
		for _, target := range entry.SemanticKinds {
			if target == semanticKind {
				return true
			}
		}
	}
	return false
}

func parameterGroupIdentitySupported(contract *SemanticMap) bool {
	for _, entry := range contract.CaptureToSemantic.ParameterGroups {
		if entry.CaptureKind == identityKindParameterGroup && entry.SemanticKind == identityKindParameterGroup {
			return true
		}
	}
	return false
}

func parameterIdentitySupported(contract *SemanticMap) bool {
	for _, entry := range contract.CaptureToSemantic.Parameters {
		if entry.CaptureKind == identityKindParameter &&
			entry.SemanticKind == identityKindParameter &&
			entry.Identity == "capture.id" &&
			entry.Name == "capture.name" &&
			entry.Group == "capture.groupId" {
			return true
		}
	}
	return false
}

func metadataIdentitySupported(contract *SemanticMap) bool {
	for _, entry := range contract.CaptureToSemantic.Metadata {
		if entry.CaptureKind == identityKindMetadata &&
			entry.SemanticKind == identityKindMetadata &&
			entry.Identity == "capture.id" &&
			entry.Key == "capture.key" {
			return true
		}
	}
	return false
}

func detectDuplicateIdentityKey(model *semantic.Model) (IdentityLink, bool) {
	candidates := make([]IdentityLink, 0, len(model.Components)+len(model.ParameterGroups)+len(model.Parameters)+len(model.Metadata))
	for _, component := range model.Components {
		candidates = append(candidates, IdentityLink{
			EntityKind: identityKindComponent,
			SemanticID: component.ID,
			CaptureID:  component.ID,
		})
	}
	for _, group := range model.ParameterGroups {
		candidates = append(candidates, IdentityLink{
			EntityKind: identityKindParameterGroup,
			SemanticID: group.ID,
			CaptureID:  group.ID,
		})
	}
	for _, parameter := range model.Parameters {
		candidates = append(candidates, IdentityLink{
			EntityKind: identityKindParameter,
			SemanticID: parameter.ID,
			CaptureID:  parameter.ID,
		})
	}
	for _, entry := range model.Metadata {
		candidates = append(candidates, IdentityLink{
			EntityKind: identityKindMetadata,
			SemanticID: entry.ID,
			CaptureID:  entry.ID,
		})
	}

	slices.SortFunc(candidates, func(a, b IdentityLink) int {
		if result := compareStrings(a.EntityKind, b.EntityKind); result != 0 {
			return result
		}
		if result := compareStrings(a.SemanticID, b.SemanticID); result != 0 {
			return result
		}
		return compareStrings(a.CaptureID, b.CaptureID)
	})

	for i := 1; i < len(candidates); i++ {
		if candidates[i-1].EntityKind == candidates[i].EntityKind &&
			candidates[i-1].SemanticID == candidates[i].SemanticID &&
			candidates[i-1].CaptureID == candidates[i].CaptureID &&
			candidates[i].SemanticID != "" {
			return candidates[i], true
		}
	}
	return IdentityLink{}, false
}

func semanticValidationProblems(err error) []string {
	var validationErr *semantic.ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Messages()
	}
	if err == nil {
		return nil
	}
	return []string{err.Error()}
}
