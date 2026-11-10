package recordcontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"parametron/internal/engine/resourcebinding"
)

const OccurrenceSchemaVersion = "1.0"
const OccurrenceDirectMemberMechanism = "freecad-1.1.4-assembly-group-individual-link"

var ErrInvalidOccurrence = errors.New("invalid occurrence contract")
var ErrOccurrenceCompletenessRequired = errors.New("complete occurrence scope required")

// OccurrenceEntity is an exact native object qualified by declared resource identity.
// SemanticID is an annotation and never participates in native identity.
type OccurrenceEntity struct {
	Resource   ReferenceResource `json:"resource"`
	NativeKind string            `json:"nativeKind"`
	Name       string            `json:"name"`
	SemanticID string            `json:"semanticId,omitempty"`
}

// OccurrenceContextFrame retains a bounded enclosing instance coordinate. It is
// context only, not a certificate of recursive membership or completeness.
type OccurrenceContextFrame struct {
	Parent OccurrenceEntity `json:"parent"`
	Member string           `json:"member"`
}

type OccurrenceScope struct {
	Parent    OccurrenceEntity         `json:"parent"`
	Context   []OccurrenceContextFrame `json:"context"`
	Mechanism string                   `json:"mechanism"`
}

// OccurrenceRequest is explicit opt-in intent. Nil intent requests no observation.
// Requirement is optional or complete. The initial contract requests one scope.
type OccurrenceRequest struct {
	SchemaVersion string          `json:"schemaVersion"`
	Requirement   string          `json:"requirement"`
	Scope         OccurrenceScope `json:"scope"`
}

type Occurrence struct {
	Member string           `json:"member"`
	Target OccurrenceEntity `json:"target"`
}

type OccurrenceQuantity struct {
	Target   OccurrenceEntity `json:"target"`
	Quantity int              `json:"quantity"`
}

// OccurrenceSummary separates accepted members from quantity availability. A nil
// quantity list means unavailable; an empty non-nil list is a complete empty scope.
type OccurrenceSummary struct {
	SchemaVersion string               `json:"schemaVersion"`
	Scope         OccurrenceScope      `json:"scope"`
	Completeness  string               `json:"completeness"`
	Occurrences   []Occurrence         `json:"occurrences"`
	Quantities    []OccurrenceQuantity `json:"quantities"`
	Evidence      ObservationEvidence  `json:"evidence"`
}

func occurrenceJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func occurrenceExact(s string) bool {
	return s != "" && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00/\\. \t\r\n")
}
func validateOccurrenceEntity(e OccurrenceEntity) error {
	if e.NativeKind != "object" || !occurrenceExact(e.Name) || e.Resource.Kind != resourcebinding.ResourceKindModel || e.Resource.ProjectID == "" || strings.TrimSpace(e.Resource.ProjectID) != e.Resource.ProjectID || e.Resource.LogicalID == "" || strings.TrimSpace(e.Resource.LogicalID) != e.Resource.LogicalID || strings.ContainsRune(e.Resource.ProjectID+e.Resource.LogicalID+e.SemanticID, '\x00') || strings.TrimSpace(e.SemanticID) != e.SemanticID {
		return fmt.Errorf("%w: invalid resource-qualified native entity", ErrInvalidOccurrence)
	}
	return nil
}

// OccurrenceEntityKey excludes optional semantic annotation.
func OccurrenceEntityKey(e OccurrenceEntity) string { e.SemanticID = ""; return occurrenceJSON(e) }

func ValidateOccurrenceScope(s OccurrenceScope) error {
	if err := validateOccurrenceEntity(s.Parent); err != nil {
		return err
	}
	if s.Mechanism == "" || strings.TrimSpace(s.Mechanism) != s.Mechanism || strings.ContainsRune(s.Mechanism, '\x00') || s.Context == nil || len(s.Context) > 16 {
		return fmt.Errorf("%w: invalid mechanism or bounded context", ErrInvalidOccurrence)
	}
	seen := map[string]bool{}
	for _, f := range s.Context {
		if err := validateOccurrenceEntity(f.Parent); err != nil {
			return err
		}
		if !occurrenceExact(f.Member) {
			return fmt.Errorf("%w: malformed context member", ErrInvalidOccurrence)
		}
		key := OccurrenceEntityKey(f.Parent)
		if seen[key] || key == OccurrenceEntityKey(s.Parent) {
			return fmt.Errorf("%w: cyclic parent context", ErrInvalidOccurrence)
		}
		seen[key] = true
	}
	return nil
}

func ValidateOccurrenceRequest(r OccurrenceRequest) error {
	if r.SchemaVersion != OccurrenceSchemaVersion || (r.Requirement != "optional" && r.Requirement != "complete") {
		return fmt.Errorf("%w: invalid request version or requirement", ErrInvalidOccurrence)
	}
	return ValidateOccurrenceScope(r.Scope)
}

// OccurrenceKey deliberately excludes the target, so conflicting targets cannot
// hide behind different duplicate keys. Context and owning parent remain distinct.
func OccurrenceKey(s OccurrenceScope, member string) (string, error) {
	if err := ValidateOccurrenceScope(s); err != nil {
		return "", err
	}
	if !occurrenceExact(member) {
		return "", fmt.Errorf("%w: invalid native member", ErrInvalidOccurrence)
	}
	s.Parent.SemanticID = ""
	s.Context = append([]OccurrenceContextFrame{}, s.Context...)
	for i := range s.Context {
		s.Context[i].Parent.SemanticID = ""
	}
	return occurrenceJSON(struct {
		Scope  OccurrenceScope
		Member string
	}{s, member}), nil
}

