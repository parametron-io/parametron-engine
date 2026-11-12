package cadruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/resourcebinding"
)

const FactEvidenceRef = recordcontract.FactEvidenceSourceRef

// FactRuntimeSubject retains both the explicit logical identity and the native
// materialized document coordinate; both must join the same inventory binding.
type FactRuntimeSubject struct {
	Entity     recordcontract.OccurrenceEntity `json:"entity"`
	Document   string                          `json:"document"`
	NativeType string                          `json:"nativeType"`
}

// FactRuntimeEvidenceFact returns one explicitly bound native fact observation.
// Observed values carry the typed scalar plus an explicit unit token;
// unavailable and unsupported states stay explicit and never substitute zero,
// false, or omission.
type FactRuntimeEvidenceFact struct {
	Kind         string                                  `json:"kind"`
	Subject      FactRuntimeSubject                      `json:"subject"`
	Context      []recordcontract.OccurrenceContextFrame `json:"context"`
	Property     string                                  `json:"property"`
	ValueKind    string                                  `json:"valueKind,omitempty"`
	Value        json.RawMessage                         `json:"value,omitempty"`
	Unit         string                                  `json:"unit,omitempty"`
	Availability string                                  `json:"availability"`
	Diagnostic   string                                  `json:"diagnostic,omitempty"`
}

// FactRuntimeEvidence is independent of generic reference traversal and of the
// occurrence sidecar. It is preserved runtime evidence; the Engine invents no
// native facts the runtime did not observe and emits no Engine-normalized
// records at this boundary.
type FactRuntimeEvidence struct {
	SchemaVersion  string                    `json:"schemaVersion"`
	Kind           string                    `json:"kind"`
	FreeCADVersion string                    `json:"freecadVersion"`
	Facts          []FactRuntimeEvidenceFact `json:"facts"`
}

// FactMappingInput carries explicitly requested scope and unchanged evidence
// bytes from the selected confined attempt.
type FactMappingInput struct {
	Request                *recordcontract.FactRequest
	Resources              resourcebinding.Inventory
	ExpectedSourceDocument string
	Content                []byte
}

func factInvalid(message string) error {
	return fmt.Errorf("%w: %s", recordcontract.ErrInvalidFact, message)
}

func knownFactAvailability(state string) bool {
	return recordcontract.KnownFactAvailability(state)
}

// DecodeFactEvidence rejects unknown fields, duplicate JSON keys and trailing
// values, and keeps value material presence strict per availability state.
// Exact bytes remain the caller's provenance material.
func DecodeFactEvidence(content []byte) (FactRuntimeEvidence, error) {
	var evidence FactRuntimeEvidence
	if err := uniqueOccurrenceJSON(content); err != nil {
		return evidence, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return evidence, factInvalid(fmt.Sprintf("malformed fact evidence: %v", err))
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return evidence, factInvalid("trailing JSON")
	}
	if evidence.SchemaVersion != recordcontract.FactEvidenceSchemaVersion {
		return evidence, factInvalid("unsupported fact evidence schema version")
	}
	// Presence strictness cannot be expressed through struct decoding alone;
	// a second raw pass distinguishes omission from explicit null.
	var raw struct {
		Facts []map[string]json.RawMessage `json:"facts"`
	}
	if err := json.Unmarshal(content, &raw); err != nil {
		return evidence, factInvalid(fmt.Sprintf("malformed fact evidence: %v", err))
	}
	for index, rawFact := range raw.Facts {
		if !knownFactAvailability(evidence.Facts[index].Availability) {
			return evidence, factInvalid(fmt.Sprintf("invalid availability on fact evidence %d", index))
		}
		value, hasValue := rawFact["value"]
		valueKind, hasValueKind := rawFact["valueKind"]
		unit, hasUnit := rawFact["unit"]
		observed := evidence.Facts[index].Availability == recordcontract.FactAvailabilityObserved
		if observed {
			if !hasValue || string(value) == "null" {
				return evidence, factInvalid(fmt.Sprintf("fact evidence %d requires an observed value", index))
			}
			if !hasValueKind || string(valueKind) == "null" {
				return evidence, factInvalid(fmt.Sprintf("fact evidence %d requires an observed value kind", index))
			}
		} else {
			if hasValue || hasValueKind {
				return evidence, factInvalid(fmt.Sprintf("fact evidence %d must not carry value material in an unobserved state", index))
			}
		}
		if hasUnit && string(unit) == "null" {
			return evidence, factInvalid(fmt.Sprintf("fact evidence %d unit must not be null", index))
		}
	}
	return evidence, nil
}

