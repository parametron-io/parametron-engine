package metadata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/integrity"
	"parametron/internal/engine/projectinput"
)

const (
	defaultSchemaVersion = "1.0"

	// FileName is the active metadata output contract filename.
	FileName = "prm.metadata.json"
)

type Metadata struct {
	SchemaVersion string                          `json:"schemaVersion"`
	PlanHash      string                          `json:"planHash"`
	DSLHash       string                          `json:"dslHash"`
	Profile       ProfileMetadata                 `json:"profile,omitempty"`
	Tables        []TableInputMetadata            `json:"tables,omitempty"`
	ProjectInputs *projectinput.CapturedResources `json:"projectInputs,omitempty"`
	Integrity     *IntegrityMetadata              `json:"integrity,omitempty"`
	Products      []ProductMetadata               `json:"products"`
	Runtime       RuntimeMetadata                 `json:"runtime"`
	Toolchain     ToolchainMetadata               `json:"toolchain"`
}

// IntegrityMetadata records the per-resource content-identity chain of a run. It
// is operational run metadata, not a normalized record contract.
type IntegrityMetadata struct {
	Lock      string                      `json:"lock"`
	Resources []IntegrityResourceMetadata `json:"resources,omitempty"`
}

// IntegrityResourceMetadata is one declared resource's content-identity chain.
type IntegrityResourceMetadata struct {
	LogicalID                   string `json:"logicalId"`
	SourceCoordinate            string `json:"sourceCoordinate"`
	MaterializedCoordinate      string `json:"materializedCoordinate,omitempty"`
	PersistedResultCoordinate   string `json:"persistedResultCoordinate,omitempty"`
	SourceDigestSHA256          string `json:"sourceDigestSha256"`
	LockState                   string `json:"lockState"`
	BaselineDigestSHA256        string `json:"baselineDigestSha256,omitempty"`
	MaterializationStatus       string `json:"materializationStatus"`
	RuntimeBaselineDigestSHA256 string `json:"runtimeBaselineDigestSha256,omitempty"`
	RuntimeBaselineStatus       string `json:"runtimeBaselineStatus"`
	PersistedResultDigestSHA256 string `json:"persistedResultDigestSha256,omitempty"`
	ResultStatus                string `json:"resultStatus"`
}

// IntegrityFromRun converts joined run integrity into operational metadata.
func IntegrityFromRun(run *integrity.RunIntegrity) *IntegrityMetadata {
	if run == nil {
		return nil
	}
	normalized := integrity.NormalizeRunIntegrity(run)
	out := &IntegrityMetadata{Lock: string(normalized.Lock)}
	for _, resource := range normalized.Resources {
		out.Resources = append(out.Resources, IntegrityResourceMetadata{
			LogicalID:                   resource.LogicalID,
			SourceCoordinate:            resource.SourceCoordinate,
			MaterializedCoordinate:      resource.MaterializedCoordinate,
			PersistedResultCoordinate:   resource.PersistedResultCoordinate,
			SourceDigestSHA256:          resource.SourceDigest.String(),
			LockState:                   string(resource.LockState),
			BaselineDigestSHA256:        resource.MaterializedBaselineDigest.String(),
			MaterializationStatus:       string(resource.MaterializationStatus),
			RuntimeBaselineDigestSHA256: resource.RuntimeBaselineDigest.String(),
			RuntimeBaselineStatus:       string(resource.RuntimeBaselineStatus),
			PersistedResultDigestSHA256: resource.PersistedResultDigest.String(),
			ResultStatus:                string(resource.ResultStatus),
		})
	}
	return out
}

// RunIntegrityFromMetadata restores joined run integrity from operational metadata.
func RunIntegrityFromMetadata(metadata *IntegrityMetadata) *integrity.RunIntegrity {
	if metadata == nil {
		return nil
	}
	out := &integrity.RunIntegrity{Lock: integrity.LockState(metadata.Lock)}
	for _, resource := range metadata.Resources {
		out.Resources = append(out.Resources, integrity.ResourceIntegrity{
			LogicalID:                  resource.LogicalID,
			SourceCoordinate:           resource.SourceCoordinate,
			MaterializedCoordinate:     resource.MaterializedCoordinate,
			PersistedResultCoordinate:  resource.PersistedResultCoordinate,
			SourceDigest:               integrity.Digest(resource.SourceDigestSHA256),
			LockState:                  integrity.LockState(resource.LockState),
			MaterializedBaselineDigest: integrity.Digest(resource.BaselineDigestSHA256),
			MaterializationStatus:      integrity.Status(resource.MaterializationStatus),
			RuntimeBaselineDigest:      integrity.Digest(resource.RuntimeBaselineDigestSHA256),
			RuntimeBaselineStatus:      integrity.Status(resource.RuntimeBaselineStatus),
			PersistedResultDigest:      integrity.Digest(resource.PersistedResultDigestSHA256),
			ResultStatus:               integrity.Status(resource.ResultStatus),
		})
	}
	return out
}

