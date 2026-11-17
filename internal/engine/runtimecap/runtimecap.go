package runtimecap

import "context"

// IntegrityRequestEnvVar names the Engine-owned environment variable that tells an
// external runtime where the per-resource integrity request document for this
// attempt is located. The variable is transport only: the contract itself is the
// versioned JSON document it points at.
const IntegrityRequestEnvVar = "PARAMETRON_RUNTIME_INTEGRITY_REQUEST"

// Capability defines the Engine-owned contract for external runtime invocation.
type Capability interface {
	Invoke(ctx context.Context, req Request) (*Result, error)
}

// Request describes the explicit file-based CLI contract for external runtime execution.
type Request struct {
	RuntimeCommand                string
	WorkingCopyDir                string
	ManifestPath                  string
	ResultPath                    string
	OutputDir                     string
	ObservationRequestPath        string
	ReferenceTraversalRequestPath string
	// IntegrityRequestPath optionally locates the Engine-authored per-resource
	// integrity request for this invocation. It is transported to the runtime as
	// the Engine-owned environment entry named by
	// IntegrityRequestEnvVar; the aligned argv contract is unchanged.
	IntegrityRequestPath string
}

// Command captures the resolved external runtime subprocess invocation.
type Command struct {
	Path string
	Args []string
}

// Result captures stdout/stderr and the resolved command from an external runtime invocation.
type Result struct {
	Command Command
	Stdout  string
	Stderr  string
}
