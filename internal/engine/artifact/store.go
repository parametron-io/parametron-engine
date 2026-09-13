package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseDir is the default artifact store root.
	DefaultBaseDir = "output/artifacts"

	storeFilesDir  = "files"
	manifestName   = "manifest.json"
	defaultBufSize = 32 * 1024
)

// Store defines artifact persistence operations.
type Store interface {
	Put(ctx context.Context, srcPath string, meta Artifact) (Artifact, error)
	// RecordExisting registers an already-written file without copying it.
	// The file at absPath must already exist within the store's base directory.
	RecordExisting(ctx context.Context, absPath string, meta Artifact) (Artifact, error)
	Get(id string) (Artifact, bool)
	GetPath(id string) (string, bool)
	List() ([]Artifact, error)
	WriteManifest() error
}

// ExistingRecord describes an already-written artifact file to register.
type ExistingRecord struct {
	AbsPath string
	Meta    Artifact
}

// RecordExistingBatchError identifies which batch element failed to register.
type RecordExistingBatchError struct {
	Index int
	Err   error
}

func (e *RecordExistingBatchError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Err.Error()
}

func (e *RecordExistingBatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// FileSystemStore stores artifacts on local filesystem.
// It is safe for concurrent use.
type FileSystemStore struct {
	baseDir   string
	mu        sync.RWMutex
	artifacts map[string]Artifact
}

// NewFileSystemStore constructs a filesystem-backed artifact store.
func NewFileSystemStore(baseDir string) *FileSystemStore {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = DefaultBaseDir
	}
	return &FileSystemStore{
		baseDir:   baseDir,
		artifacts: make(map[string]Artifact),
	}
}

// BaseDir returns the artifact store root directory.
func (s *FileSystemStore) BaseDir() string {
	if s == nil {
		return ""
	}
	return s.baseDir
}

// DeterministicID computes an artifact identifier from stable inputs.
//
// If jobID is provided, it becomes part of the ownership boundary.
// If checksumSHA256 is provided, it is preferred over filename.
func DeterministicID(jobID, productID, stepID string, class ArtifactClass, typ ArtifactType, filename, checksumSHA256 string) string {
	value := filename
	if checksumSHA256 != "" {
		value = checksumSHA256
	}
	class = DefaultArtifactClass(class)

	// Include explicit field keys to keep the serialization stable and unambiguous.
	stable := fmt.Sprintf(
		"job=%s|product=%s|step=%s|class=%s|type=%s|value=%s",
		jobID,
		productID,
		stepID,
		string(class),
		string(typ),
		value,
	)
	sum := sha256.Sum256([]byte(stable))
	return hex.EncodeToString(sum[:])
}