func (m Metadata) MarshalJSON() ([]byte, error) {
	type metadataJSON struct {
		SchemaVersion string                          `json:"schemaVersion"`
		PlanHash      string                          `json:"planHash"`
		DSLHash       string                          `json:"dslHash"`
		Profile       *ProfileMetadata                `json:"profile,omitempty"`
		Tables        []TableInputMetadata            `json:"tables,omitempty"`
		ProjectInputs *projectinput.CapturedResources `json:"projectInputs,omitempty"`
		Integrity     *IntegrityMetadata              `json:"integrity,omitempty"`
		Products      []ProductMetadata               `json:"products"`
		Runtime       RuntimeMetadata                 `json:"runtime"`
		Toolchain     ToolchainMetadata               `json:"toolchain"`
	}

	payload := metadataJSON{
		SchemaVersion: m.SchemaVersion,
		PlanHash:      m.PlanHash,
		DSLHash:       m.DSLHash,
		Tables:        m.Tables,
		ProjectInputs: m.ProjectInputs,
		Integrity:     m.Integrity,
		Products:      m.Products,
		Runtime:       m.Runtime,
		Toolchain:     m.Toolchain,
	}
	if !m.Profile.IsZero() {
		profile := m.Profile
		payload.Profile = &profile
	}
	return json.Marshal(payload)
}

type ProfileMetadata struct {
	Name             string         `json:"name"`
	ResolvedSettings map[string]any `json:"resolvedSettings"`
}

