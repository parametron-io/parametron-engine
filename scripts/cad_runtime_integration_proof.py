#!/usr/bin/env python3
"""Reusable Task 14 aligned CAD runtime integration proof."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
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


def find_one_outside_record_package(root, name):
    # The record package now additionally carries its own copy of this raw
    # CAD attempt evidence file (see recordpackage raw/ layout), so an
    # unfiltered rglob legitimately finds two matches: the original attempt
    # evidence and the package's raw evidence copy. Callers that want the
    # original attempt-produced file exclude the package copy explicitly,
    # matching the existing manifest.json/prm.metadata.json filtering below.
    matches = sorted(path for path in root.rglob(name)
                      if "parametron-record-package" not in path.parts)
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
    reports = sorted(effective_out.rglob("prm.report.json"))
    require(reports, f"{name}: prm.report.json missing")
    return {
        "root": root, "out": effective_out, "log": log, "result": result,
        "reportPath": reports[-1], "report": json_file(reports[-1]),
    }


def stable_package_manifest_digest(path):
    """Digest of the package manifest with only the evidence-dependent
    identityIds elided: those legitimately vary with attempt-root raw evidence
    (see compare_repeated_record_packages). Not a digest of the manifest bytes."""
    manifest = json_file(path)
    for entry in manifest["records"]:
        if entry["family"] in EVIDENCE_DEPENDENT_FAMILIES:
            entry["identityId"] = "<evidence-dependent>"
    return hashlib.sha256(json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def stable_cli_facts(item):
    manifests = [path for path in item["out"].rglob("manifest.json")
                 if "parametron-record-package" not in path.parts]
    require(len(manifests) == 1, f"expected one run artifact manifest, found {len(manifests)}")
    manifest = manifests[0]
    metadata_matches = [path for path in item["out"].rglob("prm.metadata.json")
                        if "parametron-record-package" not in path.parts]
    require(len(metadata_matches) == 1, f"expected one run metadata file, found {len(metadata_matches)}")
    metadata = metadata_matches[0]
    record_manifest = find_one(item["out"], "parametron.record-package.json")
    runtime_manifests = [path for path in item["out"].rglob("prm.export-manifest.json")
                         if "_working" in path.parts]
    require(len(runtime_manifests) == 1, f"expected one attempt manifest, found {len(runtime_manifests)}")
    runtime_manifest = runtime_manifests[0]
    request = find_one_outside_record_package(item["out"], "prm.verification.json")
    result = find_one_outside_record_package(item["out"], "prm.result.json")
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
        "recordPackageStableHash": stable_package_manifest_digest(record_manifest),
        "stepCount": len(steps),
    }


# --- Record-package comparison -------------------------------------------
#
# Equivalent runs with equivalent runtime evidence yield deterministic
# normalized output. Repeated proof runs deliberately execute under different
# attempt roots, so their exact raw observed/verification bytes differ by the
# attempt-root path. Those exact bytes legitimately feed the observation and
# verification records (evidence digests; the established working_copy_path
# observation fact), hence those two identities may differ. Everything else in
# the package must be exactly equal.

OBSERVED_RAW = "raw/observed/prm.observed.json"
VERIFICATION_RAW = "raw/verification/prm.verification.json"
METADATA_RAW = "raw/prm.metadata.json"
EVIDENCE_DEPENDENT_FAMILIES = ("observation", "verification")
ALLOWED_EVIDENCE_REFS = {
    "observation": {OBSERVED_RAW, METADATA_RAW},
    "verification": {VERIFICATION_RAW, OBSERVED_RAW, METADATA_RAW},
}
REQUIRED_EXACT_EVIDENCE = {"observation": OBSERVED_RAW, "verification": VERIFICATION_RAW}
ATTEMPT_ROOT_PLACEHOLDER = "<attempt-root>"
DIGEST_PLACEHOLDER = "<exact-raw-digest>"


def sha256_hex(content):
    return hashlib.sha256(content).hexdigest()


def record_package_snapshot(item):
    """Reads one run's record package into plain data: manifest, exact record
    bytes, exact packaged raw bytes, and the output root."""
    manifest_path = find_one(item["out"], "parametron.record-package.json")
    package = manifest_path.parent
    manifest = json_file(manifest_path)
    records = {entry["contractPath"]: (package / entry["contractPath"]).read_bytes()
               for entry in manifest["records"]}
    raw = {entry["contractPath"]: (package / entry["contractPath"]).read_bytes()
           for entry in manifest.get("rawEvidence", [])}
    # Raw evidence must be the exact accepted source bytes, never rewritten.
    for contract_path, name in ((OBSERVED_RAW, "prm.observed.json"),
                                (VERIFICATION_RAW, "prm.verification.json"),
                                ("raw/runtime/prm.result.json", "prm.result.json")):
        if contract_path in raw:
            source = find_one_outside_record_package(item["out"], name)
            require(source.read_bytes() == raw[contract_path],
                    f"packaged {contract_path} is not the exact source evidence bytes")
    return {"manifest": manifest, "records": records, "raw": raw,
            "outputRoots": sorted({str(item["out"]), str(item["out"].resolve())})}


def evidence_pairs(value):
    """Every (ref, digest) evidence pair beneath a decoded record."""
    pairs = []
    if isinstance(value, dict):
        for ref_key, digest_key in (("sourceRef", "digestSha256"), ("Ref", "DigestSHA256")):
            if ref_key in value and digest_key in value:
                pairs.append((value[ref_key], value[digest_key]))
        for child in value.values():
            pairs.extend(evidence_pairs(child))
    elif isinstance(value, list):
        for child in value:
            pairs.extend(evidence_pairs(child))
    return pairs


def replace_digests(value, raw):
    if isinstance(value, dict):
        out = {}
        for key, child in value.items():
            if key in ("digestSha256", "DigestSHA256") and child and child in {sha256_hex(b) for b in raw.values()}:
                out[key] = DIGEST_PLACEHOLDER
            else:
                out[key] = replace_digests(child, raw)
        return out
    if isinstance(value, list):
        return [replace_digests(child, raw) for child in value]
    return value


def attempt_root_fact(record, snapshot):
    """The single established working_copy_path observation fact; returns
    (fact, path) after proving it is confined to this run's attempt root."""
    facts = [fact for fact in record["observation"]["facts"]
             if fact.get("kind") == "reference" and fact.get("key") == "working_copy_path"]
    require(len(facts) == 1, f"expected exactly one working_copy_path fact, found {len(facts)}")
    fact = facts[0]
    path = fact["subject"]["name"]
    require(fact["value"]["kind"] == "string" and json.loads(fact["value"]["raw"]) == path,
            "working_copy_path fact value does not equal its subject")
    require(path.startswith("/") and any(path.startswith(root + "/") for root in snapshot["outputRoots"]),
            f"working_copy_path {path!r} is not beneath this run's output root")
    parts = path.split("/")
    require("_working" in parts and parts.index("_working") < len(parts) - 1,
            f"working_copy_path {path!r} is not an attempt root beneath _working")
    return fact, path


