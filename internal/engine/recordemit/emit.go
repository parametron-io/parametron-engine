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

	"parametron/internal/engine/metadata"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
)

const packageKeyPrefix = "engine-run:"

// RunEmitInput carries material for emitting a local record package from a normal Engine run.
type RunEmitInput struct {
	RunRoot            string
	PlanHash           string
	Report             report.Report
	Metadata           *metadata.Metadata
	ReferenceTraversal *ReferenceTraversalRunEvidence
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
	if mappedReport.FailureRecord != nil {
		records = append(records, recordpackage.FailureRecord(*mappedReport.FailureRecord))
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
		{contractPath: recordpackage.RawReportContractPath(), runPath: filepath.Join(runRoot, "report.json")},
		{contractPath: recordpackage.RawMetadataContractPath(), runPath: filepath.Join(runRoot, "metadata.json")},
		{contractPath: recordpackage.RawArtifactStoreManifestContractPath(), runPath: filepath.Join(runRoot, "manifest.json")},
		{contractPath: recordpackage.RawObservedContractPath(), runPath: filepath.Join(runRoot, "parametron.observed.json")},
		{contractPath: recordpackage.RawVerificationContractPath(), runPath: filepath.Join(runRoot, "parametron.verification.json")},
		{contractPath: recordpackage.RawRuntimeResultContractPath(), runPath: filepath.Join(runRoot, "result.json")},
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
