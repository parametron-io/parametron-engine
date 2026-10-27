package freecad

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"parametron/internal/engine/resourcebinding"
)

type declaredResourceCopy struct {
	binding             resourcebinding.Binding
	source, destination string
}

// planDeclaredResourceCopies validates the complete set before any staging write.
func planDeclaredResourceCopies(context *resourcebinding.Context, attempt FreeCADRuntimeAttempt) ([]declaredResourceCopy, resourcebinding.Inventory, error) {
	if context == nil {
		return nil, resourcebinding.Inventory{}, nil
	}
	selected := context.Selected()
	var copies []declaredResourceCopy
	var bindings []resourcebinding.Binding
	for _, binding := range context.Inventory().Bindings() {
		source, err := context.SourcePath(binding)
		if err != nil {
			return nil, resourcebinding.Inventory{}, &resourcebinding.Error{LogicalID: binding.LogicalID, Field: "sourceCoordinate", Err: err}
		}
		coordinate := attempt.Layout.SourceDocument
		if binding.LogicalID != selected.LogicalID {
			coordinate = fmt.Sprintf("resources/%x/%s", sha256.Sum256([]byte(binding.LogicalID)), binding.SourceCoordinate)
		} else if source != attempt.SourcePath {
			return nil, resourcebinding.Inventory{}, fmt.Errorf("selected resource %q source does not match manifest source", binding.LogicalID)
		}
		binding.MaterializedCoordinate = coordinate
		destination := filepath.Join(attempt.Layout.WorkingCopyDir, filepath.FromSlash(coordinate))
		copies = append(copies, declaredResourceCopy{binding, source, destination})
		bindings = append(bindings, binding)
	}
	inventory, err := resourcebinding.New(bindings)
	if err != nil {
		return nil, resourcebinding.Inventory{}, err
	}
	destinations := []string{attempt.Layout.WorkingCopyDir, attempt.Layout.SourceDocumentPath, attempt.Layout.ManifestPath, attempt.Layout.ResultPath, attempt.Layout.OutputDir}
	for _, copy := range copies {
		destinations = append(destinations, copy.destination)
	}
	// Include reserved runtime files in the intended set: a logical filename
	// must not become an ancestor directory of a declared resource destination.
	files := []string{attempt.Layout.ManifestPath, attempt.Layout.ResultPath}
	for _, copy := range copies {
		files = append(files, copy.destination)
	}
	for n, file := range files {
		for _, other := range files[:n] {
			if file == other || strings.HasPrefix(file, other+string(filepath.Separator)) || strings.HasPrefix(other, file+string(filepath.Separator)) {
				return nil, resourcebinding.Inventory{}, fmt.Errorf("conflicting materialization destinations %q and %q", other, file)
			}
		}
		if file == attempt.Layout.OutputDir || strings.HasPrefix(file, attempt.Layout.OutputDir+string(filepath.Separator)) {
			return nil, resourcebinding.Inventory{}, fmt.Errorf("materialization file %q conflicts with runtime output directory", file)
		}
	}
	for _, destination := range destinations {
		if err := validateResourceDestination(destination); err != nil {
			return nil, resourcebinding.Inventory{}, err
		}
		isDirectory := destination == attempt.Layout.WorkingCopyDir || destination == attempt.Layout.OutputDir
		if info, err := os.Lstat(destination); err == nil && info.IsDir() != isDirectory {
			return nil, resourcebinding.Inventory{}, fmt.Errorf("materialization destination %q has conflicting file type", destination)
		}
		for _, copy := range copies {
			sourceInfo, err := os.Stat(copy.source)
			if err != nil {
				return nil, resourcebinding.Inventory{}, err
			}
			destinationInfo, err := os.Stat(destination)
			if err == nil && os.SameFile(sourceInfo, destinationInfo) {
				return nil, resourcebinding.Inventory{}, fmt.Errorf("resource %q destination would overwrite a declared source", copy.binding.LogicalID)
			}
			if err != nil && !os.IsNotExist(err) {
				return nil, resourcebinding.Inventory{}, err
			}
		}
	}
	return copies, inventory, nil
}

// Reject symlinks and non-directory ancestors before creating directories or
// replacing files. This is a filesystem preflight, not a concurrent-writer lock.
func validateResourceDestination(destination string) error {
	for current := destination; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect materialization destination %q: %w", current, err)
		}
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("materialization destination %q must not be a symbolic link", current)
			}
			if current != destination && !info.IsDir() {
				return fmt.Errorf("materialization ancestor %q is not a directory", current)
			}
			if current == destination && !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("materialization destination %q is not a regular file or directory", current)
			}
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

func prepareDeclaredResourceCopies(copies []declaredResourceCopy, primary string) error {
	for _, copy := range copies {
		if copy.destination == primary {
			continue
		}
		if err := validateResourceDestination(copy.destination); err != nil {
			return err
		}
		source, err := os.Open(copy.source)
		if err != nil {
			return err
		}
		err = os.MkdirAll(filepath.Dir(copy.destination), 0o755)
		if err == nil {
			err = copyFreeCADRuntimeSourceDocument(source, filepath.Dir(copy.destination), copy.destination)
		}
		source.Close()
		if err != nil {
			return &resourcebinding.Error{LogicalID: copy.binding.LogicalID, Field: "materializedCoordinate", Err: err}
		}
	}
	return nil
}
