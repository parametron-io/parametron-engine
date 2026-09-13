#!/usr/bin/env python3
"""Reusable Task 14 aligned CAD runtime integration proof."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


class ProofFailure(RuntimeError):
    pass


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def normalized_json_digest(path):
    def normalize(value):
        if isinstance(value, dict):
            return {key: normalize(item) for key, item in value.items()
                    if key not in ("createdAt", "startedAt", "endedAt", "durationMs")}
        if isinstance(value, list):
            return [normalize(item) for item in value]
        if isinstance(value, str) and value.startswith("/") and "/_working/" in value:
            tail = value.split("/_working/", 1)[1].split("/", 1)
            return "<attempt>/" + (tail[1] if len(tail) == 2 else "")
        return value
    encoded = json.dumps(normalize(json_file(path)), sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


def json_file(path):
    return json.loads(path.read_text(encoding="utf-8"))


def run(command, *, cwd, env=None, timeout=180, check=True):
    completed = subprocess.run(
        [str(item) for item in command], cwd=cwd, env=env, text=True,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout,
    )
    if check and completed.returncode:
        raise ProofFailure(
            f"command failed ({completed.returncode}): {' '.join(map(str, command))}\n"
            f"stdout:\n{completed.stdout}\nstderr:\n{completed.stderr}"
        )
    return completed


def require(condition, message):
    if not condition:
        raise ProofFailure(message)


def find_one(root, name):
    matches = sorted(root.rglob(name))
    require(len(matches) == 1, f"expected one {name} beneath {root}, found {len(matches)}")
    return matches[0]


def invocations(path):
    if not path.exists():
        return []
    with path.open(encoding="utf-8") as handle:
        fcntl.flock(handle, fcntl.LOCK_SH)
        return [json.loads(line) for line in handle if line.strip()]


def cache_isolated_cli(ctx, name, mode, *, runtime, expect_success=True):
    root = ctx.workspace / name
    cwd = root / "cwd"
    out = root / "out"
    state = root / "state"
    log = root / "invocations.jsonl"
    cwd.mkdir(parents=True)
    env = dict(os.environ)
    env.update({
        "PARAMETRON_FREECAD_RUNTIME": str(runtime),
        "PARAMETRON_CAD_PROOF_MODE": mode,
        "PARAMETRON_CAD_PROOF_STATE_DIR": str(state),
        "PARAMETRON_CAD_PROOF_INVOCATION_LOG": str(log),
    })
    result = run(
        [ctx.parametron, "--project", ctx.fixture, "--out", out],
        cwd=cwd, env=env, timeout=ctx.timeout, check=False,
    )
    require((result.returncode == 0) == expect_success,
            f"{name}: unexpected exit {result.returncode}\n{result.stdout}\n{result.stderr}")
    effective_out = cwd / "output"
    reports = sorted(effective_out.rglob("report.json"))
    require(reports, f"{name}: report.json missing")
    return {
        "root": root, "out": effective_out, "log": log, "result": result,
        "reportPath": reports[-1], "report": json_file(reports[-1]),
    }


def stable_cli_facts(item):
    manifests = [path for path in item["out"].rglob("manifest.json")
                 if "parametron-record-package" not in path.parts]
    require(len(manifests) == 1, f"expected one run artifact manifest, found {len(manifests)}")
    manifest = manifests[0]
    metadata_matches = [path for path in item["out"].rglob("metadata.json")
                        if "parametron-record-package" not in path.parts]
    require(len(metadata_matches) == 1, f"expected one run metadata file, found {len(metadata_matches)}")
    metadata = metadata_matches[0]
    record_manifest = find_one(item["out"], "parametron.record-package.json")
    runtime_manifests = [path for path in item["out"].rglob("export_manifest_v1.json")
                         if "_working" in path.parts]
    require(len(runtime_manifests) == 1, f"expected one attempt manifest, found {len(runtime_manifests)}")
    runtime_manifest = runtime_manifests[0]
    request = find_one(item["out"], "parametron.verification.json")
    result = find_one(item["out"], "result.json")
    steps = sorted(item["out"].rglob("*.step"))
    require(steps, "accepted STEP artifact missing")
    report = item["report"]
    return {
        "planHash": report.get("planHash"),
        "jobID": report["jobs"][0]["jobId"],
        "manifestHash": digest(runtime_manifest),
        "requestHash": normalized_json_digest(request),
        "resultHash": digest(result),
        "stepHash": digest(steps[0]),
        "artifactManifestHash": normalized_json_digest(manifest),
        "metadataPresent": metadata.exists(),
        "recordPackageHash": digest(record_manifest),
        "stepCount": len(steps),
    }


def normalized_report_outcome(report):
    return {
        "status": report.get("status"),
        "planHash": report.get("planHash"),
        "jobs": [{
            "jobId": job.get("jobId"),
            "productKey": job.get("productKey"),
            "steps": [
                {"index": step.get("index"), "type": step.get("type"),
                 "status": step.get("status"), "attempts": step.get("attempts")}
                for step in job.get("steps", [])
            ],
        } for job in report.get("jobs", [])],
        "artifacts": [
            {"productId": item.get("productId"), "type": item.get("type"),
             "filename": item.get("filename"), "path": item.get("path"),
             "sizeBytes": item.get("sizeBytes")}
            for item in report.get("artifacts", [])
        ],
    }


def normalized_observed_semantics(path):
    value = json_file(path)
    value["workingCopy"]["path"] = "<working-copy>"
    for reference in value["observation"].get("references", []):
        if reference.get("kind") == "working_copy_path":
            reference["name"] = "<working-copy>"
    return value


def normalize_step_export_metadata(data):
    return re.sub(
        br"(FILE_NAME\('Open CASCADE Shape Model',')[^']+(')",
        br"\1<export-timestamp>\2",
        data,
        count=1,
    )


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def request_json(url, method="GET", payload=None, timeout=5):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=timeout) as response:
        return response.status, json.loads(response.read())


def wait_health(base, process, timeout=15):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise ProofFailure("API server exited before becoming healthy")
        try:
            with urllib.request.urlopen(base + "/healthz", timeout=1) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.URLError):
            time.sleep(0.05)
    raise ProofFailure("API server health timeout")


def start_api(ctx, name, mode, runtime, *, concurrent=False):
    root = ctx.workspace / name
    artifacts = root / "artifacts"
    state = root / "state"
    log = root / "invocations.jsonl"
    root.mkdir(parents=True)
    port = free_port()
    env = dict(os.environ)
    env.update({
        "PARAMETRON_FREECAD_RUNTIME": str(runtime),
        "PARAMETRON_CAD_PROOF_MODE": mode,
        "PARAMETRON_CAD_PROOF_STATE_DIR": str(state),
        "PARAMETRON_CAD_PROOF_INVOCATION_LOG": str(log),
    })
    command = [str(ctx.engine), "--addr", f"127.0.0.1:{port}", "--artifact-dir", str(artifacts)]
    if concurrent:
        env["PARAMETRON_CAD_PROOF_SERVER_ADDR"] = f"127.0.0.1:{port}"
        env["PARAMETRON_CAD_PROOF_ARTIFACT_DIR"] = str(artifacts)
        command = [str(ctx.concurrent_server), "-test.run=^TestCADRuntimeConcurrentProofServer$"]
    proc = subprocess.Popen(
        command,
        cwd=root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    base = f"http://127.0.0.1:{port}"
    wait_health(base, proc)
    return {"root": root, "artifacts": artifacts, "log": log, "proc": proc, "base": base}


def aligned_submission(ctx, product="box"):
    result = run(
        [ctx.smoke, "--source-model", ctx.source_model, "--product", product],
        cwd=ctx.workspace, timeout=30,
    )
    return json.loads(result.stdout)


def submit_and_wait(server, payload, terminal=True, timeout=90):
    code, accepted = request_json(server["base"] + "/job", "POST", payload)
    require(code == 202, "job submission was not accepted")
    status, seen = wait_job(server, accepted["jobId"], terminal=terminal, timeout=timeout)
    return accepted, status, seen


def wait_job(server, job_id, terminal=True, timeout=90):
    deadline = time.monotonic() + timeout
    seen = []
    while time.monotonic() < deadline:
        _, status = request_json(server["base"] + "/job/" + job_id)
        state = status["state"]
        if not seen or seen[-1] != state:
            seen.append(state)
        if state in ("succeeded", "failed", "canceled"):
            return status, seen
        if not terminal and state == "running":
            return status, seen
        time.sleep(0.05)
    raise ProofFailure(f"job {job_id} polling timeout")


def stop_api(server):
    proc = server["proc"]
    if proc.poll() is None:
        proc.send_signal(signal.SIGTERM)
    try:
        stdout, stderr = proc.communicate(timeout=15)
    except subprocess.TimeoutExpired:
        proc.kill()
        stdout, stderr = proc.communicate()
        raise ProofFailure(f"API did not shut down gracefully\n{stdout}\n{stderr}")
    require(proc.returncode == 0, f"API exit={proc.returncode}\n{stdout}\n{stderr}")


class Context:
    pass


def build_binaries(ctx):
    bindir = ctx.workspace / "bin"
    bindir.mkdir(parents=True)
    ctx.parametron = bindir / "parametron"
    ctx.engine = bindir / "parametron-engine"
    ctx.smoke = bindir / "parametron-engine-smoke"
    for output, package in (
        (ctx.parametron, "./cmd/parametron"),
        (ctx.engine, "./cmd/parametron-engine"),
        (ctx.smoke, "./cmd/parametron-engine-smoke"),
    ):
        run(["nix", "develop", "--command", "go", "build", "-o", output, package],
            cwd=ctx.engine_repo, timeout=ctx.timeout)


def concurrent_isolation_proof(ctx, runtime):
    ctx.concurrent_server = ctx.workspace / "bin" / "concurrent-api.test"
    run(["nix", "develop", "--command", "go", "test", "-c", "-o",
         ctx.concurrent_server, "./internal/engine/api"], cwd=ctx.engine_repo, timeout=ctx.timeout)
    server = start_api(ctx, "fake-concurrent", "concurrent", runtime, concurrent=True)
    products = ("concurrent-alpha", "concurrent-beta", "concurrent-failure")
    modes = dict(zip(products, ("success", "success", "malformed_result")))
    state_dir = server["root"] / "state"
    accepted = {}
    snapshots = []
    try:
        state_dir.mkdir()
        (state_dir / "modes.json").write_text(json.dumps(modes))
        for product in products:
            payload = aligned_submission(ctx, product)
            require([s["type"] for s in payload["handoff"]["steps"]] ==
                    ["WriteCSV", "WriteExportManifest", "RunCADRuntime"], "unexpected proof steps")
            submission, _, _ = submit_and_wait(server, payload, terminal=False)
            require(submission["productKey"] == product and
                    submission["jobId"] == payload["handoff"]["jobID"], "submission identity mismatch")
            accepted[product] = submission["jobId"]
        require(len(set(accepted.values())) == 3, "concurrent job identities collided")
        deadline = time.monotonic() + 30
        while len(invocations(server["log"])) < 3 and time.monotonic() < deadline:
            time.sleep(0.02)
        calls = invocations(server["log"])
        require(len(calls) == 3, "three external runtimes did not overlap")
        require({c["productKey"] for c in calls} == set(products), "runtime product mismatch")
        for field in ("pid", "attemptID", "workingCopy", "result"):
            require(len({c[field] for c in calls}) == 3, f"shared runtime {field}")
        for call in calls:
            os.kill(call["pid"], 0)
            require(not Path(call["result"]).exists(), "runtime completed before release")
        completed = {}
        stable = {}

        def snapshot():
            evidence = {}
            artifact_ids = set()
            artifact_paths = {}
            for product, job_id in accepted.items():
                _, status = request_json(server["base"] + "/job/" + job_id)
                _, listing = request_json(server["base"] + "/job/" + job_id + "/artifacts")
                expected = completed.get(product, "running")
                require(status["state"] == expected, f"{product}: state changed to {status['state']}")
                require(status["jobId"] == job_id and status["productKey"] == product and
                        listing["jobId"] == job_id, "HTTP identity mismatch")
                artifacts = listing["artifacts"]
                contents = []
                if expected == "succeeded":
                    require(artifacts, "successful job has no artifacts")
                    require(any(a["filename"].endswith(product + ".step") for a in artifacts),
                            "successful STEP missing")
                    for artifact in artifacts:
                        require(artifact["jobId"] == job_id and artifact["productId"] == product,
                                "artifact ownership mismatch")
                        require(artifact["id"] not in artifact_ids and
                                artifact_paths.get(artifact["path"], job_id) == job_id,
                                "concurrent jobs share an artifact identity or path")
                        artifact_ids.add(artifact["id"])
                        artifact_paths[artifact["path"]] = job_id
                        with urllib.request.urlopen(server["base"] + "/artifacts/" + artifact["id"], timeout=5) as response:
                            require(response.status == 200, "artifact download failed")
                            data = response.read()
                        contents.append(data.decode())
                        require(hashlib.sha256(data).hexdigest() == artifact["checksumSHA256"],
                                "download does not match artifact checksum")
                        if artifact["filename"].endswith(".step"):
                            require(product in data.decode(), "STEP content belongs to another product")
                else:
                    require(artifacts == [], "unfinished or failed job exposes artifacts")
                report = None
                if product in completed:
                    # Reports have no HTTP route; inspect the persisted per-job report.
                    report = json_file(find_one(server["artifacts"] / "jobs" / job_id, "report.json"))
                    require(len(report["jobs"]) == 1, "report includes another job")
                    require(report["jobs"][0]["jobId"] == job_id and
                            report["jobs"][0]["productKey"] == product, "report identity mismatch")
                    step = next(s for s in report["jobs"][0]["steps"] if s["type"] == "RunCADRuntime")
                    require(step["attempts"] == 1, "unexpected runtime retry")
                    if expected == "failed":
                        require(report["artifacts"] == [] and status["error"]["productId"] == product,
                                "failure ownership mismatch")
                    else:
                        require(not status.get("error"), "successful job carries an error")
                combined = json.dumps([status, listing, report, contents])
                for other in products:
                    if other != product:
                        require(other not in combined and accepted[other] not in combined,
                                "cross-job identity, metadata, path or content leak")
                        other_call = next(c for c in calls if c["productKey"] == other)
                        require(other_call["attemptID"] not in combined, "cross-attempt path leak")
                if product in stable:
                    require(stable[product] == combined, "terminal job changed after peer completion")
                if product in completed:
                    stable[product] = combined
                evidence[product] = {"status": status, "listing": listing, "report": report,
                                     "contents": contents}
            snapshots.append(evidence)

        snapshot()
        # Success while peers run, failure while one success runs, then final success.
        for product in (products[0], products[2], products[1]):
            (state_dir / (product + ".release")).touch()
            terminal, _ = wait_job(server, accepted[product])
            expected = "failed" if modes[product] == "malformed_result" else "succeeded"
            require(terminal["state"] == expected, "unexpected concurrent outcome")
            completed[product] = expected
            snapshot()
        snapshot()
        require(len(invocations(server["log"])) == 3, "unexpected additional invocation")
        (server["root"] / "isolation-evidence.json").write_text(json.dumps(snapshots, indent=2))
        return {"name": "fake_concurrent_isolation", "status": "passed", "runtimeInvocations": 3,
                "jobIDs": [accepted[p] for p in products], "overlappingRuntimes": 3,
                "terminalStates": [completed[p] for p in products], "isolationSnapshots": len(snapshots)}
    finally:
        stop_api(server)


def fake_proof(ctx, runtime):
    scenarios = []
    success = cache_isolated_cli(ctx, "fake-cli-success", "success", runtime=runtime)
    facts = stable_cli_facts(success)
    require(len(invocations(success["log"])) == 1, "fake CLI did not invoke runtime exactly once")
    scenarios.append({"name": "fake_cli_success", "status": "passed",
                      "runtimeInvocations": 1, **facts})

    server = start_api(ctx, "fake-api-success", "success", runtime)
    try:
        accepted, status, seen = submit_and_wait(server, aligned_submission(ctx))
        require(status["state"] == "succeeded", f"API success state={status['state']}")
        require(len(invocations(server["log"])) == 1, "fake API invocation count is not one")
        require(list(server["artifacts"].rglob("report.json")), "API report missing at terminal observation")
        scenarios.append({"name": "fake_api_success", "status": "passed",
                          "runtimeInvocations": 1, "jobIDs": [accepted["jobId"]],
                          "terminalState": status["state"]})
    finally:
        stop_api(server)

    scenarios.append(concurrent_isolation_proof(ctx, runtime))

    first = cache_isolated_cli(ctx, "fake-repeat-1", "success", runtime=runtime)
    second = cache_isolated_cli(ctx, "fake-repeat-2", "success", runtime=runtime)
    a, b = stable_cli_facts(first), stable_cli_facts(second)
    require(a == b, f"repeated fake stable facts differ: {a!r} != {b!r}")
    scenarios.append({"name": "fake_repeated", "status": "passed",
                      "runtimeInvocations": 2, "stable": True, **a})

    stale_root = "fake-stale"
    stale = cache_isolated_cli(ctx, stale_root, "success", runtime=runtime)
    cache_dir = stale["root"] / "cwd" / ".cache"
    if cache_dir.exists():
        shutil.rmtree(cache_dir)
    env = dict(os.environ)
    env.update({
        "PARAMETRON_FREECAD_RUNTIME": str(runtime),
        "PARAMETRON_CAD_PROOF_MODE": "missing_artifact",
        "PARAMETRON_CAD_PROOF_STATE_DIR": str(stale["root"] / "state"),
        "PARAMETRON_CAD_PROOF_INVOCATION_LOG": str(stale["log"]),
    })
    rerun = run([ctx.parametron, "--project", ctx.fixture, "--out", stale["out"]],
                cwd=stale["root"] / "cwd", env=env, timeout=ctx.timeout, check=False)
    require(rerun.returncode != 0, "same-root stale output caused false success")
    require(len(invocations(stale["log"])) == 2, "same-root proof did not invoke twice")
    scenarios.append({"name": "fake_stale_output", "status": "passed",
                      "runtimeInvocations": 2, "falseSuccess": False})

    for mode in ("retry_then_success", "runtime_failure", "malformed_result",
                 "missing_artifact", "observed_mismatch"):
        server = start_api(ctx, "fake-" + mode, mode, runtime)
        try:
            _, status, _ = submit_and_wait(server, aligned_submission(ctx))
            calls = len(invocations(server["log"]))
            if mode == "retry_then_success":
                require(status["state"] == "succeeded" and calls == 3,
                        f"retry proof state={status['state']} calls={calls}")
            else:
                require(status["state"] == "failed", f"{mode}: state={status['state']}")
                persisted = find_one(server["artifacts"], "report.json")
                require(json_file(persisted).get("artifacts") == [],
                        f"{mode}: report exposes accepted artifacts")
            scenarios.append({"name": mode, "status": "passed",
                              "runtimeInvocations": calls, "terminalState": status["state"]})
        finally:
            stop_api(server)

    server = start_api(ctx, "fake-cancellation", "block", runtime)
    payload = aligned_submission(ctx)
    _, _, _ = submit_and_wait(server, payload, terminal=False, timeout=30)
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline and not invocations(server["log"]):
        time.sleep(0.05)
    calls = invocations(server["log"])
    require(len(calls) == 1, "blocked runtime was not invoked once")
    child_pid = calls[0]["pid"]
    stop_api(server)
    try:
        os.kill(child_pid, 0)
        alive = True
    except ProcessLookupError:
        alive = False
    require(not alive, f"orphan controlled runtime remains alive: pid={child_pid}")
    require(not list(server["artifacts"].rglob("*.step")), "canceled job exposed STEP")
    scenarios.append({"name": "cancellation", "status": "passed",
                      "runtimeInvocations": 1, "orphanProcess": False})
    return scenarios


def real_proof(ctx):
    built = run(["nix", "build", "--no-link", "--print-out-paths",
                 str(ctx.freecad_repo) + "#parametron-freecad"],
                cwd=ctx.engine_repo, timeout=ctx.timeout)
    runtime = Path(built.stdout.strip().splitlines()[-1]) / "bin" / "parametron-freecad"
    require(runtime.is_file() and os.access(runtime, os.X_OK), "built real wrapper is not executable")
    smoke = run([runtime, "smoke"], cwd=ctx.workspace, timeout=ctx.timeout)
    smoke_payload = json.loads(smoke.stdout)
    require(smoke_payload.get("status") == "ok" and smoke_payload.get("host") == "freecadcmd",
            f"unexpected real smoke payload: {smoke_payload}")
    runs = []
    for index in (1, 2):
        item = cache_isolated_cli(ctx, f"real-{index}", "success", runtime=runtime)
        facts = stable_cli_facts(item)
        step = find_one(item["out"], "*.step")
        require(step.stat().st_size > 0 and b"ISO-10303-21" in step.read_bytes()[:4096],
                "real STEP is empty or lacks STEP header")
        runs.append((item, facts))
    a, b = runs[0][1], runs[1][1]
    stable_keys = (
        "planHash", "jobID", "manifestHash", "requestHash", "resultHash",
        "recordPackageHash", "stepCount",
    )
    require(all(a[key] == b[key] for key in stable_keys), "real repeated Engine identity differs")
    observed_a = find_one(runs[0][0]["out"], "parametron.observed.json")
    observed_b = find_one(runs[1][0]["out"], "parametron.observed.json")
    require(normalized_observed_semantics(observed_a) == normalized_observed_semantics(observed_b),
            "real repeated observed semantics differ")
    require(normalized_report_outcome(runs[0][0]["report"]) ==
            normalized_report_outcome(runs[1][0]["report"]),
            "real repeated structured report outcome differs")
    steps = [find_one(item["out"], "*.step") for item, _ in runs]
    step_bytes = [path.read_bytes() for path in steps]
    step_equal = step_bytes[0] == step_bytes[1]
    metadata_only = step_equal or (
        normalize_step_export_metadata(step_bytes[0]) ==
        normalize_step_export_metadata(step_bytes[1])
    )
    require(metadata_only, "real STEP semantic/geometric bytes differ beyond exporter metadata")
    artifact_types = [item.get("type") for item in runs[0][0]["report"].get("artifacts", [])]
    require(artifact_types.count("step") == 2 and artifact_types.count("json") == 2,
            f"real accepted artifact inventory is incomplete: {artifact_types}")
    require(not any(name in str(item.get("filename", "")) for item in runs[0][0]["report"].get("artifacts", [])
                    for name in ("parametron.observed", "parametron.verification", "source/")),
            "raw real runtime evidence was promoted")
    return {
        "launcher": "parametron-freecad",
        "smokePassed": True,
        "host": smoke_payload.get("host"),
        "freecadVersion": smoke_payload.get("freecadVersion"),
        "normalRunPassed": True,
        "repeatedRunPassed": True,
        "stepBytesIdentical": step_equal,
        "stepExporterMetadataOnlyVariance": not step_equal and metadata_only,
        "stepSHA256": [a["stepHash"], b["stepHash"]],
        "stepSizeBytes": [len(step_bytes[0]), len(step_bytes[1])],
        "stepHeader": "ISO-10303-21",
        "preparedSourceSHA256": normalized_observed_semantics(observed_a)["workingCopy"]["sha256"],
        "planHash": a["planHash"],
        "jobID": a["jobID"],
        "recordPackageStable": a["recordPackageHash"] == b["recordPackageHash"],
    }


def timeout_proof(ctx, runtime):
    """Prove that run()'s subprocess timeout is bounded and diagnostic,
    independent of machine speed or Go build-cache state: PARAMETRON_CAD_PROOF_MODE=block
    makes the controlled runtime hang forever, so ctx.timeout deterministically
    raises subprocess.TimeoutExpired here rather than depending on real work
    (e.g. `go build`) happening to run slower than the configured timeout."""
    working = ctx.workspace / "timeout-block"
    working.mkdir(parents=True)
    manifest_path = working / "native_manifest.json"
    manifest_path.write_text("{}")
    request_path = working / "parametron.verification.json"
    request_path.write_text("{}")
    traversal_request_path = working / "parametron.reference-traversal-request.json"
    traversal_request_path.write_text(json.dumps({"schemaVersion": "2.0", "externalTargets": []}))
    env = dict(os.environ)
    env["PARAMETRON_CAD_PROOF_MODE"] = "block"
    run(
        [sys.executable, str(runtime), "execute",
         "--working-copy", str(working),
         "--manifest", str(manifest_path),
         "--result", str(working / "result.json"),
         "--output-dir", str(working / "outputs"),
         "--observation-request", str(request_path),
         "--reference-traversal-request", str(traversal_request_path)],
        cwd=working, env=env, timeout=ctx.timeout,
    )


def main():
    parser = argparse.ArgumentParser()
    script_repo = Path(__file__).resolve().parents[1]
    parser.add_argument("--engine-repo", type=Path, default=script_repo)
    parser.add_argument("--freecad-repo", type=Path, default=script_repo.parent / "parametron-freecad")
    parser.add_argument("--workspace", type=Path)
    parser.add_argument("--mode", choices=("fake", "real", "all", "timeout"), default="all")
    parser.add_argument("--report", type=Path)
    parser.add_argument("--keep-workspace", action="store_true")
    parser.add_argument("--timeout", type=int, default=240)
    args = parser.parse_args()

    owned_temp = args.workspace is None
    workspace = args.workspace.resolve() if args.workspace else Path(tempfile.mkdtemp(prefix="parametron-task14-"))
    workspace.mkdir(parents=True, exist_ok=True)
    ctx = Context()
    ctx.engine_repo = args.engine_repo.resolve()
    ctx.freecad_repo = args.freecad_repo.resolve()
    ctx.workspace = workspace
    ctx.timeout = args.timeout
    ctx.fixture = ctx.engine_repo / "testdata/projects/freecad/smoke/minimal-valid-project"
    ctx.source_model = ctx.fixture / "input/box.FCStd"
    report_path = args.report.resolve() if args.report else workspace / "proof-report.json"
    controlled = ctx.engine_repo / "scripts/cad_runtime_proof_runtime.py"
    summary = {"schemaVersion": "1.0", "status": "passed", "scenarios": []}
    try:
        if args.mode == "timeout":
            timeout_proof(ctx, controlled)
            summary["scenarios"] = [{"name": "timeout_block", "status": "unexpected_success"}]
        else:
            require(ctx.source_model.read_bytes()[:2] == b"PK", "FCStd fixture is not a real ZIP archive")
            build_binaries(ctx)
            if args.mode in ("fake", "all"):
                summary["scenarios"] = fake_proof(ctx, controlled)
            if args.mode in ("real", "all"):
                summary["realRuntime"] = real_proof(ctx)
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        verdict = "PASS"
        if summary.get("realRuntime", {}).get("stepExporterMetadataOnlyVariance"):
            verdict = "PASS WITH NOTES"
        print(verdict)
        print(json.dumps(summary, indent=2, sort_keys=True))
        return 0
    except Exception as exc:
        summary["status"] = "failed"
        summary["diagnostics"] = {"message": str(exc)}
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        print("FAIL", file=sys.stderr)
        print(str(exc), file=sys.stderr)
        return 1
    finally:
        if owned_temp and not args.keep_workspace and report_path.is_relative_to(workspace):
            shutil.rmtree(workspace, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
