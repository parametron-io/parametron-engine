package recordcontract

import "errors"

var (
	ErrUnknownFamily            = errors.New("unknown record contract family")
	ErrInvalidVersion           = errors.New("invalid record contract version")
	ErrInvalidOwnership         = errors.New("invalid record contract ownership")
	ErrInvalidIdentity          = errors.New("invalid record contract identity")
	ErrInvalidProvenance        = errors.New("invalid record contract provenance")
	ErrInvalidExecutionRecord   = errors.New("invalid execution record contract")
	ErrInvalidArtifactRecord    = errors.New("invalid artifact record contract")
	ErrInvalidObservationRecord = errors.New("invalid observation record contract")
	ErrInvalidReferenceRecord   = errors.New("invalid reference record contract")
	ErrInvalidFailureRecord      = errors.New("invalid failure record contract")
	ErrInvalidVerificationRecord = errors.New("invalid verification record contract")
)
