package recordemit

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/integrity"
	"parametron/internal/engine/metadata"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
	"parametron/internal/engine/resourcebinding"
	"parametron/internal/engine/verification"
)

const packageKeyPrefix = "engine-run:"

// integrityEvidenceKind is the normalized provenance evidence kind for preserved
// raw runtime integrity evidence.
const integrityEvidenceKind = "integrity-evidence"

// RunEmitInput carries material for emitting a local record package from a normal Engine run.
type RunEmitInput struct {
	RunRoot   string
	PlanHash  string
	Report    report.Report
	Metadata  *metadata.Metadata
	Artifacts []artifact.Artifact
	// ReferenceTraversal and CADRuntime are retained for single-outcome callers.
	// When Outcomes is non-empty, ReferenceTraversal is ignored. CADRuntime is
	// also ignored for successful observation emission, but a selected runtime
	// failure on CADRuntime remains the failure-evidence input.
	ReferenceTraversal *ReferenceTraversalRunEvidence
	CADRuntime         *CADRuntimeRunEvidence
	// Outcomes carries every eligible CAD outcome's engineering-observation
	// material. Multiple outcomes emit outcome-scoped records and raw evidence.
	Outcomes []RunOutcomeEvidence
	// SourceIntegrity is the project-scoped source identity and lock state.
	SourceIntegrity *integrity.ProjectIntegrity
}

// RunOutcomeEvidence groups the engineering-observation material produced by one
// eligible CAD outcome. Coordinates must agree with any embedded evidence.
type RunOutcomeEvidence struct {
	JobID      string
	ProductKey string
	StepRef    string
	Attempt    int
	AttemptID  string

	CADRuntime         *CADRuntimeRunEvidence
	ReferenceTraversal *ReferenceTraversalRunEvidence
}

// CADRuntimeRunEvidence carries unchanged bytes read from one execution attempt.
type CADRuntimeRunEvidence struct {
	Occurrence         *OccurrenceRunEvidence
	Fact               *FactRunEvidence
	Result             []byte
	Verification       []byte
	Observed           []byte
	IntegrityEvidence  []byte
	ObservedValue      *observed.Observed
	VerificationResult *verification.Result
	// Integrity is the attempt-scoped identity chain for the declared resource
	// inventory of this attempt.
	Integrity  *integrity.OperationIntegrity
	Failure    *executor.CADRuntimeFailureOutcome
	JobID      string
	ProductKey string
	StepRef    string
	Attempt    int
	AttemptID  string
}

type ReferenceTraversalRunEvidence struct {
	Resources              resourcebinding.Inventory
	ExpectedSourceDocument string
	Content                []byte
	JobID                  string
	ProductKey             string
	StepRef                string
	Attempt                int
	AttemptID              string
}

type rawEvidenceSource struct {
	contractPath string
	runPath      string
}

