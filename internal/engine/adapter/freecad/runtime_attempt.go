package freecad

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/runtimecap"
)

const (
	freeCADRuntimeAttemptIdentityContractVersion = "freecad-runtime-attempt-identity-v1"
	freeCADRuntimeAttemptNumberWidth             = 6

	FreeCADRuntimeAttemptStageRequestValidation = "request_validation"
	FreeCADRuntimeAttemptStageAttemptIdentity   = "attempt_identity"
	FreeCADRuntimeAttemptStageWorkingCopyLayout = "working_copy_layout"
	FreeCADRuntimeAttemptStageSourcePreparation = "source_preparation"
)

// FreeCADRuntimeAttemptIdentity is deterministic runtime-operational identity
// for one FreeCAD CAD-runtime attempt. It is not written into planner, job,
// handoff, cache, or normalized record contracts.
type FreeCADRuntimeAttemptIdentity struct {
	ID         string
	JobID      string
	ProductKey string
	StepID     string
	Attempt    int
	Adapter    string
	PlanHash   string
}

// FreeCADRuntimeAttempt is the reusable prepared-or-computed FreeCAD attempt
// state for Tasks 8–10.
type FreeCADRuntimeAttempt struct {
	Identity   FreeCADRuntimeAttemptIdentity
	SourcePath string
	Layout     FreeCADRuntimeWorkingCopyLayout
}

// FreeCADRuntimeAttemptError is a typed FreeCAD attempt computation or
// preparation failure with stable stage and optional field context.
type FreeCADRuntimeAttemptError struct {
	Stage     string
	Field     string
	AttemptID string
	Err       error
}

func (e *FreeCADRuntimeAttemptError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("freecad runtime attempt")
	if e.Stage != "" {
		b.WriteString(": ")
		b.WriteString(e.Stage)
	}
	if e.Field != "" {
		b.WriteString(": ")
		b.WriteString(e.Field)
	}
	if e.AttemptID != "" {
		b.WriteString(": attempt=")
		b.WriteString(e.AttemptID)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *FreeCADRuntimeAttemptError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ComputeFreeCADRuntimeAttempt derives deterministic FreeCAD attempt identity
// and working-copy layout from a validated orchestration request without
// creating directories, files, or processes.
func ComputeFreeCADRuntimeAttempt(req adapter.CADRuntimeOrchestrationRequest) (FreeCADRuntimeAttempt, error) {
	if err := adapter.ValidateCADRuntimeOrchestrationRequest(req); err != nil {
		return FreeCADRuntimeAttempt{}, wrapFreeCADRuntimeAttemptRequestValidationError(err)
	}
	if err := validateFreeCADRuntimeAttemptRequest(req); err != nil {
		return FreeCADRuntimeAttempt{}, err
	}

	productDir := req.ProductDir
	sourcePath := req.Manifest.Inputs.SourceModel
	identity, err := deriveFreeCADRuntimeAttemptIdentity(req, sourcePath)
	if err != nil {
		return FreeCADRuntimeAttempt{}, &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageAttemptIdentity,
			Err:   err,
		}
	}

	layout, err := computeFreeCADRuntimeWorkingCopyLayoutWithID(
		productDir,
		sourcePath,
		identity.ID,
		req.CADRuntime.ManifestFilename,
		req.CADRuntime.ResultFilename,
	)
	if err != nil {
		return FreeCADRuntimeAttempt{}, &FreeCADRuntimeAttemptError{
			Stage:     FreeCADRuntimeAttemptStageWorkingCopyLayout,
			AttemptID: identity.ID,
			Err:       err,
		}
	}

	return FreeCADRuntimeAttempt{
		Identity:   identity,
		SourcePath: sourcePath,
		Layout:     layout,
	}, nil
}

// PrepareFreeCADRuntimeAttempt recomputes the authoritative attempt layout from
// the orchestration request, atomically prepares the source document, prepares
// the output directory, and validates the prepared source.
func PrepareFreeCADRuntimeAttempt(req adapter.CADRuntimeOrchestrationRequest) (FreeCADRuntimeAttempt, error) {
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		return FreeCADRuntimeAttempt{}, err
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     attempt.Layout,
		SourcePath: attempt.SourcePath,
	}); err != nil {
		return FreeCADRuntimeAttempt{}, &FreeCADRuntimeAttemptError{
			Stage:     FreeCADRuntimeAttemptStageSourcePreparation,
			AttemptID: attempt.Identity.ID,
			Err:       err,
		}
	}

	return attempt, nil
}

