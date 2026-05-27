# Contributing to Onix

> Onix is in maintenance mode for a single user (the maintainer). See
> [ROADMAP.md](./ROADMAP.md) for the parked-feature list. External
> contributions are accepted but not actively solicited.

## Development Environment

- **Go:** Version 1.26 or later (matches CI).
- **PowerShell:** Required for Windows shell integration.
- **Bash:** Required for Unix shell integration (used rarely in practice).

## Code Structure

- `internal/store`: alias database (`aliases.toml`).
- `internal/segments`: `@`-segment registry and resolution (`segments.toml`).
- `internal/config`: custom action configuration (`config.toml`).
- `internal/snippet`: shell snippet + `.cmd` shim generation.
- `internal/resolver`: shared alias-resolution helpers.
- `commands.go`, `grammar.go`, `main.go`: CLI dispatch.

## Quality Standards

1. **Testing:** New features include unit tests; the heavy
   `scripts/smoke.ps1` exercises end-to-end behaviour.
2. **Verification:** `go vet ./...`, `govulncheck ./...`, and
   `go test ./...` all pass — these are the CI gates.
3. **Golden Files:** Shell integration tests use golden files. Run
   `go test ./... -update` to refresh them after an intentional change.

There is no coverage or benchmark gate in CI; both were dropped when the
project entered maintenance mode. The `BenchmarkHotPath_*` benchmarks
in `bench_test.go` are kept as a measurement tool you can run locally
if you want to check that a change didn't tank the resolve hot path.

## Submitting Changes

1. **Format:** `go fmt ./...` (CI also runs `gofumpt` in the lint
   workflow).
2. **Lint:** `golangci-lint run` if you have it; CI runs it in the lint
   workflow.
3. **Commits:** descriptive messages; no `Co-Authored-By` trailers.

## Troubleshooting

### "The term 'onix' is not recognized"
Shell integration is not sourced or the pinned binary has moved.
- Run `onix --init` to regenerate the snippet and update your profile.
- Run `onix --doctor` to check the installation.

### "unknown alias"
- `onix --list` to see registered aliases (lookup is case-insensitive).
