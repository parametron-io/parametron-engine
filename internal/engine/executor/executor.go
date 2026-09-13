package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cache"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/runtimecap"
)

// Executor runs plans sequentially by delegating each step to the adapter.
type Executor struct {
	adapter               adapter.Adapter
	states                []ExecutionState
	pkg                   *handoff.Package
	jobID                 string
	acceptance            artifact.AcceptanceContext
	store                 artifact.Store // optional; when set, artifacts are registered after each step
	productDir            string         // absolute path to the product output directory; required when store != nil
	strategy              cache.CacheKeyStrategy
	cadRuntime            *cadRuntimeExecutionDependencies
	cadRuntimeConsumption bool
	cadRuntimeOutcome     *CADRuntimeOutcome
}

const defaultStepTimeout = 30 * time.Second
const defaultMaxRetries = 2

const DefaultStepTimeout = defaultStepTimeout
const DefaultMaxRetries = defaultMaxRetries

// New creates an executor without artifact registration.
func New(adp adapter.Adapter) *Executor {
	return &Executor{
		adapter:  adp,
		strategy: cache.GetDefaultStrategy(),
	}
}

// NewWithProductDir creates an executor without step-time artifact registration but
// with productDir set so engine-owned runtime verification can run.
func NewWithProductDir(adp adapter.Adapter, productDir string) *Executor {
	return &Executor{
		adapter:    adp,
		productDir: productDir,
		strategy:   cache.GetDefaultStrategy(),
	}
}

// NewWithArtifacts creates an executor that registers produced files in store after each step.
// productDir must be the absolute path to the directory where the adapter writes output files.
func NewWithArtifacts(adp adapter.Adapter, store artifact.Store, productDir string) *Executor {
	return &Executor{
		adapter:    adp,
		store:      store,
		productDir: productDir,
		strategy:   cache.GetDefaultStrategy(),
	}
}

// NewWithStrategy creates an executor with a custom cache key strategy.
func NewWithStrategy(adp adapter.Adapter, strategy cache.CacheKeyStrategy) *Executor {
	if strategy == nil {
		strategy = cache.GetDefaultStrategy()
	}
	return &Executor{
		adapter:  adp,
		strategy: strategy,
	}
}

// NewWithArtifactsAndStrategy creates an executor with artifact registration and a custom strategy.
func NewWithArtifactsAndStrategy(adp adapter.Adapter, store artifact.Store, productDir string, strategy cache.CacheKeyStrategy) *Executor {
	if strategy == nil {
		strategy = cache.GetDefaultStrategy()
	}
	return &Executor{
		adapter:    adp,
		store:      store,
		productDir: productDir,
		strategy:   strategy,
	}
}

// NewWithCADRuntimeOrchestration creates an executor with an injected CAD-runtime
// executable resolver for package-scoped RunCADRuntime orchestration.
// Existing constructors remain usable for legacy plans; they fail closed when a
// CAD-runtime package is executed without this dependency.
func NewWithCADRuntimeOrchestration(adp adapter.Adapter, resolver runtimecap.ExecutableResolver, productDir string) *Executor {
	e := NewWithProductDir(adp, productDir)
	e.cadRuntime = &cadRuntimeExecutionDependencies{resolver: resolver}
	return e
}

func NewWithCADRuntimeConsumption(adp adapter.Adapter, resolver runtimecap.ExecutableResolver, productDir string) *Executor {
	e := NewWithCADRuntimeOrchestration(adp, resolver, productDir)
	e.cadRuntimeConsumption = true
	return e
}

func NewWithArtifactsAndCADRuntimeConsumption(adp adapter.Adapter, store artifact.Store, resolver runtimecap.ExecutableResolver, productDir string) *Executor {
	e := NewWithCADRuntimeConsumption(adp, resolver, productDir)
	e.store = store
	return e
}

// SetStrategy sets the cache key strategy for this executor.
func (e *Executor) SetStrategy(strategy cache.CacheKeyStrategy) {
	if strategy == nil {
		strategy = cache.GetDefaultStrategy()
	}
	e.strategy = strategy
}

