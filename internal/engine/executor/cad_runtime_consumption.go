package executor

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/verification"
)

const (
	CADRuntimeConsumptionStageOutcomeUnavailable  = "outcome_unavailable"
	CADRuntimeConsumptionStageOutcomeValidation   = "outcome_validation"
	CADRuntimeConsumptionStageRuntimeFailure      = "runtime_failure"
	CADRuntimeConsumptionStageVerificationFailure = "verification_failure"
	CADRuntimeConsumptionStageVerificationResult  = "verification_result"
	CADRuntimeConsumptionStageArtifactProjection  = "artifact_projection"
)

type CADRuntimeVerifiedRunProvider interface {
	CADRuntimeVerifiedRun() (cadruntime.FreeCADRuntimeVerifiedRun, bool)
}

type CADRuntimeArtifactOutcome struct {
	RuntimeID    string
	Format       string
	RelativePath string
	Path         string
}

type CADRuntimeFailureOutcome struct {
	Classification string
	Boundary       string
	Category       string
	Code           string
	Stage          string
	Message        string
}

type CADRuntimeOutcome struct {
	AttemptID              string
	JobID                  string
	ProductKey             string
	StepID                 string
	Attempt                int
	Adapter                string
	ResultPath             string
	ReferenceTraversalPath string
	ReferenceTraversalJSON []byte
	Artifacts              []CADRuntimeArtifactOutcome
	Verification           artifact.VerificationOutcome
	VerificationClass      verification.FailureClass
	Failure                *CADRuntimeFailureOutcome
}

type CADRuntimeConsumptionError struct {
	Stage          string
	JobID          string
	ProductKey     string
	StepID         string
	Attempt        int
	AttemptID      string
	Classification string
	Boundary       string
	Category       string
	Code           string
	NativeStage    string
	Message        string
	Err            error
}

func (e *CADRuntimeConsumptionError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("cad runtime consumption")
	for _, value := range []string{e.Stage, "job=" + e.JobID, "product=" + e.ProductKey, "step=" + e.StepID} {
		if value != "" && !strings.HasSuffix(value, "=") {
			b.WriteString(": ")
			b.WriteString(value)
		}
	}
	if e.Attempt > 0 {
		b.WriteString(": attempt=")
		b.WriteString(strconv.Itoa(e.Attempt))
	}
	if e.AttemptID != "" {
		b.WriteString(": attemptId=")
		b.WriteString(e.AttemptID)
	}
	for _, value := range []string{e.Classification, e.Boundary, e.Category, e.Code, e.NativeStage} {
		if value != "" {
			b.WriteString(": ")
			b.WriteString(value)
		}
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	} else if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *CADRuntimeConsumptionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Executor) CADRuntimeOutcome() *CADRuntimeOutcome {
	if e == nil {
		return nil
	}
	return cloneCADRuntimeOutcome(e.cadRuntimeOutcome)
}

func cloneCADRuntimeOutcome(in *CADRuntimeOutcome) *CADRuntimeOutcome {
	if in == nil {
		return nil
	}
	out := *in
	out.Artifacts = append([]CADRuntimeArtifactOutcome(nil), in.Artifacts...)
	out.ReferenceTraversalJSON = append([]byte(nil), in.ReferenceTraversalJSON...)
	if in.Failure != nil {
		failure := *in.Failure
		out.Failure = &failure
	}
	return &out
}