def validate_record_package_snapshot(snapshot):
    """Positive provenance proof for one package. Returns per-family facts
    used by the repeated comparison."""
    manifest, raw = snapshot["manifest"], snapshot["raw"]
    decoded = {}
    for entry in manifest["records"]:
        record = json.loads(snapshot["records"][entry["contractPath"]])
        require(record["family"] == entry["family"] and record["recordKey"] == entry["recordKey"]
                and record["identity"]["ID"] == entry["identityId"],
                f"manifest entry {entry['contractPath']} disagrees with its record")
        decoded.setdefault(entry["family"], []).append(record)
    facts = {}
    for family in EVIDENCE_DEPENDENT_FAMILIES:
        records = decoded.get(family, [])
        require(len(records) <= 1, f"expected at most one {family} record, found {len(records)}")
        if not records:
            continue
        record = records[0]
        pairs = evidence_pairs(record)
        refs = set()
        for ref, digest in pairs:
            require(ref in ALLOWED_EVIDENCE_REFS[family],
                    f"{family} record has non-canonical evidence ref {ref!r}")
            refs.add(ref)
            if digest:
                require(ref in raw, f"{family} evidence {ref!r} has a digest but no packaged raw file")
                require(digest == sha256_hex(raw[ref]),
                        f"{family} evidence digest for {ref!r} is not SHA-256 of the exact packaged raw bytes")
            else:
                require(ref != REQUIRED_EXACT_EVIDENCE[family] and ref != OBSERVED_RAW,
                        f"{family} evidence {ref!r} lacks its exact raw digest")
        require(REQUIRED_EXACT_EVIDENCE[family] in refs, f"{family} record does not reference its raw evidence")
        facts[family] = {"record": record, "refs": refs}
    if "observation" in facts:
        _, path = attempt_root_fact(facts["observation"]["record"], snapshot)
        facts["attemptRoot"] = path
        for contract_path in (OBSERVED_RAW, VERIFICATION_RAW):
            if contract_path in raw:
                require(path.encode() in raw[contract_path],
                        f"{contract_path} does not carry the attempt root")
    return facts


def _evidence_material(family, facts, snapshot):
    raw = snapshot["raw"]
    material = {"observed": sha256_hex(raw[OBSERVED_RAW]) if OBSERVED_RAW in facts[family]["refs"] else None}
    if family == "observation":
        material["attemptRoot"] = facts["attemptRoot"]
    else:
        material["verification"] = sha256_hex(raw[VERIFICATION_RAW])
    return material


def _normalized_dependent_record(family, facts, snapshot):
    record = json.loads(json.dumps(facts[family]["record"]))
    record["identity"]["ID"] = "<identity>"
    root = facts.get("attemptRoot")
    if family == "observation":
        fact, path = attempt_root_fact(record, snapshot)
        fact["subject"]["name"] = ATTEMPT_ROOT_PLACEHOLDER
        fact["value"]["raw"] = json.dumps(ATTEMPT_ROOT_PLACEHOLDER)
    record = replace_digests(record, snapshot["raw"])
    if root:
        require(root not in json.dumps(record),
                f"attempt root leaks into {family} record beyond the working_copy_path fact")
    return record


def compare_repeated_record_packages(a, b, required_families=("execution", "artifact")):
    """Compares two snapshots of equivalent runs under different attempt roots.
    Returns the list of families whose identity legitimately differs."""
    facts_a, facts_b = validate_record_package_snapshot(a), validate_record_package_snapshot(b)
    ma, mb = a["manifest"], b["manifest"]
    for key in ("schemaVersion", "packageKey", "layoutVersion", "ownership"):
        require(ma.get(key) == mb.get(key), f"record package {key} differs: {ma.get(key)!r} != {mb.get(key)!r}")
    ea, eb = ma["records"], mb["records"]
    require(len(ea) == len(eb), f"record count differs: {len(ea)} != {len(eb)}")
    for x, y in zip(ea, eb):
        for key in ("family", "contractPath", "recordKey"):
            require(x[key] == y[key], f"record entry {key} differs: {x[key]!r} != {y[key]!r}")
    families = {entry["family"] for entry in ea}
    for family in required_families:
        require(family in families, f"required record family {family!r} missing from package")
    varying = []
    for x, y in zip(ea, eb):
        family, path = x["family"], x["contractPath"]
        if family not in EVIDENCE_DEPENDENT_FAMILIES:
            require(x["identityId"] == y["identityId"], f"{family} identity differs at {path}")
            require(a["records"][path] == b["records"][path], f"{family} record bytes differ at {path}")
            continue
        norm_a = _normalized_dependent_record(family, facts_a, a)
        norm_b = _normalized_dependent_record(family, facts_b, b)
        require(norm_a == norm_b, f"{family} normalized semantics differ beyond attempt-root evidence")
        mat_a = _evidence_material(family, facts_a, a)
        mat_b = _evidence_material(family, facts_b, b)
        # The raw evidence itself may differ only by the attempt root.
        for contract_path in (OBSERVED_RAW, VERIFICATION_RAW):
            if contract_path in a["raw"] and contract_path in b["raw"] and "attemptRoot" in facts_a and "attemptRoot" in facts_b:
                ra = a["raw"][contract_path].replace(facts_a["attemptRoot"].encode(), b"<attempt-root>")
                rb = b["raw"][contract_path].replace(facts_b["attemptRoot"].encode(), b"<attempt-root>")
                require(ra == rb, f"{contract_path} differs beyond the attempt root")
        if x["identityId"] != y["identityId"]:
            require(mat_a != mat_b,
                    f"{family} identity differs without differing exact-evidence material")
            varying.append(family)
        else:
            require(a["records"][path] == b["records"][path], f"{family} record bytes differ under equal identity")
    return varying


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


CANONICAL_LIFECYCLE_HASH = "9376277c131ac3f361f05412b3a6b455f8574eabf99f1e02d1fa75d3fca82250"
PARTDESIGN_MUTATIONS_HASH = "7187abe907ca6240bdc7cd07fb5ce7a52cf9192e3ba6d6153c581f18b328057a"
TARGET_SCENARIOS = {
    "hidden-baseline-setup": {
        "actions": (("Pad002", "hide"),),
        "mutations": {"visibility": [{"object": "Pad002", "visible": False}]},
        "observations": {"suppression": [], "visibility": ["Pad002"], "existence": []},
    },
    "combined-success": {
        "actions": (("Fillet", "suppress"), ("Pocket001", "unsuppress"),
                    ("Body003", "hide"), ("Pad002", "unhide"), ("Body002", "delete")),
        "mutations": {
            "suppression": [{"object": "Fillet", "suppressed": True},
                            {"object": "Pocket001", "suppressed": False}],
            "visibility": [{"object": "Body003", "visible": False},
                           {"object": "Pad002", "visible": True}],
            "deletion": [{"object": "Body002"}],
        },
        "observations": {"suppression": ["Fillet", "Pocket001"],
                         "visibility": ["Body003", "Pad002"], "existence": ["Body002"]},
    },
}


def stage_canonical_target_project(ctx, staged, name, source_override=None):
    fixture = ctx.freecad_repo / "tests/fixtures/canonical_lifecycle"
    shutil.copytree(fixture, staged)
    source = staged / "input/cube.FCStd"
    capture = json_file(staged / "parametron.cad.json")
    require(capture["sourceDocument"]["fingerprint"] == "sha256:" + digest(source),
            "canonical fixture capture fingerprint mismatch")
    if source_override is not None:
        shutil.copyfile(source_override, source)
    source_hash = digest(source)
    project = json_file(staged / "parametron.project.json")
    project["projectId"] = "cube-target-" + name
    project.pop("tables", None)
    (staged / "parametron.project.json").write_text(
        json.dumps(project, indent=2) + "\n", encoding="utf-8")
    shutil.rmtree(staged / "tables")
    if source_override is not None:
        capture["sourceDocument"]["fingerprint"] = "sha256:" + source_hash
        (staged / "parametron.cad.json").write_text(
            json.dumps(capture, indent=2) + "\n", encoding="utf-8")
    scenario = TARGET_SCENARIOS[name]
    dsl = ('dsl v1.0\n\nproduct CubeBox {\n'
           '  adapter = "freecad"\n'
           '  source_model = "cube_canonical_lifecycle_model"\n'
           '  outputs = ["none"]\n' +
           ''.join(f'  target {target}: action = {action}\n'
                   for target, action in scenario["actions"]) + '}\n')
    (staged / "cube.project.dsl").write_text(dsl, encoding="utf-8")
    return source_hash


