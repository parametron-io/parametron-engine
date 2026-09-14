# Local Development

This document covers building, running, testing, and developing `parametron-engine`
locally.

## Prerequisites

- **Go 1.25+**
- **Python 3.10+** (required for external CAD runtime proof scripts)
- **FreeCAD 1.1.0+** (required only for opt-in real FreeCAD integration testing)

### Optional Environment Tooling

If you use Nix or `direnv`, the repository provides a development shell that
supplies compatible Go, Python, and FreeCAD binaries:

```bash
# Optional: enter development shell
nix develop

# Or with direnv
direnv allow
```

Project-native `go` and `python` commands are the canonical interface.

## Building

```bash
# Build the CLI binary
go build ./cmd/parametron

# Build the Engine API server binary
go build ./cmd/parametron-engine

# Build both binaries
go build ./cmd/parametron ./cmd/parametron-engine
```

## Running the CLI

```bash
# Validate a DSL file
go run ./cmd/parametron validate --file testdata/dsl/smoke/smoke_constants.dsl

# Simulate an execution plan
go run ./cmd/parametron simulate --file testdata/dsl/smoke/smoke_constants.dsl

# Execute a project
go run ./cmd/parametron --project testdata/projects/freecad/smoke/minimal-valid-project --out /tmp/run-out
```

### DSL Regression Gate

The DSL regression gate verifies the entire authoring pipeline using `stress.dsl`:

```bash
go run ./cmd/parametron --file testdata/dsl/stress.dsl --print-ast
go run ./cmd/parametron --file testdata/dsl/stress.dsl --print-plan
go run ./cmd/parametron --file testdata/dsl/stress.dsl --json-plan
```

All commands must complete with exit code 0.

## Running the Engine Server

```bash
# Start the HTTP API server
go run ./cmd/parametron-engine --addr :8080 --artifact-dir /tmp/parametron-artifacts
```

### Server Verification Smoke Test

In a second terminal:

```bash
# 1. Health check
curl -i http://127.0.0.1:8080/healthz

# 2. Generate a valid sample submission payload
go run ./cmd/parametron-engine-smoke > /tmp/job-payload.json

# 3. Submit the job
curl -i -H 'Content-Type: application/json' --data @/tmp/job-payload.json http://127.0.0.1:8080/job

# 4. Inspect job status
JOB_ID=$(go run ./cmd/parametron-engine-smoke --job-id-only)
curl -i http://127.0.0.1:8080/job/$JOB_ID

# 5. Inspect job-scoped artifacts
curl -i http://127.0.0.1:8080/job/$JOB_ID/artifacts
```

## Running Tests

### Standard Test Suite

```bash
# Run all unit and package tests
go test ./...

# Run static analysis
go vet ./...

# Run tests for a specific package
go test ./internal/engine/executor/...
go test ./internal/engine/recordmap/...
go test ./internal/authoring/dsl/...
```

### Race Detection

```bash
go test -race ./internal/engine/cache/... ./internal/engine/api/...
```

## CAD Runtime Configuration and Integration

### Aligned Runtime Wrapper Selection

The engine selects the FreeCAD runtime wrapper via environment variable:

```bash
export PARAMETRON_FREECAD_RUNTIME=/path/to/parametron-freecad
```

- If unset, the default is `parametron-freecad` discovered on `PATH`.
- For underlying FreeCAD host binary selection (used by the wrapper):
  ```bash
  export PARAMETRON_FREECAD_BIN=/path/to/freecadcmd
  ```

### External Runtime Proof Scripts

The repository includes end-to-end CAD runtime integration proof scripts:

```bash
# Run controlled simulation proof (no real FreeCAD required)
python scripts/cad_runtime_integration_proof.py --mode fake

# Run opt-in real FreeCAD integration proof (requires FreeCAD and wrapper)
PARAMETRON_RUN_FREECAD_INTEGRATION=1 python scripts/cad_runtime_integration_proof.py --mode real --freecad-repo ../parametron-freecad
```

### Opt-in FreeCAD Integration Tests

```bash
PARAMETRON_RUN_FREECAD_INTEGRATION=1 \
PARAMETRON_FREECAD_BIN=/path/to/freecadcmd \
go test ./internal/engine/adapter/freecad/... -run TestFreeCAD -v
```

## Versioning Policy

Parametron Engine follows Semantic Versioning (SemVer):
- **MAJOR**: Breaking changes to DSL grammar, execution plan schema, or hash identities.
- **MINOR**: Backward-compatible feature additions and new AST expressions.
- **PATCH**: Bug fixes, performance optimizations, and documentation updates.

## Related Documents

- [Testing Strategy](testing-strategy.md) — Architectural test layers and verification philosophy.
- [Extension Guide](extension-guide.md) — How to add new syntax, step types, and adapters.
- [Documentation Map](docs-map.md) — Complete repository documentation index.
