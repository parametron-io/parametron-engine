package resourcebinding

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func model(id, source, destination string) Binding {
	return Binding{ProjectID: "project", Kind: ResourceKindModel, LogicalID: id, SourceCoordinate: source, MaterializedCoordinate: destination}
}

func TestInventoryIdentityAliasesAndCopies(t *testing.T) {
	in := []Binding{model("part", "models/part.FCStd", ""), model("Part", "models/part.FCStd", "")}
	i, err := New(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []Binding{in[1], in[0]}
	in[0].LogicalID = "corrupted"
	out := i.Bindings()
	out[0].SourceCoordinate = "corrupted"
	if !reflect.DeepEqual(i.Bindings(), want) {
		t.Fatal("inventory aliases caller storage or is not canonically ordered")
	}
	for _, b := range want {
		got, err := i.Lookup(b.ProjectID, b.Kind, b.LogicalID)
		if err != nil || got != b {
			t.Fatalf("lookup: %v %v", got, err)
		}
		got.LogicalID = "corrupted"
	}
	if _, err := i.Lookup("other-project", ResourceKindModel, "Part"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("project scope: %v", err)
	}
	for _, coordinate := range []string{"models/part.FCStd", "missing", ""} {
		if _, err := i.LookupCoordinate(coordinate, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ambiguous/missing source %q: %v", coordinate, err)
		}
	}
	if _, err := i.LookupCoordinate("", true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unmaterialized lookup: %v", err)
	}
	root := t.TempDir()
	c, err := Select(i, "project", "part", root)
	if err != nil {
		t.Fatal(err)
	}
	clone := c.Clone()
	if clone == c || clone.Selected() != want[1] || clone.SelectedSourcePath() != filepath.Join(root, "models", "part.FCStd") {
		t.Fatal("selection/clone lost exact alias")
	}
	copy := clone.Inventory().Bindings()
	copy[0].LogicalID = "corrupted"
	if !reflect.DeepEqual(c.Inventory().Bindings(), want) {
		t.Fatal("context inventory is mutable")
	}
	materialized, err := New([]Binding{model("part", "models/part.FCStd", "source/part.FCStd"), model("alias", "models/part.FCStd", "resources/alias/part.FCStd")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := materialized.LookupCoordinate("source/part.FCStd", true)
	if err != nil || b.LogicalID != "part" {
		t.Fatalf("materialized lookup: %v %v", b, err)
	}
	if _, err := Select(materialized, "project", "part", root); !errors.Is(err, ErrInvalid) {
		t.Fatalf("selection accepted completed inventory: %v", err)
	}
}

func TestInventoryRejectsConflictsDeterministically(t *testing.T) {
	for name, bindings := range map[string][]Binding{
		"duplicate":             {model("a", "a.FCStd", ""), model("a", "a.FCStd", "")},
		"conflicting source":    {model("a", "a.FCStd", ""), model("a", "b.FCStd", "")},
		"destination collision": {model("a", "a.FCStd", "same"), model("b", "b.FCStd", "same")},
		"destination ancestor":  {model("a", "a.FCStd", "folder"), model("b", "b.FCStd", "folder/file")},
	} {
		t.Run(name, func(t *testing.T) {
			_, first := New(bindings)
			slices.Reverse(bindings)
			_, second := New(bindings)
			if !errors.Is(first, ErrInvalid) || !errors.Is(second, ErrInvalid) || first.Error() != second.Error() {
				t.Fatalf("non-deterministic rejection: %v / %v", first, second)
			}
		})
	}
}

func TestInventoryCoordinatesAndKinds(t *testing.T) {
	for _, coordinate := range []string{"", ".", "..", "../a", "/a", "a/../b", "a//b", "a/", " a", "a ", `a\b`, "a\x00b", "C:/a"} {
		for _, field := range []string{"source", "materialized"} {
			if field == "materialized" && coordinate == "" {
				continue
			}
			b := model("a", "valid/a.FCStd", "")
			if field == "source" {
				b.SourceCoordinate = coordinate
			} else {
				b.MaterializedCoordinate = coordinate
			}
			if _, err := New([]Binding{b}); !errors.Is(err, ErrInvalid) {
				t.Errorf("accepted %s coordinate %q: %v", field, coordinate, err)
			}
		}
	}
	for _, kind := range []ResourceKind{ResourceKindDSL, ResourceKindTable, "", "native"} {
		b := model("a", "a.FCStd", "")
		b.Kind = kind
		if _, err := New([]Binding{b}); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted kind %q", kind)
		}
	}
	for _, field := range []string{"project", "logical"} {
		for _, value := range []string{"", " a", "a "} {
			b := model("a", "a.FCStd", "")
			if field == "project" {
				b.ProjectID = value
			} else {
				b.LogicalID = value
			}
			if _, err := New([]Binding{b}); !errors.Is(err, ErrInvalid) {
				t.Errorf("accepted %s %q", field, value)
			}
		}
	}
	if _, err := New([]Binding{model("a", "models/a.FCStd", "source/a.FCStd")}); err != nil {
		t.Fatal(err)
	}
}

func TestSourcePathFilesystemContainment(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	for _, dir := range []string{root, outside} {
		if err := os.WriteFile(filepath.Join(dir, "part.FCStd"), []byte("source"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{"inside": filepath.Join(root, "part.FCStd"), "outside": filepath.Join(outside, "part.FCStd"), "dangling": filepath.Join(root, "missing"), "directory": root} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
		_, err := SourcePath(root, name)
		if (err == nil) != (name == "inside") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, coordinate := range []string{"missing", "../part.FCStd", "/part.FCStd"} {
		if _, err := SourcePath(root, coordinate); err == nil {
			t.Errorf("accepted %q", coordinate)
		}
	}
}
