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

### ~~`[S]` Drop the `env` struct in favour of `context.Context`~~ ✓ DONE
Refactor the `Run(e *env)` signature to `Run(ctx context.Context, e *env)` (or similar) to allow cancellation to flow into long-running plugin invocations and move towards standard Go patterns.

### ~~`[M]` Define a stable plugin-author API (SDK)~~ ✓ DONE
Extract the environment variable contract into a tiny `github.com/sadirano/onix-sdk` repo with helper functions so plugin authors get IDE autocomplete. (Prototype implemented in `sdk/`).

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
*(Note: macOS, fish, and nushell are intentionally out of scope.)*

### `[L]` Daemon mode
Implement a long-running daemon process that listens on a named pipe/Unix socket to eliminate process-spawn overhead for sub-millisecond tab-completion.

---

## Navigation UX (NEW)

### ~~`[M]` Frecency-ranked tab completion~~ ✓ DONE
Rank completion candidates by recency × frequency so the first Tab is almost always the one the user wants. Keep alphabetical fallback for ties. (Implemented via `usage.log` append-only scoring).

### `[M]` Cross-shell nav history (back/forward stack)
Maintain a persistent nav stack so `o -` (back) and `o +` (forward) work across shells, like a browser. Backed by a small append-only log.

### `[M]` Multi-target aliases
Allow one alias to resolve to several candidates with a fuzzy picker. Common when the same project name exists under different parents (e.g., `web` under two clients).

### `[S]` Stale-alias detection and repair
On resolve, if the target no longer exists, prompt to update or remove. `onix doctor` reports the full set so users can clean up in bulk.

### `[S]` Per-alias metadata
Optional `description`, `tags`, `owner`, `last_used` on alias entries. Surfaces in `onix list` and feeds future search/filter commands.

### `[M]` Importers from `z`, `autojump`, `direnv`
One-shot `onix import <tool>` that ingests existing alias-style stores so new users don't start empty. Idempotent and dry-run-able.

---

## Scope & Sharing (NEW)

### `[L]` Project-scope overrides (`.onix.toml`)
A per-repo config file, checked into version control, that can declare local aliases, segment maps, and actions. Auto-activated when the user is inside the repo. Lookup order: project → user → built-ins.

### `[L]` Workspace tier with sync
A named bundle of aliases that lives between user-scope and project-scope and can be synced across machines or shared with a team. Backends: git repo or generic object store. Per-alias visibility flag so a workspace can be partially shared.

### `[S]` Schema versioning on data files
Already listed under Docs & Hygiene — prerequisite for the scope work above, so it should land first.

---

## Plugin ecosystem

### `[L]` Plugin capability model (sandbox)
Plugins declare the capabilities they need (filesystem paths, network, env vars) in their manifest; the user grants or denies per install. Default deny for anything not declared.

### `[L]` Verified plugin registry
A registry with provenance, signatures, and reviews so `onix plugin add <name>` works without a full GitHub URL and gives users confidence in what they're running. Pinning by SHA stays available.

### `[M]` Action composition
Let actions invoke other actions in their template, enabling simple chains without writing scripts. Detect and reject cycles.

---

## Reliability

### `[M]` Concurrent-write safety (future worry)
Atomic writes already prevent file corruption, but two shells doing `onix add` simultaneously can still race on read-modify-write. Add a lock (advisory file lock on Linux, named mutex on Windows) around store mutations.

### `[M]` Context segment teardown
`context apply` sets env vars on entry but doesn't unset them when the user moves to a different alias. Track applied segments per shell so transitions are clean and idempotent.

### `[S]` Undo for the last destructive operation
Keep a one-deep journal of the last `remove`, `plugin remove`, or destructive `add` so `onix undo` restores it. Reduces fear of typos.

---

## Observability

### `[S]` Local-only usage stats
Opt-in `onix stats` that reports a personal "most-used aliases", "stale aliases", and "weekly nav count" from local-only data. Never sent anywhere.

---

## Docs & Hygiene (9 → 10)

### `[S]` Add per-command examples to `--help`
Use `kong`'s `examples:""` tags to provide inline usage examples for every subcommand.

### `[M]` Architecture diagram in the README
Add a Mermaid diagram showing the interaction between the shell, snippet, binary, and TOML data files.

### `[M]` Signed releases
Use `cosign` to sign release blobs and document the verification command in the README.

### ~~`[S]` Tag the schema versions~~ ✓ DONE
Add a `version = N` field to `aliases.toml` and other data files so future migrations can be handled safely.

---

## Order of operations

1. **Foundations:** Schema versioning, concurrent-write safety, `context.Context` plumbing, and the plugin SDK. These unblock everything below.
2. **Coverage & Validation:** Push coverage to 80% and implement shell-process E2E.
3. **Daily-driver wins:** Frecency-ranked completion, stale-alias detection, per-alias metadata, did-you-mean hints, help examples. Small surface, big perceived improvement.
4. **Scope leap:** Project-scope `.onix.toml`. Validate the layering inside the current architecture before tackling the workspace tier.
5. **Sharing & ecosystem:** Workspace tier with sync, plugin capability model, verified registry.
6. **Performance peak:** Daemon mode and benchmark gating.
