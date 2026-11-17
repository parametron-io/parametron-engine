package integrity

import (
	"fmt"
	"sort"
	"strings"
)

// LockState summarizes whether accepted project-lock source identities were
// verified for a run or for one declared resource.
type LockState string

const (
	// LockStateVerified reports that every required declared resource matched
	// its accepted locked source identity.
	LockStateVerified LockState = "verified"
	// LockStateMismatch reports that source bytes differ from the accepted
	// locked source identity.
	LockStateMismatch LockState = "mismatch"
	// LockStateMissing reports a declared resource that is absent from the
	// required project-lock scope.
	LockStateMissing LockState = "missing"
	// LockStateMalformed reports a locked digest that is not canonical.
	LockStateMalformed LockState = "malformed"
	// LockStateUnavailable reports a project lock that cannot be read or parsed.
	LockStateUnavailable LockState = "unavailable"
	// LockStateNotPerformed reports an unlocked workflow, where lock
	// verification was deliberately not performed.
	LockStateNotPerformed LockState = "not_performed"
)

// Status summarizes one stage of the per-resource identity chain.
type Status string

const (
	// StatusVerified reports agreement between Engine and reported evidence.
	StatusVerified Status = "verified"
	// StatusMismatch reports disagreeing content identity.
	StatusMismatch Status = "mismatch"
	// StatusMissing reports absent required material or evidence.
	StatusMissing Status = "missing"
	// StatusMalformed reports evidence that is not in canonical form.
	StatusMalformed Status = "malformed"
	// StatusInaccessible reports persisted bytes that cannot be read.
	StatusInaccessible Status = "inaccessible"
	// StatusUnavailable reports a declared resource the runtime could not access.
	StatusUnavailable Status = "unavailable"
	// StatusUnsupported reports a declared resource kind the runtime cannot verify.
	StatusUnsupported Status = "unsupported"
	// StatusNotPerformed reports a stage that did not run.
	StatusNotPerformed Status = "not_performed"
	// StatusNotReported reports evidence the runtime did not provide.
	StatusNotReported Status = "not_reported"
)

// LockEntry is one accepted source identity read from a project lock.
type LockEntry struct {
	LogicalID string
	// Digest is the raw locked digest value. Its encoding is validated here so
	// a malformed lock is a distinguishable failure rather than a mismatch.
	Digest string
}

// LockScope is an adapted project-lock view without lock-file I/O.
type LockScope struct {
	// Present reports whether a project lock was located.
	Present bool
	// Invalid reports that a located lock could not be parsed or validated.
	Invalid bool
	// Models are the locked declared model entries. A structurally readable but
	// invalid lock still exposes its entries so a non-canonical digest is
	// classified as a malformed digest rather than an unreadable lock.
	Models []LockEntry
}

// ResourceSource is the accepted source content identity of one declared
// resource, keyed by the #48 declared resource identity and its canonical
// project-source coordinate.
type ResourceSource struct {
	LogicalID        string
	SourceCoordinate string
	SourceDigest     Digest
	LockState        LockState
	LockedDigest     Digest
}

// ProjectIntegrity carries project-scoped source identity and lock state.
// It is the lock/source half of the identity chain and is independent of any
// execution attempt.
type ProjectIntegrity struct {
	Lock      LockState
	Resources []ResourceSource
}

// Copy returns a defensive, deterministically ordered copy.
func (p *ProjectIntegrity) Copy() *ProjectIntegrity {
	if p == nil {
		return nil
	}
	return &ProjectIntegrity{Lock: p.Lock, Resources: normalizeSources(p.Resources)}
}

