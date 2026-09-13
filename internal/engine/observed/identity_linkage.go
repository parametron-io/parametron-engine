package observed

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"parametron/internal/engine/semantic"
)

var (
	ErrIdentityComparability        = errors.New("observed identity comparability error")
	ErrIdentityInvalidObserved      = errors.New("observed identity comparability invalid observed artifact")
	ErrIdentityInvalidSemanticModel = errors.New("observed identity comparability invalid semantic model")
	ErrIdentityDuplicateObservedID  = errors.New("observed identity comparability duplicate observed identity")
	ErrIdentityComparableNotFound   = errors.New("observed identity comparability comparable identity not found")
)

const (
	identityCodeInvalidObserved      = "invalid_observed"
	identityCodeInvalidSemanticModel = "invalid_semantic_model"
	identityCodeDuplicateObservedID  = "duplicate_observed_id"
	identityCodeComparableNotFound   = "comparable_identity_not_found"

	identityKindComponent = "component"
	identityKindParameter = "parameter"
	identityKindMetadata  = "metadata"
)

type IdentityComparability struct {
	Components []ObservedCaptureIdentityLink `json:"components"`
	Parameters []ObservedCaptureIdentityLink `json:"parameters"`
	Metadata   []ObservedCaptureIdentityLink `json:"metadata"`
}

type ObservedCaptureIdentityLink struct {
	ObservedID string `json:"observedId"`
	CaptureID  string `json:"captureId"`
	Kind       string `json:"kind"`
}

type IdentityError struct {
	Code       string
	Message    string
	Category   string
	ObservedID string
	RelatedID  string
	Problems   []string
	Err        error
}

func (e *IdentityError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return ErrIdentityComparability.Error()
	}
	return fmt.Sprintf("%s: %s", ErrIdentityComparability, e.Message)
}

