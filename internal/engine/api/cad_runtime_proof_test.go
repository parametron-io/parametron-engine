package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"parametron/internal/engine/jobstatus"
)

// Task 14 permanent regression coverage for the exact entrypoint the real
// parametron-engine binary uses (api.Run), as opposed to the httptest-based
// in-process handler construction the rest of this package's tests use. The
// aligned CAD-runtime integration proof harness
// (scripts/cad_runtime_integration_proof.py) launches this same built binary
// as a real subprocess and drives it over a real socket; these tests pin the
// Go-level contract that Run must not diverge from that wiring by injecting
// an ExecutorFactory override that the harness's controlled runtime would
// never see in production.

// syncBuffer lets the server goroutine and the polling test goroutine share
// stdout safely.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func waitForListenAddr(t *testing.T, out *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		text := out.String()
		if idx := strings.Index(text, "parametron-engine listening on "); idx >= 0 {
			line := text[idx+len("parametron-engine listening on "):]
			if end := strings.IndexByte(line, '\n'); end >= 0 {
				line = line[:end]
			}
			return "http://" + strings.TrimSpace(line)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Run to report a listen address; output so far: %q", out.String())
	return ""
}

func runCADRuntimeProofServer(t *testing.T, artifactDir string) (base string, stop func()) {
	t.Helper()
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		// Port 0 asks the OS for an ephemeral free port; Run reports the
		// actual bound address via its startup log line, which
		// waitForListenAddr reads.
		errCh <- Run(ctx, []string{"--addr", "127.0.0.1:0", "--artifact-dir", artifactDir}, out, out)
	}()
	base = waitForListenAddr(t, out)
	return base, func() {
		cancel()
		if err := <-errCh; err != nil {
			t.Fatalf("Run returned error during shutdown: %v", err)
		}
	}
}

func TestCADRuntimeProof_RunConstructsHandlerWithoutExecutorOverride(t *testing.T) {
	apiInstallControlledRuntime(t, "success")
	base, stop := runCADRuntimeProofServer(t, t.TempDir())
	defer stop()

	sourceModel := writeAPISourceModel(t, "run-proof.FCStd")
	pkg := apiAlignedPackage(t, "run-proof", sourceModel)

	body, err := json.Marshal(apiAlignedEnvelope(pkg))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(base+"/job", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /job: %v", err)
	}
	var accepted jobSubmissionResponse
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /job status=%d", resp.StatusCode)
	}

	status := pollRealJobStatus(t, base, accepted.JobID, func(s jobstatus.Status) bool { return s.State.IsTerminal() })
	if status.State != jobstatus.StateSucceeded {
		t.Fatalf("expected the real Run() entrypoint to execute the aligned runtime via the default factory and succeed, got %#v", status)
	}
}

func TestCADRuntimeProof_CLIAndAPIProcessesDoNotShareRuntimeState(t *testing.T) {
	apiInstallControlledRuntime(t, "success")

	baseA, stopA := runCADRuntimeProofServer(t, t.TempDir())
	defer stopA()
	baseB, stopB := runCADRuntimeProofServer(t, t.TempDir())
	defer stopB()

	submit := func(base, product string) string {
		sourceModel := writeAPISourceModel(t, product+".FCStd")
		pkg := apiAlignedPackage(t, product, sourceModel)
		body, err := json.Marshal(apiAlignedEnvelope(pkg))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.Post(base+"/job", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /job: %v", err)
		}
		defer resp.Body.Close()
		var accepted jobSubmissionResponse
		if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
			t.Fatal(err)
		}
		return accepted.JobID
	}

	jobA := submit(baseA, "isolated-a")
	jobB := submit(baseB, "isolated-b")

	statusA := pollRealJobStatus(t, baseA, jobA, func(s jobstatus.Status) bool { return s.State.IsTerminal() })
	statusB := pollRealJobStatus(t, baseB, jobB, func(s jobstatus.Status) bool { return s.State.IsTerminal() })
	if statusA.State != jobstatus.StateSucceeded || statusB.State != jobstatus.StateSucceeded {
		t.Fatalf("expected both independent server processes to succeed independently: a=%#v b=%#v", statusA, statusB)
	}

	// Job A must not be visible from server B's independent job store and
	// vice versa: separate Run() invocations must not share any singleton
	// runtime/job state.
	resp, err := http.Get(baseB + "/job/" + jobA)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected server B to have no knowledge of server A's job, got status=%d", resp.StatusCode)
	}
}

func pollRealJobStatus(t *testing.T, base, jobID string, done func(jobstatus.Status) bool) jobstatus.Status {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last jobstatus.Status
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/job/" + jobID)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&last); err != nil {
				resp.Body.Close()
				t.Fatal(err)
			}
		}
		resp.Body.Close()
		if done(last) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out polling job %s status; last=%#v", jobID, last)
	return jobstatus.Status{}
}
