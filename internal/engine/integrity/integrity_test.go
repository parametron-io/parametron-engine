package integrity

import (
	"reflect"
	"strings"
	"testing"
)

func testDigest(fill byte) Digest {
	return Digest(strings.Repeat(string(fill), DigestLength))
}

func sourceFor(logicalID, coordinate string, digest Digest) ResourceSource {
	return ResourceSource{
		LogicalID:        logicalID,
		SourceCoordinate: coordinate,
		SourceDigest:     digest,
	}
}

func TestEvaluateLockStates(t *testing.T) {
	sourceDigest := testDigest('a')
	otherDigest := testDigest('b')
	source := sourceFor("widget_model", "models/widget.FCStd", sourceDigest)

	tests := []struct {
		name          string
		scope         LockScope
		wantLock      LockState
		wantResource  LockState
		wantLocked    Digest
		wantFailCode  string
		wantNoFailure bool
	}{
		{
			name:          "unlocked workflow is not performed",
			scope:         LockScope{},
			wantLock:      LockStateNotPerformed,
			wantResource:  LockStateNotPerformed,
			wantLocked:    "",
			wantNoFailure: true,
		},
		{
			name:          "matching locked digest is verified",
			scope:         LockScope{Present: true, Models: []LockEntry{{LogicalID: "widget_model", Digest: sourceDigest.String()}}},
			wantLock:      LockStateVerified,
			wantResource:  LockStateVerified,
			wantLocked:    sourceDigest,
			wantNoFailure: true,
		},
		{
			name:         "changed source bytes are a mismatch",
			scope:        LockScope{Present: true, Models: []LockEntry{{LogicalID: "widget_model", Digest: otherDigest.String()}}},
			wantLock:     LockStateMismatch,
			wantResource: LockStateMismatch,
			wantLocked:   otherDigest,
			wantFailCode: CodeLockSourceMismatch,
		},
		{
			name:         "short locked digest is malformed",
			scope:        LockScope{Present: true, Models: []LockEntry{{LogicalID: "widget_model", Digest: strings.Repeat("a", DigestLength-1)}}},
			wantLock:     LockStateMalformed,
			wantResource: LockStateMalformed,
			wantFailCode: CodeLockDigestMalformed,
		},
		{
			name:         "uppercase locked digest is malformed",
			scope:        LockScope{Present: true, Models: []LockEntry{{LogicalID: "widget_model", Digest: strings.Repeat("A", DigestLength)}}},
			wantLock:     LockStateMalformed,
			wantResource: LockStateMalformed,
			wantFailCode: CodeLockDigestMalformed,
		},
		{
			name:         "declared resource absent from lock scope is missing",
			scope:        LockScope{Present: true, Models: []LockEntry{{LogicalID: "other_model", Digest: sourceDigest.String()}}},
			wantLock:     LockStateMissing,
			wantResource: LockStateMissing,
			wantFailCode: CodeLockResourceMissing,
		},
		{
			name:         "invalid lock with malformed entry stays distinguishable",
			scope:        LockScope{Present: true, Invalid: true, Models: []LockEntry{{LogicalID: "widget_model", Digest: "not-a-digest"}}},
			wantLock:     LockStateMalformed,
			wantResource: LockStateMalformed,
			wantFailCode: CodeLockDigestMalformed,
		},
		{
			name:         "unreadable lock is unavailable",
			scope:        LockScope{Present: true, Invalid: true},
			wantLock:     LockStateUnavailable,
			wantResource: LockStateUnavailable,
			wantFailCode: CodeLockUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := EvaluateLock(tc.scope, []ResourceSource{source})
			if project.Lock != tc.wantLock {
				t.Fatalf("Lock = %q, want %q", project.Lock, tc.wantLock)
			}
			if len(project.Resources) != 1 {
				t.Fatalf("Resources length = %d, want 1", len(project.Resources))
			}
			got := project.Resources[0]
			if got.LockState != tc.wantResource {
				t.Fatalf("resource LockState = %q, want %q", got.LockState, tc.wantResource)
			}
			if got.LockedDigest != tc.wantLocked {
				t.Fatalf("resource LockedDigest = %q, want %q", got.LockedDigest, tc.wantLocked)
			}
			failure := project.Failure()
			if tc.wantNoFailure {
				if failure != nil {
					t.Fatalf("Failure() = %v, want nil", failure)
				}
				return
			}
			if failure == nil || failure.Code != tc.wantFailCode {
				t.Fatalf("Failure() = %#v, want code %q", failure, tc.wantFailCode)
			}
			if tc.wantFailCode != CodeLockUnavailable && failure.LogicalID != "widget_model" {
				t.Fatalf("failure logical ID = %q, want widget_model", failure.LogicalID)
			}
		})
	}
}

