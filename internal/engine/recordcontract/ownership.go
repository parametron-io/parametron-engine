package recordcontract

import "fmt"

// Owner identifies a role in the Engine-produced record ownership model.
type Owner string

const (
	OwnerEngine    Owner = "engine"
	OwnerPDMServer Owner = "pdm-server"
)

// Ownership describes producer, local output, ingestion, and durable-storage boundaries.
type Ownership struct {
	Producer            Owner
	LocalOutputEmitter  Owner
	IngestionTarget     Owner
	DurableStorageOwner Owner
}

// EngineProducedOwnership returns the default ownership model for Engine-produced records.
func EngineProducedOwnership() Ownership {
	return Ownership{
		Producer:            OwnerEngine,
		LocalOutputEmitter:  OwnerEngine,
		IngestionTarget:     OwnerPDMServer,
		DurableStorageOwner: OwnerPDMServer,
	}
}

// ValidateOwnership rejects ownership outside the Engine-produced record boundary.
func ValidateOwnership(ownership Ownership) error {
	if ownership.Producer != OwnerEngine {
		return fmt.Errorf("%w: producer must be engine", ErrInvalidOwnership)
	}
	if ownership.LocalOutputEmitter != OwnerEngine {
		return fmt.Errorf("%w: local output emitter must be engine", ErrInvalidOwnership)
	}
	if ownership.IngestionTarget != OwnerPDMServer {
		return fmt.Errorf("%w: ingestion target must be pdm-server", ErrInvalidOwnership)
	}
	if ownership.DurableStorageOwner != OwnerPDMServer {
		return fmt.Errorf("%w: durable storage owner must be pdm-server", ErrInvalidOwnership)
	}
	return nil
}
