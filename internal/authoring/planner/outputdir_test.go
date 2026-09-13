package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
)

// TestResolveBaseOutputDir_PathTraversalRejection tests that path traversal attempts are rejected.
func TestResolveBaseOutputDir_PathTraversalRejection(t *testing.T) {
	tests := []struct {
		name      string
		outputDir string
	}{
		{"unix_traversal", "../../../etc/cron.d"},
		{"windows_traversal", "..\\\\..\\\\windows\\\\system32"},
		{"mixed_traversal", "../..\\\\etc/passwd"},
		{"double_dot_only", ".."},
		{"dotdot_with_subdir", "../escape"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dslContent := `
profile Prod {
    output_dir = "` + tt.outputDir + `"
}
use profile Prod

product Widget {
    param width: number = 50
}
`
			// Parse DSL
			tmpFile, err := os.CreateTemp("", "outputdir_test_*.dsl")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(withDSLVersionHeader(dslContent)); err != nil {
				t.Fatalf("failed to write DSL: %v", err)
			}
			tmpFile.Close()

			ast, err := dsl.Parse(tmpFile.Name())
			if err != nil {
				t.Fatalf("failed to parse DSL: %v", err)
			}

			// Validation should fail due to path traversal
			err = dsl.Validate(ast)
			if err == nil {
				t.Errorf("expected validation error for output_dir=%q, got nil", tt.outputDir)
			} else if !strings.Contains(err.Error(), "..") && !strings.Contains(err.Error(), "unsafe") {
				t.Errorf("expected path traversal error for output_dir=%q, got: %v", tt.outputDir, err)
			}
		})
	}
}

// TestResolveBaseOutputDir_ValidPaths tests that valid relative paths are accepted.
func TestResolveBaseOutputDir_ValidPaths(t *testing.T) {
	tests := []struct {
		name      string
		outputDir string
		expected  string
	}{
		{"simple", "output", "output/output"},
		{"nested", "build/v1", "output/build/v1"},
		{"with_underscore", "test_output", "output/test_output"},
		{"with_dash", "my-output", "output/my-output"},
		{"dot_hidden", ".hidden", "output/.hidden"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, _ := parseValidateAndPlan(t, `
profile Prod {
    output_dir = "`+tt.outputDir+`"
}
use profile Prod

product Widget {
    param width: number = 50
}
`)

			got, err := ResolveBaseOutputDir(ast, "./output")
			if err != nil {
				t.Fatalf("unexpected error for output_dir=%q: %v", tt.outputDir, err)
			}

			// Normalize for comparison
			expectedPath := filepath.Clean(tt.expected)
			gotPath := filepath.Clean(got)
			if gotPath != expectedPath {
				t.Errorf("output_dir=%q: expected %q, got %q", tt.outputDir, expectedPath, gotPath)
			}
		})
	}
}

func TestResolveBaseOutputDir_UndefinedConstantFails(t *testing.T) {
	dslContent := `
profile Prod {
    output_dir = UNDEFINED_DIR
}
use profile Prod

product Widget {
    param width: number = 50
}
`

	tmpFile, err := os.CreateTemp("", "outputdir_undefined_const_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(withDSLVersionHeader(dslContent)); err != nil {
		t.Fatalf("failed to write DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	// Parse only: this test ensures resolver still fails fast defensively
	// even if validation is skipped by the caller.
	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}

	_, err = ResolveBaseOutputDir(ast, "./output")
	if err == nil {
		t.Fatalf("expected undefined constant error, got nil")
	}
	if !strings.Contains(err.Error(), "undefined constant: UNDEFINED_DIR") {
		t.Fatalf("expected undefined constant error, got: %v", err)
	}
}