// Strategy returns the current cache key strategy.
func (e *Executor) Strategy() cache.CacheKeyStrategy {
	if e.strategy == nil {
		e.strategy = cache.GetDefaultStrategy()
	}
	return e.strategy
}

// States returns a stable snapshot of the most recent step states.
func (e *Executor) States() []ExecutionState {
	if len(e.states) == 0 {
		return nil
	}
	out := make([]ExecutionState, len(e.states))
	for i, state := range e.states {
		out[i] = state.Clone()
	}
	return out
}

// Package returns a stable snapshot of the most recently executed handoff package.
func (e *Executor) Package() *handoff.Package {
	if e == nil || e.pkg == nil {
		return nil
	}
	return cloneHandoffPackage(e.pkg)
}

// ArtifactAcceptanceContext returns the executor-owned final acceptance state for artifact registration.
func (e *Executor) ArtifactAcceptanceContext() artifact.AcceptanceContext {
	if e == nil {
		return artifact.AcceptanceContext{}
	}
	return e.acceptance
}

// Execute runs all steps in order and propagates context unchanged.
func (e *Executor) Execute(ctx context.Context, plan *planner.ExecutionPlan) error {
	e.cadRuntimeOutcome = nil
	e.jobID = ""
	e.pkg = nil
	return e.execute(ctx, plan)
}

func (e *Executor) execute(ctx context.Context, plan *planner.ExecutionPlan) error {
	e.states = nil
	e.acceptance = artifact.AcceptanceContext{
		FinalOutcome:        artifact.FinalOutcomeUnknown,
		VerificationOutcome: artifact.VerificationOutcomeNotRun,
	}

	if plan == nil {
		return fmt.Errorf("execution plan is nil")
	}
	if e.store != nil {
		for i, step := range plan.Steps {
			if step.Type == planner.StepRunCADRuntime && i != len(plan.Steps)-1 {
				return fmt.Errorf("artifact-registering CAD runtime step must be final")
			}
		}
	}

	e.states = make([]ExecutionState, len(plan.Steps))
	for i, step := range plan.Steps {
		e.states[i] = ExecutionState{
			ProductID: job.ProductKeyFromStep(step),
			StepID:    strconv.Itoa(i),
			Status:    StepPending,
		}
	}

	var cad *cadRuntimeDispatch
	if planContainsCADRuntime(plan) {
		prepared, preflightErr := e.prepareCADRuntimeDispatch(plan)
		if preflightErr != nil {
			index, _, found := findSingleCADRuntimeStep(plan)
			if !found && index < 0 {
				for i, step := range plan.Steps {
					if step.Type == planner.StepRunCADRuntime {
						index = i
						break
					}
				}
			}
			if index < 0 {
				index = 0
			}
			return e.failCADRuntimePreflight(index, preflightErr)
		}
		cad = prepared
	}

	for i, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			e.failWithoutStart(i)
			timeout := errors.Is(err, context.DeadlineExceeded)
			e.acceptance.FinalOutcome = finalOutcomeFromExecutionFailure(timeout, !timeout)
			return &ExecutionError{
				ProductID:  e.states[i].ProductID,
				StepID:     e.states[i].StepID,
				Err:        err,
				RetryCount: 0,
				Timeout:    timeout,
				Canceled:   !timeout,
			}
		}

		e.states[i].Status = StepRunning
		e.states[i].StartedAt = time.Now()

		for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
			e.states[i].Attempts = attempt + 1
			e.states[i].Status = StepRunning

			stepCtx, cancel := context.WithTimeout(ctx, defaultStepTimeout)
			err := e.executeStep(stepCtx, step, i, attempt+1, cad)
			stepCtxErr := stepCtx.Err()
			cancel()

			if err == nil {
				e.finishState(i, StepSuccess)
				if e.store != nil {
					if regErr := e.registerArtifact(ctx, step, i); regErr != nil {
						return regErr
					}
				}
				break
			}

			timeout := errors.Is(err, context.DeadlineExceeded) || errors.Is(stepCtxErr, context.DeadlineExceeded)
			canceled := !timeout && (errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) || errors.Is(stepCtxErr, context.Canceled))
			exhausted := attempt == defaultMaxRetries || isTerminalCADRuntimeConsumptionError(err)
			if timeout || canceled || exhausted {
				e.finishState(i, StepFailed)
				e.acceptance.FinalOutcome = finalOutcomeFromExecutionFailure(timeout, canceled)
				return &ExecutionError{
					ProductID:  e.states[i].ProductID,
					StepID:     e.states[i].StepID,
					Err:        err,
					RetryCount: attempt + 1,
					Timeout:    timeout,
					Canceled:   canceled,
				}
			}
		}
	}

	e.acceptance.FinalOutcome = artifact.FinalOutcomeSucceeded
	return nil
}

