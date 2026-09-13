package runtimecap

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	// FreeCADAdapterID is the canonical adapter identity for FreeCAD.
	FreeCADAdapterID = "freecad"
	// DefaultFreeCADRuntimeExecutable is the Engine-facing aligned FreeCAD
	// runtime wrapper selected from PATH when no explicit command is configured.
	DefaultFreeCADRuntimeExecutable = "parametron-freecad"
)

// ExecutableSelectionSource describes how an executable command was selected.
type ExecutableSelectionSource string

const (
	ExecutableSelectionSourceConfigured  ExecutableSelectionSource = "configured"
	ExecutableSelectionSourceDefaultPATH ExecutableSelectionSource = "default_path"
)

const (
	ExecutableSelectionErrorInvalidAdapter = "invalid_adapter"
	ExecutableSelectionErrorUnconfigured   = "unconfigured_adapter"
	ExecutableSelectionErrorInvalidCommand = "invalid_command"
	ExecutableSelectionErrorNotFound       = "executable_not_found"
	ExecutableSelectionErrorUnusable       = "executable_unusable"
)

// ExecutableResolver selects an Engine-facing CAD runtime executable for an adapter.
type ExecutableResolver interface {
	Resolve(adapter string) (ExecutableSelection, error)
}

// ExecutableSelection is the inspectable result of executable resolution.
type ExecutableSelection struct {
	Adapter string
	Command string
	Path    string
	Source  ExecutableSelectionSource
}

// ExecutableSelectionError is a typed, inspectable executable selection failure.
type ExecutableSelectionError struct {
	Kind    string
	Adapter string
	Command string
	Err     error
}