type TableInputMetadata struct {
	LogicalID   string `json:"logicalId"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

func (p ProfileMetadata) IsZero() bool {
	return p.Name == "" && len(p.ResolvedSettings) == 0
}

// MarshalJSON enforces deterministic key ordering for resolved settings.
func (p ProfileMetadata) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')

	nameBytes, err := json.Marshal(p.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal profile name: %w", err)
	}
	out.WriteString(`"name":`)
	out.Write(nameBytes)
	out.WriteByte(',')
	out.WriteString(`"resolvedSettings":`)

	settingsBytes, err := marshalSortedSettings(p.ResolvedSettings)
	if err != nil {
		return nil, err
	}
	out.Write(settingsBytes)
	out.WriteByte('}')
	return out.Bytes(), nil
}

type ProductMetadata struct {
	ID        string            `json:"id"`
	Artifacts []ArtifactSummary `json:"artifacts"`
}

type ArtifactSummary struct {
	Type           string `json:"type"`
	Path           string `json:"path"`
	ChecksumSHA256 string `json:"checksumSHA256"`
	SizeBytes      int64  `json:"sizeBytes"`
}

type RetryPolicyMetadata struct {
	MaxRetries int `json:"maxRetries"`
}

type RuntimeMetadata struct {
	StartedAt          time.Time           `json:"startedAt"`
	EndedAt            time.Time           `json:"endedAt"`
	DurationMs         int64               `json:"durationMs"`
	WorkerCount        int                 `json:"workerCount"`
	RetryPolicy        RetryPolicyMetadata `json:"retryPolicy"`
	StepTimeoutSeconds int                 `json:"stepTimeoutSeconds"`
}

type ToolchainMetadata struct {
	ParametronVersion string `json:"parametronVersion"`
	GoVersion         string `json:"goVersion"`
}

type BuildInput struct {
	SchemaVersion     string
	PlanHash          string
	DSLHash           string
	ProfileName       string
	ProfileSettings   map[string]any
	Tables            []TableInputMetadata
	Artifacts         []artifact.Artifact
	ProjectInputs     *projectinput.CapturedResources
	SourceIntegrity   *IntegrityMetadata
	StartedAt         time.Time
	EndedAt           time.Time
	WorkerCount       int
	MaxRetries        int
	StepTimeoutSecs   int
	ParametronVersion string
	GoVersion         string
}

func Build(input BuildInput) Metadata {
	schemaVersion := input.SchemaVersion
	if schemaVersion == "" {
		schemaVersion = defaultSchemaVersion
	}

	products := buildProducts(input.Artifacts)
	out := Metadata{
		SchemaVersion: schemaVersion,
		PlanHash:      input.PlanHash,
		DSLHash:       input.DSLHash,
		Products:      products,
		Runtime: RuntimeMetadata{
			StartedAt:          input.StartedAt.UTC(),
			EndedAt:            input.EndedAt.UTC(),
			DurationMs:         input.EndedAt.UTC().Sub(input.StartedAt.UTC()).Milliseconds(),
			WorkerCount:        input.WorkerCount,
			RetryPolicy:        RetryPolicyMetadata{MaxRetries: input.MaxRetries},
			StepTimeoutSeconds: input.StepTimeoutSecs,
		},
		Toolchain: ToolchainMetadata{
			ParametronVersion: input.ParametronVersion,
			GoVersion:         input.GoVersion,
		},
	}

	if input.ProfileName != "" || len(input.ProfileSettings) > 0 {
		out.Profile = ProfileMetadata{
			Name:             input.ProfileName,
			ResolvedSettings: cloneMap(input.ProfileSettings),
		}
	}
	if len(input.Tables) > 0 {
		out.Tables = cloneTableInputs(input.Tables)
	}
	if input.ProjectInputs != nil {
		out.ProjectInputs = projectinput.CloneCapturedResources(input.ProjectInputs)
	}
	if input.SourceIntegrity != nil {
		out.Integrity = cloneIntegrityMetadata(input.SourceIntegrity)
	}

	return out
}

func cloneIntegrityMetadata(in *IntegrityMetadata) *IntegrityMetadata {
	if in == nil {
		return nil
	}
	out := &IntegrityMetadata{Lock: in.Lock, Resources: make([]IntegrityResourceMetadata, len(in.Resources))}
	copy(out.Resources, in.Resources)
	return out
}

func Write(runRoot string, metadata Metadata) error {
	if err := os.MkdirAll(runRoot, 0755); err != nil {
		return fmt.Errorf("failed to create run root: %w", err)
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(runRoot, FileName)
	tmpFile, err := os.CreateTemp(runRoot, ".metadata-tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary metadata file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write temporary metadata: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temporary metadata: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize metadata file: %w", err)
	}
	return nil
}

func buildProducts(artifacts []artifact.Artifact) []ProductMetadata {
	byProduct := make(map[string][]ArtifactSummary)
	for _, a := range artifacts {
		byProduct[a.ProductID] = append(byProduct[a.ProductID], ArtifactSummary{
			Type:           string(a.Type),
			Path:           a.Path,
			ChecksumSHA256: a.ChecksumSHA256,
			SizeBytes:      a.SizeBytes,
		})
	}

	productIDs := make([]string, 0, len(byProduct))
	for productID := range byProduct {
		productIDs = append(productIDs, productID)
	}
	sort.Strings(productIDs)

	products := make([]ProductMetadata, 0, len(productIDs))
	for _, productID := range productIDs {
		entries := byProduct[productID]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Path != entries[j].Path {
				return entries[i].Path < entries[j].Path
			}
			if entries[i].Type != entries[j].Type {
				return entries[i].Type < entries[j].Type
			}
			return entries[i].ChecksumSHA256 < entries[j].ChecksumSHA256
		})
		products = append(products, ProductMetadata{
			ID:        productID,
			Artifacts: entries,
		})
	}
	return products
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneTableInputs(in []TableInputMetadata) []TableInputMetadata {
	if len(in) == 0 {
		return nil
	}
	out := make([]TableInputMetadata, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].LogicalID != out[j].LogicalID {
			return out[i].LogicalID < out[j].LogicalID
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out
}

func marshalSortedSettings(settings map[string]any) ([]byte, error) {
	if len(settings) == 0 {
		return []byte(`{}`), nil
	}

	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			out.WriteByte(',')
		}
		kb, err := json.Marshal(key)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal profile key '%s': %w", key, err)
		}
		vb, err := json.Marshal(settings[key])
		if err != nil {
			return nil, fmt.Errorf("failed to marshal profile key '%s' value: %w", key, err)
		}
		out.Write(kb)
		out.WriteByte(':')
		out.Write(vb)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
