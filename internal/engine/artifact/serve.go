package artifact

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const ArtifactListSchemaVersion = "1.0"

var ErrArtifactNotFound = errors.New("artifact not found")

// Resolver exposes deterministic artifact listing and ID-based resolution.
type Resolver interface {
	List() ([]Artifact, error)
	Resolve(id string) (ResolvedArtifact, error)
}

// ResolvedArtifact combines registered metadata with the absolute file path.
type ResolvedArtifact struct {
	Artifact     Artifact
	AbsolutePath string
}

// ListResponse is the stable JSON envelope for artifact listings.
type ListResponse struct {
	SchemaVersion string     `json:"schemaVersion"`
	Artifacts     []Artifact `json:"artifacts"`
}

type storeResolver struct {
	store Store
}

// NewResolver adapts a Store into a reusable artifact resolver.
func NewResolver(store Store) Resolver {
	if store == nil {
		return nil
	}
	return storeResolver{store: store}
}

func (r storeResolver) List() ([]Artifact, error) {
	return r.store.List()
}

func (r storeResolver) Resolve(id string) (ResolvedArtifact, error) {
	if !validArtifactID(id) {
		return ResolvedArtifact{}, ErrArtifactNotFound
	}

	meta, ok := r.store.Get(id)
	if !ok {
		return ResolvedArtifact{}, ErrArtifactNotFound
	}

	path, ok := r.store.GetPath(id)
	if !ok {
		return ResolvedArtifact{}, ErrArtifactNotFound
	}

	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return ResolvedArtifact{}, err
		}
		path = abs
	}

	return ResolvedArtifact{
		Artifact:     meta,
		AbsolutePath: path,
	}, nil
}

// NewListHandler serves deterministic artifact metadata as JSON.
func NewListHandler(resolver Resolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}

		artifacts, err := resolver.List()
		if err != nil {
			writeInternalServerError(w)
			return
		}

		writeJSON(w, http.StatusOK, ListResponse{
			SchemaVersion: ArtifactListSchemaVersion,
			Artifacts:     artifacts,
		})
	})
}

// NewFileHandler serves a registered artifact by deterministic ID.
func NewFileHandler(prefix string, resolver Resolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}

		id, ok := artifactIDFromPath(r.URL.Path, prefix)
		if !ok {
			http.NotFound(w, r)
			return
		}

		resolved, err := resolver.Resolve(id)
		if err != nil {
			if errors.Is(err, ErrArtifactNotFound) {
				http.NotFound(w, r)
				return
			}
			writeInternalServerError(w)
			return
		}

		if resolved.Artifact.MimeType != "" {
			w.Header().Set("Content-Type", resolved.Artifact.MimeType)
		}

		info, err := os.Stat(resolved.AbsolutePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			writeInternalServerError(w)
			return
		}
		if info.IsDir() {
			http.NotFound(w, r)
			return
		}

		http.ServeFile(w, r, resolved.AbsolutePath)
	})
}

func artifactIDFromPath(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}

	id := strings.TrimPrefix(path, prefix)
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return "", false
	}

	if !validArtifactID(id) {
		return "", false
	}

	return id, true
}

func validArtifactID(id string) bool {
	if len(id) != 64 {
		return false
	}

	for _, ch := range id {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		default:
			return false
		}
	}

	return true
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet)
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func writeInternalServerError(w http.ResponseWriter) {
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		writeInternalServerError(w)
		return
	}
	data = append(data, '\n')

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
