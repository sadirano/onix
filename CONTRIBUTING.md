# Contributing to Onix

Thank you for your interest in contributing to Onix! This document provides guidelines and instructions for contributing to the project.

## Development Environment

- **Go:** Version 1.23 or later.
- **PowerShell:** Required for Windows shell integration testing.
- **Bash:** Required for Unix shell integration testing.

## Code Structure

Onix follows a clean architecture with core logic separated from the CLI interface:

- `internal/store`: Alias database management (`aliases.toml`).
- `internal/segments`: Segment registry and resolution (`segments.toml`).
- `internal/config`: Custom action configuration (`config.toml`).
- `internal/plugins`: External plugin management (`plugins.toml`).
- `internal/snippet`: Shell integration code generation.
- `commands.go`: CLI command definitions (using `kong`).
- `main.go`: Entry point and global configuration.

## Quality Standards

We aim for a high quality bar (see [ROADMAP.md](./ROADMAP.md)):

1. **Testing:** All new features must include unit tests and, where applicable, E2E tests in `e2e_test.go`.
2. **Verification:** Run `go vet ./...` and `go test ./...` before submitting changes.
3. **Benchmarks:** Performance is a feature. Ensure `BenchmarkHotPath_LookupOnly` and `BenchmarkHotPath_LoadAndLookup` (and any other hot-path benchmarks) do not regress. CI fails on a >20% slowdown against the baseline; see `docs/CI.md`.
4. **Golden Files:** Shell integration tests use golden files. Run `go test ./... -update` to update them if you intentionally change the generated code.

## Submitting Changes

1. **Format:** Use `go fmt` to ensure consistent code style.
2. **Lint:** We use `golangci-lint` (or similar) to catch common issues.
3. **Commits:** Prefer descriptive commit messages following conventional commits if possible.

## Troubleshooting Common Issues

### "The term 'onix' is not recognized"
This usually means the shell integration hasn't been sourced or the pinned binary has moved.
- Run `onix --init` to regenerate the snippet and update your profile.
- Run `onix --doctor` to check the status of your installation.

### "unknown alias"
- Run `onix --list` to see your registered aliases.
- Ensure you are using the correct case (though lookup is generally case-insensitive).

### Plugin build failures
- Ensure you have a working Go toolchain.
- Check `onix --doctor` for missing plugin binaries.
- Run `onix plugin update <name>` to re-clone and rebuild.
