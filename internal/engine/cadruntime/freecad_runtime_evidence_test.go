package cadruntime

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadOptionalFreeCADRuntimeEvidence_* covers the guarded raw evidence
// reader introduced to let recordemit capture actual CAD attempt evidence
// (prm.result.json / prm.verification.json / prm.observed.json) instead of
// reconstructing filesystem paths. It reuses the same working-copy
// containment and regular-file guards exercised elsewhere in this package
// (see TestInvokeAndValidateFreeCADRuntime_RejectsOwnedOutputSymlinks and
// TestInvokeAndValidateFreeCADRuntime_RejectsSymlinkedOutputPathComponents)
// rather than inventing new safety policy.

func TestReadOptionalFreeCADRuntimeEvidence_EmptyPathReturnsNilNil(t *testing.T) {
	working := t.TempDir()
	got, err := ReadOptionalFreeCADRuntimeEvidence(working, "")
	if err != nil || got != nil {
		t.Fatalf("got=%q err=%v, want nil,nil", got, err)
	}
}

func TestReadOptionalFreeCADRuntimeEvidence_ValidRegularFile_PreservesExactBytes(t *testing.T) {
	working := t.TempDir()
	path := filepath.Join(working, "prm.result.json")
	// Deliberately non-canonical formatting (whitespace, indentation, key
	// order) so this test proves byte preservation rather than semantic
	// equality: a re-serializing implementation would normalize this away.
	raw := []byte("{\n  \"status\":   \"succeeded\",\n  \"schemaVersion\": \"1.0\"\n}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadOptionalFreeCADRuntimeEvidence(working, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("bytes differ\nwant: %q\ngot:  %q", raw, got)
	}
}

func TestReadOptionalFreeCADRuntimeEvidence_AbsentOptionalFileReturnsNilNil(t *testing.T) {
	working := t.TempDir()
	path := filepath.Join(working, "prm.observed.json")

	got, err := ReadOptionalFreeCADRuntimeEvidence(working, path)
	if err != nil || got != nil {
		t.Fatalf("got=%q err=%v, want nil,nil for an absent optional path", got, err)
	}
}

func TestReadOptionalFreeCADRuntimeEvidence_RejectsPathOutsideWorkingCopy(t *testing.T) {
	working := t.TempDir()
	outside := filepath.Join(t.TempDir(), "prm.result.json")
	if err := os.WriteFile(outside, []byte("outside-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadOptionalFreeCADRuntimeEvidence(working, outside)
	if err == nil || got != nil {
		t.Fatalf("got=%q err=%v, want an error and no bytes for a path outside the working copy", got, err)
	}
}

func TestReadOptionalFreeCADRuntimeEvidence_RejectsSymlinkPathComponent(t *testing.T) {
	working := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "prm.result.json"), []byte("external-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(working, "outputs")
	if err := os.Symlink(external, linked); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	path := filepath.Join(linked, "prm.result.json")

	got, err := ReadOptionalFreeCADRuntimeEvidence(working, path)
	if err == nil || got != nil {
		t.Fatalf("got=%q err=%v, want a symlink-component error", got, err)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want it to mention the symlink component", err)
	}
}

func TestReadOptionalFreeCADRuntimeEvidence_RejectsSymlinkFile(t *testing.T) {
	working := t.TempDir()
	target := filepath.Join(t.TempDir(), "real-prm.result.json")
	if err := os.WriteFile(target, []byte("real-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(working, "prm.result.json")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	got, err := ReadOptionalFreeCADRuntimeEvidence(working, path)
	if err == nil || got != nil {
		t.Fatalf("got=%q err=%v, want an error and no bytes for a symlinked evidence file", got, err)
	}
}

func TestReadOptionalFreeCADRuntimeEvidence_RejectsNonRegularFile(t *testing.T) {
	working := t.TempDir()
	path := filepath.Join(working, "prm.observed.json")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ReadOptionalFreeCADRuntimeEvidence(working, path)
	if err == nil || got != nil {
		t.Fatalf("got=%q err=%v, want an error and no bytes when the evidence path is a directory", got, err)
	}
}

func TestReadOptionalFreeCADRuntimeEvidence_RejectsNonCanonicalOrInvalidPaths(t *testing.T) {
	working := t.TempDir()
	regular := filepath.Join(working, "prm.result.json")
	if err := os.WriteFile(regular, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		workingCopyDir string
		evidencePath   string
	}{
		"relative working copy dir":                {"relative-working-copy", regular},
		"relative evidence path":                   {working, "relative/prm.result.json"},
		"non-clean evidence path":                  {working, working + string(filepath.Separator) + "." + string(filepath.Separator) + "prm.result.json"},
		"trailing separator evidence path":         {working, regular + string(filepath.Separator)},
		"empty working copy dir":                   {"", regular},
		"evidence path is working copy dir itself": {working, working},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ReadOptionalFreeCADRuntimeEvidence(tc.workingCopyDir, tc.evidencePath)
			if err == nil || got != nil {
				t.Fatalf("got=%q err=%v, want an error and no bytes for %s", got, err, name)
			}
		})
	}
}
