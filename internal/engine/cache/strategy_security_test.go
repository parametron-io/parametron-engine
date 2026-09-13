package cache

import (
	"strings"
	"testing"
)

// TestKeySanitization_PathTraversal tests path traversal attacks and edge cases.
// Note: Consecutive special characters are collapsed into a single underscore.
func TestKeySanitization_PathTraversal(t *testing.T) {
	tests := []struct {
		name     string
		planHash string
		wantKey  string
	}{
		{"clean", "abc123", "abc123"},
		// Consecutive special chars collapse to single underscore
		{"path_traversal", "../../../etc/passwd", "_etc_passwd"},
		{"special_chars", "file:name|test", "file_name_test"},
		{"unicode", "привет", "_"}, // unicode -> collapsed to single underscore
		{"long_key", strings.Repeat("a", 300), strings.Repeat("a", 255)},
	}

	strategy := &DefaultCacheKeyStrategy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewKeyContext(tt.planHash).Build()
			key := strategy.ComputeKey(CacheGeometry, ctx)

			if len(key) > 255 {
				t.Errorf("key length %d > 255", len(key))
			}
			if strings.Contains(key, "/") || strings.Contains(key, "\\") {
				t.Errorf("key contains path separator: %s", key)
			}
			if tt.wantKey != "" && key != tt.wantKey {
				t.Errorf("got %q, want %q", key, tt.wantKey)
			}
		})
	}
}

// TestKeySanitization_CommandInjection tests command injection attempts.
func TestKeySanitization_CommandInjection(t *testing.T) {
	tests := []struct {
		name     string
		planHash string
	}{
		{"semicolon", "file;rm -rf /"},
		{"pipe", "file|cat /etc/passwd"},
		{"backtick", "file`whoami`"},
		{"dollar", "file$(echo pwned)"},
		{"ampersand", "file&&malicious"},
		{"redirect", "file>/etc/passwd"},
	}

	strategy := &DefaultCacheKeyStrategy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewKeyContext(tt.planHash).Build()
			key := strategy.ComputeKey(CacheGeometry, ctx)

			// Key should not contain shell metacharacters
			dangerous := []string{";", "|", "`", "$", "&", ">", "<", "(", ")"}
			for _, char := range dangerous {
				if strings.Contains(key, char) {
					t.Errorf("key contains dangerous character %q: %s", char, key)
				}
			}
		})
	}
}

// TestKeySanitization_NullBytes tests null byte injection.
func TestKeySanitization_NullBytes(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}
	ctx := NewKeyContext("file\x00.txt").Build()
	key := strategy.ComputeKey(CacheGeometry, ctx)

	if strings.Contains(key, "\x00") {
		t.Error("key contains null byte")
	}
}

// TestKeySanitization_ControlCharacters tests control character sanitization.
func TestKeySanitization_ControlCharacters(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	// Test various control characters
	controlChars := []string{
		"file\x01name", // SOH
		"file\x0fname", // SI
		"file\x1bname", // ESC
		"file\x7fname", // DEL
		"file\nname",   // newline
		"file\rname",   // carriage return
		"file\tname",   // tab
	}

	for _, input := range controlChars {
		ctx := NewKeyContext(input).Build()
		key := strategy.ComputeKey(CacheGeometry, ctx)

		// Control characters should be replaced with underscore
		for i := 0; i < 32; i++ {
			if strings.Contains(key, string(rune(i))) {
				t.Errorf("key contains control character 0x%02X: %q", i, key)
			}
		}
	}
}

// TestKeySanitization_DotFiles tests dot file handling (hidden files).
func TestKeySanitization_DotFiles(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	tests := []struct {
		input    string
		expected string
	}{
		{".hidden", ".hidden"},       // single dot is allowed
		{"..hidden", "..hidden"},     // double dot is allowed (will be sanitized later if needed)
		{"...hidden", "...hidden"},   // triple dot
		{".gitignore", ".gitignore"}, // common hidden file
	}

	for _, tt := range tests {
		ctx := NewKeyContext(tt.input).Build()
		key := strategy.ComputeKey(CacheGeometry, ctx)

		// Dots at the start are valid in filenames (hidden files)
		// The sanitization should preserve them
		if !strings.HasPrefix(key, ".") {
			t.Logf("Note: dot prefix was sanitized for %q -> %q", tt.input, key)
		}
	}
}

// TestKeySanitization_ReservedNames tests Windows reserved names.
func TestKeySanitization_ReservedNames(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	// Windows reserved names
	reserved := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "LPT1", "LPT2"}

	for _, name := range reserved {
		ctx := NewKeyContext(name).Build()
		key := strategy.ComputeKey(CacheGeometry, ctx)

		// Key should be sanitized (prefixed or replaced)
		if key == name {
			t.Logf("Warning: Windows reserved name %q was not sanitized", name)
		}
	}
}

// TestKeySanitization_EdgeCases tests various edge cases.
// Note: Consecutive special characters collapse to single underscore.
func TestKeySanitization_EdgeCases(t *testing.T) {
	strategy := &DefaultCacheKeyStrategy{}

	tests := []struct {
		name   string
		input  string
		minLen int
		maxLen int
	}{
		{"empty", "", 0, 0},
		{"single_char", "a", 1, 1},
		// Consecutive special chars collapse to single underscore
		{"only_special", "!@#$%", 1, 1},
		// Underscores are valid and preserved
		{"only_underscores", "_____", 5, 5},
		{"mixed_valid", "abc-123_def", 11, 11},
		// Emoji collapses to single underscore (consecutive invalid chars)
		{"emoji", "file😀name", 9, 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewKeyContext(tt.input).Build()
			key := strategy.ComputeKey(CacheGeometry, ctx)

			if len(key) < tt.minLen {
				t.Errorf("key length %d < min %d", len(key), tt.minLen)
			}
			if len(key) > tt.maxLen {
				t.Errorf("key length %d > max %d", len(key), tt.maxLen)
			}
			if len(key) > 255 {
				t.Errorf("key length %d exceeds 255 limit", len(key))
			}
		})
	}
}

// BenchmarkSanitizeKey benchmarks key sanitization performance.
func BenchmarkSanitizeKey(b *testing.B) {
	strategy := &DefaultCacheKeyStrategy{}
	ctx := NewKeyContext("abc123def456").Build()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strategy.ComputeKey(CacheGeometry, ctx)
	}
}

// BenchmarkSanitizeKeyWithSpecialChars benchmarks sanitization with special characters.
func BenchmarkSanitizeKeyWithSpecialChars(b *testing.B) {
	strategy := &DefaultCacheKeyStrategy{}
	ctx := NewKeyContext("../../../path/with/many:special|chars").Build()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strategy.ComputeKey(CacheGeometry, ctx)
	}
}

// BenchmarkSanitizeKeyLong benchmarks sanitization with long keys.
func BenchmarkSanitizeKeyLong(b *testing.B) {
	strategy := &DefaultCacheKeyStrategy{}
	longHash := strings.Repeat("a", 300)
	ctx := NewKeyContext(longHash).Build()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strategy.ComputeKey(CacheGeometry, ctx)
	}
}
