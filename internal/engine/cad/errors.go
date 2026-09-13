package cad

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrIO         = errors.New("cad contract I/O error")
	ErrDecode     = errors.New("cad contract decode error")
	ErrValidation = errors.New("cad contract validation error")
	ErrAuthoring  = errors.New("cad authoring validation error")
)

const (
	AuthoringDiagnosticCategoryInvalidDSLReference          = "invalid_dsl_reference"
	AuthoringDiagnosticCategoryAmbiguousDSLReference        = "ambiguous_dsl_reference"
	AuthoringDiagnosticCategorySourceModelReferenceMismatch = "source_model_reference_mismatch"

	AuthoringDiagnosticCodeCapturedParameterReferenceNotFound  = "captured_parameter_reference_not_found"
	AuthoringDiagnosticCodeCapturedParameterReferenceAmbiguous = "captured_parameter_reference_ambiguous"
	AuthoringDiagnosticCodeSourceModelLogicalIDMismatch        = "source_model_logical_id_mismatch"
)

type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string {
	if e == nil {
		return ErrIO.Error()
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *FileError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrIO}
	}
	return []error{ErrIO, e.Err}
}

type DecodeError struct {
	Err error
}

func (e *DecodeError) Error() string {
	if e == nil || e.Err == nil {
		return ErrDecode.Error()
	}
	return fmt.Sprintf("%s: %v", ErrDecode, e.Err)
}

func (e *DecodeError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrDecode}
	}
	return []error{ErrDecode, e.Err}
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Problems) == 0 {
		return ErrValidation.Error()
	}
	return fmt.Sprintf("%s: %s", ErrValidation, strings.Join(e.Problems, "; "))
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

func (e *ValidationError) Messages() []string {
	if e == nil {
		return nil
	}
	out := make([]string, len(e.Problems))
	copy(out, e.Problems)
	return out
}

type AuthoringDiagnostic struct {
	Category string
	Code     string
	Message  string
}

type AuthoringValidationError struct {
	Problems []AuthoringDiagnostic
}

func (e *AuthoringValidationError) Error() string {
	if e == nil || len(e.Problems) == 0 {
		return ErrAuthoring.Error()
	}
	return fmt.Sprintf("%s: %s", ErrAuthoring, strings.Join(e.Messages(), "; "))
}

func (e *AuthoringValidationError) Unwrap() error {
	return ErrAuthoring
}

func (e *AuthoringValidationError) Messages() []string {
	if e == nil {
		return nil
	}
	out := make([]string, 0, len(e.Problems))
	for _, problem := range e.Problems {
		out = append(out, problem.Message)
	}
	return out
}

func (e *AuthoringValidationError) Diagnostics() []AuthoringDiagnostic {
	if e == nil {
		return nil
	}
	out := make([]AuthoringDiagnostic, len(e.Problems))
	copy(out, e.Problems)
	return out
}