// NormalizeOccurrenceSummary defensively copies and sorts; validation rejects
// conflicting or duplicate members rather than choosing one.
func NormalizeOccurrenceSummary(s *OccurrenceSummary) *OccurrenceSummary {
	if s == nil {
		return nil
	}
	out := *s
	if s.Scope.Context != nil {
		out.Scope.Context = append([]OccurrenceContextFrame{}, s.Scope.Context...)
	}
	if s.Occurrences != nil {
		out.Occurrences = append([]Occurrence{}, s.Occurrences...)
	}
	if s.Quantities != nil {
		out.Quantities = append([]OccurrenceQuantity{}, s.Quantities...)
	}
	sort.Slice(out.Occurrences, func(i, j int) bool { return out.Occurrences[i].Member < out.Occurrences[j].Member })
	sort.Slice(out.Quantities, func(i, j int) bool {
		return OccurrenceEntityKey(out.Quantities[i].Target) < OccurrenceEntityKey(out.Quantities[j].Target)
	})
	return &out
}

// RollupOccurrences counts only distinct members already accepted by validation.
// Semantic annotations do not split groups and are omitted from rollup targets.
func RollupOccurrences(members []Occurrence) []OccurrenceQuantity {
	groups := map[string]OccurrenceQuantity{}
	for _, o := range members {
		t := o.Target
		t.SemanticID = ""
		k := OccurrenceEntityKey(t)
		q := groups[k]
		q.Target = t
		q.Quantity++
		groups[k] = q
	}
	out := make([]OccurrenceQuantity, 0, len(groups))
	for _, q := range groups {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return OccurrenceEntityKey(out[i].Target) < OccurrenceEntityKey(out[j].Target) })
	return out
}

func ValidateOccurrenceSummary(s OccurrenceSummary) error {
	if s.SchemaVersion != OccurrenceSchemaVersion {
		return fmt.Errorf("%w: unsupported summary version", ErrInvalidOccurrence)
	}
	if err := ValidateOccurrenceScope(s.Scope); err != nil {
		return err
	}
	switch s.Completeness {
	case "complete", "incomplete", "unsupported", "unavailable":
	default:
		return fmt.Errorf("%w: invalid completeness", ErrInvalidOccurrence)
	}
	if s.Completeness == "complete" && (s.Scope.Mechanism != OccurrenceDirectMemberMechanism || len(s.Scope.Context) != 0) {
		return fmt.Errorf("%w: unsupported complete scope", ErrInvalidOccurrence)
	}
	if s.Occurrences == nil || (s.Completeness == "unavailable" && len(s.Occurrences) != 0) {
		return fmt.Errorf("%w: invalid accepted member set", ErrInvalidOccurrence)
	}
	if len(s.Occurrences) > 0 && (s.Scope.Mechanism != OccurrenceDirectMemberMechanism || len(s.Scope.Context) != 0) {
		return fmt.Errorf("%w: accepted occurrences require supported direct scope", ErrInvalidOccurrence)
	}
	seen := map[string]bool{}
	for _, o := range s.Occurrences {
		k, err := OccurrenceKey(s.Scope, o.Member)
		if err != nil {
			return err
		}
		if seen[k] {
			return fmt.Errorf("%w: duplicate occurrence identity", ErrInvalidOccurrence)
		}
		seen[k] = true
		if err := validateOccurrenceEntity(o.Target); err != nil {
			return err
		}
	}
	if s.Completeness != "complete" {
		if s.Quantities != nil {
			return fmt.Errorf("%w: unavailable quantity must be null", ErrInvalidOccurrence)
		}
	} else if s.Quantities == nil || occurrenceJSON(RollupOccurrences(s.Occurrences)) != occurrenceJSON(s.Quantities) {
		return fmt.Errorf("%w: quantity differs from distinct occurrence rollup", ErrInvalidOccurrence)
	}
	if s.Evidence.SourceKind != "" || s.Evidence.SourceRef != "" || s.Evidence.DigestSHA256 != "" {
		if s.Evidence.SourceKind != "occurrence-evidence" || s.Evidence.SourceRef != "raw/runtime/prm.occurrence-evidence.json" || s.Evidence.DigestSHA256 == "" {
			return fmt.Errorf("%w: canonical occurrence evidence required", ErrInvalidOccurrence)
		}
	} else if s.Completeness != "unavailable" {
		return fmt.Errorf("%w: observed scope requires raw evidence provenance", ErrInvalidOccurrence)
	}
	if err := validateObservationEvidence(s.Evidence, 0); err != nil {
		return err
	}
	return nil
}

// CloneOccurrenceRequest copies caller-owned context without normalizing intent.
func CloneOccurrenceRequest(r *OccurrenceRequest) *OccurrenceRequest {
	if r == nil {
		return nil
	}
	out := *r
	if r.Scope.Context != nil {
		out.Scope.Context = append([]OccurrenceContextFrame{}, r.Scope.Context...)
	}
	return &out
}