func validateFreeCADRuntimeAttemptRequest(req adapter.CADRuntimeOrchestrationRequest) error {
	if req.CADRuntime.Adapter != runtimecap.FreeCADAdapterID {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "CADRuntime.Adapter",
			Err: fmt.Errorf(
				"must equal %q: got=%q",
				runtimecap.FreeCADAdapterID,
				req.CADRuntime.Adapter,
			),
		}
	}
	if err := requireCanonicalAbsoluteFreeCADRuntimeHostPath("ProductDir", req.ProductDir); err != nil {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "ProductDir",
			Err:   err,
		}
	}
	if err := validateCanonicalNonNegativeDecimalStepID(req.StepID); err != nil {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "StepID",
			Err:   err,
		}
	}
	if req.Attempt < 1 {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "Attempt",
			Err:   fmt.Errorf("must be >= 1"),
		}
	}
	if err := validateFreeCADRuntimeAttemptPlanHash(req.Manifest.PlanHash); err != nil {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "Manifest.PlanHash",
			Err:   err,
		}
	}
	if err := requireCanonicalAbsoluteFreeCADRuntimeHostPath(
		"Manifest.Inputs.SourceModel",
		req.Manifest.Inputs.SourceModel,
	); err != nil {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "Manifest.Inputs.SourceModel",
			Err:   err,
		}
	}
	if err := validateFreeCADRuntimeLogicalFilename(
		"CADRuntime.ManifestFilename",
		req.CADRuntime.ManifestFilename,
	); err != nil {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "CADRuntime.ManifestFilename",
			Err:   err,
		}
	}
	if err := validateFreeCADRuntimeLogicalFilename(
		"CADRuntime.ResultFilename",
		req.CADRuntime.ResultFilename,
	); err != nil {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "CADRuntime.ResultFilename",
			Err:   err,
		}
	}
	if req.CADRuntime.ManifestFilename != req.Manifest.ManifestFilename {
		return &FreeCADRuntimeAttemptError{
			Stage: FreeCADRuntimeAttemptStageRequestValidation,
			Field: "CADRuntime.ManifestFilename",
			Err: fmt.Errorf(
				"must match Manifest.ManifestFilename: got=%q want=%q",
				req.CADRuntime.ManifestFilename,
				req.Manifest.ManifestFilename,
			),
		}
	}
	return nil
}

func deriveFreeCADRuntimeAttemptIdentity(
	req adapter.CADRuntimeOrchestrationRequest,
	canonicalSourcePath string,
) (FreeCADRuntimeAttemptIdentity, error) {
	if req.Attempt < 1 {
		return FreeCADRuntimeAttemptIdentity{}, fmt.Errorf("attempt must be >= 1")
	}

	hash := freeCADRuntimeAttemptIdentityHash(
		freeCADRuntimeAttemptIdentityContractVersion,
		req.JobID,
		req.ProductKey,
		req.StepID,
		req.CADRuntime.Adapter,
		req.Manifest.PlanHash,
		canonicalSourcePath,
		req.CADRuntime.ManifestFilename,
		req.CADRuntime.ResultFilename,
		strconv.Itoa(req.Attempt),
	)

	prefix := sanitizeFreeCADRuntimeWorkingCopyPrefix(req.ProductKey)
	if prefix == "" {
		prefix = "attempt"
	}
	id := fmt.Sprintf(
		"%s-%s-attempt-%0*d",
		prefix,
		hash,
		freeCADRuntimeAttemptNumberWidth,
		req.Attempt,
	)
	if err := validateFreeCADRuntimeWorkingCopyID(id); err != nil {
		return FreeCADRuntimeAttemptIdentity{}, err
	}

	return FreeCADRuntimeAttemptIdentity{
		ID:         id,
		JobID:      req.JobID,
		ProductKey: req.ProductKey,
		StepID:     req.StepID,
		Attempt:    req.Attempt,
		Adapter:    req.CADRuntime.Adapter,
		PlanHash:   req.Manifest.PlanHash,
	}, nil
}

func freeCADRuntimeAttemptIdentityHash(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		fmt.Fprintf(hash, "%d:", len(value))
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))[:freeCADRuntimeWorkingCopyHashLength]
}

func validateCanonicalNonNegativeDecimalStepID(stepID string) error {
	if stepID == "" {
		return fmt.Errorf("must be non-empty")
	}
	if strings.TrimSpace(stepID) != stepID {
		return fmt.Errorf("must not have surrounding whitespace")
	}
	if strings.Contains(stepID, "\x00") {
		return fmt.Errorf("contains null byte")
	}
	value, err := strconv.ParseUint(stepID, 10, 64)
	if err != nil {
		return fmt.Errorf("must be canonical non-negative decimal: %w", err)
	}
	if strconv.FormatUint(value, 10) != stepID {
		return fmt.Errorf("must be canonical non-negative decimal")
	}
	return nil
}

func validateFreeCADRuntimeAttemptPlanHash(planHash string) error {
	if planHash == "" {
		return fmt.Errorf("must be non-empty")
	}
	if strings.TrimSpace(planHash) != planHash {
		return fmt.Errorf("must not have surrounding whitespace")
	}
	if strings.Contains(planHash, "\x00") {
		return fmt.Errorf("contains null byte")
	}
	for _, r := range planHash {
		if unicode.IsControl(r) {
			return fmt.Errorf("contains control character")
		}
	}
	return nil
}

func wrapFreeCADRuntimeAttemptRequestValidationError(err error) error {
	field := ""
	var requestErr *adapter.CADRuntimeOrchestrationRequestError
	if errors.As(err, &requestErr) {
		field = requestErr.Field
	}
	return &FreeCADRuntimeAttemptError{
		Stage: FreeCADRuntimeAttemptStageRequestValidation,
		Field: field,
		Err:   err,
	}
}
