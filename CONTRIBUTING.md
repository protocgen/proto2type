# Contributing to proto2type

Thank you for your interest in contributing to proto2type! This guide will help you get started.

## Development Environment

### Prerequisites

This project uses [Nix](https://nixos.org/) to manage the development environment. All required tools (Go, buf, protoc, linters, etc.) are provided by the Nix dev shell.

### Getting Started

1. **Clone the repository:**

   ```bash
   git clone https://github.com/protocgen/proto2type.git
   cd proto2type
   ```

2. **Enter the Nix dev shell:**

   ```bash
   nix develop
   ```

   This will install and make available all required tools, including Go, buf, protoc, golangci-lint, and pre-commit hooks.

3. **Pre-commit hooks** are installed automatically when you enter the dev shell. They run `go vet`, `gofmt`, `go mod tidy`, and `golangci-lint` on every commit.

## Running Tests

```bash
go test ./... -v
```

## Golden File Regeneration

When you change the code generator output, you need to regenerate the golden test files:

```bash
cd testdata/proto && buf generate
```

After regeneration, review the diff carefully to confirm the changes are intentional, then commit the updated golden files alongside your code changes.

## Code Style

- Run `gofmt` on all Go source files (enforced by pre-commit).
- Run `go vet ./...` to catch common issues (enforced by pre-commit).
- Lint with `golangci-lint run` for additional checks.

## Commit Conventions

This project follows [Conventional Commits](https://www.conventionalcommits.org/). Please format your commit messages as:

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

Common types: `feat`, `fix`, `docs`, `test`, `ci`, `refactor`, `chore`.

Examples:

```
feat(generator): add support for oneof fields
fix(parser): handle nested message types correctly
docs: update contributing guide
```

## Signed Commits Required

All commits must be signed. Configure Git commit signing with either GPG or SSH keys:

```bash
# GPG signing
git config --global commit.gpgsign true

# SSH signing
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519.pub
git config --global commit.gpgsign true
```

See [GitHub's documentation on commit signing](https://docs.github.com/en/authentication/managing-commit-signature-verification) for detailed setup instructions.

## Pull Request Process

1. Fork the repository and create a feature branch from `main`.
2. Make your changes with appropriate tests.
3. Ensure all tests pass: `go test ./... -v`
4. Ensure linting passes: `golangci-lint run`
5. Regenerate golden files if needed: `cd testdata/proto && buf generate`
6. Submit a pull request with a clear description of the changes.

## Reporting Issues

Please use [GitHub Issues](https://github.com/protocgen/proto2type/issues) to report bugs or request features. Include as much detail as possible, including Proto definitions that trigger the issue.