def target_foundation_proof(ctx):
    fixture_relative = Path("tests/fixtures/canonical_lifecycle")
    fixture = ctx.freecad_repo / fixture_relative
    require(ctx.freecad_repo.is_dir(), f"FreeCAD repository missing: {ctx.freecad_repo}")
    require(fixture.is_dir(), f"canonical lifecycle fixture missing: {fixture}")
    required = ("input/cube.FCStd", "parametron.project.json", "parametron.cad.json",
                "parametron.semantic-map.json", "cube.project.dsl")
    for name in required:
        require((fixture / name).is_file(), f"canonical lifecycle file missing: {fixture / name}")
    source = fixture / "input/cube.FCStd"
    source_hash = digest(source)
    require(source_hash == CANONICAL_LIFECYCLE_HASH,
            f"canonical lifecycle fixture hash mismatch: {source_hash}")
    project = json_file(fixture / "parametron.project.json")
    capture = json_file(fixture / "parametron.cad.json")
    require(project.get("dsl") == "cube.project.dsl" and
            project.get("resources", {}).get("models", {}).get(
                "cube_canonical_lifecycle_model") == "input/cube.FCStd",
            "canonical lifecycle project model/DSL mapping changed")
    require(capture.get("sourceDocument") == {
        "logicalId": "cube_canonical_lifecycle_model", "path": "input/cube.FCStd",
        "fingerprint": "sha256:" + source_hash}, "capture source fingerprint mismatch")
    components = capture.get("entities", {}).get("components", [])
    features = capture.get("entities", {}).get("features", [])
    for name, kind, capabilities in (
        ("Fillet", "feature", (True, True, False, False, False)),
        ("Pocket001", "feature", (True, True, False, False, False)),
        ("Body003", "part", (False, False, True, True, False)),
        ("Pad002", "feature", (False, False, True, True, False)),
        ("Body002", "part", (False, False, True, True, True)),
    ):
        entities = features if kind == "feature" else components
        matches = [entry for entry in entities if entry.get("name") == name and
                   entry.get("identitySource", {}).get("nativeRef") == name and
                   entry.get("kind", kind) == kind]
        require(len(matches) == 1, f"capture native identity missing or ambiguous: {name}")
        want = dict(zip(("suppress", "unsuppress", "hide", "unhide", "delete"),
                        capabilities))
        require(matches[0].get("targetability") == want,
                f"capture capability mismatch: {name}")

    runtime = ctx.workspace / "reject-runtime"
    invocation = ctx.workspace / "runtime-invocation"
    runtime.write_text("#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + shlex.quote(str(invocation)) +
                       "\nexit 23\n", encoding="utf-8")
    runtime.chmod(0o700)
    revision = lambda repo: run(["git", "rev-parse", "HEAD"], cwd=repo).stdout.strip()
    result = {"engineRevision": revision(ctx.engine_repo),
              "freecadRevision": revision(ctx.freecad_repo),
              "fixture": str(fixture_relative / "input/cube.FCStd"),
              "fixtureSHA256": source_hash, "scenarios": []}
    for name, scenario in TARGET_SCENARIOS.items():
        root = ctx.workspace / name
        staged = root / "project"
        require(stage_canonical_target_project(ctx, staged, name) == source_hash,
                "staged source drifted")
        cwd = root / "cwd"
        cwd.mkdir()
        output = root / "out"
        command = [str(ctx.parametron), "--project", str(staged), "--out", str(output)]
        before_calls = invocation.read_text() if invocation.exists() else ""
        cli = run(command, cwd=cwd, env={**os.environ,
                  "PARAMETRON_FREECAD_RUNTIME": str(runtime)}, timeout=ctx.timeout,
                  check=False)
        require(cli.returncode != 0, f"{name}: rejecting runtime unexpectedly succeeded")
        require(invocation.exists() and invocation.read_text() != before_calls,
                f"{name}: rejecting runtime was never invoked\n{cli.stdout}\n{cli.stderr}")
        actual_out = output
        manifests = sorted(path for path in actual_out.rglob("prm.export-manifest.json")
                           if "_working" in path.parts)
        requests = sorted(path for path in actual_out.rglob("prm.verification.json")
                          if "_working" in path.parts)
        require(len(manifests) == len(requests) == 1,
                f"{name}: expected one attempt-local manifest and request")
        manifest_path, request_path = manifests[0], requests[0]
        attempt = manifest_path.parent
        require(request_path.parent == attempt and attempt.parent.name == "_working",
                f"{name}: requests are not attempt-local")
        arguments = invocation.read_text()[len(before_calls):].splitlines()
        require("--working-copy" in arguments and
                arguments[arguments.index("--working-copy") + 1] == str(attempt) and
                "--manifest" in arguments and
                arguments[arguments.index("--manifest") + 1] == str(manifest_path) and
                "--observation-request" in arguments and
                arguments[arguments.index("--observation-request") + 1] == str(request_path),
                f"{name}: rejected runtime was not passed the Engine attempt inputs: {arguments}")
        staged_source = attempt / "source/cube.FCStd"
        require(staged_source.is_file() and digest(staged_source) == source_hash,
                f"{name}: Engine working-copy source missing or changed")
        require(staged_source.resolve() != source.resolve() and
                staged_source.resolve() != (staged / "input/cube.FCStd").resolve(),
                f"{name}: runtime source is not an independent working copy")
        manifest, request = json_file(manifest_path), json_file(request_path)
        require(manifest.get("schemaVersion") == request.get("schemaVersion") == "1.0",
                f"{name}: mutation/observation schema changed")
        require(manifest.get("partMutations") == scenario["mutations"],
                f"{name}: Part mutation projection mismatch: {manifest.get('partMutations')}")
        require("assemblyMutations" not in manifest and
                not manifest.get("parameterAssignments") and not manifest.get("outputs"),
                f"{name}: unexpected assembly, parameter, or output intent")
        require(manifest.get("sourceDocument") == "source/cube.FCStd",
                f"{name}: runtime source is not the working-copy source")
        target_state = request.get("observationContext", {}).get("targetState", {})
        expected = {family: [{"destination": "part", "object": target}
                             for target in sorted(names)]
                    for family, names in scenario["observations"].items()}
        require(target_state == expected and request.get("observe", {}).get("targetState") is True,
                f"{name}: target-state observation mismatch: {target_state}")
        require("targetState" not in request.get("expected", {}),
                f"{name}: expected target state leaked into serialized request")
        require(not list(actual_out.rglob("prm.result.json")) and
                not list(actual_out.rglob("prm.observed.json")),
                f"{name}: rejecting runtime produced CAD evidence")
        require(digest(source) == source_hash, f"{name}: authoritative fixture changed")
        report_path = sorted(actual_out.rglob("prm.report.json"))
        report = json_file(report_path[-1]) if report_path else {}
        result["scenarios"].append({
            "name": name, "status": "passed", "stagedProject": str(staged),
            "cliInvocation": command, "cliExit": cli.returncode,
            "planHash": report.get("planHash"),
            "jobIDs": [job.get("jobId") for job in report.get("jobs", [])],
            "manifestPath": str(manifest_path), "manifestSHA256": digest(manifest_path),
            "verificationPath": str(request_path), "verificationSHA256": digest(request_path),
            "workingCopySource": str(staged_source),
            "stoppingPoint": "intentional Stage 1A external runtime rejection (exit 23)",
        })
    require(digest(source) == source_hash, "authoritative fixture changed after proof")
    return result


