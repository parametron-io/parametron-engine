package scheduler

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
)

// Scheduler orchestrates plan execution through the runtime layer.
type Scheduler struct {
	runtime        *executor.Executor
	runRoot        string
	adapterFactory func(productDir string) *executor.Executor
}

// ExecutionResult captures deterministic per-job execution state in scheduler order.
type ExecutionResult struct {
	Jobs []JobExecution
}

// JobExecution captures one job's runtime state and any job-local error.
type JobExecution struct {
	ProductIndex      int
	Job               *job.Job
	States            []executor.ExecutionState
	Err               error
	CADRuntimeOutcome *executor.CADRuntimeOutcome
}

// New creates a Scheduler backed by a single Executor for all products.
// This is the standard constructor and preserves existing behavior.
func New(rt *executor.Executor) *Scheduler {
	return &Scheduler{runtime: rt}
}

// NewWithProductDirs creates a Scheduler that provisions a dedicated Executor
// per product, rooted at the product's subdirectory within runRoot.
// factory receives the fully resolved productDir and must return a configured Executor.
func NewWithProductDirs(runRoot string, factory func(productDir string) *executor.Executor) *Scheduler {
	return &Scheduler{
		runRoot:        runRoot,
		adapterFactory: factory,
	}
}

// Execute is the execution entrypoint for plan processing.
func (s *Scheduler) Execute(ctx context.Context, plan *planner.ExecutionPlan) error {
	_, err := s.ExecuteWithResult(ctx, plan)
	if err != nil {
		return err
	}
	return nil
}

// ExecuteWithResult runs the plan and returns stable per-job runtime snapshots.
func (s *Scheduler) ExecuteWithResult(ctx context.Context, plan *planner.ExecutionPlan) (ExecutionResult, error) {
	jobsByProduct, err := job.SplitPlan(plan)
	if err != nil {
		return ExecutionResult{}, err
	}

	if len(jobsByProduct) == 0 {
		rt := s.resolveAnonymousExecutor(0)
		execCopy := *rt
		err := execCopy.Execute(ctx, plan)
		return ExecutionResult{}, err
	}

	workerCount := EffectiveWorkerCount(plan)
	result := ExecutionResult{
		Jobs: make([]JobExecution, len(jobsByProduct)),
	}
	for i, scheduledJob := range jobsByProduct {
		result.Jobs[i] = JobExecution{
			ProductIndex: i,
			Job:          scheduledJob,
		}
	}

	type executeJob struct {
		productIndex int
		job          *job.Job
	}

	type executeResult struct {
		productIndex      int
		states            []executor.ExecutionState
		err               error
		cadRuntimeOutcome *executor.CADRuntimeOutcome
	}

	jobs := make(chan executeJob)
	results := make(chan executeResult, len(jobsByProduct))

	var workers sync.WaitGroup
	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer workers.Done()
			for scheduledJob := range jobs {
				rt := s.resolveExecutor(scheduledJob.job, scheduledJob.productIndex)
				execCopy := *rt
				var err error
				if jobRequiresCADRuntime(scheduledJob.job) {
					var pkg *handoff.Package
					pkg, err = handoff.FromJob(scheduledJob.job)
					if err == nil {
						err = execCopy.ExecutePackage(ctx, pkg)
					}
				} else {
					err = execCopy.Execute(ctx, scheduledJob.job.Plan())
				}
				results <- executeResult{
					productIndex:      scheduledJob.productIndex,
					states:            execCopy.States(),
					err:               err,
					cadRuntimeOutcome: execCopy.CADRuntimeOutcome(),
				}
			}
		}()
	}

	scheduled := 0
scheduleLoop:
	for i, productJob := range jobsByProduct {
		select {
		case <-ctx.Done():
			break scheduleLoop
		case jobs <- executeJob{productIndex: i, job: productJob}:
			scheduled++
		}
	}
	close(jobs)

	go func() {
		workers.Wait()
		close(results)
	}()

	executed := make([]bool, len(jobsByProduct))
	orderedResults := make([]error, len(jobsByProduct))
	for jobResult := range results {
		executed[jobResult.productIndex] = true
		orderedResults[jobResult.productIndex] = jobResult.err
		if jobResult.states != nil {
			result.Jobs[jobResult.productIndex].States = jobResult.states
		}
		result.Jobs[jobResult.productIndex].Err = jobResult.err
		result.Jobs[jobResult.productIndex].CADRuntimeOutcome = jobResult.cadRuntimeOutcome
	}

	for i := 0; i < len(orderedResults); i++ {
		if executed[i] && orderedResults[i] != nil {
			return result, orderedResults[i]
		}
		if !executed[i] && scheduled < len(jobsByProduct) {
			if err := ctx.Err(); err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

func jobRequiresCADRuntime(j *job.Job) bool {
	if j == nil {
		return false
	}
	for _, step := range j.Steps() {
		if step.Type == planner.StepRunCADRuntime {
			return true
		}
	}
	return false
}

// EffectiveWorkerCount returns the bounded worker count used by scheduler execution.
func EffectiveWorkerCount(plan *planner.ExecutionPlan) int {
	jobsByProduct, err := job.SplitPlan(plan)
	if err != nil || len(jobsByProduct) == 0 {
		return 1
	}

	workerCount := runtime.NumCPU()
	// NOTE: Future improvement:
	// Worker count may be limited by memory constraints or config layer.
	if workerCount > len(jobsByProduct) {
		workerCount = len(jobsByProduct)
	}
	return workerCount
}

// resolveExecutor returns the executor to use for a given product plan.
// When adapterFactory is set, a new executor is provisioned per product dir.
// Otherwise the shared runtime executor is returned.
func (s *Scheduler) resolveExecutor(j *job.Job, productIndex int) *executor.Executor {
	if s.adapterFactory == nil {
		return s.runtime
	}
	productKey := fmt.Sprintf("product_%d", productIndex)
	if j != nil && j.ProductKey != "" {
		productKey = j.ProductKey
	}
	productDir := planner.BuildProductDir(s.runRoot, productKey)
	return s.adapterFactory(productDir)
}

func (s *Scheduler) resolveAnonymousExecutor(productIndex int) *executor.Executor {
	return s.resolveExecutor(nil, productIndex)
}
