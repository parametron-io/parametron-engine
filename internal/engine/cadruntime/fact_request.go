package cadruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/recordcontract"
)

const FactRequestFilename = "prm.fact-request.json"
const FactEvidenceFilename = "prm.fact-evidence.json"

// writeFactRequest writes an optional sidecar in the already prepared, confined
// attempt. No new process argument or runtime capability is implied.
func writeFactRequest(root string, request *recordcontract.FactRequest) error {
	destination := filepath.Join(root, FactRequestFilename)
	if err := removeStaleFreeCADRuntimeOutput(root, destination); err != nil {
		return err
	}
	if request == nil {
		return nil
	}
	if err := recordcontract.ValidateFactRequest(*request); err != nil {
		return err
	}
	content, err := json.Marshal(request)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(root, ".fact-request-*")
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
		return fmt.Errorf("write fact request: %w", err)
	}
	return nil
}

// FactRun retains opt-in evidence separately from the raw execution result.
type FactRun struct {
	Request *recordcontract.FactRequest
	Path    string
	Content []byte
}

// LoadFactRun reads only the canonical confined evidence path, correlates it
// with explicit intent, and validates it before acceptance or packaging.
func LoadFactRun(req adapter.CADRuntimeOrchestrationRequest, run FreeCADRuntimeRun) (*FactRun, error) {
	if req.FactObservation == nil {
		return nil, nil
	}
	attempt := run.ObservationRequest.Manifest.Attempt
	path := filepath.Join(attempt.Layout.OutputDir, FactEvidenceFilename)
	content, err := loadOptionalRegularFreeCADRuntimeBytes(attempt.Layout.WorkingCopyDir, path)
	if err != nil {
		return nil, err
	}
	out := &FactRun{Request: recordcontract.CloneFactRequest(req.FactObservation), Path: path, Content: content}
	_, err = MapFactEvidence(FactMappingInput{Request: out.Request, Resources: attempt.Resources, ExpectedSourceDocument: attempt.Layout.SourceDocument, Content: content})
	return out, err
}

// DecodeFactRequest strictly accepts the explicit versioned request sidecar.
func DecodeFactRequest(content []byte) (recordcontract.FactRequest, error) {
	var request recordcontract.FactRequest
	if err := uniqueOccurrenceJSON(content); err != nil {
		return request, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("%w: %v", recordcontract.ErrInvalidFact, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return request, fmt.Errorf("%w: trailing JSON", recordcontract.ErrInvalidFact)
	}
	if err := recordcontract.ValidateFactRequest(request); err != nil {
		return request, err
	}
	return request, nil
}
