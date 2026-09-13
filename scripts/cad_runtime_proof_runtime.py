#!/usr/bin/env python3
"""Controlled external executable for the aligned CAD runtime proof."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import signal
import sys
import time


def fail(message):
    print(message, file=sys.stderr)
    raise SystemExit(2)


def canonical(path):
    return Path(path).resolve(strict=False)


def beneath(root, path):
    try:
        path.relative_to(root)
    except ValueError:
        fail(f"path escapes working copy: {path}")


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")


def locked_sequence(state_dir):
    state_dir.mkdir(parents=True, exist_ok=True)
    path = state_dir / "sequence"
    with path.open("a+", encoding="utf-8") as handle:
        fcntl.flock(handle, fcntl.LOCK_EX)
        handle.seek(0)
        raw = handle.read().strip()
        value = int(raw or "0") + 1
        handle.seek(0)
        handle.truncate()
        handle.write(str(value))
        handle.flush()
        os.fsync(handle.fileno())
        return value


def append_invocation(path, record):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        fcntl.flock(handle, fcntl.LOCK_EX)
        handle.write(json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n")
        handle.flush()
        os.fsync(handle.fileno())


def failure(result_path, code, message, stage="execute"):
    write_json(result_path, {
        "schemaVersion": "1.0",
        "status": "failed",
        "failure": {
            "boundary": "freecad",
            "category": "runtime",
            "code": code,
            "message": message,
            "stage": stage,
        },
    })
    return 17


def observed_payload(working, manifest, request, mismatch):
    source = Path(manifest["sourceDocument"])
    if not source.is_absolute():
        source = working / source
    digest = hashlib.sha256(source.read_bytes()).hexdigest()
    expected = request.get("expected", {})
    bindings = {
        item["id"]: item
        for item in request.get("observationContext", {}).get("parameters", [])
    }
    parameters = []
    for item in expected.get("parameters", []):
        binding = bindings.get(item["id"], {})
        value = item["value"]
        if mismatch and not parameters:
            value = value + 1 if isinstance(value, (int, float)) else str(value) + "-mismatch"
        parameters.append({
            "id": item["id"],
            "name": binding.get("name", item.get("name", item["id"])),
            "value": value,
            "valueKind": item.get("valueKind", "number"),
        })
    metadata = []
    for item in expected.get("metadata", []):
        value = digest if item["key"] == "working_copy_sha256" else item["value"]
        if mismatch and not parameters and not metadata:
            value = "mismatch"
        metadata.append({
            "id": item.get("id", item["key"]),
            "key": item["key"],
            "value": value,
            "valueKind": item.get("valueKind", "string"),
        })
    references = [
        {"kind": item["kind"], "name": item["name"]}
        for item in expected.get("references", [])
    ]
    if mismatch and not parameters and not metadata and references:
        references[0]["name"] += "-mismatch"
    components = [
        {"id": item["id"], "kind": item["kind"], "name": item["name"]}
        for item in expected.get("components", [])
    ]
    return {
        "schemaVersion": "1.0",
        "workingCopy": {"path": str(working), "sha256": digest},
        "observation": {
            "parameters": parameters,
            "metadata": metadata,
            "references": references,
            "components": components,
        },
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("command")
    parser.add_argument("--working-copy", required=True)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--result", required=True)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--observation-request", required=True)
    parser.add_argument("--reference-traversal-request", required=True)
    args = parser.parse_args()
    if args.command != "execute":
        fail("expected execute subcommand")

    paths = [canonical(getattr(args, name)) for name in (
        "working_copy", "manifest", "result", "output_dir", "observation_request",
        "reference_traversal_request"
    )]
    working, manifest_path, result_path, output_dir, request_path, traversal_request_path = paths
    if any(not Path(getattr(args, name)).is_absolute() for name in (
        "working_copy", "manifest", "result", "output_dir", "observation_request",
        "reference_traversal_request"
    )):
        fail("all aligned paths must be absolute")
    for path in paths[1:]:
        beneath(working, path)

    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    request = json.loads(request_path.read_text(encoding="utf-8"))
    traversal_request = json.loads(traversal_request_path.read_text(encoding="utf-8"))
    if traversal_request != {"schemaVersion": "2.0", "externalTargets": []}:
        fail("unexpected traversal request")
    mode = os.environ.get("PARAMETRON_CAD_PROOF_MODE", "success")
    state_dir = canonical(os.environ.get("PARAMETRON_CAD_PROOF_STATE_DIR", working / ".proof-state"))
    concurrent_product = None
    if mode == "concurrent":
        concurrent_product = Path(manifest["outputs"][0]["path"]).stem
        modes = json.loads((state_dir / "modes.json").read_text())
        mode = modes[concurrent_product]
    sequence = locked_sequence(state_dir)
    log_path = canonical(os.environ.get("PARAMETRON_CAD_PROOF_INVOCATION_LOG", state_dir / "invocations.jsonl"))
    append_invocation(log_path, {
        "sequence": sequence,
        "attemptID": working.name,
        "workingCopy": str(working),
        "manifest": str(manifest_path),
        "result": str(result_path),
        "outputDirectory": str(output_dir),
        "mode": mode,
        "pid": os.getpid(),
        **({"productKey": concurrent_product} if concurrent_product else {}),
    })

    if concurrent_product:
        # The harness observes all live invocations before releasing any job.
        # Poll explicit state; elapsed time never grants release.
        deadline = time.monotonic() + 60
        while not (state_dir / (concurrent_product + ".release")).exists():
            if time.monotonic() >= deadline:
                fail("concurrent proof release timeout")
            time.sleep(0.02)

    if mode == "block":
        signal.signal(signal.SIGTERM, lambda *_: raise_system_exit())
        while True:
            time.sleep(1)
    if mode == "runtime_failure":
        return failure(result_path, "controlled_runtime_failure", "intentional controlled runtime failure")
    if mode == "retry_then_success" and sequence <= 2:
        return failure(result_path, f"controlled_retry_{sequence}", f"retryable controlled failure {sequence}")
    if mode == "malformed_result":
        result_path.write_text("{\n", encoding="utf-8")
        return 0

    artifacts = []
    for output in manifest.get("outputs", []):
        relative = output["path"]
        target = working.joinpath(*relative.split("/"))
        beneath(output_dir, target)
        if mode != "missing_artifact":
            target.parent.mkdir(parents=True, exist_ok=True)
            logical_expected = json.loads(json.dumps(request.get("expected", {})))
            for reference in logical_expected.get("references", []):
                if reference.get("kind") == "working_copy_path":
                    reference["name"] = "source/" + Path(reference["name"]).name
            logical = json.dumps({
                "format": output["format"],
                "id": output["id"],
                "manifest": manifest,
                "requestExpected": logical_expected,
            }, sort_keys=True, separators=(",", ":"))
            target.write_bytes((logical + "\n").encode())
        artifacts.append({"id": output["id"], "format": output["format"], "path": relative})

    write_json(output_dir / "parametron.observed.json",
               observed_payload(working, manifest, request, mode == "observed_mismatch"))
    write_json(result_path, {"schemaVersion": "1.0", "status": "succeeded", "artifacts": artifacts})
    return 0


def raise_system_exit():
    raise SystemExit(143)


if __name__ == "__main__":
    raise SystemExit(main())
