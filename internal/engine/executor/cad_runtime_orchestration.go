package executor

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/runtimecap"
)

const (
	cadRuntimeDispatchStagePreflight            = "preflight"
	cadRuntimeDispatchStageExecutableSelection  = "executable_selection"
	cadRuntimeDispatchStageRequestValidation    = "request_validation"
	cadRuntimeDispatchStageAdapterOrchestration = "adapter_orchestration"
)

var (
	ErrCADRuntimeHandoffRequired                = errors.New("CAD-runtime execution requires a handoff package")
	ErrCADRuntimePayloadMismatch                = errors.New("CAD-runtime payload is missing or mismatched")
	ErrCADRuntimeProductDirMissing              = errors.New("CAD-runtime product directory is missing")
	ErrCADRuntimeResolverNotConfigured          = errors.New("CAD-runtime executable resolver is not configured")
	ErrCADRuntimeOrchestratorUnsupported        = errors.New("adapter does not implement CAD-runtime orchestration")
	ErrCADRuntimeVerifiedRunProviderUnsupported = errors.New("adapter does not implement CAD-runtime verified-run provider")
)

// CADRuntimeDispatchError is an inspectable Executor-side CAD-runtime
// orchestration failure.
type CADRuntimeDispatchError struct {
	Stage      string
	JobID      string
	ProductKey string
	Adapter    string
	StepID     string
	Attempt    int
	Err        error
}

func (e *CADRuntimeDispatchError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("cad runtime dispatch")
	if e.Stage != "" {
		b.WriteString(": ")
		b.WriteString(e.Stage)
	}
	if e.JobID != "" {
		b.WriteString(": job=")
		b.WriteString(e.JobID)
	}
	if e.ProductKey != "" {
		b.WriteString(": product=")
		b.WriteString(e.ProductKey)
	}
	if e.Adapter != "" {
		b.WriteString(": adapter=")
		b.WriteString(e.Adapter)
	}
	if e.StepID != "" {
		b.WriteString(": step=")
		b.WriteString(e.StepID)
	}
	if e.Attempt > 0 {
		b.WriteString(": attempt=")
		b.WriteString(strconv.Itoa(e.Attempt))
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *CADRuntimeDispatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type cadRuntimeExecutionDependencies struct {
	resolver runtimecap.ExecutableResolver
}

type cadRuntimeDispatch struct {
	stepIndex    int
	orchestrator adapter.CADRuntimeOrchestrator
	base         adapter.CADRuntimeOrchestrationRequest
	provider     CADRuntimeVerifiedRunProvider
	consume      bool
}

func (e *Executor) prepareCADRuntimeDispatch(plan *planner.ExecutionPlan) (*cadRuntimeDispatch, error) {
	index, step, ok := findSingleCADRuntimeStep(plan)
	if !ok {
		if index < 0 {
			return nil, nil
		}
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStagePreflight, "", "", "", strconv.Itoa(index), 0, ErrCADRuntimePayloadMismatch)
	}

	stepID := strconv.Itoa(index)
	adapterID := cadRuntimeAdapterID(step)

	if e.pkg == nil {
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStagePreflight, "", "", adapterID, stepID, 0, ErrCADRuntimeHandoffRequired)
	}
	if e.pkg.CADRuntime == nil {
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStagePreflight, e.pkg.JobID, e.pkg.ProductKey, adapterID, stepID, 0, ErrCADRuntimePayloadMismatch)
	}

	payload, ok := step.Payload.(planner.RunCADRuntimePayload)
	if !ok || payload != *e.pkg.CADRuntime {
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStagePreflight, e.pkg.JobID, e.pkg.ProductKey, e.pkg.CADRuntime.Adapter, stepID, 0, ErrCADRuntimePayloadMismatch)
	}

	if strings.TrimSpace(e.productDir) == "" {
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStagePreflight, e.pkg.JobID, e.pkg.ProductKey, payload.Adapter, stepID, 0, ErrCADRuntimeProductDirMissing)
	}

	if e.cadRuntime == nil || e.cadRuntime.resolver == nil {
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStagePreflight, e.pkg.JobID, e.pkg.ProductKey, payload.Adapter, stepID, 0, ErrCADRuntimeResolverNotConfigured)
	}

	orchestrator, ok := e.adapter.(adapter.CADRuntimeOrchestrator)
	if !ok || orchestrator == nil {
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStagePreflight, e.pkg.JobID, e.pkg.ProductKey, payload.Adapter, stepID, 0, ErrCADRuntimeOrchestratorUnsupported)
	}
	selection, err := e.cadRuntime.resolver.Resolve(payload.Adapter)
	if err != nil {
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStageExecutableSelection, e.pkg.JobID, e.pkg.ProductKey, payload.Adapter, stepID, 0, err)
	}

	base := adapter.CADRuntimeOrchestrationRequest{
		JobID:      e.pkg.JobID,
		ProductKey: e.pkg.ProductKey,
		ProductDir: e.productDir,
		StepID:     stepID,
		Attempt:    1,
		CSV:        cloneWriteCSVPayload(e.pkg.CSV),
		Manifest:   cloneManifestPayload(e.pkg.Manifest),
		CADRuntime: cloneRunCADRuntimePayload(payload),
		Executable: selection,
	}
	if err := adapter.ValidateCADRuntimeOrchestrationRequest(base); err != nil {
		return nil, e.wrapCADRuntimeDispatchError(cadRuntimeDispatchStageRequestValidation, e.pkg.JobID, e.pkg.ProductKey, payload.Adapter, stepID, 0, err)
	}
	var provider CADRuntimeVerifiedRunProvider
	if e.cadRuntimeConsumption {
		provider, ok = e.adapter.(CADRuntimeVerifiedRunProvider)
		if !ok || provider == nil {
			return nil, e.consumptionError(
				CADRuntimeConsumptionStageOutcomeUnavailable,
				base,
				"",
				"",
				"",
				"",
				"",
				"",
				ErrCADRuntimeVerifiedRunProviderUnsupported,
			)
		}
	}

	return &cadRuntimeDispatch{
		stepIndex:    index,
		orchestrator: orchestrator,
		base:         base,
		provider:     provider,
		consume:      e.cadRuntimeConsumption,
	}, nil
}