func (e *ExecutableSelectionError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("cad runtime executable selection")
	if e.Kind != "" {
		b.WriteString(": ")
		b.WriteString(e.Kind)
	}
	if e.Adapter != "" {
		b.WriteString(": adapter=")
		b.WriteString(e.Adapter)
	}
	if e.Command != "" {
		b.WriteString(": command=")
		b.WriteString(e.Command)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *ExecutableSelectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// DefaultExecutableResolver resolves adapter executable references from a
// defensive configuration snapshot and filesystem/PATH lookups.
type DefaultExecutableResolver struct {
	configured map[string]string
	lookPath   func(file string) (string, error)
	stat       func(name string) (fs.FileInfo, error)
}

var defaultRuntimeExecutables = map[string]string{
	FreeCADAdapterID: DefaultFreeCADRuntimeExecutable,
}

// NewExecutableResolver constructs a resolver from configured adapter commands.
// The input map is copied; later mutation does not affect the resolver.
func NewExecutableResolver(configured map[string]string) (*DefaultExecutableResolver, error) {
	copied := make(map[string]string, len(configured))
	for adapter, command := range configured {
		copied[adapter] = command
	}
	return &DefaultExecutableResolver{
		configured: copied,
		lookPath:   exec.LookPath,
		stat:       os.Stat,
	}, nil
}

// Resolve selects and validates the runtime executable for the given adapter.
func (r *DefaultExecutableResolver) Resolve(adapter string) (ExecutableSelection, error) {
	if r == nil {
		return ExecutableSelection{}, &ExecutableSelectionError{
			Kind:    ExecutableSelectionErrorInvalidAdapter,
			Adapter: adapter,
			Err:     fmt.Errorf("resolver is nil"),
		}
	}
	if err := validateAdapterID(adapter); err != nil {
		return ExecutableSelection{}, &ExecutableSelectionError{
			Kind:    ExecutableSelectionErrorInvalidAdapter,
			Adapter: adapter,
			Err:     err,
		}
	}

	if command, ok := r.configured[adapter]; ok {
		return r.resolveReference(adapter, command, ExecutableSelectionSourceConfigured)
	}
	if command, ok := defaultRuntimeExecutables[adapter]; ok {
		return r.resolveReference(adapter, command, ExecutableSelectionSourceDefaultPATH)
	}
	return ExecutableSelection{}, &ExecutableSelectionError{
		Kind:    ExecutableSelectionErrorUnconfigured,
		Adapter: adapter,
		Err:     fmt.Errorf("adapter has no configured command and no default executable"),
	}
}

func (r *DefaultExecutableResolver) resolveReference(adapter, command string, source ExecutableSelectionSource) (ExecutableSelection, error) {
	refKind, err := classifyExecutableReference(command)
	if err != nil {
		return ExecutableSelection{}, &ExecutableSelectionError{
			Kind:    ExecutableSelectionErrorInvalidCommand,
			Adapter: adapter,
			Command: command,
			Err:     err,
		}
	}

	var resolved string
	switch refKind {
	case "absolute":
		resolved = filepath.Clean(command)
		if err := r.validateResolvedPath(resolved); err != nil {
			kind := ExecutableSelectionErrorUnusable
			if os.IsNotExist(err) {
				kind = ExecutableSelectionErrorNotFound
			}
			return ExecutableSelection{}, &ExecutableSelectionError{
				Kind:    kind,
				Adapter: adapter,
				Command: command,
				Err:     fmt.Errorf("source=%s: %w", source, err),
			}
		}
	case "bare":
		lookPath := r.lookPath
		if lookPath == nil {
			lookPath = exec.LookPath
		}
		found, lookErr := lookPath(command)
		if lookErr != nil {
			return ExecutableSelection{}, &ExecutableSelectionError{
				Kind:    ExecutableSelectionErrorNotFound,
				Adapter: adapter,
				Command: command,
				Err:     fmt.Errorf("source=%s: %w", source, lookErr),
			}
		}
		resolved = found
		if !filepath.IsAbs(resolved) {
			abs, absErr := filepath.Abs(resolved)
			if absErr != nil {
				return ExecutableSelection{}, &ExecutableSelectionError{
					Kind:    ExecutableSelectionErrorUnusable,
					Adapter: adapter,
					Command: command,
					Err:     fmt.Errorf("source=%s: resolve absolute path: %w", source, absErr),
				}
			}
			resolved = abs
		}
		resolved = filepath.Clean(resolved)
		if err := r.validateResolvedPath(resolved); err != nil {
			kind := ExecutableSelectionErrorUnusable
			if os.IsNotExist(err) {
				kind = ExecutableSelectionErrorNotFound
			}
			return ExecutableSelection{}, &ExecutableSelectionError{
				Kind:    kind,
				Adapter: adapter,
				Command: command,
				Err:     fmt.Errorf("source=%s: %w", source, err),
			}
		}
	default:
		return ExecutableSelection{}, &ExecutableSelectionError{
			Kind:    ExecutableSelectionErrorInvalidCommand,
			Adapter: adapter,
			Command: command,
			Err:     fmt.Errorf("unsupported executable reference form %q", refKind),
		}
	}

	return ExecutableSelection{
		Adapter: adapter,
		Command: resolvedCommand(command, resolved, refKind),
		Path:    resolved,
		Source:  source,
	}, nil
}

func resolvedCommand(command, resolved, refKind string) string {
	if refKind == "absolute" {
		return resolved
	}
	return command
}

func (r *DefaultExecutableResolver) validateResolvedPath(path string) error {
	if path == "" {
		return fmt.Errorf("resolved path is empty")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("resolved path is not absolute")
	}
	stat := r.stat
	if stat == nil {
		stat = os.Stat
	}
	info, err := stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file")
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("path is not executable")
	}
	return nil
}

func validateAdapterID(adapter string) error {
	if adapter == "" {
		return fmt.Errorf("adapter id is empty")
	}
	if strings.TrimSpace(adapter) != adapter {
		return fmt.Errorf("adapter id has leading or trailing whitespace")
	}
	for i, r := range adapter {
		if r == '/' || r == '\\' {
			return fmt.Errorf("adapter id contains a path separator")
		}
		if unicode.IsControl(r) {
			return fmt.Errorf("adapter id contains a control character")
		}
		if i == 0 {
			if !isLowerASCIILetter(r) && !isASCIIDigit(r) {
				return fmt.Errorf("adapter id must start with a lowercase letter or digit")
			}
			continue
		}
		switch {
		case isLowerASCIILetter(r), isASCIIDigit(r), r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("adapter id contains an invalid character")
		}
	}
	return nil
}

func classifyExecutableReference(command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("executable reference is empty")
	}
	if strings.TrimSpace(command) != command {
		return "", fmt.Errorf("executable reference has leading or trailing whitespace")
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("executable reference is blank")
	}
	for _, r := range command {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("executable reference contains a control character")
		}
	}

	if filepath.IsAbs(command) {
		return "absolute", nil
	}

	if strings.Contains(command, "/") || strings.Contains(command, `\`) {
		return "", fmt.Errorf("relative executable paths are not allowed")
	}
	if strings.ContainsAny(command, " \t") {
		return "", fmt.Errorf("bare executable names must not contain whitespace or arguments")
	}
	return "bare", nil
}

func isLowerASCIILetter(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