func finalOutcomeFromExecutionFailure(timeout, canceled bool) artifact.FinalOutcome {
	if timeout {
		return artifact.FinalOutcomeTimedOut
	}
	if canceled {
		return artifact.FinalOutcomeCanceled
	}
	return artifact.FinalOutcomeFailed
}

// ExecutePackage runs a validated handoff package and preserves it for downstream inspection.
func (e *Executor) ExecutePackage(ctx context.Context, pkg *handoff.Package) error {
	e.cadRuntimeOutcome = nil
	if pkg == nil {
		e.states = nil
		e.pkg = nil
		e.jobID = ""
		return fmt.Errorf("handoff package is nil")
	}

	snapshot := cloneHandoffPackage(pkg)
	e.jobID = snapshot.JobID
	e.pkg = snapshot
	err := e.execute(ctx, snapshot.Plan())
	e.pkg = snapshot
	e.jobID = snapshot.JobID
	return err
}

func (e *Executor) finishState(index int, status StepStatus) {
	e.states[index].Status = status
	endedAt := time.Now()
	if !endedAt.After(e.states[index].StartedAt) {
		endedAt = e.states[index].StartedAt.Add(time.Nanosecond)
	}
	e.states[index].EndedAt = endedAt
}

func (e *Executor) failWithoutStart(index int) {
	e.states[index].Status = StepFailed
	e.states[index].EndedAt = time.Now()
}

// registerArtifact records a step's output file(s) in the artifact store.
// Called after successful step execution; errors propagate to the caller.
func (e *Executor) registerArtifact(ctx context.Context, step planner.Step, stepIndex int) error {
	productID := job.ProductKeyFromStep(step)
	stepID := strconv.Itoa(stepIndex)

	switch step.Type {
	case planner.StepWriteCSV:
		payload, ok := step.Payload.(planner.WriteCSVPayload)
		if !ok {
			return nil
		}
		absPath := filepath.Join(e.productDir, payload.Filename)
		_, err := e.store.RecordExisting(ctx, absPath, artifact.Artifact{
			Type:      artifact.ArtifactTypeCSV,
			Class:     artifact.ArtifactClassExecutionOutput,
			JobID:     e.jobID,
			ProductID: productID,
			StepID:    stepID,
			Filename:  payload.Filename,
		})
		return err

	case planner.StepWriteExportManifest:
		payload, ok := step.Payload.(planner.WriteExportManifestPayload)
		if !ok {
			return nil
		}
		absPath := filepath.Join(e.productDir, payload.ManifestFilename)
		_, err := e.store.RecordExisting(ctx, absPath, artifact.Artifact{
			Type:      artifact.ArtifactTypeJSON,
			Class:     artifact.ArtifactClassExecutionOutput,
			JobID:     e.jobID,
			ProductID: productID,
			StepID:    stepID,
			Filename:  payload.ManifestFilename,
		})
		return err

	case planner.StepRunCADRuntime:
		return e.registerCADRuntimeArtifacts(ctx, step, stepID)
	}

	return nil
}

type existingBatchStore interface {
	RecordExistingBatch(context.Context, []artifact.ExistingRecord) ([]artifact.Artifact, error)
}

