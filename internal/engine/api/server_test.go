package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/jobstatus"
	"parametron/internal/engine/metadata"
	"parametron/internal/engine/report"
	"parametron/internal/engine/verification"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
)

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}

	if cfg.Addr != DefaultAddr {
		t.Fatalf("expected default addr %q, got %q", DefaultAddr, cfg.Addr)
	}
	if cfg.ArtifactDir != "" {
		t.Fatalf("expected artifact dir to default to empty, got %q", cfg.ArtifactDir)
	}

	if cfg.Help {
		t.Fatal("expected help to default to false")
	}

	if cfg.Version {
		t.Fatal("expected version to default to false")
	}
}

func TestParseConfigCustomAddr(t *testing.T) {
	cfg, err := parseConfig([]string{"--addr", "127.0.0.1:9090"})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}

	if cfg.Addr != "127.0.0.1:9090" {
		t.Fatalf("expected custom addr to be preserved, got %q", cfg.Addr)
	}
}

func TestParseConfigCustomArtifactDir(t *testing.T) {
	cfg, err := parseConfig([]string{"--artifact-dir", "/tmp/parametron-artifacts"})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}

	if cfg.ArtifactDir != "/tmp/parametron-artifacts" {
		t.Fatalf("expected artifact dir to be preserved, got %q", cfg.ArtifactDir)
	}
}

func TestParseConfigRejectsEmptyAddr(t *testing.T) {
	_, err := parseConfig([]string{"--addr", "   "})
	if err == nil {
		t.Fatal("expected empty addr to fail")
	}

	if !strings.Contains(err.Error(), "addr must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConfigRejectsWhitespaceArtifactDir(t *testing.T) {
	_, err := parseConfig([]string{"--artifact-dir", "   "})
	if err == nil {
		t.Fatal("expected empty artifact dir to fail")
	}

	if !strings.Contains(err.Error(), "artifact-dir must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServerRun_ShutdownOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan net.Addr, 1)
	var output bytes.Buffer
	listener := newFakeListener("127.0.0.1:0")

	server := Server{
		Addr:   "127.0.0.1:0",
		Output: &output,
		onListen: func(addr net.Addr) {
			started <- addr
		},
		listen: func(network, addr string) (net.Listener, error) {
			return listener, nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx)
	}()

	addr := <-started
	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("server.Run returned error: %v", err)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "parametron-engine listening on "+addr.String()) {
		t.Fatalf("expected startup output to contain listening address, got %q", logLine)
	}

	if !listener.isClosed() {
		t.Fatal("expected listener to be closed during graceful shutdown")
	}
}

func TestNewHandlerHealthzReturnsOK(t *testing.T) {
	handler := newHandler()

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)

	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected healthz status 200, got %d", healthRec.Code)
	}

	if healthRec.Body.String() != "ok\n" {
		t.Fatalf("unexpected healthz body: %q", healthRec.Body.String())
	}
}

func TestNewHandlerUnknownRouteReturnsNotFound(t *testing.T) {
	handler := newHandler()

	missingReq := httptest.NewRequest(http.MethodGet, "/missing", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)

	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("expected missing route status 404, got %d", missingRec.Code)
	}
}

func TestNewHandlerJobSubmission(t *testing.T) {
	basePackage := mustAPITestPackage(t, "widget")

	tests := []struct {
		name           string
		method         string
		contentType    string
		body           string
		wantCode       int
		wantAllow      string
		wantLocation   string
		wantJobID      string
		wantProductKey string
		wantState      jobstatus.State
	}{
		{
			name:           "valid submission returns accepted queued status",
			method:         http.MethodPost,
			contentType:    "application/json",
			body:           mustSubmissionJSON(t, packageEnvelopeFromPackage(basePackage)),
			wantCode:       http.StatusAccepted,
			wantLocation:   "/job/" + basePackage.JobID,
			wantJobID:      basePackage.JobID,
			wantProductKey: basePackage.ProductKey,
			wantState:      jobstatus.StateQueued,
		},
		{
			name:        "invalid json returns bad request",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"handoff":`,
			wantCode:    http.StatusBadRequest,
		},
		{
			name:        "structurally invalid handoff package returns bad request",
			method:      http.MethodPost,
			contentType: "application/json",
			body: mustSubmissionJSON(t, jobSubmissionRequest{
				Handoff: &handoffPackagePayload{
					ProductKey: "widget",
					CSV: planner.WriteCSVPayload{
						ProductKey: "widget",
						Filename:   "widget.csv",
					},
				},
			}),
			wantCode: http.StatusBadRequest,
		},
		{
			name:        "conflicting submitted job id is rejected",
			method:      http.MethodPost,
			contentType: "application/json",
			body: mustSubmissionJSON(t, func() jobSubmissionRequest {
				req := packageEnvelopeFromPackage(basePackage)
				req.Handoff.JobID = strings.Repeat("f", len(basePackage.JobID))
				return req
			}()),
			wantCode: http.StatusBadRequest,
		},
		{
			name:        "non post returns method not allowed",
			method:      http.MethodPut,
			contentType: "application/json",
			body:        "",
			wantCode:    http.StatusMethodNotAllowed,
			wantAllow:   http.MethodPost,
		},
		{
			name:        "get returns method not allowed",
			method:      http.MethodGet,
			contentType: "application/json",
			body:        "",
			wantCode:    http.StatusMethodNotAllowed,
			wantAllow:   http.MethodPost,
		},
	}

	handler := newHandler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/job", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d body=%q", tt.wantCode, rec.Code, rec.Body.String())
			}
			if tt.wantAllow != "" && rec.Header().Get("Allow") != tt.wantAllow {
				t.Fatalf("expected Allow %q, got %q", tt.wantAllow, rec.Header().Get("Allow"))
			}
			if tt.wantLocation != "" && rec.Header().Get("Location") != tt.wantLocation {
				t.Fatalf("expected Location %q, got %q", tt.wantLocation, rec.Header().Get("Location"))
			}
			if tt.wantCode == http.StatusAccepted {
				var resp jobSubmissionResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.SchemaVersion != submissionSchemaVersion {
					t.Fatalf("unexpected schema version %q", resp.SchemaVersion)
				}
				if resp.JobID != tt.wantJobID {
					t.Fatalf("unexpected job id %q", resp.JobID)
				}
				if resp.ProductKey != tt.wantProductKey {
					t.Fatalf("unexpected product key %q", resp.ProductKey)
				}
				if resp.State != tt.wantState {
					t.Fatalf("unexpected state %q", resp.State)
				}
			}
		})
	}
}

func TestPOSTJob_IsIdempotent(t *testing.T) {
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{JobQueue: queue})
	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))

	firstReq := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, firstReq)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first submission status = %d body=%q", first.Code, first.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, secondReq)
	if second.Code != http.StatusAccepted {
		t.Fatalf("second submission status = %d body=%q", second.Code, second.Body.String())
	}

	var firstResp jobSubmissionResponse
	var secondResp jobSubmissionResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if firstResp.JobID != secondResp.JobID {
		t.Fatalf("expected stable job ids, got %q and %q", firstResp.JobID, secondResp.JobID)
	}
	if queue.Len() != 1 {
		t.Fatalf("queue Len = %d, want 1", queue.Len())
	}
}

func TestPOSTJob_EnqueuesCanonicalPackage(t *testing.T) {
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{JobQueue: queue})
	pkg := mustAPITestPackage(t, "widget")

	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(pkg))))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%q", rec.Code, rec.Body.String())
	}
	if queue.Len() != 1 {
		t.Fatalf("queue Len = %d, want 1", queue.Len())
	}

	queued, ok := queue.Peek()
	if !ok {
		t.Fatal("expected queued package")
	}
	gotJSON := mustSubmissionJSON(t, packageEnvelopeFromPackage(queued))
	wantJSON := mustSubmissionJSON(t, packageEnvelopeFromPackage(pkg))
	if gotJSON != wantJSON {
		t.Fatalf("queued package mismatch:\ngot=%s\nwant=%s", gotJSON, wantJSON)
	}
}

func TestPOSTJob_InvalidSubmissionDoesNotEnqueueAnything(t *testing.T) {
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{JobQueue: queue})

	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(`{"handoff":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len = %d, want 0", queue.Len())
	}
}

func TestPOSTJob_StoreRegistrationFailureLeavesQueueUnchanged(t *testing.T) {
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: failingSubmissionStore{err: errors.New("store failed")},
		JobQueue:        queue,
	})

	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len = %d, want 0", queue.Len())
	}
}

func TestPOSTJob_QueueInsertionFailureReturnsInternalServerError(t *testing.T) {
	handler := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        failingJobQueue{err: errors.New("queue failed")},
	})

	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestGETJobStatus_ReturnsQueuedStatusForSubmittedJob(t *testing.T) {
	handler := newHandler()
	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))

	postReq := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d body=%q", postRec.Code, postRec.Body.String())
	}

	location := postRec.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	getReq := httptest.NewRequest(http.MethodGet, location, nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("status lookup = %d body=%q", getRec.Code, getRec.Body.String())
	}
	if got := getRec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}

	var status jobstatus.Status
	if err := json.Unmarshal(getRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}

	if status.SchemaVersion != jobstatus.SchemaVersion {
		t.Fatalf("schemaVersion = %q", status.SchemaVersion)
	}

	var submitResp jobSubmissionResponse
	if err := json.Unmarshal(postRec.Body.Bytes(), &submitResp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if status.JobID != submitResp.JobID {
		t.Fatalf("jobId = %q, want %q", status.JobID, submitResp.JobID)
	}
	if status.ProductKey != submitResp.ProductKey {
		t.Fatalf("productKey = %q, want %q", status.ProductKey, submitResp.ProductKey)
	}
	if status.State != jobstatus.StateQueued {
		t.Fatalf("state = %q", status.State)
	}
	if status.CreatedAt.IsZero() {
		t.Fatal("createdAt should be present")
	}
	if status.UpdatedAt.IsZero() {
		t.Fatal("updatedAt should be present")
	}
	if status.StartedAt != nil {
		t.Fatalf("startedAt = %v, want nil", status.StartedAt)
	}
	if status.EndedAt != nil {
		t.Fatalf("endedAt = %v, want nil", status.EndedAt)
	}
	if status.Error != nil {
		t.Fatalf("error = %#v, want nil", status.Error)
	}
}

func TestGETJobStatus_UnknownJobReturnsNotFound(t *testing.T) {
	handler := newHandler()

	req := httptest.NewRequest(http.MethodGet, "/job/"+strings.Repeat("a", 64), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGETJobArtifacts_ReturnsEmptyDeterministicListForKnownJob(t *testing.T) {
	handler := newHandler()
	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))

	postReq := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d body=%q", postRec.Code, postRec.Body.String())
	}

	var submitResp jobSubmissionResponse
	if err := json.Unmarshal(postRec.Body.Bytes(), &submitResp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}

	path := "/job/" + submitResp.JobID + "/artifacts"
	req1 := httptest.NewRequest(http.MethodGet, path, nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, path, nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 responses, got %d and %d", rec1.Code, rec2.Code)
	}
	if rec1.Body.String() != rec2.Body.String() {
		t.Fatal("expected repeated artifact list responses to be byte-for-byte identical")
	}

	var resp jobArtifactsResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode artifact list response: %v", err)
	}
	if resp.SchemaVersion != jobArtifactsSchemaVersion {
		t.Fatalf("schemaVersion = %q", resp.SchemaVersion)
	}
	if resp.JobID != submitResp.JobID {
		t.Fatalf("jobId = %q, want %q", resp.JobID, submitResp.JobID)
	}
	if len(resp.Artifacts) != 0 {
		t.Fatalf("expected no artifacts, got %+v", resp.Artifacts)
	}
}

func TestGETJobArtifacts_ReturnsOnlyJobArtifactsInStableOrder(t *testing.T) {
	store := artifact.NewFileSystemStore(t.TempDir())
	submissionStore := newInMemorySubmissionStore()
	handler := newHandler(HandlerOptions{SubmissionStore: submissionStore, ArtifactStore: store})

	widgetJobID := submitAPITestJob(t, handler, "widget")
	_ = submitAPITestJob(t, handler, "gear")
	mustMarkJobStatusSucceeded(t, submissionStore, widgetJobID)

	registerAPITestArtifact(t, store, widgetJobID, "widget", "2", "widget-b.csv", "widget-b\n")
	registerAPITestArtifact(t, store, "", "gear", "1", "gear-a.csv", "gear-a\n")
	registerAPITestArtifact(t, store, widgetJobID, "widget", "1", "widget-a.csv", "widget-a\n")

	path := "/job/" + widgetJobID + "/artifacts"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}

	var resp jobArtifactsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode artifact list response: %v", err)
	}
	if resp.JobID != widgetJobID {
		t.Fatalf("jobId = %q, want %q", resp.JobID, widgetJobID)
	}
	if len(resp.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(resp.Artifacts))
	}
	for _, item := range resp.Artifacts {
		if item.ProductID != "widget" {
			t.Fatalf("unexpected productId in response: %+v", item)
		}
		if item.Class != artifact.ArtifactClassExecutionOutput {
			t.Fatalf("artifact class = %q, want %q", item.Class, artifact.ArtifactClassExecutionOutput)
		}
	}
	if !strings.Contains(rec.Body.String(), `"class": "execution_output"`) {
		t.Fatalf("job artifact response must include class, got:\n%s", rec.Body.String())
	}
	if resp.Artifacts[0].StepID != "1" || resp.Artifacts[0].Filename != "widget-a.csv" {
		t.Fatalf("unexpected first artifact ordering: %+v", resp.Artifacts[0])
	}
	if resp.Artifacts[1].StepID != "2" || resp.Artifacts[1].Filename != "widget-b.csv" {
		t.Fatalf("unexpected second artifact ordering: %+v", resp.Artifacts[1])
	}
}

func TestJobArtifacts_CrossJobIsolation(t *testing.T) {
	store := artifact.NewFileSystemStore(t.TempDir())
	submissionStore := newInMemorySubmissionStore()
	handler := newHandler(HandlerOptions{SubmissionStore: submissionStore, ArtifactStore: store})

	jobAID := submitAPITestJob(t, handler, "widget")
	jobBID := submitAPITestJob(t, handler, "gear")
	mustMarkJobStatusSucceeded(t, submissionStore, jobAID)
	mustMarkJobStatusSucceeded(t, submissionStore, jobBID)

	jobAArtifact1 := registerAPITestArtifact(t, store, jobAID, "widget", "1", "widget-a.csv", "widget-a\n")
	jobAArtifact2 := registerAPITestArtifact(t, store, jobAID, "widget", "2", "widget-b.csv", "widget-b\n")
	jobBArtifact := registerAPITestArtifact(t, store, jobBID, "gear", "1", "gear-a.csv", "gear-a\n")

	req := httptest.NewRequest(http.MethodGet, "/job/"+jobAID+"/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}

	var resp jobArtifactsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode artifact list response: %v", err)
	}

	if resp.JobID != jobAID {
		t.Fatalf("jobId = %q, want %q", resp.JobID, jobAID)
	}
	if len(resp.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(resp.Artifacts))
	}

	gotIDs := make(map[string]struct{}, len(resp.Artifacts))
	for _, item := range resp.Artifacts {
		gotIDs[item.ID] = struct{}{}
		if item.ProductID != "widget" {
			t.Fatalf("artifact %+v did not belong to jobA product", item)
		}
	}

	if _, ok := gotIDs[jobAArtifact1.ID]; !ok {
		t.Fatalf("jobA artifact %q missing from response", jobAArtifact1.ID)
	}
	if _, ok := gotIDs[jobAArtifact2.ID]; !ok {
		t.Fatalf("jobA artifact %q missing from response", jobAArtifact2.ID)
	}
	if _, ok := gotIDs[jobBArtifact.ID]; ok {
		t.Fatalf("jobB artifact %q leaked into jobA response", jobBArtifact.ID)
	}

	jobBReq := httptest.NewRequest(http.MethodGet, "/job/"+jobBID+"/artifacts", nil)
	jobBRec := httptest.NewRecorder()
	handler.ServeHTTP(jobBRec, jobBReq)
	if jobBRec.Code != http.StatusOK {
		t.Fatalf("jobB expected 200, got %d body=%q", jobBRec.Code, jobBRec.Body.String())
	}
}

func TestJobArtifacts_DeterministicResponse(t *testing.T) {
	store := artifact.NewFileSystemStore(t.TempDir())
	submissionStore := newInMemorySubmissionStore()
	handler := newHandler(HandlerOptions{SubmissionStore: submissionStore, ArtifactStore: store})

	jobID := submitAPITestJob(t, handler, "widget")
	mustMarkJobStatusSucceeded(t, submissionStore, jobID)
	registerAPITestArtifact(t, store, jobID, "widget", "2", "widget-b.csv", "widget-b\n")
	registerAPITestArtifact(t, store, jobID, "widget", "1", "widget-a.csv", "widget-a\n")

	path := "/job/" + jobID + "/artifacts"

	req1 := httptest.NewRequest(http.MethodGet, path, nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, path, nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 responses, got %d and %d", rec1.Code, rec2.Code)
	}
	if !bytes.Equal(rec1.Body.Bytes(), rec2.Body.Bytes()) {
		t.Fatalf("expected byte-identical responses,\nfirst=%q\nsecond=%q", rec1.Body.Bytes(), rec2.Body.Bytes())
	}
}

func TestGETJobArtifacts_UnknownJobReturnsNotFound(t *testing.T) {
	handler := newHandler()

	req := httptest.NewRequest(http.MethodGet, "/job/"+strings.Repeat("a", 64)+"/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestJobStatusRoute_MethodRestrictions(t *testing.T) {
	handler := newHandler()
	path := "/job/" + strings.Repeat("a", 64)

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s expected 405, got %d", method, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Fatalf("%s Allow = %q", method, got)
		}
	}
}

func TestJobArtifactsRoute_MethodRestrictions(t *testing.T) {
	handler := newHandler()
	jobID := submitAPITestJob(t, handler, "widget")
	path := "/job/" + jobID + "/artifacts"

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s expected 405, got %d", method, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Fatalf("%s Allow = %q", method, got)
		}
	}
}

func TestJobRoutes_RemainSeparated(t *testing.T) {
	handler := newHandler()
	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))

	postReq := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusAccepted {
		t.Fatalf("POST /job expected 202, got %d body=%q", postRec.Code, postRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/job", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /job expected 405, got %d", getRec.Code)
	}
	if got := getRec.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("GET /job Allow = %q", got)
	}
}

