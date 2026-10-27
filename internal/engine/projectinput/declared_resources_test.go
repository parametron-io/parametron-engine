package projectinput

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/resourcebinding"
)

func writeDeclaredProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"project.dsl": `dsl v1.0
product First { adapter = "freecad" source_model = "primary" outputs = ["step"] }
product Second { adapter = "freecad" source_model = "alias" outputs = ["step"] }
`,
		"prm.project.json":  `{"version":"1.0","projectId":"declared","dsl":"project.dsl","resources":{"models":{"unused":"other/part.FCStd","primary":"./models/part.FCStd","alias":"models/part.FCStd","standalone":"standalone.FCStd"}}}`,
		"models/part.FCStd": "primary bytes", "other/part.FCStd": "unused bytes", "standalone.FCStd": "standalone bytes",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDeclaredInventoryRelocationAndExplicitSelection(t *testing.T) {
	var previous []resourcebinding.Binding
	for run := 0; run < 2; run++ {
		root := writeDeclaredProject(t)
		resolved, err := ResolveProject(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(resolved.ModelSelections) != 0 {
			t.Fatal("resolution invented a selection")
		}
		want := []resourcebinding.Binding{
			{ProjectID: "declared", Kind: ResourceKindModel, LogicalID: "alias", SourceCoordinate: "models/part.FCStd"},
			{ProjectID: "declared", Kind: ResourceKindModel, LogicalID: "primary", SourceCoordinate: "models/part.FCStd"},
			{ProjectID: "declared", Kind: ResourceKindModel, LogicalID: "standalone", SourceCoordinate: "standalone.FCStd"},
			{ProjectID: "declared", Kind: ResourceKindModel, LogicalID: "unused", SourceCoordinate: "other/part.FCStd"},
		}
		if !reflect.DeepEqual(resolved.ModelInventory.Bindings(), want) {
			t.Fatalf("declaration inventory: %v", resolved.ModelInventory.Bindings())
		}
		if previous != nil && !reflect.DeepEqual(previous, resolved.ModelInventory.Bindings()) {
			t.Fatal("resource identity depends on checkout root")
		}
		previous = resolved.ModelInventory.Bindings()
		fromFile, err := ResolveProject(filepath.Join(root, "prm.project.json"))
		if err != nil || !reflect.DeepEqual(resolved, fromFile) {
			t.Fatalf("entrypoint equivalence: %v", err)
		}
		ast, err := dsl.Parse(resolved.DSLPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := dsl.Validate(ast); err != nil {
			t.Fatal(err)
		}
		if err := ApplyModelMappings(ast, resolved); err != nil {
			t.Fatal(err)
		}
		for product, id := range map[string]string{"First": "primary", "Second": "alias"} {
			context := resolved.ModelSelections[product]
			if context == nil || context.Selected().LogicalID != id || !reflect.DeepEqual(context.Inventory().Bindings(), want) {
				t.Fatalf("%s selection: %v", product, context)
			}
		}
		captured, err := CaptureResources(resolved)
		if err != nil || len(captured.Models) != 4 {
			t.Fatalf("capture dropped unused/standalone zero-edge declarations: %v %v", captured, err)
		}
		if _, err := resolved.ModelInventory.LookupCoordinate("models/part.FCStd", false); !errors.Is(err, resourcebinding.ErrInvalid) {
			t.Fatalf("alias inverse lookup: %v", err)
		}
	}
}

func TestDeclaredUnusedSourceValidation(t *testing.T) {
	for _, mode := range []string{"missing", "directory", "escaping", "dangling", "inside"} {
		t.Run(mode, func(t *testing.T) {
			root := writeDeclaredProject(t)
			source := filepath.Join(root, "other", "part.FCStd")
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "directory":
				if err := os.Mkdir(source, 0o755); err != nil {
					t.Fatal(err)
				}
			case "escaping", "dangling", "inside":
				target := filepath.Join(root, "models", "part.FCStd")
				if mode == "escaping" {
					target = filepath.Join(writeDeclaredProject(t), "models", "part.FCStd")
				}
				if mode == "dangling" {
					target = filepath.Join(root, "absent")
				}
				if err := os.Symlink(target, source); err != nil {
					t.Fatal(err)
				}
			}
			_, err := ResolveProject(root)
			if mode == "inside" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var bindingErr *resourcebinding.Error
			if !errors.Is(err, resourcebinding.ErrInvalid) || !errors.As(err, &bindingErr) || bindingErr.LogicalID != "unused" || bindingErr.Field != "sourceCoordinate" {
				t.Fatalf("wrong boundary/classification: %v", err)
			}
		})
	}
}

func TestMissingDeclaredSourceDiagnosticRetainsCoordinate(t *testing.T) {
	root := writeDeclaredProject(t)
	if err := os.RemoveAll(filepath.Join(root, "other")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveProject(root)
	if !errors.Is(err, resourcebinding.ErrInvalid) || !strings.Contains(err.Error(), "other/part.FCStd") || !strings.Contains(err.Error(), "unused") {
		t.Fatalf("missing declaration coordinate: %v", err)
	}
}

func TestDeclaredSourceFailureOrdering(t *testing.T) {
	root := writeDeclaredProject(t)
	var first string
	for _, declarations := range []string{`"z":"missing/z.FCStd","a":"missing/a.FCStd"`, `"a":"missing/a.FCStd","z":"missing/z.FCStd"`} {
		raw := `{"version":"1.0","projectId":"p","dsl":"project.dsl","resources":{"models":{` + declarations + `}}}`
		if err := os.WriteFile(filepath.Join(root, "prm.project.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := ResolveProject(root)
		var failure *resourcebinding.Error
		if !errors.As(err, &failure) || failure.LogicalID != "a" || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("wrong first missing resource: %v", err)
		}
		if first != "" && first != err.Error() {
			t.Fatalf("declaration order changed failure: %s / %v", first, err)
		}
		first = err.Error()
	}
}
