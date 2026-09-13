package runtimecap

import "context"

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