func TestJobArtifactRoutes_RemainSeparated(t *testing.T) {
	store, item := newAPITestStore(t)
	submissionStore := newInMemorySubmissionStore()
	handler := newHandler(HandlerOptions{SubmissionStore: submissionStore, ArtifactStore: store})
	jobID := submitAPITestJob(t, handler, "widget")
	mustMarkJobStatusSucceeded(t, submissionStore, jobID)

	statusReq := httptest.NewRequest(http.MethodGet, "/job/"+jobID, nil)
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("GET /job/{id} expected 200, got %d body=%q", statusRec.Code, statusRec.Body.String())
	}

	jobArtifactsReq := httptest.NewRequest(http.MethodGet, "/job/"+jobID+"/artifacts", nil)
	jobArtifactsRec := httptest.NewRecorder()
	handler.ServeHTTP(jobArtifactsRec, jobArtifactsReq)
	if jobArtifactsRec.Code != http.StatusOK {
		t.Fatalf("GET /job/{id}/artifacts expected 200, got %d body=%q", jobArtifactsRec.Code, jobArtifactsRec.Body.String())
	}

	genericListReq := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	genericListRec := httptest.NewRecorder()
	handler.ServeHTTP(genericListRec, genericListReq)
	if genericListRec.Code != http.StatusOK {
		t.Fatalf("GET /artifacts expected 200, got %d", genericListRec.Code)
	}
	if !strings.Contains(genericListRec.Body.String(), `"class": "execution_output"`) {
		t.Fatalf("GET /artifacts response must include class, got:\n%s", genericListRec.Body.String())
	}

	genericFileReq := httptest.NewRequest(http.MethodGet, "/artifacts/"+item.ID, nil)
	genericFileRec := httptest.NewRecorder()
	handler.ServeHTTP(genericFileRec, genericFileReq)
	if genericFileRec.Code != http.StatusOK {
		t.Fatalf("GET /artifacts/{artifactID} expected 200, got %d", genericFileRec.Code)
	}
}

func TestPOSTJob_IdempotentSubmissionPreservesRetrievableQueuedStatus(t *testing.T) {
	handler := newHandler()
	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))

	var jobID string
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("submission %d status = %d body=%q", i+1, rec.Code, rec.Body.String())
		}

		var resp jobSubmissionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode submission %d: %v", i+1, err)
		}
		if jobID == "" {
			jobID = resp.JobID
			continue
		}
		if resp.JobID != jobID {
			t.Fatalf("jobId = %q, want %q", resp.JobID, jobID)
		}
	}

	getReq := httptest.NewRequest(http.MethodGet, "/job/"+jobID, nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /job/{id} expected 200, got %d body=%q", getRec.Code, getRec.Body.String())
	}

	var status jobstatus.Status
	if err := json.Unmarshal(getRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if status.JobID != jobID {
		t.Fatalf("jobId = %q, want %q", status.JobID, jobID)
	}
	if status.State != jobstatus.StateQueued {
		t.Fatalf("state = %q", status.State)
	}
}

func TestGETJobStatus_PersistsAcrossHandlerRecreation(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	firstHandler := newHandler(HandlerOptions{SubmissionStore: store})
	jobID := submitAPITestJob(t, firstHandler, "widget")

	reloadedStore, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("reloaded newFileSystemSubmissionStore returned error: %v", err)
	}
	secondHandler := newHandler(HandlerOptions{SubmissionStore: reloadedStore})

	req := httptest.NewRequest(http.MethodGet, "/job/"+jobID, nil)
	rec := httptest.NewRecorder()
	secondHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}

	var status jobstatus.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if status.JobID != jobID {
		t.Fatalf("jobId = %q, want %q", status.JobID, jobID)
	}
	if status.State != jobstatus.StateQueued {
		t.Fatalf("state = %q, want %q", status.State, jobstatus.StateQueued)
	}
}

func TestGETJobArtifacts_PersistsAcrossHandlerRecreation(t *testing.T) {
	rootDir := t.TempDir()
	submissionStore, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	firstHandler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
	})

	jobID := submitAPITestJob(t, firstHandler, "widget")
	gearJobID := submitAPITestJob(t, firstHandler, "gear")
	mustMarkJobStatusSucceeded(t, submissionStore, jobID)
	mustMarkJobStatusSucceeded(t, submissionStore, gearJobID)
	registerAPITestArtifact(t, artifactStore, jobID, "widget", "2", "widget-b.csv", "widget-b\n")
	registerAPITestArtifact(t, artifactStore, gearJobID, "gear", "1", "gear-a.csv", "gear-a\n")
	registerAPITestArtifact(t, artifactStore, jobID, "widget", "1", "widget-a.csv", "widget-a\n")

	reloadedStore, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("reloaded newFileSystemSubmissionStore returned error: %v", err)
	}
	secondHandler := newHandler(HandlerOptions{
		SubmissionStore: reloadedStore,
		ArtifactStore:   artifactStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/job/"+jobID+"/artifacts", nil)
	rec := httptest.NewRecorder()
	secondHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}

	var resp jobArtifactsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode artifact response: %v", err)
	}
	if resp.JobID != jobID {
		t.Fatalf("jobId = %q, want %q", resp.JobID, jobID)
	}
	if len(resp.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(resp.Artifacts))
	}
	if resp.Artifacts[0].ProductID != "widget" || resp.Artifacts[1].ProductID != "widget" {
		t.Fatalf("unexpected product identities: %+v", resp.Artifacts)
	}
	if resp.Artifacts[0].StepID != "1" || resp.Artifacts[1].StepID != "2" {
		t.Fatalf("unexpected artifact order: %+v", resp.Artifacts)
	}
}

func TestGETJobRoutes_MissingPersistedMetadataAfterRestartReturnsNotFound(t *testing.T) {
	rootDir := t.TempDir()
	submissionStore, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}

	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	firstHandler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
	})

	jobID := submitAPITestJob(t, firstHandler, "widget")

	statusReq := httptest.NewRequest(http.MethodGet, "/job/"+jobID, nil)
	statusRec := httptest.NewRecorder()
	firstHandler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("initial GET /job/{id} expected 200, got %d body=%q", statusRec.Code, statusRec.Body.String())
	}

	if err := os.Remove(filepath.Join(rootDir, jobID+".json")); err != nil {
		t.Fatalf("failed to remove persisted metadata: %v", err)
	}

	reloadedStore, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("reloaded newFileSystemSubmissionStore returned error: %v", err)
	}
	secondHandler := newHandler(HandlerOptions{
		SubmissionStore: reloadedStore,
		ArtifactStore:   artifactStore,
	})

	restartedStatusReq := httptest.NewRequest(http.MethodGet, "/job/"+jobID, nil)
	restartedStatusRec := httptest.NewRecorder()
	secondHandler.ServeHTTP(restartedStatusRec, restartedStatusReq)
	if restartedStatusRec.Code != http.StatusNotFound {
		t.Fatalf("restarted GET /job/{id} expected 404, got %d body=%q", restartedStatusRec.Code, restartedStatusRec.Body.String())
	}

	restartedArtifactsReq := httptest.NewRequest(http.MethodGet, "/job/"+jobID+"/artifacts", nil)
	restartedArtifactsRec := httptest.NewRecorder()
	secondHandler.ServeHTTP(restartedArtifactsRec, restartedArtifactsReq)
	if restartedArtifactsRec.Code != http.StatusNotFound {
		t.Fatalf("restarted GET /job/{id}/artifacts expected 404, got %d body=%q", restartedArtifactsRec.Code, restartedArtifactsRec.Body.String())
	}
}

func TestPOSTJob_IdempotentSubmissionPersistsMetadata(t *testing.T) {
	rootDir := t.TempDir()
	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}
	handler := newHandler(HandlerOptions{SubmissionStore: store})
	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))

	var firstResp jobSubmissionResponse
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("submission %d expected 202, got %d body=%q", i+1, rec.Code, rec.Body.String())
		}

		var resp jobSubmissionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode submission %d response: %v", i+1, err)
		}
		if i == 0 {
			firstResp = resp
			continue
		}
		if resp.JobID != firstResp.JobID {
			t.Fatalf("jobId = %q, want %q", resp.JobID, firstResp.JobID)
		}
	}

	reloadedStore, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("reloaded newFileSystemSubmissionStore returned error: %v", err)
	}
	status, found, err := reloadedStore.GetStatus(firstResp.JobID)
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if !found {
		t.Fatal("expected persisted status")
	}
	if status.JobID != firstResp.JobID {
		t.Fatalf("persisted jobId = %q, want %q", status.JobID, firstResp.JobID)
	}
	if status.State != jobstatus.StateQueued {
		t.Fatalf("persisted state = %q, want %q", status.State, jobstatus.StateQueued)
	}
}

func TestGETJobStatus_CorruptPersistedMetadataReturnsInternalServerError(t *testing.T) {
	rootDir := t.TempDir()
	jobID := strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(rootDir, jobID+".json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt status file: %v", err)
	}

	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}
	handler := newHandler(HandlerOptions{SubmissionStore: store})

	req := httptest.NewRequest(http.MethodGet, "/job/"+jobID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGETJobArtifacts_CorruptPersistedMetadataReturnsInternalServerError(t *testing.T) {
	rootDir := t.TempDir()
	jobID := strings.Repeat("b", 64)
	if err := os.WriteFile(filepath.Join(rootDir, jobID+".json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt status file: %v", err)
	}

	store, err := newFileSystemSubmissionStore(rootDir)
	if err != nil {
		t.Fatalf("newFileSystemSubmissionStore returned error: %v", err)
	}
	handler := newHandler(HandlerOptions{SubmissionStore: store})

	req := httptest.NewRequest(http.MethodGet, "/job/"+jobID+"/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestInMemorySubmissionStore_GetStatusReturnsDefensiveCopy(t *testing.T) {
	store := newInMemorySubmissionStore()
	pkg := mustAPITestPackage(t, "widget")

	j, err := job.New(pkg.ProductKey, pkg.Plan().Steps)
	if err != nil {
		t.Fatalf("job.New returned error: %v", err)
	}

	status, err := jobstatus.New(j, time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("jobstatus.New returned error: %v", err)
	}
	if _, created, err := store.Register(pkg, status); err != nil {
		t.Fatalf("Register returned error: %v", err)
	} else if !created {
		t.Fatal("expected first Register call to create status")
	}

	got, ok, err := store.GetStatus(pkg.JobID)
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected stored status")
	}

	got.State = jobstatus.StateFailed
	got.Error = &jobstatus.Failure{Message: "mutated"}

	again, ok, err := store.GetStatus(pkg.JobID)
	if err != nil {
		t.Fatalf("second GetStatus returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected stored status on second lookup")
	}
	if again.State != jobstatus.StateQueued {
		t.Fatalf("state = %q, want %q", again.State, jobstatus.StateQueued)
	}
	if again.Error != nil {
		t.Fatalf("error = %#v, want nil", again.Error)
	}
}

func TestInMemorySubmissionStore_RegisterDuplicatePreservesExistingStatus(t *testing.T) {
	store := newInMemorySubmissionStore()
	pkg := mustAPITestPackage(t, "widget")

	j, err := job.New(pkg.ProductKey, pkg.Plan().Steps)
	if err != nil {
		t.Fatalf("job.New returned error: %v", err)
	}

	initial, err := jobstatus.New(j, time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("jobstatus.New returned error: %v", err)
	}
	stored, created, err := store.Register(pkg, initial)
	if err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	if !created {
		t.Fatal("expected first Register to create status")
	}
	if stored.State != jobstatus.StateQueued {
		t.Fatalf("first Register state = %q, want %q", stored.State, jobstatus.StateQueued)
	}

	if err := store.UpdateStatus(pkg.JobID, func(status *jobstatus.Status) error {
		return status.Start(status.UpdatedAt.Add(time.Second))
	}); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}

	duplicate, created, err := store.Register(clonePackage(pkg), initial)
	if err != nil {
		t.Fatalf("duplicate Register returned error: %v", err)
	}
	if created {
		t.Fatal("expected duplicate Register to report existing status")
	}
	if duplicate.State != jobstatus.StateRunning {
		t.Fatalf("duplicate Register state = %q, want %q", duplicate.State, jobstatus.StateRunning)
	}

	got, ok, err := store.GetStatus(pkg.JobID)
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected stored status")
	}
	if got.State != jobstatus.StateRunning {
		t.Fatalf("stored state after duplicate Register = %q, want %q", got.State, jobstatus.StateRunning)
	}
}

func TestGETJobStatus_RejectsMalformedPaths(t *testing.T) {
	handler := newHandler()

	for _, path := range []string{"/job/", "/job//", "/job/a/b"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s expected 404, got %d", path, rec.Code)
		}
	}
}

func TestGETJobArtifacts_RejectsMalformedPaths(t *testing.T) {
	handler := newHandler()
	jobID := strings.Repeat("a", 64)

	for _, path := range []string{"/job/", "/job//artifacts", "/job/" + jobID + "/artifacts/extra"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s expected 404, got %d", path, rec.Code)
		}
	}
}

func TestPOSTJob_ContentTypeValidation(t *testing.T) {
	handler := newHandler()

	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rec.Code)
	}
}

func TestPOSTJob_RequestBodyTooLarge(t *testing.T) {
	handler := newHandler()

	oversizedBody := `{"handoff":{"productKey":"` + strings.Repeat("x", int(maxJobSubmissionBodyBytes)) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(oversizedBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestPOSTJob_ConcurrentSubmission(t *testing.T) {
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{JobQueue: queue})
	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))

	const workers = 10
	type result struct {
		code  int
		jobID string
		err   error
	}

	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			res := result{code: rec.Code}
			if rec.Code == http.StatusAccepted {
				var payload jobSubmissionResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
					res.err = err
				} else {
					res.jobID = payload.JobID
				}
			}
			results <- res
		}()
	}

	wg.Wait()
	close(results)

	var canonicalJobID string
	for res := range results {
		if res.err != nil {
			t.Fatalf("failed to decode concurrent response: %v", res.err)
		}
		if res.code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d", res.code)
		}
		if canonicalJobID == "" {
			canonicalJobID = res.jobID
			continue
		}
		if res.jobID != canonicalJobID {
			t.Fatalf("expected same job id across submissions, got %q and %q", canonicalJobID, res.jobID)
		}
	}
	if queue.Len() != 1 {
		t.Fatalf("queue Len = %d, want 1", queue.Len())
	}
	if !queue.Contains(canonicalJobID) {
		t.Fatalf("queue missing canonical job %q", canonicalJobID)
	}
}

func TestPOSTJob_ConcurrentSubmissionsRetainDistinctJobsExactlyOnce(t *testing.T) {
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{JobQueue: queue})
	packages := []*handoff.Package{
		mustAPITestPackage(t, "widget"),
		mustAPITestPackage(t, "gear"),
		mustAPITestPackage(t, "bracket"),
		mustAPITestPackage(t, "panel"),
	}

	var wg sync.WaitGroup
	for _, pkg := range packages {
		pkg := pkg
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(pkg))))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusAccepted {
					t.Errorf("POST /job for %s returned %d body=%q", pkg.JobID, rec.Code, rec.Body.String())
				}
			}()
		}
	}
	wg.Wait()

	if queue.Len() != len(packages) {
		t.Fatalf("queue Len = %d, want %d", queue.Len(), len(packages))
	}

	seen := make(map[string]struct{}, len(packages))
	for range packages {
		pkg, ok := queue.Dequeue()
		if !ok {
			t.Fatal("expected queued package")
		}
		if _, exists := seen[pkg.JobID]; exists {
			t.Fatalf("duplicate dequeued jobID %q", pkg.JobID)
		}
		seen[pkg.JobID] = struct{}{}
	}
	for _, pkg := range packages {
		if _, ok := seen[pkg.JobID]; !ok {
			t.Fatalf("missing jobID %q", pkg.JobID)
		}
	}
}

func TestPOSTJob_RuntimeIsAsynchronous(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")
	status := mustGetJobStatus(t, handler, jobID)
	if status.State != jobstatus.StateQueued {
		t.Fatalf("state before runtime start = %q, want %q", status.State, jobstatus.StateQueued)
	}

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	status = waitForJobState(t, handler, jobID, jobstatus.StateRunning)
	if status.StartedAt == nil {
		t.Fatal("startedAt should be populated once execution begins")
	}
}

func TestJobRuntime_StatusPollingIsStableAcrossQueuedRunningAndFinalStates(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")

	queued := mustGetJobStatus(t, handler, jobID)
	if queued.State != jobstatus.StateQueued {
		t.Fatalf("initial state = %q, want %q", queued.State, jobstatus.StateQueued)
	}
	for i := 0; i < 3; i++ {
		again := mustGetJobStatus(t, handler, jobID)
		if !reflect.DeepEqual(again, queued) {
			t.Fatalf("queued status should stay stable across repeated reads:\nfirst=%#v\nread%d=%#v", queued, i+1, again)
		}
	}

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	running := waitForJobState(t, handler, jobID, jobstatus.StateRunning)
	if running.StartedAt == nil {
		t.Fatal("running status should include startedAt")
	}
	for i := 0; i < 3; i++ {
		again := mustGetJobStatus(t, handler, jobID)
		if !reflect.DeepEqual(again, running) {
			t.Fatalf("running status should stay stable across repeated reads:\nfirst=%#v\nread%d=%#v", running, i+1, again)
		}
	}

	factory.Release(jobID)
	succeeded := waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)
	for i := 0; i < 3; i++ {
		again := mustGetJobStatus(t, handler, jobID)
		if !reflect.DeepEqual(again, succeeded) {
			t.Fatalf("final success status should stay stable across repeated reads:\nfirst=%#v\nread%d=%#v", succeeded, i+1, again)
		}
	}
}

func TestJobRuntime_StatusProgressionSuccess(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	running := waitForJobState(t, handler, jobID, jobstatus.StateRunning)
	if running.EndedAt != nil {
		t.Fatalf("running status endedAt = %v, want nil", running.EndedAt)
	}

	factory.Release(jobID)
	succeeded := waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)
	if succeeded.EndedAt == nil {
		t.Fatal("succeeded status should include endedAt")
	}
	if succeeded.Error != nil {
		t.Fatalf("succeeded status error = %#v, want nil", succeeded.Error)
	}
}

