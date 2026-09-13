package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/jobstatus"
	"parametron/internal/engine/report"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/scheduler"
	"parametron/internal/shared/config"
)

const defaultQueuePollInterval = 25 * time.Millisecond
const defaultRuntimeWorkerCount = 1

var completionArtifactRecordsFunc = completionArtifactRecords

type packageExecutor interface {
	ExecutePackage(ctx context.Context, pkg *handoff.Package) error
}

type executionStateSnapshotter interface {
	States() []executor.ExecutionState
}

type artifactAcceptanceContextProvider interface {
	ArtifactAcceptanceContext() artifact.AcceptanceContext
}

type cadRuntimeOutcomeProvider interface {
	CADRuntimeOutcome() *executor.CADRuntimeOutcome
}

type handlerRuntime interface {
	http.Handler
	Start(context.Context)
	Wait()
}

type runtimeAwareHandler struct {
	base   http.Handler
	runner *jobRunner
}

func (h *runtimeAwareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.base.ServeHTTP(w, r)
}

func (h *runtimeAwareHandler) Start(ctx context.Context) {
	if h == nil || h.runner == nil {
		return
	}
	h.runner.Start(ctx)
}

func (h *runtimeAwareHandler) Wait() {
	if h == nil || h.runner == nil {
		return
	}
	h.runner.Wait()
}

type jobRunner struct {
	store           SubmissionStore
	artifactStore   artifact.Store
	runRoot         string
	queue           JobQueue
	executorFactory func(*handoff.Package) packageExecutor
	workerCount     int
	pollInterval    time.Duration
	now             func() time.Time

	startOnce sync.Once
	done      chan struct{}
	workers   sync.WaitGroup
}

func newJobRunner(store SubmissionStore, artifactStore artifact.Store, runRoot string, queue JobQueue, factory func(*handoff.Package) packageExecutor, workerCount int, pollInterval time.Duration) *jobRunner {
	if store == nil || queue == nil || factory == nil {
		return nil
	}
	if workerCount <= 0 {
		workerCount = defaultRuntimeWorkerCount
	}
	if pollInterval <= 0 {
		pollInterval = defaultQueuePollInterval
	}

	return &jobRunner{
		store:           store,
		artifactStore:   artifactStore,
		runRoot:         runRoot,
		queue:           queue,
		executorFactory: factory,
		workerCount:     workerCount,
		pollInterval:    pollInterval,
		now:             func() time.Time { return time.Now().UTC() },
		done:            make(chan struct{}),
	}
}

func (r *jobRunner) Start(ctx context.Context) {
	if r == nil {
		return
	}

	r.startOnce.Do(func() {
		r.workers.Add(r.workerCount)
		go func() {
			defer close(r.done)
			r.workers.Wait()
		}()
		for i := 0; i < r.workerCount; i++ {
			go func() {
				defer r.workers.Done()
				r.runWorker(ctx)
			}()
		}
	})
}

func (r *jobRunner) Wait() {
	if r == nil || r.done == nil {
		return
	}
	<-r.done
}

