package api

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"parametron/internal/engine/handoff"
	"parametron/internal/engine/metadata"
)

func TestInMemoryJobQueue_EnqueueThenDequeueReturnsClonedPackage(t *testing.T) {
	queue := newInMemoryJobQueue()
	pkg := mustQueueTestPackage(t, "widget")
	want := clonePackage(pkg)

	if err := queue.Enqueue(pkg); err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}

	got, ok := queue.Dequeue()
	if !ok {
		t.Fatal("expected queued package")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dequeued package mismatch:\ngot=%#v\nwant=%#v", got, want)
	}
	if queue.Len() != 0 {
		t.Fatalf("Len after dequeue = %d, want 0", queue.Len())
	}
}

func TestInMemoryJobQueue_FIFOOrdering(t *testing.T) {
	queue := newInMemoryJobQueue()
	first := mustQueueTestPackage(t, "widget")
	second := mustQueueTestPackage(t, "gear")
	third := mustQueueTestPackage(t, "bracket")

	for _, pkg := range []*handoff.Package{first, second, third} {
		if err := queue.Enqueue(pkg); err != nil {
			t.Fatalf("Enqueue(%s) returned error: %v", pkg.JobID, err)
		}
	}

	for _, want := range []*handoff.Package{first, second, third} {
		got, ok := queue.Dequeue()
		if !ok {
			t.Fatalf("expected package for %s", want.JobID)
		}
		if got.JobID != want.JobID {
			t.Fatalf("dequeue jobID = %q, want %q", got.JobID, want.JobID)
		}
	}
}

func TestInMemoryJobQueue_DuplicateEnqueueDoesNotAppend(t *testing.T) {
	queue := newInMemoryJobQueue()
	pkg := mustQueueTestPackage(t, "widget")

	if err := queue.Enqueue(pkg); err != nil {
		t.Fatalf("first Enqueue returned error: %v", err)
	}
	if err := queue.Enqueue(clonePackage(pkg)); err != nil {
		t.Fatalf("second Enqueue returned error: %v", err)
	}

	if queue.Len() != 1 {
		t.Fatalf("Len = %d, want 1", queue.Len())
	}

	got, ok := queue.Dequeue()
	if !ok {
		t.Fatal("expected queued package")
	}
	if got.JobID != pkg.JobID {
		t.Fatalf("dequeue jobID = %q, want %q", got.JobID, pkg.JobID)
	}
	if _, ok := queue.Dequeue(); ok {
		t.Fatal("expected queue to be empty after single dequeue")
	}
}

func TestInMemoryJobQueue_DequeueOnEmptyIsStable(t *testing.T) {
	queue := newInMemoryJobQueue()

	for i := 0; i < 2; i++ {
		got, ok := queue.Dequeue()
		if ok || got != nil {
			t.Fatalf("Dequeue empty = (%#v, %v), want (nil, false)", got, ok)
		}
	}
}

func TestInMemoryJobQueue_DefensiveClones(t *testing.T) {
	queue := newInMemoryJobQueue()
	pkg := mustQueueTestPackage(t, "widget")
	pkg.Tables = []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
	}

	if err := queue.Enqueue(pkg); err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}

	pkg.CSV.Headers[0] = "mutated-header"
	pkg.Manifest.Values["width"] = 9001
	pkg.Tables[0].Name = "mutated-table"

	peeked, ok := queue.Peek()
	if !ok {
		t.Fatal("expected queued package")
	}
	if peeked.CSV.Headers[0] != "width" {
		t.Fatalf("peek CSV header = %q, want %q", peeked.CSV.Headers[0], "width")
	}
	if got := peeked.Manifest.Values["width"]; got != 42 {
		t.Fatalf("peek manifest width = %#v, want 42", got)
	}
	if peeked.Tables[0].Name != "fastener_catalog" {
		t.Fatalf("peek table name = %q, want %q", peeked.Tables[0].Name, "fastener_catalog")
	}

	peeked.CSV.Headers[0] = "peek-mutated"
	peeked.Manifest.Values["width"] = -1
	peeked.Tables[0].Name = "peek-table-mutated"

	dequeued, ok := queue.Dequeue()
	if !ok {
		t.Fatal("expected queued package on dequeue")
	}
	if dequeued.CSV.Headers[0] != "width" {
		t.Fatalf("dequeue CSV header = %q, want %q", dequeued.CSV.Headers[0], "width")
	}
	if got := dequeued.Manifest.Values["width"]; got != 42 {
		t.Fatalf("dequeue manifest width = %#v, want 42", got)
	}
	if dequeued.Tables[0].Name != "fastener_catalog" {
		t.Fatalf("dequeue table name = %q, want %q", dequeued.Tables[0].Name, "fastener_catalog")
	}
}