func TestJobRuntime_StatusProgressionFailure(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        queue,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")
	factory.SetError(jobID, &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "2",
		Err:        errors.New("runner exploded"),
		RetryCount: 2,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("failed status should include error details")
	}
	if failed.Error.Message != "runner exploded" {
		t.Fatalf("error message = %q, want %q", failed.Error.Message, "runner exploded")
	}
	if failed.Error.ProductID != "widget" {
		t.Fatalf("error productId = %q, want %q", failed.Error.ProductID, "widget")
	}
	if failed.Error.StepID != "2" {
		t.Fatalf("error stepId = %q, want %q", failed.Error.StepID, "2")
	}
	if failed.Error.RetryCount != 2 {
		t.Fatalf("error retryCount = %d, want 2", failed.Error.RetryCount)
	}
	if got := factory.ExecutionCount(jobID); got != 1 {
		t.Fatalf("execution count = %d, want 1", got)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len after failed execution = %d, want 0", queue.Len())
	}
	if queue.Contains(jobID) {
		t.Fatalf("queue should not contain failed job %q after finalization", jobID)
	}
}

func TestJobRuntime_VerificationFailureSurfacedAsFinalFailedState(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")
	verifyErr := &verification.VerifyError{
		Class:   verification.FailureClassMetadataMismatch,
		Message: "verification failed: metadata \"working_copy_sha256\" mismatch",
		Err:     verification.ErrMetadataMismatch,
	}
	factory.SetError(jobID, &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "2",
		Err:        fmt.Errorf("verify observed execution state: %w", verifyErr),
		RetryCount: 1,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	running := waitForJobState(t, handler, jobID, jobstatus.StateRunning)
	if running.StartedAt == nil {
		t.Fatal("running status should include startedAt")
	}

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("verification failure should surface job error details")
	}
	if failed.Error.Message != `verify observed execution state: verification failed: metadata "working_copy_sha256" mismatch` {
		t.Fatalf("error message = %q", failed.Error.Message)
	}
	if failed.Error.ProductID != "widget" {
		t.Fatalf("error productId = %q, want %q", failed.Error.ProductID, "widget")
	}
	if failed.Error.StepID != "2" {
		t.Fatalf("error stepId = %q, want %q", failed.Error.StepID, "2")
	}
	if failed.Error.Timeout || failed.Error.Canceled {
		t.Fatalf("verification failure should remain a plain failure, got timeout=%v canceled=%v", failed.Error.Timeout, failed.Error.Canceled)
	}

	for i := 0; i < 3; i++ {
		again := mustGetJobStatus(t, handler, jobID)
		if !reflect.DeepEqual(again, failed) {
			t.Fatalf("final verification-failure status should stay stable across repeated reads:\nfirst=%#v\nread%d=%#v", failed, i+1, again)
		}
	}
}

func TestJobRuntime_WritesSuccessReportForCompletedJob(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	pkg := mustAPITestPackage(t, "widget")
	setHandlerRuntimeClockSequence(t, handler,
		time.Date(2030, time.January, 2, 10, 0, 0, 0, time.UTC),
		time.Date(2030, time.January, 2, 10, 0, 1, 0, time.UTC),
	)

	jobID := submitSpecificAPITestJob(t, handler, pkg)

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	succeeded := waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)
	if succeeded.Error != nil {
		t.Fatalf("succeeded status error = %#v, want nil", succeeded.Error)
	}

	reportBytes, runReport := mustReadRuntimeReport(t, artifactStore.BaseDir(), pkg)
	if len(reportBytes) == 0 || reportBytes[len(reportBytes)-1] != '\n' {
		t.Fatalf("report.json must be newline-terminated, got %q", string(reportBytes))
	}
	if runReport.SchemaVersion != report.SchemaVersion {
		t.Fatalf("report schemaVersion = %q, want %q", runReport.SchemaVersion, report.SchemaVersion)
	}
	if runReport.Status != report.StatusSuccess {
		t.Fatalf("report status = %q, want %q", runReport.Status, report.StatusSuccess)
	}
	if runReport.PlanHash != pkg.Manifest.PlanHash {
		t.Fatalf("report planHash = %q, want %q", runReport.PlanHash, pkg.Manifest.PlanHash)
	}
	if len(runReport.Jobs) != 1 {
		t.Fatalf("report jobs len = %d, want 1", len(runReport.Jobs))
	}
	if runReport.Jobs[0].JobID != jobID {
		t.Fatalf("report jobId = %q, want %q", runReport.Jobs[0].JobID, jobID)
	}
	if runReport.Jobs[0].ProductKey != pkg.ProductKey {
		t.Fatalf("report productKey = %q, want %q", runReport.Jobs[0].ProductKey, pkg.ProductKey)
	}

	for i := 0; i < 3; i++ {
		again := mustGetJobStatus(t, handler, jobID)
		if !reflect.DeepEqual(again, succeeded) {
			t.Fatalf("final success status should stay stable across repeated reads:\nfirst=%#v\nread%d=%#v", succeeded, i+1, again)
		}
	}
}

func TestJobRuntime_WritesFailureReportForExecutionError(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        queue,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	pkg := mustAPITestPackage(t, "widget")
	jobID := submitSpecificAPITestJob(t, handler, pkg)
	factory.SetError(jobID, &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "2",
		Err:        errors.New("runner exploded"),
		RetryCount: 2,
	})
	setHandlerRuntimeClockSequence(t, handler,
		time.Date(2030, time.January, 2, 11, 0, 0, 0, time.UTC),
		time.Date(2030, time.January, 2, 11, 0, 1, 0, time.UTC),
	)

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("failed status should include error details")
	}

	_, runReport := mustReadRuntimeReport(t, artifactStore.BaseDir(), pkg)
	if runReport.Status != report.StatusFailed {
		t.Fatalf("report status = %q, want %q", runReport.Status, report.StatusFailed)
	}
	if runReport.Error == nil {
		t.Fatal("report error should be populated")
	}
	if runReport.Error.Message != "runner exploded" {
		t.Fatalf("report error message = %q, want %q", runReport.Error.Message, "runner exploded")
	}
	if runReport.Error.ProductID != "widget" {
		t.Fatalf("report error productId = %q, want %q", runReport.Error.ProductID, "widget")
	}
	if runReport.Error.StepID != "2" {
		t.Fatalf("report error stepId = %q, want %q", runReport.Error.StepID, "2")
	}
	if runReport.Error.RetryCount != 2 {
		t.Fatalf("report error retryCount = %d, want 2", runReport.Error.RetryCount)
	}
	if got := factory.ExecutionCount(jobID); got != 1 {
		t.Fatalf("execution count = %d, want 1", got)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len after failed execution report = %d, want 0", queue.Len())
	}
	if queue.Contains(jobID) {
		t.Fatalf("queue should not contain failed job %q after report write", jobID)
	}
}

func TestJobRuntime_WritesFailureReportForVerificationError(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	pkg := mustAPITestPackage(t, "widget")
	jobID := submitSpecificAPITestJob(t, handler, pkg)
	verifyErr := &verification.VerifyError{
		Class:   verification.FailureClassMetadataMismatch,
		Message: `observed metadata "working_copy_sha256" mismatch: want "aaa", got "bbb"`,
		Err:     verification.ErrMetadataMismatch,
	}
	factory.SetError(jobID, &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "2",
		Err:        fmt.Errorf("verify observed execution state: %w", verifyErr),
		RetryCount: 1,
	})
	setHandlerRuntimeClockSequence(t, handler,
		time.Date(2030, time.January, 2, 12, 0, 0, 0, time.UTC),
		time.Date(2030, time.January, 2, 12, 0, 1, 0, time.UTC),
	)

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("verification failure should surface job error details")
	}

	_, runReport := mustReadRuntimeReport(t, artifactStore.BaseDir(), pkg)
	if runReport.Status != report.StatusFailed {
		t.Fatalf("report status = %q, want %q", runReport.Status, report.StatusFailed)
	}
	if runReport.Error == nil {
		t.Fatal("report error should be populated")
	}
	if runReport.Error.Classification != verification.FailureClassMetadataMismatch {
		t.Fatalf("report classification = %q, want %q", runReport.Error.Classification, verification.FailureClassMetadataMismatch)
	}
	if runReport.Error.Message != verifyErr.Message {
		t.Fatalf("report error message = %q, want %q", runReport.Error.Message, verifyErr.Message)
	}
	if runReport.Error.Timeout || runReport.Error.Canceled {
		t.Fatalf("verification failure should remain plain failed in report, got timeout=%v canceled=%v", runReport.Error.Timeout, runReport.Error.Canceled)
	}

	for i := 0; i < 3; i++ {
		again := mustGetJobStatus(t, handler, jobID)
		if !reflect.DeepEqual(again, failed) {
			t.Fatalf("final verification-failure status should stay stable across repeated reads:\nfirst=%#v\nread%d=%#v", failed, i+1, again)
		}
	}
}

func TestJobRuntime_ReportMapsCanceledAndTimeoutOutcomes(t *testing.T) {
	tests := []struct {
		name         string
		execErr      *executor.ExecutionError
		wantJobState jobstatus.State
		wantReport   report.Status
		wantTimeout  bool
		wantCanceled bool
	}{
		{
			name: "canceled",
			execErr: &executor.ExecutionError{
				ProductID:  "widget",
				StepID:     "2",
				Err:        context.Canceled,
				RetryCount: 1,
				Canceled:   true,
			},
			wantJobState: jobstatus.StateCanceled,
			wantReport:   report.StatusCanceled,
			wantCanceled: true,
		},
		{
			name: "timeout",
			execErr: &executor.ExecutionError{
				ProductID:  "widget",
				StepID:     "2",
				Err:        context.DeadlineExceeded,
				RetryCount: 1,
				Timeout:    true,
			},
			wantJobState: jobstatus.StateFailed,
			wantReport:   report.StatusTimeout,
			wantTimeout:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			submissionStore := newInMemorySubmissionStore()
			artifactStore := artifact.NewFileSystemStore(t.TempDir())
			factory := newRuntimeTestFactory(t)
			factory.runRoot = artifactStore.BaseDir()
			handler := newHandler(HandlerOptions{
				SubmissionStore: submissionStore,
				ArtifactStore:   artifactStore,
				JobQueue:        newInMemoryJobQueue(),
				ExecutorFactory: factory.Executor,
				PollInterval:    time.Millisecond,
			})

			pkg := mustAPITestPackage(t, "widget")
			jobID := submitSpecificAPITestJob(t, handler, pkg)
			factory.SetError(jobID, tt.execErr)
			setHandlerRuntimeClockSequence(t, handler,
				time.Date(2030, time.January, 2, 13, 0, 0, 0, time.UTC),
				time.Date(2030, time.January, 2, 13, 0, 1, 0, time.UTC),
			)

			cancel, wait := startHandlerRuntime(t, handler)
			defer func() {
				cancel()
				wait()
			}()

			factory.WaitForStart(t, jobID)
			_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)
			factory.Release(jobID)

			status := waitForJobState(t, handler, jobID, tt.wantJobState)
			if status.Error == nil {
				t.Fatal("terminal status should include failure details")
			}

			_, runReport := mustReadRuntimeReport(t, artifactStore.BaseDir(), pkg)
			if runReport.Status != tt.wantReport {
				t.Fatalf("report status = %q, want %q", runReport.Status, tt.wantReport)
			}
			if runReport.Error == nil {
				t.Fatal("report error should be populated")
			}
			if runReport.Error.Timeout != tt.wantTimeout {
				t.Fatalf("report timeout = %v, want %v", runReport.Error.Timeout, tt.wantTimeout)
			}
			if runReport.Error.Canceled != tt.wantCanceled {
				t.Fatalf("report canceled = %v, want %v", runReport.Error.Canceled, tt.wantCanceled)
			}
		})
	}
}

func TestJobRuntime_NonSucceededFinalOutcomesDoNotRegisterCompletionArtifacts(t *testing.T) {
	tests := []struct {
		name         string
		acceptance   artifact.AcceptanceContext
		execErr      *executor.ExecutionError
		wantJobState jobstatus.State
	}{
		{
			name: "failed",
			acceptance: artifact.AcceptanceContext{
				FinalOutcome:        artifact.FinalOutcomeFailed,
				VerificationOutcome: artifact.VerificationOutcomePassed,
			},
			execErr: &executor.ExecutionError{
				ProductID:  "widget",
				StepID:     "2",
				Err:        errors.New("runner failed"),
				RetryCount: 1,
			},
			wantJobState: jobstatus.StateFailed,
		},
		{
			name: "canceled",
			acceptance: artifact.AcceptanceContext{
				FinalOutcome:        artifact.FinalOutcomeCanceled,
				VerificationOutcome: artifact.VerificationOutcomePassed,
			},
			execErr: &executor.ExecutionError{
				ProductID:  "widget",
				StepID:     "2",
				Err:        context.Canceled,
				RetryCount: 1,
				Canceled:   true,
			},
			wantJobState: jobstatus.StateCanceled,
		},
		{
			name: "timed out",
			acceptance: artifact.AcceptanceContext{
				FinalOutcome:        artifact.FinalOutcomeTimedOut,
				VerificationOutcome: artifact.VerificationOutcomePassed,
			},
			execErr: &executor.ExecutionError{
				ProductID:  "widget",
				StepID:     "2",
				Err:        context.DeadlineExceeded,
				RetryCount: 1,
				Timeout:    true,
			},
			wantJobState: jobstatus.StateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			submissionStore := newInMemorySubmissionStore()
			artifactStore := artifact.NewFileSystemStore(t.TempDir())
			factory := newRuntimeTestFactory(t)
			factory.runRoot = artifactStore.BaseDir()
			handler := newHandler(HandlerOptions{
				SubmissionStore: submissionStore,
				ArtifactStore:   artifactStore,
				JobQueue:        newInMemoryJobQueue(),
				ExecutorFactory: factory.Executor,
				PollInterval:    time.Millisecond,
			})

			jobID := submitAPITestJob(t, handler, "widget")
			factory.SetAcceptanceContext(jobID, tt.acceptance)
			factory.SetError(jobID, tt.execErr)

			cancel, wait := startHandlerRuntime(t, handler)
			defer func() {
				cancel()
				wait()
			}()

			factory.WaitForStart(t, jobID)
			_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)
			factory.Release(jobID)
			_ = waitForJobState(t, handler, jobID, tt.wantJobState)

			gotArtifacts := mustGetJobArtifacts(t, handler, jobID)
			if len(gotArtifacts.Artifacts) != 0 {
				t.Fatalf("artifacts after non-succeeded final outcome = %+v, want empty", gotArtifacts.Artifacts)
			}

			storeItems, err := artifactStore.List()
			if err != nil {
				t.Fatalf("artifact store List returned error: %v", err)
			}
			if len(storeItems) != 0 {
				t.Fatalf("artifact store after non-succeeded final outcome = %+v, want empty", storeItems)
			}
		})
	}
}

func TestJobRuntime_ReportIsDeterministicForIdenticalVerificationFailure(t *testing.T) {
	verifyErr := &verification.VerifyError{
		Class:   verification.FailureClassMetadataMismatch,
		Message: `observed metadata "working_copy_sha256" mismatch: want "aaa", got "bbb"`,
		Err:     verification.ErrMetadataMismatch,
	}

	runOnce := func(t *testing.T) []byte {
		t.Helper()

		submissionStore := newInMemorySubmissionStore()
		artifactStore := artifact.NewFileSystemStore(t.TempDir())
		factory := newRuntimeTestFactory(t)
		factory.runRoot = artifactStore.BaseDir()
		handler := newHandler(HandlerOptions{
			SubmissionStore: submissionStore,
			ArtifactStore:   artifactStore,
			JobQueue:        newInMemoryJobQueue(),
			ExecutorFactory: factory.Executor,
			PollInterval:    time.Millisecond,
		})

		pkg := mustAPITestPackage(t, "widget")
		jobID := submitSpecificAPITestJob(t, handler, pkg)
		factory.SetError(jobID, &executor.ExecutionError{
			ProductID:  "widget",
			StepID:     "2",
			Err:        fmt.Errorf("verify observed execution state: %w", verifyErr),
			RetryCount: 1,
		})
		setHandlerRuntimeClockSequence(t, handler,
			time.Date(2030, time.January, 2, 14, 0, 0, 0, time.UTC),
			time.Date(2030, time.January, 2, 14, 0, 1, 0, time.UTC),
		)

		cancel, wait := startHandlerRuntime(t, handler)
		defer func() {
			cancel()
			wait()
		}()

		factory.WaitForStart(t, jobID)
		_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)
		factory.Release(jobID)
		_ = waitForJobState(t, handler, jobID, jobstatus.StateFailed)

		reportBytes, _ := mustReadRuntimeReport(t, artifactStore.BaseDir(), pkg)
		return reportBytes
	}

	first := runOnce(t)
	second := runOnce(t)
	if !bytes.Equal(first, second) {
		t.Fatalf("expected byte-identical runtime reports across identical verification failures\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestJobRuntime_ArtifactsAppearOnlyAfterSuccessfulCompletion(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	before := mustGetJobArtifacts(t, handler, jobID)
	if len(before.Artifacts) != 0 {
		t.Fatalf("artifacts before completion = %+v, want empty", before.Artifacts)
	}

	factory.Release(jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)

	after := mustGetJobArtifacts(t, handler, jobID)
	if len(after.Artifacts) != 2 {
		t.Fatalf("artifacts after completion = %d, want 2", len(after.Artifacts))
	}
	for _, item := range after.Artifacts {
		if item.JobID != jobID {
			t.Fatalf("artifact jobId = %q, want %q", item.JobID, jobID)
		}
		if item.Class != artifact.ArtifactClassExecutionOutput {
			t.Fatalf("artifact class = %q, want %q", item.Class, artifact.ArtifactClassExecutionOutput)
		}
	}
}

func TestJobRuntime_DoesNotProduceVerifiedArtifactsWithoutExplicitVerificationPass(t *testing.T) {
	testCases := []struct {
		name         string
		verification artifact.VerificationOutcome
	}{
		{name: "not run", verification: artifact.VerificationOutcomeNotRun},
		{name: "unknown", verification: artifact.VerificationOutcomeUnknown},
		{name: "skipped", verification: artifact.VerificationOutcomeSkipped},
		{name: "failed", verification: artifact.VerificationOutcomeFailed},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			submissionStore := newInMemorySubmissionStore()
			artifactStore := artifact.NewFileSystemStore(t.TempDir())
			factory := newRuntimeTestFactory(t)
			factory.runRoot = artifactStore.BaseDir()
			handler := newHandler(HandlerOptions{
				SubmissionStore: submissionStore,
				ArtifactStore:   artifactStore,
				JobQueue:        newInMemoryJobQueue(),
				ExecutorFactory: factory.Executor,
				PollInterval:    time.Millisecond,
			})

			jobID := submitAPITestJob(t, handler, "widget")
			factory.SetAcceptanceContext(jobID, artifact.AcceptanceContext{
				FinalOutcome:        artifact.FinalOutcomeSucceeded,
				VerificationOutcome: tt.verification,
			})

			cancel, wait := startHandlerRuntime(t, handler)
			defer func() {
				cancel()
				wait()
			}()

			factory.WaitForStart(t, jobID)
			_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

			factory.Release(jobID)
			_ = waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)

			artifacts := mustGetJobArtifacts(t, handler, jobID)
			if len(artifacts.Artifacts) != 2 {
				t.Fatalf("artifact count = %d, want 2", len(artifacts.Artifacts))
			}
			for _, item := range artifacts.Artifacts {
				if item.Class != artifact.ArtifactClassExecutionOutput {
					t.Fatalf("artifact class = %q, want %q", item.Class, artifact.ArtifactClassExecutionOutput)
				}
			}
		})
	}
}