func (r *jobRunner) runWorker(ctx context.Context) {
	if queue, ok := r.queue.(blockingJobQueue); ok {
		r.runBlocking(ctx, queue)
		return
	}

	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		pkg, ok := r.queue.Dequeue()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}

		r.executeJob(ctx, pkg)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (r *jobRunner) runBlocking(ctx context.Context, queue blockingJobQueue) {
	for {
		pkg, ok := queue.WaitDequeue(ctx)
		if !ok {
			return
		}

		r.executeJob(ctx, pkg)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (r *jobRunner) executeJob(ctx context.Context, pkg *handoff.Package) {
	if pkg == nil {
		return
	}

	startedAt := r.now()
	started := false
	if err := r.store.UpdateStatus(pkg.JobID, func(status *jobstatus.Status) error {
		if status == nil || status.State != jobstatus.StateQueued {
			return nil
		}
		if err := status.Start(startedAt); err != nil {
			return err
		}
		started = true
		return nil
	}); err != nil || !started {
		return
	}

	exec := r.executorFactory(pkg)
	if exec == nil {
		_ = r.store.UpdateStatus(pkg.JobID, func(status *jobstatus.Status) error {
			if status == nil || status.State != jobstatus.StateRunning {
				return nil
			}
			return status.Fail(r.now(), errors.New("job executor is not configured"))
		})
		return
	}

	err := exec.ExecutePackage(ctx, pkg)
	if err == nil {
		if regErr := r.registerCompletionArtifacts(ctx, pkg, exec); regErr != nil {
			err = regErr
		}
	}
	endedAt := r.now()
	_ = r.writeJobReport(pkg, exec, startedAt, endedAt, err)
	_ = r.store.UpdateStatus(pkg.JobID, func(status *jobstatus.Status) error {
		if status == nil || status.State != jobstatus.StateRunning {
			return nil
		}
		if err == nil {
			return status.Succeed(endedAt)
		}
		if shouldCancelJob(ctx, err) {
			return status.Cancel(endedAt, err)
		}
		return status.Fail(endedAt, err)
	})
}

func (r *jobRunner) registerCompletionArtifacts(ctx context.Context, pkg *handoff.Package, exec packageExecutor) error {
	recordStore, ok := r.artifactStore.(completionArtifactStore)
	if !ok || recordStore == nil || pkg == nil {
		return nil
	}

	records, err := completionArtifactRecordsFunc(runtimeProductDir(r.runRoot, pkg), pkg)
	if err != nil {
		return err
	}
	if provider, ok := exec.(cadRuntimeOutcomeProvider); ok && provider != nil {
		aligned, alignedErr := alignedCompletionArtifactRecords(pkg, provider.CADRuntimeOutcome())
		if alignedErr != nil {
			return alignedErr
		}
		records = append(records, aligned...)
	}
	acceptanceCtx := completionArtifactAcceptanceContext(exec)
	sortCompletionArtifactRecords(records)
	batch, err := acceptedCompletionArtifactBatch(records, acceptanceCtx)
	if err != nil {
		var batchErr *artifact.BatchAcceptanceError
		if errors.As(err, &batchErr) && batchErr != nil && batchErr.Index >= 0 && batchErr.Index < len(records) {
			return fmt.Errorf("register completion artifact %q: %w", records[batchErr.Index].meta.Filename, batchErr.Err)
		}
		return fmt.Errorf("register completion artifacts: %w", err)
	}

	for _, record := range records {
		if _, err := os.Stat(record.absPath); err != nil {
			return fmt.Errorf("register completion artifact %q: %w", record.meta.Filename, err)
		}
	}

	if batchStore, ok := recordStore.(completionArtifactBatchStore); ok && batchStore != nil {
		if _, err := batchStore.RecordExistingBatch(ctx, batch); err != nil {
			var batchErr *artifact.RecordExistingBatchError
			if errors.As(err, &batchErr) && batchErr != nil && batchErr.Index >= 0 && batchErr.Index < len(records) {
				return fmt.Errorf("register completion artifact %q: %w", records[batchErr.Index].meta.Filename, batchErr.Err)
			}
			return fmt.Errorf("register completion artifacts: %w", err)
		}
		return nil
	}

	for _, record := range batch {
		if _, err := recordStore.RecordExisting(ctx, record.AbsPath, record.Meta); err != nil {
			return fmt.Errorf("register completion artifact %q: %w", record.Meta.Filename, err)
		}
	}

	return nil
}

func completionArtifactAcceptanceContext(exec packageExecutor) artifact.AcceptanceContext {
	ctx := artifact.AcceptanceContext{
		FinalOutcome:        artifact.FinalOutcomeSucceeded,
		VerificationOutcome: artifact.VerificationOutcomeUnknown,
	}
	provider, ok := exec.(artifactAcceptanceContextProvider)
	if !ok || provider == nil {
		return ctx
	}

	provided := provider.ArtifactAcceptanceContext()
	if provided.FinalOutcome != "" {
		ctx.FinalOutcome = provided.FinalOutcome
	}
	if provided.VerificationOutcome != "" {
		ctx.VerificationOutcome = provided.VerificationOutcome
	}
	return ctx
}

func acceptedCompletionArtifactBatch(records []completionArtifactRecord, ctx artifact.AcceptanceContext) ([]artifact.ExistingRecord, error) {
	items := make([]artifact.Artifact, 0, len(records))
	batch := make([]artifact.ExistingRecord, 0, len(records))
	for _, record := range records {
		items = append(items, record.meta)
		batch = append(batch, artifact.ExistingRecord{
			AbsPath: record.absPath,
			Meta:    record.meta,
		})
	}

	if err := artifact.AcceptBatchForRegistration(items, ctx); err != nil {
		return nil, err
	}
	return batch, nil
}

func shouldCancelJob(ctx context.Context, err error) bool {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return true
	}

	var executionErr *executor.ExecutionError
	return errors.As(err, &executionErr) && executionErr.Canceled
}

func (r *jobRunner) writeJobReport(pkg *handoff.Package, exec packageExecutor, startedAt, endedAt time.Time, err error) error {
	if pkg == nil {
		return fmt.Errorf("write runtime report: handoff package is nil")
	}

	reportJob, buildErr := job.FromPlan(pkg.Plan())
	if buildErr != nil {
		return fmt.Errorf("write runtime report: build job: %w", buildErr)
	}

	artifacts, listErr := r.reportArtifacts(pkg)
	if listErr != nil {
		return fmt.Errorf("write runtime report: list artifacts: %w", listErr)
	}

	runReport, reportErr := report.Build(report.BuildInput{
		PlanHash: pkg.Manifest.PlanHash,
		Execution: scheduler.ExecutionResult{
			Jobs: []scheduler.JobExecution{{
				ProductIndex: 0,
				Job:          reportJob,
				States:       executionStates(exec),
				Err:          err,
			}},
		},
		Artifacts:          artifacts,
		StartedAt:          startedAt,
		EndedAt:            endedAt,
		WorkerCount:        r.workerCount,
		StepTimeoutSeconds: int(executor.DefaultStepTimeout / time.Second),
		MaxRetries:         executor.DefaultMaxRetries,
		Err:                err,
	})
	if reportErr != nil {
		return fmt.Errorf("write runtime report: build report: %w", reportErr)
	}

	if err := report.Write(runtimeProductDir(r.runRoot, pkg), runReport); err != nil {
		return fmt.Errorf("write runtime report: %w", err)
	}

	return nil
}

func (r *jobRunner) reportArtifacts(pkg *handoff.Package) ([]artifact.Artifact, error) {
	if pkg == nil || r.artifactStore == nil {
		return nil, nil
	}
	return listJobArtifacts(r.artifactStore, pkg.JobID, pkg.ProductKey)
}

func executionStates(exec packageExecutor) []executor.ExecutionState {
	snapshotter, ok := exec.(executionStateSnapshotter)
	if !ok || snapshotter == nil {
		return nil
	}
	return snapshotter.States()
}

type completionArtifactRecord struct {
	absPath string
	meta    artifact.Artifact
}

type completionArtifactStore interface {
	RecordExisting(ctx context.Context, absPath string, meta artifact.Artifact) (artifact.Artifact, error)
}

type completionArtifactBatchStore interface {
	RecordExistingBatch(ctx context.Context, records []artifact.ExistingRecord) ([]artifact.Artifact, error)
}

type artifactBaseDirStore interface {
	BaseDir() string
}

func resolveRuntimeRoot(store artifact.Store, runRoot string) (string, error) {
	root := runRoot
	if filepath.Clean(root) == "." || root == "" {
		root = ""
	}

	if root == "" {
		if baseDirStore, ok := store.(artifactBaseDirStore); ok {
			root = baseDirStore.BaseDir()
		} else {
			tempRoot, err := os.MkdirTemp("", "parametron-engine-api-*")
			if err != nil {
				return "", err
			}
			root = tempRoot
		}
	}

	return root, nil
}

func defaultExecutorFactory(store artifact.Store, runRoot string) (func(*handoff.Package) packageExecutor, error) {
	root, err := resolveRuntimeRoot(store, runRoot)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	resolver, err := runtimecap.NewExecutableResolver(cfg.CADRuntimeCommands())
	if err != nil {
		return nil, err
	}
	capability := runtimecap.NewExternalCapability()

	return func(pkg *handoff.Package) packageExecutor {
		if pkg == nil {
			return nil
		}

		productDir := runtimeProductDir(root, pkg)
		adp := freecad.NewFreeCADAdapter(productDir)
		aligned, wrapErr := cadruntime.NewFreeCADRuntimeAdapter(adp, capability)
		if wrapErr != nil {
			panic(wrapErr)
		}
		return executor.NewWithCADRuntimeConsumption(aligned, resolver, productDir)
	}, nil
}

func runtimeProductDir(runRoot string, pkg *handoff.Package) string {
	if pkg == nil {
		return ""
	}
	return filepath.Join(runRoot, "jobs", pkg.JobID)
}

func completionArtifactRecords(productDir string, pkg *handoff.Package) ([]completionArtifactRecord, error) {
	if pkg == nil {
		return nil, fmt.Errorf("register completion artifacts: handoff package is nil")
	}

	records := make([]completionArtifactRecord, 0, 2+len(pkg.Manifest.Outputs))
	for index, step := range pkg.Steps {
		stepID := fmt.Sprintf("%d", index)

		switch step.Type {
		case planner.StepWriteCSV:
			if step.CSV == nil {
				continue
			}
			records = append(records, completionArtifactRecord{
				absPath: filepath.Join(productDir, step.CSV.Filename),
				meta: artifact.Artifact{
					Type:      artifact.ArtifactTypeCSV,
					Class:     artifact.ArtifactClassExecutionOutput,
					JobID:     pkg.JobID,
					ProductID: pkg.ProductKey,
					StepID:    stepID,
					Filename:  step.CSV.Filename,
				},
			})

		case planner.StepWriteExportManifest:
			if step.Manifest == nil {
				continue
			}
			records = append(records, completionArtifactRecord{
				absPath: filepath.Join(productDir, step.Manifest.ManifestFilename),
				meta: artifact.Artifact{
					Type:      artifact.ArtifactTypeJSON,
					Class:     artifact.ArtifactClassExecutionOutput,
					JobID:     pkg.JobID,
					ProductID: pkg.ProductKey,
					StepID:    stepID,
					Filename:  step.Manifest.ManifestFilename,
				},
			})

		}
	}

	sortCompletionArtifactRecords(records)
	return records, nil
}

func alignedCompletionArtifactRecords(pkg *handoff.Package, outcome *executor.CADRuntimeOutcome) ([]completionArtifactRecord, error) {
	if pkg == nil || outcome == nil {
		return nil, nil
	}
	if outcome.Verification != artifact.VerificationOutcomePassed || pkg.CADRuntime == nil {
		return nil, nil
	}
	if filepath.Base(outcome.ResultPath) != pkg.CADRuntime.ResultFilename {
		return nil, fmt.Errorf("register aligned completion artifacts: result filename mismatch")
	}
	records := []completionArtifactRecord{{
		absPath: outcome.ResultPath,
		meta: artifact.Artifact{
			Type: artifact.ArtifactTypeJSON, Class: artifact.ArtifactClassExecutionOutput,
			JobID: pkg.JobID, ProductID: pkg.ProductKey, StepID: outcome.StepID,
			Filename: pkg.CADRuntime.ResultFilename,
		},
	}}
	for _, item := range outcome.Artifacts {
		typ, err := runtimeArtifactTypeForOutput(item.Format)
		if err != nil {
			return nil, err
		}
		for _, class := range []artifact.ArtifactClass{artifact.ArtifactClassExecutionOutput, artifact.ArtifactClassVerified} {
			records = append(records, completionArtifactRecord{
				absPath: item.Path,
				meta: artifact.Artifact{
					Type: typ, Class: class, JobID: pkg.JobID, ProductID: pkg.ProductKey,
					StepID: outcome.StepID, Filename: item.RelativePath,
				},
			})
		}
	}
	return records, nil
}

func sortCompletionArtifactRecords(records []completionArtifactRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].meta.JobID != records[j].meta.JobID {
			return records[i].meta.JobID < records[j].meta.JobID
		}
		if records[i].meta.ProductID != records[j].meta.ProductID {
			return records[i].meta.ProductID < records[j].meta.ProductID
		}
		if records[i].meta.StepID != records[j].meta.StepID {
			return records[i].meta.StepID < records[j].meta.StepID
		}
		if records[i].meta.Class != records[j].meta.Class {
			return records[i].meta.Class < records[j].meta.Class
		}
		if records[i].meta.Filename != records[j].meta.Filename {
			return records[i].meta.Filename < records[j].meta.Filename
		}
		if records[i].meta.Type != records[j].meta.Type {
			return records[i].meta.Type < records[j].meta.Type
		}
		return records[i].absPath < records[j].absPath
	})
}

func runtimeArtifactTypeForOutput(outputType string) (artifact.ArtifactType, error) {
	switch artifact.NormalizeExportOutputType(outputType) {
	case artifact.ExportOutputTypeSTEP:
		return artifact.ArtifactTypeSTEP, nil
	case artifact.ExportOutputTypeCSV:
		return artifact.ArtifactTypeCSV, nil
	case artifact.ExportOutputTypePDF:
		return artifact.ArtifactTypePDF, nil
	default:
		return artifact.ArtifactTypeUnknown, fmt.Errorf("register completion artifact: unsupported manifest output type %q", outputType)
	}
}