// Put copies a source file into the artifact store and returns recorded metadata.
func (s *FileSystemStore) Put(ctx context.Context, srcPath string, meta Artifact) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}

	class, err := normalizeRegisteredArtifactClass(meta.Class)
	if err != nil {
		return Artifact{}, err
	}
	meta.Class = class

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to stat source artifact: %w", err)
	}
	if srcInfo.IsDir() {
		return Artifact{}, fmt.Errorf("source artifact path is a directory: %s", srcPath)
	}

	if meta.Type == "" {
		meta.Type = ArtifactTypeUnknown
	}

	filename := meta.Filename
	if filename == "" {
		filename = filepath.Base(srcPath)
	} else if err := validateFilename(filename); err != nil {
		return Artifact{}, err
	}
	meta.Filename = filename

	if meta.MimeType == "" {
		meta.MimeType = mime.TypeByExtension(filepath.Ext(filename))
	}

	if err := os.MkdirAll(filepath.Join(s.baseDir, storeFilesDir), 0755); err != nil {
		return Artifact{}, fmt.Errorf("failed to create artifact store directory: %w", err)
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to open source artifact: %w", err)
	}
	defer srcFile.Close()

	// Initial ID can be computed from filename; checksum-based ID is finalized after copy.
	if meta.ID == "" {
		meta.ID = DeterministicID(meta.JobID, meta.ProductID, meta.StepID, meta.Class, meta.Type, meta.Filename, meta.ChecksumSHA256)
	}

	filesDir := filepath.Join(s.baseDir, storeFilesDir)
	tmpFile, err := os.CreateTemp(filesDir, ".artifact-tmp-*")
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to create artifact temp destination: %w", err)
	}
	tmpPath := tmpFile.Name()

	sum := sha256.New()
	w := io.MultiWriter(tmpFile, sum)
	written, copyErr := copyWithContext(ctx, w, srcFile)
	closeErr := tmpFile.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return Artifact{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return Artifact{}, fmt.Errorf("failed to close artifact destination: %w", closeErr)
	}

	checksum := hex.EncodeToString(sum.Sum(nil))
	meta.ChecksumSHA256 = checksum
	meta.SizeBytes = written

	// Prefer checksum-driven ID when available.
	checksumID := DeterministicID(meta.JobID, meta.ProductID, meta.StepID, meta.Class, meta.Type, meta.Filename, checksum)
	meta.ID = checksumID

	finalName := fmt.Sprintf("%s_%s", meta.ID, filepath.Base(meta.Filename))
	finalRelPath := filepath.Join(storeFilesDir, finalName)
	finalDstPath := filepath.Join(s.baseDir, finalRelPath)
	if err := os.Rename(tmpPath, finalDstPath); err != nil {
		_ = os.Remove(tmpPath)
		return Artifact{}, fmt.Errorf("failed to finalize artifact path: %w", err)
	}

	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	} else {
		meta.CreatedAt = meta.CreatedAt.UTC()
	}

	meta.Path = filepath.ToSlash(finalRelPath)

	s.mu.Lock()
	s.artifacts[meta.ID] = meta
	s.mu.Unlock()

	return meta, nil
}

// GetPath resolves an artifact ID to its absolute filesystem path.
func (s *FileSystemStore) GetPath(id string) (string, bool) {
	s.mu.RLock()
	artifact, ok := s.artifacts[id]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	path := filepath.Join(s.baseDir, filepath.FromSlash(artifact.Path))
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, true
	}
	return abs, true
}

// Get resolves an artifact ID to its recorded metadata.
func (s *FileSystemStore) Get(id string) (Artifact, bool) {
	s.mu.RLock()
	artifact, ok := s.artifacts[id]
	s.mu.RUnlock()
	return artifact, ok
}

// List returns all tracked artifacts in deterministic order.
func (s *FileSystemStore) List() ([]Artifact, error) {
	s.mu.RLock()
	artifacts := make([]Artifact, 0, len(s.artifacts))
	for _, artifact := range s.artifacts {
		artifacts = append(artifacts, artifact)
	}
	s.mu.RUnlock()
	sortArtifacts(artifacts)
	return artifacts, nil
}

// RecordExisting registers a file that has already been written to disk by an adapter.
// The file at absPath must exist and reside within the store's base directory; it is not
// copied—only its checksum, size, and store-relative path are recorded.
// Calling RecordExisting with the same content is idempotent.
func (s *FileSystemStore) RecordExisting(ctx context.Context, absPath string, meta Artifact) (Artifact, error) {
	registered, err := s.prepareExistingArtifact(ctx, absPath, meta)
	if err != nil {
		return Artifact{}, err
	}

	s.mu.Lock()
	s.artifacts[registered.ID] = registered
	s.mu.Unlock()

	return registered, nil
}

// RecordExistingBatch registers already-written files atomically with respect to the
// store's observable in-memory index.
func (s *FileSystemStore) RecordExistingBatch(ctx context.Context, records []ExistingRecord) ([]Artifact, error) {
	registered := make([]Artifact, 0, len(records))
	for index, record := range records {
		item, err := s.prepareExistingArtifact(ctx, record.AbsPath, record.Meta)
		if err != nil {
			return nil, &RecordExistingBatchError{Index: index, Err: err}
		}
		registered = append(registered, item)
	}

	s.mu.Lock()
	for _, item := range registered {
		s.artifacts[item.ID] = item
	}
	s.mu.Unlock()

	return registered, nil
}