// EmitRunPackage builds and writes a local PDM-ready record package under the run root.
func EmitRunPackage(input RunEmitInput) error {
	runRoot := strings.TrimSpace(input.RunRoot)
	if runRoot == "" {
		return fmt.Errorf("recordemit: run root is required")
	}

	planHash := strings.TrimSpace(input.PlanHash)
	if planHash == "" {
		return fmt.Errorf("recordemit: plan hash is required")
	}

	outcomes, scoped, err := resolveEmitOutcomes(input)
	if err != nil {
		return err
	}

	baseProvenance := recordcontract.Provenance{}
	if input.Metadata != nil {
		mapped, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{
			Metadata:   *input.Metadata,
			Provenance: baseProvenance,
		})
		if err != nil {
			return fmt.Errorf("recordemit: map metadata: %w", err)
		}
		baseProvenance = mapped.Provenance
	}

	runIntegrity, err := resolvedRunIntegrity(input, outcomes, scoped)
	if err != nil {
		return fmt.Errorf("recordemit: merge integrity provenance: %w", err)
	}
	mappedRunIntegrity, err := recordmap.MapIntegrityProvenance(runIntegrity)
	if err != nil {
		return fmt.Errorf("recordemit: map integrity provenance: %w", err)
	}
	runProvenance := baseProvenance
	runProvenance.Integrity = mappedRunIntegrity
	if !scoped {
		if evidence := legacyIntegrityEvidence(input, outcomes); len(evidence) > 0 {
			runProvenance.Evidence = append(runProvenance.Evidence, recordcontract.EvidenceReference{
				Kind:         integrityEvidenceKind,
				Ref:          recordpackage.RawRuntimeIntegrityEvidenceContractPath(),
				DigestSHA256: evidenceDigest(evidence),
			})
		}
	}
	runProvenance = recordcontract.NormalizeProvenance(runProvenance)

	recordKey := recordKeyForPlanHash(planHash)
	mappedReport, err := recordmap.MapReport(recordmap.ReportMappingInput{
		Report:     deterministicReportForRecords(input.Report),
		RecordKey:  recordKey,
		Provenance: runProvenance,
	})
	if err != nil {
		return fmt.Errorf("recordemit: map report: %w", err)
	}

	records := []recordpackage.Record{
		recordpackage.ExecutionRecord(mappedReport.ExecutionRecord),
	}
	failureRecord := mappedReport.FailureRecord
	mappedArtifacts, err := recordmap.MapArtifactStoreRecords(recordmap.ArtifactMappingInput{
		Artifacts:       input.Artifacts,
		RecordKeyPrefix: recordKey,
		Provenance:      runProvenance,
	})
	if err != nil {
		return fmt.Errorf("recordemit: map artifacts: %w", err)
	}
	for _, artifactRecord := range mappedArtifacts.ArtifactRecords {
		records = append(records, recordpackage.ArtifactRecord(artifactRecord))
	}

	var outcomeRaw []recordpackage.RawEvidenceFile
	for _, outcome := range outcomes {
		refs, err := evidenceRefsForOutcome(outcome, scoped)
		if err != nil {
			return err
		}
		outcomeProvenance := runProvenance
		if scoped {
			outcomeProvenance, err = buildOutcomeProvenance(input, outcome, baseProvenance, runIntegrity, refs)
			if err != nil {
				return err
			}
		}
		outcomeRecords, err := mapOutcomeRecords(outcome, scoped, recordKey, outcomeProvenance, refs)
		if err != nil {
			return err
		}
		records = append(records, outcomeRecords...)

		if outcome.cad != nil {
			outcomeRaw = append(outcomeRaw, rawEvidenceForOutcomeCAD(outcome.cad, refs)...)
		}
		if outcome.traversal != nil {
			scope := ""
			if scoped {
				scope = outcome.key
			}
			mappedTraversal, raw, err := mapReferenceTraversal(outcome.traversal, outcomeRecordKey(recordKey, scope, ":reference"), refs.traversal, scoped, outcomeProvenance)
			if err != nil {
				return err
			}
			outcomeRaw = append(outcomeRaw, raw)
			if mappedTraversal.ReferenceRecord != nil {
				rec := recordpackage.ReferenceRecord(*mappedTraversal.ReferenceRecord)
				rec.OutcomeKey = scope
				records = append(records, rec)
			}
		}
	}

	if failureRecord, err = mapRuntimeFailureRecord(input, outcomes, planHash, baseProvenance, runProvenance, failureRecord); err != nil {
		return err
	}
	if failureRecord != nil {
		records = append(records, recordpackage.FailureRecord(*failureRecord))
	}

	rawEvidence, err := collectRawEvidence(runRoot, input)
	if err != nil {
		return err
	}
	if !hasRawEvidence(rawEvidence, recordpackage.RawReportContractPath()) {
		return fmt.Errorf("recordemit: raw report evidence is required")
	}
	if legacy := input.CADRuntime; legacy != nil && (len(input.Outcomes) > 0 || legacy.Failure != nil) {
		// A selected runtime failure or integrity failure keeps its established
		// raw paths; it is failure evidence, not a successful
		// engineering-observation outcome.
		rawEvidence = append(rawEvidence, rawEvidenceForOutcomeCAD(legacy, canonicalOutcomeEvidenceRefs())...)
	}
	rawEvidence = append(rawEvidence, outcomeRaw...)

	return recordpackage.WritePackage(recordpackage.PackageInput{
		PackageRoot:               runRoot,
		UseCanonicalDirectoryName: true,
		PackageKey:                packageKeyForPlanHash(planHash),
		Records:                   records,
		RawEvidenceFiles:          rawEvidence,
		OverwriteExisting:         true,
	})
}

type emitOutcome struct {
	key       string
	coords    outcomeCoordinates
	cad       *CADRuntimeRunEvidence
	traversal *ReferenceTraversalRunEvidence
}

