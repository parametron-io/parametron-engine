package projectinput

import (
	"fmt"
	"os"
	"path/filepath"

	"parametron/internal/engine/semanticmap"
)

func LoadSemanticMap(resolved *Resolved) (*semanticmap.SemanticMap, error) {
	if resolved == nil {
		return nil, nil
	}

	path := filepath.Join(resolved.ProjectRoot, semanticmap.FileName)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat project semantic map %q: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("project semantic map %q must be a file", path)
	}

	contract, err := semanticmap.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load project semantic map %q: %w", path, err)
	}
	return contract, nil
}