func TestInMemoryJobQueue_ConcurrentEnqueueSameJobDoesNotDuplicate(t *testing.T) {
	queue := newInMemoryJobQueue()
	pkg := mustQueueTestPackage(t, "widget")

	const workers = 32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := queue.Enqueue(clonePackage(pkg)); err != nil {
				t.Errorf("Enqueue returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	if queue.Len() != 1 {
		t.Fatalf("Len = %d, want 1", queue.Len())
	}
	if !queue.Contains(pkg.JobID) {
		t.Fatalf("Contains(%q) = false, want true", pkg.JobID)
	}
}

func TestInMemoryJobQueue_ConcurrentEnqueueDifferentJobsRetainsAll(t *testing.T) {
	queue := newInMemoryJobQueue()
	pkgs := []*handoff.Package{
		mustQueueTestPackage(t, "widget"),
		mustQueueTestPackage(t, "gear"),
		mustQueueTestPackage(t, "bracket"),
		mustQueueTestPackage(t, "panel"),
	}

	var wg sync.WaitGroup
	for _, pkg := range pkgs {
		pkg := pkg
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := queue.Enqueue(pkg); err != nil {
				t.Errorf("Enqueue(%s) returned error: %v", pkg.JobID, err)
			}
		}()
	}
	wg.Wait()

	if queue.Len() != len(pkgs) {
		t.Fatalf("Len = %d, want %d", queue.Len(), len(pkgs))
	}

	seen := make(map[string]struct{}, len(pkgs))
	for range pkgs {
		pkg, ok := queue.Dequeue()
		if !ok {
			t.Fatal("expected queued package")
		}
		seen[pkg.JobID] = struct{}{}
	}
	for _, pkg := range pkgs {
		if _, ok := seen[pkg.JobID]; !ok {
			t.Fatalf("job %q missing from queue", pkg.JobID)
		}
	}
}

func TestInMemoryJobQueue_LenPeekContainsDeterministic(t *testing.T) {
	queue := newInMemoryJobQueue()
	first := mustQueueTestPackage(t, "widget")
	second := mustQueueTestPackage(t, "gear")

	if queue.Len() != 0 {
		t.Fatalf("initial Len = %d, want 0", queue.Len())
	}
	if queue.Contains(first.JobID) {
		t.Fatalf("initial Contains(%q) = true, want false", first.JobID)
	}
	if pkg, ok := queue.Peek(); ok || pkg != nil {
		t.Fatalf("initial Peek = (%#v, %v), want (nil, false)", pkg, ok)
	}

	if err := queue.Enqueue(first); err != nil {
		t.Fatalf("Enqueue(first) returned error: %v", err)
	}
	if err := queue.Enqueue(second); err != nil {
		t.Fatalf("Enqueue(second) returned error: %v", err)
	}

	if queue.Len() != 2 {
		t.Fatalf("Len = %d, want 2", queue.Len())
	}
	if !queue.Contains(first.JobID) || !queue.Contains(second.JobID) {
		t.Fatal("expected queue to contain both jobs")
	}

	peeked, ok := queue.Peek()
	if !ok {
		t.Fatal("expected Peek to succeed")
	}
	if peeked.JobID != first.JobID {
		t.Fatalf("Peek jobID = %q, want %q", peeked.JobID, first.JobID)
	}
	if queue.Len() != 2 {
		t.Fatalf("Len after Peek = %d, want 2", queue.Len())
	}
}

func TestInMemoryJobQueue_RejectsInvalidPackagesDeterministically(t *testing.T) {
	queue := newInMemoryJobQueue()

	if err := queue.Enqueue(nil); !errors.Is(err, errQueuedPackageRequired) {
		t.Fatalf("Enqueue(nil) error = %v, want %v", err, errQueuedPackageRequired)
	}
	if err := queue.Enqueue(&handoff.Package{}); !errors.Is(err, errQueuedJobIDRequired) {
		t.Fatalf("Enqueue(empty jobID) error = %v, want %v", err, errQueuedJobIDRequired)
	}
	if queue.Len() != 0 {
		t.Fatalf("Len after invalid enqueue = %d, want 0", queue.Len())
	}
}

func mustQueueTestPackage(t *testing.T, productKey string) *handoff.Package {
	t.Helper()
	return clonePackage(mustAPITestPackage(t, productKey))
}
