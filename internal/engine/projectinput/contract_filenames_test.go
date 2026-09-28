package projectinput

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCaptureContract_IgnoresOldProjectRootFilename(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "parametron.cad.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	contract, err := LoadCaptureContract(&Resolved{ProjectRoot: projectRoot})
	if contract != nil || err != nil {
		t.Fatalf("old-only capture contract: contract=%#v, err=%v", contract, err)
	}
}

func TestLoadSemanticMap_IgnoresOldProjectRootFilename(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "parametron.semantic-map.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	contract, err := LoadSemanticMap(&Resolved{ProjectRoot: projectRoot})
	if contract != nil || err != nil {
		t.Fatalf("old-only semantic map: contract=%#v, err=%v", contract, err)
	}
}