func (e *Executor) executeStep(
	ctx context.Context,
	step planner.Step,
	stepIndex int,
	attempt int,
	cad *cadRuntimeDispatch,
) error {
	if step.Type == planner.StepRunCADRuntime {
		if cad == nil {
			return e.wrapCADRuntimeDispatchError(
				cadRuntimeDispatchStagePreflight,
				"",
				"",
				"",
				strconv.Itoa(stepIndex),
				attempt,
				ErrCADRuntimeHandoffRequired,
			)
		}
		req := e.buildCADRuntimeOrchestrationRequest(cad, attempt)
		if err := adapter.ValidateCADRuntimeOrchestrationRequest(req); err != nil {
			return e.wrapCADRuntimeDispatchError(
				cadRuntimeDispatchStageRequestValidation,
				req.JobID,
				req.ProductKey,
				req.CADRuntime.Adapter,
				req.StepID,
				attempt,
				err,
			)
		}
		orchestrationErr := cad.orchestrator.OrchestrateCADRuntime(ctx, req)
		if cad.consume {
			run, available := cad.provider.CADRuntimeVerifiedRun()
			if !available {
				cause := orchestrationErr
				if cause == nil {
					cause = errors.New("verified-run provider returned no snapshot")
				}
				return e.consumptionError(CADRuntimeConsumptionStageOutcomeUnavailable, req, "", "", "", "", "", "", cause)
			}
			return e.consumeCADRuntimeResult(req, run, orchestrationErr)
		}
		if orchestrationErr != nil {
			return e.wrapCADRuntimeDispatchError(
				cadRuntimeDispatchStageAdapterOrchestration,
				req.JobID,
				req.ProductKey,
				req.CADRuntime.Adapter,
				req.StepID,
				attempt,
				orchestrationErr,
			)
		}
		return nil
	}
	return e.adapter.Run(ctx, step)
}

func (e *Executor) buildCADRuntimeOrchestrationRequest(
	cad *cadRuntimeDispatch,
	attempt int,
) adapter.CADRuntimeOrchestrationRequest {
	return adapter.CADRuntimeOrchestrationRequest{
		JobID:      cad.base.JobID,
		ProductKey: cad.base.ProductKey,
		ProductDir: cad.base.ProductDir,
		StepID:     cad.base.StepID,
		Attempt:    attempt,
		CSV:        cloneWriteCSVPayload(cad.base.CSV),
		Manifest:   cloneManifestPayload(cad.base.Manifest),
		CADRuntime: cloneRunCADRuntimePayload(cad.base.CADRuntime),
		Executable: cad.base.Executable,
	}
}

func (e *Executor) failCADRuntimePreflight(index int, err error) error {
	e.failWithoutStart(index)
	e.acceptance.FinalOutcome = artifact.FinalOutcomeFailed
	return &ExecutionError{
		ProductID:  e.states[index].ProductID,
		StepID:     e.states[index].StepID,
		Err:        err,
		RetryCount: 0,
	}
}

func (e *Executor) wrapCADRuntimeDispatchError(
	stage, jobID, productKey, adapterID, stepID string,
	attempt int,
	err error,
) error {
	return &CADRuntimeDispatchError{
		Stage:      stage,
		JobID:      jobID,
		ProductKey: productKey,
		Adapter:    adapterID,
		StepID:     stepID,
		Attempt:    attempt,
		Err:        err,
	}
}

func findSingleCADRuntimeStep(plan *planner.ExecutionPlan) (int, planner.Step, bool) {
	if plan == nil {
		return -1, planner.Step{}, false
	}
	found := -1
	var step planner.Step
	for i, candidate := range plan.Steps {
		if candidate.Type != planner.StepRunCADRuntime {
			continue
		}
		if found >= 0 {
			return found, step, false
		}
		found = i
		step = candidate
	}
	if found < 0 {
		return -1, planner.Step{}, false
	}
	return found, step, true
}

func cadRuntimeAdapterID(step planner.Step) string {
	payload, ok := step.Payload.(planner.RunCADRuntimePayload)
	if !ok {
		return ""
	}
	return payload.Adapter
}

func cloneRunCADRuntimePayload(payload planner.RunCADRuntimePayload) planner.RunCADRuntimePayload {
	return planner.RunCADRuntimePayload{
		ProductKey:       payload.ProductKey,
		Adapter:          payload.Adapter,
		ManifestFilename: payload.ManifestFilename,
		ResultFilename:   payload.ResultFilename,
	}
}

func cloneCADRuntimePtr(payload *planner.RunCADRuntimePayload) *planner.RunCADRuntimePayload {
	if payload == nil {
		return nil
	}
	cloned := cloneRunCADRuntimePayload(*payload)
	return &cloned
}

func planContainsCADRuntime(plan *planner.ExecutionPlan) bool {
	if plan == nil {
		return false
	}
	for _, step := range plan.Steps {
		if step.Type == planner.StepRunCADRuntime {
			return true
		}
	}
	return false
}