def stage_unsafe_delete_project(ctx, source_override=None, action="delete"):
    """Stage the PartDesign target through normal Engine authoring.

    BaseSketch is a real Sketcher object in the pinned PartDesign document.
    FreeCAD's deletion consumer supports exact object deletion in principle,
    then rejects this concrete object because its live InList has dependents.
    The capture grants authoring of that operation, not preclearance of safety.
    The unhide variant uses a real FreeCAD-prepared derivative and its own fingerprint.
    """
    fixture_relative = Path("tests/fixtures/partdesign_mutations/partdesign-mutations.FCStd")
    source = ctx.freecad_repo / fixture_relative
    require(ctx.freecad_repo.is_dir() and source.is_file(),
            f"FreeCAD PartDesign fixture missing: {source}")
    source_hash = digest(source)
    require(source_hash == PARTDESIGN_MUTATIONS_HASH,
            f"PartDesign fixture provenance mismatch: {source_hash}")
    semantic_map_source = (ctx.freecad_repo /
                           "tests/fixtures/canonical_lifecycle/parametron.semantic-map.json")
    require(semantic_map_source.is_file(), "FreeCAD semantic map foundation missing")

    require(action in ("delete", "unhide"), f"unsupported PartDesign rehearsal action: {action}")
    staged = ctx.workspace / ("native-validity-foundation" if action == "unhide"
                              else "unsafe-delete-foundation") / "project"
    (staged / "input").mkdir(parents=True)
    shutil.copyfile(source_override or source, staged / "input/partdesign-mutations.FCStd")
    staged_hash = digest(staged / "input/partdesign-mutations.FCStd")
    shutil.copyfile(semantic_map_source, staged / "parametron.semantic-map.json")
    require(staged_hash == digest(source_override or source),
            "staged PartDesign source drifted")
    dsl_name = "native-validity.project.dsl" if action == "unhide" else "unsafe-delete.project.dsl"
    project = {
        "version": "1.0", "projectId": "partdesign-" + ("native-validity" if action == "unhide"
                                                        else "unsafe-delete") + "-foundation",
        "dsl": dsl_name,
        "resources": {"models": {"partdesign_mutations_model":
                                 "input/partdesign-mutations.FCStd"}},
    }
    (staged / "parametron.project.json").write_text(
        json.dumps(project, indent=2) + "\n", encoding="utf-8")
    (staged / dsl_name).write_text(
        'dsl v1.0\n\nproduct PartDesign {\n'
        '  adapter = "freecad"\n'
        '  source_model = "partdesign_mutations_model"\n'
        '  outputs = ["none"]\n'
        f'  target BaseSketch: action = {action}\n}}\n', encoding="utf-8")

    no_actions = dict.fromkeys(("suppress", "unsuppress", "hide", "unhide", "delete"), False)
    annotations = {"description": "", "comment": "", "purpose": ""}
    root = {
        "id": "cmp.root", "kind": "assembly", "name": "partdesign_mutations",
        "displayName": "partdesign-mutations", "cadType": "App::Document",
        "quantity": 1, "material": "", "identitySource": {
            "kind": "parent_scoped_path", "path": "cmp.root",
            "nativeRef": "partdesign_mutations"},
        "stabilityClass": "stable", "targetability": no_actions,
        "annotations": annotations,
    }
    body = {
        "id": "cmp.mutationBody", "kind": "part", "name": "MutationBody",
        "displayName": "MutationBody", "cadType": "PartDesign::Body",
        "quantity": 1, "material": "", "identitySource": {
            "kind": "parent_scoped_path", "path": "cmp.root/MutationBody",
            "nativeRef": "MutationBody"},
        "stabilityClass": "stable", "targetability": no_actions,
        "annotations": annotations,
    }
    sketch = {
        "id": "fea.mutationBody.baseSketch", "componentId": "cmp.mutationBody",
        "name": "BaseSketch", "displayName": "BaseSketch",
        "cadType": "Sketcher::SketchObject", "identitySource": {
            "kind": "owner_scoped_key", "ownerScopedKey": "BaseSketch",
            "nativeRef": "BaseSketch"},
        "stabilityClass": "conditionally_stable",
        "targetability": {**no_actions, "delete": True,
                          "hide": action == "unhide", "unhide": action == "unhide"},
        "annotations": {
            "description": "Native BaseSketch owned by MutationBody",
            "comment": ("Temporary EmptyBody makes post-mutation native validity fail"
                        if action == "unhide" else
                        "IntermediatePad depends on this sketch; FreeCAD must reject deletion in this source state"),
            "purpose": ("Author supported visibility intent before native validity inspection"
                        if action == "unhide" else
                        "Author supported delete intent and defer live dependency safety to FreeCAD"),
        },
    }
    capture = {
        "schemaVersion": "1.0", "captureId": "cap.freecad.partdesign-" +
        ("unsafe-delete" if action == "delete" else "native-validity") + ".v1",
        "adapter": {"name": "freecad", "version": ""},
        "cadSystem": {"name": "FreeCAD", "version": "1.1.1"},
        "sourceDocument": {"logicalId": "partdesign_mutations_model",
                           "path": "input/partdesign-mutations.FCStd",
                           "fingerprint": "sha256:" + staged_hash},
        "rootProduct": {"id": "cmp.root"},
        "annotations": {
            "description": ("Focused PartDesign visibility authoring surface" if action == "unhide"
                            else "Focused PartDesign deletion authoring surface"),
            "comment": ("Pinned to a temporary FreeCAD-prepared derivative" if action == "unhide"
                        else "Pinned to the FreeCAD-owned native fixture and its proven BaseSketch dependency"),
            "purpose": ("Rehearse Engine visibility authoring before native validity inspection"
                        if action == "unhide" else
                        "Rehearse Engine delete authoring before native safety rejection")},
        "entities": {"components": [root, body], "features": [sketch],
                     "relationships": [], "parameterGroups": [], "parameters": [],
                     "metadata": []},
        "structure": {"rootComponentId": "cmp.root", "nodes": [
            {"componentId": "cmp.root", "parentComponentId": "",
             "children": ["cmp.mutationBody"]},
            {"componentId": "cmp.mutationBody", "parentComponentId": "cmp.root",
             "children": []}]},
    }
    (staged / "parametron.cad.json").write_text(
        json.dumps(capture, indent=2) + "\n", encoding="utf-8")
    return staged, source_override or source, staged_hash