func (e *Executor) consumeCADRuntimeResult(req adapter.CADRuntimeOrchestrationRequest, run cadruntime.FreeCADRuntimeVerifiedRun, orchestrationErr error) error {
	attempt := run.Runtime.ObservationRequest.Manifest.Attempt
	identity := attempt.Identity
	outcome := &CADRuntimeOutcome{
		AttemptID: identity.ID, JobID: identity.JobID, ProductKey: identity.ProductKey,
		StepID: identity.StepID, Attempt: identity.Attempt, Adapter: identity.Adapter,
		ResultPath:             run.Runtime.ExecutionRequest.ResultPath,
		ReferenceTraversalPath: run.Runtime.ReferenceTraversalPath,
		ReferenceTraversalJSON: append([]byte(nil), run.Runtime.ReferenceTraversalJSON...),
		Verification:           artifact.VerificationOutcomeUnknown,
	}

	if err := validateConsumedRuntimeIdentity(req, run); err != nil {
		return e.consumptionError(CADRuntimeConsumptionStageOutcomeValidation, req, identity.ID, "", "", "", "", "", err)
	}
	// Identity correlation is the trust boundary. Once it succeeds, this
	// attempt replaces any prior snapshot even if its semantic state is later
	// rejected as contradictory.
	e.cadRuntimeOutcome = cloneCADRuntimeOutcome(outcome)
	for _, item := range run.Runtime.Artifacts {
		switch item.Format {
		case "step", "csv", "pdf":
		default:
			return e.consumptionError(
				CADRuntimeConsumptionStageArtifactProjection,
				req,
				identity.ID,
				"",
				"",
				"",
				"",
				"",
				fmt.Errorf("validated runtime artifact %q has unsupported format %q", item.ID, item.Format),
			)
		}
		outcome.Artifacts = append(outcome.Artifacts, CADRuntimeArtifactOutcome{
			RuntimeID: item.ID, Format: item.Format, RelativePath: item.RelativePath, Path: item.Path,
		})
	}
	e.cadRuntimeOutcome = cloneCADRuntimeOutcome(outcome)

	var verificationErr *cadruntime.FreeCADRuntimeVerificationError
	if orchestrationErr == nil {
		if run.Runtime.Result == nil || run.Runtime.Result.Status != freecad.FreeCADRuntimeResultStatusSucceeded ||
			run.Verification == nil || run.Verification.Status != verification.StatusPass ||
			run.Verification.Failure != verification.FailureClassNone {
			return e.consumptionError(CADRuntimeConsumptionStageOutcomeValidation, req, identity.ID, "", "", "", "", "", errors.New("provider returned contradictory successful runtime verification state"))
		}
		outcome.Verification = artifact.VerificationOutcomePassed
		e.cadRuntimeOutcome = cloneCADRuntimeOutcome(outcome)
		e.acceptance.VerificationOutcome = artifact.VerificationOutcomePassed
		return nil
	}
	if !errors.As(orchestrationErr, &verificationErr) {
		return e.consumptionError(CADRuntimeConsumptionStageRuntimeFailure, req, identity.ID, "", "engine", "runtime", "adapter_orchestration", "adapter_orchestration", orchestrationErr)
	}

	switch verificationErr.Stage {
	case cadruntime.FreeCADRuntimeVerificationStageVerificationComparison:
		if run.Runtime.Result == nil || run.Runtime.Result.Status != freecad.FreeCADRuntimeResultStatusSucceeded ||
			run.Verification == nil || run.Verification.Status != verification.StatusFail ||
			run.Verification.Failure != verificationErr.FailureClass ||
			verificationErr.FailureClass == verification.FailureClassNone {
			return e.consumptionError(CADRuntimeConsumptionStageOutcomeValidation, req, identity.ID, "", "", "", "", verificationErr.Stage, errors.New("provider returned contradictory verification failure state"))
		}
		outcome.Verification = artifact.VerificationOutcomeFailed
		outcome.VerificationClass = verificationErr.FailureClass
		message := verificationErr.Error()
		if run.Verification != nil && run.Verification.Message != "" {
			message = run.Verification.Message
		}
		outcome.Failure = &CADRuntimeFailureOutcome{
			Classification: string(verificationErr.FailureClass), Boundary: "engine", Category: "verification",
			Code: string(verificationErr.FailureClass), Stage: verificationErr.Stage, Message: message,
		}
		e.cadRuntimeOutcome = cloneCADRuntimeOutcome(outcome)
		e.acceptance.VerificationOutcome = artifact.VerificationOutcomeFailed
		return e.consumptionError(CADRuntimeConsumptionStageVerificationFailure, req, identity.ID,
			string(verificationErr.FailureClass), "engine", "verification", string(verificationErr.FailureClass), verificationErr.Stage, errors.New(message), orchestrationErr)

	case cadruntime.FreeCADRuntimeVerificationStageVerificationResult:
		outcome.Verification = artifact.VerificationOutcomeUnknown
		outcome.VerificationClass = verification.FailureClassInternalError
		outcome.Failure = &CADRuntimeFailureOutcome{
			Classification: string(verification.FailureClassInternalError), Boundary: "engine", Category: "verification",
			Code: string(verification.FailureClassInternalError), Stage: verificationErr.Stage, Message: verificationErr.Error(),
		}
		e.cadRuntimeOutcome = cloneCADRuntimeOutcome(outcome)
		e.acceptance.VerificationOutcome = artifact.VerificationOutcomeUnknown
		return e.consumptionError(CADRuntimeConsumptionStageVerificationResult, req, identity.ID,
			string(verification.FailureClassInternalError), "engine", "verification", string(verification.FailureClassInternalError), verificationErr.Stage, errors.New(verificationErr.Error()), orchestrationErr)

	case cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution:
		if run.Verification != nil {
			return e.consumptionError(CADRuntimeConsumptionStageOutcomeValidation, req, identity.ID, "", "", "", "", verificationErr.Stage, errors.New("provider returned verification state for a pre-verification runtime failure"))
		}
		outcome.Verification = artifact.VerificationOutcomeNotRun
		var runErr *cadruntime.FreeCADRuntimeRunError
		_ = errors.As(orchestrationErr, &runErr)
		boundary, category, code, nativeStage, classification := "engine", "runtime", "", "", ""
		message := verificationErr.Error()
		if run.Runtime.Result != nil && run.Runtime.Result.Failure != nil {
			failure := run.Runtime.Result.Failure
			boundary, category, code, classification = failure.Boundary, failure.Category, failure.Code, failure.Code
			if failure.Stage != nil {
				nativeStage = *failure.Stage
			}
			message = failure.Message
		} else if runErr != nil {
			code, nativeStage, classification = runErr.Stage, runErr.Stage, runErr.Stage
		}
		outcome.Failure = &CADRuntimeFailureOutcome{
			Classification: classification, Boundary: boundary, Category: category,
			Code: code, Stage: nativeStage, Message: message,
		}
		e.cadRuntimeOutcome = cloneCADRuntimeOutcome(outcome)
		e.acceptance.VerificationOutcome = artifact.VerificationOutcomeNotRun
		return e.consumptionError(CADRuntimeConsumptionStageRuntimeFailure, req, identity.ID,
			classification, boundary, category, code, nativeStage, errors.New(message), orchestrationErr)
	default:
		return e.consumptionError(CADRuntimeConsumptionStageOutcomeValidation, req, identity.ID, "", "", "", "", verificationErr.Stage, orchestrationErr)
	}
}

