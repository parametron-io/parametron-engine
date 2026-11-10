package cadruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/recordcontract"
)

const OccurrenceRequestFilename = "prm.occurrence-request.json"
const OccurrenceEvidenceFilename = "prm.occurrence-evidence.json"

// writeOccurrenceRequest writes an optional sidecar in the already prepared,
// confined attempt. No new process argument or runtime capability is implied.
func writeOccurrenceRequest(root string, request *recordcontract.OccurrenceRequest) error {
	destination := filepath.Join(root, OccurrenceRequestFilename)
	if err := removeStaleFreeCADRuntimeOutput(root, destination); err != nil {
		return err
	}
	if request == nil {
		return nil
	}
	if err := recordcontract.ValidateOccurrenceRequest(*request); err != nil {
		return err
	}
	content, err := json.Marshal(request)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(root, ".occurrence-request-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(append(content, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), destination); err != nil {
		return fmt.Errorf("write occurrence request: %w", err)
	}
	return nil
}

// OccurrenceRun retains opt-in evidence separately from the raw execution result.
type OccurrenceRun struct {
	Request *recordcontract.OccurrenceRequest
	Path    string
	Content []byte
}

// LoadOccurrenceRun reads only the canonical confined evidence path, correlates
// it with explicit intent, and validates it before acceptance or packaging.
func LoadOccurrenceRun(req adapter.CADRuntimeOrchestrationRequest, run FreeCADRuntimeRun) (*OccurrenceRun, error) {
	if req.OccurrenceObservation == nil {
		return nil, nil
	}
	attempt := run.ObservationRequest.Manifest.Attempt
	path := filepath.Join(attempt.Layout.OutputDir, OccurrenceEvidenceFilename)
	content, err := loadOptionalRegularFreeCADRuntimeBytes(attempt.Layout.WorkingCopyDir, path)
	if err != nil {
		return nil, err
	}
	out := &OccurrenceRun{Request: recordcontract.CloneOccurrenceRequest(req.OccurrenceObservation), Path: path, Content: content}
	_, err = MapOccurrenceEvidence(OccurrenceMappingInput{Request: out.Request, Resources: attempt.Resources, ExpectedSourceDocument: attempt.Layout.SourceDocument, Content: content})
	return out, err
}
