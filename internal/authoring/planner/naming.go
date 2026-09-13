package planner

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"parametron/internal/authoring/dsl"
	"parametron/internal/shared/security"
)

// NamingContext is the deterministic input for file pattern interpolation.
type NamingContext struct {
	ProductName    string
	ProfileName    string
	PlanHash       string
	ResolvedParams map[string]any
	ResolvedConsts map[string]ResolvedConst
}

// ResolvedConst carries compile-time constant metadata for deterministic naming.
type ResolvedConst struct {
	TypeName string
	Value    any
}

// ResolveFilePattern resolves placeholders in a deterministic way.
// Supported placeholders:
// - {product}
// - {profile}
// - {plan_hash}
// - {param:<name>}
// - {const:<name>}
func ResolveFilePattern(pattern string, context NamingContext) (string, error) {
	var b strings.Builder
	for i := 0; i < len(pattern); {
		ch := pattern[i]
		if ch != '{' {
			b.WriteByte(ch)
			i++
			continue
		}

		closeIdx := strings.IndexByte(pattern[i+1:], '}')
		if closeIdx < 0 {
			return "", fmt.Errorf("invalid placeholder: missing closing '}'")
		}
		closeIdx += i + 1
		placeholder := pattern[i+1 : closeIdx]

		switch {
		case placeholder == "product":
			b.WriteString(context.ProductName)
		case placeholder == "profile":
			b.WriteString(context.ProfileName)
		case placeholder == "plan_hash":
			b.WriteString(context.PlanHash)
		case strings.HasPrefix(placeholder, "param:"):
			paramName := strings.TrimPrefix(placeholder, "param:")
			if paramName == "" {
				return "", fmt.Errorf("invalid placeholder '{%s}'", placeholder)
			}
			raw, ok := context.ResolvedParams[paramName]
			if !ok {
				return "", fmt.Errorf("unknown param '%s'", paramName)
			}
			formatted, err := formatPatternParamValue(raw)
			if err != nil {
				return "", fmt.Errorf("param '%s': %w", paramName, err)
			}
			b.WriteString(formatted)
		case strings.HasPrefix(placeholder, "const:"):
			constName := strings.TrimPrefix(placeholder, "const:")
			if constName == "" {
				return "", fmt.Errorf("invalid placeholder '{%s}'", placeholder)
			}
			if strings.Contains(constName, ":") {
				return "", fmt.Errorf("invalid placeholder '{%s}'", placeholder)
			}
			raw, ok := context.ResolvedConsts[constName]
			if !ok {
				return "", fmt.Errorf("unknown const '%s'", constName)
			}
			if raw.TypeName != "string" {
				return "", fmt.Errorf("const '%s' must resolve to string, got %s", constName, raw.TypeName)
			}
			s, ok := raw.Value.(string)
			if !ok {
				return "", fmt.Errorf("const '%s' has non-string runtime type %T", constName, raw.Value)
			}
			b.WriteString(s)
		default:
			return "", fmt.Errorf("unknown placeholder '{%s}'", placeholder)
		}

		i = closeIdx + 1
	}
	return b.String(), nil
}

func formatPatternParamValue(v any) (string, error) {
	switch val := v.(type) {
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return "", fmt.Errorf("non-finite number is not allowed")
		}
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case float32:
		f := float64(val)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("non-finite number is not allowed")
		}
		return strconv.FormatFloat(float64(val), 'f', -1, 32), nil
	case int:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case int8:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case int16:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case int32:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case int64:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case uint:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case uint8:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case uint16:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case uint32:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case uint64:
		return strconv.FormatFloat(float64(val), 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(val), nil
	case string:
		return val, nil
	default:
		return "", fmt.Errorf("unsupported param type %T", v)
	}
}

// ValidateFileBaseName validates that a filename is safe to use.
func ValidateFileBaseName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("resolved filename base cannot be empty")
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("resolved filename base must be relative")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("resolved filename base must not contain path separators")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("resolved filename base must not be '.' or '..'")
	}
	// Additional check for shell metacharacters
	if security.ContainsShellMetacharacter(name) {
		return fmt.Errorf("resolved filename base must not contain shell metacharacters")
	}
	return nil
}

func normalizePatternBaseName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".csv"):
		return name[:len(name)-len(".csv")]
	case strings.HasSuffix(lower, ".step"):
		return name[:len(name)-len(".step")]
	default:
		return name
	}
}

func resolveActiveFilePattern(ast *dsl.AST) (string, bool, error) {
	if ast == nil || ast.ActiveProfileName == nil {
		return "", false, nil
	}

	var activeProfile *dsl.ProfileDeclarationNode
	for _, profile := range ast.Profiles {
		if profile.Name == *ast.ActiveProfileName {
			activeProfile = profile
			break
		}
	}
	if activeProfile == nil {
		return "", false, fmt.Errorf("selected profile '%s' is not defined", *ast.ActiveProfileName)
	}

	expr, ok := activeProfile.Settings["file_pattern"]
	if !ok {
		return "", false, nil
	}

	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		v, ok := e.Value.(string)
		if !ok {
			return "", false, fmt.Errorf("profile file_pattern must be a string literal or string constant, got %T", e.Value)
		}
		return v, true, nil
	case *dsl.IdentifierExpression:
		constVal, ok := ast.ResolvedConstants[e.Name]
		if !ok {
			return "", false, fmt.Errorf("profile file_pattern identifier '%s' must reference a string constant", e.Name)
		}
		if constVal.Type.Kind != dsl.ParamTypeString {
			return "", false, fmt.Errorf("profile file_pattern constant '%s' must resolve to string, got %s", e.Name, constVal.Type)
		}
		s, ok := constVal.Value.(string)
		if !ok {
			return "", false, fmt.Errorf("profile file_pattern constant '%s' has non-string runtime type %T", e.Name, constVal.Value)
		}
		return s, true, nil
	default:
		return "", false, fmt.Errorf("profile file_pattern must be a string literal or string constant")
	}
}