func validateConsumedRuntimeIdentity(req adapter.CADRuntimeOrchestrationRequest, run cadruntime.FreeCADRuntimeVerifiedRun) error {
	attempt := run.Runtime.ObservationRequest.Manifest.Attempt
	id := attempt.Identity
	checks := []struct {
		field     string
		got, want any
	}{
		{"JobID", id.JobID, req.JobID}, {"ProductKey", id.ProductKey, req.ProductKey},
		{"StepID", id.StepID, req.StepID}, {"Attempt", id.Attempt, req.Attempt},
		{"Adapter", id.Adapter, req.CADRuntime.Adapter}, {"PlanHash", id.PlanHash, req.Manifest.PlanHash},
		{"ResultPath", run.Runtime.ExecutionRequest.ResultPath, attempt.Layout.ResultPath},
		{"OutputDir", run.Runtime.ExecutionRequest.OutputDir, attempt.Layout.OutputDir},
	}
	for _, check := range checks {
		if check.got != check.want {
			return fmt.Errorf("%s mismatch: got %v, want %v", check.field, check.got, check.want)
		}
	}
	if strings.TrimSpace(id.ID) == "" {
		return errors.New("attempt identity is missing")
	}
	owned := attempt.Layout.WorkingCopyDir
	if !filepath.IsAbs(owned) || filepath.Clean(owned) != owned {
		return fmt.Errorf("attempt working-copy path %q is not canonical absolute", owned)
	}
	for _, path := range append([]string{run.Runtime.ExecutionRequest.ResultPath, attempt.Layout.OutputDir}, artifactPaths(run.Runtime.Artifacts)...) {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("attempt path %q is not canonical absolute", path)
		}
		rel, err := filepath.Rel(owned, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("attempt path %q is outside working copy %q", path, owned)
		}
	}
	wantTraversalPath := filepath.Join(attempt.Layout.OutputDir, cadruntime.FreeCADRuntimeReferenceTraversalFilename)
	if run.Runtime.ReferenceTraversalPath != "" && run.Runtime.ReferenceTraversalPath != wantTraversalPath {
		return fmt.Errorf("ReferenceTraversalPath mismatch: got %q, want %q", run.Runtime.ReferenceTraversalPath, wantTraversalPath)
	}
	if len(run.Runtime.ReferenceTraversalJSON) > 0 {
		path := run.Runtime.ReferenceTraversalPath
		if path == "" {
			return fmt.Errorf("reference traversal bytes require authoritative path %q", wantTraversalPath)
		}
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("reference traversal path %q is not canonical absolute", path)
		}
		rel, err := filepath.Rel(owned, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("reference traversal path %q is outside working copy %q", path, owned)
		}
	}
	for _, item := range run.Runtime.Artifacts {
		relative := filepath.FromSlash(item.RelativePath)
		if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
			return fmt.Errorf("runtime artifact %q relative path %q is not canonical relative", item.ID, item.RelativePath)
		}
		if filepath.Join(owned, relative) != item.Path {
			return fmt.Errorf("runtime artifact %q path %q does not match relative path %q", item.ID, item.Path, item.RelativePath)
		}
	}
	return nil
}

