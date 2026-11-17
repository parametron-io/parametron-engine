package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/integrity"
	"parametron/internal/engine/metadata"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

// These tests drive the normal CLI execution path with a controlled aligned
// runtime that emits per-resource integrity evidence. They prove lock
// enforcement, byte-preserving baseline verification, independent persisted
// result hashing, provenance separation, and raw evidence preservation without
// requiring FreeCAD.

type integrityRuntimeInvocation struct {
	Argv                 []string `json:"argv"`
	IntegrityRequestPath string   `json:"integrityRequestPath"`
}

type integrityPackageRun struct {
	planned     *plannedRun
	result      *executionResult
	err         error
	invocations []integrityRuntimeInvocation
}

func installIntegrityAwareRuntime(t *testing.T, mode string) string {
	t.Helper()
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("resolve python3 for controlled integrity runtime: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "parametron-freecad")
	script := `#!` + pythonPath + `
import hashlib
import json
import os
import sys

def arg(name):
    i = sys.argv.index(name)
    return sys.argv[i + 1]

def sha(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()

working = os.path.abspath(arg("--working-copy"))
manifest_path = arg("--manifest")
result_path = arg("--result")
output_dir = arg("--output-dir")
request_path = arg("--observation-request")
mode = os.environ.get("PARAMETRON_INTEGRITY_RUNTIME_MODE", "success")

with open(manifest_path, "r", encoding="utf-8") as handle:
    manifest = json.load(handle)
with open(request_path, "r", encoding="utf-8") as handle:
    contract = json.load(handle)

integrity_path = os.environ.get("PARAMETRON_RUNTIME_INTEGRITY_REQUEST", "")
log_path = os.environ.get("PARAMETRON_INTEGRITY_RUNTIME_LOG", "")
if log_path:
    with open(log_path, "a", encoding="utf-8") as handle:
        json.dump({"argv": sys.argv[1:], "integrityRequestPath": integrity_path}, handle, sort_keys=True)
        handle.write("\n")

source = manifest["sourceDocument"]
source_path = source if os.path.isabs(source) else os.path.join(working, *source.split("/"))
baseline = sha(source_path)

artifacts = []
for output in manifest.get("outputs", []):
    relative = output["path"]
    target = os.path.join(working, *relative.split("/"))
    os.makedirs(os.path.dirname(target), exist_ok=True)
    with open(target, "w", encoding="utf-8") as handle:
        handle.write(output["format"] + "\n")
    artifacts.append({"id": output["id"], "format": output["format"], "path": relative})

expected = contract.get("expected", {})
bindings = {item["id"]: item for item in contract.get("observationContext", {}).get("parameters", [])}
parameters = []
for item in expected.get("parameters", []):
    binding = bindings.get(item["id"], {})
    parameters.append({
        "id": item["id"],
        "name": binding.get("name", item.get("name", item["id"])),
        "value": item["value"],
        "valueKind": "number",
    })
metadata = []
for item in expected.get("metadata", []):
    metadata.append({
        "id": item.get("id", item.get("key")),
        "key": item["key"],
        "value": baseline if item["key"] == "working_copy_sha256" else item["value"],
        "valueKind": item.get("valueKind", "string"),
    })
references = [{"kind": item["kind"], "name": item["name"]} for item in expected.get("references", [])]
components = [{"id": item["id"], "kind": item["kind"], "name": item["name"]} for item in expected.get("components", [])]
observed = {
    "schemaVersion": "1.0",
    "workingCopy": {"path": working, "sha256": baseline},
    "observation": {
        "parameters": parameters,
        "metadata": metadata,
        "references": references,
        "components": components,
    },
}
with open(os.path.join(output_dir, "prm.observed.json"), "w", encoding="utf-8") as handle:
    json.dump(observed, handle, sort_keys=True, separators=(",", ":"))
    handle.write("\n")

if integrity_path:
    with open(integrity_path, "r", encoding="utf-8") as handle:
        request = json.load(handle)
    resources = []
    for item in request["resources"]:
        coordinate = item["materializedCoordinate"]
        path = os.path.join(working, *coordinate.split("/"))
        entry = {
            "logicalId": item["logicalId"],
            "materializedCoordinate": coordinate,
            "status": "observed",
            "baselineSha256": sha(path),
        }
        if item["expectsPersistedResult"]:
            if mode == "baseline_mismatch":
                entry["baselineSha256"] = "0" * 64
            with open(path, "ab") as handle:
                handle.write(b"configured-result-mutation")
            entry["persistedResult"] = {"coordinate": coordinate, "sha256": sha(path)}
            if mode == "persisted_mismatch":
                entry["persistedResult"]["sha256"] = "0" * 64
        resources.append(entry)
    if mode == "evidence_unrequested":
        resources.append({
            "logicalId": "extra",
            "materializedCoordinate": "source/Extra.FCStd",
            "status": "observed",
            "baselineSha256": "0" * 64,
        })
    with open(os.path.join(output_dir, "prm.integrity-evidence.json"), "w", encoding="utf-8") as handle:
        json.dump({"schemaVersion": "1.0", "resources": resources}, handle, sort_keys=True, separators=(",", ":"))
        handle.write("\n")

result = {"schemaVersion": "1.0", "status": "succeeded", "artifacts": artifacts}
with open(result_path, "w", encoding="utf-8") as handle:
    json.dump(result, handle, sort_keys=True, separators=(",", ":"))
    handle.write("\n")
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write controlled integrity runtime: %v", err)
	}
	t.Setenv("PARAMETRON_FREECAD_RUNTIME", path)
	t.Setenv("PARAMETRON_INTEGRITY_RUNTIME_MODE", mode)
	return path
}

func runIntegrityProject(t *testing.T, projectDir, outDir, mode string) integrityPackageRun {
	t.Helper()
	installIntegrityAwareRuntime(t, mode)
	logPath := filepath.Join(t.TempDir(), "invocations.jsonl")
	t.Setenv("PARAMETRON_INTEGRITY_RUNTIME_LOG", logPath)

	resetGlobals()
	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun: %v", err)
	}
	result, err := executePlanRun(executionOptions{Planned: planned, OutputDir: outDir, UseCache: false})
	if result == nil {
		t.Fatalf("executePlanRun returned no result: %v", err)
	}
	return integrityPackageRun{planned: planned, result: result, err: err, invocations: readIntegrityInvocations(t, logPath)}
}

func readIntegrityInvocations(t *testing.T, logPath string) []integrityRuntimeInvocation {
	t.Helper()
	file, err := os.Open(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("open controlled runtime invocation log: %v", err)
	}
	defer file.Close()

	var out []integrityRuntimeInvocation
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var invocation integrityRuntimeInvocation
		if err := json.Unmarshal(scanner.Bytes(), &invocation); err != nil {
			t.Fatalf("decode invocation log line: %v", err)
		}
		out = append(out, invocation)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeIntegrityLock(t *testing.T, projectDir string) string {
	t.Helper()
	if _, err := executeHarnessCommand(t, []string{"sync", "--file", projectDir}); err != nil {
		t.Fatalf("sync project lock: %v", err)
	}
	return filepath.Join(projectDir, "prm.project-lock.json")
}

func rewriteIntegrityLock(t *testing.T, lockPath string, mutate func(map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	mutate(lock)
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lockModels(t *testing.T, lock map[string]any) map[string]any {
	t.Helper()
	resources, ok := lock["resources"].(map[string]any)
	if !ok {
		t.Fatalf("lock resources = %#v", lock["resources"])
	}
	models, ok := resources["models"].(map[string]any)
	if !ok {
		t.Fatalf("lock model resources = %#v", resources["models"])
	}
	return models
}

func loadRunMetadata(t *testing.T, runRoot string) metadata.Metadata {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runRoot, metadata.FileName))
	if err != nil {
		t.Fatalf("read run metadata: %v", err)
	}
	var md metadata.Metadata
	if err := json.Unmarshal(data, &md); err != nil {
		t.Fatalf("unmarshal run metadata: %v\n%s", err, data)
	}
	return md
}

func integrityOperationByID(resources []integrity.ResourceOperation, logicalID string) integrity.ResourceOperation {
	for _, resource := range resources {
		if resource.LogicalID == logicalID {
			return resource
		}
	}
	return integrity.ResourceOperation{}
}

func metadataResourceByID(md metadata.Metadata, logicalID string) metadata.IntegrityResourceMetadata {
	if md.Integrity == nil {
		return metadata.IntegrityResourceMetadata{}
	}
	for _, resource := range md.Integrity.Resources {
		if resource.LogicalID == logicalID {
			return resource
		}
	}
	return metadata.IntegrityResourceMetadata{}
}

func provenanceResourceByID(provenance *recordcontract.IntegrityProvenance, logicalID string) recordcontract.ResourceProvenance {
	if provenance == nil {
		return recordcontract.ResourceProvenance{}
	}
	for _, resource := range provenance.Resources {
		if resource.LogicalID == logicalID {
			return resource
		}
	}
	return recordcontract.ResourceProvenance{}
}

func readExecutionRecord(t *testing.T, runRoot string) recordcontract.ExecutionRecord {
	t.Helper()
	packageRoot := recordPackageRoot(runRoot)
	data := readRecordPackageFiles(t, packageRoot)[filepath.ToSlash(recordpackage.MustRecordContractPath("execution"))]
	if len(data) == 0 {
		t.Fatalf("execution record missing from package %q", packageRoot)
	}
	var record recordcontract.ExecutionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("unmarshal execution record: %v\n%s", err, data)
	}
	return record
}

func TestIntegrityNormalRunLockedVerifiesSourceAndPersistedResult(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, projectDir)
	lockPath := writeIntegrityLock(t, projectDir)
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	run := runIntegrityProject(t, projectDir, filepath.Join(t.TempDir(), "out"), "success")
	if run.err != nil {
		t.Fatalf("normal integrity run failed: %v", run.err)
	}
	if len(run.invocations) != 1 {
		t.Fatalf("runtime invoked %d times, want once", len(run.invocations))
	}

	invocation := run.invocations[0]
	if invocation.IntegrityRequestPath == "" {
		t.Fatal("runtime was not given an integrity request path")
	}
	if strings.Contains(strings.Join(invocation.Argv, " "), invocation.IntegrityRequestPath) {
		t.Fatalf("integrity request path leaked into argv: %v", invocation.Argv)
	}
	requestBytes, err := os.ReadFile(invocation.IntegrityRequestPath)
	if err != nil {
		t.Fatalf("read published integrity request: %v", err)
	}
	request, err := cadruntime.ParseFreeCADIntegrityRequest(requestBytes)
	if err != nil {
		t.Fatalf("parse published integrity request: %v\n%s", err, requestBytes)
	}
	if len(request.Resources) != 3 {
		t.Fatalf("integrity request resources = %#v, want all three declared resources", request.Resources)
	}
	persisted := 0
	for _, resource := range request.Resources {
		if resource.ExpectsPersistedResult {
			persisted++
			if resource.LogicalID != "box_model" {
				t.Fatalf("unexpected persisted-result resource %q", resource.LogicalID)
			}
		}
	}
	if persisted != 1 {
		t.Fatalf("expectsPersistedResult count = %d, want exactly one", persisted)
	}

	outcome := singleCADRuntimeOutcome(t, run.result.Execution)
	if outcome.Integrity == nil || len(outcome.Integrity.Resources) != 3 {
		t.Fatalf("runtime outcome integrity = %#v, want three resources", outcome.Integrity)
	}
	primary := integrityOperationByID(outcome.Integrity.Resources, "box_model")
	if primary.LogicalID == "" {
		t.Fatal("primary resource absent from operation provenance")
	}
	if primary.MaterializationStatus != integrity.StatusVerified ||
		primary.RuntimeBaselineStatus != integrity.StatusVerified ||
		primary.ResultStatus != integrity.StatusVerified {
		t.Fatalf("primary statuses = %#v, want verified materialization, baseline, and result", primary)
	}
	if primary.MaterializedDigest != primary.RuntimeBaselineDigest {
		t.Fatalf("materialized and runtime baseline digests differ: %q / %q", primary.MaterializedDigest, primary.RuntimeBaselineDigest)
	}
	if primary.PersistedResultDigest == "" || primary.PersistedResultDigest == primary.MaterializedDigest {
		t.Fatalf("configured-result digest = %q, want a distinct persisted identity", primary.PersistedResultDigest)
	}
	for _, logicalID := range []string{"alias", "unused"} {
		secondary := integrityOperationByID(outcome.Integrity.Resources, logicalID)
		if secondary.LogicalID == "" || secondary.RuntimeBaselineStatus != integrity.StatusVerified {
			t.Fatalf("secondary resource %q = %#v, want a verified baseline", logicalID, secondary)
		}
		if secondary.ResultStatus != integrity.StatusNotReported {
			t.Fatalf("secondary resource %q claimed a persisted result: %#v", logicalID, secondary)
		}
	}

	md := loadRunMetadata(t, run.result.RunRoot)
	if md.Integrity == nil || md.Integrity.Lock != string(integrity.LockStateVerified) {
		t.Fatalf("metadata integrity lock = %#v, want verified", md.Integrity)
	}
	metadataPrimary := metadataResourceByID(md, "box_model")
	if metadataPrimary.LockState != string(integrity.LockStateVerified) {
		t.Fatalf("metadata lock state = %q, want verified", metadataPrimary.LockState)
	}
	if metadataPrimary.SourceDigestSHA256 == "" || metadataPrimary.BaselineDigestSHA256 != metadataPrimary.SourceDigestSHA256 {
		t.Fatalf("metadata primary identities = %#v, want source/baseline equality", metadataPrimary)
	}
	if metadataPrimary.PersistedResultDigestSHA256 != primary.PersistedResultDigest.String() {
		t.Fatalf("metadata result digest = %q, want %q", metadataPrimary.PersistedResultDigestSHA256, primary.PersistedResultDigest)
	}

	execution := readExecutionRecord(t, run.result.RunRoot)
	provenancePrimary := provenanceResourceByID(execution.Provenance.Integrity, "box_model")
	if provenancePrimary.SourceDigestSHA256 != metadataPrimary.SourceDigestSHA256 ||
		provenancePrimary.BaselineDigestSHA256 != metadataPrimary.BaselineDigestSHA256 ||
		provenancePrimary.RuntimeBaselineDigestSHA256 != metadataPrimary.BaselineDigestSHA256 ||
		provenancePrimary.PersistedResultDigestSHA256 != metadataPrimary.PersistedResultDigestSHA256 {
		t.Fatalf("normalized provenance identities = %#v, want separate source/baseline/result fields", provenancePrimary)
	}
	if provenancePrimary.PersistedResultDigestSHA256 == provenancePrimary.SourceDigestSHA256 {
		t.Fatal("configured-result digest replaced the source identity in normalized provenance")
	}

	rawPath := filepath.Join(recordPackageRoot(run.result.RunRoot), filepath.FromSlash(recordpackage.RawRuntimeIntegrityEvidenceContractPath()))
	rawBytes, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read preserved raw integrity evidence: %v", err)
	}
	runtimeEvidence, err := os.ReadFile(outcome.IntegrityEvidencePath)
	if err != nil {
		t.Fatalf("read runtime integrity evidence: %v", err)
	}
	if !bytes.Equal(rawBytes, runtimeEvidence) {
		t.Fatalf("raw integrity evidence was not preserved verbatim")
	}
	if bytes.Equal(rawBytes, readRecordPackageFiles(t, recordPackageRoot(run.result.RunRoot))[filepath.ToSlash(recordpackage.MustRecordContractPath("execution"))]) {
		t.Fatal("raw integrity evidence was not kept separate from normalized records")
	}

	lockAfter, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lockBefore, lockAfter) {
		t.Fatal("execution rewrote the project source lock")
	}
	if bytes.Contains(lockAfter, []byte(primary.PersistedResultDigest.String())) {
		t.Fatal("configured-result digest was written into the project source lock")
	}
	if !bytes.Contains(lockAfter, []byte(metadataPrimary.SourceDigestSHA256)) {
		t.Fatal("project source lock lost the accepted source digest")
	}
}

func TestIntegrityNormalRunUnlockedRecordsNotPerformed(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, projectDir)

	run := runIntegrityProject(t, projectDir, filepath.Join(t.TempDir(), "out"), "success")
	if run.err != nil {
		t.Fatalf("unlocked integrity run failed: %v", run.err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "prm.project-lock.json")); !os.IsNotExist(err) {
		t.Fatalf("unlocked run created a lock: %v", err)
	}
	if len(run.invocations) != 1 {
		t.Fatalf("runtime invoked %d times, want once", len(run.invocations))
	}

	outcome := singleCADRuntimeOutcome(t, run.result.Execution)
	primary := integrityOperationByID(outcome.Integrity.Resources, "box_model")
	if primary.MaterializationStatus != integrity.StatusVerified || primary.ResultStatus != integrity.StatusVerified {
		t.Fatalf("unlocked run did not record materialization and result identity: %#v", primary)
	}
	if primary.PersistedResultDigest == primary.MaterializedDigest {
		t.Fatal("unlocked run did not represent the configured result separately")
	}

	md := loadRunMetadata(t, run.result.RunRoot)
	if md.Integrity == nil || md.Integrity.Lock != string(integrity.LockStateNotPerformed) {
		t.Fatalf("unlocked metadata integrity = %#v, want not_performed", md.Integrity)
	}
	metadataPrimary := metadataResourceByID(md, "box_model")
	if metadataPrimary.LockState != string(integrity.LockStateNotPerformed) {
		t.Fatalf("unlocked resource lock state = %q, want not_performed", metadataPrimary.LockState)
	}
	if metadataPrimary.LockState == string(integrity.LockStateVerified) {
		t.Fatal("unlocked workflow falsely claimed lock verification")
	}

	execution := readExecutionRecord(t, run.result.RunRoot)
	if execution.Provenance.Integrity == nil || execution.Provenance.Integrity.Lock != recordcontract.LockVerificationNotPerformed {
		t.Fatalf("unlocked normalized lock state = %#v, want not_performed", execution.Provenance.Integrity)
	}
	provenancePrimary := provenanceResourceByID(execution.Provenance.Integrity, "box_model")
	if provenancePrimary.LockState != recordcontract.LockVerificationNotPerformed {
		t.Fatalf("unlocked resource normalized lock state = %q, want not_performed", provenancePrimary.LockState)
	}
	if provenancePrimary.SourceDigestSHA256 == "" || provenancePrimary.PersistedResultDigestSHA256 == "" {
		t.Fatalf("unlocked run lost content identities: %#v", provenancePrimary)
	}
}

func TestIntegrityRunRejectsChangedSourceAfterLock(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, projectDir)
	lockPath := writeIntegrityLock(t, projectDir)
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "input", "box.FCStd"), []byte("changed after lock creation"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := runIntegrityProject(t, projectDir, filepath.Join(t.TempDir(), "out"), "success")
	if run.err == nil {
		t.Fatal("run accepted source bytes changed after lock creation")
	}
	if len(run.invocations) != 0 {
		t.Fatalf("runtime invoked %d times despite a rejected source lock", len(run.invocations))
	}
	failure := readCLIFailureRecord(t, recordPackageRoot(run.result.RunRoot))
	if failure.Failure.Class != recordcontract.FailureClassIntegrity ||
		failure.Failure.Stage != recordcontract.FailureStageIntegrity ||
		failure.Failure.Code != integrity.CodeLockSourceMismatch {
		t.Fatalf("failure record = %#v, want integrity/integrity/%s", failure.Failure, integrity.CodeLockSourceMismatch)
	}
	if _, err := os.Stat(filepath.Join(recordPackageRoot(run.result.RunRoot), filepath.FromSlash(recordpackage.RawRuntimeIntegrityEvidenceContractPath()))); !os.IsNotExist(err) {
		t.Fatalf("rejected preflight run preserved runtime integrity evidence: %v", err)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lockBefore, lockAfter) {
		t.Fatal("rejected run rewrote the project source lock")
	}
}

func TestIntegrityRunRejectsMalformedLockDigest(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, projectDir)
	lockPath := writeIntegrityLock(t, projectDir)
	rewriteIntegrityLock(t, lockPath, func(lock map[string]any) {
		models := lockModels(t, lock)
		entry := models["box_model"].(map[string]any)
		entry["sha256"] = "not-a-canonical-digest"
	})

	run := runIntegrityProject(t, projectDir, filepath.Join(t.TempDir(), "out"), "success")
	if run.err == nil {
		t.Fatal("run accepted a malformed locked digest")
	}
	if len(run.invocations) != 0 {
		t.Fatalf("runtime invoked %d times despite a malformed lock", len(run.invocations))
	}
	failure := readCLIFailureRecord(t, recordPackageRoot(run.result.RunRoot))
	if failure.Failure.Class != recordcontract.FailureClassIntegrity ||
		failure.Failure.Code != integrity.CodeLockDigestMalformed {
		t.Fatalf("failure record = %#v, want integrity/%s", failure.Failure, integrity.CodeLockDigestMalformed)
	}
}

func TestIntegrityRunRejectsDeclaredResourceMissingFromLock(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, projectDir)
	lockPath := writeIntegrityLock(t, projectDir)
	rewriteIntegrityLock(t, lockPath, func(lock map[string]any) {
		delete(lockModels(t, lock), "unused")
	})

	run := runIntegrityProject(t, projectDir, filepath.Join(t.TempDir(), "out"), "success")
	if run.err == nil {
		t.Fatal("run accepted a declared resource missing from the required lock scope")
	}
	if len(run.invocations) != 0 {
		t.Fatalf("runtime invoked %d times despite an incomplete lock scope", len(run.invocations))
	}
	failure := readCLIFailureRecord(t, recordPackageRoot(run.result.RunRoot))
	if failure.Failure.Class != recordcontract.FailureClassIntegrity ||
		failure.Failure.Code != integrity.CodeLockResourceMissing {
		t.Fatalf("failure record = %#v, want integrity/%s", failure.Failure, integrity.CodeLockResourceMissing)
	}
}

func TestIntegrityRunRejectsRuntimeBaselineMismatch(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, projectDir)
	writeIntegrityLock(t, projectDir)

	run := runIntegrityProject(t, projectDir, filepath.Join(t.TempDir(), "out"), "baseline_mismatch")
	if run.err == nil {
		t.Fatal("run accepted a runtime baseline mismatch")
	}
	if len(run.invocations) != 1 {
		t.Fatalf("runtime invoked %d times, want once", len(run.invocations))
	}
	failure := readCLIFailureRecord(t, recordPackageRoot(run.result.RunRoot))
	if failure.Failure.Class != recordcontract.FailureClassIntegrity ||
		failure.Failure.Stage != recordcontract.FailureStageIntegrity ||
		failure.Failure.Code != integrity.CodeRuntimeBaselineMismatch {
		t.Fatalf("failure record = %#v, want integrity/integrity/%s", failure.Failure, integrity.CodeRuntimeBaselineMismatch)
	}
	if failure.Failure.Class == recordcontract.FailureClassVerification || failure.Failure.Class == recordcontract.FailureClassRuntime {
		t.Fatalf("runtime baseline mismatch collapsed into %q", failure.Failure.Class)
	}
	if _, err := os.Stat(filepath.Join(recordPackageRoot(run.result.RunRoot), filepath.FromSlash(recordpackage.RawRuntimeIntegrityEvidenceContractPath()))); err != nil {
		t.Fatalf("runtime baseline mismatch did not preserve raw integrity evidence: %v", err)
	}
}

func TestIntegrityRunRejectsPersistedResultMismatch(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, projectDir)
	writeIntegrityLock(t, projectDir)

	run := runIntegrityProject(t, projectDir, filepath.Join(t.TempDir(), "out"), "persisted_mismatch")
	if run.err == nil {
		t.Fatal("run accepted a reported persisted-result digest that disagrees with the persisted bytes")
	}
	if len(run.invocations) != 1 {
		t.Fatalf("runtime invoked %d times, want once", len(run.invocations))
	}
	failure := readCLIFailureRecord(t, recordPackageRoot(run.result.RunRoot))
	if failure.Failure.Class != recordcontract.FailureClassIntegrity ||
		failure.Failure.Code != integrity.CodeResultMismatch {
		t.Fatalf("failure record = %#v, want integrity/%s", failure.Failure, integrity.CodeResultMismatch)
	}
	rawBytes, err := os.ReadFile(filepath.Join(recordPackageRoot(run.result.RunRoot), filepath.FromSlash(recordpackage.RawRuntimeIntegrityEvidenceContractPath())))
	if err != nil {
		t.Fatalf("persisted-result mismatch did not preserve raw integrity evidence: %v", err)
	}
	if !bytes.Contains(rawBytes, []byte(strings.Repeat("0", 64))) {
		t.Fatalf("raw integrity evidence does not contain the reported result digest: %s", rawBytes)
	}
}

func TestIntegrityRunRejectsUnrequestedEvidence(t *testing.T) {
	contractPackageEnv(t)
	projectDir := writeContractPackageFixture(t, nil)
	addDeclaredModels(t, projectDir)
	writeIntegrityLock(t, projectDir)

	run := runIntegrityProject(t, projectDir, filepath.Join(t.TempDir(), "out"), "evidence_unrequested")
	if run.err == nil {
		t.Fatal("run accepted integrity evidence for an unrequested coordinate")
	}
	failure := readCLIFailureRecord(t, recordPackageRoot(run.result.RunRoot))
	if failure.Failure.Class != recordcontract.FailureClassIntegrity ||
		failure.Failure.Code != integrity.CodeEvidenceUnrequested {
		t.Fatalf("failure record = %#v, want integrity/%s", failure.Failure, integrity.CodeEvidenceUnrequested)
	}
}