// Failure returns the deterministic integrity failure implied by the project
// source and lock state, or nil when the run may proceed.
//
// Verification of an unlocked workflow is deliberately not a failure; the
// provenance records that lock verification was not performed.
func (p *ProjectIntegrity) Failure() *Failure {
	if p == nil {
		return nil
	}
	state := p.Lock
	if state == "" {
		state = LockStateNotPerformed
	}
	switch state {
	case LockStateVerified, LockStateNotPerformed:
		return nil
	case LockStateUnavailable:
		return &Failure{Code: CodeLockUnavailable, Message: "project lock is not readable"}
	}

	sources := normalizeSources(p.Resources)
	for _, source := range sources {
		switch source.LockState {
		case LockStateMalformed:
			return &Failure{
				Code: CodeLockDigestMalformed, LogicalID: source.LogicalID,
				ExpectedDigest: source.LockedDigest,
				Message:        fmt.Sprintf("locked digest for resource %q is not canonical", source.LogicalID),
			}
		case LockStateMissing:
			return &Failure{
				Code: CodeLockResourceMissing, LogicalID: source.LogicalID,
				Coordinate: source.SourceCoordinate,
				Message:    fmt.Sprintf("declared resource %q is missing from the project lock", source.LogicalID),
			}
		case LockStateMismatch:
			return &Failure{
				Code: CodeLockSourceMismatch, LogicalID: source.LogicalID,
				Coordinate:     source.SourceCoordinate,
				ExpectedDigest: source.LockedDigest, ActualDigest: source.SourceDigest,
				Message: fmt.Sprintf("source content for resource %q changed after lock creation", source.LogicalID),
			}
		}
	}
	return nil
}

// EvaluateLock joins declared resource source identities to an adapted lock
// scope. Sources are ordered by logical resource identity so the selected
// failure is deterministic.
func EvaluateLock(scope LockScope, sources []ResourceSource) *ProjectIntegrity {
	normalized := normalizeSources(sources)

	if !scope.Present {
		for i := range normalized {
			normalized[i].LockState = LockStateNotPerformed
			normalized[i].LockedDigest = ""
		}
		return &ProjectIntegrity{Lock: LockStateNotPerformed, Resources: normalized}
	}
	if scope.Invalid {
		locked := make(map[string]string, len(scope.Models))
		for _, entry := range scope.Models {
			logicalID := strings.TrimSpace(entry.LogicalID)
			if logicalID == "" {
				continue
			}
			if _, exists := locked[logicalID]; !exists {
				locked[logicalID] = entry.Digest
			}
		}

		overall := LockStateUnavailable
		for i := range normalized {
			source := &normalized[i]
			source.LockedDigest = ""
			raw, ok := locked[source.LogicalID]
			if ok && ValidateDigest(raw) != nil {
				source.LockState = LockStateMalformed
				overall = LockStateMalformed
				continue
			}
			source.LockState = LockStateUnavailable
		}
		return &ProjectIntegrity{Lock: overall, Resources: normalized}
	}

	locked := make(map[string]string, len(scope.Models))
	for _, entry := range scope.Models {
		logicalID := strings.TrimSpace(entry.LogicalID)
		if logicalID == "" {
			continue
		}
		if _, exists := locked[logicalID]; !exists {
			locked[logicalID] = entry.Digest
		}
	}

	overall := LockStateVerified
	record := func(state LockState) {
		if lockStateSeverity(state) > lockStateSeverity(overall) {
			overall = state
		}
	}

	for i := range normalized {
		source := &normalized[i]
		raw, ok := locked[source.LogicalID]
		if !ok {
			source.LockState = LockStateMissing
			source.LockedDigest = ""
			record(LockStateMissing)
			continue
		}
		if err := ValidateDigest(raw); err != nil {
			source.LockState = LockStateMalformed
			source.LockedDigest = ""
			record(LockStateMalformed)
			continue
		}
		source.LockedDigest = Digest(raw)
		if source.SourceDigest != Digest(raw) {
			source.LockState = LockStateMismatch
			record(LockStateMismatch)
			continue
		}
		source.LockState = LockStateVerified
	}

	return &ProjectIntegrity{Lock: overall, Resources: normalized}
}

// lockStateSeverity orders lock problems so an overall state never hides the
// most fundamental problem. Equal severities keep the first observed state.
func lockStateSeverity(state LockState) int {
	switch state {
	case LockStateUnavailable:
		return 5
	case LockStateMalformed:
		return 4
	case LockStateMissing:
		return 3
	case LockStateMismatch:
		return 2
	case LockStateVerified:
		return 1
	default:
		return 0
	}
}