def unsafe_delete_foundation_proof(ctx):
    staged, source, source_hash = stage_unsafe_delete_project(ctx)
    fixture_relative = Path("tests/fixtures/partdesign_mutations/partdesign-mutations.FCStd")

    runtime = ctx.workspace / "unsafe-delete-reject-runtime"
    invocation = ctx.workspace / "unsafe-delete-runtime-invocation"
    runtime.write_text("#!/bin/sh\nprintf '%s\\n' \"$@\" > " +
                       shlex.quote(str(invocation)) + "\nexit 23\n", encoding="utf-8")
    runtime.chmod(0o700)
    output = ctx.workspace / "unsafe-delete-foundation" / "out"
    command = [str(ctx.parametron), "--project", str(staged), "--out", str(output)]
    cli = run(command, cwd=staged.parent,
              env={**os.environ, "PARAMETRON_FREECAD_RUNTIME": str(runtime)},
              timeout=ctx.timeout, check=False)
    require(cli.returncode != 0 and invocation.is_file(),
            f"unsafe-delete: normal CLI did not reach rejecting runtime\n{cli.stdout}\n{cli.stderr}")
    manifests = sorted(path for path in output.rglob("prm.export-manifest.json")
                       if "_working" in path.parts)
    requests = sorted(path for path in output.rglob("prm.verification.json")
                      if "_working" in path.parts)
    require(len(manifests) == len(requests) == 1,
            "unsafe-delete: expected one Engine attempt manifest and request")
    manifest_path, request_path = manifests[0], requests[0]
    attempt = manifest_path.parent
    require(attempt.parent.name == "_working" and request_path.parent == attempt,
            "unsafe-delete: request paths are not attempt-local")
    args = invocation.read_text().splitlines()
    for flag, value in (("--working-copy", attempt), ("--manifest", manifest_path),
                        ("--observation-request", request_path)):
        require(flag in args and args[args.index(flag) + 1] == str(value),
                f"unsafe-delete: runtime was not passed Engine's {flag} path")
    working_source = attempt / "source/partdesign-mutations.FCStd"
    require(working_source.is_file() and digest(working_source) == source_hash and
            working_source.resolve() != source.resolve() and
            working_source.resolve() != (staged / "input/partdesign-mutations.FCStd").resolve(),
            "unsafe-delete: Engine working-copy source is missing or not isolated")
    manifest, request = json_file(manifest_path), json_file(request_path)
    require(manifest.get("schemaVersion") == request.get("schemaVersion") == "1.0",
            "unsafe-delete: request schema is not 1.0")
    require(manifest.get("sourceDocument") == "source/partdesign-mutations.FCStd" and
            manifest.get("partMutations") == {"deletion": [{"object": "BaseSketch"}]} and
            "assemblyMutations" not in manifest and
            not manifest.get("parameterAssignments") and not manifest.get("outputs"),
            f"unsafe-delete: unexpected manifest projection: {manifest}")
    require(request.get("observe", {}).get("targetState") is True and
            request.get("observationContext", {}).get("targetState") == {
                "suppression": [], "visibility": [], "existence": [
                    {"destination": "part", "object": "BaseSketch"}]} and
            "targetState" not in request.get("expected", {}),
            f"unsafe-delete: unexpected observation request: {request}")
    require(not list(output.rglob("prm.result.json")) and
            not list(output.rglob("prm.observed.json")),
            "unsafe-delete: rejecting runtime fabricated CAD evidence")
    require(digest(source) == source_hash, "FreeCAD PartDesign fixture changed")
    reports = sorted(output.rglob("prm.report.json"))
    report = json_file(reports[-1]) if reports else {}
    revision = lambda repo: run(["git", "rev-parse", "HEAD"], cwd=repo).stdout.strip()
    return {"engineRevision": revision(ctx.engine_repo),
            "freecadRevision": revision(ctx.freecad_repo),
            "fixture": str(fixture_relative), "fixtureSHA256": source_hash,
            "scenario": {"name": "unsafe-delete-foundation", "status": "passed",
                         "stagedProject": str(staged), "cliInvocation": command,
                         "cliExit": cli.returncode, "planHash": report.get("planHash"),
                         "jobIDs": [job.get("jobId") for job in report.get("jobs", [])],
                         "manifestPath": str(manifest_path),
                         "manifestSHA256": digest(manifest_path),
                         "verificationPath": str(request_path),
                         "verificationSHA256": digest(request_path),
                         "workingCopySource": str(working_source),
                         "stoppingPoint": "intentional Stage 1B external runtime rejection (exit 23)"}}


def inspect_persisted_native(ctx, source, name):
    """Reopen an attempt document with real FreeCAD, without saving it."""
    script = ctx.workspace / "inspect-persisted-native.py"
    if not script.exists():
        script.write_text('''import json
import sys
from pathlib import Path
import FreeCAD
from parametron_freecad.execution.post_mutation_validity import (
    PostMutationValidityError, inspect_document_post_mutation_validity)

request = json.loads(Path(next(a[7:] for a in sys.argv if a.startswith("--pass="))).read_text())
document = FreeCAD.openDocument(request["source"])
try:
    facts = {}
    for name in ("Fillet", "Pocket001", "Body003", "Pad002", "Body002", "BaseSketch", "IntermediatePad", "MutationBody", "EmptyBody"):
        item = document.getObject(name)
        facts[name] = None if item is None else {
            "name": item.Name, "typeId": item.TypeId,
            "suppressed": item.Suppressed if "Suppressed" in item.PropertiesList else None,
            "visible": item.Visibility if "Visibility" in item.PropertiesList else None,
            "visibilityType": item.getTypeIdOfProperty("Visibility") if "Visibility" in item.PropertiesList else None,
            "dependents": sorted(obj.Name for obj in item.InList),
        }
    facts["bodyValidity"] = {
        obj.Name: {"null": obj.Shape.isNull(),
                   "valid": not obj.Shape.isNull() and obj.Shape.isValid()}
        for obj in document.Objects if obj.TypeId == "PartDesign::Body"
    }
    try:
        inspect_document_post_mutation_validity(document)
        facts["validityError"] = None
    except PostMutationValidityError as exc:
        facts["validityError"] = {"type": type(exc).__name__, "message": str(exc)}
    Path(request["output"]).write_text(json.dumps(facts, sort_keys=True))
finally:
    FreeCAD.closeDocument(document.Name)
''', encoding="utf-8")
    request = ctx.workspace / (name + "-native-inspection-request.json")
    output = ctx.workspace / (name + "-native-inspection.json")
    request.write_text(json.dumps({"source": str(source), "output": str(output)}), encoding="utf-8")
    run(["nix", "develop", "--command", "freecadcmd", "-P", ctx.freecad_repo,
         script, "--pass=" + str(request)], cwd=ctx.engine_repo, timeout=ctx.timeout)
    require(output.is_file(), f"read-only FreeCAD inspection did not return: {name}")
    return json_file(output)


def real_target_run(ctx, name, staged, runtime, *, expect_success):
    root = ctx.workspace / name
    root.mkdir(exist_ok=True)
    output = root / "out"
    command = [str(ctx.parametron), "--project", str(staged), "--out", str(output)]
    cli = run(command, cwd=root, env={**os.environ, "PARAMETRON_FREECAD_RUNTIME": str(runtime)},
              timeout=ctx.timeout, check=False)
    require((cli.returncode == 0) == expect_success,
            f"{name}: unexpected real CLI exit {cli.returncode}\n{cli.stdout}\n{cli.stderr}")
    report_path = find_one_outside_record_package(output, "prm.report.json")
    report = json_file(report_path)
    require(report["status"] == ("success" if expect_success else "failed"),
            f"{name}: unexpected Engine report status: {report['status']}")
    attempts = sorted((path.parent for path in output.rglob("prm.export-manifest.json")
                       if "_working" in path.parts),
                      key=lambda path: int(path.name.rsplit("-attempt-", 1)[1]))
    require(len(attempts) == report["jobs"][0]["steps"][-1]["attempts"],
            f"{name}: missing Engine attempt working copies")
    attempt = attempts[-1]
    require(attempt.parent.name == "_working" and attempt.is_dir(),
            f"{name}: working copy is not Engine-owned")
    manifest = attempt / "prm.export-manifest.json"
    request = attempt / "prm.verification.json"
    result = attempt / "prm.result.json"
    observed = attempt / "outputs/prm.observed.json"
    source = attempt / "source" / ("partdesign-mutations.FCStd" if name in
                                   ("unsafe-delete-real", "native-validity-real") else "cube.FCStd")
    require(all(path.is_file() for path in (manifest, request, result, source)),
            f"{name}: Engine request or real runtime result missing")
    require(json_file(manifest)["schemaVersion"] == json_file(request)["schemaVersion"] ==
            json_file(result)["schemaVersion"] == "1.0", f"{name}: schema mismatch")
    require(json_file(manifest)["sourceDocument"] == "source/" + source.name,
            f"{name}: runtime did not use attempt source")
    require(digest(staged / "input" / source.name) == json_file(request)["expected"]["metadata"][0]["value"],
            f"{name}: request source fingerprint disagrees with staged source")
    require(json_file(result)["status"] == ("succeeded" if expect_success else "failed"),
            f"{name}: real result status mismatch")
    if expect_success:
        require(json_file(result).get("artifacts") == [],
                f"{name}: native-only runtime returned derived artifacts")
        require(observed.is_file() and json_file(observed)["schemaVersion"] == "1.0",
                f"{name}: real observation missing")
        require(json_file(observed)["workingCopy"]["path"] == str(attempt),
                f"{name}: real observation attempt mismatch")
    else:
        require(not observed.exists(), f"{name}: failed native execution emitted observation")
    package_path = find_one(output, "parametron.record-package.json")
    return {"name": name, "root": root, "out": output, "stagedProject": staged,
            "command": command, "report": report, "reportPath": report_path,
            "attempt": attempt, "attempts": attempts, "manifest": manifest,
            "request": request, "result": result, "observed": observed,
            "source": source, "package": package_path}


