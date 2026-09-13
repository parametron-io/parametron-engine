package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"parametron/internal/engine/handoff"
	"parametron/internal/engine/jobstatus"
)

type fileSystemSubmissionStore struct {
	rootDir string
	mu      sync.Mutex
}

type persistedJobStatus struct {
	Status jobstatus.Status `json:"status"`
}

func newFileSystemSubmissionStore(rootDir string) (SubmissionStore, error) {
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return nil, errors.New("submission store root directory is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create submission store root: %w", err)
	}
	return &fileSystemSubmissionStore{rootDir: root}, nil
}

func (s *fileSystemSubmissionStore) Register(pkg *handoff.Package, status *jobstatus.Status) (*jobstatus.Status, bool, error) {
	if s == nil || pkg == nil || status == nil {
		return nil, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.jobStatusPath(pkg.JobID)
	if err != nil {
		return nil, false, err
	}
	if existing, err := s.readStatusFile(path); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	payload := persistedJobStatus{Status: *cloneStatus(status)}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("marshal job status %q: %w", pkg.JobID, err)
	}
	data = append(data, '\n')

	if err := s.writeStatusFile(path, pkg.JobID, data); err != nil {
		return nil, false, err
	}
	return cloneStatus(status), true, nil
}

func (s *fileSystemSubmissionStore) GetStatus(jobID string) (*jobstatus.Status, bool, error) {
	if s == nil || strings.TrimSpace(jobID) == "" {
		return nil, false, nil
	}

	path, err := s.jobStatusPath(jobID)
	if err != nil {
		return nil, false, err
	}

	status, err := s.readStatusFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	return status, true, nil
}

func (s *fileSystemSubmissionStore) UpdateStatus(jobID string, update func(*jobstatus.Status) error) error {
	if s == nil || strings.TrimSpace(jobID) == "" || update == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.jobStatusPath(jobID)
	if err != nil {
		return err
	}

	status, err := s.readStatusFile(path)
	if err != nil {
		return err
	}

	next := cloneStatus(status)
	if err := update(next); err != nil {
		return err
	}

	payload := persistedJobStatus{Status: *cloneStatus(next)}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal job status %q: %w", jobID, err)
	}
	data = append(data, '\n')

	return s.writeStatusFile(path, jobID, data)
}

func (s *fileSystemSubmissionStore) readStatusFile(path string) (*jobstatus.Status, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read job status file %q: %w", path, err)
	}

	var payload persistedJobStatus
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode job status file %q: %w", path, err)
	}

	return cloneStatus(&payload.Status), nil
}

func (s *fileSystemSubmissionStore) writeStatusFile(path, jobID string, data []byte) error {
	pattern := filepath.Base(path) + ".tmp-*"
	tmpFile, err := os.CreateTemp(s.rootDir, pattern)
	if err != nil {
		return fmt.Errorf("create temp job status %q: %w", jobID, err)
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		_ = tmpFile.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write temp job status %q: %w", jobID, err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp job status %q: %w", jobID, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp job status %q: %w", jobID, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename job status %q: %w", jobID, err)
	}

	cleanup = false
	return nil
}

func (s *fileSystemSubmissionStore) jobStatusPath(jobID string) (string, error) {
	if s == nil {
		return "", errors.New("submission store is nil")
	}
	safeID, err := validateSubmissionJobID(jobID)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.rootDir, safeID+".json"), nil
}

func validateSubmissionJobID(jobID string) (string, error) {
	trimmed := strings.TrimSpace(jobID)
	if trimmed == "" {
		return "", errors.New("job id is required")
	}
	if trimmed == "." || trimmed == ".." {
		return "", fmt.Errorf("invalid job id %q", jobID)
	}
	if strings.Contains(trimmed, string(filepath.Separator)) {
		return "", fmt.Errorf("invalid job id %q", jobID)
	}
	if os.PathSeparator != '/' && strings.ContainsRune(trimmed, '/') {
		return "", fmt.Errorf("invalid job id %q", jobID)
	}
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return "", fmt.Errorf("invalid job id %q", jobID)
		}
	}
	return trimmed, nil
}