func TestJobRuntime_SuccessAloneDoesNotProduceVerifiedArtifacts(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)

	artifacts := mustGetJobArtifacts(t, handler, jobID)
	if len(artifacts.Artifacts) != 2 {
		t.Fatalf("artifact count = %d, want 2", len(artifacts.Artifacts))
	}
	for _, item := range artifacts.Artifacts {
		if item.Class != artifact.ArtifactClassExecutionOutput {
			t.Fatalf("artifact class = %q, want %q", item.Class, artifact.ArtifactClassExecutionOutput)
		}
	}
}

func TestJobRuntime_VerificationFailureKeepsJobFailedAndArtifactsHidden(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")
	factory.SetAcceptanceContext(jobID, artifact.AcceptanceContext{
		FinalOutcome:        artifact.FinalOutcomeFailed,
		VerificationOutcome: artifact.VerificationOutcomeFailed,
	})
	factory.SetError(jobID, &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "2",
		Err:        verification.ErrMetadataMismatch,
		RetryCount: 1,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("verification failure should surface a job error")
	}

	artifacts := mustGetJobArtifacts(t, handler, jobID)
	if len(artifacts.Artifacts) != 0 {
		t.Fatalf("artifacts after verification failure = %+v, want empty", artifacts.Artifacts)
	}
}

func TestCompletionArtifactRecords_ClassifiesProducedOutputsAsExecutionOutputs(t *testing.T) {
	pkg := mustAPITestPackage(t, "widget")

	records, err := completionArtifactRecords(t.TempDir(), pkg)
	if err != nil {
		t.Fatalf("completionArtifactRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("record count = %d, want 2", len(records))
	}

	for _, record := range records {
		if record.meta.Class != artifact.ArtifactClassExecutionOutput {
			t.Fatalf("record for %s class = %q, want %q", record.meta.Filename, record.meta.Class, artifact.ArtifactClassExecutionOutput)
		}
		if record.meta.Class == artifact.ArtifactClassVerified {
			t.Fatalf("runtime completion must not produce verified artifacts: %+v", record.meta)
		}
	}
}

func TestAcceptedCompletionArtifactBatch_RejectsVerifiedArtifactWithoutExplicitVerificationPass(t *testing.T) {
	baseDir := t.TempDir()
	store := artifact.NewFileSystemStore(baseDir)
	productDir := filepath.Join(baseDir, "jobs", "job-1")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatalf("failed to create product dir: %v", err)
	}

	csvPath := filepath.Join(productDir, "widget.csv")
	if err := os.WriteFile(csvPath, []byte("value\n42\n"), 0o644); err != nil {
		t.Fatalf("failed to write csv artifact: %v", err)
	}
	stepPath := filepath.Join(productDir, "widget.step")
	if err := os.WriteFile(stepPath, []byte("ISO-10303-21; runtime test"), 0o644); err != nil {
		t.Fatalf("failed to write step artifact: %v", err)
	}

	records := []completionArtifactRecord{
		{
			absPath: csvPath,
			meta: artifact.Artifact{
				Class:     artifact.ArtifactClassExecutionOutput,
				Type:      artifact.ArtifactTypeCSV,
				JobID:     "job-1",
				ProductID: "widget",
				StepID:    "0",
				Filename:  "widget.csv",
			},
		},
		{
			absPath: stepPath,
			meta: artifact.Artifact{
				Class:     artifact.ArtifactClassVerified,
				Type:      artifact.ArtifactTypeSTEP,
				JobID:     "job-1",
				ProductID: "widget",
				StepID:    "2",
				Filename:  "widget.step",
			},
		},
	}

	_, err := acceptedCompletionArtifactBatch(records, artifact.AcceptanceContext{
		FinalOutcome:        artifact.FinalOutcomeSucceeded,
		VerificationOutcome: artifact.VerificationOutcomeNotRun,
	})
	if err == nil {
		t.Fatal("expected verified artifact acceptance to fail without explicit verification pass")
	}

	var batchErr *artifact.BatchAcceptanceError
	if !errors.As(err, &batchErr) {
		t.Fatalf("expected BatchAcceptanceError, got %T", err)
	}
	if batchErr.Index != 1 {
		t.Fatalf("batch error index = %d, want 1", batchErr.Index)
	}
	if got, want := batchErr.Err.Error(), `artifact registration rejected for class "verified_artifact": explicit engine verification pass is required`; got != want {
		t.Fatalf("batch error = %q, want %q", got, want)
	}

	items, listErr := store.List()
	if listErr != nil {
		t.Fatalf("artifact store List returned error: %v", listErr)
	}
	if len(items) != 0 {
		t.Fatalf("artifact store should remain empty after acceptance failure, got %+v", items)
	}
}

func TestAcceptedCompletionArtifactBatch_RepeatedRejectionLeavesStoreEmpty(t *testing.T) {
	baseDir := t.TempDir()
	store := artifact.NewFileSystemStore(baseDir)
	productDir := filepath.Join(baseDir, "jobs", "job-1")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatalf("failed to create product dir: %v", err)
	}

	csvPath := filepath.Join(productDir, "widget.csv")
	if err := os.WriteFile(csvPath, []byte("value\n42\n"), 0o644); err != nil {
		t.Fatalf("failed to write csv artifact: %v", err)
	}
	stepPath := filepath.Join(productDir, "widget.step")
	if err := os.WriteFile(stepPath, []byte("ISO-10303-21; runtime test"), 0o644); err != nil {
		t.Fatalf("failed to write step artifact: %v", err)
	}

	records := []completionArtifactRecord{
		{
			absPath: csvPath,
			meta: artifact.Artifact{
				Class:     artifact.ArtifactClassExecutionOutput,
				Type:      artifact.ArtifactTypeCSV,
				JobID:     "job-1",
				ProductID: "widget",
				StepID:    "0",
				Filename:  "widget.csv",
			},
		},
		{
			absPath: stepPath,
			meta: artifact.Artifact{
				Class:     artifact.ArtifactClassVerified,
				Type:      artifact.ArtifactTypeSTEP,
				JobID:     "job-1",
				ProductID: "widget",
				StepID:    "2",
				Filename:  "widget.step",
			},
		},
	}

	first, firstErr := acceptedCompletionArtifactBatch(records, artifact.AcceptanceContext{
		FinalOutcome:        artifact.FinalOutcomeSucceeded,
		VerificationOutcome: artifact.VerificationOutcomeSkipped,
	})
	second, secondErr := acceptedCompletionArtifactBatch(records, artifact.AcceptanceContext{
		FinalOutcome:        artifact.FinalOutcomeSucceeded,
		VerificationOutcome: artifact.VerificationOutcomeSkipped,
	})
	if firstErr == nil || secondErr == nil {
		t.Fatal("expected repeated acceptance attempts to fail")
	}
	if first != nil || second != nil {
		t.Fatalf("rejected batches must not produce partial records: first=%v second=%v", first, second)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("rejected batch messages must remain stable, got %q and %q", firstErr.Error(), secondErr.Error())
	}

	items, listErr := store.List()
	if listErr != nil {
		t.Fatalf("artifact store List returned error: %v", listErr)
	}
	if len(items) != 0 {
		t.Fatalf("artifact store should remain empty after repeated acceptance failures, got %+v", items)
	}
}

func TestAcceptedCompletionArtifactBatch_AcceptsVerifiedArtifactOnExplicitVerificationPass(t *testing.T) {
	baseDir := t.TempDir()
	productDir := filepath.Join(baseDir, "jobs", "job-1")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatalf("failed to create product dir: %v", err)
	}

	records := []completionArtifactRecord{
		{
			absPath: filepath.Join(productDir, "widget.step"),
			meta: artifact.Artifact{
				Class:     artifact.ArtifactClassVerified,
				Type:      artifact.ArtifactTypeSTEP,
				JobID:     "job-1",
				ProductID: "widget",
				StepID:    "2",
				Filename:  "widget.step",
			},
		},
		{
			absPath: filepath.Join(productDir, "widget.csv"),
			meta: artifact.Artifact{
				Class:     artifact.ArtifactClassExecutionOutput,
				Type:      artifact.ArtifactTypeCSV,
				JobID:     "job-1",
				ProductID: "widget",
				StepID:    "0",
				Filename:  "widget.csv",
			},
		},
	}

	batch, err := acceptedCompletionArtifactBatch(records, artifact.AcceptanceContext{
		FinalOutcome:        artifact.FinalOutcomeSucceeded,
		VerificationOutcome: artifact.VerificationOutcomePassed,
	})
	if err != nil {
		t.Fatalf("acceptedCompletionArtifactBatch returned error: %v", err)
	}
	if len(batch) != len(records) {
		t.Fatalf("accepted batch length = %d, want %d", len(batch), len(records))
	}
	for i := range batch {
		if batch[i].Meta != records[i].meta {
			t.Fatalf("accepted batch meta[%d] = %+v, want %+v", i, batch[i].Meta, records[i].meta)
		}
		if batch[i].AbsPath != records[i].absPath {
			t.Fatalf("accepted batch path[%d] = %q, want %q", i, batch[i].AbsPath, records[i].absPath)
		}
	}
}

func TestDefaultExecutorFactory_UsesProductDirAwareExecutorWithoutStepTimeArtifacts(t *testing.T) {
	store := artifact.NewFileSystemStore(t.TempDir())
	factory, err := defaultExecutorFactory(store, store.BaseDir())
	if err != nil {
		t.Fatalf("defaultExecutorFactory returned error: %v", err)
	}

	pkg := mustAPITestPackage(t, "widget")
	exec := factory(pkg)
	concrete, ok := exec.(*executor.Executor)
	if !ok {
		t.Fatalf("executor type = %T, want *executor.Executor", exec)
	}

	value := reflect.ValueOf(concrete).Elem()
	gotProductDir := value.FieldByName("productDir").String()
	wantProductDir := runtimeProductDir(store.BaseDir(), pkg)
	if gotProductDir != wantProductDir {
		t.Fatalf("executor productDir = %q, want %q", gotProductDir, wantProductDir)
	}
	if !value.FieldByName("store").IsNil() {
		t.Fatal("default runtime executor must not enable step-time artifact registration")
	}
}

func TestJobRuntime_DuplicateSubmissionDuringAsyncExecutionExecutesExactlyOnce(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	queue := newInMemoryJobQueue()
	factory := newIdempotencyRuntimeTestFactory(t, artifactStore.BaseDir())
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        queue,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))
	submit := func() jobSubmissionResponse {
		t.Helper()

		req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("POST /job expected 202, got %d body=%q", rec.Code, rec.Body.String())
		}

		var resp jobSubmissionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode submission response: %v", err)
		}
		return resp
	}

	first := submit()
	jobID := first.JobID

	factory.WaitForStart(t, jobID)
	running := waitForJobState(t, handler, jobID, jobstatus.StateRunning)
	if running.StartedAt == nil {
		t.Fatal("startedAt should be populated for in-flight job")
	}

	before := mustGetJobArtifacts(t, handler, jobID)
	if len(before.Artifacts) != 0 {
		t.Fatalf("artifacts before completion = %+v, want empty", before.Artifacts)
	}

	for i := 0; i < 2; i++ {
		dup := submit()
		if dup.JobID != jobID {
			t.Fatalf("duplicate submission jobId = %q, want %q", dup.JobID, jobID)
		}
		if dup.ProductKey != first.ProductKey {
			t.Fatalf("duplicate submission productKey = %q, want %q", dup.ProductKey, first.ProductKey)
		}
		if dup.State != jobstatus.StateRunning {
			t.Fatalf("duplicate submission state = %q, want %q", dup.State, jobstatus.StateRunning)
		}
	}

	duringDuplicateSubmit := mustGetJobStatus(t, handler, jobID)
	if duringDuplicateSubmit.State != jobstatus.StateRunning {
		t.Fatalf("state after duplicate submissions = %q, want %q", duringDuplicateSubmit.State, jobstatus.StateRunning)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len after duplicate submissions = %d, want 0", queue.Len())
	}

	factory.Release()
	succeeded := waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)
	if succeeded.EndedAt == nil {
		t.Fatal("succeeded status should include endedAt")
	}

	again := mustGetJobStatus(t, handler, jobID)
	if !reflect.DeepEqual(again, succeeded) {
		t.Fatalf("final job status should be stable across repeated reads:\nfirst=%#v\nsecond=%#v", succeeded, again)
	}

	after := mustGetJobArtifacts(t, handler, jobID)
	if len(after.Artifacts) != 2 {
		t.Fatalf("artifacts after completion = %d, want 2", len(after.Artifacts))
	}
	for _, item := range after.Artifacts {
		if item.JobID != jobID {
			t.Fatalf("artifact jobId = %q, want %q", item.JobID, jobID)
		}
		if item.Class != artifact.ArtifactClassExecutionOutput {
			t.Fatalf("artifact class = %q, want %q", item.Class, artifact.ArtifactClassExecutionOutput)
		}
	}

	storeItems, err := artifactStore.List()
	if err != nil {
		t.Fatalf("artifact store List returned error: %v", err)
	}
	if len(storeItems) != 2 {
		t.Fatalf("artifact store count = %d, want 2", len(storeItems))
	}
	if !reflect.DeepEqual(storeItems, after.Artifacts) {
		t.Fatalf("artifact list mismatch:\nstore=%#v\nresponse=%#v", storeItems, after.Artifacts)
	}
	if got := factory.ExecutionCount(); got != 1 {
		t.Fatalf("execution count = %d, want 1", got)
	}
}

func TestJobRuntime_CompletionArtifactRegistrationFailureFailsJob(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        queue,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")
	factory.SetOmitDeclaredOutputs(jobID, true)

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("registration failure should surface a job error")
	}
	if !strings.Contains(failed.Error.Message, `register completion artifact "widget.csv"`) {
		t.Fatalf("error message = %q", failed.Error.Message)
	}

	artifacts := mustGetJobArtifacts(t, handler, jobID)
	if len(artifacts.Artifacts) != 0 {
		t.Fatalf("artifacts after registration failure = %+v, want empty", artifacts.Artifacts)
	}
	if got := factory.ExecutionCount(jobID); got != 1 {
		t.Fatalf("execution count = %d, want 1", got)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len after registration failure = %d, want 0", queue.Len())
	}
	if queue.Contains(jobID) {
		t.Fatalf("queue should not contain failed job %q after registration failure", jobID)
	}
}

func TestJobRuntime_MixedClassCompletionBatchRejectedAtomicallyBeforeVisibility(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        queue,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	pkg := mustAPITestPackage(t, "widget")
	jobID := submitSpecificAPITestJob(t, handler, pkg)

	previousRecordsFunc := completionArtifactRecordsFunc
	completionArtifactRecordsFunc = func(productDir string, gotPkg *handoff.Package) ([]completionArtifactRecord, error) {
		records, err := previousRecordsFunc(productDir, gotPkg)
		if err != nil {
			return nil, err
		}
		if gotPkg == nil || gotPkg.JobID != jobID {
			return records, nil
		}
		if len(records) < 2 {
			t.Fatalf("expected at least 2 completion records, got %d", len(records))
		}

		mixed := make([]completionArtifactRecord, 2)
		mixed[0] = records[0]
		mixed[0].meta.Class = artifact.ArtifactClassExecutionOutput
		mixed[1] = records[1]
		mixed[1].meta.Class = artifact.ArtifactClassVerified
		return mixed, nil
	}
	t.Cleanup(func() {
		completionArtifactRecordsFunc = previousRecordsFunc
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("mixed-class acceptance failure should surface a job error")
	}
	if got, want := failed.Error.Message, `register completion artifact "export_manifest_v1.json": artifact registration rejected for class "verified_artifact": explicit engine verification pass is required`; got != want {
		t.Fatalf("error message = %q, want %q", got, want)
	}

	artifacts := mustGetJobArtifacts(t, handler, jobID)
	if len(artifacts.Artifacts) != 0 {
		t.Fatalf("artifacts after mixed-class batch rejection = %+v, want empty", artifacts.Artifacts)
	}

	storeItems, err := artifactStore.List()
	if err != nil {
		t.Fatalf("artifact store List returned error: %v", err)
	}
	if len(storeItems) != 0 {
		t.Fatalf("artifact store count after mixed-class batch rejection = %d, want 0", len(storeItems))
	}

	_, runReport := mustReadRuntimeReport(t, artifactStore.BaseDir(), pkg)
	if runReport.Status != report.StatusFailed {
		t.Fatalf("report status = %q, want %q", runReport.Status, report.StatusFailed)
	}
	if runReport.Error == nil || runReport.Error.Message != failed.Error.Message {
		t.Fatalf("report error = %#v, want runtime registration failure details", runReport.Error)
	}
	if len(runReport.Artifacts) != 0 {
		t.Fatalf("report artifacts after mixed-class batch rejection = %+v, want empty", runReport.Artifacts)
	}
	if got := factory.ExecutionCount(jobID); got != 1 {
		t.Fatalf("execution count = %d, want 1", got)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len after mixed-class batch rejection = %d, want 0", queue.Len())
	}
	if queue.Contains(jobID) {
		t.Fatalf("queue should not contain failed job %q after mixed-class batch rejection", jobID)
	}
}

func TestJobRuntime_ReportWriteFailureDoesNotReexecuteOrChangeTerminalStatus(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	factory.runRoot = t.TempDir()
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        queue,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	badRunRoot := filepath.Join(t.TempDir(), "report-write-blocker")
	if err := os.WriteFile(badRunRoot, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("write bad report root: %v", err)
	}
	setHandlerRuntimeRunRoot(t, handler, badRunRoot)

	jobID := submitAPITestJob(t, handler, "widget")

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	succeeded := waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)
	if succeeded.Error != nil {
		t.Fatalf("succeeded status error = %#v, want nil", succeeded.Error)
	}
	if got := factory.ExecutionCount(jobID); got != 1 {
		t.Fatalf("execution count = %d, want 1", got)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len after report write failure = %d, want 0", queue.Len())
	}
	if queue.Contains(jobID) {
		t.Fatalf("queue should not contain succeeded job %q after report write failure", jobID)
	}

	for i := 0; i < 3; i++ {
		again := mustGetJobStatus(t, handler, jobID)
		if !reflect.DeepEqual(again, succeeded) {
			t.Fatalf("terminal status should stay stable after report write failure:\nfirst=%#v\nread%d=%#v", succeeded, i+1, again)
		}
	}
}

