package job

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"parametron/internal/authoring/planner"
)

var (
	ErrNilPlan           = errors.New("job plan is nil")
	ErrEmptyPlan         = errors.New("job plan has no steps")
	ErrMissingProductKey = errors.New("job product key is missing")
	ErrMixedProductKeys  = errors.New("job steps belong to multiple products")
)

// Job is the engine-domain representation of one executable unit.
// It intentionally stays independent from API, persistence, and queue concerns.
type Job struct {
	ID         string
	ProductKey string
	steps      []planner.Step
}

// New constructs a job from ordered steps. When productKey is empty, it must be
// derivable consistently from the steps themselves.
func New(productKey string, steps []planner.Step) (*Job, error) {
	if len(steps) == 0 {
		return nil, ErrEmptyPlan
	}

	copiedSteps := append([]planner.Step(nil), steps...)
	resolvedProductKey := strings.TrimSpace(productKey)

	for _, step := range copiedSteps {
		stepProductKey := ProductKeyFromStep(step)
		if stepProductKey == "" {
			if resolvedProductKey == "" {
				return nil, ErrMissingProductKey
			}
			continue
		}

		if resolvedProductKey == "" {
			resolvedProductKey = stepProductKey
			continue
		}

		if resolvedProductKey != stepProductKey {
			return nil, fmt.Errorf("%w: %q != %q", ErrMixedProductKeys, resolvedProductKey, stepProductKey)
		}
	}

	if resolvedProductKey == "" {
		return nil, ErrMissingProductKey
	}

	id, err := generateID(resolvedProductKey, copiedSteps)
	if err != nil {
		return nil, err
	}

	return &Job{
		ID:         id,
		ProductKey: resolvedProductKey,
		steps:      copiedSteps,
	}, nil
}

// FromPlan constructs a job from a single-product execution plan.
func FromPlan(plan *planner.ExecutionPlan) (*Job, error) {
	if plan == nil {
		return nil, ErrNilPlan
	}
	return New("", plan.Steps)
}

// Steps returns the job steps in their original planned order.
func (j *Job) Steps() []planner.Step {
	if j == nil {
		return nil
	}
	return append([]planner.Step(nil), j.steps...)
}

// Plan returns the job as an execution plan copy for current executor compatibility.
func (j *Job) Plan() *planner.ExecutionPlan {
	if j == nil {
		return nil
	}
	return &planner.ExecutionPlan{Steps: j.Steps()}
}

// SplitPlan converts an execution plan into deterministic per-product jobs.
// Steps with no derivable product identity become standalone jobs with a stable
// positional fallback key, preserving current scheduler behavior.
func SplitPlan(plan *planner.ExecutionPlan) ([]*Job, error) {
	if plan == nil || len(plan.Steps) == 0 {
		return nil, nil
	}

	type draft struct {
		productKey string
		steps      []planner.Step
	}

	drafts := make([]draft, 0, len(plan.Steps))
	indexByProductKey := make(map[string]int)

	for _, step := range plan.Steps {
		productKey := ProductKeyFromStep(step)
		if productKey == "" {
			fallbackKey := fmt.Sprintf("product_%d", len(drafts))
			drafts = append(drafts, draft{
				productKey: fallbackKey,
				steps:      []planner.Step{step},
			})
			continue
		}

		if index, ok := indexByProductKey[productKey]; ok {
			drafts[index].steps = append(drafts[index].steps, step)
			continue
		}

		indexByProductKey[productKey] = len(drafts)
		drafts = append(drafts, draft{
			productKey: productKey,
			steps:      []planner.Step{step},
		})
	}

	jobs := make([]*Job, 0, len(drafts))
	for _, draft := range drafts {
		j, err := New(draft.productKey, draft.steps)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}

	return jobs, nil
}

// ProductKeyFromStep derives the stable product key for engine-side execution helpers.
func ProductKeyFromStep(step planner.Step) string {
	switch payload := step.Payload.(type) {
	case planner.WriteCSVPayload:
		if payload.ProductKey != "" {
			return payload.ProductKey
		}
		return strings.TrimSuffix(payload.Filename, ".csv")
	case planner.WriteExportManifestPayload:
		if payload.ProductKey != "" {
			return payload.ProductKey
		}
		if payload.Product.ID != "" {
			return payload.Product.ID
		}
		return ""
	case planner.RunCADRuntimePayload:
		return payload.ProductKey
	default:
		return ""
	}
}

type canonicalJobIdentity struct {
	ProductKey string                `json:"productKey"`
	Steps      []canonicalJobStepRef `json:"steps"`
}

type canonicalJobStepRef struct {
	Type    planner.StepType `json:"type"`
	Payload any              `json:"payload"`
}

type canonicalWriteExportManifestPayload struct {
	ProductKey             string                                      `json:"productKey,omitempty"`
	ManifestFilename       string                                      `json:"manifestFilename,omitempty"`
	ManifestProjectionMode planner.ExportManifestProjectionMode        `json:"manifestProjectionMode,omitempty"`
	SchemaVersion          string                                      `json:"schemaVersion"`
	PlanHash               string                                      `json:"planHash"`
	Adapter                string                                      `json:"adapter,omitempty"`
	Product                planner.ExportManifestProduct               `json:"product"`
	SourceDocument         string                                      `json:"sourceDocument,omitempty"`
	Inputs                 planner.ExportManifestInputs                `json:"inputs"`
	Values                 map[string]interface{}                      `json:"values"`
	ParameterAssignments   []planner.ExportManifestParameterAssignment `json:"parameterAssignments"`
	AssemblyMutations      *planner.ExportManifestMutationCollection   `json:"assemblyMutations,omitempty"`
	PartMutations          *planner.ExportManifestMutationCollection   `json:"partMutations,omitempty"`
	Outputs                []planner.ExportManifestOutput              `json:"outputs"`
}

func generateID(productKey string, steps []planner.Step) (string, error) {
	identity := canonicalJobIdentity{
		ProductKey: productKey,
		Steps:      make([]canonicalJobStepRef, 0, len(steps)),
	}

	for _, step := range steps {
		payload, err := canonicalPayload(step.Payload)
		if err != nil {
			return "", fmt.Errorf("canonicalize job step %q: %w", step.Type, err)
		}
		identity.Steps = append(identity.Steps, canonicalJobStepRef{
			Type:    step.Type,
			Payload: payload,
		})
	}

	data, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("serialize job identity: %w", err)
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalPayload(payload planner.StepPayload) (any, error) {
	switch p := payload.(type) {
	case planner.WriteCSVPayload:
		return p, nil
	case planner.RunCADRuntimePayload:
		return p, nil
	case planner.WriteExportManifestPayload:
		return canonicalWriteExportManifestPayload{
			ProductKey:             p.ProductKey,
			ManifestFilename:       p.ManifestFilename,
			ManifestProjectionMode: p.ManifestProjectionMode,
			SchemaVersion:          p.SchemaVersion,
			PlanHash:               p.PlanHash,
			Adapter:                p.Adapter,
			Product:                p.Product,
			SourceDocument:         p.SourceDocument,
			Inputs:                 p.Inputs,
			Values:                 p.Values,
			ParameterAssignments:   p.ParameterAssignments,
			AssemblyMutations:      p.AssemblyMutations,
			PartMutations:          p.PartMutations,
			Outputs:                p.Outputs,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported step payload type %T", payload)
	}
}
