package security

import (
	"runtime"
	"strings"
	"testing"
)

func TestValidateSafePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		baseDir   string
		wantError bool
		errMsg    string
	}{
		// Valid paths
		{"valid_simple", "output", "", false, ""},
		{"valid_with_subdir", "build/v1", "", false, ""},
		{"valid_underscore", "test_output", "", false, ""},
		{"valid_dash", "my-output", "", false, ""},
		{"valid_dot_prefix", ".hidden", "", false, ""},

		// Path traversal attacks
		{"traversal_unix", "../../../etc/cron.d", "", true, ".."},
		{"traversal_windows", "..\\..\\windows\\system32", "", true, ".."},
		{"traversal_mixed", "../..\\etc/passwd", "", true, ".."},
		{"traversal_deep", "../../../../../../../../etc/passwd", "", true, ".."},
		{"traversal_encoded", "..%2f..%2fetc", "", true, ".."},

		// Absolute paths
		{"absolute_unix", "/etc/passwd", "", true, "absolute"},
		{"absolute_current", "/tmp/test", "", true, "absolute"},

		// Empty/null
		{"empty", "", "", true, "empty"},
		{"whitespace_only", "   ", "", true, "empty"},
		{"null_byte", "test\x00file", "", true, "null"},

		// Special sequences
		{"single_dot", "./file", "", false, ""},
		{"double_dot", "..", "", true, ".."},
		{"dotdot_prefix", "../escape", "", true, ".."},
		{"dotdot_embedded", "path/../escape", "", true, ".."},

		// With base directory containment check
		{"contained_valid", "subdir/file", "/tmp/work", false, ""},
		{"contained_traversal", "../escape", "/tmp/work", true, ".."},
		{"contained_deep", "a/b/../../../c", "/tmp/work", true, ".."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSafePath(tt.path, tt.baseDir)
			if tt.wantError {
				if err == nil {
					t.Errorf("ValidateSafePath(%q, %q) expected error, got nil", tt.path, tt.baseDir)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) &&
					!containsAny(err.Error(), []string{"path escapes", "null byte", "absolute", "empty"}) {
					// For some error messages, we check for general categories
					t.Errorf("ValidateSafePath(%q, %q) error = %v, want containing %q",
						tt.path, tt.baseDir, err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateSafePath(%q, %q) unexpected error: %v", tt.path, tt.baseDir, err)
				}
			}
		})
	}
}

func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

func TestValidateSafePath_Containment(t *testing.T) {
	// Create a temporary directory for testing
	if runtime.GOOS == "windows" {
		t.Skip("Skipping containment test on Windows")
	}

	tests := []struct {
		name      string
		baseDir   string
		path      string
		wantError bool
	}{
		{"valid_subdir", "/tmp/work", "subdir", false},
		{"valid_nested", "/tmp/work", "a/b/c", false},
		{"escape_via_traversal", "/tmp/work", "../escape", true},
		{"escape_via_deep_traversal", "/tmp/work", "a/b/../../../c", true},
		{"current_dir", "/tmp/work", ".", false},
		{"empty_subdir", "/tmp/work", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSafePath(tt.path, tt.baseDir)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for base=%q path=%q", tt.baseDir, tt.path)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for base=%q path=%q: %v", tt.baseDir, tt.path, err)
				}
			}
		})
	}
}