// qualifyFactSubject joins the evidence subject's declared resource identity
// with its native materialized document coordinate. The same native name in
// two resources stays distinct through this join, and operational absolute
// paths are never used as identity material.
func qualifyFactSubject(resources resourcebinding.Inventory, subject FactRuntimeSubject, expected recordcontract.OccurrenceEntity) error {
	if !occurrenceExactText(subject.NativeType) {
		return factInvalid("native type required")
	}
	if err := resourcebinding.ValidateCoordinate(subject.Document); err != nil {
		return err
	}
	resolved, err := resources.LookupCoordinate(subject.Document, true)
	if err != nil {
		return err
	}
	resource := subject.Entity.Resource
	if resolved.ProjectID != resource.ProjectID || resolved.Kind != resource.Kind || resolved.LogicalID != resource.LogicalID {
		return factInvalid("logical identity disagrees with materialized coordinate")
	}
	if occurrenceJSON(subject.Entity) != occurrenceJSON(expected) {
		return factInvalid("fact subject identity mismatch")
	}
	return nil
}

// factEvidenceItemIdentity captures normalized equivalence material: two
// evidence items with the same fact identity and equivalent material
// canonicalize, while any differing material conflicts deterministically.
type factEvidenceItemIdentity struct {
	Kind         string `json:"kind"`
	Subject      string `json:"subject"`
	Document     string `json:"document"`
	NativeType   string `json:"nativeType"`
	Context      string `json:"context"`
	Property     string `json:"property"`
	ValueKind    string `json:"valueKind"`
	ValueRaw     string `json:"valueRaw"`
	Unit         string `json:"unit"`
	Availability string `json:"availability"`
	Diagnostic   string `json:"diagnostic"`
}

func factEvidenceItemIdentityOf(item FactRuntimeEvidenceFact) (string, string, error) {
	identity := factEvidenceItemIdentity{
		Kind:         item.Kind,
		Subject:      occurrenceJSON(item.Subject.Entity),
		Document:     item.Subject.Document,
		NativeType:   item.Subject.NativeType,
		Context:      occurrenceJSON(normalizedContext(item.Context)),
		Property:     item.Property,
		ValueKind:    item.ValueKind,
		Unit:         strings.TrimSpace(item.Unit),
		Availability: item.Availability,
		Diagnostic:   item.Diagnostic,
	}
	if item.Availability == recordcontract.FactAvailabilityObserved {
		canonical, err := recordcontract.CanonicalFactScalar(item.ValueKind, string(item.Value))
		if err != nil {
			return "", "", err
		}
		identity.ValueRaw = canonical
	}
	slot, err := factEvidenceItemBindingKey(item)
	if err != nil {
		return "", "", err
	}
	return occurrenceJSON(identity), slot, nil
}

// factEvidenceItemBindingKey derives the requested-binding slot key.
func factEvidenceItemBindingKey(item FactRuntimeEvidenceFact) (string, error) {
	return recordcontract.FactBindingKey(recordcontract.FactBinding{
		Kind:     item.Kind,
		Subject:  item.Subject.Entity,
		Context:  item.Context,
		Property: item.Property,
	})
}

