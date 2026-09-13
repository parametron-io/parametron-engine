package recordmap

import "errors"

var (
	ErrInvalidReportMapping             = errors.New("invalid report mapping")
	ErrInvalidMetadataMapping           = errors.New("invalid metadata mapping")
	ErrInvalidArtifactMapping           = errors.New("invalid artifact mapping")
	ErrInvalidObservedMapping           = errors.New("invalid observed mapping")
	ErrInvalidVerificationMapping       = errors.New("invalid verification mapping")
	ErrInvalidRuntimeResultMapping      = errors.New("invalid runtime result mapping")
	ErrInvalidReferenceTraversalMapping = errors.New("invalid reference traversal mapping")
)