func (s *FileSystemStore) prepareExistingArtifact(ctx context.Context, absPath string, meta Artifact) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}

	class, err := normalizeRegisteredArtifactClass(meta.Class)
	if err != nil {
		return Artifact{}, err
	}
	meta.Class = class

	// Resolve both paths to absolute so comparison is unambiguous.
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to resolve store base directory: %w", err)
	}
	absResolved, err := filepath.Abs(absPath)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to resolve artifact path: %w", err)
	}

	// Security: reject any path that escapes the store root.
	rel, err := filepath.Rel(absBase, absResolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Artifact{}, fmt.Errorf("artifact path %q is outside store base directory %q", absPath, absBase)
	}

	info, err := os.Stat(absResolved)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to stat artifact: %w", err)
	}
	if info.IsDir() {
		return Artifact{}, fmt.Errorf("artifact path is a directory: %s", absResolved)
	}

	filename := meta.Filename
	if filename == "" {
		filename = filepath.Base(absResolved)
	} else if err := validateFilename(filename); err != nil {
		return Artifact{}, err
	}
	meta.Filename = filename

	if meta.Type == "" {
		meta.Type = ArtifactTypeUnknown
	}
	if meta.MimeType == "" {
		meta.MimeType = mime.TypeByExtension(filepath.Ext(filename))
	}

	// Compute checksum by streaming the file without copying it.
	f, err := os.Open(absResolved)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to open artifact for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	written, err := copyWithContext(ctx, h, f)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to compute artifact checksum: %w", err)
	}

	checksum := hex.EncodeToString(h.Sum(nil))
	meta.ChecksumSHA256 = checksum
	meta.SizeBytes = written
	meta.ID = DeterministicID(meta.JobID, meta.ProductID, meta.StepID, meta.Class, meta.Type, meta.Filename, checksum)

	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	} else {
		meta.CreatedAt = meta.CreatedAt.UTC()
	}

	// Store as forward-slash path relative to baseDir for cross-platform JSON.
	meta.Path = filepath.ToSlash(rel)

	return meta, nil
}

// WriteManifest writes a deterministic JSON manifest to the store base directory.
func (s *FileSystemStore) WriteManifest() error {
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create artifact base directory: %w", err)
	}

	artifacts, err := s.List()
	if err != nil {
		return err
	}
	manifest := Manifest{Artifacts: artifacts}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal artifact manifest: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(s.baseDir, manifestName)
	tmpFile, err := os.CreateTemp(s.baseDir, ".manifest-tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary manifest file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write temporary manifest: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temporary manifest: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize artifact manifest: %w", err)
	}
	return nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, defaultBufSize)
	var written int64

	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}

		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			written += int64(nw)
			if ew != nil {
				return written, fmt.Errorf("failed to write artifact data: %w", ew)
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				break
			}
			return written, fmt.Errorf("failed to read source artifact: %w", er)
		}
	}

	return written, nil
}

func sortArtifacts(artifacts []Artifact) {
	sort.Slice(artifacts, func(i, j int) bool {
		// Deterministic and human-meaningful ordering for manifests:
		// 1) product/step identity, 2) path, 3) stable metadata tie-breakers.
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

func normalizeRegisteredArtifactClass(class ArtifactClass) (ArtifactClass, error) {
	normalized := DefaultArtifactClass(class)
	if !ValidArtifactClass(normalized) {
		return "", fmt.Errorf("invalid artifact class: %q", class)
	}
	return normalized, nil
}

func validateFilename(name string) error {
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) {
		return fmt.Errorf("invalid artifact filename: absolute paths are not allowed")
	}
	if strings.Contains(clean, "..") {
		return fmt.Errorf("invalid artifact filename: path traversal is not allowed")
	}
	if clean == "." || clean == "" {
		return fmt.Errorf("invalid artifact filename: empty filename")
	}
	return nil
}
