package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/jobstatus"
	"parametron/internal/engine/metadata"
)

const submissionSchemaVersion = "1.0"
const maxJobSubmissionBodyBytes int64 = 10 << 20

var errInvalidSubmission = errors.New("invalid job submission")

type SubmissionStore interface {
	Register(pkg *handoff.Package, status *jobstatus.Status) (*jobstatus.Status, bool, error)
	GetStatus(jobID string) (*jobstatus.Status, bool, error)
	UpdateStatus(jobID string, update func(*jobstatus.Status) error) error
}

type inMemorySubmissionStore struct {
	mu       sync.Mutex
	packages map[string]handoff.Package
	statuses map[string]jobstatus.Status
}

func newInMemorySubmissionStore() SubmissionStore {
	return &inMemorySubmissionStore{
		packages: make(map[string]handoff.Package),
		statuses: make(map[string]jobstatus.Status),
	}
}

func (s *inMemorySubmissionStore) Register(pkg *handoff.Package, status *jobstatus.Status) (*jobstatus.Status, bool, error) {
	if s == nil || pkg == nil || status == nil {
		return nil, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.statuses[pkg.JobID]; ok {
		return cloneStatus(&existing), false, nil
	}

	s.packages[pkg.JobID] = *clonePackage(pkg)
	s.statuses[pkg.JobID] = *cloneStatus(status)
	return cloneStatus(status), true, nil
}

func (s *inMemorySubmissionStore) GetStatus(jobID string) (*jobstatus.Status, bool, error) {
	if s == nil || strings.TrimSpace(jobID) == "" {
		return nil, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	status, ok := s.statuses[jobID]
	if !ok {
		return nil, false, nil
	}

	return cloneStatus(&status), true, nil
}

func (s *inMemorySubmissionStore) UpdateStatus(jobID string, update func(*jobstatus.Status) error) error {
	if s == nil || strings.TrimSpace(jobID) == "" || update == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.statuses[jobID]
	if !ok {
		return nil
	}

	next := cloneStatus(&current)
	if err := update(next); err != nil {
		return err
	}

	s.statuses[jobID] = *cloneStatus(next)
	return nil
}

type jobSubmissionRequest struct {
	Handoff *handoffPackagePayload `json:"handoff"`
}

type handoffPackagePayload struct {
	JobID      string                        `json:"jobID"`
	ProductKey string                        `json:"productKey"`
	CSV        planner.WriteCSVPayload       `json:"csv"`
	Manifest   manifestPayload               `json:"manifest"`
	CADRuntime *planner.RunCADRuntimePayload `json:"cadRuntime,omitempty"`
	Tables     []metadata.TableInputMetadata `json:"tables,omitempty"`
	Steps      []stepSnapshotPayload         `json:"steps"`
}

type stepSnapshotPayload struct {
	Type       planner.StepType              `json:"type"`
	CSV        *planner.WriteCSVPayload      `json:"csv,omitempty"`
	Manifest   *manifestPayload              `json:"manifest,omitempty"`
	CADRuntime *planner.RunCADRuntimePayload `json:"cadRuntime,omitempty"`
}

type manifestPayload struct {
	ProductKey             string                                      `json:"productKey,omitempty"`
	ManifestFilename       string                                      `json:"manifestFilename"`
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

type jobSubmissionResponse struct {
	SchemaVersion string          `json:"schemaVersion"`
	JobID         string          `json:"jobId"`
	ProductKey    string          `json:"productKey"`
	State         jobstatus.State `json:"state"`
}

func newJobSubmissionHandler(store SubmissionStore, queue JobQueue) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if !isJSONRequest(r.Header.Get("Content-Type")) {
			http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxJobSubmissionBodyBytes)

		pkg, status, err := decodeAndCanonicalizeSubmission(r)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		currentStatus := status
		isNewJob := true
		if store != nil {
			registeredStatus, created, err := store.Register(pkg, status)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if registeredStatus != nil {
				currentStatus = registeredStatus
			}
			isNewJob = created
		}
		if isNewJob && queue != nil {
			if err := queue.Enqueue(pkg); err != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", "/job/"+pkg.JobID)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(jobSubmissionResponse{
			SchemaVersion: submissionSchemaVersion,
			JobID:         pkg.JobID,
			ProductKey:    pkg.ProductKey,
			State:         currentStatus.State,
		})
	})
}

