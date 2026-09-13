package semanticmap

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrIO         = errors.New("semantic map I/O error")
	ErrDecode     = errors.New("semantic map decode error")
	ErrValidation = errors.New("semantic map validation error")
	ErrIdentity   = errors.New("semantic map identity linkage error")
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