type outcomeCoordinates struct {
	JobID      string
	ProductKey string
	StepRef    string
	Attempt    int
	AttemptID  string
}

type outcomeEvidenceRefs struct {
	observed     string
	verification string
	occurrence   string
	fact         string
	traversal    string
	integrity    string
	result       string
}

func canonicalOutcomeEvidenceRefs() outcomeEvidenceRefs {
	return outcomeEvidenceRefs{
		observed:     recordpackage.RawObservedContractPath(),
		verification: recordpackage.RawVerificationContractPath(),
		occurrence:   recordpackage.RawRuntimeOccurrenceEvidenceContractPath(),
		fact:         recordpackage.RawFactEvidenceContractPath(),
		traversal:    recordpackage.RawRuntimeReferenceTraversalContractPath(),
		integrity:    recordpackage.RawRuntimeIntegrityEvidenceContractPath(),
		result:       recordpackage.RawRuntimeResultContractPath(),
	}
}

func rawEvidenceForOutcomeCAD(evidence *CADRuntimeRunEvidence, refs outcomeEvidenceRefs) []recordpackage.RawEvidenceFile {
	if evidence == nil {
		return nil
	}
	out := make([]recordpackage.RawEvidenceFile, 0, 6)
	if evidence.Occurrence != nil && len(evidence.Occurrence.Input.Content) > 0 {
		out = append(out, recordpackage.RawEvidenceFile{ContractPath: refs.occurrence, Content: append([]byte(nil), evidence.Occurrence.Input.Content...)})
	}
	if evidence.Fact != nil && len(evidence.Fact.Input.Content) > 0 {
		out = append(out, recordpackage.RawEvidenceFile{ContractPath: refs.fact, Content: append([]byte(nil), evidence.Fact.Input.Content...)})
	}
	for _, item := range []recordpackage.RawEvidenceFile{
		{ContractPath: refs.integrity, Content: evidence.IntegrityEvidence},
		{ContractPath: refs.result, Content: evidence.Result},
		{ContractPath: refs.observed, Content: evidence.Observed},
		{ContractPath: refs.verification, Content: evidence.Verification},
	} {
		if len(item.Content) > 0 {
			out = append(out, recordpackage.RawEvidenceFile{ContractPath: item.ContractPath, Content: append([]byte(nil), item.Content...)})
		}
	}
	return out
}

func cadString(evidence *CADRuntimeRunEvidence, value func(*CADRuntimeRunEvidence) string) string {
	if evidence == nil {
		return ""
	}
	return value(evidence)
}

func cadInt(evidence *CADRuntimeRunEvidence, value func(*CADRuntimeRunEvidence) int) int {
	if evidence == nil {
		return 0
	}
	return value(evidence)
}

func traversalString(evidence *ReferenceTraversalRunEvidence, value func(*ReferenceTraversalRunEvidence) string) string {
	if evidence == nil {
		return ""
	}
	return value(evidence)
}

func traversalInt(evidence *ReferenceTraversalRunEvidence, value func(*ReferenceTraversalRunEvidence) int) int {
	if evidence == nil {
		return 0
	}
	return value(evidence)
}

func canonicalExecutionLink(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}

