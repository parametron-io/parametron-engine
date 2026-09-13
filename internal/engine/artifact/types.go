package artifact

import (
	"strings"
	"time"
)

// ArtifactType identifies the kind of produced output file.
type ArtifactType string

const (
	ArtifactTypeSTEP    ArtifactType = "step"
	ArtifactTypeCSV     ArtifactType = "csv"
	ArtifactTypePDF     ArtifactType = "pdf"
	ArtifactTypeJSON    ArtifactType = "json"
	ArtifactTypeLog     ArtifactType = "log"
	ArtifactTypeUnknown ArtifactType = "unknown"
)

// ArtifactClass identifies which artifact contract surface an artifact belongs to.
type ArtifactClass string

const (
	ArtifactClassExecutionOutput ArtifactClass = "execution_output"
	ArtifactClassVerified        ArtifactClass = "verified_artifact"
)

// NormalizeArtifactClass applies safe normalization without changing unknown values.
func NormalizeArtifactClass(class ArtifactClass) ArtifactClass {
	return ArtifactClass(strings.TrimSpace(string(class)))
}

// DefaultArtifactClass returns the backward-compatible artifact class for empty input.
func DefaultArtifactClass(class ArtifactClass) ArtifactClass {
	normalized := NormalizeArtifactClass(class)
	if normalized == "" {
		return ArtifactClassExecutionOutput
	}
	return normalized
}

// ValidArtifactClass reports whether class is one of the explicit supported classes.
func ValidArtifactClass(class ArtifactClass) bool {
	switch NormalizeArtifactClass(class) {
	case ArtifactClassExecutionOutput, ArtifactClassVerified:
		return true
	default:
		return false
	}
}

// Artifact represents a produced execution output.
//
// Path is store-relative.
type Artifact struct {
	ID             string        `json:"id"`
	Class          ArtifactClass `json:"class"`
	Type           ArtifactType  `json:"type"`
	Path           string        `json:"path"`
	Filename       string        `json:"filename"`
	MimeType       string        `json:"mimeType,omitempty"`
	SizeBytes      int64         `json:"sizeBytes,omitempty"`
	ChecksumSHA256 string        `json:"checksumSHA256,omitempty"`
	JobID          string        `json:"jobId,omitempty"`
	ProductID      string        `json:"productId,omitempty"`
	StepID         string        `json:"stepId,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
}

// Manifest is the serialized artifact index.
type Manifest struct {
	Artifacts []Artifact `json:"artifacts"`
}
