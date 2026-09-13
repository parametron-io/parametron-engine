package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"parametron/internal/authoring/dsl"
)

type profileEnumValue string

// ComputePlanHash returns the deterministic hash for an execution plan.
// When no profile is selected, hashing behavior remains identical to legacy plan-only hashing.
func ComputePlanHash(plan *ExecutionPlan, ast *dsl.AST) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("failed to serialize plan: %w", err)
	}

	// Backward compatibility: keep exact previous hash when no selected profile exists.
	if ast == nil || ast.ActiveProfileName == nil {
		hash := sha256.Sum256(data)
		return hex.EncodeToString(hash[:]), nil
	}

	profileSettings, err := resolveActiveProfileSettings(ast)
	if err != nil {
		return "", err
	}
	profileSignature, err := BuildProfileResolvedSignature(*ast.ActiveProfileName, profileSettings)
	if err != nil {
		return "", err
	}

	hasher := sha256.New()
	hasher.Write(data)
	hasher.Write([]byte{'\n'})
	hasher.Write([]byte(profileSignature))
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// BuildProfileResolvedSignature returns a deterministic string for resolved profile settings.
func BuildProfileResolvedSignature(activeProfileName string, settings map[string]interface{}) (string, error) {
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var b strings.Builder
	b.WriteString("profile:")
	b.WriteString(activeProfileName)
	b.WriteByte('\n')

	for _, key := range keys {
		canonicalValue, err := canonicalProfileValue(settings[key])
		if err != nil {
			return "", fmt.Errorf("profile setting '%s': %w", key, err)
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(canonicalValue)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func resolveActiveProfileSettings(ast *dsl.AST) (map[string]interface{}, error) {
	if ast.ActiveProfileName == nil {
		return nil, nil
	}

	var activeProfile *dsl.ProfileDeclarationNode
	for _, profile := range ast.Profiles {
		if profile.Name == *ast.ActiveProfileName {
			activeProfile = profile
			break
		}
	}
	if activeProfile == nil {
		return nil, fmt.Errorf("selected profile '%s' is not defined", *ast.ActiveProfileName)
	}

	resolved := make(map[string]interface{}, len(activeProfile.Settings))
	for key, expr := range activeProfile.Settings {
		value, err := resolveProfileSettingValue(expr, ast.ResolvedConstants)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve profile setting '%s': %w", key, err)
		}
		resolved[key] = value
	}

	return resolved, nil
}

// ResolveActiveProfileSettings returns resolved key/value settings for the selected profile.
func ResolveActiveProfileSettings(ast *dsl.AST) (map[string]interface{}, error) {
	return resolveActiveProfileSettings(ast)
}

func resolveProfileSettingValue(expr dsl.ExpressionNode, constScope map[string]dsl.ConstantValue) (interface{}, error) {
	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		return e.Value, nil
	case *dsl.IdentifierExpression:
		constVal, ok := constScope[e.Name]
		if !ok {
			return "", fmt.Errorf("undefined constant: %s", e.Name)
		}
		if constVal.Type.Kind == dsl.ParamTypeEnum {
			enumValue, ok := constVal.Value.(string)
			if !ok {
				return nil, fmt.Errorf("invalid enum constant value type %T", constVal.Value)
			}
			return profileEnumValue(enumValue), nil
		}
		return constVal.Value, nil
	case *dsl.ArrayExpression:
		values := make([]interface{}, 0, len(e.Elements))
		for _, elem := range e.Elements {
			val, err := resolveProfileSettingValue(elem, constScope)
			if err != nil {
				return nil, err
			}
			values = append(values, val)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("profile setting value must be a literal, identifier, or array")
	}
}

func canonicalProfileValue(v interface{}) (string, error) {
	switch val := v.(type) {
	case string:
		encoded, err := json.Marshal(val)
		if err != nil {
			return "", fmt.Errorf("failed to serialize string value: %w", err)
		}
		return string(encoded), nil
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32), nil
	case int:
		return strconv.FormatInt(int64(val), 10), nil
	case int8:
		return strconv.FormatInt(int64(val), 10), nil
	case int16:
		return strconv.FormatInt(int64(val), 10), nil
	case int32:
		return strconv.FormatInt(int64(val), 10), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case uint:
		return strconv.FormatUint(uint64(val), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(val), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(val), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(val), 10), nil
	case uint64:
		return strconv.FormatUint(val, 10), nil
	case bool:
		return strconv.FormatBool(val), nil
	case profileEnumValue:
		return string(val), nil
	case []interface{}:
		return canonicalProfileArrayValue(val)
	case []string:
		items := make([]interface{}, 0, len(val))
		for _, item := range val {
			items = append(items, item)
		}
		return canonicalProfileArrayValue(items)
	default:
		return "", fmt.Errorf("unsupported profile setting value type %T", v)
	}
}

func canonicalProfileArrayValue(values []interface{}) (string, error) {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		canonical, err := canonicalProfileValue(value)
		if err != nil {
			return "", err
		}
		parts = append(parts, canonical)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}
