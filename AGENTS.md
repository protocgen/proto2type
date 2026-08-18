# AI Agent Coding Guidelines for proto2type

This document provides essential context and rules for AI coding assistants working on the `proto2type` codebase. Read this file carefully before making changes.

## 1. Project Overview
`proto2type` is a `protoc`/`buf` plugin that generates typed domain models and storage layers from Protocol Buffer definitions. It supports 5 backends: Go, Rust, Python, Kotlin, and TypeScript. It eliminates the need to manually maintain parallel structs for business logic and databases (e.g., Firestore, MongoDB, SQLite).

## 2. Architecture
The generator pipeline is structured as follows:
- **IR Layer**: `generator/ir.go` and `generator/ir_build.go` parse proto files and build an Intermediate Representation.
- **Backend Generators**: The IR is passed to backend-specific generators (`<lang>_domain.go`, `<lang>_types.go`, `<lang>_validate.go`), which emit the code.

## 3. Build & Test
- **Build**: `go build .`
- **Test**: `go test ./...`
- **Install & Generate**: `go install . && buf generate`

*(Note: Run these inside the Nix dev shell by prefixing with `nix develop -c`)*

## 4. Golden File Workflow
Whenever generator logic or IR is modified, golden files must be updated:
1. `UPDATE_GOLDEN=1 go test -run TestTSGoldenUpdate`
2. Run `buf generate` for each backend (see `.github/workflows/ci.yml` or `lefthook.yml` for exact commands).
3. Run `git diff testdata/golden/` to verify the generated drift is intentional.

## 5. Backend Pattern
Each language backend follows a consistent architectural pattern:
1. **Options Parsing**: Read `proto2type` specific proto annotations.
2. **IR Construction**: Map to the generic intermediate representation.
3. **Domain Generator**: `*_domain.go` generates the main struct/class definitions.
4. **Type Mapper**: `*_types.go` maps proto types to native language types.
5. **Constraint Emitter**: `*_validate.go` translates `buf.validate` rules to native validation.

## 6. Testing Conventions
- **Unit Tests**: Co-located `*_test.go` files in the `generator/` directory.
- **Golden File Comparison**: Snapshot testing for generator outputs (`testdata/golden/`).
- **Property-Based Testing**: Used in certain tests, such as `ts_pbt_test.go`.
- **Integration Tests**: Located in `tests/integration/` for database interactions (MongoDB, Firestore).
- **Runtime Tests**: Language-specific runtime validations in `tests/python/`, `testdata/golden/ts/test/`, and `tests/rust-integration/`.

## 7. Pre-commit & Pre-push (Lefthook)
The project uses `lefthook` for Git hooks:
- **Pre-commit**: Runs `gofmt`, `go-vet`, and `golangci-lint` to ensure code quality.
- **Pre-push**: Runs `go-test` and `golden-check` to verify no accidental drift in generated code.

## 8. CI Pipeline
The GitHub Actions CI pipeline consists of the following key jobs:
- `build-and-test`: Core Go build and tests.
- `lint`: Format and security checks.
- `golden-test`: Validates no uncommitted golden file drift.
- `rust-compile-check`: Validates generated Rust code compiles.
- `kotlin-compile-check`: Validates generated Kotlin code compiles.
- `integration-db`: Tests MongoDB/Firestore storage functionality via emulators.
- `rust-integration-test`: Tests generated Rust/SQLite code functionality.
- `typescript-check`: Runs `tsc` and runtime validation on generated Zod code.
- `python-check`: Runs `mypy` and Pydantic validation on generated Python code.

## 9. Key Rules
- **DO NOT** use `--no-verify` on Git commits. The `pre-commit` hooks must always run.
- **ALWAYS** regenerate ALL golden files after changing the IR or any generator logic to ensure no unintended consequences.
- **ALWAYS** run `go test ./...` before pushing changes.
- Ensure terminal commands are prefixed with `nix develop -c` if a Nix environment is available.

## 10. TypeScript/Zod Specifics
When working on the TypeScript backend, observe these key files in `generator/`:
- `ts_domain.go`: Main Zod schema and type generation.
- `ts_types.go`: Proto-to-TypeScript type mapping logic.
- `ts_validate.go`: Translates `buf.validate` into Zod constraints (`.min()`, `.regex()`, etc.).
- `ts_imports.go`: Manages required imports (like `zod`).