func TestJobRuntime_FailedExecutionDoesNotExposeCompletedArtifacts(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")
	factory.SetError(jobID, &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "2",
		Err:        errors.New("runner exploded"),
		RetryCount: 1,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil || failed.Error.Message != "runner exploded" {
		t.Fatalf("failed error = %#v, want runner failure details", failed.Error)
	}

	artifacts := mustGetJobArtifacts(t, handler, jobID)
	if len(artifacts.Artifacts) != 0 {
		t.Fatalf("artifacts after failed execution = %+v, want empty", artifacts.Artifacts)
	}
}

func TestJobRuntime_MidRegistrationFailureLeavesJobFailedAndGlobalStoreEmpty(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	baseStore := artifact.NewFileSystemStore(t.TempDir())
	artifactStore := newControlledCompletionArtifactStore(baseStore)
	artifactStore.FailOnCall(2, errors.New("injected registration failure"))
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("mid-registration failure should surface a job error")
	}
	if !strings.Contains(failed.Error.Message, "injected registration failure") {
		t.Fatalf("error message = %q", failed.Error.Message)
	}
	if got := artifactStore.RecordCalls(); got != 2 {
		t.Fatalf("RecordExisting calls = %d, want 2 to prove registration started before failing", got)
	}

	artifacts := mustGetJobArtifacts(t, handler, jobID)
	if len(artifacts.Artifacts) != 0 {
		t.Fatalf("job artifacts after partial registration failure = %+v, want empty", artifacts.Artifacts)
	}

	storeItems, err := artifactStore.List()
	if err != nil {
		t.Fatalf("artifact store List returned error: %v", err)
	}
	if len(storeItems) != 0 {
		t.Fatalf("artifact store count after partial registration failure = %d, want 0", len(storeItems))
	}
}

func TestJobRuntime_JobDoesNotBecomeSucceededBeforeCompletionRegistrationFinishes(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	baseStore := artifact.NewFileSystemStore(t.TempDir())
	artifactStore := newControlledCompletionArtifactStore(baseStore)
	artifactStore.BlockOnCall(1)
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handler, "widget")

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	artifactStore.WaitForBlockedCall(t)

	duringRegistration := mustGetJobStatus(t, handler, jobID)
	if duringRegistration.State == jobstatus.StateSucceeded {
		t.Fatalf("state during blocked completion registration = %q, must not be succeeded", duringRegistration.State)
	}
	if duringRegistration.State != jobstatus.StateRunning {
		t.Fatalf("state during blocked completion registration = %q, want %q", duringRegistration.State, jobstatus.StateRunning)
	}

	before := mustGetJobArtifacts(t, handler, jobID)
	if len(before.Artifacts) != 0 {
		t.Fatalf("job artifacts during blocked completion registration = %+v, want empty", before.Artifacts)
	}

	artifactStore.ReleaseBlockedCall()

	succeeded := waitForJobState(t, handler, jobID, jobstatus.StateSucceeded)
	if succeeded.Error != nil {
		t.Fatalf("succeeded status error = %#v, want nil", succeeded.Error)
	}

	after := mustGetJobArtifacts(t, handler, jobID)
	if len(after.Artifacts) != 2 {
		t.Fatalf("artifacts after completion registration = %d, want 2", len(after.Artifacts))
	}
}

func TestJobRuntime_ConcurrentCompletionRegistrationKeepsSuccessfulBatchIsolatedFromFailedBatch(t *testing.T) {
	sharedStore := newControlledCompletionArtifactStore(artifact.NewFileSystemStore(t.TempDir()))
	submissionStoreA := newInMemorySubmissionStore()
	submissionStoreB := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	factory.runRoot = sharedStore.BaseDir()

	handlerA := newHandler(HandlerOptions{
		SubmissionStore: submissionStoreA,
		ArtifactStore:   sharedStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})
	handlerB := newHandler(HandlerOptions{
		SubmissionStore: submissionStoreB,
		ArtifactStore:   sharedStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobAID := submitAPITestJob(t, handlerA, "widget")
	jobBID := submitAPITestJob(t, handlerB, "gear")
	sharedStore.BlockJobOnCall(jobBID, 1)
	sharedStore.FailJobOnCall(jobBID, 2, errors.New("injected concurrent registration failure"))

	cancelA, waitA := startHandlerRuntime(t, handlerA)
	defer func() {
		cancelA()
		waitA()
	}()
	cancelB, waitB := startHandlerRuntime(t, handlerB)
	defer func() {
		cancelB()
		waitB()
	}()

	factory.WaitForStarts(t, jobAID, jobBID)
	_ = waitForJobState(t, handlerA, jobAID, jobstatus.StateRunning)
	_ = waitForJobState(t, handlerB, jobBID, jobstatus.StateRunning)

	factory.Release(jobBID)
	sharedStore.WaitForJobBlockedCall(t, jobBID)

	factory.Release(jobAID)
	succeeded := waitForJobState(t, handlerA, jobAID, jobstatus.StateSucceeded)
	if succeeded.Error != nil {
		t.Fatalf("jobA succeeded status error = %#v, want nil", succeeded.Error)
	}

	jobAArtifacts := mustGetJobArtifacts(t, handlerA, jobAID)
	assertRuntimeArtifactFilenames(t, jobAArtifacts.Artifacts, []string{"widget.csv", planner.ExportManifestFilename})
	for _, item := range jobAArtifacts.Artifacts {
		if item.JobID != jobAID {
			t.Fatalf("jobA artifact jobId = %q, want %q", item.JobID, jobAID)
		}
	}

	sharedStore.ReleaseJobBlockedCall(jobBID)
	failed := waitForJobState(t, handlerB, jobBID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("jobB should fail during completion registration")
	}
	if !strings.Contains(failed.Error.Message, "injected concurrent registration failure") {
		t.Fatalf("jobB error message = %q", failed.Error.Message)
	}

	jobBArtifacts := mustGetJobArtifacts(t, handlerB, jobBID)
	if len(jobBArtifacts.Artifacts) != 0 {
		t.Fatalf("jobB artifacts after registration failure = %+v, want empty", jobBArtifacts.Artifacts)
	}

	storeItems, err := sharedStore.List()
	if err != nil {
		t.Fatalf("artifact store List returned error: %v", err)
	}

	jobAStoreItems := filterArtifactsByJobID(storeItems, jobAID)
	assertRuntimeArtifactFilenames(t, jobAStoreItems, []string{"widget.csv", planner.ExportManifestFilename})
	jobBStoreItems := filterArtifactsByJobID(storeItems, jobBID)
	if len(jobBStoreItems) != 0 {
		t.Fatalf("jobB global store artifacts = %+v, want empty", jobBStoreItems)
	}
	if len(storeItems) != len(jobAStoreItems) {
		t.Fatalf("global store should contain only jobA artifacts, got %+v", storeItems)
	}
}

func TestJobRuntime_ConcurrentSuccessAndFailureRemainIsolatedWithinSingleRuntime(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		WorkerCount:     2,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	successPkg := mustAPITestPackage(t, "widget")
	failurePkg := mustAPITestPackage(t, "gear")
	failureJobID := submitSpecificAPITestJob(t, handler, failurePkg)
	successJobID := submitSpecificAPITestJob(t, handler, successPkg)
	factory.SetError(failureJobID, &executor.ExecutionError{
		ProductID:  failurePkg.ProductKey,
		StepID:     "2",
		Err:        errors.New("gear runtime failed"),
		RetryCount: 1,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStarts(t, successJobID, failureJobID)
	_ = waitForJobState(t, handler, successJobID, jobstatus.StateRunning)
	_ = waitForJobState(t, handler, failureJobID, jobstatus.StateRunning)

	factory.Release(successJobID)
	factory.Release(failureJobID)

	succeeded := waitForJobState(t, handler, successJobID, jobstatus.StateSucceeded)
	if succeeded.Error != nil {
		t.Fatalf("success job error = %#v, want nil", succeeded.Error)
	}

	failed := waitForJobState(t, handler, failureJobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("failure job should include error details")
	}
	if failed.Error.Message != "gear runtime failed" {
		t.Fatalf("failure job error message = %q", failed.Error.Message)
	}
	if failed.Error.ProductID != failurePkg.ProductKey {
		t.Fatalf("failure job productId = %q, want %q", failed.Error.ProductID, failurePkg.ProductKey)
	}

	successArtifacts := mustGetJobArtifacts(t, handler, successJobID)
	assertRuntimeArtifactFilenames(t, successArtifacts.Artifacts, []string{"widget.csv", planner.ExportManifestFilename})
	for _, item := range successArtifacts.Artifacts {
		if item.JobID != successJobID {
			t.Fatalf("success artifact jobId = %q, want %q", item.JobID, successJobID)
		}
	}

	failureArtifacts := mustGetJobArtifacts(t, handler, failureJobID)
	if len(failureArtifacts.Artifacts) != 0 {
		t.Fatalf("failure artifacts = %+v, want empty", failureArtifacts.Artifacts)
	}

	_, successReport := mustReadRuntimeReport(t, artifactStore.BaseDir(), successPkg)
	if successReport.Status != report.StatusSuccess {
		t.Fatalf("success report status = %q, want %q", successReport.Status, report.StatusSuccess)
	}
	if successReport.Error != nil {
		t.Fatalf("success report error = %#v, want nil", successReport.Error)
	}
	if len(successReport.Artifacts) != 2 {
		t.Fatalf("success report artifacts = %d, want 2", len(successReport.Artifacts))
	}

	_, failureReport := mustReadRuntimeReport(t, artifactStore.BaseDir(), failurePkg)
	if failureReport.Status != report.StatusFailed {
		t.Fatalf("failure report status = %q, want %q", failureReport.Status, report.StatusFailed)
	}
	if failureReport.Error == nil || failureReport.Error.Message != "gear runtime failed" {
		t.Fatalf("failure report error = %#v, want isolated failure details", failureReport.Error)
	}
	if len(failureReport.Artifacts) != 0 {
		t.Fatalf("failure report artifacts = %+v, want empty", failureReport.Artifacts)
	}
}

func TestJobRuntime_ConcurrentSameShapeJobsKeepArtifactsJobScoped(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		WorkerCount:     2,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobAPkg := mustAPITestPackageWithWidth(t, "widget", 42)
	jobBPkg := mustAPITestPackageWithWidth(t, "widget", 99)
	jobAID := submitSpecificAPITestJob(t, handler, jobAPkg)
	jobBID := submitSpecificAPITestJob(t, handler, jobBPkg)
	if jobAID == jobBID {
		t.Fatalf("same-shape jobs must retain distinct job IDs, got %q", jobAID)
	}

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStarts(t, jobAID, jobBID)
	_ = waitForJobState(t, handler, jobAID, jobstatus.StateRunning)
	_ = waitForJobState(t, handler, jobBID, jobstatus.StateRunning)

	factory.Release(jobAID)
	factory.Release(jobBID)

	_ = waitForJobState(t, handler, jobAID, jobstatus.StateSucceeded)
	_ = waitForJobState(t, handler, jobBID, jobstatus.StateSucceeded)

	jobAArtifacts := mustGetJobArtifacts(t, handler, jobAID)
	jobBArtifacts := mustGetJobArtifacts(t, handler, jobBID)
	assertRuntimeArtifactFilenames(t, jobAArtifacts.Artifacts, []string{"widget.csv", planner.ExportManifestFilename})
	assertRuntimeArtifactFilenames(t, jobBArtifacts.Artifacts, []string{"widget.csv", planner.ExportManifestFilename})

	jobAIDs := make(map[string]struct{}, len(jobAArtifacts.Artifacts))
	for _, item := range jobAArtifacts.Artifacts {
		if item.JobID != jobAID {
			t.Fatalf("jobA artifact jobId = %q, want %q", item.JobID, jobAID)
		}
		jobAIDs[item.ID] = struct{}{}
	}
	for _, item := range jobBArtifacts.Artifacts {
		if item.JobID != jobBID {
			t.Fatalf("jobB artifact jobId = %q, want %q", item.JobID, jobBID)
		}
		if _, ok := jobAIDs[item.ID]; ok {
			t.Fatalf("artifact ID %q leaked across job ownership boundary", item.ID)
		}
	}

	storeItems, err := artifactStore.List()
	if err != nil {
		t.Fatalf("artifact store List returned error: %v", err)
	}
	if len(storeItems) != 4 {
		t.Fatalf("global artifact store count = %d, want 4", len(storeItems))
	}
	assertRuntimeArtifactFilenames(t, filterArtifactsByJobID(storeItems, jobAID), []string{"widget.csv", planner.ExportManifestFilename})
	assertRuntimeArtifactFilenames(t, filterArtifactsByJobID(storeItems, jobBID), []string{"widget.csv", planner.ExportManifestFilename})
}

func TestJobRuntime_ConcurrentFinalizationFailureRemainsLocalWithinSingleRuntime(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	baseStore := artifact.NewFileSystemStore(t.TempDir())
	artifactStore := newControlledCompletionArtifactStore(baseStore)
	factory := newRuntimeTestFactory(t)
	factory.runRoot = artifactStore.BaseDir()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		ArtifactStore:   artifactStore,
		JobQueue:        newInMemoryJobQueue(),
		WorkerCount:     2,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	successPkg := mustAPITestPackage(t, "widget")
	failurePkg := mustAPITestPackage(t, "gear")
	successJobID := submitSpecificAPITestJob(t, handler, successPkg)
	failureJobID := submitSpecificAPITestJob(t, handler, failurePkg)
	artifactStore.BlockJobOnCall(failureJobID, 1)
	artifactStore.FailJobOnCall(failureJobID, 2, errors.New("injected single-runtime registration failure"))

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStarts(t, successJobID, failureJobID)
	_ = waitForJobState(t, handler, successJobID, jobstatus.StateRunning)
	_ = waitForJobState(t, handler, failureJobID, jobstatus.StateRunning)

	factory.Release(failureJobID)
	artifactStore.WaitForJobBlockedCall(t, failureJobID)

	factory.Release(successJobID)
	succeeded := waitForJobState(t, handler, successJobID, jobstatus.StateSucceeded)
	if succeeded.Error != nil {
		t.Fatalf("success job error = %#v, want nil", succeeded.Error)
	}

	artifactStore.ReleaseJobBlockedCall(failureJobID)
	failed := waitForJobState(t, handler, failureJobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("failure job should surface registration error")
	}
	if !strings.Contains(failed.Error.Message, "injected single-runtime registration failure") {
		t.Fatalf("failure job error message = %q", failed.Error.Message)
	}

	successArtifacts := mustGetJobArtifacts(t, handler, successJobID)
	assertRuntimeArtifactFilenames(t, successArtifacts.Artifacts, []string{"widget.csv", planner.ExportManifestFilename})
	failureArtifacts := mustGetJobArtifacts(t, handler, failureJobID)
	if len(failureArtifacts.Artifacts) != 0 {
		t.Fatalf("failure job artifacts = %+v, want empty", failureArtifacts.Artifacts)
	}

	storeItems, err := artifactStore.List()
	if err != nil {
		t.Fatalf("artifact store List returned error: %v", err)
	}
	assertRuntimeArtifactFilenames(t, filterArtifactsByJobID(storeItems, successJobID), []string{"widget.csv", planner.ExportManifestFilename})
	if got := filterArtifactsByJobID(storeItems, failureJobID); len(got) != 0 {
		t.Fatalf("failure job leaked artifacts into global store: %+v", got)
	}
}

func TestJobRuntime_MultipleJobsRemainIsolated(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	jobA := submitAPITestJob(t, handler, "widget")
	jobB := submitAPITestJob(t, handler, "gear")
	jobC := submitAPITestJob(t, handler, "bracket")
	factory.SetError(jobB, &executor.ExecutionError{
		ProductID:  "gear",
		StepID:     "1",
		Err:        errors.New("gear failed"),
		RetryCount: 1,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	for _, jobID := range []string{jobA, jobB, jobC} {
		factory.WaitForStart(t, jobID)
		_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)
		factory.Release(jobID)
	}

	if got := waitForJobState(t, handler, jobA, jobstatus.StateSucceeded); got.Error != nil {
		t.Fatalf("jobA error = %#v, want nil", got.Error)
	}
	if got := waitForJobState(t, handler, jobB, jobstatus.StateFailed); got.Error == nil || got.Error.ProductID != "gear" {
		t.Fatalf("jobB failure = %#v, want mapped gear failure", got.Error)
	}
	if got := waitForJobState(t, handler, jobC, jobstatus.StateSucceeded); got.Error != nil {
		t.Fatalf("jobC error = %#v, want nil", got.Error)
	}
}

func TestJobRuntime_ShutdownStopsWorkerCleanly(t *testing.T) {
	t.Run("idle", func(t *testing.T) {
		handler := newHandler(HandlerOptions{
			SubmissionStore: newInMemorySubmissionStore(),
			JobQueue:        newInMemoryJobQueue(),
			ExecutorFactory: newRuntimeTestFactory(t).Executor,
			PollInterval:    time.Millisecond,
		})

		cancel, wait := startHandlerRuntime(t, handler)
		cancel()
		wait()
	})

	t.Run("in-flight", func(t *testing.T) {
		submissionStore := newInMemorySubmissionStore()
		artifactStore := artifact.NewFileSystemStore(t.TempDir())
		factory := newIdempotencyRuntimeTestFactory(t, artifactStore.BaseDir())
		queue := newInMemoryJobQueue()
		handler := newHandler(HandlerOptions{
			SubmissionStore: submissionStore,
			ArtifactStore:   artifactStore,
			JobQueue:        queue,
			ExecutorFactory: factory.Executor,
			PollInterval:    time.Millisecond,
		})

		jobID := submitAPITestJob(t, handler, "widget")
		cancel, wait := startHandlerRuntime(t, handler)

		factory.WaitForStart(t, jobID)
		_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

		cancel()
		wait()

		status := waitForTerminalJobState(t, handler, jobID)
		if status.State != jobstatus.StateCanceled && status.State != jobstatus.StateFailed {
			t.Fatalf("terminal state after shutdown = %q, want canceled or failed", status.State)
		}
		if got := factory.ExecutionCount(); got != 1 {
			t.Fatalf("execution count after shutdown = %d, want 1", got)
		}
		if queue.Len() != 0 {
			t.Fatalf("queue Len after shutdown = %d, want 0", queue.Len())
		}
	})
}

func TestJobRuntime_IdleLoopBlocksWithoutPolling(t *testing.T) {
	queue := newBlockingObservationQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        queue,
		ExecutorFactory: newRuntimeTestFactory(t).Executor,
		PollInterval:    time.Millisecond,
	})

	cancel, wait := startHandlerRuntime(t, handler)

	queue.WaitForWaiter(t)
	time.Sleep(20 * time.Millisecond)

	if got := queue.WaitCalls(); got != 1 {
		t.Fatalf("WaitDequeue calls while idle = %d, want 1", got)
	}
	if got := queue.DequeueCalls(); got != 0 {
		t.Fatalf("Dequeue calls while idle = %d, want 0", got)
	}

	cancel()
	wait()
}

func TestJobRuntime_ConfiguredWorkersBlockWhenIdleWithoutPolling(t *testing.T) {
	queue := newBlockingObservationQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        queue,
		WorkerCount:     2,
		ExecutorFactory: newRuntimeTestFactory(t).Executor,
		PollInterval:    time.Millisecond,
	})

	cancel, wait := startHandlerRuntime(t, handler)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := queue.WaitCalls(); got == 2 {
			if deq := queue.DequeueCalls(); deq != 0 {
				t.Fatalf("Dequeue calls while idle = %d, want 0", deq)
			}
			cancel()
			wait()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	wait()
	t.Fatalf("WaitDequeue calls while idle = %d, want 2", queue.WaitCalls())
}

func TestJobRuntime_PostShutdownSubmissionRemainsQueuedWhileWorkerStaysStopped(t *testing.T) {
	artifactStore := artifact.NewFileSystemStore(t.TempDir())
	baseQueue := newInMemoryJobQueue()
	blockingQueue, ok := baseQueue.(blockingJobQueue)
	if !ok {
		t.Fatal("in-memory queue does not expose blocking dequeue")
	}

	queue := newObservingJobQueue(blockingQueue)
	factory := newIdempotencyRuntimeTestFactory(t, artifactStore.BaseDir())
	handler := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		ArtifactStore:   artifactStore,
		JobQueue:        queue,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	queue.WaitForWaiter(t)
	cancel()
	wait()

	jobID := submitAPITestJob(t, handler, "widget")

	status := mustGetJobStatus(t, handler, jobID)
	if status.State != jobstatus.StateQueued {
		t.Fatalf("state after shutdown submission = %q, want %q", status.State, jobstatus.StateQueued)
	}

	artifacts := mustGetJobArtifacts(t, handler, jobID)
	if len(artifacts.Artifacts) != 0 {
		t.Fatalf("artifacts after shutdown submission = %+v, want empty", artifacts.Artifacts)
	}

	if got := factory.ExecutionCount(); got != 0 {
		t.Fatalf("execution count after shutdown submission = %d, want 0", got)
	}

	if got := queue.Len(); got != 1 {
		t.Fatalf("queue Len after shutdown submission = %d, want 1", got)
	}
	if !queue.Contains(jobID) {
		t.Fatalf("queue should still contain never-started job %q after shutdown", jobID)
	}
}

func TestJobRuntime_RestartQueuedJobIsNotExecutedAutomatically(t *testing.T) {
	queueA := newInMemoryJobQueue()
	factoryA := newRuntimeTestFactory(t)
	handlerA := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        queueA,
		ExecutorFactory: factoryA.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handlerA, "widget")

	statusA := mustGetJobStatus(t, handlerA, jobID)
	if statusA.State != jobstatus.StateQueued {
		t.Fatalf("server A state = %q, want %q", statusA.State, jobstatus.StateQueued)
	}
	if got := queueA.Len(); got != 1 {
		t.Fatalf("server A queue Len = %d, want 1", got)
	}
	if got := factoryA.ExecutionCount(jobID); got != 0 {
		t.Fatalf("server A execution count = %d, want 0", got)
	}

	queueB := newBlockingObservationQueue()
	factoryB := newRuntimeTestFactory(t)
	handlerB := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        queueB,
		ExecutorFactory: factoryB.Executor,
		PollInterval:    time.Millisecond,
	})

	cancelB, waitB := startHandlerRuntime(t, handlerB)
	queueB.WaitForWaiter(t)
	cancelB()
	waitB()

	if got := factoryA.ExecutionCount(jobID) + factoryB.ExecutionCount(jobID); got != 0 {
		t.Fatalf("execution count across restart = %d, want 0", got)
	}
}