func TestEvaluateLockSelectsFailureDeterministically(t *testing.T) {
	locked := testDigest('1')
	sourcesA := []ResourceSource{
		{LogicalID: "z_model", SourceCoordinate: "models/z.FCStd", SourceDigest: testDigest('2')},
		{LogicalID: "a_model", SourceCoordinate: "models/a.FCStd", SourceDigest: testDigest('3')},
	}
	sourcesB := []ResourceSource{sourcesA[1], sourcesA[0]}
	scope := LockScope{Present: true, Models: []LockEntry{
		{LogicalID: "z_model", Digest: locked.String()},
		{LogicalID: "a_model", Digest: locked.String()},
	}}

	first := EvaluateLock(scope, sourcesA)
	second := EvaluateLock(scope, sourcesB)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent sources in different order differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	failure := first.Failure()
	if failure == nil || failure.Code != CodeLockSourceMismatch || failure.LogicalID != "a_model" {
		t.Fatalf("Failure() = %#v, want the lexicographically first mismatch", failure)
	}
}

func TestEvaluateLockIncludesZeroEdgeDeclarations(t *testing.T) {
	// A zero-reference or standalone resource has no reference edge and no
	// product selection; it is still a declared resource that participates.
	sources := []ResourceSource{
		sourceFor("standalone", "standalone/part.FCStd", testDigest('a')),
		sourceFor("unused", "other/part.FCStd", testDigest('b')),
	}
	scope := LockScope{Present: true, Models: []LockEntry{
		{LogicalID: "standalone", Digest: testDigest('a').String()},
		{LogicalID: "unused", Digest: testDigest('b').String()},
	}}
	project := EvaluateLock(scope, sources)
	if project.Lock != LockStateVerified || len(project.Resources) != 2 {
		t.Fatalf("project = %#v, want two verified zero-edge resources", project)
	}
	if project.Failure() != nil {
		t.Fatalf("Failure() = %v, want nil", project.Failure())
	}
}

func TestProjectIntegrityCopyIsDefensive(t *testing.T) {
	project := &ProjectIntegrity{
		Lock: LockStateVerified,
		Resources: []ResourceSource{
			sourceFor("b_model", "models/b.FCStd", testDigest('b')),
			sourceFor("a_model", "models/a.FCStd", testDigest('a')),
		},
	}
	copy := project.Copy()
	if copy == nil {
		t.Fatal("Copy() returned nil")
	}
	if got := []string{copy.Resources[0].LogicalID, copy.Resources[1].LogicalID}; !reflect.DeepEqual(got, []string{"a_model", "b_model"}) {
		t.Fatalf("Copy() ordering = %v, want logical ID order", got)
	}
	copy.Resources[0].LogicalID = "mutated"
	if project.Resources[0].LogicalID == "mutated" {
		t.Fatal("Copy() exposed caller-owned storage")
	}
	var nilProject *ProjectIntegrity
	if nilProject.Copy() != nil {
		t.Fatal("nil ProjectIntegrity.Copy() is not nil")
	}
	if nilProject.Failure() != nil {
		t.Fatal("nil ProjectIntegrity.Failure() is not nil")
	}
}