def real_target_facts(run_item, native):
    package = json_file(run_item["package"])
    verification = run_item["package"].parent / "records/parametron.verification-record.json"
    return {"name": run_item["name"], "cliInvocation": run_item["command"],
            "stagedProject": str(run_item["stagedProject"]),
            "planHash": run_item["report"]["planHash"],
            "jobID": run_item["report"]["jobs"][0]["jobId"],
            "attemptIDs": [path.name for path in run_item["attempts"]],
            "attemptPath": str(run_item["attempt"]),
            "manifestPath": str(run_item["manifest"]), "manifestSHA256": digest(run_item["manifest"]),
            "verificationPath": str(run_item["request"]), "verificationSHA256": digest(run_item["request"]),
            "resultPath": str(run_item["result"]), "resultSHA256": digest(run_item["result"]),
            "observedPath": str(run_item["observed"]) if run_item["observed"].exists() else None,
            "observedSHA256": digest(run_item["observed"]) if run_item["observed"].exists() else None,
            "engineStatus": run_item["report"]["status"],
            "recordPackage": str(run_item["package"].parent),
            "recordFamilies": [entry["family"] for entry in package["records"]],
            "rawEvidencePaths": [entry["contractPath"] for entry in package["rawEvidence"]],
            "verificationStatus": json_file(verification)["verification"]["outcome"]
                                  if verification.exists() else None,
            "persistedSource": str(run_item["source"]),
            "persistedSHA256": digest(run_item["source"]),
            "reopenedNativeState": native}


def target_mutations_real_proof(ctx):
    canonical = ctx.freecad_repo / "tests/fixtures/canonical_lifecycle/input/cube.FCStd"
    unsafe = ctx.freecad_repo / "tests/fixtures/partdesign_mutations/partdesign-mutations.FCStd"
    require(canonical.is_file() and digest(canonical) == CANONICAL_LIFECYCLE_HASH,
            "canonical fixture provenance mismatch")
    require(unsafe.is_file() and digest(unsafe) == PARTDESIGN_MUTATIONS_HASH,
            "PartDesign fixture provenance mismatch")
    built = run(["nix", "build", "--no-link", "--print-out-paths",
                 str(ctx.freecad_repo) + "#parametron-freecad"],
                cwd=ctx.engine_repo, timeout=ctx.timeout)
    runtime = Path(built.stdout.strip().splitlines()[-1]) / "bin/parametron-freecad"
    require(runtime.is_file() and os.access(runtime, os.X_OK), "real FreeCAD wrapper unavailable")
    smoke = json.loads(run([runtime, "smoke"], cwd=ctx.workspace, timeout=ctx.timeout).stdout)
    require(smoke.get("status") == "ok" and smoke.get("host") == "freecadcmd" and
            smoke.get("freecadVersion", [])[:3] == ["1", "1", "1"],
            f"real FreeCAD smoke failed: {smoke}")
    revision = lambda repo: run(["git", "rev-parse", "HEAD"], cwd=repo).stdout.strip()
    provenance = {"engineRevision": revision(ctx.engine_repo),
                  "freecadRevision": revision(ctx.freecad_repo),
                  "runtime": str(runtime), "freecadVersion": smoke["freecadVersion"],
                  "canonicalFixture": str(canonical), "canonicalSHA256": digest(canonical),
                  "unsafeFixture": str(unsafe), "unsafeSHA256": digest(unsafe)}

    setup_project = ctx.workspace / "hidden-setup-project"
    require(stage_canonical_target_project(ctx, setup_project, "hidden-baseline-setup") ==
            CANONICAL_LIFECYCLE_HASH, "setup staged source drifted")
    setup = real_target_run(ctx, "hidden-baseline-real", setup_project, runtime, expect_success=True)
    require(json_file(setup["manifest"]).get("partMutations") ==
            TARGET_SCENARIOS["hidden-baseline-setup"]["mutations"] and
            json_file(setup["request"])["observationContext"]["targetState"] == {
                "suppression": [], "visibility": [{"destination": "part", "object": "Pad002"}],
                "existence": []}, "real hidden-baseline request drifted")
    setup_state = json_file(setup["observed"])["observation"]["targetState"]
    require(setup_state == {"suppression": [], "visibility": [
        {"destination": "part", "object": "Pad002", "status": "observed", "value": False}],
        "existence": []}, f"real hide observation mismatch: {setup_state}")
    setup_native = inspect_persisted_native(ctx, setup["source"], "hidden-setup")
    require(setup_native["Pad002"]["visible"] is False and
            setup_native["Body002"] is not None and
            all(fact["valid"] for fact in setup_native["bodyValidity"].values()),
            "persisted hidden baseline did not reopen as healthy hidden source")
    setup_package = record_package_snapshot(setup)
    setup_facts = validate_record_package_snapshot(setup_package)
    require(setup_facts["verification"]["record"]["verification"]["outcome"] == "pass",
            "Engine did not verify real hidden-baseline evidence")
    hidden_hash = digest(setup["source"])
    require(hidden_hash != CANONICAL_LIFECYCLE_HASH,
            "real setup did not persist a distinct hidden native document")

    # Two independent copies of the exact hidden bytes enter the same project
    # path sequentially, preserving Engine plan identity while keeping each
    # CLI execution and attempt working copy independent.
    combined = []
    for index in (1, 2):
        copy_project = ctx.workspace / f"combined-copy-{index}"
        require(stage_canonical_target_project(ctx, copy_project, "combined-success",
                                               setup["source"]) == hidden_hash,
                "combined source is not the proven hidden derivative")
        active = ctx.workspace / "combined-active-project"
        if active.exists():
            shutil.rmtree(active)
        shutil.copytree(copy_project, active)
        require(digest(active / "input/cube.FCStd") == hidden_hash and
                json_file(active / "parametron.cad.json")["sourceDocument"]["fingerprint"] ==
                "sha256:" + hidden_hash, "derived capture provenance mismatch")
        item = real_target_run(ctx, f"combined-real-{index}", active, runtime,
                               expect_success=True)
        require(json_file(item["manifest"]).get("partMutations") ==
                TARGET_SCENARIOS["combined-success"]["mutations"] and
                "assemblyMutations" not in json_file(item["manifest"]) and
                not json_file(item["manifest"]).get("parameterAssignments") and
                not json_file(item["manifest"]).get("outputs") and
                not list(item["out"].rglob("*.step")),
                "combined real mutation manifest drifted")
        requested = {family: [{"destination": "part", "object": target}
                              for target in sorted(names)]
                     for family, names in TARGET_SCENARIOS["combined-success"]["observations"].items()}
        require(json_file(item["request"])["observationContext"]["targetState"] == requested,
                "combined real observation request identities drifted")
        observed = json_file(item["observed"])["observation"]["targetState"]
        expected = {"suppression": [
            {"destination": "part", "object": "Fillet", "status": "observed", "value": True},
            {"destination": "part", "object": "Pocket001", "status": "observed", "value": False}],
            "visibility": [
                {"destination": "part", "object": "Body003", "status": "observed", "value": False},
                {"destination": "part", "object": "Pad002", "status": "observed", "value": True}],
            "existence": [{"destination": "part", "object": "Body002", "status": "absent"}]}
        require(observed == expected, f"combined real observation mismatch: {observed}")
        native = inspect_persisted_native(ctx, item["source"], f"combined-{index}")
        require(native["Fillet"]["suppressed"] is True and
                native["Pocket001"]["suppressed"] is False and
                native["Body003"]["visible"] is False and
                native["Pad002"]["visible"] is True and
                native["Body002"] is None and
                all(fact["valid"] for fact in native["bodyValidity"].values()),
                f"combined persisted native state mismatch: {native}")
        snapshot = record_package_snapshot(item)
        facts = validate_record_package_snapshot(snapshot)
        families = {entry["family"] for entry in snapshot["manifest"]["records"]}
        require({"execution", "observation", "reference", "verification"} <= families and
                "failure" not in families and
                facts["verification"]["record"]["verification"]["outcome"] == "pass" and
                next(category for category in facts["verification"]["record"]["verification"]["categories"]
                     if category["category"] == "target_state")["outcome"] == "pass",
                "Engine did not verify or normalize the real combined state")
        observed_facts = [fact for fact in facts["observation"]["record"]["observation"]["facts"]
                          if fact["kind"] == "target_state"]
        require(len(observed_facts) == 5, "normalized observation lost target states")
        for raw_path in (OBSERVED_RAW, VERIFICATION_RAW, "raw/runtime/prm.result.json"):
            require(raw_path in snapshot["raw"], f"combined package missing {raw_path}")
        combined.append((item, native, snapshot))
    first, second = combined[0][0], combined[1][0]
    require(first["report"]["planHash"] == second["report"]["planHash"] and
            first["report"]["jobs"][0]["jobId"] == second["report"]["jobs"][0]["jobId"] and
            digest(first["manifest"]) == digest(second["manifest"]) and
            digest(first["result"]) == digest(second["result"]) and
            normalized_json_digest(first["request"]) == normalized_json_digest(second["request"]) and
            normalized_observed_semantics(first["observed"]) ==
            normalized_observed_semantics(second["observed"]) and
            combined[0][1] == combined[1][1], "combined real repeatability mismatch")
    package_variance = compare_repeated_record_packages(
        combined[0][2], combined[1][2],
        required_families=("execution", "observation", "reference", "verification"))

    unsafe_project, unsafe_source, unsafe_hash = stage_unsafe_delete_project(ctx)
    failed = real_target_run(ctx, "unsafe-delete-real", unsafe_project, runtime,
                             expect_success=False)
    native_failure = json_file(failed["result"])["failure"]
    require(json_file(failed["manifest"]).get("partMutations") ==
            {"deletion": [{"object": "BaseSketch"}]} and
            json_file(failed["request"])["observationContext"]["targetState"] == {
                "suppression": [], "visibility": [], "existence": [
                    {"destination": "part", "object": "BaseSketch"}]} and
            failed["report"]["error"]["classification"] == "runtime_failure" and
            failed["report"]["error"]["stage"] == "deletion",
            "Engine unsafe-delete request or failure classification drifted")
    require(native_failure["boundary"] == "execution_entrypoint" and
            native_failure["category"] == "execution" and
            native_failure["code"] == "runtime_failure" and
            native_failure["stage"] == "deletion" and
            "BaseSketch" in native_failure["message"] and
            "IntermediatePad" in native_failure["message"],
            f"real native deletion rejection changed: {native_failure}")
    failed_native = inspect_persisted_native(ctx, failed["source"], "unsafe-delete")
    require(failed_native["BaseSketch"] is not None and
            "IntermediatePad" in failed_native["BaseSketch"]["dependents"] and
            all(fact["valid"] for fact in failed_native["bodyValidity"].values()) and
            digest(failed["source"]) == unsafe_hash == digest(unsafe_source),
            "unsafe deletion altered the native working copy")
    failed_package = failed["package"].parent
    failed_manifest = json_file(failed["package"])
    failed_families = [entry["family"] for entry in failed_manifest["records"]]
    require("failure" in failed_families and
            "observation" not in failed_families and "verification" not in failed_families and
            any(entry["contractPath"] == "raw/runtime/prm.result.json"
                for entry in failed_manifest["rawEvidence"]),
            f"Engine failed-run package missing native failure: {failed_families}")
    failure_record = json_file(failed_package / "records/parametron.failure-record.json")
    packaged_result = failed_package / "raw/runtime/prm.result.json"
    require(packaged_result.read_bytes() == failed["result"].read_bytes() and
            failure_record["failure"]["class"] == "runtime" and
            failure_record["failure"]["code"] == "runtime_failure" and
            any(e["sourceRef"] == "raw/runtime/prm.result.json" and
                e["digestSha256"] == digest(packaged_result)
                for e in failure_record["failure"]["evidence"]),
            "Engine did not normalize/provenance-link real native failure")
    require(digest(canonical) == CANONICAL_LIFECYCLE_HASH and
            digest(unsafe) == PARTDESIGN_MUTATIONS_HASH,
            "authoritative FreeCAD fixtures changed during real proof")
    return {**provenance, "hiddenBaselineSHA256": hidden_hash,
            "hiddenBaselineSourceAttempt": str(setup["attempt"]),
            "combinedStagedCopies": [str(ctx.workspace / f"combined-copy-{index}")
                                     for index in (1, 2)],
            "scenarios": [real_target_facts(setup, setup_native)] +
                         [real_target_facts(item, native) for item, native, _ in combined] +
                         [real_target_facts(failed, failed_native)],
            "repeatability": {"planAndJobStable": True, "manifestAndResultBytesStable": True,
                              "requestAndObservationSemanticsStable": True,
                              "nativeReopenSemanticsStable": True,
                              "recordPackageEvidenceIdentityVariance": package_variance},
            "unsafeFailure": native_failure}


