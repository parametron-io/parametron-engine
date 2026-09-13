package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/job"
	"parametron/internal/engine/jobstatus"
)

func TestFileSystemSubmissionStore_GetStatusReturnsDefensiveCopy(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	pkg := mustAPITestPackage(t, "widget")
	status := mustQueuedAPIStatus(t, pkg.ProductKey, pkg.Plan().Steps, time.Date(2026, 3, 18, 9, 0, 0, 0, time.UTC))
	if _, created, err := store.Register(pkg, status); err != nil {
		t.Fatalf("Register returned error: %v", err)
	} else if !created {
		t.Fatal("expected Register to create initial status")
	}

	got, found, err := store.GetStatus(pkg.JobID)
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if !found {
		t.Fatal("expected stored status")
	}

	got.State = jobstatus.StateFailed
	got.Error = &jobstatus.Failure{Message: "mutated"}

	again, found, err := store.GetStatus(pkg.JobID)
	if err != nil {
		t.Fatalf("second GetStatus returned error: %v", err)
	}
	if !found {
		t.Fatal("expected stored status on second lookup")
	}
	if again.State != jobstatus.StateQueued {
		t.Fatalf("state = %q, want %q", again.State, jobstatus.StateQueued)
	}
	if again.Error != nil {
		t.Fatalf("error = %#v, want nil", again.Error)
	}
}

func TestFileSystemSubmissionStore_GetStatusCorruptFileReturnsError(t *testing.T) {
	rootDir := t.TempDir()
	jobID := strings.Repeat("c", 64)
	if err := os.WriteFile(filepath.Join(rootDir, jobID+".json"), []byte("{bad-json"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	status, found, err := store.GetStatus(jobID)
	if err == nil {
		t.Fatal("expected corrupt file lookup to fail")
	}
	if found {
		t.Fatal("expected corrupt file lookup to not report found")
	}
	if status != nil {
		t.Fatalf("expected nil status, got %#v", status)
	}
}

func TestFileSystemSubmissionStore_RegisterWritesDeterministicJSON(t *testing.T) {
	pkg := mustAPITestPackage(t, "widget")
	status := mustQueuedAPIStatus(t, pkg.ProductKey, pkg.Plan().Steps, time.Date(2026, 3, 18, 10, 30, 0, 0, time.UTC))

	firstRoot := t.TempDir()
	firstStore, err := newFileSystemSubmissionStore(firstRoot)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore(first) returned error: %v", err)
	}
	if _, created, err := firstStore.Register(pkg, status); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	} else if !created {
		t.Fatal("expected first Register to create initial status")
	}

	secondRoot := t.TempDir()
	secondStore, err := newFileSystemSubmissionStore(secondRoot)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore(second) returned error: %v", err)
	}
	if _, created, err := secondStore.Register(pkg, status); err != nil {
		t.Fatalf("second Register returned error: %v", err)
	} else if !created {
		t.Fatal("expected second Register to create initial status")
	}

	firstData := mustReadAPIStatusFile(t, firstRoot, pkg.JobID)
	secondData := mustReadAPIStatusFile(t, secondRoot, pkg.JobID)
	if !bytes.Equal(firstData, secondData) {
		t.Fatalf("expected deterministic JSON writes,\nfirst=%q\nsecond=%q", firstData, secondData)
	}

	var payload persistedJobStatus
	if err := json.Unmarshal(firstData, &payload); err != nil {
		t.Fatalf("failed to decode persisted payload: %v", err)
	}
	if payload.Status.JobID != pkg.JobID {
		t.Fatalf("persisted jobId = %q, want %q", payload.Status.JobID, pkg.JobID)
	}
}

func TestFileSystemSubmissionStore_RegisterLeavesNoTempFiles(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	pkg := mustAPITestPackage(t, "widget")
	status := mustQueuedAPIStatus(t, pkg.ProductKey, pkg.Plan().Steps, time.Date(2026, 3, 18, 11, 45, 0, 0, time.UTC))
	if _, created, err := store.Register(pkg, status); err != nil {
		t.Fatalf("Register returned error: %v", err)
	} else if !created {
		t.Fatal("expected Register to create initial status")
	}

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one persisted file, got %d", len(entries))
	}
	if entries[0].Name() != pkg.JobID+".json" {
		t.Fatalf("unexpected persisted filename %q", entries[0].Name())
	}
}

