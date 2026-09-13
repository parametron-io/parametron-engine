package adapter

import (
	"context"

	"parametron/internal/authoring/planner"
)

// Adapter defines the interface for execution backends.
type Adapter interface {
	Run(ctx context.Context, step planner.Step) error
}
