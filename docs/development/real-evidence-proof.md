# Real FreeCAD Evidence Acceptance Proof

This document owns the repository-local, reproducible proof that Engine
occurrence, fact, and integrity contracts are accepted from a real FreeCAD
1.1.4 host behind the production `parametron-freecad` launcher.

The proof is Engine-owned. FreeCAD remains responsible for CAD-native
observation, mutation, persistence, and evidence emission. Engine remains
responsible for authoritative request documents, strict intake, normalization,
quantity rollup, integrity provenance, and record-package emission.

## Scope

The proof exercises:

- Engine-authored `prm.occurrence-request.json` and `prm.fact-request.json`
  sidecars, and the Engine-authored integrity request delivered through
  `PARAMETRON_RUNTIME_INTEGRITY_REQUEST`;
- the production `parametron-freecad execute` launcher and real FreeCAD 1.1.4;
- strict occurrence and fact evidence decoding, declared-resource joins,
  occurrence quantity rollup, fact unit/value normalization, and integrity
  evidence evaluation;
- outcome-scoped normalized observation records and exact raw-evidence
  provenance in a local record package;
- an independent persisted-result hash that must agree with the runtime report;
- a baseline mismatch rejected before mutation with the source bytes unchanged;
- ordinary references that do not create occurrences or quantity;
- shared targets, duplicate native names in separate declared resources,
  incomplete/unsupported/unresolved/auxiliary occurrence states, and required
  unavailable fact failure;
- relocation and repeated equivalent inputs without requiring byte-identical
  persisted `.FCStd` output.

## Prerequisites

- Run inside the Engine Nix environment, which provides Go and FreeCAD 1.1.4.
- Have the sibling `parametron-freecad` checkout, with Git LFS fixtures
  fetched (`git lfs pull`).
- Build the production wrapper from the FreeCAD flake.

```bash
nix build --no-link --print-out-paths \
  /path/to/parametron-freecad#parametron-freecad
```

The printed store path contains `bin/parametron-freecad`.

## Canonical command

From `parametron-engine`, run the Go proof harness directly:

```bash
nix develop --command env \
  PARAMETRON_FREECAD_EVIDENCE_PROOF=1 \
  PARAMETRON_FREECAD_RUNTIME=/nix/store/.../bin/parametron-freecad \
  PARAMETRON_FREECAD_REPO=/path/to/parametron-freecad \
  PARAMETRON_FREECAD_EVIDENCE_WORKSPACE=/tmp/parametron-evidence-proof \
  PARAMETRON_FREECAD_EVIDENCE_REPORT=/tmp/parametron-evidence-proof/evidence-proof-report.json \
  go test ./cmd/parametron -run '^TestRealFreeCADEvidenceProof$' -count=1 -v
```

The harness writes `evidence-proof-report.json` with:

- `status: "passed"` only when every scenario passed;
- per-scenario Engine path and normalized facts;
- the documented cross-repository blocker summary and impact.

For the proof harness report wrapper used by the repository proof tooling:

```bash
nix develop --command python3 scripts/cad_runtime_integration_proof.py \
  --mode evidence-real \
  --freecad-repo /path/to/parametron-freecad \
  --workspace /tmp/parametron-evidence-proof \
  --report /tmp/parametron-evidence-proof/proof-report.json
```

The wrapper builds the FreeCAD launcher, verifies the real FreeCAD smoke
payload (`freecadcmd`, version `1.1.4`), runs the Go harness, and embeds the
Engine evidence report under `realEvidence`.

Permanent opt-in coverage for the wrapper:

```bash
nix develop --command env PARAMETRON_RUN_FREECAD_INTEGRATION=1 \
  python -m unittest scripts.tests.test_cad_runtime_evidence_proof -v
```

The Go harness is skipped during ordinary `go test ./...` runs unless
`PARAMETRON_FREECAD_EVIDENCE_PROOF=1` is set.

## Engine-authored request documents

The harness does not hand-author request JSON as proof input:

- `cadruntime.ComposeOccurrenceRequestJSON` validates typed occurrence intent
  and returns the exact canonical request-document bytes. The normal adapter
  write path uses the same function.
- `cadruntime.ComposeFactRequestJSON` does the same for fact intent.
- `cadruntime.ComposeFreeCADIntegrityRequest` and
  `cadruntime.WriteFreeCADIntegrityRequest` author the per-resource integrity
  request from Engine-verified baselines, including a canonical placeholder for
  the intentionally absent fixture resource.
- The runtime execution manifest is produced from the Engine-owned
  `freecad.FreeCADRuntimeExportManifest` and its canonical marshaler.

## Evidence paths

The proof accepts two Engine paths, and both are required:

1. **Executor path** — the normal executor, real FreeCAD adapter, external
   runtime capability, and record-package emission path. Fact, required-fact
   failure, integrity, repeated-run, and multi-outcome scenarios use this path.
2. **Fixture-layout cadruntime path** — the unchanged FreeCAD fixture bundle
   layout is used for occurrence evidence because the fixture stores external
   link targets relative to the source document. Engine-composed occurrence,
   fact, and integrity requests are written into that attempt; the production
   launcher executes; Engine strict acceptance and package emission run on the
   returned evidence.

The second path is a documented cross-repository limitation, not a fake
runtime or a controlled in-process capability. It uses the real launcher and
real FreeCAD evidence.

## Cross-repository blocker

FreeCAD resolves an `App::Link` external target from the relative path stored
in the source document (for example `resources/component-a.FCStd`). Engine's
normal attempt layout materializes the selected source at
`source/<basename>` and additional declared resources at
`resources/<hash>/<coordinate>`. FreeCAD therefore looks for
`source/resources/component-a.FCStd`, which the executor layout does not
provide.

The proof records this deterministically:

- the normal executor occurrence scenario is explicitly `incomplete` with ten
  unresolved members and no quantity;
- the fixture-layout cadruntime scenario accepts the same unchanged fixture
  and proves ten distinct occurrences, quantity ten, mixed-scope explicit
  states, relocation stability, baseline rejection, and normalized provenance.

Resolving the executor-layout case requires a FreeCAD-side capability (for
example, resolving external link targets from the declared resource inventory)
or a coordinated fixture shape whose stored relative coordinates match Engine
materialization. It must not be fixed by moving FreeCAD link-resolution logic
into Engine.

## Determinism and non-claims

- Raw occurrence and fact evidence bytes are stable across equivalent
  relocated fixture attempts in the fixture-layout proof.
- Persisted `.FCStd` bytes are not claimed to be byte-identical across runs;
  each run's persisted-result digest is independently read and must agree with
  the runtime report.
- The proof does not add BOM, material inference, physical mass, universal unit
  conversion, recursive occurrence expansion, or array quantity semantics.
