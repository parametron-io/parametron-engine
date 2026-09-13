package freecad

import (
	"context"
	"errors"
	"fmt"

	"parametron/internal/engine/runtimecap"
)

const (
	FreeCADRuntimeExecutionErrorInvalid   = runtimecap.ErrorInvalid
	FreeCADRuntimeExecutionErrorStart     = runtimecap.ErrorStart
	FreeCADRuntimeExecutionErrorExit      = runtimecap.ErrorExit
	FreeCADRuntimeExecutionErrorCancelled = runtimecap.ErrorCancelled
)

// FreeCADRuntimeExecutionRequest describes the explicit file-based CLI contract
// used to invoke the external parametron-freecad runtime.
type FreeCADRuntimeExecutionRequest struct {
	RuntimeCommand                string
	WorkingCopyDir                string
	ManifestPath                  string
	ResultPath                    string
	OutputDir                     string
	ObservationRequestPath        string
	ReferenceTraversalRequestPath string
}

type FreeCADRuntimeCommand struct {
	Path string
	Args []string
}

type FreeCADRuntimeExecutionResult struct {
	Command FreeCADRuntimeCommand
	Stdout  string
	Stderr  string
}

type FreeCADRuntimeExecutionError struct {
	Kind     string
	Message  string
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

func (e *FreeCADRuntimeExecutionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *FreeCADRuntimeExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func buildFreeCADRuntimeExecutionCommand(req FreeCADRuntimeExecutionRequest) (FreeCADRuntimeCommand, error) {
	command, err := runtimecap.BuildCommand(freeCADRuntimeExecutionRequestToRuntimecap(req))
	if err != nil {
		return FreeCADRuntimeCommand{}, err
	}
	return freeCADRuntimeCommandFromRuntimecap(command), nil
}

func executeFreeCADRuntime(ctx context.Context, req FreeCADRuntimeExecutionRequest) (*FreeCADRuntimeExecutionResult, error) {
	return invokeFreeCADRuntime(runtimecap.NewExternalCapability(), ctx, req)
}

// InvokeFreeCADRuntime invokes the aligned FreeCAD runtime through the supplied
// Engine runtime capability. A nil capability uses the external capability.
func InvokeFreeCADRuntime(cap runtimecap.Capability, ctx context.Context, req FreeCADRuntimeExecutionRequest) (*FreeCADRuntimeExecutionResult, error) {
	return invokeFreeCADRuntime(cap, ctx, req)
}

func invokeFreeCADRuntime(cap runtimecap.Capability, ctx context.Context, req FreeCADRuntimeExecutionRequest) (*FreeCADRuntimeExecutionResult, error) {
	if cap == nil {
		cap = runtimecap.NewExternalCapability()
	}

	result, err := cap.Invoke(ctx, freeCADRuntimeExecutionRequestToRuntimecap(req))
	if err == nil {
		return freeCADRuntimeExecutionResultFromRuntimecap(result), nil
	}

	var runtimeErr *runtimecap.Error
	if !errors.As(err, &runtimeErr) {
		return freeCADRuntimeExecutionResultFromRuntimecap(result), err
	}

	freeCADResult := freeCADRuntimeExecutionResultFromRuntimecap(result)
	freeCADErr := freeCADRuntimeExecutionErrorFromRuntimecap(runtimeErr)
	if freeCADErr.Kind == FreeCADRuntimeExecutionErrorInvalid && freeCADResult == nil {
		return nil, freeCADErr
	}
	return freeCADResult, freeCADErr
}

func freeCADRuntimeExecutionRequestToRuntimecap(req FreeCADRuntimeExecutionRequest) runtimecap.Request {
	return runtimecap.Request{
		RuntimeCommand:                req.RuntimeCommand,
		WorkingCopyDir:                req.WorkingCopyDir,
		ManifestPath:                  req.ManifestPath,
		ResultPath:                    req.ResultPath,
		OutputDir:                     req.OutputDir,
		ObservationRequestPath:        req.ObservationRequestPath,
		ReferenceTraversalRequestPath: req.ReferenceTraversalRequestPath,
	}
}

func freeCADRuntimeCommandFromRuntimecap(command runtimecap.Command) FreeCADRuntimeCommand {
	return FreeCADRuntimeCommand{
		Path: command.Path,
		Args: command.Args,
	}
}

func freeCADRuntimeExecutionResultFromRuntimecap(result *runtimecap.Result) *FreeCADRuntimeExecutionResult {
	if result == nil {
		return nil
	}
	return &FreeCADRuntimeExecutionResult{
		Command: freeCADRuntimeCommandFromRuntimecap(result.Command),
		Stdout:  result.Stdout,
		Stderr:  result.Stderr,
	}
}

func freeCADRuntimeExecutionErrorFromRuntimecap(err *runtimecap.Error) *FreeCADRuntimeExecutionError {
	if err == nil {
		return nil
	}

	message := err.Message
	switch err.Kind {
	case FreeCADRuntimeExecutionErrorInvalid:
		message = "invalid FreeCAD runtime execution request"
	case FreeCADRuntimeExecutionErrorCancelled:
		message = "FreeCAD runtime execution cancelled"
	case FreeCADRuntimeExecutionErrorExit:
		message = "FreeCAD runtime execution failed"
	case FreeCADRuntimeExecutionErrorStart:
		message = "failed to start FreeCAD runtime execution"
	}

	return &FreeCADRuntimeExecutionError{
		Kind:     err.Kind,
		Message:  message,
		ExitCode: err.ExitCode,
		Stdout:   err.Stdout,
		Stderr:   err.Stderr,
		Err:      err.Err,
	}
}

func freeCADRuntimeExecutionEnv(env []string) []string {
	return runtimecap.FilterLegacyEnvVars(env)
}
