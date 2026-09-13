// Package security provides security utilities for path validation and sanitization.
package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Common shell metacharacters that should be rejected in filenames
var shellMetacharacters = []string{
	";",    // command separator
	"|",    // pipe
	"&",    // background execution
	"`",    // command substitution
	"$",    // variable expansion
	"(",    // subshell start
	")",    // subshell end
	"<",    // input redirection
	">",    // output redirection
	"*",    // glob wildcard
	"?",    // single char wildcard
	"#",    // comment
	"\\",   // escape character
	"'",    // single quote
	"\"",   // double quote
	"\n",   // newline
	"\r",   // carriage return
	"\t",   // tab
	"\x00", // null byte
}

// ValidateSafePath validates that a path is safe for use.
// It checks:
// - Rejects absolute paths
// - Rejects any path containing ".." or starting with ".."
// - Uses filepath.Clean()
// - If baseDir provided, ensures resolved path stays within baseDir
//
// Parameters:
//   - path: The path to validate
//   - baseDir: Optional base directory for containment check (empty string skips containment)
//
// Returns an error describing the violation if the path is unsafe.
func ValidateSafePath(path string, baseDir string) error {
	// Check for empty path
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Check for null bytes (injection attempt)
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("path contains null byte")
	}

	// Reject absolute paths (security policy: relative only)
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths are not allowed: %s", path)
	}

	// Check for path traversal attempts before cleaning
	// This catches patterns like "../../../etc/passwd"
	if strings.Contains(path, "..") {
		return fmt.Errorf("path contains directory traversal sequence '..': %s", path)
	}

	// Clean the path
	cleaned := filepath.Clean(path)

	// Double-check after cleaning
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path escapes base directory: %s", path)
	}

	// Check that the cleaned path is still relative
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("cleaned path became absolute: %s", cleaned)
	}

	// Containment check if baseDir provided
	if baseDir != "" {
		// Get absolute path of baseDir
		absBase, err := filepath.Abs(baseDir)
		if err != nil {
			return fmt.Errorf("failed to resolve base directory: %w", err)
		}

		// Resolve the cleaned path relative to base
		candidatePath := filepath.Join(absBase, cleaned)
		absCandidate, err := filepath.Abs(candidatePath)
		if err != nil {
			return fmt.Errorf("failed to resolve candidate path: %w", err)
		}

		// Ensure the resolved path is within base directory
		// We check using HasPrefix but ensure the prefix ends with separator
		// to avoid false positives (e.g., "/tmp/work" matching "/tmp/work2")
		basePrefix := absBase
		if !strings.HasSuffix(basePrefix, string(filepath.Separator)) {
			basePrefix += string(filepath.Separator)
		}

		if !strings.HasPrefix(absCandidate, basePrefix) && absCandidate != absBase {
			return fmt.Errorf("path escapes base directory: %s resolves to %s", path, absCandidate)
		}
	}

	return nil
}

// ValidateFileName validates that a filename is safe.
// It checks:
// - Rejects path separators (/ or \)
// - Rejects shell metacharacters
// - Rejects "." and ".." as filenames
// - Rejects leading/trailing whitespace
//
// Returns an error describing the violation if the filename is unsafe.
func ValidateFileName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("filename cannot be empty or whitespace only")
	}

	// Check for exact match with . or ..
	if name == "." || name == ".." {
		return fmt.Errorf("filename cannot be '.' or '..'")
	}

	// Check for path separators
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("filename cannot contain path separators: %s", name)
	}

	// Check for shell metacharacters
	for _, char := range shellMetacharacters {
		if strings.Contains(name, char) {
			return fmt.Errorf("filename contains shell metacharacter '%s': %s", char, name)
		}
	}

	// Check for null bytes
	if strings.Contains(name, "\x00") {
		return fmt.Errorf("filename contains null byte")
	}

	// Check for control characters
	for _, r := range name {
		if r < 32 || r == 127 {
			return fmt.Errorf("filename contains control character")
		}
	}

	return nil
}

// ContainsShellMetacharacter checks if a string contains any shell metacharacter.
// This is a lighter check when full filename validation is not needed.
func ContainsShellMetacharacter(s string) bool {
	for _, char := range shellMetacharacters {
		if strings.Contains(s, char) {
			return true
		}
	}
	return false
}

// SanitizePathComponent sanitizes a single path component for safe use.
// It replaces unsafe characters with underscores.
func SanitizePathComponent(name string) string {
	// Replace path separators
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")

	// Replace shell metacharacters
	for _, char := range shellMetacharacters {
		name = strings.ReplaceAll(name, char, "_")
	}

	// Handle special filenames
	if name == "." || name == ".." {
		return "_"
	}

	// Trim whitespace
	name = strings.TrimSpace(name)

	// Collapse multiple underscores
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}

	// Ensure non-empty
	if name == "" {
		name = "_"
	}

	return name
}
