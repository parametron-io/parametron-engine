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

const OccurrenceEvidenceRef = "raw/runtime/" + OccurrenceEvidenceFilename

// OccurrenceEvidenceSchemaVersion adds verified auxiliary contents to evidence.
// Requests and normalized summaries retain their independent 1.0 contracts.
const OccurrenceEvidenceSchemaVersion = "1.1"

// OccurrenceNativeEntity retains both explicit logical identity and the native
// materialized document coordinate; both must join the same inventory binding.
type OccurrenceNativeEntity struct {
	Entity     recordcontract.OccurrenceEntity `json:"entity"`
	Document   string                          `json:"document"`
	NativeType string                          `json:"nativeType"`
}

// OccurrenceRuntimeMember classifies every enumerated direct Group member.
// Supported classifications are occurrence, unresolved, unsupported, and (in
// evidence 1.1) auxiliary for an explicitly inspected empty joint group only.
type OccurrenceRuntimeMember struct {
	Name                   string                           `json:"name"`
	Parent                 recordcontract.OccurrenceEntity  `json:"parent"`
	NativeType             string                           `json:"nativeType"`
	ElementCount           *int                             `json:"elementCount"`
	MembershipCorroborated bool                             `json:"membershipCorroborated"`
	Classification         string                           `json:"classification"`
	Resolution             string                           `json:"resolution"`
	NativeStateValid       *bool                            `json:"nativeStateValid"`
	Target                 *OccurrenceNativeEntity          `json:"target"`
	Diagnostic             string                           `json:"diagnostic,omitempty"`
	Contents               *OccurrenceRuntimeMemberContents `json:"contents,omitempty"`
}

// OccurrenceRuntimeMemberContents describes native child inspection, not recursive
// occurrence expansion. A nil Group is missing evidence, never an empty inventory.
type OccurrenceRuntimeMemberContents struct {
	InspectionCompleted bool     `json:"inspectionCompleted"`
	Group               []string `json:"group"`
}

// OccurrenceRuntimeEvidence is independent of generic reference traversal. Group
// is the entire native inventory, not a filtered list of recognized links.
type OccurrenceRuntimeEvidence struct {
	SchemaVersion       string                         `json:"schemaVersion"`
	Kind                string                         `json:"kind"`
	FreeCADVersion      string                         `json:"freecadVersion"`
	Scope               recordcontract.OccurrenceScope `json:"scope"`
	Parent              *OccurrenceNativeEntity        `json:"parent"`
	Completeness        string                         `json:"completeness"`
	InspectionCompleted bool                           `json:"inspectionCompleted"`
	Group               []string                       `json:"group"`
	Members             []OccurrenceRuntimeMember      `json:"members"`
	Diagnostics         []string                       `json:"diagnostics"`
}

type OccurrenceMappingInput struct {
	Request                *recordcontract.OccurrenceRequest
	Resources              resourcebinding.Inventory
	ExpectedSourceDocument string
	Content                []byte
}

func occurrenceInvalid(message string) error {
	return fmt.Errorf("%w: %s", recordcontract.ErrInvalidOccurrence, message)
}

