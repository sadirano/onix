# Road to 10/10

Current score: **9.5/10** (verified 2026-05-17 by audit and `go test ./...`).

A 10/10 here means: a stranger could clone the repo, build it on Windows or Linux, and trust that everything works the way the README says — including under hostile inputs and in the presence of pre-existing aliases-tool installs. The CI verifies that property on every PR; releases are reproducible and signed; the hot path has a measured ceiling that nobody can silently regress.

Pick from this list in order of cost/value. Items are grouped by axis and labelled `[S]` small, `[M]` medium, `[L]` large.

Since the previous revision, the `main`-package coverage push landed (Phases 1–8 of the working plan) and the 80%-per-package gate is now enforced in CI. Earlier in the cycle: end-to-end shell tests (`pwsh` + `bash` subprocesses that source the snippet and assert on `cd`), the README architecture diagram, least-privilege `GITHUB_TOKEN` on the test/lint workflows, third-party actions pinned by commit SHA, the `actions/dependency-review` job on pull_request, and `testing/quick`-based property tests for the name validators with a roundtrip invariant through `Save → Load → Lookup`.

---

## Code quality

### `[L]` Structured trace mode (`ONIX_DEBUG=1`)
Thread a `slog.Logger` through `env` so every command can emit a structured trace on demand for easier remote debugging. Default off; zero allocations on the hot path when disabled.

---

## Test suite

### `[M]` Benchmark regression gate
CI runs `benchstat bench_current.txt` informationally today. Add a second step that fetches the baseline from `main`, runs `benchstat baseline.txt current.txt`, and fails the build on >20% slowdown for `BenchmarkHotPath_LookupOnly`.

---

## Cross-platform parity
*(Note: macOS, fish, and nushell are intentionally out of scope.)*

### `[L]` Daemon mode
Implement a long-running daemon process that listens on a named pipe / Unix socket to eliminate process-spawn overhead for sub-millisecond tab-completion. Opt-in; the standalone binary remains the supported default.

---

## Navigation UX

### `[M]` Cross-shell nav history (back/forward stack)
Maintain a persistent nav stack so `o -` (back) and `o +` (forward) work across shells, like a browser. Backed by a small append-only log under `~/.onix/`.

### `[M]` Multi-target aliases
Allow one alias to resolve to several candidates with a fuzzy picker. Common when the same project name exists under different parents (e.g., `web` under two clients).

### `[S]` Undo for the last destructive operation
Keep a one-deep journal of the last `remove`, `plugin remove`, or destructive `add` so `onix undo` restores it. Reduces fear of typos.

---

## Scope & Sharing

### `[L]` Project-scope overrides (`.onix.toml`)
A per-repo config file, checked into version control, that can declare local aliases, segment maps, and actions. Auto-activated when the user is inside the repo. Lookup order: project → user → built-ins.

### `[L]` Workspace tier with sync
A named bundle of aliases that lives between user-scope and project-scope and can be synced across machines or shared with a team. Backends: git repo or generic object store. Per-alias visibility flag so a workspace can be partially shared.

### `[M]` Per-alias segment scope (with global opt-in)
Today every `[[contexts]]` entry in `~/.onix/segments.toml` is implicitly global — a segment named `tasks` matches `@tasks` under any alias, so two projects can't have their own `tasks` shape without clobbering each other. Move the default to per-alias scope and require an explicit `scope = "global"` marker on entries that should remain shared across every alias. Resolution rule for `seg1@seg2@...@alias`: look up each segment against the **terminal alias**'s local contexts first, then fall back to global contexts, then error or invoke the interactive prompt. The "Pick a source" prompt must also ask where to save (local-to-alias vs global).

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

### `[M]` Concurrent-write safety
Atomic writes already prevent file corruption, but two shells doing `onix add` simultaneously can still race on read-modify-write. Add a lock (advisory file lock on Linux, named mutex on Windows) around store mutations.

### `[M]` Context segment teardown
`context apply` sets env vars on entry but doesn't unset them when the user moves to a different alias. Track applied segments per shell so transitions are clean and idempotent.

---

## Supply chain & Hygiene

CI workflows declare least-privilege `GITHUB_TOKEN` permissions (closed CodeQL alert #1), pin third-party actions by commit SHA, and run `actions/dependency-review` on every pull_request. The remaining item in this axis:

### `[M]` Signed releases
Use `cosign` to sign release blobs and document the verification command in the README.

---

## Order of operations

1. **Hot-path safety net:** wire the benchstat-vs-main comparison so the perf claim is enforced, not asserted.
2. **Daily-driver wins:** cross-shell nav history, multi-target aliases, undo. Small surface, big perceived improvement.
3. **Scope leap:** project-scope `.onix.toml` and per-alias segment scope. Validate layering inside the current architecture before tackling the workspace tier.
4. **Sharing & ecosystem:** workspace tier with sync, plugin capability model, verified registry.
5. **Supply-chain finish:** cosign-signed releases.
6. **Performance peak:** daemon mode.