func (e *Executor) registerCADRuntimeArtifacts(ctx context.Context, step planner.Step, stepID string) error {
	payload, ok := step.Payload.(planner.RunCADRuntimePayload)
	if !ok {
		return fmt.Errorf("invalid RunCADRuntime payload")
	}
	outcome := e.CADRuntimeOutcome()
	if outcome == nil || outcome.Verification != artifact.VerificationOutcomePassed {
		return fmt.Errorf("register aligned artifacts: explicit verification pass is required")
	}
	if outcome.JobID != e.jobID || outcome.ProductKey != payload.ProductKey || outcome.StepID != stepID ||
		outcome.Adapter != payload.Adapter || filepath.Base(outcome.ResultPath) != payload.ResultFilename {
		return fmt.Errorf("register aligned artifacts: runtime outcome identity mismatch")
	}
	records := []artifact.ExistingRecord{{
		AbsPath: outcome.ResultPath,
		Meta: artifact.Artifact{Type: artifact.ArtifactTypeJSON, Class: artifact.ArtifactClassExecutionOutput,
			JobID: e.jobID, ProductID: payload.ProductKey, StepID: stepID, Filename: payload.ResultFilename},
	}}
	for _, item := range outcome.Artifacts {
		typ, err := artifactTypeForOutput(item.Format)
		if err != nil {
			return err
		}
		for _, class := range []artifact.ArtifactClass{artifact.ArtifactClassExecutionOutput, artifact.ArtifactClassVerified} {
			records = append(records, artifact.ExistingRecord{
				AbsPath: item.Path,
				Meta: artifact.Artifact{Type: typ, Class: class, JobID: e.jobID,
					ProductID: payload.ProductKey, StepID: stepID, Filename: item.RelativePath},
			})
		}
	}
	for _, record := range records {
		info, err := os.Stat(record.AbsPath)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("register aligned artifact %q: file must exist and be regular", record.Meta.Filename)
		}
	}
	if batch, ok := e.store.(existingBatchStore); ok {
		_, err := batch.RecordExistingBatch(ctx, records)
		return err
	}
	for _, record := range records {
		if _, err := e.store.RecordExisting(ctx, record.AbsPath, record.Meta); err != nil {
			return err
		}
	}
	return nil
}

func cloneHandoffPackage(pkg *handoff.Package) *handoff.Package {
	if pkg == nil {
		return nil
	}

	steps := make([]handoff.StepSnapshot, 0, len(pkg.Steps))
	for _, step := range pkg.Steps {
		steps = append(steps, handoff.StepSnapshot{
			Type:       step.Type,
			CSV:        cloneWriteCSVPtr(step.CSV),
			Manifest:   cloneManifestPtr(step.Manifest),
			CADRuntime: cloneCADRuntimePtr(step.CADRuntime),
		})
	}

	return &handoff.Package{
		JobID:      pkg.JobID,
		ProductKey: pkg.ProductKey,
		CSV:        cloneWriteCSVPayload(pkg.CSV),
		Manifest:   cloneManifestPayload(pkg.Manifest),
		CADRuntime: cloneCADRuntimePtr(pkg.CADRuntime),
		Tables:     handoff.CloneTableInputs(pkg.Tables),
		Steps:      steps,
	}
}

func cloneWriteCSVPtr(payload *planner.WriteCSVPayload) *planner.WriteCSVPayload {
	if payload == nil {
		return nil
	}
	cloned := cloneWriteCSVPayload(*payload)
	return &cloned
}

func cloneManifestPtr(payload *planner.WriteExportManifestPayload) *planner.WriteExportManifestPayload {
	if payload == nil {
		return nil
	}
	cloned := cloneManifestPayload(*payload)
	return &cloned
}

func cloneWriteCSVPayload(payload planner.WriteCSVPayload) planner.WriteCSVPayload {
	return planner.WriteCSVPayload{
		ProductKey: payload.ProductKey,
		Filename:   payload.Filename,
		Headers:    append([]string(nil), payload.Headers...),
		Values:     cloneInterfaceSlice(payload.Values),
	}
}