func TestFileSystemSubmissionStore_RegisterConcurrentSameJobIsSafe(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	pkg := mustAPITestPackage(t, "widget")
	status := mustQueuedAPIStatus(t, pkg.ProductKey, pkg.Plan().Steps, time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC))

	const workers = 16
	type registerResult struct {
		created bool
		err     error
	}
	errCh := make(chan registerResult, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := store.Register(pkg, status)
			errCh <- registerResult{created: created, err: err}
		}()
	}

	wg.Wait()
	close(errCh)

	createdCount := 0
	for result := range errCh {
		if result.err != nil {
			t.Fatalf("concurrent Register returned error: %v", result.err)
		}
		if result.created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want 1", createdCount)
	}

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one persisted file, got %d", len(entries))
	}
	if entries[0].Name() != pkg.JobID+".json" {
		t.Fatalf("unexpected persisted filename %q", entries[0].Name())
	}

	data := mustReadAPIStatusFile(t, rootDir, pkg.JobID)
	var payload persistedJobStatus
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("persisted JSON was not valid: %v", err)
	}
	if payload.Status.JobID != pkg.JobID {
		t.Fatalf("persisted jobId = %q, want %q", payload.Status.JobID, pkg.JobID)
	}
	if payload.Status.State != jobstatus.StateQueued {
		t.Fatalf("persisted state = %q, want %q", payload.Status.State, jobstatus.StateQueued)
	}

	got, found, err := store.GetStatus(pkg.JobID)
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if !found {
		t.Fatal("expected stored status")
	}
	if got.JobID != pkg.JobID {
		t.Fatalf("GetStatus jobId = %q, want %q", got.JobID, pkg.JobID)
	}
	if got.State != jobstatus.StateQueued {
		t.Fatalf("GetStatus state = %q, want %q", got.State, jobstatus.StateQueued)
	}
}

func TestFileSystemSubmissionStore_UpdateStatusPersistsAtomically(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	pkg := mustAPITestPackage(t, "widget")
	status := mustQueuedAPIStatus(t, pkg.ProductKey, pkg.Plan().Steps, time.Date(2026, 3, 18, 12, 30, 0, 0, time.UTC))
	if _, created, err := store.Register(pkg, status); err != nil {
		t.Fatalf("Register returned error: %v", err)
	} else if !created {
		t.Fatal("expected Register to create initial status")
	}

	if err := store.UpdateStatus(pkg.JobID, func(status *jobstatus.Status) error {
		if err := status.Start(status.UpdatedAt.Add(time.Second)); err != nil {
			return err
		}
		return status.Succeed(status.UpdatedAt.Add(time.Second))
	}); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}

	got, found, err := store.GetStatus(pkg.JobID)
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if !found {
		t.Fatal("expected updated status")
	}
	if got.State != jobstatus.StateSucceeded {
		t.Fatalf("state = %q, want %q", got.State, jobstatus.StateSucceeded)
	}
	if got.StartedAt == nil || got.EndedAt == nil {
		t.Fatalf("updated timestamps = started:%v ended:%v, want both populated", got.StartedAt, got.EndedAt)
	}

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != pkg.JobID+".json" {
		t.Fatalf("unexpected files after update: %+v", entries)
	}
}