def prepare_native_validity_derivative(ctx, source):
    """Use real FreeCAD to add only the invalid precondition to a temporary copy."""
    root = ctx.workspace / "native-validity-precondition"
    root.mkdir(parents=True)
    derivative = root / source.name
    shutil.copyfile(source, derivative)
    helper = root / "prepare-empty-body.py"
    helper.write_text('''import json
import sys
from pathlib import Path
import FreeCAD

request = json.loads(Path(next(a[7:] for a in sys.argv if a.startswith("--pass="))).read_text())
document = FreeCAD.openDocument(request["source"])
try:
    assert document.getObject("EmptyBody") is None
    document.addObject("PartDesign::Body", "EmptyBody")
    document.recompute()
    document.save()
    Path(request["output"]).write_text(json.dumps({"freecadVersion": FreeCAD.Version()}))
finally:
    FreeCAD.closeDocument(document.Name)
''', encoding="utf-8")
    request = root / "preparation-request.json"
    output = root / "preparation-result.json"
    request.write_text(json.dumps({"source": str(derivative), "output": str(output)}), encoding="utf-8")
    command = ["nix", "develop", "--command", "freecadcmd", "-P", ctx.freecad_repo,
               helper, "--pass=" + str(request)]
    run(command, cwd=ctx.engine_repo, timeout=ctx.timeout)
    require(output.is_file(), "real FreeCAD derivative preparation produced no result")
    return derivative, command, json_file(output)


