package semanticmap

import (
	"fmt"
	"strings"

	"parametron/internal/engine/semantic"
)

// ResolveTargetNativeObject resolves stable semantic target identity to the
// captured native object selector. Destination and capability decisions remain
// separate from native addressing; semantic names and labels are never fallbacks.
func ResolveTargetNativeObject(model *semantic.Model, linkage *IdentityLinkage, targetEntityKind, targetSemanticID string) (string, error) {
	if model == nil {
		return "", fmt.Errorf("native target resolution requires a semantic model")
	}

	var nativeRef string
	switch targetEntityKind {
	case "assembly", "part":
		component, ok := findComponentByID(model, targetSemanticID)
		if !ok || component.Kind != targetEntityKind {
			return "", fmt.Errorf("missing target for %s %q", targetEntityKind, targetSemanticID)
		}
		if _, err := requireIdentityLink(linkage, identityKindComponent, component.ID); err != nil {
			return "", err
		}
		nativeRef = component.NativeRef
	case "feature":
		feature, ok := findFeatureByID(model, targetSemanticID)
		if !ok {
			return "", fmt.Errorf("missing target for feature %q", targetSemanticID)
		}
		nativeRef = feature.NativeRef
	default:
		return "", fmt.Errorf("unsupported semantic target kind %q", targetEntityKind)
	}

	if strings.TrimSpace(nativeRef) == "" {
		return "", fmt.Errorf("missing native target mapping for %s %q", targetEntityKind, targetSemanticID)
	}
	if strings.TrimSpace(nativeRef) != nativeRef || strings.ContainsRune(nativeRef, '\x00') {
		return "", fmt.Errorf("invalid native target mapping for %s %q: must be an exact native object selector without surrounding whitespace or NUL", targetEntityKind, targetSemanticID)
	}
	return nativeRef, nil
}