func TestMergeRunIntegrityKeepsContentIdentitiesSeparate(t *testing.T) {
	sourceDigest := testDigest('a')
	resultDigest := testDigest('b')
	project := &ProjectIntegrity{
		Lock: LockStateVerified,
		Resources: []ResourceSource{{
			LogicalID:        "widget_model",
			SourceCoordinate: "models/widget.FCStd",
			SourceDigest:     sourceDigest,
			LockState:        LockStateVerified,
			LockedDigest:     sourceDigest,
		}},
	}
	operation := &OperationIntegrity{Resources: []ResourceOperation{{
		LogicalID:                 "widget_model",
		MaterializedCoordinate:    "source/widget.FCStd",
		MaterializedDigest:        sourceDigest,
		MaterializationStatus:     StatusVerified,
		RuntimeBaselineDigest:     sourceDigest,
		RuntimeBaselineStatus:     StatusVerified,
		PersistedResultCoordinate: "source/widget.FCStd",
		PersistedResultDigest:     resultDigest,
		ResultStatus:              StatusVerified,
	}}}

	run, err := MergeRunIntegrity(project, operation)
	if err != nil {
		t.Fatalf("MergeRunIntegrity returned error: %v", err)
	}
	if run.Lock != LockStateVerified || len(run.Resources) != 1 {
		t.Fatalf("run = %#v, want one verified resource", run)
	}
	got := run.Resources[0]
	if got.SourceDigest != sourceDigest {
		t.Fatalf("SourceDigest = %q, want %q", got.SourceDigest, sourceDigest)
	}
	if got.MaterializedBaselineDigest != sourceDigest {
		t.Fatalf("MaterializedBaselineDigest = %q, want %q", got.MaterializedBaselineDigest, sourceDigest)
	}
	if got.RuntimeBaselineDigest != sourceDigest {
		t.Fatalf("RuntimeBaselineDigest = %q, want %q", got.RuntimeBaselineDigest, sourceDigest)
	}
	if got.PersistedResultDigest != resultDigest {
		t.Fatalf("PersistedResultDigest = %q, want the distinct configured-result digest %q", got.PersistedResultDigest, resultDigest)
	}
	if got.PersistedResultDigest == got.SourceDigest {
		t.Fatal("configured-result digest replaced the source identity")
	}
	if got.LockState != LockStateVerified || got.MaterializationStatus != StatusVerified ||
		got.RuntimeBaselineStatus != StatusVerified || got.ResultStatus != StatusVerified {
		t.Fatalf("statuses = %#v, want all verified", got)
	}
	if got.SourceCoordinate != "models/widget.FCStd" || got.MaterializedCoordinate != "source/widget.FCStd" ||
		got.PersistedResultCoordinate != "source/widget.FCStd" {
		t.Fatalf("coordinates = %#v, want source and attempt coordinates preserved", got)
	}
}

func TestMergeRunIntegrityDefaultsUndatedStagesToNotPerformed(t *testing.T) {
	project := &ProjectIntegrity{
		Lock: LockStateNotPerformed,
		Resources: []ResourceSource{{
			LogicalID:        "widget_model",
			SourceCoordinate: "models/widget.FCStd",
			SourceDigest:     testDigest('a'),
			LockState:        LockStateNotPerformed,
		}},
	}
	run, err := MergeRunIntegrity(project, nil)
	if err != nil {
		t.Fatalf("MergeRunIntegrity returned error: %v", err)
	}
	if run.Lock != LockStateNotPerformed {
		t.Fatalf("Lock = %q, want not_performed", run.Lock)
	}
	got := run.Resources[0]
	if got.LockState != LockStateNotPerformed {
		t.Fatalf("resource LockState = %q, want not_performed", got.LockState)
	}
	for name, status := range map[string]Status{
		"MaterializationStatus": got.MaterializationStatus,
		"RuntimeBaselineStatus": got.RuntimeBaselineStatus,
		"ResultStatus":          got.ResultStatus,
	} {
		if status != StatusNotPerformed {
			t.Fatalf("%s = %q, want not_performed", name, status)
		}
	}
	if got.MaterializedBaselineDigest != "" || got.RuntimeBaselineDigest != "" || got.PersistedResultDigest != "" {
		t.Fatalf("absent evidence produced content identities: %#v", got)
	}
}