func (e *IdentityError) Unwrap() []error {
	if e == nil {
		return []error{ErrIdentityComparability}
	}

	out := []error{ErrIdentityComparability}
	switch e.Code {
	case identityCodeInvalidObserved:
		out = append(out, ErrIdentityInvalidObserved)
	case identityCodeInvalidSemanticModel:
		out = append(out, ErrIdentityInvalidSemanticModel)
	case identityCodeDuplicateObservedID:
		out = append(out, ErrIdentityDuplicateObservedID)
	case identityCodeComparableNotFound:
		out = append(out, ErrIdentityComparableNotFound)
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

func LinkToSemantic(observed *Observed, model *semantic.Model) (*IdentityComparability, error) {
	if observed == nil {
		return nil, &IdentityError{
			Code:    identityCodeInvalidObserved,
			Message: "observed artifact must not be nil",
		}
	}
	if duplicate := detectDuplicateObservedIdentity(observed); duplicate != nil {
		return nil, duplicate
	}
	if err := Validate(observed); err != nil {
		return nil, &IdentityError{
			Code:     identityCodeInvalidObserved,
			Message:  err.Error(),
			Problems: []string{err.Error()},
			Err:      err,
		}
	}

	if model == nil {
		return nil, &IdentityError{
			Code:    identityCodeInvalidSemanticModel,
			Message: "semantic model must not be nil",
		}
	}
	if err := semantic.Validate(model); err != nil {
		problems := semanticProblems(err)
		return nil, &IdentityError{
			Code:     identityCodeInvalidSemanticModel,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	canonical, err := semantic.Canonicalize(model)
	if err != nil {
		problems := semanticProblems(err)
		return nil, &IdentityError{
			Code:     identityCodeInvalidSemanticModel,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	componentIDs := makeIDSet(len(canonical.Components))
	for _, component := range canonical.Components {
		componentIDs[component.ID] = struct{}{}
	}
	parameterGroupIDs := makeIDSet(len(canonical.ParameterGroups))
	for _, group := range canonical.ParameterGroups {
		parameterGroupIDs[group.ID] = struct{}{}
	}
	parameterIDs := makeIDSet(len(canonical.Parameters))
	for _, parameter := range canonical.Parameters {
		parameterIDs[parameter.ID] = struct{}{}
	}
	metadataIDs := makeIDSet(len(canonical.Metadata))
	for _, entry := range canonical.Metadata {
		metadataIDs[entry.ID] = struct{}{}
	}
	ownerIDs := knownSemanticOwnerIDs(canonical)

	componentLinks := make([]ObservedCaptureIdentityLink, 0, len(observed.Observation.Components))
	for _, component := range sortedObservedComponents(observed.Observation.Components) {
		if _, ok := componentIDs[component.ID]; !ok {
			return nil, comparableNotFoundError(identityKindComponent, component.ID, "", fmt.Sprintf("observed component id %q was not found in semantic component identities", component.ID))
		}
		componentLinks = append(componentLinks, ObservedCaptureIdentityLink{
			ObservedID: component.ID,
			CaptureID:  component.ID,
			Kind:       identityKindComponent,
		})
	}

	parameterLinks := make([]ObservedCaptureIdentityLink, 0, len(observed.Observation.Parameters))
	for _, parameter := range sortedObservedParameters(observed.Observation.Parameters) {
		if _, ok := parameterIDs[parameter.ID]; !ok {
			return nil, comparableNotFoundError(identityKindParameter, parameter.ID, "", fmt.Sprintf("observed parameter id %q was not found in semantic parameter identities", parameter.ID))
		}
		if parameter.GroupID != "" {
			if _, ok := parameterGroupIDs[parameter.GroupID]; !ok {
				return nil, comparableNotFoundError(identityKindParameter, parameter.ID, parameter.GroupID, fmt.Sprintf("observed parameter %q groupId %q was not found in semantic parameter group identities", parameter.ID, parameter.GroupID))
			}
		}
		parameterLinks = append(parameterLinks, ObservedCaptureIdentityLink{
			ObservedID: parameter.ID,
			CaptureID:  parameter.ID,
			Kind:       identityKindParameter,
		})
	}

	metadataLinks := make([]ObservedCaptureIdentityLink, 0, len(observed.Observation.Metadata))
	for _, entry := range sortedObservedMetadata(observed.Observation.Metadata) {
		if _, ok := metadataIDs[entry.ID]; !ok {
			return nil, comparableNotFoundError(identityKindMetadata, entry.ID, "", fmt.Sprintf("observed metadata id %q was not found in semantic metadata identities", entry.ID))
		}
		if entry.OwnerID != "" {
			if _, ok := ownerIDs[entry.OwnerID]; !ok {
				return nil, comparableNotFoundError(identityKindMetadata, entry.ID, entry.OwnerID, fmt.Sprintf("observed metadata %q ownerId %q was not found in semantic owner identities", entry.ID, entry.OwnerID))
			}
		}
		metadataLinks = append(metadataLinks, ObservedCaptureIdentityLink{
			ObservedID: entry.ID,
			CaptureID:  entry.ID,
			Kind:       identityKindMetadata,
		})
	}

	sortIdentityLinks(componentLinks)
	sortIdentityLinks(parameterLinks)
	sortIdentityLinks(metadataLinks)

	return &IdentityComparability{
		Components: componentLinks,
		Parameters: parameterLinks,
		Metadata:   metadataLinks,
	}, nil
}

func comparableNotFoundError(category, observedID, relatedID, message string) *IdentityError {
	return &IdentityError{
		Code:       identityCodeComparableNotFound,
		Message:    message,
		Category:   category,
		ObservedID: observedID,
		RelatedID:  relatedID,
	}
}

func detectDuplicateObservedIdentity(observed *Observed) *IdentityError {
	for _, candidate := range []struct {
		category string
		ids      []string
	}{
		{category: identityKindComponent, ids: observedComponentIDs(observed.Observation.Components)},
		{category: identityKindParameter, ids: observedParameterIDs(observed.Observation.Parameters)},
		{category: identityKindMetadata, ids: observedMetadataIDs(observed.Observation.Metadata)},
	} {
		duplicateID, ok := findDuplicateID(candidate.ids)
		if !ok {
			continue
		}
		return &IdentityError{
			Code:       identityCodeDuplicateObservedID,
			Message:    fmt.Sprintf("duplicate observed %s id %q", candidate.category, duplicateID),
			Category:   candidate.category,
			ObservedID: duplicateID,
		}
	}
	return nil
}

func observedComponentIDs(components []Component) []string {
	out := make([]string, 0, len(components))
	for _, component := range components {
		if strings.TrimSpace(component.ID) == "" {
			continue
		}
		out = append(out, component.ID)
	}
	return out
}

func observedParameterIDs(parameters []Parameter) []string {
	out := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		if strings.TrimSpace(parameter.ID) == "" {
			continue
		}
		out = append(out, parameter.ID)
	}
	return out
}

func observedMetadataIDs(metadata []Metadata) []string {
	out := make([]string, 0, len(metadata))
	for _, entry := range metadata {
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		out = append(out, entry.ID)
	}
	return out
}

func findDuplicateID(ids []string) (string, bool) {
	if len(ids) == 0 {
		return "", false
	}
	ordered := append([]string(nil), ids...)
	slices.Sort(ordered)
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1] == ordered[i] {
			return ordered[i], true
		}
	}
	return "", false
}

func knownSemanticOwnerIDs(model *semantic.Model) map[string]struct{} {
	out := makeIDSet(len(model.Components) + len(model.ParameterGroups) + len(model.Features) + len(model.Parameters) + len(model.Metadata))
	for _, component := range model.Components {
		out[component.ID] = struct{}{}
	}
	for _, group := range model.ParameterGroups {
		out[group.ID] = struct{}{}
	}
	for _, feature := range model.Features {
		out[feature.ID] = struct{}{}
	}
	for _, parameter := range model.Parameters {
		out[parameter.ID] = struct{}{}
	}
	for _, entry := range model.Metadata {
		out[entry.ID] = struct{}{}
	}
	return out
}

func makeIDSet(size int) map[string]struct{} {
	if size < 0 {
		size = 0
	}
	return make(map[string]struct{}, size)
}

func sortIdentityLinks(links []ObservedCaptureIdentityLink) {
	slices.SortFunc(links, func(left, right ObservedCaptureIdentityLink) int {
		if cmp := strings.Compare(left.ObservedID, right.ObservedID); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(left.CaptureID, right.CaptureID); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.Kind, right.Kind)
	})
}

func sortedObservedComponents(components []Component) []Component {
	out := append([]Component(nil), components...)
	slices.SortFunc(out, compareComponents)
	return out
}

func sortedObservedParameters(parameters []Parameter) []Parameter {
	out := append([]Parameter(nil), parameters...)
	slices.SortFunc(out, compareParameters)
	return out
}

func sortedObservedMetadata(metadata []Metadata) []Metadata {
	out := append([]Metadata(nil), metadata...)
	slices.SortFunc(out, compareMetadata)
	return out
}

func semanticProblems(err error) []string {
	var validationErr *semantic.ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Messages()
	}
	if err == nil {
		return nil
	}
	return []string{err.Error()}
}