func resolveEmitOutcomes(input RunEmitInput) ([]emitOutcome, bool, error) {
	if len(input.Outcomes) == 0 {
		var out []emitOutcome
		cad := input.CADRuntime
		if cad != nil && cad.Failure != nil {
			cad = nil
		}
		if cad != nil || input.ReferenceTraversal != nil {
			out = append(out, emitOutcome{cad: cad, traversal: input.ReferenceTraversal})
		}
		return out, false, nil
	}

	outcomes := make([]emitOutcome, 0, len(input.Outcomes))
	coordinates := make([]outcomeCoordinates, 0, len(input.Outcomes))
	seen := make(map[string]struct{}, len(input.Outcomes))
	for i, raw := range input.Outcomes {
		if raw.CADRuntime != nil && raw.CADRuntime.Failure != nil {
			return nil, false, fmt.Errorf("recordemit: outcomes[%d] carries a runtime failure and is not eligible for engineering-observation emission", i)
		}
		coords, err := resolveOutcomeCoordinates(raw)
		if err != nil {
			return nil, false, fmt.Errorf("recordemit: outcomes[%d]: %w", i, err)
		}
		key, err := outcomeKeyFor(coords)
		if err != nil {
			return nil, false, fmt.Errorf("recordemit: outcomes[%d]: %w", i, err)
		}
		if _, exists := seen[key]; exists {
			return nil, false, fmt.Errorf("recordemit: outcomes[%d]: duplicate outcome identity %q", i, key)
		}
		seen[key] = struct{}{}
		outcomes = append(outcomes, emitOutcome{key: key, coords: coords, cad: raw.CADRuntime, traversal: raw.ReferenceTraversal})
		coordinates = append(coordinates, coords)
	}
	sort.SliceStable(outcomes, func(i, j int) bool { return outcomes[i].key < outcomes[j].key })
	if len(outcomes) > 1 {
		for i, coords := range coordinates {
			if !canonicalExecutionLink(coords.JobID) || !canonicalExecutionLink(coords.ProductKey) || !canonicalExecutionLink(coords.StepRef) {
				return nil, false, fmt.Errorf("recordemit: outcomes[%d]: job, product, and step identity are required for multi-outcome emission", i)
			}
		}
	}
	return outcomes, len(outcomes) > 1, nil
}

func resolveOutcomeCoordinates(outcome RunOutcomeEvidence) (outcomeCoordinates, error) {
	coords := outcomeCoordinates{JobID: outcome.JobID, ProductKey: outcome.ProductKey, StepRef: outcome.StepRef, Attempt: outcome.Attempt, AttemptID: outcome.AttemptID}
	for _, embedded := range []outcomeCoordinates{
		{JobID: cadString(outcome.CADRuntime, func(e *CADRuntimeRunEvidence) string { return e.JobID }), ProductKey: cadString(outcome.CADRuntime, func(e *CADRuntimeRunEvidence) string { return e.ProductKey }), StepRef: cadString(outcome.CADRuntime, func(e *CADRuntimeRunEvidence) string { return e.StepRef }), Attempt: cadInt(outcome.CADRuntime, func(e *CADRuntimeRunEvidence) int { return e.Attempt }), AttemptID: cadString(outcome.CADRuntime, func(e *CADRuntimeRunEvidence) string { return e.AttemptID })},
		{JobID: traversalString(outcome.ReferenceTraversal, func(e *ReferenceTraversalRunEvidence) string { return e.JobID }), ProductKey: traversalString(outcome.ReferenceTraversal, func(e *ReferenceTraversalRunEvidence) string { return e.ProductKey }), StepRef: traversalString(outcome.ReferenceTraversal, func(e *ReferenceTraversalRunEvidence) string { return e.StepRef }), Attempt: traversalInt(outcome.ReferenceTraversal, func(e *ReferenceTraversalRunEvidence) int { return e.Attempt }), AttemptID: traversalString(outcome.ReferenceTraversal, func(e *ReferenceTraversalRunEvidence) string { return e.AttemptID })},
	} {
		if err := mergeOutcomeCoordinates(&coords, embedded); err != nil {
			return outcomeCoordinates{}, err
		}
	}
	coords.JobID = strings.TrimSpace(coords.JobID)
	coords.ProductKey = strings.TrimSpace(coords.ProductKey)
	coords.StepRef = strings.TrimSpace(coords.StepRef)
	coords.AttemptID = strings.TrimSpace(coords.AttemptID)
	return coords, nil
}

func mergeOutcomeCoordinates(target *outcomeCoordinates, other outcomeCoordinates) error {
	for _, pair := range []struct {
		name   string
		target *string
		value  string
	}{{"job", &target.JobID, other.JobID}, {"product", &target.ProductKey, other.ProductKey}, {"step", &target.StepRef, other.StepRef}, {"attemptId", &target.AttemptID, other.AttemptID}} {
		value := strings.TrimSpace(pair.value)
		if value == "" {
			continue
		}
		if *pair.target != "" && strings.TrimSpace(*pair.target) != value {
			return fmt.Errorf("conflicting %s outcome identity", pair.name)
		}
		*pair.target = value
	}
	if other.Attempt != 0 {
		if target.Attempt != 0 && target.Attempt != other.Attempt {
			return fmt.Errorf("conflicting attempt outcome identity")
		}
		target.Attempt = other.Attempt
	}
	return nil
}

