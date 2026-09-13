package recordcontract

import (
	"fmt"
	"strings"
)

// Family identifies an Engine-produced normalized record contract family.
type Family string

const (
	FamilyExecution    Family = "execution"
	FamilyArtifact     Family = "artifact"
	FamilyObservation  Family = "observation"
	FamilyReference    Family = "reference"
	FamilyFailure      Family = "failure"
	FamilyVerification Family = "verification"
)

// NormalizeFamily trims leading and trailing whitespace without rewriting unknown values.
func NormalizeFamily(family Family) Family {
	return Family(strings.TrimSpace(string(family)))
}

// KnownFamily reports whether family is one of the six supported record families.
func KnownFamily(family Family) bool {
	switch NormalizeFamily(family) {
	case FamilyExecution, FamilyArtifact, FamilyObservation, FamilyReference, FamilyFailure, FamilyVerification:
		return true
	default:
		return false
	}
}

// ValidateFamily returns ErrUnknownFamily when family is not supported.
func ValidateFamily(family Family) error {
	if !KnownFamily(family) {
		return fmt.Errorf("%w: %q", ErrUnknownFamily, family)
	}
	return nil
}