// ResourceOperation is the attempt-scoped identity of one declared resource:
// materialized baseline, runtime-reported baseline, and persisted configured
// result.
type ResourceOperation struct {
	LogicalID                 string
	MaterializedCoordinate    string
	MaterializedDigest        Digest
	MaterializationStatus     Status
	RuntimeBaselineDigest     Digest
	RuntimeBaselineStatus     Status
	PersistedResultCoordinate string
	PersistedResultDigest     Digest
	ResultStatus              Status
}

// OperationIntegrity carries attempt-scoped resource identity.
type OperationIntegrity struct {
	Resources []ResourceOperation
}

// Copy returns a defensive, deterministically ordered copy.
func (o *OperationIntegrity) Copy() *OperationIntegrity {
	if o == nil {
		return nil
	}
	return &OperationIntegrity{Resources: normalizeOperations(o.Resources)}
}

// ResourceIntegrity is the joined, normalized per-resource identity chain. The
// four content identities are separate fields and never share storage.
type ResourceIntegrity struct {
	LogicalID        string
	SourceCoordinate string

	MaterializedCoordinate    string
	PersistedResultCoordinate string

	SourceDigest               Digest
	LockState                  LockState
	MaterializedBaselineDigest Digest
	MaterializationStatus      Status
	RuntimeBaselineDigest      Digest
	RuntimeBaselineStatus      Status
	PersistedResultDigest      Digest
	ResultStatus               Status
}

// RunIntegrity is the joined normalized integrity provenance for one run.
type RunIntegrity struct {
	Lock      LockState
	Resources []ResourceIntegrity
}

// MergeRunIntegrity joins the project-scoped source/lock half with the
// attempt-scoped operation half by declared logical resource identity.
//
// Declared resources without attempt evidence keep explicit not-performed
// statuses; attempt evidence for an undeclared resource is a provenance
// conflict, because Engine must not accept resource identities it never
// declared.
func MergeRunIntegrity(project *ProjectIntegrity, operation *OperationIntegrity) (*RunIntegrity, error) {
	if project == nil && operation == nil {
		return nil, nil
	}

	lockState := LockStateNotPerformed
	var sources []ResourceSource
	if project != nil {
		if project.Lock != "" {
			lockState = project.Lock
		}
		sources = normalizeSources(project.Resources)
	}

	operations := make(map[string]ResourceOperation)
	if operation != nil {
		for _, item := range normalizeOperations(operation.Resources) {
			if _, exists := operations[item.LogicalID]; exists {
				return nil, &Failure{Code: CodeProvenanceConflict, LogicalID: item.LogicalID,
					Message: fmt.Sprintf("duplicate operation evidence for resource %q", item.LogicalID)}
			}
			operations[item.LogicalID] = item
		}
	}

	seen := make(map[string]struct{}, len(sources))
	resources := make([]ResourceIntegrity, 0, len(sources)+len(operations))
	for _, source := range sources {
		if _, exists := seen[source.LogicalID]; exists {
			return nil, &Failure{Code: CodeProvenanceConflict, LogicalID: source.LogicalID,
				Message: fmt.Sprintf("duplicate source identity for resource %q", source.LogicalID)}
		}
		seen[source.LogicalID] = struct{}{}

		record := ResourceIntegrity{
			LogicalID:             source.LogicalID,
			SourceCoordinate:      source.SourceCoordinate,
			SourceDigest:          source.SourceDigest,
			LockState:             source.LockState,
			MaterializationStatus: StatusNotPerformed,
			RuntimeBaselineStatus: StatusNotPerformed,
			ResultStatus:          StatusNotPerformed,
		}
		if record.LockState == "" {
			record.LockState = lockState
		}
		if item, ok := operations[source.LogicalID]; ok {
			record.MaterializedCoordinate = item.MaterializedCoordinate
			record.MaterializedBaselineDigest = item.MaterializedDigest
			record.MaterializationStatus = item.MaterializationStatus
			record.RuntimeBaselineDigest = item.RuntimeBaselineDigest
			record.RuntimeBaselineStatus = item.RuntimeBaselineStatus
			record.PersistedResultCoordinate = item.PersistedResultCoordinate
			record.PersistedResultDigest = item.PersistedResultDigest
			record.ResultStatus = item.ResultStatus
			delete(operations, source.LogicalID)
		}
		resources = append(resources, record)
	}

	if len(operations) > 0 {
		undeclared := make([]string, 0, len(operations))
		for logicalID := range operations {
			undeclared = append(undeclared, logicalID)
		}
		sort.Strings(undeclared)
		return nil, &Failure{Code: CodeProvenanceConflict, LogicalID: undeclared[0],
			Message: fmt.Sprintf("operation evidence names undeclared resource %q", undeclared[0])}
	}

	return &RunIntegrity{Lock: lockState, Resources: resources}, nil
}