func TestValidateFileName(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		wantError bool
		errType   string
	}{
		// Valid filenames
		{"valid_simple", "file.txt", false, ""},
		{"valid_with_dash", "my-file.txt", false, ""},
		{"valid_with_underscore", "my_file.txt", false, ""},
		{"valid_numbers", "file123.txt", false, ""},
		{"valid_dot_prefix", ".gitignore", false, ""},

		// Path separators
		{"path_sep_unix", "path/file.txt", true, "separator"},
		{"path_sep_windows", "path\\file.txt", true, "separator"},
		{"absolute_unix", "/etc/passwd", true, "separator"},
		{"absolute_windows", "C:\\file.txt", true, "separator"},

		// Shell metacharacters
		{"semicolon", "file;rm -rf /", true, "metacharacter"},
		{"pipe", "file|cat", true, "metacharacter"},
		{"ampersand", "file&whoami", true, "metacharacter"},
		{"backtick", "file`cmd`", true, "metacharacter"},
		{"dollar", "file$(whoami)", true, "metacharacter"},
		{"parens", "file()", true, "metacharacter"},
		{"redirect_in", "file<input", true, "metacharacter"},
		{"redirect_out", "file>output", true, "metacharacter"},
		{"asterisk", "file*.txt", true, "metacharacter"},
		{"question", "file?.txt", true, "metacharacter"},
		{"hash", "file#comment", true, "metacharacter"},
		{"backslash", "file\\name", true, "separator"},
		{"quote_single", "file'name", true, "metacharacter"},
		{"quote_double", `file"name`, true, "metacharacter"},
		{"newline", "file\nname", true, "metacharacter"},

		// Special names
		{"dot_only", ".", true, "special"},
		{"dotdot_only", "..", true, "special"},

		// Empty/whitespace
		{"empty", "", true, "empty"},
		{"whitespace_only", "   ", true, "empty"},
		{"null_byte", "file\x00.txt", true, "null"},

		// Control characters
		{"tab", "file\tname", true, "metacharacter"},
		{"carriage_return", "file\rname", true, "metacharacter"},
		{"bell", "file\x07name", true, "control"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFileName(tt.filename)
			if tt.wantError {
				if err == nil {
					t.Errorf("ValidateFileName(%q) expected error, got nil", tt.filename)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateFileName(%q) unexpected error: %v", tt.filename, err)
				}
			}
		})
	}
}

func TestContainsShellMetacharacter(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"clean", false},
		{"file.txt", false},
		{"file;cmd", true},
		{"file|cmd", true},
		{"file`cmd`", true},
		{"file$(cmd)", true},
		{"file>out", true},
		{"file*", true},
		{"file?", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ContainsShellMetacharacter(tt.input)
			if got != tt.want {
				t.Errorf("ContainsShellMetacharacter(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizePathComponent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"file.txt", "file.txt"},
		{"file;rm -rf /", "file_rm -rf _"},
		{"path/file", "path_file"},
		{"file|pipe", "file_pipe"},
		{"file`backtick", "file_backtick"},
		{".", "_"},
		{"..", "_"},
		{"  spaces  ", "spaces"},
		{"", "_"},
		{"a__b__c", "a_b_c"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizePathComponent(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizePathComponent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestValidateSafePath_RealWorldAttacks tests known path traversal attack patterns
func TestValidateSafePath_RealWorldAttacks(t *testing.T) {
	attacks := []struct {
		name string
		path string
	}{
		{"null_byte_injection", "file.txt\x00.php"},
		{"mixed_separators", "../..\\../etc/passwd"},
		{"dot_dot_with_extra_dots", "....//....//etc/passwd"},
		{"traversal_with_null", "../../etc/passwd\x00"},
	}

	for _, attack := range attacks {
		t.Run(attack.name, func(t *testing.T) {
			err := ValidateSafePath(attack.path, "")
			if err == nil {
				t.Errorf("ValidateSafePath should reject attack pattern: %s", attack.path)
			}
		})
	}
}

// BenchmarkValidateSafePath benchmarks the path validation
func BenchmarkValidateSafePath(b *testing.B) {
	paths := []string{
		"valid/path/to/file",
		"../../../etc/passwd",
		"/absolute/path",
		"normal_file.txt",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_ = ValidateSafePath(path, "")
	}
}

// BenchmarkValidateFileName benchmarks filename validation
func BenchmarkValidateFileName(b *testing.B) {
	names := []string{
		"file.txt",
		"file;rm -rf /",
		"path/file",
		"clean_name",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := names[i%len(names)]
		_ = ValidateFileName(name)
	}
}