func artifactPaths(items []cadruntime.FreeCADRuntimeValidatedArtifact) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Path)
	}
	return out
}

func (e *Executor) consumptionError(stage string, req adapter.CADRuntimeOrchestrationRequest, attemptID, classification, boundary, category, code, nativeStage string, args ...error) error {
	var err error
	if len(args) > 0 {
		err = args[len(args)-1]
	}
	message := ""
	if len(args) > 1 && args[0] != nil {
		message = args[0].Error()
	}
	return &CADRuntimeConsumptionError{
		Stage: stage, JobID: req.JobID, ProductKey: req.ProductKey, StepID: req.StepID,
		Attempt: req.Attempt, AttemptID: attemptID, Classification: classification,
		Boundary: boundary, Category: category, Code: code, NativeStage: nativeStage, Message: message, Err: err,
	}
}

func isTerminalCADRuntimeConsumptionError(err error) bool {
	var consumptionErr *CADRuntimeConsumptionError
	if !errors.As(err, &consumptionErr) {
		return false
	}
	if consumptionErr.Stage != CADRuntimeConsumptionStageRuntimeFailure {
		return true
	}
	var runErr *cadruntime.FreeCADRuntimeRunError
	if !errors.As(err, &runErr) {
		return true
	}
	return runErr.Stage != cadruntime.FreeCADRuntimeRunStageProcessInvocation &&
		runErr.Stage != cadruntime.FreeCADRuntimeRunStageRuntimeReportedFailure
}
