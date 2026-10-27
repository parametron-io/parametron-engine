package projectmap

import (
	"errors"
	"reflect"
	"testing"
)

func TestReadRejectsDuplicateResourceDeclarations(t *testing.T) {
	for _, declaration := range []string{
		`"resources":{"models":{"a":"a.FCStd","a":"a.FCStd"}}`,
		`"resources":{"models":{"a":"a.FCStd","a":"other.FCStd"}}`,
		`"resources":{"models":{},"models":{}}`,
		`"resources":{"models":{}},"resources":{"models":{}}`,
		`"projectId":"other","resources":{"models":{}}`,
	} {
		_, err := Read([]byte(`{"version":"1.0","projectId":"p","dsl":"project.dsl",` + declaration + `}`))
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected validation error for %s: %v", declaration, err)
		}
	}
}

func TestReadResourceErrorOrderingIgnoresDeclarationOrder(t *testing.T) {
	for _, declarations := range [][2]string{
		{`"z":"../z","a":"../a"`, `"a":"../a","z":"../z"`},
		{`"z":"z","z":"other","a":"a","a":"other"`, `"a":"a","a":"other","z":"z","z":"other"`},
	} {
		var first error
		for _, models := range declarations {
			_, err := Read([]byte(`{"version":"1.0","projectId":"p","dsl":"project.dsl","resources":{"models":{` + models + `}}}`))
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected validation error: %v", err)
			}
			if first != nil && first.Error() != err.Error() {
				t.Fatalf("order-dependent errors: %v / %v", first, err)
			}
			first = err
		}
	}
	project, err := Read([]byte(`{"version":"1.0","projectId":"p","dsl":"project.dsl","resources":{"models":{"Part":"./models/nested/../part.FCStd","part":"models/part.FCStd"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(project.Resources.Models, map[string]string{"Part": "models/part.FCStd", "part": "models/part.FCStd"}) {
		t.Fatalf("case-sensitive aliases lost: %v", project.Resources.Models)
	}
}
