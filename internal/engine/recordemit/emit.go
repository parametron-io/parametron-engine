package recordemit

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/metadata"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
	"parametron/internal/engine/verification"
)

const packageKeyPrefix = "engine-run:"

// RunEmitInput carries material for emitting a local record package from a normal Engine run.
type RunEmitInput struct {
	RunRoot            string
	PlanHash           string
	Report             report.Report
	Metadata           *metadata.Metadata
	Artifacts          []artifact.Artifact
	ReferenceTraversal *ReferenceTraversalRunEvidence
	CADRuntime         *CADRuntimeRunEvidence
}

// CADRuntimeRunEvidence carries unchanged bytes read from one execution attempt.
type CADRuntimeRunEvidence struct {
	Result             []byte
	Verification       []byte
	Observed           []byte
	ObservedValue      *observed.Observed
	VerificationResult *verification.Result
	Failure            *executor.CADRuntimeFailureOutcome
	JobID              string
	ProductKey         string
	StepRef            string
}

type ReferenceTraversalRunEvidence struct {
	Content    []byte
	JobID      string
	ProductKey string
	StepRef    string
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

	provenance := recordcontract.Provenance{}
	if input.Metadata != nil {
		mapped, err := recordmap.MapMetadata(recordmap.MetadataMappingInput{
			Metadata:   *input.Metadata,
			Provenance: provenance,
		})
		if err != nil {
			return fmt.Errorf("recordemit: map metadata: %w", err)
		}
		provenance = mapped.Provenance
	}

	recordKey := recordKeyForPlanHash(planHash)
	mappedReport, err := recordmap.MapReport(recordmap.ReportMappingInput{
		Report:     deterministicReportForRecords(input.Report),
		RecordKey:  recordKey,
		Provenance: provenance,
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
		Provenance:      provenance,
	})
	if err != nil {
		return fmt.Errorf("recordemit: map artifacts: %w", err)
	}
	for _, artifactRecord := range mappedArtifacts.ArtifactRecords {
		records = append(records, recordpackage.ArtifactRecord(artifactRecord))
	}
	if input.CADRuntime != nil {
		runtimeRecords, runtimeFailure, err := mapCADRuntimeRecords(input.CADRuntime, recordKey, planHash, provenance, failureRecord)
		if err != nil {
			return err
		}
		records = append(records, runtimeRecords...)
		if runtimeFailure != nil {
			failureRecord = runtimeFailure
		}
	}
	if failureRecord != nil {
		records = append(records, recordpackage.FailureRecord(*failureRecord))
	}
	var traversalRaw *recordpackage.RawEvidenceFile
	if input.ReferenceTraversal != nil {
		mappedTraversal, raw, err := mapReferenceTraversal(input.ReferenceTraversal, planHash, provenance)
		if err != nil {
			return err
		}
		traversalRaw = &raw
		if mappedTraversal.ReferenceRecord != nil {
			records = append(records, recordpackage.ReferenceRecord(*mappedTraversal.ReferenceRecord))
		}
	}

	rawEvidence, err := collectRawEvidence(runRoot, input)
	if err != nil {
		return err
	}
	if !hasRawEvidence(rawEvidence, recordpackage.RawReportContractPath()) {
		return fmt.Errorf("recordemit: raw report evidence is required")
	}
	if traversalRaw != nil {
		rawEvidence = append(rawEvidence, *traversalRaw)
	}

	return recordpackage.WritePackage(recordpackage.PackageInput{
		PackageRoot:               runRoot,
		UseCanonicalDirectoryName: true,
		PackageKey:                packageKeyForPlanHash(planHash),
		Records:                   records,
		RawEvidenceFiles:          rawEvidence,
		OverwriteExisting:         true,
	})
}

