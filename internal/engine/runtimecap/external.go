package runtimecap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExternalCapability invokes an external runtime through the aligned file-based CLI contract.
type ExternalCapability struct{}

// NewExternalCapability returns the default Engine-owned external runtime capability.
func NewExternalCapability() Capability {
	return ExternalCapability{}
}

// BuildCommand resolves the aligned external runtime CLI command for a request.
func BuildCommand(req Request) (Command, error) {
	normalized, err := NormalizeRequest(req)
	if err != nil {
		return Command{}, err
	}

	args := []string{
		"execute",
		"--working-copy", normalized.WorkingCopyDir,
		"--manifest", normalized.ManifestPath,
		"--result", normalized.ResultPath,
	}
	if normalized.OutputDir != "" {
		args = append(args, "--output-dir", normalized.OutputDir)
	}
	if normalized.ObservationRequestPath != "" {
		args = append(args, "--observation-request", normalized.ObservationRequestPath)
	}
	if normalized.ReferenceTraversalRequestPath != "" {
		args = append(args, "--reference-traversal-request", normalized.ReferenceTraversalRequestPath)
	}

	return Command{
		Path: normalized.RuntimeCommand,
		Args: args,
	}, nil
}

// NormalizeRequest validates and normalizes an external runtime invocation request.
func NormalizeRequest(req Request) (Request, error) {
	normalized := Request{
		RuntimeCommand:                strings.TrimSpace(req.RuntimeCommand),
		WorkingCopyDir:                strings.TrimSpace(req.WorkingCopyDir),
		ManifestPath:                  strings.TrimSpace(req.ManifestPath),
		ResultPath:                    strings.TrimSpace(req.ResultPath),
		OutputDir:                     strings.TrimSpace(req.OutputDir),
		ObservationRequestPath:        strings.TrimSpace(req.ObservationRequestPath),
		ReferenceTraversalRequestPath: strings.TrimSpace(req.ReferenceTraversalRequestPath),
	}

	switch {
	case normalized.RuntimeCommand == "":
		return Request{}, fmt.Errorf("runtime command is required")
	case normalized.WorkingCopyDir == "":
		return Request{}, fmt.Errorf("working copy directory is required")
	case normalized.ManifestPath == "":
		return Request{}, fmt.Errorf("manifest path is required")
	case normalized.ResultPath == "":
		return Request{}, fmt.Errorf("result path is required")
	case req.OutputDir != "" && normalized.OutputDir == "":
		return Request{}, fmt.Errorf("output directory must be non-empty when provided")
	case req.ObservationRequestPath != "" && normalized.ObservationRequestPath == "":
		return Request{}, fmt.Errorf("observation request path must be non-empty when provided")
	case normalized.ObservationRequestPath != "" && normalized.OutputDir == "":
		return Request{}, fmt.Errorf("observation request requires an output directory")
	case req.ReferenceTraversalRequestPath != "" && normalized.ReferenceTraversalRequestPath == "":
		return Request{}, fmt.Errorf("reference traversal request path must be non-empty when provided")
	case normalized.ReferenceTraversalRequestPath != "" && normalized.OutputDir == "":
		return Request{}, fmt.Errorf("reference traversal request requires an output directory")
	}

	return normalized, nil
}

// FilterLegacyEnvVars removes legacy runtime environment variables from aligned invocation.
func FilterLegacyEnvVars(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		switch {
		case strings.HasPrefix(entry, "PARAMETRON_MANIFEST="):
			continue
		case strings.HasPrefix(entry, "PARAMETRON_OUT="):
			continue
		default:
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (ExternalCapability) Invoke(ctx context.Context, req Request) (*Result, error) {
	if ctx == nil {
		return nil, &Error{
			Kind:    ErrorInvalid,
			Message: "invalid runtime capability invocation request",
			Err:     fmt.Errorf("context is required"),
		}
	}

	command, err := BuildCommand(req)
	if err != nil {
		return nil, &Error{
			Kind:    ErrorInvalid,
			Message: "invalid runtime capability invocation request",
			Err:     err,
		}
	}

	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Env = FilterLegacyEnvVars(os.Environ())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	result := &Result{
		Command: command,
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
	}
	if err == nil {
		return result, nil
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, &Error{
			Kind:     ErrorCancelled,
			Message:  "runtime capability invocation cancelled",
			ExitCode: exitCode(err),
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			Err:      ctxErr,
		}
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return result, &Error{
			Kind:     ErrorExit,
			Message:  "runtime capability invocation failed",
			ExitCode: exitErr.ExitCode(),
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			Err:      err,
		}
	}

	return result, &Error{
		Kind:     ErrorStart,
		Message:  "failed to start runtime capability invocation",
		ExitCode: -1,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		Err:      err,
	}
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
