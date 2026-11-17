package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDigestAcceptsCanonicalSHA256(t *testing.T) {
	want := strings.Repeat("0123456789abcdef", 4)
	got, err := ParseDigest(want)
	if err != nil {
		t.Fatalf("ParseDigest(%q) returned error: %v", want, err)
	}
	if got.String() != want {
		t.Fatalf("ParseDigest(%q).String() = %q, want the canonical value", want, got.String())
	}
	if err := ValidateDigest(want); err != nil {
		t.Fatalf("ValidateDigest(%q) returned error: %v", want, err)
	}
}

func TestParseDigestRejectsNonCanonicalEncodings(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "too short", value: strings.Repeat("a", DigestLength-1)},
		{name: "too long", value: strings.Repeat("a", DigestLength+1)},
		{name: "uppercase characters", value: strings.Repeat("A", DigestLength)},
		{name: "non hexadecimal characters", value: strings.Repeat("g", DigestLength)},
		{name: "leading whitespace", value: " " + strings.Repeat("a", DigestLength)},
		{name: "trailing whitespace", value: strings.Repeat("a", DigestLength) + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseDigest(tc.value); err == nil {
				t.Fatalf("ParseDigest(%q) accepted a non-canonical digest", tc.value)
			}
			if err := ValidateDigest(tc.value); err == nil {
				t.Fatalf("ValidateDigest(%q) accepted a non-canonical digest", tc.value)
			}
		})
	}
}

func TestNewDigestIsContentAddressed(t *testing.T) {
	content := []byte("byte-preserving source content")
	got := NewDigest(content)

	sum := sha256.Sum256(content)
	want := Digest(hex.EncodeToString(sum[:]))
	if got != want {
		t.Fatalf("NewDigest(content) = %q, want %q", got, want)
	}
	if got == NewDigest([]byte("different content")) {
		t.Fatal("NewDigest collapsed distinct content identities")
	}
}

func TestHashFileIsPathFreeAndContentAddressed(t *testing.T) {
	content := []byte("shared content identity")
	first := filepath.Join(t.TempDir(), "first.FCStd")
	second := filepath.Join(t.TempDir(), "second.FCStd")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	firstDigest, err := HashFile(first)
	if err != nil {
		t.Fatalf("HashFile(first) returned error: %v", err)
	}
	secondDigest, err := HashFile(second)
	if err != nil {
		t.Fatalf("HashFile(second) returned error: %v", err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("equivalent bytes at different paths produced %q and %q", firstDigest, secondDigest)
	}
	if string(firstDigest) != NewDigest(content).String() {
		t.Fatalf("HashFile digest = %q, want NewDigest result %q", firstDigest, NewDigest(content))
	}
	if strings.Contains(firstDigest.String(), filepath.Base(first)) {
		t.Fatalf("HashFile digest leaked path material: %q", firstDigest)
	}
}

func TestHashFileRejectsMissingFile(t *testing.T) {
	if _, err := HashFile(filepath.Join(t.TempDir(), "absent.FCStd")); err == nil {
		t.Fatal("HashFile accepted a missing file")
	}
}

func TestDigestIsZero(t *testing.T) {
	if !Digest("").IsZero() {
		t.Fatal("empty digest is not reported as zero")
	}
	if Digest(strings.Repeat("0", DigestLength)).IsZero() {
		t.Fatal("canonical all-zero digest is reported as unset")
	}
}
