package semanticmap

import (
	"fmt"

	"parametron/internal/engine/semantic"
)

// RoutedTargetMutation is the minimal Engine-owned handoff between semantic
// target-action lowering and later runtime mutation-family projection.
type RoutedTargetMutation struct {
	OperationKind string
	Object        string
}

type TargetMutationRouting struct {
	Assembly []RoutedTargetMutation
	Part     []RoutedTargetMutation
}

type TargetMutationRoutingRequest struct {
	Model           *semantic.Model
	IdentityLinkage *IdentityLinkage
	Mutations       []semantic.MutationIntent
}

// RouteTargetMutations projects canonical target-action intents to their
// destination and adapter/capture-backed native object names. It deliberately
// does not project runtime mutation families.
func RouteTargetMutations(request *TargetMutationRoutingRequest) (*TargetMutationRouting, error) {
	if request == nil {
		return nil, fmt.Errorf("target mutation routing request must not be nil")
	}
	if request.Model == nil {
		return nil, fmt.Errorf("target mutation routing requires a semantic model")
	}
	if request.IdentityLinkage == nil {
		return nil, fmt.Errorf("target mutation routing requires identity linkage")
	}

	routing := &TargetMutationRouting{
		Assembly: []RoutedTargetMutation{},
		Part:     []RoutedTargetMutation{},
	}
	for _, mutation := range request.Mutations {
		if mutation.ValueSource.Kind != "target_action" {
			return nil, fmt.Errorf("semantic mutation operation %q is not a canonical target-action mutation", mutation.OperationKind)
		}
		switch mutation.OperationKind {
		case "suppress", "unsuppress", "hide", "unhide", "delete":
		default:
			return nil, fmt.Errorf("unsupported target-action mutation operation %q", mutation.OperationKind)
		}

		destination, err := resolveMutationDestination(mutation)
		if err != nil {
			return nil, err
		}
		object, err := resolveManifestEntityName(request.Model, request.IdentityLinkage, mutation.TargetEntityKind, mutation.TargetSemanticID)
		if err != nil {
			return nil, fmt.Errorf("cannot route target-action mutation %q: %w", mutation.OperationKind, err)
		}

		routed := RoutedTargetMutation{
			OperationKind: mutation.OperationKind,
			Object:        object,
		}
		if destination == "assembly" {
			routing.Assembly = append(routing.Assembly, routed)
		} else {
			routing.Part = append(routing.Part, routed)
		}
	}
	return routing, nil
}
