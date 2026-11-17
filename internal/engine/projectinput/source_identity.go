package projectinput

import (
	"fmt"
	"sort"

	"parametron/internal/engine/resourcebinding"
)

// AttachSourceIdentities returns a declared-resource inventory whose bindings
// carry the captured path-free source content identity for their logical ID.
//
// The result reuses the #48 inventory: identity keys and project-source
// coordinates are unchanged, and only the content identity of the actual
// project source bytes is attached. Resources that are unused, zero-reference,
// or standalone participate because the inventory is derived from the whole
// project declaration rather than from executed products.
func AttachSourceIdentities(resolved *Resolved, captured *CapturedResources) (resourcebinding.Inventory, error) {
	if resolved == nil {
		return resourcebinding.Inventory{}, nil
	}

	signatures := make(map[string]string, len(capturedModels(captured)))
	if captured != nil {
		for _, model := range captured.Models {
			if model.LogicalID == "" {
				return resourcebinding.Inventory{}, fmt.Errorf("captured model source identity requires a logical ID")
			}
			if _, exists := signatures[model.LogicalID]; exists {
				return resourcebinding.Inventory{}, fmt.Errorf("captured model source identities contain duplicate logical ID %q", model.LogicalID)
			}
			signatures[model.LogicalID] = model.Signature
		}
	}

	bindings := resolved.ModelInventory.Bindings()
	enriched := make([]resourcebinding.Binding, 0, len(bindings))
	for _, binding := range bindings {
		signature, ok := signatures[binding.LogicalID]
		if !ok {
			return resourcebinding.Inventory{}, fmt.Errorf("declared resource %q has no captured source identity", binding.LogicalID)
		}
		binding.SourceDigest = signature
		enriched = append(enriched, binding)
		delete(signatures, binding.LogicalID)
	}

	if len(signatures) > 0 {
		undeclared := make([]string, 0, len(signatures))
		for logicalID := range signatures {
			undeclared = append(undeclared, logicalID)
		}
		sort.Strings(undeclared)
		return resourcebinding.Inventory{}, fmt.Errorf("captured source identity %q is not a declared resource", undeclared[0])
	}

	return resourcebinding.New(enriched)
}

func capturedModels(captured *CapturedResources) []CapturedResource {
	if captured == nil {
		return nil
	}
	return captured.Models
}