func TestMergeRunIntegrityRejectsContradictoryEvidence(t *testing.T) {
	source := ResourceSource{
		LogicalID:        "widget_model",
		SourceCoordinate: "models/widget.FCStd",
		SourceDigest:     testDigest('a'),
		LockState:        LockStateVerified,
	}
	operation := ResourceOperation{
		LogicalID:              "widget_model",
		MaterializedCoordinate: "source/widget.FCStd",
		MaterializedDigest:     testDigest('a'),
		MaterializationStatus:  StatusVerified,
	}
	tests := []struct {
		name      string
		project   *ProjectIntegrity
		operation *OperationIntegrity
	}{
		{
			name:      "duplicate source identity",
			project:   &ProjectIntegrity{Lock: LockStateVerified, Resources: []ResourceSource{source, source}},
			operation: &OperationIntegrity{Resources: []ResourceOperation{operation}},
		},
		{
			name:      "duplicate operation evidence",
			project:   &ProjectIntegrity{Lock: LockStateVerified, Resources: []ResourceSource{source}},
			operation: &OperationIntegrity{Resources: []ResourceOperation{operation, operation}},
		},
		{
			name:      "operation evidence for an undeclared resource",
			project:   &ProjectIntegrity{Lock: LockStateVerified, Resources: []ResourceSource{source}},
			operation: &OperationIntegrity{Resources: []ResourceOperation{{LogicalID: "undeclared", MaterializedCoordinate: "source/other.FCStd"}}},
		},
		{
			name:      "operation evidence without declared resources",
			project:   nil,
			operation: &OperationIntegrity{Resources: []ResourceOperation{operation}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run, err := MergeRunIntegrity(tc.project, tc.operation)
			if err == nil || run != nil {
				t.Fatalf("MergeRunIntegrity = %#v, %v; want a provenance conflict", run, err)
			}
			failure := AsFailure(err)
			if failure == nil || failure.Code != CodeProvenanceConflict {
				t.Fatalf("failure = %#v, want code %q", failure, CodeProvenanceConflict)
			}
		})
	}
}

func TestNormalizeRunIntegrityIsDeterministic(t *testing.T) {
	input := &RunIntegrity{
		Lock: LockStateVerified,
		Resources: []ResourceIntegrity{
			{
				LogicalID:        " z_model ",
				SourceCoordinate: " models/z.FCStd ",
				SourceDigest:     testDigest('b'),
				LockState:        LockStateVerified,
			},
			{
				LogicalID:        "a_model",
				SourceCoordinate: "models/a.FCStd",
				SourceDigest:     testDigest('a'),
				LockState:        LockStateVerified,
			},
		},
	}

	first := NormalizeRunIntegrity(input)
	second := NormalizeRunIntegrity(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated normalization differs:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if got := []string{first.Resources[0].LogicalID, first.Resources[1].LogicalID}; !reflect.DeepEqual(got, []string{"a_model", "z_model"}) {
		t.Fatalf("normalized order = %v, want logical ID order with trimmed IDs", got)
	}
	if first.Resources[1].SourceCoordinate != "models/z.FCStd" {
		t.Fatalf("normalized coordinate = %q, want trimmed coordinate", first.Resources[1].SourceCoordinate)
	}
	if input.Resources[0].LogicalID != " z_model " {
		t.Fatal("normalization mutated caller-owned input")
	}
	if NormalizeRunIntegrity(nil) != nil {
		t.Fatal("NormalizeRunIntegrity(nil) is not nil")
	}
}

func TestOperationIntegrityCopyIsDefensive(t *testing.T) {
	operation := &OperationIntegrity{Resources: []ResourceOperation{
		{LogicalID: "b_model", MaterializedCoordinate: "source/b.FCStd"},
		{LogicalID: "a_model", MaterializedCoordinate: "source/a.FCStd"},
	}}
	copy := operation.Copy()
	if copy == nil || len(copy.Resources) != 2 {
		t.Fatalf("Copy() = %#v", copy)
	}
	if copy.Resources[0].LogicalID != "a_model" {
		t.Fatalf("Copy() ordering = %#v, want logical ID order", copy.Resources)
	}
	copy.Resources[0].LogicalID = "mutated"
	if operation.Resources[0].LogicalID == "mutated" {
		t.Fatal("Copy() exposed caller-owned storage")
	}
	var nilOperation *OperationIntegrity
	if nilOperation.Copy() != nil {
		t.Fatal("nil OperationIntegrity.Copy() is not nil")
	}
	if _, err := MergeRunIntegrity(nil, nil); err != nil {
		t.Fatalf("MergeRunIntegrity(nil, nil) returned error: %v", err)
	}
}

func TestEvaluateLockDoesNotTreatEmptyDigestAsVerified(t *testing.T) {
	source := sourceFor("widget_model", "models/widget.FCStd", "")
	project := EvaluateLock(LockScope{Present: true, Models: []LockEntry{{LogicalID: "widget_model", Digest: testDigest('a').String()}}}, []ResourceSource{source})
	failure := project.Failure()
	if failure == nil || failure.Code != CodeLockSourceMismatch {
		t.Fatalf("Failure() = %#v, want a source mismatch for an empty source identity", failure)
	}
}