// DecodeOccurrenceEvidence rejects unknown fields, duplicate JSON keys and
// trailing values. Exact bytes remain the caller's provenance material.
func DecodeOccurrenceEvidence(content []byte) (OccurrenceRuntimeEvidence, error) {
	if err := uniqueOccurrenceJSON(content); err != nil {
		return OccurrenceRuntimeEvidence{}, err
	}
	d := json.NewDecoder(bytes.NewReader(content))
	d.DisallowUnknownFields()
	var e OccurrenceRuntimeEvidence
	if err := d.Decode(&e); err != nil {
		return e, fmt.Errorf("%w: %v", recordcontract.ErrInvalidOccurrence, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return e, occurrenceInvalid("trailing JSON")
	}
	switch e.SchemaVersion {
	case recordcontract.OccurrenceSchemaVersion:
		// Preserve the closed 1.0 member shape, including rejection of an explicit
		// contents:null. Pointer values alone cannot distinguish null from omission.
		var raw struct {
			Members []map[string]json.RawMessage `json:"members"`
		}
		if err := json.Unmarshal(content, &raw); err != nil {
			return e, fmt.Errorf("%w: %v", recordcontract.ErrInvalidOccurrence, err)
		}
		for _, member := range raw.Members {
			for field := range member {
				if strings.EqualFold(field, "contents") {
					return e, occurrenceInvalid("member contents require evidence schema 1.1")
				}
			}
		}
	case OccurrenceEvidenceSchemaVersion:
	default:
		return e, occurrenceInvalid("unsupported evidence schema version")
	}
	return e, nil
}

func uniqueOccurrenceJSON(content []byte) error {
	d := json.NewDecoder(bytes.NewReader(content))
	var value func() error
	value = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if delim == '{' {
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := k.(string)
				if !ok || seen[strings.ToLower(s)] {
					return occurrenceInvalid("duplicate JSON field")
				}
				seen[strings.ToLower(s)] = true
				if err := value(); err != nil {
					return err
				}
			}
		} else if delim == '[' {
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
		} else {
			return occurrenceInvalid("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(); err != nil {
		return fmt.Errorf("%w: %v", recordcontract.ErrInvalidOccurrence, err)
	}
	if _, err := d.Token(); err != io.EOF {
		return occurrenceInvalid("trailing JSON")
	}
	return nil
}

func qualifyOccurrence(e OccurrenceNativeEntity, inventory resourcebinding.Inventory) error {
	if !occurrenceExactText(e.NativeType) {
		return occurrenceInvalid("native type required")
	}
	if err := resourcebinding.ValidateCoordinate(e.Document); err != nil {
		return err
	}
	b, err := inventory.LookupCoordinate(e.Document, true)
	if err != nil {
		return err
	}
	r := e.Entity.Resource
	if b.ProjectID != r.ProjectID || b.Kind != r.Kind || b.LogicalID != r.LogicalID {
		return occurrenceInvalid("logical identity disagrees with materialized coordinate")
	}
	// Reuse native entity validation through a minimal exact scope.
	return recordcontract.ValidateOccurrenceScope(recordcontract.OccurrenceScope{Parent: e.Entity, Context: []recordcontract.OccurrenceContextFrame{}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism})
}

func occurrenceJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// MapOccurrenceEvidence accepts controlled evidence without a native producer.
// Omitted optional evidence is unavailable, never an inspected empty scope.
func MapOccurrenceEvidence(input OccurrenceMappingInput) (*recordcontract.OccurrenceSummary, error) {
	if input.Request == nil {
		if len(input.Content) != 0 {
			return nil, occurrenceInvalid("unsolicited evidence")
		}
		return nil, nil
	}
	r := *input.Request
	if err := recordcontract.ValidateOccurrenceRequest(r); err != nil {
		return nil, err
	}
	for _, b := range input.Resources.Bindings() {
		if b.MaterializedCoordinate == "" {
			return nil, occurrenceInvalid("resource inventory is not materialized")
		}
	}
	for _, entity := range append([]recordcontract.OccurrenceContextFrame{{Parent: r.Scope.Parent}}, r.Scope.Context...) {
		p := entity.Parent.Resource
		if _, err := input.Resources.Lookup(p.ProjectID, p.Kind, p.LogicalID); err != nil {
			return nil, err
		}
	}
	if input.ExpectedSourceDocument != "" {
		b, err := input.Resources.LookupCoordinate(input.ExpectedSourceDocument, true)
		if err != nil {
			return nil, err
		}
		p := r.Scope.Parent.Resource
		if b.ProjectID != p.ProjectID || b.Kind != p.Kind || b.LogicalID != p.LogicalID {
			return nil, occurrenceInvalid("requested parent is not selected source")
		}
	}
	s := &recordcontract.OccurrenceSummary{SchemaVersion: recordcontract.OccurrenceSchemaVersion, Scope: r.Scope, Completeness: "unavailable", Occurrences: []recordcontract.Occurrence{}}
	finish := func() (*recordcontract.OccurrenceSummary, error) {
		if r.Requirement == "complete" && s.Completeness != "complete" {
			return nil, fmt.Errorf("%w: %s", recordcontract.ErrOccurrenceCompletenessRequired, s.Completeness)
		}
		s = recordcontract.NormalizeOccurrenceSummary(s)
		if err := recordcontract.ValidateOccurrenceSummary(*s); err != nil {
			return nil, err
		}
		return s, nil
	}
	if len(input.Content) == 0 {
		return finish()
	}
	e, err := DecodeOccurrenceEvidence(input.Content)
	if err != nil {
		return nil, err
	}
	if e.Kind != "native_occurrence_evidence" || occurrenceJSON(e.Scope) != occurrenceJSON(r.Scope) {
		return nil, occurrenceInvalid("evidence version, kind or requested scope mismatch")
	}
	if e.Diagnostics == nil || e.Group == nil || e.Members == nil {
		return nil, occurrenceInvalid("explicit diagnostics, Group and member inventory required")
	}
	for _, d := range e.Diagnostics {
		if !occurrenceExactText(d) {
			return nil, occurrenceInvalid("invalid diagnostic")
		}
	}
	switch e.Completeness {
	case "complete", "incomplete", "unsupported", "unavailable":
	default:
		return nil, occurrenceInvalid("invalid completeness")
	}
	supported := e.Scope.Mechanism == recordcontract.OccurrenceDirectMemberMechanism && e.FreeCADVersion == "1.1.4" && len(e.Scope.Context) == 0
	if !supported && e.Completeness != "unsupported" && e.Completeness != "unavailable" {
		return nil, occurrenceInvalid("unproven mechanism, version or nested scope")
	}
	s.Completeness = e.Completeness
	digest := fmt.Sprintf("%x", sha256.Sum256(input.Content))
	s.Evidence = recordcontract.ObservationEvidence{SourceKind: "occurrence-evidence", SourceRef: OccurrenceEvidenceRef, DigestSHA256: digest}
	if e.Completeness == "unavailable" {
		if e.Parent != nil || e.InspectionCompleted || len(e.Group) != 0 || len(e.Members) != 0 || len(e.Diagnostics) == 0 {
			return nil, occurrenceInvalid("contradictory unavailable scope")
		}
		return finish()
	}
	if e.Parent == nil {
		return nil, occurrenceInvalid("observed parent required")
	}
	if err := qualifyOccurrence(*e.Parent, input.Resources); err != nil {
		return nil, err
	}
	if occurrenceJSON(e.Parent.Entity) != occurrenceJSON(r.Scope.Parent) {
		return nil, occurrenceInvalid("parent identity mismatch")
	}
	if input.ExpectedSourceDocument != "" && e.Parent.Document != input.ExpectedSourceDocument {
		return nil, occurrenceInvalid("parent document mismatch")
	}
	if supported && e.Parent.NativeType != "Assembly::AssemblyObject" && e.Completeness != "unsupported" {
		return nil, occurrenceInvalid("unsupported assembly root")
	}
	supported = supported && e.Parent.NativeType == "Assembly::AssemblyObject"
	group := map[string]bool{}
	names := append([]string{}, e.Group...)
	sort.Strings(names)
	for _, n := range names {
		if _, err := recordcontract.OccurrenceKey(r.Scope, n); err != nil {
			return nil, err
		}
		if group[n] {
			return nil, occurrenceInvalid("duplicate native Group member")
		}
		group[n] = true
	}
	members := append([]OccurrenceRuntimeMember{}, e.Members...)
	sort.Slice(members, func(i, j int) bool { return occurrenceJSON(members[i]) < occurrenceJSON(members[j]) })
	seen := map[string]string{}
	unhandled := false
	for _, m := range members {
		if !group[m.Name] || occurrenceJSON(m.Parent) != occurrenceJSON(r.Scope.Parent) || !m.MembershipCorroborated {
			return nil, occurrenceInvalid("member outside requested corroborated assembly")
		}
		raw := occurrenceJSON(m)
		if previous, ok := seen[m.Name]; ok {
			if previous != raw {
				return nil, occurrenceInvalid("conflicting native occurrence identity")
			}
			continue
		}
		seen[m.Name] = raw
		if !occurrenceExactText(m.NativeType) || (m.Diagnostic != "" && !occurrenceExactText(m.Diagnostic)) {
			return nil, occurrenceInvalid("invalid member type or diagnostic")
		}
		if m.ElementCount != nil && *m.ElementCount < 0 {
			return nil, occurrenceInvalid("negative ElementCount")
		}
		if m.Contents != nil && m.Classification != "auxiliary" {
			return nil, occurrenceInvalid("member contents require auxiliary classification")
		}
		switch m.Resolution {
		case "resolved", "unresolved", "unsupported", "unavailable":
		default:
			return nil, occurrenceInvalid("invalid member resolution")
		}
		switch m.Classification {
		case "occurrence":
			if !supported || m.NativeType != "App::Link" || m.ElementCount == nil || *m.ElementCount != 0 || m.Target == nil || m.Diagnostic != "" || m.Resolution != "resolved" || m.NativeStateValid == nil || !*m.NativeStateValid {
				return nil, occurrenceInvalid("unsupported individual link evidence")
			}
			if err := qualifyOccurrence(*m.Target, input.Resources); err != nil {
				return nil, err
			}
			// Count the exact referenced entity, including App::Part, without
			// expanding its contents. Assembly definitions and link targets remain unsupported.
			switch m.Target.NativeType {
			case "PartDesign::Body", "Part::Feature", "App::Part":
			default:
				return nil, occurrenceInvalid("unsupported resolved component target type")
			}
			s.Occurrences = append(s.Occurrences, recordcontract.Occurrence{Member: m.Name, Target: m.Target.Entity})
		case "auxiliary":
			if e.SchemaVersion != OccurrenceEvidenceSchemaVersion || !supported || m.NativeType != "Assembly::JointGroup" || m.ElementCount != nil || m.Target != nil || m.Diagnostic != "" || m.Resolution != "resolved" || m.NativeStateValid == nil || !*m.NativeStateValid || m.Contents == nil || !m.Contents.InspectionCompleted || m.Contents.Group == nil || len(m.Contents.Group) != 0 {
				return nil, occurrenceInvalid("unsupported auxiliary member evidence")
			}
			// Membership and identity were checked above. This inspected empty
			// member contributes no occurrence; whole-scope coverage is still required.
		case "unresolved", "unsupported":
			if m.Diagnostic == "" || m.Target != nil || m.Resolution == "resolved" || (m.Classification == "unsupported" && m.Resolution != "unsupported") || (m.Classification == "unresolved" && m.Resolution != "unresolved" && m.Resolution != "unavailable") {
				return nil, occurrenceInvalid("unaccepted member requires diagnostic and no resolved target")
			}
			unhandled = true
		default:
			return nil, occurrenceInvalid("unclassified native member")
		}
	}
	if e.InspectionCompleted && len(group) != len(seen) {
		return nil, occurrenceInvalid("completed inventory has unclassified members")
	}
	if e.Completeness == "complete" {
		if !supported || e.Parent.NativeType != "Assembly::AssemblyObject" || !e.InspectionCompleted || unhandled || len(e.Diagnostics) != 0 {
			return nil, occurrenceInvalid("contradictory complete scope")
		}
		s.Quantities = recordcontract.RollupOccurrences(s.Occurrences)
	} else if len(e.Diagnostics) == 0 {
		return nil, occurrenceInvalid("noncomplete scope requires diagnostics")
	}
	return finish()
}

func occurrenceExactText(s string) bool {
	return s != "" && strings.TrimSpace(s) == s && !strings.ContainsRune(s, '\x00')
}

// DecodeOccurrenceRequest strictly accepts the explicit versioned request sidecar.
func DecodeOccurrenceRequest(content []byte) (recordcontract.OccurrenceRequest, error) {
	var request recordcontract.OccurrenceRequest
	if err := uniqueOccurrenceJSON(content); err != nil {
		return request, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("%w: %v", recordcontract.ErrInvalidOccurrence, err)
	}
	if err := recordcontract.ValidateOccurrenceRequest(request); err != nil {
		return request, err
	}
	return request, nil
}
