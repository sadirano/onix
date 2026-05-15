# Road to 10/10

Current score: **9.2/10** (verified 2026-05-14 by audit and `go test ./...`).

Recent improvements have established automated linting/formatting in CI, reached >50% statement coverage, and verified the plugin lifecycle with integration tests. The core is rock-solid.

A 10/10 here means: a stranger could clone the repo, build it on Windows or Linux, and trust that everything works the way the README says — including under hostile inputs and in the presence of pre-existing aliases-tool installs. The CI verifies that property on every PR; releases are reproducible and signed; the hot path has a measured ceiling that nobody can silently regress.

Pick from this list in order of cost/value. Items are grouped by axis and labelled `[S]` small, `[M]` medium, `[L]` large.

---

## Code quality (9.5 → 10)

### ~~`[S]` Run `golangci-lint` clean in CI~~ ✓ DONE
`.github/workflows/lint.yml` verifies every commit.

### ~~`[S]` Sort imports and run `gofumpt`~~ ✓ DONE
Codebase is formatted and checked in CI.

### `[M]` Tighten error messages with hints
For user-visible errors like "unknown alias", include a "did you mean: <closest>" hint using a Levenshtein helper.

### `[L]` Add a structured trace mode (`ONIX_DEBUG=1`)
Thread a `slog.Logger` through `env` so every command can emit a structured trace on demand for easier remote debugging.

---

## Architecture coherence (9 → 10)

### `[S]` Drop the `env` struct in favour of `context.Context`
Refactor the `Run(e *env)` signature to `Run(ctx context.Context, e *env)` (or similar) to allow cancellation to flow into long-running plugin invocations and move towards standard Go patterns.

### `[M]` Define a stable plugin-author API (SDK)
Extract the environment variable contract into a tiny `github.com/sadirano/onix-sdk` repo with helper functions so plugin authors get IDE autocomplete.

---

## Test suite (9 → 10)

### `[M]` Coverage gate at 80%
Current coverage is **51.9%**. Target is 80%. Remaining areas: `main` package commands (run, exec, install-actions) and non-error paths in `search.go`.

### `[L]` End-to-end shell tests
The current E2E suite verifies the binary but doesn't source the snippet in a real shell process to assert on `cd` side effects. Implement actual `pwsh` and `bash` subprocess tests.

### `[M]` Benchmark regression gate
Add `benchstat` comparison in CI against the main branch and gate at 20% slowdown for `BenchmarkHotPath_LookupOnly`.

### `[S]` Property-based tests for name validators
Add `quick.Check` tests for `ValidateAliasName` to ensure it correctly rejects all illegal characters across a wide range of random inputs.

---

## Cross-platform parity (9.5 → 10)
*(Note: macOS is officially not supported in this repo)*

### `[S]` Doctor: fish, nushell awareness
Add soft hints in `onix doctor` if it detects a non-supported shell like fish or nu, pointing to the manual integration guide.

### `[L]` Daemon mode
Implement a long-running daemon process that listens on a named pipe/Unix socket to eliminate process-spawn overhead for sub-millisecond tab-completion.

---

## Docs & Hygiene (9 → 10)

### `[S]` Add per-command examples to `--help`
Use `kong`'s `examples:""` tags to provide inline usage examples for every subcommand.

### `[M]` Architecture diagram in the README
Add a Mermaid diagram showing the interaction between the shell, snippet, binary, and TOML data files.

### `[M]` Signed releases
Use `cosign` to sign release blobs and document the verification command in the README.

### `[S]` Tag the schema versions
Add a `version = N` field to `aliases.toml` and other data files so future migrations can be handled safely.

---

## Order of operations

1. **Architecture & API:** Transition to `context.Context` and define the plugin SDK.
2. **Coverage & Validation:** Push coverage to 80% and implement shell-process E2E.
3. **Product Polish:** Mermaid diagrams, help examples, and Levenshtein hints.
4. **Performance Peak:** Daemon mode and benchmark gating.
