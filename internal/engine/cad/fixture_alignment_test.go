package cad

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #86 boundary coverage: every parseable prm.cad.json fixture in the repository
// declares the required v1.0 entities.drawings registry. The intentionally
// malformed fixture is excluded because it is not JSON.

func TestRepositoryCaptureFixturesDeclareDrawings(t *testing.T) {
	root := repositoryRoot(t)
	checked := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Name() != "prm.cad.json" {
			return nil
		}
		slashPath := filepath.ToSlash(path)
		if strings.Contains(slashPath, "/testdata/break/malformed-json/") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: read fixture: %v", slashPath, err)
			return nil
		}
		var rootObject map[string]any
		if err := json.Unmarshal(data, &rootObject); err != nil {
			t.Errorf("%s: parse fixture JSON: %v", slashPath, err)
			return nil
		}
		entities, ok := rootObject["entities"].(map[string]any)
		if !ok {
			t.Errorf("%s: missing entities object", slashPath)
			return nil
		}
		drawings, ok := entities["drawings"]
		if !ok {
			t.Errorf("%s: missing entities.drawings", slashPath)
			return nil
		}
		if _, ok := drawings.([]any); !ok {
			t.Errorf("%s: entities.drawings must be a non-null array, got %T", slashPath, drawings)
			return nil
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository fixtures: %v", err)
	}
	if checked == 0 {
		t.Fatal("no prm.cad.json fixtures found")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root")
		}
		dir = parent
	}
}