// NormalizeRunIntegrity returns a deterministic, copy-safe ordering.
func NormalizeRunIntegrity(in *RunIntegrity) *RunIntegrity {
	if in == nil {
		return nil
	}
	resources := make([]ResourceIntegrity, len(in.Resources))
	copy(resources, in.Resources)
	for i := range resources {
		resources[i] = normalizeResourceIntegrity(resources[i])
	}
	sort.SliceStable(resources, func(i, j int) bool {
		return resources[i].LogicalID < resources[j].LogicalID
	})
	return &RunIntegrity{Lock: in.Lock, Resources: resources}
}

func normalizeSources(in []ResourceSource) []ResourceSource {
	out := make([]ResourceSource, 0, len(in))
	for _, item := range in {
		out = append(out, ResourceSource{
			LogicalID:        strings.TrimSpace(item.LogicalID),
			SourceCoordinate: strings.TrimSpace(item.SourceCoordinate),
			SourceDigest:     Digest(strings.TrimSpace(string(item.SourceDigest))),
			LockState:        item.LockState,
			LockedDigest:     Digest(strings.TrimSpace(string(item.LockedDigest))),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LogicalID < out[j].LogicalID })
	return out
}

func normalizeOperations(in []ResourceOperation) []ResourceOperation {
	out := make([]ResourceOperation, 0, len(in))
	for _, item := range in {
		out = append(out, ResourceOperation{
			LogicalID:                 strings.TrimSpace(item.LogicalID),
			MaterializedCoordinate:    strings.TrimSpace(item.MaterializedCoordinate),
			MaterializedDigest:        Digest(strings.TrimSpace(string(item.MaterializedDigest))),
			MaterializationStatus:     item.MaterializationStatus,
			RuntimeBaselineDigest:     Digest(strings.TrimSpace(string(item.RuntimeBaselineDigest))),
			RuntimeBaselineStatus:     item.RuntimeBaselineStatus,
			PersistedResultCoordinate: strings.TrimSpace(item.PersistedResultCoordinate),
			PersistedResultDigest:     Digest(strings.TrimSpace(string(item.PersistedResultDigest))),
			ResultStatus:              item.ResultStatus,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LogicalID < out[j].LogicalID })
	return out
}

func normalizeResourceIntegrity(in ResourceIntegrity) ResourceIntegrity {
	return ResourceIntegrity{
		LogicalID:                  strings.TrimSpace(in.LogicalID),
		SourceCoordinate:           strings.TrimSpace(in.SourceCoordinate),
		MaterializedCoordinate:     strings.TrimSpace(in.MaterializedCoordinate),
		PersistedResultCoordinate:  strings.TrimSpace(in.PersistedResultCoordinate),
		SourceDigest:               Digest(strings.TrimSpace(string(in.SourceDigest))),
		LockState:                  in.LockState,
		MaterializedBaselineDigest: Digest(strings.TrimSpace(string(in.MaterializedBaselineDigest))),
		MaterializationStatus:      in.MaterializationStatus,
		RuntimeBaselineDigest:      Digest(strings.TrimSpace(string(in.RuntimeBaselineDigest))),
		RuntimeBaselineStatus:      in.RuntimeBaselineStatus,
		PersistedResultDigest:      Digest(strings.TrimSpace(string(in.PersistedResultDigest))),
		ResultStatus:               in.ResultStatus,
	}
}