func mapCADRuntimeRecords(evidence *CADRuntimeRunEvidence, recordKey, planHash string, provenance recordcontract.Provenance, reportFailure *recordcontract.FailureRecord) ([]recordpackage.Record, *recordcontract.FailureRecord, error) {
	if evidence == nil {
		return nil, nil, nil
	}
	linkage := recordmap.ObservedMappingLinkage{JobID: evidence.JobID, ProductKey: evidence.ProductKey, StepRef: evidence.StepRef}
	records := make([]recordpackage.Record, 0, 2)
	if evidence.ObservedValue != nil {
		mapped, err := recordmap.MapObservedToObservationRecord(recordmap.ObservedMappingInput{
			Observed:             *evidence.ObservedValue,
			RecordKey:            recordKey + ":observation",
			Provenance:           provenance,
			Linkage:              linkage,
			EvidenceDigestSHA256: evidenceDigest(evidence.Observed),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("recordemit: map observed evidence: %w", err)
		}
		records = append(records, recordpackage.ObservationRecord(mapped))
	}
	if evidence.VerificationResult != nil {
		mapped, err := recordmap.MapVerificationToVerificationRecord(recordmap.VerificationMappingInput{
			Result:                       *evidence.VerificationResult,
			RecordKey:                    recordKey + ":verification",
			Provenance:                   provenance,
			Linkage:                      recordmap.VerificationMappingLinkage{JobID: evidence.JobID, ProductKey: evidence.ProductKey, StepRef: evidence.StepRef},
			EvidenceDigestSHA256:         evidenceDigest(evidence.Verification),
			ObservedEvidenceDigestSHA256: evidenceDigest(evidence.Observed),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("recordemit: map verification result: %w", err)
		}
		records = append(records, recordpackage.VerificationRecord(mapped))
	}
	var failureRecord *recordcontract.FailureRecord
	if evidence.Failure != nil {
		// A failed run has no metadata provenance; like the report-derived
		// failure record it replaces, the record still names its plan.
		failureProvenance := provenance
		if strings.TrimSpace(failureProvenance.Plan.PlanHash) == "" {
			failureProvenance.Plan.PlanHash = planHash
		}
		mappingInput := recordmap.CADRuntimeFailureMappingInput{
			Failure:              *evidence.Failure,
			RecordKey:            recordKey,
			Provenance:           failureProvenance,
			Linkage:              recordcontract.FailureLinkage{JobID: evidence.JobID, ProductKey: evidence.ProductKey, StepRef: evidence.StepRef},
			EvidenceDigestSHA256: evidenceDigest(evidence.Result),
		}
		// Only Engine-owned operational context comes from the report-derived
		// failure; class, stage, code and message stay runtime-native.
		if reportFailure != nil {
			mappingInput.RetryCount = reportFailure.Failure.RetryCount
			mappingInput.OccurredAt = reportFailure.Failure.OccurredAt
		}
		mapped, err := recordmap.MapCADRuntimeFailure(mappingInput)
		if err != nil {
			return nil, nil, fmt.Errorf("recordemit: map CAD runtime failure: %w", err)
		}
		failureRecord = &mapped
	}
	return records, failureRecord, nil
}

func evidenceDigest(content []byte) string {
	if len(content) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

func mapReferenceTraversal(evidence *ReferenceTraversalRunEvidence, planHash string, provenance recordcontract.Provenance) (recordmap.ReferenceTraversalMappingOutput, recordpackage.RawEvidenceFile, error) {
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
		Traversal:            traversal,
		RecordKey:            packageKeyForPlanHash(planHash) + ":reference",
		Provenance:           provenance,
		Linkage:              recordmap.ReferenceTraversalMappingLinkage{JobID: evidence.JobID, ProductKey: evidence.ProductKey, StepRef: evidence.StepRef},
		EvidenceDigestSHA256: digest,
	})
	if err != nil {
		return recordmap.ReferenceTraversalMappingOutput{}, recordpackage.RawEvidenceFile{}, fmt.Errorf("recordemit: map reference traversal: %w", err)
	}
	return mapped, recordpackage.RawEvidenceFile{ContractPath: recordpackage.RawRuntimeReferenceTraversalContractPath(), Content: content}, nil
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
		{contractPath: recordpackage.RawArtifactStoreManifestContractPath(), runPath: filepath.Join(runRoot, "manifest.json")},
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
	if input.CADRuntime != nil {
		for _, evidence := range []recordpackage.RawEvidenceFile{
			{ContractPath: recordpackage.RawRuntimeResultContractPath(), Content: input.CADRuntime.Result},
			{ContractPath: recordpackage.RawVerificationContractPath(), Content: input.CADRuntime.Verification},
			{ContractPath: recordpackage.RawObservedContractPath(), Content: input.CADRuntime.Observed},
		} {
			if len(evidence.Content) > 0 {
				out = append(out, evidence)
			}
		}
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