// factEvidenceItemObservationFact maps one accepted evidence fact into the
// normalized observation fact shape. The Engine invents no facts: every
// material field comes from the evidence bytes or the explicit request.
func factEvidenceItemObservationFact(item FactRuntimeEvidenceFact, binding recordcontract.FactBinding, evidence recordcontract.ObservationEvidence) (recordcontract.ObservationFact, error) {
	subject := binding.Subject
	fact := recordcontract.ObservationFact{
		Kind: recordcontract.ObservationKindFact,
		Subject: recordcontract.ObservationSubject{
			Name:       subject.Name,
			Resource:   &recordcontract.ReferenceResource{ProjectID: subject.Resource.ProjectID, Kind: subject.Resource.Kind, LogicalID: subject.Resource.LogicalID},
			NativeKind: subject.NativeKind,
			SemanticID: subject.SemanticID,
			Context:    append([]recordcontract.OccurrenceContextFrame{}, binding.Context...),
		},
		Key:          binding.Kind,
		Property:     binding.Property,
		Unit:         binding.Unit,
		Availability: item.Availability,
		Evidence:     evidence,
	}
	switch item.Availability {
	case recordcontract.FactAvailabilityObserved:
		if item.Diagnostic != "" {
			return recordcontract.ObservationFact{}, factInvalid("observed fact evidence must not carry a diagnostic")
		}
		if item.ValueKind != binding.ValueKind {
			return recordcontract.ObservationFact{}, factInvalid("evidence value kind disagrees with requested binding")
		}
		itemUnit := strings.TrimSpace(item.Unit)
		if itemUnit != strings.TrimSpace(binding.Unit) {
			return recordcontract.ObservationFact{}, factInvalid("evidence unit disagrees with requested binding")
		}
		canonical, err := recordcontract.CanonicalFactScalar(binding.ValueKind, string(item.Value))
		if err != nil {
			return recordcontract.ObservationFact{}, err
		}
		fact.Value = recordcontract.ObservationValue{Kind: binding.ValueKind, Raw: canonical}
	case recordcontract.FactAvailabilityUnavailable, recordcontract.FactAvailabilityUnsupported:
		if item.Diagnostic == "" {
			return recordcontract.ObservationFact{}, factInvalid("unobserved fact evidence requires a diagnostic")
		}
		if !occurrenceExactText(item.Diagnostic) {
			return recordcontract.ObservationFact{}, factInvalid("invalid fact diagnostic")
		}
	default:
		return recordcontract.ObservationFact{}, factInvalid("invalid availability")
	}
	return fact, nil
}