func TestJobRuntime_RestartDoesNotResumeRunningJob(t *testing.T) {
	factoryA := newRuntimeTestFactory(t)
	handlerA := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factoryA.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handlerA, "widget")

	cancelA, waitA := startHandlerRuntime(t, handlerA)
	factoryA.WaitForStart(t, jobID)
	running := waitForJobState(t, handlerA, jobID, jobstatus.StateRunning)
	if running.StartedAt == nil {
		t.Fatal("running job should include startedAt")
	}

	cancelA()
	waitA()

	terminal := waitForTerminalJobState(t, handlerA, jobID)
	if terminal.State != jobstatus.StateCanceled && terminal.State != jobstatus.StateFailed {
		t.Fatalf("server A terminal state = %q, want canceled or failed", terminal.State)
	}

	queueB := newBlockingObservationQueue()
	factoryB := newRuntimeTestFactory(t)
	handlerB := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        queueB,
		ExecutorFactory: factoryB.Executor,
		PollInterval:    time.Millisecond,
	})

	cancelB, waitB := startHandlerRuntime(t, handlerB)
	queueB.WaitForWaiter(t)
	cancelB()
	waitB()

	if got := factoryA.ExecutionCount(jobID); got != 1 {
		t.Fatalf("server A execution count = %d, want 1", got)
	}
	if got := factoryB.ExecutionCount(jobID); got != 0 {
		t.Fatalf("server B execution count = %d, want 0", got)
	}
}

func TestJobRuntime_RestartDoesNotReplayFailedJob(t *testing.T) {
	factoryA := newRuntimeTestFactory(t)
	handlerA := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factoryA.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handlerA, "widget")
	factoryA.SetError(jobID, &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "2",
		Err:        errors.New("forced restart-boundary failure"),
		RetryCount: 1,
	})

	cancelA, waitA := startHandlerRuntime(t, handlerA)
	defer func() {
		cancelA()
		waitA()
	}()

	factoryA.WaitForStart(t, jobID)
	_ = waitForJobState(t, handlerA, jobID, jobstatus.StateRunning)
	factoryA.Release(jobID)

	failed := waitForJobState(t, handlerA, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("failed job should include error details")
	}

	queueB := newBlockingObservationQueue()
	factoryB := newRuntimeTestFactory(t)
	handlerB := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        queueB,
		ExecutorFactory: factoryB.Executor,
		PollInterval:    time.Millisecond,
	})

	cancelB, waitB := startHandlerRuntime(t, handlerB)
	queueB.WaitForWaiter(t)
	cancelB()
	waitB()

	if got := factoryA.ExecutionCount(jobID); got != 1 {
		t.Fatalf("server A execution count = %d, want 1", got)
	}
	if got := factoryB.ExecutionCount(jobID); got != 0 {
		t.Fatalf("server B execution count = %d, want 0", got)
	}
}

func TestJobRuntime_DuplicateSubmissionAfterRestartDoesNotImplicitlyExecute(t *testing.T) {
	pkg := mustAPITestPackage(t, "widget")
	factoryA := newRuntimeTestFactory(t)
	handlerA := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factoryA.Executor,
		PollInterval:    time.Millisecond,
	})

	cancelA, waitA := startHandlerRuntime(t, handlerA)
	defer func() {
		cancelA()
		waitA()
	}()

	first := submitSpecificAPITestJobResponse(t, handlerA, pkg)
	factoryA.WaitForStart(t, first.JobID)
	_ = waitForJobState(t, handlerA, first.JobID, jobstatus.StateRunning)
	factoryA.Release(first.JobID)

	completed := waitForJobState(t, handlerA, first.JobID, jobstatus.StateSucceeded)
	if completed.Error != nil {
		t.Fatalf("server A completed error = %#v, want nil", completed.Error)
	}

	queueB := newInMemoryJobQueue()
	factoryB := newRuntimeTestFactory(t)
	handlerB := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        queueB,
		ExecutorFactory: factoryB.Executor,
		PollInterval:    time.Millisecond,
	})

	second := submitSpecificAPITestJobResponse(t, handlerB, pkg)
	if second.JobID != first.JobID {
		t.Fatalf("server B duplicate jobId = %q, want %q", second.JobID, first.JobID)
	}
	if second.State != jobstatus.StateQueued {
		t.Fatalf("server B duplicate state = %q, want %q", second.State, jobstatus.StateQueued)
	}
	if got := factoryA.ExecutionCount(first.JobID); got != 1 {
		t.Fatalf("server A execution count = %d, want 1", got)
	}
	if got := factoryB.ExecutionCount(first.JobID); got != 0 {
		t.Fatalf("server B execution count = %d, want 0", got)
	}
	if got := queueB.Len(); got != 1 {
		t.Fatalf("server B queue Len = %d, want 1", got)
	}
	if !queueB.Contains(first.JobID) {
		t.Fatalf("server B queue should contain duplicate submission %q", first.JobID)
	}
}

func TestJobRuntime_RestartDoesNotReexecuteInterruptedInFlightJob(t *testing.T) {
	factoryA := newRuntimeTestFactory(t)
	handlerA := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factoryA.Executor,
		PollInterval:    time.Millisecond,
	})

	jobID := submitAPITestJob(t, handlerA, "widget")

	cancelA, waitA := startHandlerRuntime(t, handlerA)
	factoryA.WaitForStart(t, jobID)
	_ = waitForJobState(t, handlerA, jobID, jobstatus.StateRunning)

	cancelA()
	waitA()

	canceled := waitForJobState(t, handlerA, jobID, jobstatus.StateCanceled)
	if canceled.EndedAt == nil {
		t.Fatal("canceled job should include endedAt")
	}

	queueB := newBlockingObservationQueue()
	factoryB := newRuntimeTestFactory(t)
	handlerB := newHandler(HandlerOptions{
		SubmissionStore: newInMemorySubmissionStore(),
		JobQueue:        queueB,
		ExecutorFactory: factoryB.Executor,
		PollInterval:    time.Millisecond,
	})

	cancelB, waitB := startHandlerRuntime(t, handlerB)
	queueB.WaitForWaiter(t)
	cancelB()
	waitB()

	if got := factoryA.ExecutionCount(jobID); got != 1 {
		t.Fatalf("server A execution count = %d, want 1", got)
	}
	if got := factoryB.ExecutionCount(jobID); got != 0 {
		t.Fatalf("server B execution count = %d, want 0", got)
	}
}

func TestJobRuntime_DuplicateSubmissionAfterTerminalFailureDoesNotReexecute(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	queue := newInMemoryJobQueue()
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        queue,
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))
	submit := func() jobSubmissionResponse {
		t.Helper()

		req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("POST /job expected 202, got %d body=%q", rec.Code, rec.Body.String())
		}

		var resp jobSubmissionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode submission response: %v", err)
		}
		return resp
	}

	first := submit()
	jobID := first.JobID
	factory.SetError(jobID, &executor.ExecutionError{
		ProductID:  "widget",
		StepID:     "2",
		Err:        errors.New("runner exploded"),
		RetryCount: 2,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	factory.WaitForStart(t, jobID)
	_ = waitForJobState(t, handler, jobID, jobstatus.StateRunning)

	factory.Release(jobID)
	failed := waitForJobState(t, handler, jobID, jobstatus.StateFailed)
	if failed.Error == nil {
		t.Fatal("failed status should include error details")
	}

	dup := submit()
	if dup.JobID != jobID {
		t.Fatalf("duplicate submission jobId = %q, want %q", dup.JobID, jobID)
	}
	if dup.State != jobstatus.StateFailed {
		t.Fatalf("duplicate submission state = %q, want %q", dup.State, jobstatus.StateFailed)
	}
	if got := factory.ExecutionCount(jobID); got != 1 {
		t.Fatalf("execution count = %d, want 1", got)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue Len after duplicate failed submission = %d, want 0", queue.Len())
	}
	if queue.Contains(jobID) {
		t.Fatalf("queue should not contain terminal failed job %q after duplicate submission", jobID)
	}

	for i := 0; i < 3; i++ {
		again := mustGetJobStatus(t, handler, jobID)
		if !reflect.DeepEqual(again, failed) {
			t.Fatalf("terminal failed status should stay stable after duplicate submission:\nfirst=%#v\nread%d=%#v", failed, i+1, again)
		}
	}
}

func TestJobRuntime_WakesForNewJobsAcrossMultipleEnqueueCycles(t *testing.T) {
	submissionStore := newInMemorySubmissionStore()
	factory := newRuntimeTestFactory(t)
	handler := newHandler(HandlerOptions{
		SubmissionStore: submissionStore,
		JobQueue:        newInMemoryJobQueue(),
		ExecutorFactory: factory.Executor,
		PollInterval:    time.Millisecond,
	})

	cancel, wait := startHandlerRuntime(t, handler)
	defer func() {
		cancel()
		wait()
	}()

	jobA := submitAPITestJob(t, handler, "widget")
	factory.WaitForStart(t, jobA)
	_ = waitForJobState(t, handler, jobA, jobstatus.StateRunning)
	factory.Release(jobA)
	if got := waitForJobState(t, handler, jobA, jobstatus.StateSucceeded); got.Error != nil {
		t.Fatalf("jobA error = %#v, want nil", got.Error)
	}

	jobB := submitAPITestJob(t, handler, "gear")
	factory.WaitForStart(t, jobB)
	_ = waitForJobState(t, handler, jobB, jobstatus.StateRunning)
	factory.Release(jobB)
	if got := waitForJobState(t, handler, jobB, jobstatus.StateSucceeded); got.Error != nil {
		t.Fatalf("jobB error = %#v, want nil", got.Error)
	}
}

func TestNewHandlerDefaultConstructionExposesJobSubmission(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	newHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestNewHandlerDefaultConstructionCreatesWorkingQueue(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, "widget")))))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	newHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestNewHandlerArtifactListRouteWorksWithInjectedStore(t *testing.T) {
	store, _ := newAPITestStore(t)
	handler := newHandler(HandlerOptions{ArtifactStore: store})

	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"schemaVersion": "1.0"`) {
		t.Fatalf("unexpected list body: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"class": "execution_output"`) {
		t.Fatalf("artifact list body must include class: %q", rec.Body.String())
	}
}

func TestNewHandlerArtifactFileRouteWorksWithInjectedStore(t *testing.T) {
	store, item := newAPITestStore(t)
	handler := newHandler(HandlerOptions{ArtifactStore: store})

	req := httptest.NewRequest(http.MethodGet, "/artifacts/"+item.ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "api,test\n" {
		t.Fatalf("unexpected file body: %q", rec.Body.String())
	}
}

func TestNewHandlerWithoutArtifactStoreLeavesArtifactRoutesUnavailable(t *testing.T) {
	handler := newHandler()

	reqs := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/artifacts", nil),
		httptest.NewRequest(http.MethodGet, "/artifacts/"+strings.Repeat("a", 64), nil),
	}

	for _, req := range reqs {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", req.URL.Path, rec.Code)
		}
	}
}

type fakeListener struct {
	addr   net.Addr
	closed chan struct{}
	once   sync.Once
}

func newFakeListener(addr string) *fakeListener {
	return &fakeListener{
		addr:   fakeAddr(addr),
		closed: make(chan struct{}),
	}
}

func (l *fakeListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *fakeListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *fakeListener) Addr() net.Addr {
	return l.addr
}

func (l *fakeListener) isClosed() bool {
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

type fakeAddr string

func (a fakeAddr) Network() string {
	return "tcp"
}

func (a fakeAddr) String() string {
	return string(a)
}

func newAPITestStore(t *testing.T) (*artifact.FileSystemStore, artifact.Artifact) {
	t.Helper()

	dir := t.TempDir()
	store := artifact.NewFileSystemStore(dir)

	src := filepath.Join(dir, "artifact.csv")
	if err := os.WriteFile(src, []byte("api,test\n"), 0644); err != nil {
		t.Fatalf("failed to write test artifact: %v", err)
	}

	item, err := store.Put(context.Background(), src, artifact.Artifact{
		Type:      artifact.ArtifactTypeCSV,
		ProductID: "api",
		StepID:    "test",
		Filename:  "artifact.csv",
	})
	if err != nil {
		t.Fatalf("failed to register test artifact: %v", err)
	}

	return store, item
}

func submitAPITestJob(t *testing.T, handler http.Handler, productKey string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(mustAPITestPackage(t, productKey)))))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit %s job expected 202, got %d body=%q", productKey, rec.Code, rec.Body.String())
	}

	var resp jobSubmissionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s submission response: %v", productKey, err)
	}

	return resp.JobID
}

func submitSpecificAPITestJob(t *testing.T, handler http.Handler, pkg *handoff.Package) string {
	t.Helper()

	resp := submitSpecificAPITestJobResponse(t, handler, pkg)
	return resp.JobID
}

