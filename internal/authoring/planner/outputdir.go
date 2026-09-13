package planner

import (
	"fmt"
	"path/filepath"
	"strings"

	"parametron/internal/authoring/dsl"
	"parametron/internal/shared/security"
)

// defaultBase is the root under which all output directories are placed.
const defaultBase = "./output"

// ResolveBaseOutputDir returns the base output directory for the current execution.
//
//   - No active profile or no output_dir setting → fallback is returned unchanged
//     (preserves backward compatibility for callers that pass "./output").
//   - Profile with output_dir → validated for path traversal, then joined under defaultBase
//     ("./output/<output_dir>") so that relative paths never escape to the repository root.
//
// Returns an error if output_dir is unsafe (contains path traversal, absolute paths, etc.)
func ResolveBaseOutputDir(ast *dsl.AST, fallback string) (string, error) {
	if ast == nil || ast.ActiveProfileName == nil {
		return fallback, nil
	}

	for _, profile := range ast.Profiles {
		if profile.Name != *ast.ActiveProfileName {
			continue
		}

		expr, ok := profile.Settings["output_dir"]
		if !ok {
			return fallback, nil
		}

		val, err := resolveProfileSettingValue(expr, ast.ResolvedConstants)
		if err != nil {
			return "", fmt.Errorf("failed to resolve profile output_dir: %w", err)
		}

		var outputDir string
		switch v := val.(type) {
		case string:
			outputDir = v
		case profileEnumValue:
			// Defensive: output_dir should be a plain string, not an enum.
			outputDir = string(v)
		default:
			return "", fmt.Errorf("profile output_dir must be a string, got %T", val)
		}

		// Validate outputDir for path traversal attacks
		if err := security.ValidateSafePath(outputDir, ""); err != nil {
			return "", fmt.Errorf("profile output_dir is unsafe: %w", err)
		}

		// Clean the path before joining
		outputDir = filepath.Clean(outputDir)

		// Join with defaultBase
		result := filepath.Join(defaultBase, outputDir)

		// Final containment check: ensure result is within defaultBase
		absBase, err := filepath.Abs(defaultBase)
		if err != nil {
			return "", fmt.Errorf("failed to resolve base directory: %w", err)
		}

		absResult, err := filepath.Abs(result)
		if err != nil {
			return "", fmt.Errorf("failed to resolve output directory: %w", err)
		}

		// Ensure absResult is within absBase
		if !isWithinDirectory(absResult, absBase) {
			return "", fmt.Errorf("profile output_dir escapes base directory: %s", outputDir)
		}

		return result, nil
	}

	return fallback, nil
}

// isWithinDirectory checks if path is within or equal to base directory.
func isWithinDirectory(path, base string) bool {
	// Ensure base ends with separator for proper prefix check
	if !strings.HasSuffix(base, string(filepath.Separator)) {
		base += string(filepath.Separator)
	}

	// Add separator to path if it doesn't have one (for exact match case)
	pathWithSep := path
	if !strings.HasSuffix(pathWithSep, string(filepath.Separator)) {
		pathWithSep += string(filepath.Separator)
	}

	return strings.HasPrefix(pathWithSep, base) || path == strings.TrimSuffix(base, string(filepath.Separator))
}

// BuildRunRoot returns the deterministic run-specific output root directory.
// runRoot = filepath.Join(baseDir, planHash)
func BuildRunRoot(baseDir, planHash string) string {
	return filepath.Join(baseDir, planHash)
}

// BuildProductDir returns the per-product output directory within a run root.
// productDir = filepath.Join(runRoot, "products", productKey)
func BuildProductDir(runRoot, productKey string) string {
	return filepath.Join(runRoot, "products", productKey)
}