// MapFactEvidence accepts controlled evidence without a native producer. An
// omitted evidence file is explicit unavailability for every requested binding;
// a present evidence file must cover every requested binding. Equivalent
// duplicate evidence items canonicalize; conflicting evidence fails
// deterministically instead of selecting one value.
func MapFactEvidence(input FactMappingInput) ([]recordcontract.ObservationFact, error) {
	if input.Request == nil {
		if len(input.Content) != 0 {
			return nil, factInvalid("unsolicited fact evidence")
		}
		return nil, nil
	}
	request := *recordcontract.CloneFactRequest(input.Request)
	if err := recordcontract.ValidateFactRequest(request); err != nil {
		return nil, err
	}
	// Declared-resource joins are Engine-owned; an undeclared subject is a
	// deterministic failure rather than a runtime lookup.
	bindingKeys := make(map[string]struct{}, len(request.Bindings))
	for _, binding := range request.Bindings {
		resource := binding.Subject.Resource
		if _, err := input.Resources.Lookup(resource.ProjectID, resource.Kind, resource.LogicalID); err != nil {
			return nil, err
		}
		for _, frame := range binding.Context {
			parent := frame.Parent.Resource
			if _, err := input.Resources.Lookup(parent.ProjectID, parent.Kind, parent.LogicalID); err != nil {
				return nil, err
			}
		}
		key, err := recordcontract.FactBindingKey(binding)
		if err != nil {
			return nil, err
		}
		bindingKeys[key] = struct{}{}
	}

	var evidence *FactRuntimeEvidence
	digest := ""
	if len(input.Content) != 0 {
		decoded, err := DecodeFactEvidence(input.Content)
		if err != nil {
			return nil, err
		}
		if decoded.Kind != "native_fact_evidence" {
			return nil, factInvalid("unexpected fact evidence kind")
		}
		if !occurrenceExactText(decoded.FreeCADVersion) {
			return nil, factInvalid("invalid native runtime version")
		}
		// Runtime evidence order must not influence acceptance or output.
		items := append([]FactRuntimeEvidenceFact{}, decoded.Facts...)
		sort.SliceStable(items, func(i, j int) bool {
			return occurrenceJSON(items[i]) < occurrenceJSON(items[j])
		})
		decoded.Facts = items
		evidence = &decoded
		digest = fmt.Sprintf("%x", sha256.Sum256(input.Content))
	}

	itemsByKey := make(map[string]FactRuntimeEvidenceFact, len(request.Bindings))
	identities := make(map[string]string, len(request.Bindings))
	if evidence != nil {
		for _, item := range evidence.Facts {
			identity, key, err := factEvidenceItemIdentityOf(item)
			if err != nil {
				return nil, err
			}
			if _, requested := bindingKeys[key]; !requested {
				return nil, factInvalid("unsolicited fact evidence")
			}
			if previousIdentity, exists := identities[key]; exists {
				if previousIdentity != identity {
					return nil, factInvalid("conflicting duplicate fact evidence")
				}
				// Equivalent duplicates canonicalize into one fact.
				continue
			}
			identities[key] = identity
			itemsByKey[key] = item
		}
	}

	var factEvidence recordcontract.ObservationEvidence
	if evidence != nil {
		factEvidence = recordcontract.ObservationEvidence{
			SourceKind:   recordcontract.FactEvidenceSourceKind,
			SourceRef:    recordcontract.FactEvidenceSourceRef,
			DigestSHA256: digest,
		}
	}

	facts := make([]recordcontract.ObservationFact, 0, len(request.Bindings))
	for _, binding := range request.Bindings {
		key, err := recordcontract.FactBindingKey(binding)
		if err != nil {
			return nil, err
		}
		item, covered := itemsByKey[key]
		var fact recordcontract.ObservationFact
		if !covered {
			if evidence != nil {
				return nil, factInvalid("fact evidence does not cover a requested binding")
			}
			// No evidence file at all is explicit unavailability for the
			// requested binding; the fact stays a fact with an explicit state.
			subject := binding.Subject
			fact = recordcontract.ObservationFact{
				Kind: recordcontract.ObservationKindFact,
				Subject: recordcontract.ObservationSubject{
					Name:       subject.Name,
					Resource:   &recordcontract.ReferenceResource{ProjectID: subject.Resource.ProjectID, Kind: subject.Resource.Kind, LogicalID: subject.Resource.LogicalID},
					NativeKind: subject.NativeKind,
					SemanticID: subject.SemanticID,
					Context:    append([]recordcontract.OccurrenceContextFrame{}, binding.Context...),
				},
				Key:          binding.Kind,
				Property:     binding.Property,
				Availability: recordcontract.FactAvailabilityUnavailable,
			}
		} else {
			if item.Availability == recordcontract.FactAvailabilityObserved {
				if err := qualifyFactSubject(input.Resources, item.Subject, binding.Subject); err != nil {
					return nil, err
				}
				if occurrenceJSON(normalizedContext(item.Context)) != occurrenceJSON(normalizedContext(binding.Context)) {
					return nil, factInvalid("fact subject context mismatch")
				}
			}
			fact, err = factEvidenceItemObservationFact(item, binding, factEvidence)
			if err != nil {
				return nil, err
			}
		}
		if binding.Requirement == recordcontract.FactRequirementRequired && fact.Availability != recordcontract.FactAvailabilityObserved {
			return nil, fmt.Errorf("%w: %s %s", recordcontract.ErrRequiredFactUnavailable, binding.Property, fact.Availability)
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func normalizedContext(context []recordcontract.OccurrenceContextFrame) []recordcontract.OccurrenceContextFrame {
	if context == nil {
		return []recordcontract.OccurrenceContextFrame{}
	}
	return context
}

// FactEvidenceProvenance returns the shared raw fact evidence provenance of
// normalized facts. Every fact from one mapping shares one evidence state.
func FactEvidenceProvenance(facts []recordcontract.ObservationFact) recordcontract.ObservationEvidence {
	for _, fact := range facts {
		if fact.Evidence.SourceKind != "" || fact.Evidence.SourceRef != "" || fact.Evidence.DigestSHA256 != "" {
			return fact.Evidence
		}
	}
	return recordcontract.ObservationEvidence{}
}