func submitSpecificAPITestJobResponse(t *testing.T, handler http.Handler, pkg *handoff.Package) jobSubmissionResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(pkg))))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit %s job expected 202, got %d body=%q", pkg.ProductKey, rec.Code, rec.Body.String())
	}

	var resp jobSubmissionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s submission response: %v", pkg.ProductKey, err)
	}

	return resp
}

func registerAPITestArtifact(t *testing.T, store *artifact.FileSystemStore, jobID, productID, stepID, filename, contents string) artifact.Artifact {
	t.Helper()

	src := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(src, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write test artifact %s: %v", filename, err)
	}

	item, err := store.Put(context.Background(), src, artifact.Artifact{
		Type:      artifact.ArtifactTypeCSV,
		JobID:     jobID,
		ProductID: productID,
		StepID:    stepID,
		Filename:  filename,
	})
	if err != nil {
		t.Fatalf("failed to register test artifact %s: %v", filename, err)
	}

	return item
}

func mustMarkJobStatusSucceeded(t *testing.T, store SubmissionStore, jobID string) {
	t.Helper()

	if err := store.UpdateStatus(jobID, func(status *jobstatus.Status) error {
		if status == nil {
			t.Fatalf("missing job status for %s", jobID)
		}
		if status.State == jobstatus.StateQueued {
			if err := status.Start(status.UpdatedAt.Add(time.Second)); err != nil {
				return err
			}
		}
		return status.Succeed(status.UpdatedAt.Add(time.Second))
	}); err != nil {
		t.Fatalf("UpdateStatus(%s) returned error: %v", jobID, err)
	}
}

func startHandlerRuntime(t *testing.T, handler http.Handler) (context.CancelFunc, func()) {
	t.Helper()

	runtimeHandler, ok := handler.(handlerRuntime)
	if !ok {
		t.Fatal("handler does not expose runtime lifecycle")
	}

	ctx, cancel := context.WithCancel(context.Background())
	runtimeHandler.Start(ctx)
	return cancel, runtimeHandler.Wait
}

func setHandlerRuntimeClockSequence(t *testing.T, handler http.Handler, times ...time.Time) {
	t.Helper()

	runtimeHandler, ok := handler.(*runtimeAwareHandler)
	if !ok || runtimeHandler.runner == nil {
		t.Fatal("handler does not expose runtime runner")
	}
	if len(times) == 0 {
		t.Fatal("times must not be empty")
	}

	index := 0
	runtimeHandler.runner.now = func() time.Time {
		if index >= len(times) {
			return times[len(times)-1].UTC()
		}
		current := times[index].UTC()
		index++
		return current
	}
}

func setHandlerRuntimeRunRoot(t *testing.T, handler http.Handler, runRoot string) {
	t.Helper()

	runtimeHandler, ok := handler.(*runtimeAwareHandler)
	if !ok || runtimeHandler.runner == nil {
		t.Fatal("handler does not expose runtime runner")
	}
	runtimeHandler.runner.runRoot = runRoot
}

func mustGetJobStatus(t *testing.T, handler http.Handler, jobID string) jobstatus.Status {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/job/"+jobID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /job/%s expected 200, got %d body=%q", jobID, rec.Code, rec.Body.String())
	}

	var status jobstatus.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	return status
}

func waitForJobState(t *testing.T, handler http.Handler, jobID string, want jobstatus.State) jobstatus.Status {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := mustGetJobStatus(t, handler, jobID)
		if status.State == want {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}

	status := mustGetJobStatus(t, handler, jobID)
	t.Fatalf("timed out waiting for job %s state %q; last state %q", jobID, want, status.State)
	return jobstatus.Status{}
}

func waitForTerminalJobState(t *testing.T, handler http.Handler, jobID string) jobstatus.Status {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := mustGetJobStatus(t, handler, jobID)
		if status.State.IsTerminal() {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}

	status := mustGetJobStatus(t, handler, jobID)
	t.Fatalf("timed out waiting for job %s to reach terminal state; last state %q", jobID, status.State)
	return jobstatus.Status{}
}

func mustGetJobArtifacts(t *testing.T, handler http.Handler, jobID string) jobArtifactsResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/job/"+jobID+"/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /job/%s/artifacts expected 200, got %d body=%q", jobID, rec.Code, rec.Body.String())
	}

	var response jobArtifactsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode artifacts response: %v", err)
	}
	return response
}

func mustReadRuntimeReport(t *testing.T, runRoot string, pkg *handoff.Package) ([]byte, report.Report) {
	t.Helper()

	path := filepath.Join(runtimeProductDir(runRoot, pkg), "report.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected report.json to exist at %s: %v", path, err)
	}

	var runReport report.Report
	if err := json.Unmarshal(data, &runReport); err != nil {
		t.Fatalf("decode runtime report %s: %v\ncontent:\n%s", path, err, data)
	}

	return data, runReport
}

func assertRuntimeArtifactFilenames(t *testing.T, artifacts []artifact.Artifact, want []string) {
	t.Helper()

	if len(artifacts) != len(want) {
		t.Fatalf("artifact count = %d, want %d (%v)", len(artifacts), len(want), want)
	}
	for i, filename := range want {
		if artifacts[i].Filename != filename {
			t.Fatalf("artifact[%d].Filename = %q, want %q", i, artifacts[i].Filename, filename)
		}
	}
}