func decodeAndCanonicalizeSubmission(r *http.Request) (*handoff.Package, *jobstatus.Status, error) {
	defer r.Body.Close()

	var req jobSubmissionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, nil, err
		}
		return nil, nil, errInvalidSubmission
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, nil, err
		}
		return nil, nil, errInvalidSubmission
	}
	if req.Handoff == nil {
		return nil, nil, errInvalidSubmission
	}

	submitted := req.Handoff.toPackage()
	if err := handoff.ValidateTableInputs(submitted.Tables); err != nil {
		return nil, nil, errors.Join(errInvalidSubmission, err)
	}
	canonicalJob, err := job.New(strings.TrimSpace(submitted.ProductKey), submitted.Plan().Steps)
	if err != nil {
		return nil, nil, errors.Join(errInvalidSubmission, err)
	}
	if submitted.JobID != "" && submitted.JobID != canonicalJob.ID {
		return nil, nil, errors.Join(errInvalidSubmission, errors.New("submitted jobID does not match canonical job identity"))
	}

	canonicalPkg, err := handoff.FromJob(canonicalJob)
	if err != nil {
		return nil, nil, errors.Join(errInvalidSubmission, err)
	}
	canonicalPkg.Tables = handoff.CloneTableInputs(submitted.Tables)

	normalized := clonePackage(submitted)
	normalized.JobID = canonicalPkg.JobID
	if !reflect.DeepEqual(normalized, canonicalPkg) {
		return nil, nil, errInvalidSubmission
	}

	status, err := jobstatus.New(canonicalJob, time.Now().UTC())
	if err != nil {
		return nil, nil, errors.Join(errInvalidSubmission, err)
	}

	return canonicalPkg, status, nil
}

func isJSONRequest(contentType string) bool {
	mediaType := strings.TrimSpace(strings.Split(contentType, ";")[0])
	return mediaType == "application/json"
}

func (p *handoffPackagePayload) toPackage() *handoff.Package {
	if p == nil {
		return nil
	}

	steps := make([]handoff.StepSnapshot, 0, len(p.Steps))
	for _, step := range p.Steps {
		steps = append(steps, handoff.StepSnapshot{
			Type:       step.Type,
			CSV:        cloneWriteCSVPtr(step.CSV),
			Manifest:   cloneManifestPtr(step.Manifest),
			CADRuntime: cloneCADRuntimePtr(step.CADRuntime),
		})
	}

	return &handoff.Package{
		JobID:      p.JobID,
		ProductKey: p.ProductKey,
		CSV:        cloneWriteCSVPayload(p.CSV),
		Manifest:   p.Manifest.toPlanner(),
		CADRuntime: cloneCADRuntimePtr(p.CADRuntime),
		Tables:     handoff.CloneTableInputs(p.Tables),
		Steps:      steps,
	}
}

func (p manifestPayload) toPlanner() planner.WriteExportManifestPayload {
	return planner.WriteExportManifestPayload{
		ProductKey:             p.ProductKey,
		ManifestFilename:       p.ManifestFilename,
		ManifestProjectionMode: p.ManifestProjectionMode,
		SchemaVersion:          p.SchemaVersion,
		PlanHash:               p.PlanHash,
		Adapter:                p.Adapter,
		Product:                p.Product,
		SourceDocument:         p.SourceDocument,
		Inputs:                 p.Inputs,
		Values:                 cloneStringAnyMap(p.Values),
		ParameterAssignments:   append([]planner.ExportManifestParameterAssignment(nil), p.ParameterAssignments...),
		AssemblyMutations:      cloneMutationCollection(p.AssemblyMutations),
		PartMutations:          cloneMutationCollection(p.PartMutations),
		Outputs:                append([]planner.ExportManifestOutput(nil), p.Outputs...),
	}
}

func clonePackage(pkg *handoff.Package) *handoff.Package {
	if pkg == nil {
		return nil
	}

	steps := make([]handoff.StepSnapshot, 0, len(pkg.Steps))
	for _, step := range pkg.Steps {
		steps = append(steps, handoff.StepSnapshot{
			Type:       step.Type,
			CSV:        cloneWriteCSVPtr(step.CSV),
			Manifest:   cloneManifestPlannerPtr(step.Manifest),
			CADRuntime: cloneCADRuntimePtr(step.CADRuntime),
		})
	}

	return &handoff.Package{
		JobID:      pkg.JobID,
		ProductKey: pkg.ProductKey,
		CSV:        cloneWriteCSVPayload(pkg.CSV),
		Manifest:   cloneManifestPayload(pkg.Manifest),
		CADRuntime: cloneCADRuntimePtr(pkg.CADRuntime),
		Tables:     handoff.CloneTableInputs(pkg.Tables),
		Steps:      steps,
	}
}

