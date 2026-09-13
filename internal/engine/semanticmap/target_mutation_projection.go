package semanticmap

import "fmt"

// ProjectedTargetMutationRouting preserves Task 9 destination buckets while
// converting routed target actions into canonical Engine mutation families.
type ProjectedTargetMutationRouting struct {
	Assembly *ManifestMutationCollection
	Part     *ManifestMutationCollection
}

func ProjectTargetMutationRouting(routing *TargetMutationRouting) (*ProjectedTargetMutationRouting, error) {
	if routing == nil || (len(routing.Assembly) == 0 && len(routing.Part) == 0) {
		return nil, nil
	}

	result := &ProjectedTargetMutationRouting{}
	if len(routing.Assembly) > 0 {
		result.Assembly = &ManifestMutationCollection{}
		for _, mutation := range routing.Assembly {
			if err := appendRoutedTargetMutation(result.Assembly, mutation); err != nil {
				return nil, err
			}
		}
	}
	if len(routing.Part) > 0 {
		result.Part = &ManifestMutationCollection{}
		for _, mutation := range routing.Part {
			if err := appendRoutedTargetMutation(result.Part, mutation); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func appendRoutedTargetMutation(collection *ManifestMutationCollection, mutation RoutedTargetMutation) error {
	switch mutation.OperationKind {
	case string(OperationSuppress):
		collection.Suppression = append(collection.Suppression, ManifestSuppressionMutation{Object: mutation.Object, Suppressed: true})
	case string(OperationUnsuppress):
		collection.Suppression = append(collection.Suppression, ManifestSuppressionMutation{Object: mutation.Object, Suppressed: false})
	case string(OperationHide):
		collection.Visibility = append(collection.Visibility, ManifestVisibilityMutation{Object: mutation.Object, Visible: false})
	case string(OperationUnhide):
		collection.Visibility = append(collection.Visibility, ManifestVisibilityMutation{Object: mutation.Object, Visible: true})
	case string(OperationDelete):
		collection.Deletion = append(collection.Deletion, ManifestDeletionMutation{Object: mutation.Object})
	default:
		return fmt.Errorf("unsupported routed target mutation operation %q", mutation.OperationKind)
	}
	return nil
}
