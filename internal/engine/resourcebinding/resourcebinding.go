// Package resourcebinding joins declared project models to Engine materialization
// coordinates. It contains no native CAD identity or discovery semantics.
package resourcebinding

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type ResourceKind string

const (
	ResourceKindDSL   ResourceKind = "dsl"
	ResourceKindModel ResourceKind = "model"
	ResourceKindTable ResourceKind = "table"
)

var ErrInvalid = errors.New("invalid declared resource binding")

type Error struct {
	LogicalID string
	Field     string
	Err       error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: resource=%q %s: %v", ErrInvalid, e.LogicalID, e.Field, e.Err)
}
func (e *Error) Unwrap() []error              { return []error{ErrInvalid, e.Err} }
func invalid(id, field, message string) error { return &Error{id, field, errors.New(message)} }

// Binding is a value snapshot. Empty MaterializedCoordinate means no completed
// materialization. Source access paths are deliberately absent from this value.
type Binding struct {
	ProjectID              string
	Kind                   ResourceKind
	LogicalID              string
	SourceCoordinate       string
	MaterializedCoordinate string
}

// Inventory is immutable through its public API; copying it cannot expose
// caller-owned slices. It is internal execution context, not a JSON contract.
type Inventory struct{ bindings []Binding }

func (i Inventory) Bindings() []Binding { return slices.Clone(i.bindings) }
func (i Inventory) Lookup(project string, kind ResourceKind, id string) (Binding, error) {
	for _, b := range i.bindings {
		if b.ProjectID == project && b.Kind == kind && b.LogicalID == id {
			return b, nil
		}
	}
	return Binding{}, invalid(id, "identity", "resource is not declared")
}
func (i Inventory) LookupCoordinate(coordinate string, materialized bool) (Binding, error) {
	var found Binding
	count := 0
	for _, b := range i.bindings {
		c := b.SourceCoordinate
		if materialized {
			c = b.MaterializedCoordinate
		}
		if c != "" && c == coordinate {
			found = b
			count++
		}
	}
	if count != 1 {
		return Binding{}, invalid("", "coordinate", fmt.Sprintf("%q matches %d resources", coordinate, count))
	}
	return found, nil
}
func New(bindings []Binding) (Inventory, error) {
	out := slices.Clone(bindings)
	slices.SortFunc(out, func(a, b Binding) int {
		if n := strings.Compare(a.ProjectID, b.ProjectID); n != 0 {
			return n
		}
		if n := strings.Compare(string(a.Kind), string(b.Kind)); n != 0 {
			return n
		}
		if n := strings.Compare(a.LogicalID, b.LogicalID); n != 0 {
			return n
		}
		if n := strings.Compare(a.SourceCoordinate, b.SourceCoordinate); n != 0 {
			return n
		}
		return strings.Compare(a.MaterializedCoordinate, b.MaterializedCoordinate)
	})
	for n, b := range out {
		if b.ProjectID == "" || strings.TrimSpace(b.ProjectID) != b.ProjectID {
			return Inventory{}, invalid(b.LogicalID, "projectId", "must be non-empty without surrounding whitespace")
		}
		if b.Kind != ResourceKindModel {
			return Inventory{}, invalid(b.LogicalID, "kind", "unsupported CAD resource kind")
		}
		if b.LogicalID == "" || strings.TrimSpace(b.LogicalID) != b.LogicalID {
			return Inventory{}, invalid(b.LogicalID, "logicalId", "must be non-empty without surrounding whitespace")
		}
		if err := ValidateCoordinate(b.SourceCoordinate); err != nil {
			return Inventory{}, &Error{b.LogicalID, "sourceCoordinate", err}
		}
		if b.MaterializedCoordinate != "" {
			if err := ValidateCoordinate(b.MaterializedCoordinate); err != nil {
				return Inventory{}, &Error{b.LogicalID, "materializedCoordinate", err}
			}
		}
		for _, previous := range out[:n] {
			if previous.ProjectID == b.ProjectID && previous.Kind == b.Kind && previous.LogicalID == b.LogicalID {
				return Inventory{}, invalid(b.LogicalID, "identity", "duplicate or conflicting logical resource binding")
			}
			if b.MaterializedCoordinate != "" && previous.MaterializedCoordinate == b.MaterializedCoordinate {
				return Inventory{}, invalid(b.LogicalID, "materializedCoordinate", "coordinate is already bound")
			}
			if b.MaterializedCoordinate != "" && previous.MaterializedCoordinate != "" && (strings.HasPrefix(b.MaterializedCoordinate, previous.MaterializedCoordinate+"/") || strings.HasPrefix(previous.MaterializedCoordinate, b.MaterializedCoordinate+"/")) {
				return Inventory{}, invalid(b.LogicalID, "materializedCoordinate", "conflicting destination ancestor")
			}
		}
	}
	return Inventory{out}, nil
}
func ValidateCoordinate(c string) error {
	if c == "" || strings.TrimSpace(c) != c || strings.ContainsAny(c, "\\\x00") || path.IsAbs(c) || path.Clean(c) != c || c == "." || c == ".." || strings.HasPrefix(c, "../") || (len(c) >= 3 && c[1] == ':' && c[2] == '/') {
		return fmt.Errorf("invalid canonical relative coordinate %q", c)
	}
	return nil
}

// Context retains the explicit selection and operational project root separately
// from stable bindings. All fields are private and all accessors return values.
type Context struct {
	inventory                   Inventory
	projectID, selectedID, root string
}

func (c *Context) Clone() *Context {
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}

func Select(i Inventory, projectID, selectedID, root string) (*Context, error) {
	if _, err := i.Lookup(projectID, ResourceKindModel, selectedID); err != nil {
		return nil, err
	}
	for _, b := range i.bindings {
		if b.ProjectID != projectID || b.MaterializedCoordinate != "" {
			return nil, invalid(b.LogicalID, "inventory", "selection requires one unmaterialized project inventory")
		}
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, invalid(selectedID, "projectRoot", "must be canonical and absolute")
	}
	return &Context{i, projectID, selectedID, root}, nil
}
func (c *Context) Inventory() Inventory {
	if c == nil {
		return Inventory{}
	}
	return c.inventory
}
func (c *Context) Selected() Binding {
	if c == nil {
		return Binding{}
	}
	b, _ := c.inventory.Lookup(c.projectID, ResourceKindModel, c.selectedID)
	return b
}

// SelectedSourcePath is operational access context, never resource identity.
func (c *Context) SelectedSourcePath() string {
	if c == nil {
		return ""
	}
	return filepath.Join(c.root, filepath.FromSlash(c.Selected().SourceCoordinate))
}
func (c *Context) SourcePath(b Binding) (string, error) {
	if c == nil {
		return "", invalid(b.LogicalID, "context", "missing context")
	}
	declared, err := c.inventory.Lookup(b.ProjectID, b.Kind, b.LogicalID)
	if err != nil {
		return "", err
	}
	if declared.SourceCoordinate != b.SourceCoordinate {
		return "", invalid(b.LogicalID, "sourceCoordinate", "conflicts with declaration")
	}
	return SourcePath(c.root, b.SourceCoordinate)
}
func SourcePath(root, coordinate string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", fmt.Errorf("project root must be canonical and absolute: %q", root)
	}
	if err := ValidateCoordinate(coordinate); err != nil {
		return "", err
	}
	source := filepath.Join(root, filepath.FromSlash(coordinate))
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("source %q escapes project root", coordinate)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source %q is not a regular file", coordinate)
	}
	return source, nil
}