func outcomeKeyFor(coords outcomeCoordinates) (string, error) {
	if coords.JobID == "" && coords.ProductKey == "" && coords.StepRef == "" && coords.AttemptID == "" && coords.Attempt == 0 {
		return "", fmt.Errorf("execution outcome identity is required")
	}
	data, err := json.Marshal(coords)
	if err != nil {
		return "", fmt.Errorf("serialize outcome identity: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func outcomeRecordKey(base, outcomeKey, suffix string) string {
	if outcomeKey == "" {
		return base + suffix
	}
	return base + ":" + outcomeKey + suffix
}

func evidenceRefsForOutcome(outcome emitOutcome, scoped bool) (outcomeEvidenceRefs, error) {
	refs := canonicalOutcomeEvidenceRefs()
	if !scoped {
		return refs, nil
	}
	for _, item := range []struct {
		target    *string
		canonical string
	}{{&refs.observed, refs.observed}, {&refs.verification, refs.verification}, {&refs.occurrence, refs.occurrence}, {&refs.fact, refs.fact}, {&refs.traversal, refs.traversal}, {&refs.integrity, refs.integrity}, {&refs.result, refs.result}} {
		path, err := recordpackage.OutcomeRawEvidenceContractPath(outcome.key, item.canonical)
		if err != nil {
			return outcomeEvidenceRefs{}, fmt.Errorf("recordemit: outcome evidence path: %w", err)
		}
		*item.target = path
	}
	return refs, nil
}

func mapOutcomeRecords(outcome emitOutcome, scoped bool, baseRecordKey string, provenance recordcontract.Provenance, refs outcomeEvidenceRefs) ([]recordpackage.Record, error) {
	evidence := outcome.cad
	if evidence == nil {
		return nil, nil
	}
	scope := ""
	if scoped {
		scope = outcome.key
	}
	observationKey := outcomeRecordKey(baseRecordKey, scope, ":observation")
	linkage := recordmap.ObservedMappingLinkage{JobID: evidence.JobID, ProductKey: evidence.ProductKey, StepRef: evidence.StepRef}
	records := make([]recordpackage.Record, 0, 2)
	if evidence.ObservedValue != nil {
		mapped, err := recordmap.MapObservedToObservationRecord(recordmap.ObservedMappingInput{
			Observed:             *evidence.ObservedValue,
			RecordKey:            observationKey,
			Provenance:           provenance,
			Linkage:              linkage,
			EvidenceDigestSHA256: evidenceDigest(evidence.Observed),
			EvidenceRef:          refs.observed,
		})
		if err != nil {
			return nil, fmt.Errorf("recordemit: map observed evidence: %w", err)
		}
		rec := recordpackage.ObservationRecord(mapped)
		rec.OutcomeKey = scope
		records = append(records, rec)
	}
	if evidence.VerificationResult != nil {
		mapped, err := recordmap.MapVerificationToVerificationRecord(recordmap.VerificationMappingInput{
			Result:                       *evidence.VerificationResult,
			RecordKey:                    outcomeRecordKey(baseRecordKey, scope, ":verification"),
			Provenance:                   provenance,
			Linkage:                      recordmap.VerificationMappingLinkage{JobID: evidence.JobID, ProductKey: evidence.ProductKey, StepRef: evidence.StepRef},
			EvidenceDigestSHA256:         evidenceDigest(evidence.Verification),
			ObservedEvidenceDigestSHA256: evidenceDigest(evidence.Observed),
			EvidenceRef:                  refs.verification,
			ObservedEvidenceRef:          refs.observed,
		})
		if err != nil {
			return nil, fmt.Errorf("recordemit: map verification result: %w", err)
		}
		rec := recordpackage.VerificationRecord(mapped)
		rec.OutcomeKey = scope
		records = append(records, rec)
	}
	var err error
	records, err = appendFactRecords(records, evidence, observationKey, provenance, refs.fact, scope)
	if err != nil {
		return nil, err
	}
	records, err = appendOccurrenceRecords(records, evidence, observationKey, provenance, refs.occurrence, scope)
	if err != nil {
		return nil, err
	}
	return records, nil
}

func buildOutcomeProvenance(input RunEmitInput, outcome emitOutcome, base recordcontract.Provenance, runIntegrity *integrity.RunIntegrity, refs outcomeEvidenceRefs) (recordcontract.Provenance, error) {
	provenance := base
	linkage := recordcontract.LinkageProvenance{JobID: outcome.coords.JobID, ProductKey: outcome.coords.ProductKey, StepRef: outcome.coords.StepRef}
	if provenance.Linkage != (recordcontract.LinkageProvenance{}) && provenance.Linkage != linkage {
		return recordcontract.Provenance{}, fmt.Errorf("recordemit: run provenance linkage conflicts with outcome %q", outcome.key)
	}
	provenance.Linkage = linkage
	if outcome.cad != nil && outcome.cad.Integrity != nil {
		project := input.SourceIntegrity
		if project == nil {
			project = projectIntegrityFromRun(runIntegrity)
		}
		merged, err := integrity.MergeRunIntegrity(project, outcome.cad.Integrity)
		if err != nil {
			return recordcontract.Provenance{}, fmt.Errorf("recordemit: outcome integrity provenance: %w", err)
		}
		mapped, err := recordmap.MapIntegrityProvenance(merged)
		if err != nil {
			return recordcontract.Provenance{}, fmt.Errorf("recordemit: map outcome integrity provenance: %w", err)
		}
		provenance.Integrity = mapped
	} else {
		mapped, err := recordmap.MapIntegrityProvenance(runIntegrity)
		if err != nil {
			return recordcontract.Provenance{}, fmt.Errorf("recordemit: map outcome integrity provenance: %w", err)
		}
		provenance.Integrity = mapped
	}
	if outcome.cad != nil && len(outcome.cad.IntegrityEvidence) > 0 {
		provenance.Evidence = append(provenance.Evidence, recordcontract.EvidenceReference{
			Kind:         integrityEvidenceKind,
			Ref:          refs.integrity,
			DigestSHA256: evidenceDigest(outcome.cad.IntegrityEvidence),
		})
	}
	return recordcontract.NormalizeProvenance(provenance), nil
}

func projectIntegrityFromRun(run *integrity.RunIntegrity) *integrity.ProjectIntegrity {
	if run == nil {
		return nil
	}
	project := &integrity.ProjectIntegrity{Lock: run.Lock}
	for _, resource := range run.Resources {
		project.Resources = append(project.Resources, integrity.ResourceSource{
			LogicalID: resource.LogicalID, SourceCoordinate: resource.SourceCoordinate, SourceDigest: resource.SourceDigest, LockState: resource.LockState,
		})
	}
	return project
}

func mapRuntimeFailureRecord(input RunEmitInput, outcomes []emitOutcome, planHash string, base, runProvenance recordcontract.Provenance, reportFailure *recordcontract.FailureRecord) (*recordcontract.FailureRecord, error) {
	evidence := input.CADRuntime
	if evidence == nil || evidence.Failure == nil {
		return reportFailure, nil
	}
	provenance := runProvenance
	if len(input.Outcomes) > 0 {
		// Successful outcome scoping must not absorb the selected failure's
		// configured-result provenance; the failure record keeps its own chain.
		merged, err := resolvedLegacyIntegrity(input, outcomes)
		if err != nil {
			return nil, fmt.Errorf("recordemit: failure integrity provenance: %w", err)
		}
		mapped, err := recordmap.MapIntegrityProvenance(merged)
		if err != nil {
			return nil, fmt.Errorf("recordemit: map failure integrity provenance: %w", err)
		}
		provenance = base
		provenance.Integrity = mapped
		if len(evidence.IntegrityEvidence) > 0 {
			provenance.Evidence = append(provenance.Evidence, recordcontract.EvidenceReference{
				Kind: integrityEvidenceKind, Ref: recordpackage.RawRuntimeIntegrityEvidenceContractPath(), DigestSHA256: evidenceDigest(evidence.IntegrityEvidence),
			})
		}
		provenance = recordcontract.NormalizeProvenance(provenance)
	}
	if strings.TrimSpace(provenance.Plan.PlanHash) == "" {
		provenance.Plan.PlanHash = planHash
	}
	mappingInput := recordmap.CADRuntimeFailureMappingInput{
		Failure:              *evidence.Failure,
		RecordKey:            recordKeyForPlanHash(planHash),
		Provenance:           provenance,
		Linkage:              recordcontract.FailureLinkage{JobID: evidence.JobID, ProductKey: evidence.ProductKey, StepRef: evidence.StepRef},
		EvidenceDigestSHA256: evidenceDigest(evidence.Result),
	}
	if reportFailure != nil {
		mappingInput.RetryCount = reportFailure.Failure.RetryCount
		mappingInput.OccurredAt = reportFailure.Failure.OccurredAt
	}
	mapped, err := recordmap.MapCADRuntimeFailure(mappingInput)
	if err != nil {
		return nil, fmt.Errorf("recordemit: map CAD runtime failure: %w", err)
	}
	return &mapped, nil
}

// resolvedRunIntegrity restores the joined integrity chain at the run level.
//
// Multi-outcome packages carry run-level source/lock identity only; each
// successful outcome's configured-result identity belongs to that outcome's
// records. Single-outcome packages retain the established joined chain.
func resolvedRunIntegrity(input RunEmitInput, outcomes []emitOutcome, scoped bool) (*integrity.RunIntegrity, error) {
	if scoped {
		if input.Metadata != nil && input.Metadata.Integrity != nil {
			// Run metadata can carry one selected attempt's chain. The run-level
			// block keeps only the source/lock half; outcome result identity is
			// emitted per outcome.
			return integrity.MergeRunIntegrity(projectIntegrityFromRun(metadata.RunIntegrityFromMetadata(input.Metadata.Integrity)), nil)
		}
		if input.SourceIntegrity == nil {
			return nil, nil
		}
		return integrity.MergeRunIntegrity(input.SourceIntegrity, nil)
	}
	return resolvedLegacyIntegrity(input, outcomes)
}

// resolvedLegacyIntegrity restores the joined chain for a single selected
// outcome. Run metadata remains authoritative for re-emission because it
// preserves the executed attempt's chain.
func resolvedLegacyIntegrity(input RunEmitInput, outcomes []emitOutcome) (*integrity.RunIntegrity, error) {
	operation := operationIntegrity(input, outcomes)
	if input.SourceIntegrity != nil {
		return integrity.MergeRunIntegrity(input.SourceIntegrity, operation)
	}
	if input.Metadata != nil && input.Metadata.Integrity != nil {
		run := metadata.RunIntegrityFromMetadata(input.Metadata.Integrity)
		if operation == nil {
			return run, nil
		}
		return integrity.MergeRunIntegrity(projectIntegrityFromRun(run), operation)
	}
	return nil, nil
}

// operationIntegrity returns the single selected attempt-scoped integrity chain.
func operationIntegrity(input RunEmitInput, outcomes []emitOutcome) *integrity.OperationIntegrity {
	if input.CADRuntime != nil && input.CADRuntime.Failure != nil {
		return input.CADRuntime.Integrity
	}
	if len(outcomes) == 1 && outcomes[0].cad != nil {
		return outcomes[0].cad.Integrity
	}
	if input.CADRuntime != nil {
		return input.CADRuntime.Integrity
	}
	return nil
}

func legacyIntegrityEvidence(input RunEmitInput, outcomes []emitOutcome) []byte {
	if input.CADRuntime != nil && len(input.CADRuntime.IntegrityEvidence) > 0 {
		return input.CADRuntime.IntegrityEvidence
	}
	if len(outcomes) == 1 && outcomes[0].cad != nil {
		return outcomes[0].cad.IntegrityEvidence
	}
	return nil
}

func evidenceDigest(content []byte) string {
	if len(content) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

func mapReferenceTraversal(evidence *ReferenceTraversalRunEvidence, recordKey, evidenceRef string, scoped bool, provenance recordcontract.Provenance) (recordmap.ReferenceTraversalMappingOutput, recordpackage.RawEvidenceFile, error) {
	content := append([]byte(nil), evidence.Content...)
	var traversal recordmap.ReferenceTraversalRuntimeEvidence
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&traversal); err != nil {
		return recordmap.ReferenceTraversalMappingOutput{}, recordpackage.RawEvidenceFile{}, fmt.Errorf("recordemit: decode reference traversal: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("trailing JSON value")
		}
		return recordmap.ReferenceTraversalMappingOutput{}, recordpackage.RawEvidenceFile{}, fmt.Errorf("recordemit: decode reference traversal: %w", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	mapped, err := recordmap.MapReferenceTraversal(recordmap.ReferenceTraversalMappingInput{
		Resources:              evidence.Resources,
		ExpectedSourceDocument: evidence.ExpectedSourceDocument,
		Traversal:              traversal,
		RecordKey:              recordKey,
		Provenance:             provenance,
		Linkage:                recordmap.ReferenceTraversalMappingLinkage{JobID: evidence.JobID, ProductKey: evidence.ProductKey, StepRef: evidence.StepRef},
		EvidenceDigestSHA256:   digest,
		EvidenceRef:            evidenceRef,
	})
	if err != nil {
		return recordmap.ReferenceTraversalMappingOutput{}, recordpackage.RawEvidenceFile{}, fmt.Errorf("recordemit: map reference traversal: %w", err)
	}
	rawPath := evidenceRef
	if !scoped {
		rawPath = recordpackage.RawRuntimeReferenceTraversalContractPath()
	}
	return mapped, recordpackage.RawEvidenceFile{ContractPath: rawPath, Content: content}, nil
}

func deterministicReportForRecords(rep report.Report) report.Report {
	out := rep
	out.Runtime.StartedAt = time.Time{}
	out.Runtime.EndedAt = time.Time{}
	out.Runtime.DurationMs = 0
	out.Jobs = append([]report.JobReport(nil), rep.Jobs...)
	for jobIndex := range out.Jobs {
		out.Jobs[jobIndex].Steps = append([]report.StepReport(nil), out.Jobs[jobIndex].Steps...)
		for stepIndex := range out.Jobs[jobIndex].Steps {
			out.Jobs[jobIndex].Steps[stepIndex].StartedAt = nil
			out.Jobs[jobIndex].Steps[stepIndex].EndedAt = nil
			out.Jobs[jobIndex].Steps[stepIndex].DurationMs = nil
		}
	}
	return out
}

func packageKeyForPlanHash(planHash string) string {
	return packageKeyPrefix + strings.TrimSpace(planHash)
}

func recordKeyForPlanHash(planHash string) string {
	return packageKeyForPlanHash(planHash)
}

func collectRawEvidence(runRoot string, input RunEmitInput) ([]recordpackage.RawEvidenceFile, error) {
	sources := []rawEvidenceSource{
		{contractPath: recordpackage.RawReportContractPath(), runPath: filepath.Join(runRoot, report.FileName)},
		{contractPath: recordpackage.RawMetadataContractPath(), runPath: filepath.Join(runRoot, metadata.FileName)},
		{contractPath: recordpackage.RawArtifactStoreManifestContractPath(), runPath: filepath.Join(runRoot, artifact.ManifestFileName)},
	}

	out := make([]recordpackage.RawEvidenceFile, 0, len(sources))
	for _, source := range sources {
		content, include, err := rawEvidenceContent(source, input)
		if err != nil {
			return nil, err
		}
		if !include {
			continue
		}
		out = append(out, recordpackage.RawEvidenceFile{
			ContractPath: source.contractPath,
			Content:      content,
		})
	}
	return out, nil
}

func rawEvidenceContent(source rawEvidenceSource, input RunEmitInput) ([]byte, bool, error) {
	content, err := os.ReadFile(source.runPath)
	if err == nil {
		if len(content) == 0 {
			return nil, false, nil
		}
		return content, true, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("recordemit: read %s: %w", source.runPath, err)
	}

	switch source.contractPath {
	case recordpackage.RawReportContractPath():
		return marshalReportFallback(input.Report)
	case recordpackage.RawMetadataContractPath():
		if input.Metadata == nil {
			return nil, false, nil
		}
		return marshalMetadataFallback(*input.Metadata)
	default:
		return nil, false, nil
	}
}

func marshalReportFallback(rep report.Report) ([]byte, bool, error) {
	if strings.TrimSpace(rep.SchemaVersion) == "" {
		return nil, false, nil
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("recordemit: marshal report fallback: %w", err)
	}
	return append(data, '\n'), true, nil
}

func marshalMetadataFallback(md metadata.Metadata) ([]byte, bool, error) {
	if strings.TrimSpace(md.SchemaVersion) == "" {
		return nil, false, nil
	}
	data, err := json.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("recordemit: marshal metadata fallback: %w", err)
	}
	return append(data, '\n'), true, nil
}

func hasRawEvidence(files []recordpackage.RawEvidenceFile, contractPath string) bool {
	for _, file := range files {
		if file.ContractPath == contractPath && len(file.Content) > 0 {
			return true
		}
	}
	return false
}
