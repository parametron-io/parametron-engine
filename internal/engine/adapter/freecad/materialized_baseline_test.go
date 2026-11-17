package freecad

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/integrity"
	"parametron/internal/engine/resourcebinding"
)

func contentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// prepareBaselineAttempt builds a prepared attempt with a selected resource and
// one additional zero-reference declared resource. Both source digests are
// captured from the exact source bytes unless overridden.
func prepareBaselineAttempt(t *testing.T, source, additional []byte, override map[string]string) FreeCADRuntimeAttempt {
	t.Helper()
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, source)
	root := filepath.Dir(req.Manifest.Inputs.SourceModel)
	additionalCoordinate := "additional/part.FCStd"
	additionalPath := filepath.Join(root, filepath.FromSlash(additionalCoordinate))
	if err := os.MkdirAll(filepath.Dir(additionalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(additionalPath, additional, 0o644); err != nil {
		t.Fatal(err)
	}

	digests := map[string]string{
		"widget":  contentDigest(source),
		"zunused": contentDigest(additional),
	}
	for logicalID, value := range override {
		digests[logicalID] = value
	}
	inventory, err := resourcebinding.New([]resourcebinding.Binding{
		{
			ProjectID: "p", Kind: resourcebinding.ResourceKindModel, LogicalID: "widget",
			SourceCoordinate: filepath.Base(req.Manifest.Inputs.SourceModel), SourceDigest: digests["widget"],
		},
		{
			ProjectID: "p", Kind: resourcebinding.ResourceKindModel, LogicalID: "zunused",
			SourceCoordinate: additionalCoordinate, SourceDigest: digests["zunused"],
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Resources, err = resourcebinding.Select(inventory, "p", "widget", root)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func materializedPaths(t *testing.T, attempt FreeCADRuntimeAttempt) map[string]string {
	t.Helper()
	paths := make(map[string]string)
	for _, binding := range attempt.Resources.Bindings() {
		path, err := attempt.Layout.ResourcePath(binding.MaterializedCoordinate)
		if err != nil {
			t.Fatalf("resolve materialized coordinate for %q: %v", binding.LogicalID, err)
		}
		paths[binding.LogicalID] = path
	}
	return paths
}

func TestVerifyFreeCADRuntimeMaterializedBaselinesAcceptsBytePreservingEquality(t *testing.T) {
	source := []byte("byte-preserving primary source")
	additional := []byte("byte-preserving zero-reference source")
	attempt := prepareBaselineAttempt(t, source, additional, nil)

	baselines, err := VerifyFreeCADRuntimeMaterializedBaselines(attempt)
	if err != nil {
		t.Fatalf("VerifyFreeCADRuntimeMaterializedBaselines returned error: %v", err)
	}
	if len(baselines) != 2 {
		t.Fatalf("baselines = %d, want every declared resource", len(baselines))
	}
	if got := []string{baselines[0].Binding.LogicalID, baselines[1].Binding.LogicalID}; !reflect.DeepEqual(got, []string{"widget", "zunused"}) {
		t.Fatalf("baseline order = %v, want declared logical ID order", got)
	}
	wantDigests := map[string]string{"widget": contentDigest(source), "zunused": contentDigest(additional)}
	for _, baseline := range baselines {
		if baseline.Digest.String() != wantDigests[baseline.Binding.LogicalID] {
			t.Fatalf("baseline digest for %q = %q, want captured source digest %q",
				baseline.Binding.LogicalID, baseline.Digest, wantDigests[baseline.Binding.LogicalID])
		}
		if baseline.Binding.SourceDigest != baseline.Digest.String() {
			t.Fatalf("materialized baseline for %q does not equal the accepted source identity", baseline.Binding.LogicalID)
		}
		if baseline.Coordinate != baseline.Binding.MaterializedCoordinate {
			t.Fatalf("baseline coordinate for %q = %q, want %q",
				baseline.Binding.LogicalID, baseline.Coordinate, baseline.Binding.MaterializedCoordinate)
		}
	}
}

func TestVerifyFreeCADRuntimeMaterializedBaselinesRejectsTamperedMaterialization(t *testing.T) {
	source := []byte("accepted source bytes")
	additional := []byte("accepted additional bytes")
	attempt := prepareBaselineAttempt(t, source, additional, nil)
	paths := materializedPaths(t, attempt)
	tampered := []byte("tampered materialized bytes")
	if err := os.WriteFile(paths["widget"], tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	baselines, err := VerifyFreeCADRuntimeMaterializedBaselines(attempt)
	failure := integrity.AsFailure(err)
	if failure == nil || failure.Code != integrity.CodeMaterializationMismatch {
		t.Fatalf("error = %v, want materialization_content_mismatch", err)
	}
	if failure.LogicalID != "widget" {
		t.Fatalf("failure logical ID = %q, want widget", failure.LogicalID)
	}
	if failure.Coordinate != "source/Widget.FCStd" {
		t.Fatalf("failure coordinate = %q, want the attempt-relative coordinate", failure.Coordinate)
	}
	if failure.ExpectedDigest.String() != contentDigest(source) || failure.ActualDigest.String() != contentDigest(tampered) {
		t.Fatalf("failure digests = %q/%q, want captured source and actual materialized digests",
			failure.ExpectedDigest, failure.ActualDigest)
	}
	if strings.Contains(failure.Error(), attempt.Layout.WorkingCopyDir) || strings.Contains(failure.Error(), filepath.Dir(paths["widget"])) {
		t.Fatalf("failure identity leaked a host absolute path: %q", failure.Error())
	}
	if !strings.Contains(failure.Error(), "source/Widget.FCStd") {
		t.Fatalf("failure identity omitted the attempt-relative coordinate: %q", failure.Error())
	}
	if len(baselines) != 1 {
		t.Fatalf("verified baselines before the mismatch = %d, want the preceding resource only", len(baselines))
	}
}

func TestVerifyFreeCADRuntimeMaterializedBaselinesRejectsMissingMaterializedResource(t *testing.T) {
	attempt := prepareBaselineAttempt(t, []byte("source"), []byte("additional"), nil)
	paths := materializedPaths(t, attempt)
	if err := os.Remove(paths["zunused"]); err != nil {
		t.Fatal(err)
	}

	_, err := VerifyFreeCADRuntimeMaterializedBaselines(attempt)
	failure := integrity.AsFailure(err)
	if failure == nil || failure.Code != integrity.CodeMaterializationMissing {
		t.Fatalf("error = %v, want materialized_resource_missing", err)
	}
	if failure.LogicalID != "zunused" {
		t.Fatalf("failure logical ID = %q, want zunused", failure.LogicalID)
	}
	if failure.Coordinate == "" || strings.Contains(failure.Coordinate, string(filepath.Separator)) && filepath.IsAbs(failure.Coordinate) {
		t.Fatalf("failure coordinate = %q, want an attempt-relative coordinate", failure.Coordinate)
	}
}

func TestVerifyFreeCADRuntimeMaterializedBaselinesReportsUnmaterializedResource(t *testing.T) {
	attempt := prepareBaselineAttempt(t, []byte("source"), []byte("additional"), nil)
	// Drop the completed materialization coordinate without touching the disk.
	bindings := attempt.Resources.Bindings()
	for i := range bindings {
		bindings[i].MaterializedCoordinate = ""
	}
	inventory, err := resourcebinding.New(bindings)
	if err != nil {
		t.Fatal(err)
	}
	attempt.Resources = inventory

	_, err = VerifyFreeCADRuntimeMaterializedBaselines(attempt)
	failure := integrity.AsFailure(err)
	if failure == nil || failure.Code != integrity.CodeMaterializationMissing {
		t.Fatalf("error = %v, want materialized_resource_missing", err)
	}
}

func TestVerifyFreeCADRuntimeMaterializedBaselinesIsRelocationInvariant(t *testing.T) {
	source := []byte("identical accepted source bytes")
	first := prepareBaselineAttempt(t, source, []byte("identical additional"), nil)
	second := prepareBaselineAttempt(t, source, []byte("identical additional"), nil)

	firstBaselines, err := VerifyFreeCADRuntimeMaterializedBaselines(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBaselines, err := VerifyFreeCADRuntimeMaterializedBaselines(second)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstBaselines) != len(secondBaselines) {
		t.Fatalf("baseline counts differ: %d / %d", len(firstBaselines), len(secondBaselines))
	}
	for index := range firstBaselines {
		if firstBaselines[index].Binding.LogicalID != secondBaselines[index].Binding.LogicalID ||
			firstBaselines[index].Coordinate != secondBaselines[index].Coordinate ||
			firstBaselines[index].Digest != secondBaselines[index].Digest {
			t.Fatalf("relocation changed stable baseline identity:\nfirst:  %#v\nsecond: %#v",
				firstBaselines[index], secondBaselines[index])
		}
	}

	tampered := []byte("identically tampered bytes")
	for _, attempt := range []FreeCADRuntimeAttempt{first, second} {
		paths := materializedPaths(t, attempt)
		if err := os.WriteFile(paths["widget"], tampered, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	firstFailure := integrity.AsFailure(mustVerifyFailure(t, first))
	secondFailure := integrity.AsFailure(mustVerifyFailure(t, second))
	if firstFailure == nil || secondFailure == nil {
		t.Fatal("tampered materialization was not rejected")
	}
	if firstFailure.Error() != secondFailure.Error() {
		t.Fatalf("host root changed stable failure identity:\nfirst:  %q\nsecond: %q", firstFailure.Error(), secondFailure.Error())
	}
}

func TestVerifyFreeCADRuntimeMaterializedBaselinesReportsFirstFailureDeterministically(t *testing.T) {
	attempt := prepareBaselineAttempt(t, []byte("source"), []byte("additional"), nil)
	paths := materializedPaths(t, attempt)
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	first := integrity.AsFailure(mustVerifyFailure(t, attempt))
	if first == nil || first.Code != integrity.CodeMaterializationMismatch {
		t.Fatalf("failure = %#v, want materialization_content_mismatch", first)
	}
	if first.LogicalID != "widget" {
		t.Fatalf("first failure logical ID = %q, want the lexicographically first resource widget", first.LogicalID)
	}
}

func TestVerifyFreeCADRuntimeMaterializedBaselinesWithoutSourceIdentityStillHashes(t *testing.T) {
	// A binding without captured source identity cannot prove byte equality, but
	// the materialized baseline is still hashed and reported.
	attempt := prepareBaselineAttempt(t, []byte("source"), []byte("additional"), map[string]string{"widget": ""})
	baselines, err := VerifyFreeCADRuntimeMaterializedBaselines(attempt)
	if err != nil {
		t.Fatalf("VerifyFreeCADRuntimeMaterializedBaselines returned error: %v", err)
	}
	if len(baselines) != 2 {
		t.Fatalf("baselines = %d, want both resources", len(baselines))
	}
	if baselines[0].Digest.String() != contentDigest([]byte("source")) {
		t.Fatalf("hash-only baseline = %q, want the actual materialized content digest", baselines[0].Digest)
	}
}

func mustVerifyFailure(t *testing.T, attempt FreeCADRuntimeAttempt) error {
	t.Helper()
	_, err := VerifyFreeCADRuntimeMaterializedBaselines(attempt)
	if err == nil {
		t.Fatal("VerifyFreeCADRuntimeMaterializedBaselines accepted an invalid baseline")
	}
	return err
}
