package projectinput

import (
	"fmt"
	"os"
	"path/filepath"

	"parametron/internal/engine/cad"
)

const captureContractFileName = "parametron.cad.json"

func LoadCaptureContract(resolved *Resolved) (*cad.CADContract, error) {
	if resolved == nil {
		return nil, nil
	}

	path := filepath.Join(resolved.ProjectRoot, captureContractFileName)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat project capture contract %q: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("project capture contract %q must be a file", path)
	}

	contract, err := cad.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load project capture contract %q: %w", path, err)
	}
	return contract, nil
}
