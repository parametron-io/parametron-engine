package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverResolveByID(t *testing.T) {
	t.Parallel()

	store, items := newTestStoreWithArtifacts(t)
	resolver := NewResolver(store)

	resolved, err := resolver.Resolve(items[0].ID)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if resolved.Artifact.ID != items[0].ID {
		t.Fatalf("unexpected artifact ID: got %q want %q", resolved.Artifact.ID, items[0].ID)
	}
	if !filepath.IsAbs(resolved.AbsolutePath) {
		t.Fatalf("expected absolute path, got %q", resolved.AbsolutePath)
	}
}

func TestResolverResolveUnknownIDReturnsNotFound(t *testing.T) {
	t.Parallel()

	store, _ := newTestStoreWithArtifacts(t)
	resolver := NewResolver(store)

	_, err := resolver.Resolve(strings.Repeat("a", 64))
	if !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound, got %v", err)
	}
}

func TestListHandlerReturnsDeterministicJSON(t *testing.T) {
	t.Parallel()

	store, items := newTestStoreWithArtifacts(t)
	handler := NewListHandler(NewResolver(store))

	req1 := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 responses, got %d and %d", rec1.Code, rec2.Code)
	}
	if rec1.Body.String() != rec2.Body.String() {
		t.Fatal("expected repeated list responses to be byte-for-byte identical")
	}

	var body ListResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if body.SchemaVersion != ArtifactListSchemaVersion {
		t.Fatalf("unexpected schemaVersion: %q", body.SchemaVersion)
	}
	if len(body.Artifacts) != len(items) {
		t.Fatalf("unexpected artifact count: got %d want %d", len(body.Artifacts), len(items))
	}
	if body.Artifacts[0].ProductID != "alpha" || body.Artifacts[1].ProductID != "beta" {
		t.Fatalf("unexpected deterministic ordering: %+v", body.Artifacts)
	}
	for _, item := range body.Artifacts {
		if item.Class != ArtifactClassExecutionOutput {
			t.Fatalf("artifact %s class = %q, want %q", item.ID, item.Class, ArtifactClassExecutionOutput)
		}
	}
	if !strings.Contains(rec1.Body.String(), `"class": "execution_output"`) {
		t.Fatalf("list response must include explicit class, got:\n%s", rec1.Body.String())
	}
}

func TestListHandlerRejectsUnsupportedMethods(t *testing.T) {
	t.Parallel()

	store, _ := newTestStoreWithArtifacts(t)
	handler := NewListHandler(NewResolver(store))

	req := httptest.NewRequest(http.MethodPost, "/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("unexpected Allow header: %q", got)
	}
}

func TestListHandlerReturnsInternalServerErrorOnListFailure(t *testing.T) {
	t.Parallel()

	handler := NewListHandler(failingResolver{listErr: errors.New("boom")})

	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), t.TempDir()) {
		t.Fatalf("response leaked filesystem details: %q", rec.Body.String())
	}
}

func TestFileHandlerServesArtifactContents(t *testing.T) {
	t.Parallel()

	store, items := newTestStoreWithArtifacts(t)
	handler := NewFileHandler("/artifacts/", NewResolver(store))

	req := httptest.NewRequest(http.MethodGet, "/artifacts/"+items[0].ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "a,b\n1,2\n" {
		t.Fatalf("unexpected response body: %q", body)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("expected csv content type, got %q", got)
	}
	if rec.Header().Get("Content-Length") != "8" {
		t.Fatalf("expected Content-Length 8, got %q", rec.Header().Get("Content-Length"))
	}
}

func TestFileHandlerUnknownIDReturnsNotFound(t *testing.T) {
	t.Parallel()

	store, _ := newTestStoreWithArtifacts(t)
	handler := NewFileHandler("/artifacts/", NewResolver(store))

	req := httptest.NewRequest(http.MethodGet, "/artifacts/"+strings.Repeat("a", 64), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestFileHandlerMissingBackingFileReturnsNotFound(t *testing.T) {
	t.Parallel()

	store, items := newTestStoreWithArtifacts(t)
	path, ok := store.GetPath(items[0].ID)
	if !ok {
		t.Fatalf("expected artifact path for %s", items[0].ID)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("failed to remove backing file: %v", err)
	}

	handler := NewFileHandler("/artifacts/", NewResolver(store))
	req := httptest.NewRequest(http.MethodGet, "/artifacts/"+items[0].ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestFileHandlerRejectsUnsupportedMethods(t *testing.T) {
	t.Parallel()

	store, items := newTestStoreWithArtifacts(t)
	handler := NewFileHandler("/artifacts/", NewResolver(store))

	req := httptest.NewRequest(http.MethodHead, "/artifacts/"+items[0].ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestFileHandlerTraversalStyleInputReturnsNotFound(t *testing.T) {
	t.Parallel()

	store, _ := newTestStoreWithArtifacts(t)
	handler := NewFileHandler("/artifacts/", NewResolver(store))

	req := httptest.NewRequest(http.MethodGet, "/artifacts/../../etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestFileHandlerReturnsInternalServerErrorOnResolutionFailure(t *testing.T) {
	t.Parallel()

	handler := NewFileHandler("/artifacts/", failingResolver{resolveErr: errors.New("resolve failed")})

	req := httptest.NewRequest(http.MethodGet, "/artifacts/"+strings.Repeat("b", 64), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

type failingResolver struct {
	listErr    error
	resolveErr error
}

func (r failingResolver) List() ([]Artifact, error) {
	return nil, r.listErr
}

func (r failingResolver) Resolve(id string) (ResolvedArtifact, error) {
	if r.resolveErr != nil {
		return ResolvedArtifact{}, r.resolveErr
	}
	return ResolvedArtifact{}, ErrArtifactNotFound
}

func newTestStoreWithArtifacts(t *testing.T) (*FileSystemStore, []Artifact) {
	t.Helper()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	csvPath := filepath.Join(dir, "alpha.csv")
	jsonPath := filepath.Join(dir, "beta.json")
	if err := os.WriteFile(csvPath, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("failed to write csv artifact: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte("{\"ok\":true}\n"), 0644); err != nil {
		t.Fatalf("failed to write json artifact: %v", err)
	}

	alpha, err := store.Put(context.Background(), csvPath, Artifact{
		Type:      ArtifactTypeCSV,
		ProductID: "alpha",
		StepID:    "write-csv",
		Filename:  "alpha.csv",
	})
	if err != nil {
		t.Fatalf("failed to register csv artifact: %v", err)
	}

	beta, err := store.Put(context.Background(), jsonPath, Artifact{
		Type:      ArtifactTypeJSON,
		ProductID: "beta",
		StepID:    "write-json",
		Filename:  "beta.json",
	})
	if err != nil {
		t.Fatalf("failed to register json artifact: %v", err)
	}

	return store, []Artifact{alpha, beta}
}