func cloneManifestPayload(payload planner.WriteExportManifestPayload) planner.WriteExportManifestPayload {
	return planner.WriteExportManifestPayload{
		ProductKey:             payload.ProductKey,
		ManifestFilename:       payload.ManifestFilename,
		ManifestProjectionMode: payload.ManifestProjectionMode,
		SchemaVersion:          payload.SchemaVersion,
		PlanHash:               payload.PlanHash,
		Adapter:                payload.Adapter,
		Product:                payload.Product,
		SourceDocument:         payload.SourceDocument,
		Inputs:                 payload.Inputs,
		Values:                 cloneStringAnyMap(payload.Values),
		ParameterAssignments:   append([]planner.ExportManifestParameterAssignment(nil), payload.ParameterAssignments...),
		AssemblyMutations:      cloneMutationCollection(payload.AssemblyMutations),
		PartMutations:          cloneMutationCollection(payload.PartMutations),
		Outputs:                append([]planner.ExportManifestOutput(nil), payload.Outputs...),
		Verification: planner.VerificationManifestIntent{
			ExpectedParameters:        append([]planner.VerificationExpectedParameter(nil), payload.Verification.ExpectedParameters...),
			ObservationParameterLinks: append([]planner.VerificationObservationParameterLink(nil), payload.Verification.ObservationParameterLinks...),
		},
	}
}

func cloneMutationCollection(in *planner.ExportManifestMutationCollection) *planner.ExportManifestMutationCollection {
	if in == nil {
		return nil
	}

	out := &planner.ExportManifestMutationCollection{
		Parameters:  append([]planner.ExportManifestParameterMutation(nil), in.Parameters...),
		Suppression: append([]planner.ExportManifestSuppressionMutation(nil), in.Suppression...),
	}
	if in.Properties != nil {
		out.Properties = make([]planner.ExportManifestPropertyMutation, len(in.Properties))
		for i, property := range in.Properties {
			out.Properties[i] = planner.ExportManifestPropertyMutation{
				Object:   property.Object,
				Property: property.Property,
				Value:    cloneInterfaceValue(property.Value),
			}
		}
	}
	return out
}

func cloneStringAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = cloneInterfaceValue(value)
	}
	return out
}

func cloneInterfaceSlice(in []interface{}) []interface{} {
	if in == nil {
		return nil
	}
	out := make([]interface{}, len(in))
	for i, value := range in {
		out[i] = cloneInterfaceValue(value)
	}
	return out
}

func cloneInterfaceValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return cloneStringAnyMap(v)
	case []interface{}:
		return cloneInterfaceSlice(v)
	default:
		return v
	}
}

func artifactTypeForOutput(outputType string) (artifact.ArtifactType, error) {
	switch artifact.NormalizeExportOutputType(outputType) {
	case artifact.ExportOutputTypeSTEP:
		return artifact.ArtifactTypeSTEP, nil
	case artifact.ExportOutputTypeCSV:
		return artifact.ArtifactTypeCSV, nil
	case artifact.ExportOutputTypePDF:
		return artifact.ArtifactTypePDF, nil
	default:
		return artifact.ArtifactTypeUnknown, fmt.Errorf("register runner artifact: unsupported manifest output type %q", outputType)
	}
}

// CreateKeyContext creates a KeyContext for the given step index.
// This can be used for cache key generation with the executor's strategy.
func (e *Executor) CreateKeyContext(plan *planner.ExecutionPlan, stepIndex int) (cache.KeyContext, error) {
	if plan == nil {
		return cache.KeyContext{}, fmt.Errorf("execution plan is nil")
	}
	if stepIndex < 0 || stepIndex >= len(plan.Steps) {
		return cache.KeyContext{}, fmt.Errorf("step index %d out of range [0, %d)", stepIndex, len(plan.Steps))
	}

	step := plan.Steps[stepIndex]
	productID := job.ProductKeyFromStep(step)
	stepID := strconv.Itoa(stepIndex)

	// Use the strategy to get the current plan hash from context if needed.
	// For now, we create a basic context that can be extended.
	ctx := cache.NewKeyContext("").
		WithProductID(productID).
		WithStepID(stepID).
		Build()

	return ctx, nil
}

// ComputeCacheKey computes a cache key for the given layer and context using the executor's strategy.
func (e *Executor) ComputeCacheKey(layer cache.CacheLayer, ctx cache.KeyContext) string {
	return e.Strategy().ComputeKey(layer, ctx)
}