func filterArtifactsByJobID(items []artifact.Artifact, jobID string) []artifact.Artifact {
	filtered := make([]artifact.Artifact, 0, len(items))
	for _, item := range items {
		if item.JobID == jobID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

type blockingObservationQueue struct {
	waitCalls    atomic.Int32
	dequeueCalls atomic.Int32
	waiting      chan struct{}
}

func newBlockingObservationQueue() *blockingObservationQueue {
	return &blockingObservationQueue{
		waiting: make(chan struct{}, 1),
	}
}

func (q *blockingObservationQueue) Enqueue(pkg *handoff.Package) error {
	return nil
}

func (q *blockingObservationQueue) Dequeue() (*handoff.Package, bool) {
	q.dequeueCalls.Add(1)
	return nil, false
}

func (q *blockingObservationQueue) Peek() (*handoff.Package, bool) {
	return nil, false
}

func (q *blockingObservationQueue) Len() int {
	return 0
}

func (q *blockingObservationQueue) Contains(jobID string) bool {
	return false
}

func (q *blockingObservationQueue) WaitDequeue(ctx context.Context) (*handoff.Package, bool) {
	q.waitCalls.Add(1)
	select {
	case q.waiting <- struct{}{}:
	default:
	}

	<-ctx.Done()
	return nil, false
}

func (q *blockingObservationQueue) WaitForWaiter(t *testing.T) {
	t.Helper()

	select {
	case <-q.waiting:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for idle worker to block on queue")
	}
}

func (q *blockingObservationQueue) WaitCalls() int32 {
	return q.waitCalls.Load()
}

func (q *blockingObservationQueue) DequeueCalls() int32 {
	return q.dequeueCalls.Load()
}

type observingJobQueue struct {
	base    blockingJobQueue
	waiting chan struct{}
}

func newObservingJobQueue(base blockingJobQueue) *observingJobQueue {
	return &observingJobQueue{
		base:    base,
		waiting: make(chan struct{}, 1),
	}
}

func (q *observingJobQueue) Enqueue(pkg *handoff.Package) error {
	return q.base.Enqueue(pkg)
}

func (q *observingJobQueue) Dequeue() (*handoff.Package, bool) {
	return q.base.Dequeue()
}

func (q *observingJobQueue) Peek() (*handoff.Package, bool) {
	return q.base.Peek()
}

func (q *observingJobQueue) Len() int {
	return q.base.Len()
}

func (q *observingJobQueue) Contains(jobID string) bool {
	return q.base.Contains(jobID)
}

func (q *observingJobQueue) WaitDequeue(ctx context.Context) (*handoff.Package, bool) {
	select {
	case q.waiting <- struct{}{}:
	default:
	}
	return q.base.WaitDequeue(ctx)
}

func (q *observingJobQueue) WaitForWaiter(t *testing.T) {
	t.Helper()

	select {
	case <-q.waiting:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime to become idle")
	}
}

type runtimeTestFactory struct {
	t         *testing.T
	mu        sync.Mutex
	runRoot   string
	behaviors map[string]*runtimeTestBehavior
	started   chan string
	counts    map[string]int
}

type runtimeTestBehavior struct {
	release             chan struct{}
	err                 error
	omitDeclaredOutputs bool
	acceptance          artifact.AcceptanceContext
}

func newRuntimeTestFactory(t *testing.T) *runtimeTestFactory {
	t.Helper()

	return &runtimeTestFactory{
		t:         t,
		behaviors: make(map[string]*runtimeTestBehavior),
		started:   make(chan string, 32),
		counts:    make(map[string]int),
	}
}

func (f *runtimeTestFactory) Executor(pkg *handoff.Package) packageExecutor {
	f.mu.Lock()
	defer f.mu.Unlock()

	behavior, ok := f.behaviors[pkg.JobID]
	if !ok {
		behavior = &runtimeTestBehavior{release: make(chan struct{})}
		f.behaviors[pkg.JobID] = behavior
	}

	return &runtimeTestExecutor{
		t:        f.t,
		factory:  f,
		runRoot:  f.runRoot,
		pkg:      clonePackage(pkg),
		behavior: behavior,
		started:  f.started,
	}
}

func (f *runtimeTestFactory) SetError(jobID string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	behavior, ok := f.behaviors[jobID]
	if !ok {
		behavior = &runtimeTestBehavior{release: make(chan struct{})}
		f.behaviors[jobID] = behavior
	}
	behavior.err = err
}

func (f *runtimeTestFactory) SetOmitDeclaredOutputs(jobID string, omit bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	behavior, ok := f.behaviors[jobID]
	if !ok {
		behavior = &runtimeTestBehavior{release: make(chan struct{})}
		f.behaviors[jobID] = behavior
	}
	behavior.omitDeclaredOutputs = omit
}

func (f *runtimeTestFactory) Release(jobID string) {
	f.mu.Lock()
	behavior, ok := f.behaviors[jobID]
	f.mu.Unlock()
	if !ok {
		f.t.Fatalf("missing behavior for job %s", jobID)
	}

	select {
	case <-behavior.release:
	default:
		close(behavior.release)
	}
}

func (f *runtimeTestFactory) SetAcceptanceContext(jobID string, acceptance artifact.AcceptanceContext) {
	f.mu.Lock()
	defer f.mu.Unlock()

	behavior, ok := f.behaviors[jobID]
	if !ok {
		behavior = &runtimeTestBehavior{release: make(chan struct{})}
		f.behaviors[jobID] = behavior
	}
	behavior.acceptance = acceptance
}

func (f *runtimeTestFactory) WaitForStart(t *testing.T, wantJobID string) {
	t.Helper()

	select {
	case got := <-f.started:
		if got != wantJobID {
			t.Fatalf("started job = %q, want %q", got, wantJobID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for job %s to start", wantJobID)
	}
}

func (f *runtimeTestFactory) WaitForStarts(t *testing.T, wantJobIDs ...string) {
	t.Helper()

	remaining := make(map[string]struct{}, len(wantJobIDs))
	for _, jobID := range wantJobIDs {
		remaining[jobID] = struct{}{}
	}

	deadline := time.After(2 * time.Second)
	for len(remaining) > 0 {
		select {
		case got := <-f.started:
			if _, ok := remaining[got]; !ok {
				t.Fatalf("started unexpected job %q while waiting for %v", got, wantJobIDs)
			}
			delete(remaining, got)
		case <-deadline:
			missing := make([]string, 0, len(remaining))
			for jobID := range remaining {
				missing = append(missing, jobID)
			}
			sort.Strings(missing)
			t.Fatalf("timed out waiting for jobs to start: remaining=%v", missing)
		}
	}
}

func (f *runtimeTestFactory) RecordExecution(jobID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[jobID]++
}

func (f *runtimeTestFactory) ExecutionCount(jobID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[jobID]
}

type runtimeTestExecutor struct {
	t        *testing.T
	factory  *runtimeTestFactory
	runRoot  string
	pkg      *handoff.Package
	behavior *runtimeTestBehavior
	started  chan<- string
	states   []executor.ExecutionState
}

func (e *runtimeTestExecutor) ExecutePackage(ctx context.Context, pkg *handoff.Package) error {
	if e.started != nil {
		e.started <- e.pkg.JobID
	}
	if e.factory != nil && e.pkg != nil {
		e.factory.RecordExecution(e.pkg.JobID)
	}

	select {
	case <-e.behavior.release:
	case <-ctx.Done():
		return ctx.Err()
	}

	if e.behavior.err != nil {
		e.states = runtimeTestStates(e.pkg, e.behavior.err)
		return e.behavior.err
	}
	if e.runRoot == "" {
		e.states = runtimeTestStates(e.pkg, nil)
		return nil
	}

	if err := writeRuntimeTestOutputs(e.runRoot, e.pkg, e.behavior.omitDeclaredOutputs); err != nil {
		e.t.Fatalf("failed to write runtime outputs: %v", err)
	}
	e.states = runtimeTestStates(e.pkg, nil)
	return nil
}

func (e *runtimeTestExecutor) States() []executor.ExecutionState {
	return cloneExecutionStates(e.states)
}

func (e *runtimeTestExecutor) ArtifactAcceptanceContext() artifact.AcceptanceContext {
	if e == nil || e.behavior == nil {
		return artifact.AcceptanceContext{}
	}
	return e.behavior.acceptance
}

type idempotencyRuntimeTestFactory struct {
	t              *testing.T
	runRoot        string
	release        chan struct{}
	started        chan string
	executionCount atomic.Int32
}

func newIdempotencyRuntimeTestFactory(t *testing.T, runRoot string) *idempotencyRuntimeTestFactory {
	t.Helper()

	return &idempotencyRuntimeTestFactory{
		t:       t,
		runRoot: runRoot,
		release: make(chan struct{}),
		started: make(chan string, 8),
	}
}

func (f *idempotencyRuntimeTestFactory) Executor(pkg *handoff.Package) packageExecutor {
	return &idempotencyRuntimeTestExecutor{
		factory: f,
		pkg:     clonePackage(pkg),
	}
}

func (f *idempotencyRuntimeTestFactory) WaitForStart(t *testing.T, wantJobID string) {
	t.Helper()

	select {
	case got := <-f.started:
		if got != wantJobID {
			t.Fatalf("started job = %q, want %q", got, wantJobID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for job %s to start", wantJobID)
	}
}

func (f *idempotencyRuntimeTestFactory) Release() {
	select {
	case <-f.release:
	default:
		close(f.release)
	}
}

func (f *idempotencyRuntimeTestFactory) ExecutionCount() int32 {
	return f.executionCount.Load()
}

type idempotencyRuntimeTestExecutor struct {
	factory *idempotencyRuntimeTestFactory
	pkg     *handoff.Package
	states  []executor.ExecutionState
}

func (e *idempotencyRuntimeTestExecutor) ExecutePackage(ctx context.Context, pkg *handoff.Package) error {
	attempt := e.factory.executionCount.Add(1)
	e.factory.started <- e.pkg.JobID

	if attempt > 1 {
		e.states = runtimeTestStates(e.pkg, errors.New("duplicate execution"))
		return errors.New("duplicate execution")
	}

	select {
	case <-e.factory.release:
	case <-ctx.Done():
		return ctx.Err()
	}

	if e.factory.runRoot == "" {
		e.states = runtimeTestStates(e.pkg, nil)
		return nil
	}
	if err := writeRuntimeTestOutputs(e.factory.runRoot, e.pkg, false); err != nil {
		e.factory.t.Fatalf("failed to write runtime outputs: %v", err)
	}
	e.states = runtimeTestStates(e.pkg, nil)
	return nil
}

func (e *idempotencyRuntimeTestExecutor) States() []executor.ExecutionState {
	return cloneExecutionStates(e.states)
}

func runtimeTestStates(pkg *handoff.Package, err error) []executor.ExecutionState {
	if pkg == nil {
		return nil
	}

	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	states := make([]executor.ExecutionState, len(pkg.Steps))

	failIndex := -1
	var executionErr *executor.ExecutionError
	if errors.As(err, &executionErr) {
		for i := range pkg.Steps {
			if executionErr.StepID == fmt.Sprintf("%d", i) {
				failIndex = i
				break
			}
		}
	}
	if err != nil && failIndex == -1 && len(pkg.Steps) > 0 {
		failIndex = len(pkg.Steps) - 1
	}

	for i := range pkg.Steps {
		states[i] = executor.ExecutionState{
			ProductID: pkg.ProductKey,
			StepID:    fmt.Sprintf("%d", i),
			Status:    executor.StepPending,
		}

		if failIndex >= 0 && i > failIndex {
			continue
		}

		startedAt := base.Add(time.Duration(i) * time.Second)
		endedAt := startedAt.Add(time.Millisecond)
		states[i].StartedAt = startedAt
		states[i].EndedAt = endedAt
		states[i].Attempts = 1

		if i == failIndex {
			states[i].Status = executor.StepFailed
			if executionErr != nil && executionErr.RetryCount > 0 {
				states[i].Attempts = executionErr.RetryCount
			}
			continue
		}

		states[i].Status = executor.StepSuccess
	}

	return states
}

func cloneExecutionStates(states []executor.ExecutionState) []executor.ExecutionState {
	if len(states) == 0 {
		return nil
	}
	cloned := make([]executor.ExecutionState, len(states))
	for i, state := range states {
		cloned[i] = state.Clone()
	}
	return cloned
}

func writeRuntimeTestOutputs(runRoot string, pkg *handoff.Package, omitDeclaredOutputs bool) error {
	productDir := runtimeProductDir(runRoot, pkg)
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		return err
	}

	if omitDeclaredOutputs {
		return nil
	}

	if err := os.WriteFile(filepath.Join(productDir, pkg.CSV.Filename), []byte("value\n42\n"), 0o644); err != nil {
		return err
	}

	manifestBytes, err := json.Marshal(pkg.Manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(productDir, pkg.Manifest.ManifestFilename), append(manifestBytes, '\n'), 0o644); err != nil {
		return err
	}

	for _, output := range pkg.Manifest.Outputs {
		target := filepath.Join(productDir, output.Filename)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, runtimeTestOutputContents(output.Type), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func runtimeTestOutputContents(outputType string) []byte {
	switch artifact.NormalizeExportOutputType(outputType) {
	case artifact.ExportOutputTypeCSV:
		return []byte("part,qty\nwidget,1\n")
	case artifact.ExportOutputTypePDF:
		return []byte("%PDF-1.4\n% runtime test\n")
	default:
		return []byte("ISO-10303-21; runtime test")
	}
}

type controlledCompletionArtifactStore struct {
	*artifact.FileSystemStore

	mu            sync.Mutex
	recordCalls   int
	failOnCall    int
	failErr       error
	blockOnCall   int
	blockStarted  chan struct{}
	blockRelease  chan struct{}
	blockSignaled bool
	jobControls   map[string]*controlledJobCompletionBehavior
}

type controlledJobCompletionBehavior struct {
	calls         int
	failOnCall    int
	failErr       error
	blockOnCall   int
	blockStarted  chan struct{}
	blockRelease  chan struct{}
	blockSignaled bool
}

func newControlledCompletionArtifactStore(base *artifact.FileSystemStore) *controlledCompletionArtifactStore {
	return &controlledCompletionArtifactStore{
		FileSystemStore: base,
		jobControls:     make(map[string]*controlledJobCompletionBehavior),
	}
}

func (s *controlledCompletionArtifactStore) FailOnCall(call int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failOnCall = call
	s.failErr = err
}

func (s *controlledCompletionArtifactStore) BlockOnCall(call int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.blockOnCall = call
	s.blockStarted = make(chan struct{}, 1)
	s.blockRelease = make(chan struct{})
	s.blockSignaled = false
}

func (s *controlledCompletionArtifactStore) FailJobOnCall(jobID string, call int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	control := s.jobControlLocked(jobID)
	control.failOnCall = call
	control.failErr = err
}

func (s *controlledCompletionArtifactStore) BlockJobOnCall(jobID string, call int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	control := s.jobControlLocked(jobID)
	control.blockOnCall = call
	control.blockStarted = make(chan struct{}, 1)
	control.blockRelease = make(chan struct{})
	control.blockSignaled = false
}

func (s *controlledCompletionArtifactStore) WaitForBlockedCall(t *testing.T) {
	t.Helper()

	s.mu.Lock()
	blockStarted := s.blockStarted
	s.mu.Unlock()
	if blockStarted == nil {
		t.Fatal("blocked call was not configured")
	}

	select {
	case <-blockStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for completion registration to block")
	}
}

func (s *controlledCompletionArtifactStore) WaitForJobBlockedCall(t *testing.T, jobID string) {
	t.Helper()

	s.mu.Lock()
	control := s.jobControls[jobID]
	var blockStarted chan struct{}
	if control != nil {
		blockStarted = control.blockStarted
	}
	s.mu.Unlock()
	if blockStarted == nil {
		t.Fatalf("blocked call was not configured for job %s", jobID)
	}

	select {
	case <-blockStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for completion registration to block for job %s", jobID)
	}
}

func (s *controlledCompletionArtifactStore) ReleaseBlockedCall() {
	s.mu.Lock()
	blockRelease := s.blockRelease
	s.mu.Unlock()
	if blockRelease == nil {
		return
	}

	select {
	case <-blockRelease:
	default:
		close(blockRelease)
	}
}

func (s *controlledCompletionArtifactStore) ReleaseJobBlockedCall(jobID string) {
	s.mu.Lock()
	control := s.jobControls[jobID]
	var blockRelease chan struct{}
	if control != nil {
		blockRelease = control.blockRelease
	}
	s.mu.Unlock()
	if blockRelease == nil {
		return
	}

	select {
	case <-blockRelease:
	default:
		close(blockRelease)
	}
}

func (s *controlledCompletionArtifactStore) RecordCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordCalls
}

func (s *controlledCompletionArtifactStore) RecordExisting(ctx context.Context, absPath string, meta artifact.Artifact) (artifact.Artifact, error) {
	if err := s.beforeRecordCall(ctx, meta.JobID); err != nil {
		return artifact.Artifact{}, err
	}
	return s.FileSystemStore.RecordExisting(ctx, absPath, meta)
}

func (s *controlledCompletionArtifactStore) RecordExistingBatch(ctx context.Context, records []artifact.ExistingRecord) ([]artifact.Artifact, error) {
	for index := range records {
		if err := s.beforeRecordCall(ctx, records[index].Meta.JobID); err != nil {
			return nil, &artifact.RecordExistingBatchError{Index: index, Err: err}
		}
	}
	return s.FileSystemStore.RecordExistingBatch(ctx, records)
}

func (s *controlledCompletionArtifactStore) beforeRecordCall(ctx context.Context, jobID string) error {
	s.mu.Lock()
	s.recordCalls++
	call := s.recordCalls
	blockOnCall := s.blockOnCall
	blockStarted := s.blockStarted
	blockRelease := s.blockRelease
	shouldSignal := !s.blockSignaled && blockOnCall > 0 && call == blockOnCall
	if shouldSignal {
		s.blockSignaled = true
	}
	failOnCall := s.failOnCall
	failErr := s.failErr

	jobCall := 0
	jobBlockOnCall := 0
	var jobBlockStarted chan struct{}
	var jobBlockRelease chan struct{}
	jobShouldSignal := false
	jobFailOnCall := 0
	var jobFailErr error
	if jobID != "" {
		if control, ok := s.jobControls[jobID]; ok {
			control.calls++
			jobCall = control.calls
			jobBlockOnCall = control.blockOnCall
			jobBlockStarted = control.blockStarted
			jobBlockRelease = control.blockRelease
			jobShouldSignal = !control.blockSignaled && jobBlockOnCall > 0 && jobCall == jobBlockOnCall
			if jobShouldSignal {
				control.blockSignaled = true
			}
			jobFailOnCall = control.failOnCall
			jobFailErr = control.failErr
		}
	}
	s.mu.Unlock()

	if shouldSignal && blockStarted != nil {
		blockStarted <- struct{}{}
	}
	if jobShouldSignal && jobBlockStarted != nil {
		jobBlockStarted <- struct{}{}
	}
	if blockOnCall > 0 && call == blockOnCall && blockRelease != nil {
		select {
		case <-blockRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if jobBlockOnCall > 0 && jobCall == jobBlockOnCall && jobBlockRelease != nil {
		select {
		case <-jobBlockRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if failOnCall > 0 && call == failOnCall {
		return failErr
	}
	if jobFailOnCall > 0 && jobCall == jobFailOnCall {
		return jobFailErr
	}
	return nil
}

func (s *controlledCompletionArtifactStore) jobControlLocked(jobID string) *controlledJobCompletionBehavior {
	control, ok := s.jobControls[jobID]
	if !ok {
		control = &controlledJobCompletionBehavior{}
		s.jobControls[jobID] = control
	}
	return control
}

func TestServerRun_ListenError(t *testing.T) {
	expected := errors.New("listen failed")
	server := Server{
		Addr: "127.0.0.1:0",
		listen: func(network, addr string) (net.Listener, error) {
			return nil, expected
		},
	}

	err := server.Run(context.Background())
	if !errors.Is(err, expected) {
		t.Fatalf("expected listen error %v, got %v", expected, err)
	}
}

func TestServerRun_CancelledContextReturnsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	server := Server{
		Addr:            "127.0.0.1:0",
		ShutdownTimeout: 50 * time.Millisecond,
		listen: func(network, addr string) (net.Listener, error) {
			return newFakeListener("127.0.0.1:0"), nil
		},
	}

	if err := server.Run(ctx); err != nil {
		t.Fatalf("expected cancelled start to return nil, got %v", err)
	}
}

func TestServerRun_UsesDefaultAddrWhenUnset(t *testing.T) {
	var gotNetwork string
	var gotAddr string

	server := Server{
		listen: func(network, addr string) (net.Listener, error) {
			gotNetwork = network
			gotAddr = addr
			return newFakeListener(DefaultAddr), nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := server.Run(ctx); err != nil {
		t.Fatalf("expected default-address run to return nil, got %v", err)
	}

	if gotNetwork != "tcp" {
		t.Fatalf("expected tcp network, got %q", gotNetwork)
	}

	if gotAddr != DefaultAddr {
		t.Fatalf("expected default addr %q, got %q", DefaultAddr, gotAddr)
	}
}

type failingSubmissionStore struct {
	err error
}

func (s failingSubmissionStore) Register(pkg *handoff.Package, status *jobstatus.Status) (*jobstatus.Status, bool, error) {
	return nil, false, s.err
}

func (s failingSubmissionStore) GetStatus(jobID string) (*jobstatus.Status, bool, error) {
	return nil, false, nil
}

func (s failingSubmissionStore) UpdateStatus(jobID string, update func(*jobstatus.Status) error) error {
	return s.err
}

type failingJobQueue struct {
	err error
}

func (q failingJobQueue) Enqueue(pkg *handoff.Package) error {
	return q.err
}

func (q failingJobQueue) Dequeue() (*handoff.Package, bool) {
	return nil, false
}

func (q failingJobQueue) Peek() (*handoff.Package, bool) {
	return nil, false
}

func (q failingJobQueue) Len() int {
	return 0
}

func (q failingJobQueue) Contains(jobID string) bool {
	return false
}

func mustAPITestPackage(t *testing.T, productKey string) *handoff.Package {
	t.Helper()

	return mustAPITestPackageWithWidth(t, productKey, 42)
}

func mustRebuildAPITestPackage(t *testing.T, pkg *handoff.Package) *handoff.Package {
	t.Helper()

	if pkg == nil {
		t.Fatal("package is nil")
	}

	rebuiltJob, err := job.New(pkg.ProductKey, pkg.Plan().Steps)
	if err != nil {
		t.Fatalf("job.New returned error: %v", err)
	}
	rebuiltPkg, err := handoff.FromJob(rebuiltJob)
	if err != nil {
		t.Fatalf("handoff.FromJob returned error: %v", err)
	}
	return rebuiltPkg
}

func mustAPITestPackageWithWidth(t *testing.T, productKey string, width int) *handoff.Package {
	t.Helper()

	j, err := job.New(productKey, []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: productKey,
				Filename:   productKey + ".csv",
				Headers:    []string{"width"},
				Values:     []interface{}{width},
			},
		},
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       productKey,
				ManifestFilename: planner.ExportManifestFilename,
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				PlanHash:         fmt.Sprintf("plan-%s-%d", productKey, width),
				Adapter:          "freecad",
				Product:          planner.ExportManifestProduct{ID: productKey},
				Values: map[string]interface{}{
					"width": width,
				},
				AssemblyMutations: &planner.ExportManifestMutationCollection{
					Parameters: []planner.ExportManifestParameterMutation{
						{Object: "Assembly", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"},
					},
				},
				Outputs: []planner.ExportManifestOutput{
					{Type: "step", Filename: productKey + ".step", Object: "Body"},
				},
			},
		},
		{
			Type: planner.StepRunCADRuntime,
			Payload: planner.RunCADRuntimePayload{
				ProductKey:       productKey,
				Adapter:          "freecad",
				ManifestFilename: planner.ExportManifestFilename,
				ResultFilename:   planner.FreeCADRuntimeResultFilename,
			},
		},
	})
	if err != nil {
		t.Fatalf("job.New returned error: %v", err)
	}

	pkg, err := handoff.FromJob(j)
	if err != nil {
		t.Fatalf("handoff.FromJob returned error: %v", err)
	}
	return pkg
}

func packageEnvelopeFromPackage(pkg *handoff.Package) jobSubmissionRequest {
	if pkg == nil {
		return jobSubmissionRequest{}
	}

	req := jobSubmissionRequest{
		Handoff: &handoffPackagePayload{
			JobID:      pkg.JobID,
			ProductKey: pkg.ProductKey,
			CSV:        pkg.CSV,
			Manifest: manifestPayload{
				ProductKey:           pkg.Manifest.ProductKey,
				ManifestFilename:     pkg.Manifest.ManifestFilename,
				SchemaVersion:        pkg.Manifest.SchemaVersion,
				PlanHash:             pkg.Manifest.PlanHash,
				Adapter:              pkg.Manifest.Adapter,
				Product:              pkg.Manifest.Product,
				Inputs:               pkg.Manifest.Inputs,
				Values:               pkg.Manifest.Values,
				ParameterAssignments: pkg.Manifest.ParameterAssignments,
				AssemblyMutations:    pkg.Manifest.AssemblyMutations,
				PartMutations:        pkg.Manifest.PartMutations,
				Outputs:              pkg.Manifest.Outputs,
			},
			CADRuntime: pkg.CADRuntime,
			Tables:     handoff.CloneTableInputs(pkg.Tables),
			Steps:      make([]stepSnapshotPayload, 0, len(pkg.Steps)),
		},
	}

	for _, step := range pkg.Steps {
		snapshot := stepSnapshotPayload{Type: step.Type}
		if step.CSV != nil {
			csv := *step.CSV
			snapshot.CSV = &csv
		}
		if step.Manifest != nil {
			manifest := manifestPayload{
				ProductKey:           step.Manifest.ProductKey,
				ManifestFilename:     step.Manifest.ManifestFilename,
				SchemaVersion:        step.Manifest.SchemaVersion,
				PlanHash:             step.Manifest.PlanHash,
				Adapter:              step.Manifest.Adapter,
				Product:              step.Manifest.Product,
				Inputs:               step.Manifest.Inputs,
				Values:               step.Manifest.Values,
				ParameterAssignments: step.Manifest.ParameterAssignments,
				AssemblyMutations:    step.Manifest.AssemblyMutations,
				PartMutations:        step.Manifest.PartMutations,
				Outputs:              step.Manifest.Outputs,
			}
			snapshot.Manifest = &manifest
		}
		if step.CADRuntime != nil {
			cadRuntime := *step.CADRuntime
			snapshot.CADRuntime = &cadRuntime
		}
		req.Handoff.Steps = append(req.Handoff.Steps, snapshot)
	}

	return req
}

func mustSubmissionJSON(t *testing.T, value interface{}) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(data)
}

func TestPackageEnvelopeFromPackage_RoundTrips(t *testing.T) {
	pkg := mustAPITestPackage(t, "widget")
	roundTrip := packageEnvelopeFromPackage(pkg).Handoff.toPackage()
	roundTrip.JobID = pkg.JobID

	if !reflect.DeepEqual(roundTrip, pkg) {
		t.Fatalf("package round trip mismatch:\ngot=%#v\nwant=%#v", roundTrip, pkg)
	}
}

func TestPackageEnvelopeFromPackage_RoundTripsTables(t *testing.T) {
	pkg := mustAPITestPackage(t, "widget")
	pkg.Tables = []metadata.TableInputMetadata{
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
	}

	roundTrip := packageEnvelopeFromPackage(pkg).Handoff.toPackage()
	roundTrip.JobID = pkg.JobID
	pkg.Tables = handoff.CloneTableInputs(pkg.Tables)

	if !reflect.DeepEqual(roundTrip, pkg) {
		t.Fatalf("package tables round trip mismatch:\ngot=%#v\nwant=%#v", roundTrip, pkg)
	}
}

func TestDecodeAndCanonicalizeSubmission_NormalizesAndPreservesTables(t *testing.T) {
	base := mustAPITestPackage(t, "widget")
	base.Tables = []metadata.TableInputMetadata{
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
	}

	makeRequest := func(pkg *handoff.Package) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(pkg))))
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	firstPkg, _, err := decodeAndCanonicalizeSubmission(makeRequest(base))
	if err != nil {
		t.Fatalf("first decodeAndCanonicalizeSubmission returned error: %v", err)
	}

	reordered := clonePackage(base)
	reordered.Tables = []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
	}
	secondPkg, _, err := decodeAndCanonicalizeSubmission(makeRequest(reordered))
	if err != nil {
		t.Fatalf("second decodeAndCanonicalizeSubmission returned error: %v", err)
	}

	wantTables := []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
	}
	if !reflect.DeepEqual(firstPkg.Tables, wantTables) {
		t.Fatalf("first tables mismatch:\ngot=%#v\nwant=%#v", firstPkg.Tables, wantTables)
	}
	if !reflect.DeepEqual(secondPkg.Tables, wantTables) {
		t.Fatalf("second tables mismatch:\ngot=%#v\nwant=%#v", secondPkg.Tables, wantTables)
	}
	if !reflect.DeepEqual(firstPkg, secondPkg) {
		t.Fatalf("canonical packages differ:\nfirst=%#v\nsecond=%#v", firstPkg, secondPkg)
	}
}

func TestDecodeAndCanonicalizeSubmission_RejectsInvalidTables(t *testing.T) {
	base := mustAPITestPackage(t, "widget")
	base.Tables = []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
		{LogicalID: "fasteners", Name: "duplicate_catalog", Fingerprint: "bbb"},
	}

	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, packageEnvelopeFromPackage(base))))
	req.Header.Set("Content-Type", "application/json")

	_, _, err := decodeAndCanonicalizeSubmission(req)
	if !errors.Is(err, errInvalidSubmission) {
		t.Fatalf("expected errInvalidSubmission, got %v", err)
	}
	if !errors.Is(err, handoff.ErrDuplicateTableLogicalID) {
		t.Fatalf("expected ErrDuplicateTableLogicalID, got %v", err)
	}
}

func TestPOSTJob_PreservesCanonicalTablesInStoredPackage(t *testing.T) {
	store := newInMemorySubmissionStore().(*inMemorySubmissionStore)
	handler := newHandler(HandlerOptions{SubmissionStore: store})

	pkg := mustAPITestPackage(t, "widget")
	pkg.Tables = []metadata.TableInputMetadata{
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
	}

	body := mustSubmissionJSON(t, packageEnvelopeFromPackage(pkg))
	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%q", rec.Code, rec.Body.String())
	}

	store.mu.Lock()
	stored, ok := store.packages[pkg.JobID]
	store.mu.Unlock()
	if !ok {
		t.Fatal("expected stored canonical package")
	}

	canonicalReq := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
	canonicalReq.Header.Set("Content-Type", "application/json")
	want, _, err := decodeAndCanonicalizeSubmission(canonicalReq)
	if err != nil {
		t.Fatalf("decodeAndCanonicalizeSubmission returned error: %v", err)
	}
	if !reflect.DeepEqual(&stored, want) {
		t.Fatalf("stored package mismatch:\ngot=%#v\nwant=%#v", stored, *want)
	}
}
