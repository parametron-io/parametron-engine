package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/jobstatus"
)

const jobArtifactsSchemaVersion = "1.0"

type jobRouteKind int

const (
	jobRouteUnknown jobRouteKind = iota
	jobRouteStatus
	jobRouteArtifacts
)

type jobArtifactsResponse struct {
	SchemaVersion string              `json:"schemaVersion"`
	JobID         string              `json:"jobId"`
	Artifacts     []artifact.Artifact `json:"artifacts"`
}

func newJobScopedHandler(store SubmissionStore, artifactStore artifact.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jobID, route, ok := jobRouteFromPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		switch route {
		case jobRouteStatus:
			handleJobStatus(w, r, store, jobID)
		case jobRouteArtifacts:
			handleJobArtifacts(w, r, store, artifactStore, jobID)
		default:
			http.NotFound(w, r)
		}
	})
}

func newJobArtifactsHandler(store SubmissionStore, artifactStore artifact.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jobID, route, ok := jobRouteFromPath(r.URL.Path)
		if !ok || route != jobRouteArtifacts {
			http.NotFound(w, r)
			return
		}

		handleJobArtifacts(w, r, store, artifactStore, jobID)
	})
}

func handleJobStatus(w http.ResponseWriter, r *http.Request, store SubmissionStore, jobID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	status, found, err := store.GetStatus(jobID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
}

func handleJobArtifacts(w http.ResponseWriter, r *http.Request, store SubmissionStore, artifactStore artifact.Store, jobID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	status, found, err := store.GetStatus(jobID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	var artifacts []artifact.Artifact
	if status.State != jobstatus.StateSucceeded {
		artifacts = []artifact.Artifact{}
	} else {
		artifacts, err = listJobArtifacts(artifactStore, jobID, status.ProductKey)
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJobArtifactsJSON(w, http.StatusOK, jobArtifactsResponse{
		SchemaVersion: jobArtifactsSchemaVersion,
		JobID:         jobID,
		Artifacts:     artifacts,
	})
}

func jobRouteFromPath(path string) (string, jobRouteKind, bool) {
	const prefix = "/job/"

	if !strings.HasPrefix(path, prefix) {
		return "", jobRouteUnknown, false
	}

	rest := strings.TrimPrefix(path, prefix)
	if strings.TrimSpace(rest) == "" {
		return "", jobRouteUnknown, false
	}

	parts := strings.Split(rest, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", jobRouteUnknown, false
	}

	switch len(parts) {
	case 1:
		return parts[0], jobRouteStatus, true
	case 2:
		if parts[1] == "artifacts" {
			return parts[0], jobRouteArtifacts, true
		}
	}

	return "", jobRouteUnknown, false
}

func listJobArtifacts(store artifact.Store, jobID, productKey string) ([]artifact.Artifact, error) {
	if store == nil {
		return []artifact.Artifact{}, nil
	}

	items, err := store.List()
	if err != nil {
		return nil, err
	}

	filtered := make([]artifact.Artifact, 0, len(items))
	for _, item := range items {
		if item.JobID != "" {
			if item.JobID != jobID {
				continue
			}
		} else if item.ProductID != productKey {
			continue
		}
		if item.ProductID == productKey {
			filtered = append(filtered, cloneArtifact(item))
		}
	}

	sortJobArtifacts(filtered)
	return filtered, nil
}

func sortJobArtifacts(artifacts []artifact.Artifact) {
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].JobID != artifacts[j].JobID {
			return artifacts[i].JobID < artifacts[j].JobID
		}
		if artifacts[i].ProductID != artifacts[j].ProductID {
			return artifacts[i].ProductID < artifacts[j].ProductID
		}
		if artifacts[i].StepID != artifacts[j].StepID {
			return artifacts[i].StepID < artifacts[j].StepID
		}
		if artifacts[i].Class != artifacts[j].Class {
			return artifacts[i].Class < artifacts[j].Class
		}
		if artifacts[i].Path != artifacts[j].Path {
			return artifacts[i].Path < artifacts[j].Path
		}
		if artifacts[i].Filename != artifacts[j].Filename {
			return artifacts[i].Filename < artifacts[j].Filename
		}
		if artifacts[i].Type != artifacts[j].Type {
			return artifacts[i].Type < artifacts[j].Type
		}
		if artifacts[i].ChecksumSHA256 != artifacts[j].ChecksumSHA256 {
			return artifacts[i].ChecksumSHA256 < artifacts[j].ChecksumSHA256
		}
		return artifacts[i].ID < artifacts[j].ID
	})
}

func cloneArtifact(item artifact.Artifact) artifact.Artifact {
	return item
}

func writeJobArtifactsJSON(w http.ResponseWriter, status int, value jobArtifactsResponse) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	data = append(data, '\n')

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
