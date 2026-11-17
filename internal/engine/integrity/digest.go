// Package integrity owns the Engine-side identity and provenance chain for
// declared CAD resources: accepted source content identity, materialized
// baseline identity, runtime-reported baseline identity, and persisted
// configured-result identity.
//
// The package is a leaf: it depends only on the standard library. It contains
// no filesystem discovery, no CAD-native semantics, and no project-lock
// parsing. Callers adapt their own contracts to these values.
package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// DigestLength is the canonical SHA-256 digest length in hexadecimal characters.
const DigestLength = sha256.Size * 2

// Digest is a canonical lowercase hexadecimal SHA-256 content digest.
type Digest string

// ParseDigest validates the repository's canonical digest encoding.
func ParseDigest(value string) (Digest, error) {
	if value == "" {
		return "", fmt.Errorf("digest is required")
	}
	if value != strings.TrimSpace(value) {
		return "", fmt.Errorf("digest must not have leading or trailing whitespace")
	}
	if len(value) != DigestLength {
		return "", fmt.Errorf("digest must be %d lowercase hexadecimal characters", DigestLength)
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return "", fmt.Errorf("digest must be %d lowercase hexadecimal characters", DigestLength)
		}
	}
	return Digest(value), nil
}

// ValidateDigest reports whether value is a canonical digest.
func ValidateDigest(value string) error {
	_, err := ParseDigest(value)
	return err
}

// NewDigest returns the content digest of data.
func NewDigest(data []byte) Digest {
	sum := sha256.Sum256(data)
	return Digest(hex.EncodeToString(sum[:]))
}

// HashFile returns the content digest of the bytes of the regular file at path.
// It preserves no path information in the result.
func HashFile(path string) (Digest, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return Digest(hex.EncodeToString(hash.Sum(nil))), nil
}

// String returns the canonical digest text.
func (d Digest) String() string { return string(d) }

// IsZero reports whether the digest is unset.
func (d Digest) IsZero() bool { return d == "" }
