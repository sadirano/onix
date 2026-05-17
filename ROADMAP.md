# Road to 10/10

Current score: **9.2/10** (verified 2026-05-14 by audit and `go test ./...`).

A 10/10 here means: a stranger could clone the repo, build it on Windows or Linux, and trust that everything works the way the README says — including under hostile inputs and in the presence of pre-existing aliases-tool installs. The CI verifies that property on every PR; releases are reproducible and signed; the hot path has a measured ceiling that nobody can silently regress.

Pick from this list in order of cost/value. Items are grouped by axis and labelled `[S]` small, `[M]` medium, `[L]` large.

---

## Code quality

### `[L]` Add a structured trace mode (`ONIX_DEBUG=1`)
Thread a `slog.Logger` through `env` so every command can emit a structured trace on demand for easier remote debugging.

---

## Test suite

### `[M]` Coverage gate at 80%
Per-package coverage as of 2026-05-17:

| Package            | Coverage |
|--------------------|----------|
| `main`             | **56.1%** |
| `internal/config`  | 88.9%    |
| `internal/plugins` | 73.0%    |
| `internal/resolver`| 75.7%    |
| `internal/segments`| 88.6%    |
| `internal/snippet` | 86.1%    |
| `internal/store`   | 78.6%    |
| `sdk`              | 92.3%    |

The remaining gap is entirely in the `main` package — the alias-flag dispatcher and the handlers it routes to.

### `[L]` End-to-end shell tests
The current E2E suite verifies the binary but doesn't source the snippet in a real shell process to assert on `cd` side effects. Implement actual `pwsh` and `bash` subprocess tests.

### `[M]` Benchmark regression gate
Add `benchstat` comparison in CI against the main branch and gate at 20% slowdown for `BenchmarkHotPath_LookupOnly`.

### `[S]` Property-based tests for name validators
Add `quick.Check` tests for `ValidateAliasName` to ensure it correctly rejects all illegal characters across a wide range of random inputs.

---

## Cross-platform parity
*(Note: macOS, fish, and nushell are intentionally out of scope.)*

### `[L]` Daemon mode
Implement a long-running daemon process that listens on a named pipe/Unix socket to eliminate process-spawn overhead for sub-millisecond tab-completion.

---

## Navigation UX

### `[M]` Cross-shell nav history (back/forward stack)
Maintain a persistent nav stack so `o -` (back) and `o +` (forward) work across shells, like a browser. Backed by a small append-only log.

### `[M]` Multi-target aliases
Allow one alias to resolve to several candidates with a fuzzy picker. Common when the same project name exists under different parents (e.g., `web` under two clients).

---

## Scope & Sharing

### `[L]` Project-scope overrides (`.onix.toml`)
A per-repo config file, checked into version control, that can declare local aliases, segment maps, and actions. Auto-activated when the user is inside the repo. Lookup order: project → user → built-ins.

### `[L]` Workspace tier with sync
A named bundle of aliases that lives between user-scope and project-scope and can be synced across machines or shared with a team. Backends: git repo or generic object store. Per-alias visibility flag so a workspace can be partially shared.

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

## Docs & Hygiene

### `[M]` Architecture diagram in the README
Add a Mermaid diagram showing the interaction between the shell, snippet, binary, and TOML data files.

### `[M]` Signed releases
Use `cosign` to sign release blobs and document the verification command in the README.

---

## Order of operations

1. **Coverage & Validation:** Push `main`-package coverage to 80% and implement shell-process E2E.
2. **Daily-driver wins:** Cross-shell nav history, multi-target aliases. Small surface, big perceived improvement.
3. **Scope leap:** Project-scope `.onix.toml`. Validate the layering inside the current architecture before tackling the workspace tier.
4. **Sharing & ecosystem:** Workspace tier with sync, plugin capability model, verified registry.
5. **Performance peak:** Daemon mode and benchmark gating.