func cloneStatus(status *jobstatus.Status) *jobstatus.Status {
	if status == nil {
		return nil
	}

	cloned := *status
	if status.StartedAt != nil {
		started := *status.StartedAt
		cloned.StartedAt = &started
	}
	if status.EndedAt != nil {
		ended := *status.EndedAt
		cloned.EndedAt = &ended
	}
	if status.Error != nil {
		errCopy := *status.Error
		cloned.Error = &errCopy
	}
	return &cloned
}

func cloneWriteCSVPtr(payload *planner.WriteCSVPayload) *planner.WriteCSVPayload {
	if payload == nil {
		return nil
	}
	cloned := cloneWriteCSVPayload(*payload)
	return &cloned
}

func cloneCADRuntimePtr(payload *planner.RunCADRuntimePayload) *planner.RunCADRuntimePayload {
	if payload == nil {
		return nil
	}
	cloned := *payload
	return &cloned
}

func cloneManifestPtr(payload *manifestPayload) *planner.WriteExportManifestPayload {
	if payload == nil {
		return nil
	}
	cloned := payload.toPlanner()
	return &cloned
}

func cloneManifestPlannerPtr(payload *planner.WriteExportManifestPayload) *planner.WriteExportManifestPayload {
	if payload == nil {
		return nil
	}
	cloned := cloneManifestPayload(*payload)
	return &cloned
}

func cloneWriteCSVPayload(payload planner.WriteCSVPayload) planner.WriteCSVPayload {
	return planner.WriteCSVPayload{
		ProductKey: payload.ProductKey,
		Filename:   payload.Filename,
		Headers:    append([]string(nil), payload.Headers...),
		Values:     cloneInterfaceSlice(payload.Values),
	}
}

func cloneManifestPayload(payload planner.WriteExportManifestPayload) planner.WriteExportManifestPayload {
	return planner.WriteExportManifestPayload{
		ProductKey:             payload.ProductKey,
		ManifestFilename:       payload.ManifestFilename,
		ManifestProjectionMode: payload.ManifestProjectionMode,
		SchemaVersion:          payload.SchemaVersion,
		PlanHash:               payload.PlanHash,
		Adapter:                payload.Adapter,
		Product:                payload.Product,
		SourceDocument:         payload.SourceDocument,
		Inputs:                 payload.Inputs,
		Values:                 cloneStringAnyMap(payload.Values),
		ParameterAssignments:   append([]planner.ExportManifestParameterAssignment(nil), payload.ParameterAssignments...),
		AssemblyMutations:      cloneMutationCollection(payload.AssemblyMutations),
		PartMutations:          cloneMutationCollection(payload.PartMutations),
		Outputs:                append([]planner.ExportManifestOutput(nil), payload.Outputs...),
	}
}

func cloneMutationCollection(in *planner.ExportManifestMutationCollection) *planner.ExportManifestMutationCollection {
	if in == nil {
		return nil
	}

	out := &planner.ExportManifestMutationCollection{
		Parameters:  append([]planner.ExportManifestParameterMutation(nil), in.Parameters...),
		Suppression: append([]planner.ExportManifestSuppressionMutation(nil), in.Suppression...),
	}
	if in.Properties != nil {
		out.Properties = make([]planner.ExportManifestPropertyMutation, len(in.Properties))
		for i, property := range in.Properties {
			out.Properties[i] = planner.ExportManifestPropertyMutation{
				Object:   property.Object,
				Property: property.Property,
				Value:    cloneInterfaceValue(property.Value),
			}
		}
	}

	return out
}

func cloneStringAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = cloneInterfaceValue(value)
	}
	return out
}

func cloneInterfaceSlice(in []interface{}) []interface{} {
	if in == nil {
		return nil
	}
	out := make([]interface{}, len(in))
	for i, value := range in {
		out[i] = cloneInterfaceValue(value)
	}
	return out
}

func cloneInterfaceValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneStringAnyMap(typed)
	case []interface{}:
		return cloneInterfaceSlice(typed)
	default:
		return typed
	}
}