def native_validity_real_proof(ctx):
    source = ctx.freecad_repo / "tests/fixtures/partdesign_mutations/partdesign-mutations.FCStd"
    require(source.is_file() and digest(source) == PARTDESIGN_MUTATIONS_HASH,
            "authoritative PartDesign fixture provenance mismatch")
    built = run(["nix", "build", "--no-link", "--print-out-paths",
                 str(ctx.freecad_repo) + "#parametron-freecad"],
                cwd=ctx.engine_repo, timeout=ctx.timeout)
    runtime = Path(built.stdout.strip().splitlines()[-1]) / "bin/parametron-freecad"
    require(runtime.is_file() and os.access(runtime, os.X_OK), "real FreeCAD wrapper unavailable")
    smoke = json.loads(run([runtime, "smoke"], cwd=ctx.workspace, timeout=ctx.timeout).stdout)
    require(smoke.get("status") == "ok" and smoke.get("host") == "freecadcmd",
            f"real FreeCAD smoke failed: {smoke}")
    derivative, preparation_command, preparation = prepare_native_validity_derivative(ctx, source)
    derivative_hash = digest(derivative)
    before = inspect_persisted_native(ctx, derivative, "native-validity-before")
    require(before["EmptyBody"] is not None and
            before["EmptyBody"]["typeId"] == "PartDesign::Body" and
            before["bodyValidity"]["EmptyBody"]["null"] is True and
            before["validityError"]["type"] == "InvalidNativeCadStateError" and
            "EmptyBody" in before["validityError"]["message"] and
            before["BaseSketch"] is not None and
            before["BaseSketch"]["visible"] is False and
            before["BaseSketch"]["visibilityType"] == "App::PropertyBool",
            f"native validity precondition differs from permanent fixture proof: {before}")
    staged, staged_source, staged_hash = stage_unsafe_delete_project(
        ctx, source_override=derivative, action="unhide")
    require(staged_hash == derivative_hash and
            json_file(staged / "parametron.cad.json")["sourceDocument"]["fingerprint"] ==
            "sha256:" + derivative_hash, "derived capture fingerprint mismatch")
    failed = real_target_run(ctx, "native-validity-real", staged, runtime, expect_success=False)
    manifest = json_file(failed["manifest"])
    verification = json_file(failed["request"])
    require(manifest.get("partMutations") ==
            {"visibility": [{"object": "BaseSketch", "visible": True}]} and
            "assemblyMutations" not in manifest and
            not manifest.get("parameterAssignments") and
            not manifest.get("outputs") and
            verification["observationContext"]["targetState"] == {
                "suppression": [], "visibility": [{"destination": "part", "object": "BaseSketch"}],
                "existence": []}, "Engine-authored native validity request drifted")
    result = json_file(failed["result"])
    failure = result["failure"]
    require(failure["boundary"] == "execution_entrypoint" and
            failure["category"] == "execution" and
            failure["code"] == "runtime_failure" and
            failure["stage"] == "post_mutation_validity" and
            "EmptyBody" in failure["message"] and
            failed["report"]["error"]["classification"] == "runtime_failure" and
            failed["report"]["error"]["stage"] == "post_mutation_validity",
            f"real native validity failure was not preserved: {failure}")
    after = inspect_persisted_native(ctx, failed["source"], "native-validity-after")
    require(after["EmptyBody"] is not None and
            after["BaseSketch"] is not None and
            after["BaseSketch"]["visible"] == before["BaseSketch"]["visible"] and
            after["validityError"]["type"] == "InvalidNativeCadStateError" and
            "EmptyBody" in after["validityError"]["message"],
            "failed visibility mutation was persisted or native precondition disappeared")
    package = failed["package"].parent
    package_manifest = json_file(failed["package"])
    families = [item["family"] for item in package_manifest["records"]]
    raw = package / "raw/runtime/prm.result.json"
    record = package / "records/parametron.failure-record.json"
    require("failure" in families and "observation" not in families and
            "verification" not in families and raw.is_file() and record.is_file() and
            raw.read_bytes() == failed["result"].read_bytes() and
            any(item["contractPath"] == "raw/runtime/prm.result.json"
                for item in package_manifest["rawEvidence"]),
            "Engine failed-run package lacks exact raw validity evidence")
    normalized = json_file(record)["failure"]
    require(normalized["class"] == "runtime" and normalized["code"] == "runtime_failure" and
            any(item["sourceRef"] == "raw/runtime/prm.result.json" and
                item["digestSha256"] == digest(raw) for item in normalized["evidence"]),
            "Engine normalized failure lost its raw-result provenance")
    require(digest(source) == PARTDESIGN_MUTATIONS_HASH and digest(staged_source) == derivative_hash,
            "authoritative or derived source drifted during real rehearsal")
    revision = lambda repo: run(["git", "rev-parse", "HEAD"], cwd=repo).stdout.strip()
    return {"engineRevision": revision(ctx.engine_repo),
            "freecadRevision": revision(ctx.freecad_repo), "runtime": str(runtime),
            "freecadVersion": smoke["freecadVersion"], "smoke": smoke,
            "authoritativeSource": str(source), "authoritativeSHA256": digest(source),
            "derivedSource": str(derivative), "derivedSHA256": derivative_hash,
            "preparationCommand": list(map(str, preparation_command)),
            "preparationFreeCADVersion": preparation["freecadVersion"],
            "stagedProject": str(staged), "stagedSHA256": staged_hash,
            "scenario": real_target_facts(failed, after),
            "requestMutation": manifest["partMutations"],
            "observationIdentity": verification["observationContext"]["targetState"],
            "nativeFailure": failure, "normalizedFailure": normalized,
            "failureRecord": str(record), "rawResultEvidence": str(raw),
            "rawResultSHA256": digest(raw), "before": before, "after": after,
            "workingCopyBytesUnchanged": digest(failed["source"]) == derivative_hash,
            "distinctFromUnsafeDeletionStage": True}


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
                    report = json_file(find_one(server["artifacts"] / "jobs" / job_id, "prm.report.json"))
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
        require(list(server["artifacts"].rglob("prm.report.json")), "API report missing at terminal observation")
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
    varying = compare_repeated_record_packages(
        record_package_snapshot(first), record_package_snapshot(second),
        required_families=("execution", "artifact", "observation", "verification"))
    scenarios.append({"name": "fake_repeated", "status": "passed",
                      "runtimeInvocations": 2, "stable": True,
                      "recordPackageIdentityVariance": varying, **a})

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
                persisted = find_one(server["artifacts"], "prm.report.json")
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
        "recordPackageStableHash", "stepCount",
    )
    require(all(a[key] == b[key] for key in stable_keys), "real repeated Engine identity differs")
    package_variance = compare_repeated_record_packages(
        record_package_snapshot(runs[0][0]), record_package_snapshot(runs[1][0]),
        required_families=("execution", "artifact", "observation", "verification"))
    observed_a = find_one_outside_record_package(runs[0][0]["out"], "prm.observed.json")
    observed_b = find_one_outside_record_package(runs[1][0]["out"], "prm.observed.json")
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
                    for name in ("prm.observed", "prm.verification", "source/")),
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
        "recordPackageStable": True,
        "recordPackageIdentityVariance": package_variance,
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
    traversal_request_path.write_text(json.dumps({"schemaVersion": "1.0", "externalTargets": []}))
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
    parser.add_argument("--mode", choices=("fake", "real", "all", "timeout", "target-foundation",
                                           "unsafe-delete-foundation", "target-mutations-real",
                                           "native-validity-real"), default="all")
    parser.add_argument("--report", type=Path)
    parser.add_argument("--keep-workspace", action="store_true")
    parser.add_argument("--timeout", type=int, default=240)
    args = parser.parse_args()
    if args.mode in ("target-foundation", "unsafe-delete-foundation", "target-mutations-real",
                     "native-validity-real") and "--freecad-repo" not in sys.argv:
        parser.error(f"{args.mode} requires explicit --freecad-repo")

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
        if args.mode in ("target-foundation", "unsafe-delete-foundation", "target-mutations-real",
                         "native-validity-real"):
            bindir = ctx.workspace / "bin"
            bindir.mkdir(parents=True, exist_ok=True)
            ctx.parametron = bindir / "parametron"
            run(["nix", "develop", "--command", "go", "build", "-o",
                 ctx.parametron, "./cmd/parametron"], cwd=ctx.engine_repo, timeout=ctx.timeout)
            if args.mode == "target-foundation":
                foundation = target_foundation_proof(ctx)
                summary["scenarios"] = foundation.pop("scenarios")
                summary["targetFoundation"] = foundation
            elif args.mode == "unsafe-delete-foundation":
                foundation = unsafe_delete_foundation_proof(ctx)
                summary["scenarios"] = [foundation.pop("scenario")]
                summary["unsafeDeleteFoundation"] = foundation
            elif args.mode == "native-validity-real":
                validity = native_validity_real_proof(ctx)
                summary["scenarios"] = [validity.pop("scenario")]
                summary["nativeValidityReal"] = validity
            else:
                real = target_mutations_real_proof(ctx)
                summary["scenarios"] = real.pop("scenarios")
                summary["targetMutationsReal"] = real
        elif args.mode == "timeout":
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