func TestFileSystemSubmissionStore_RegisterDuplicatePreservesExistingStatus(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	pkg := mustAPITestPackage(t, "widget")
	status := mustQueuedAPIStatus(t, pkg.ProductKey, pkg.Plan().Steps, time.Date(2026, 3, 18, 12, 45, 0, 0, time.UTC))
	if _, created, err := store.Register(pkg, status); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	} else if !created {
		t.Fatal("expected first Register to create initial status")
	}

	if err := store.UpdateStatus(pkg.JobID, func(status *jobstatus.Status) error {
		return status.Start(status.UpdatedAt.Add(time.Second))
	}); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}

	duplicate, created, err := store.Register(clonePackage(pkg), status)
	if err != nil {
		t.Fatalf("duplicate Register returned error: %v", err)
	}
	if created {
		t.Fatal("expected duplicate Register to report existing status")
	}
	if duplicate.State != jobstatus.StateRunning {
		t.Fatalf("duplicate Register state = %q, want %q", duplicate.State, jobstatus.StateRunning)
	}

	got, found, err := store.GetStatus(pkg.JobID)
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if !found {
		t.Fatal("expected stored status")
	}
	if got.State != jobstatus.StateRunning {
		t.Fatalf("stored state after duplicate Register = %q, want %q", got.State, jobstatus.StateRunning)
	}
}

func TestFileSystemSubmissionStore_UpdateStatusReturnsErrorForCorruptFile(t *testing.T) {
	rootDir := t.TempDir()
	jobID := strings.Repeat("d", 64)
	if err := os.WriteFile(filepath.Join(rootDir, jobID+".json"), []byte("{bad-json"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	if err := store.UpdateStatus(jobID, func(status *jobstatus.Status) error {
		return status.Succeed(time.Date(2026, 3, 18, 12, 31, 0, 0, time.UTC))
	}); err == nil {
		t.Fatal("expected UpdateStatus to fail for corrupt metadata")
	}
}

func TestFileSystemSubmissionStore_RegisterRejectsInvalidJobID(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	pkg := mustAPITestPackage(t, "widget")
	pkg.JobID = "../escape"
	status := mustQueuedAPIStatus(t, pkg.ProductKey, pkg.Plan().Steps, time.Date(2026, 3, 18, 12, 15, 0, 0, time.UTC))

	if _, _, err := store.Register(pkg, status); err == nil {
		t.Fatal("expected invalid job id registration to fail")
	}
}

func TestFileSystemSubmissionStore_InvalidJobIDsAreRejected(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	pkg := mustAPITestPackage(t, "widget")
	status := mustQueuedAPIStatus(t, pkg.ProductKey, pkg.Plan().Steps, time.Date(2026, 3, 18, 12, 15, 0, 0, time.UTC))

	for _, jobID := range []string{"", "../evil", "../../outside", "a/b"} {
		t.Run(jobIDLabel(jobID), func(t *testing.T) {
			candidate := clonePackage(pkg)
			candidate.JobID = jobID

			if _, _, err := store.Register(candidate, status); err == nil {
				t.Fatalf("expected Register to reject %q", jobID)
			}

			if jobID != "" {
				got, found, err := store.GetStatus(jobID)
				if err == nil {
					t.Fatalf("expected GetStatus to reject %q", jobID)
				}
				if found {
					t.Fatalf("expected %q to not be reported as found", jobID)
				}
				if got != nil {
					t.Fatalf("expected nil status for %q, got %#v", jobID, got)
				}
			}

			entries, err := os.ReadDir(rootDir)
			if err != nil {
				t.Fatalf("ReadDir returned error: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("expected no files in store root for invalid id %q, got %d", jobID, len(entries))
			}
		})
	}
}

func mustQueuedAPIStatus(t *testing.T, productKey string, steps []planner.Step, at time.Time) *jobstatus.Status {
	t.Helper()

	j, err := job.New(productKey, steps)
	if err != nil {
		t.Fatalf("job.New returned error: %v", err)
	}
	status, err := jobstatus.New(j, at)
	if err != nil {
		t.Fatalf("jobstatus.New returned error: %v", err)
	}
	return status
}

func mustReadAPIStatusFile(t *testing.T, rootDir, jobID string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(rootDir, jobID+".json"))
	if err != nil {
		t.Fatalf("failed to read persisted status file: %v", err)
	}
	return data
}

func jobIDLabel(jobID string) string {
	if jobID == "" {
		return "empty"
	}
	replacer := strings.NewReplacer("/", "_", ".", "_")
	return replacer.Replace(jobID)
}
