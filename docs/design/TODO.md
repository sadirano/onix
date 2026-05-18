# TODO — Main-package coverage to 80%

Last measured: 2026-05-17, `go test ./... -count=1`. `main` package at **56.1%** (882/1573 statements). Target: **80%** → need to flip **~376 statements** from uncovered to covered.

This file is the working plan for that push. Phases are ordered by `(value/effort)` — easy pure-function tests first so the early commits move the needle without scaffolding cost. The non-trivial refactors (extracting `run()` from `main`, injecting an exec/git runner) come once we've harvested the cheap statements and need testability to go further.

Each phase has: target files, what's missing, the work (refactor + tests), and an expected statement-count delta.

---

## Coverage budget

Running total is approximate; each phase lists statements newly covered. Stop when total covered ≥ 1259 (80% of 1573). Phases are listed in execution order. The plan totals ~388 newly-covered statements, which clears 80% with a small margin.

| Phase | Theme                                          | New stmts | Cumulative | Pct  |
|-------|------------------------------------------------|----------:|-----------:|-----:|
| ~~0~~ | ~~Scaffolding (no coverage delta on its own)~~ |       ~~0~~ |      ~~882~~ | ~~56.1%~~ |
| ~~1~~ | ~~`show.go` pure helpers~~                     |      ~~45~~ |      ~~927~~ | ~~58.9%~~ |
| ~~2~~ | ~~`grammar.go` parsers (`atoi`, `runStatsFromArgs`, `dispatchSystem`)~~ |      ~~70~~ |      ~~997~~ | ~~63.4%~~ |
| ~~3~~ | ~~`main.go` extraction + flag-dispatch tests~~ |      ~~40~~ |     ~~1037~~ | ~~65.9%~~ |
| ~~4~~ | ~~`show.go` `Run` / `buildShowCommand` via injected exec~~ |      ~~19~~ |     ~~1056~~ | ~~67.1%~~ |
| ~~5~~ | ~~`commands.go` `ExploreCmd.Run` + small gaps~~ |      ~~45~~ |     ~~1101~~ | ~~70.0%~~ |
| ~~6~~ | ~~`doctor.go` `checkPluginsFile` / `checkSegmentsFile` / `checkInstalledPlugins`~~ |      ~~45~~ |     ~~1146~~ | ~~72.9%~~ |
| 7     | `plugin_install.go` git helpers via injected runner |       30  |      1176  | 74.8%|
| 8     | `plugin_cmd.go` `Add` / `Update` / `Remove`    |       70  |      1246  | 79.2%|
| 9     | `search.go` `GrepCmd.Run` / `FindCmd.Run` builders |       20  |      1266  | 80.5%|

If we overshoot in earlier phases, phase 9 becomes optional polish. If we undershoot, the contingency is more `grammar.go` table cases (the parser there has the most testable surface left).

---

## Strategy & ground rules

- **No production behaviour changes.** Refactors are mechanical: extract a function, swap a free function for a struct method, add a `var execCommand = exec.Command` indirection.
- **Each phase is a separate commit** (per the granular-commits memory). Phases that combine a refactor and tests can split into two commits: one for the refactor, one for the tests.
- **Test files live next to the file under test.** `show_test.go`, `plugin_install_test.go`, etc. No giant `coverage_test.go` dump.
- **Prefer unit tests over E2E.** `e2e_test.go` is for golden-path shell integration; coverage work belongs in package-level tests so it's fast and deterministic.
- **Coverage is a means, not the goal.** Skip statements that exist only for `os.Exit` plumbing or platform syscalls; account for them in the budget. We don't need every line — we need the gate to pass with tests that would catch a real regression.
- **Run `gofumpt -l .` and `go vet ./...` before each commit.** Both gates are part of CI; failing them locally wastes a roundtrip.

---

## Phase 0 — Scaffolding

No coverage delta on its own. Lands as one or two commits before the per-file work begins, so later phases can land as small additions.

### 0.1 `var execCommand = exec.Command` indirection
**Files:** `plugin_install.go`, `plugin_cmd.go`, `show.go`, `search.go`, `commands.go`, `explorer_windows.go`
- Add a package-level `var execCommand = exec.Command` (and `var execCommandContext = exec.CommandContext` where used).
- Replace direct `exec.Command(...)` / `exec.CommandContext(...)` calls in the files above with the variables.
- Tests can override these to a fake that records argv and returns a synthetic `*exec.Cmd` (built with `exec.Command("cmd", "/c", "exit 0")` on Windows, `("true")` on Unix) so we can exercise the build-the-command-line logic without actually shelling out.

**Acceptance:** all existing tests still pass. Build still produces an identical binary. No new file.

### 0.2 Extract `run(args []string, stdout, stderr io.Writer) int` from `main()`
**Files:** `main.go`
- Move the body of `main()` into `func run(args []string, stdout, stderr io.Writer) int` returning an exit code.
- `main()` becomes `os.Exit(run(os.Args, os.Stdout, os.Stderr))`.
- The recover/panic-formatter stays in `run` and returns `1` instead of calling `os.Exit(1)` directly. Same for the two other `os.Exit(1)` sites — `return 1` from `run`.
- Tests can call `run([]string{"onix", "--version"}, ...)` and assert on the exit code and captured output.

**Acceptance:** `e2e_test.go` still passes (the binary boundary is unchanged). Hot-path benchmark unaffected.

### 0.3 Inject `confirmInstall`'s stdin
**Files:** `plugin_install.go`
- Change `confirmInstall(...)` to take an `io.Reader` (or accept it through a package-level `var stdinSource io.Reader = os.Stdin`).
- Update callers in `plugin_cmd.go` to pass `os.Stdin`.

**Acceptance:** existing flow unchanged; tests can pass `strings.NewReader("y\n")` to drive the confirmation.

---

## Phase 1 — `show.go` pure helpers

**Target:** `psQuoteArgs`, `hasShortFlag`, `hasPositional`, `targetDir`, `buildShowCommand` (non-exec parts).
**Coverage gain:** ~45 statements (the file has 64 uncovered total; the remaining ~19 land in Phase 4 once exec is injected).

### What to test in `show_test.go`
1. `psQuoteArgs` — table-driven:
   - flag tokens (`-Head`, `--Filter`) pass through unchanged
   - bare identifiers (`README.md`) pass through unchanged
   - strings with whitespace get single-quoted (`hello world` → `'hello world'`)
   - strings with embedded single quotes get them doubled (`it's` → `'it''s'`)
   - all metachars in the list (`"'`,$;` + tab) trigger quoting
2. `hasShortFlag` — table-driven:
   - `-l`, `-la`, `-lah` all match `l`
   - `--long` does NOT match (we explicitly skip `--`)
   - non-flag positionals don't match
3. `hasPositional` — true when any arg lacks a `-` prefix; false otherwise; empty slice false.
4. `targetDir` — feed a faked `env` with a temp `Home` and an alias-less command; assert `Home` is returned. With a known alias, assert the resolved path.
5. `buildShowCommand` (just argv construction):
   - Windows: mode="list" → first arg is `powershell`, last arg of the script is `Get-ChildItem` (or with quoted args appended)
   - Windows: mode="files" → script uses `Get-Content`
   - Unix: mode="list" with no `-l` flag → `ls -la` is the argv; with `-la` already present, no duplicate
   - Unix: mode="files" → `cat <args...>`

**Effort:** ~1.5h. Pure functions, no scaffolding needed once Phase 0.1 lands.

---

## Phase 2 — `grammar.go` parsers

**Target:** `atoi`, `runStatsFromArgs`, `dispatchSystem` (the verb-routing switch), `printUsage` (just confirm it writes something non-empty).
**Coverage gain:** ~70 statements.

### What to test in `grammar_test.go` (extend the existing file)
1. `atoi`:
   - parses `"42"`, `" 42 "`, `"-7"`
   - rejects `"abc"` with an error message that quotes the bad input
2. `runStatsFromArgs`:
   - `--full` sets `Full=true`; `--cold` sets `Cold=true`
   - `--since 30d` and `--since=30d` both set `Since="30d"`; `--since` alone errors
   - `--top 5` and `--top=5` set `Top=5`; `--top` alone errors; `--top abc` errors via `atoi`
   - unknown flag returns a wrapped error
   - Call `Run` against a temp `env.Home` with an empty store to confirm the parsed `StatsCmd` actually executes (and the parser's plumbing is right).
3. `dispatchSystem` — invoke each verb against a temp home and assert it runs without error:
   - `list-names` → calls `fastListNames`
   - `init` with no flags → creates the home dir layout (verify `~/.onix/config.toml` exists after)
   - `init --skip-profile` → same, no shell-snippet side effect
   - `init` with unknown flag → returns "unknown flag" error
   - `apply-context` without an alias → "requires an alias" error
   - `apply-context` with `--shell pwsh demo` against a registered alias → returns nil (writes to a discarded buffer)
   - `apply-context` with `--shell=bash demo` (= form) → same
   - bad verb → "unknown system action" (defensive — caller should have filtered it; still good to lock the message)
4. `printUsage` — capture stdout, assert it contains `"USAGE:"` and `"ALIAS ACTIONS:"`. Snapshot-style, just enough that anyone editing it can't accidentally delete it.

**Effort:** ~2h. `dispatchSystem` is wide but each case is a one-liner once the temp-home helper exists; reuse `e2e_test.go`'s `onixRunner` pattern but at the function level.

---

## Phase 3 — `main.go` extraction + flag dispatch

**Target:** the `hasFlag` helper, `runPluginKong` happy paths, and the post-extraction `run` body.
**Coverage gain:** ~40 statements. The crash-recover branch and the `os.Exit` plumbing stay uncovered — that's ~10–15 statements we don't fight for.

### What to test in `main_test.go` (new file)
1. `hasFlag`:
   - matches a literal token
   - rejects `--json=foo` (since the comment says it doesn't understand `=` for bools)
   - empty `args` → false
2. `run([]string{"onix", "--version"}, stdoutBuf, stderrBuf)`:
   - returns 0, stdoutBuf contains `"onix"` and a version string
3. `run([]string{"onix", "--help"}, ...)` → 0, contains `"USAGE:"`
4. `run([]string{"onix"}, ...)` → 0, also prints usage
5. `run([]string{"onix", "--bogus"}, ...)` → 1, stderrBuf contains `"unknown flag"`
6. `run([]string{"onix", "plugin", "ls"}, ...)` with an empty home → 0, stdoutBuf says `"no plugins installed"`. Verifies the kong path.
7. `run([]string{"onix", "plugin", "ls", "--config-dir", tempHome}, ...)` → same, exercises the `--config-dir` override branch.

`ONIX_HOME` must be set via `t.Setenv` to a temp dir so `resolveHome` succeeds without touching the user's real config.

**Effort:** ~1.5h, mostly building the runner pattern. The extraction itself (Phase 0.2) carries the risk; this phase is pure tests on top.

---

## Phase 4 — `show.go` `Run` / `buildShowCommand` via injected exec

**Target:** `ShowCmd.Run`, `runShowList`, `runShowFiles`, `passthroughExit`.
**Coverage gain:** ~19 statements (the remainder of `show.go`).

### What to test in `show_test.go`
1. Set `execCommandContext` to a fake that returns a no-op `*exec.Cmd` (`exec.Command("cmd", "/c", "exit 0")` on Windows, `("true")` on Unix). Capture the args via the fake.
2. `runShowList` with a temp dir and `["-Filter", "*.go"]` → fake captures argv; assert `cmd.Dir == tmp`.
3. `runShowFiles` with `["README.md"]` → assert argv includes `README.md`.
4. `passthroughExit` with a `*exec.ExitError` is hard to test without spawning — we can either skip the `os.Exit` branch (lose 2 stmts) or wrap `os.Exit` behind `var exit = os.Exit` and assert it's called with the right code. The wrap is cheap; do it.
5. `ShowCmd.Run` against an unknown alias → resolver error surfaces.

**Effort:** ~1h once Phase 0.1's `execCommandContext` indirection is in.

---

## Phase 5 — `commands.go` `ExploreCmd.Run` + small gaps

**Target:** `ExploreCmd.Run`, any low-hanging branches in `RunCmd`, `YankCmd`, `EditCmd`.
**Coverage gain:** ~45 statements (the file has 61 uncovered; the remaining ~16 are inside the kong-bound bits that the dispatcher already drives via `e2e_test.go`).

### What to test in `commands_test.go` (extend the existing file)
1. `ExploreCmd.Run` against an unknown alias → resolver error.
2. `ExploreCmd.Run` against a registered alias → with `execCommand` faked (Phase 0.1), assert argv is `explorer.exe <path>` on Windows / `xdg-open <path>` on Linux (or whichever fallback the file uses). We're testing argv construction, not the actual GUI launch.
3. `YankCmd.Run`: register an alias, call `Run`, capture stdout, assert path is printed. Clipboard failure is non-fatal — wire a mock clipboard or accept the warning.
4. `RunCmd.Run`:
   - `len(c.Args) < 2` returns the usage error
   - With a `"--"` first extra arg, that token gets stripped before the child gets it
5. `EditCmd.Run` with no `$EDITOR` set and no fallback → useful error.

**Effort:** ~2h. `openInExplorer` on Windows needs the `execCommand` indirection to be testable.

---

## Phase 6 — `doctor.go` checks

**Target:** `checkPluginsFile`, `checkSegmentsFile`, `checkInstalledPlugins`.
**Coverage gain:** ~45 statements.

### What to test in `doctor_test.go` (extend the existing file)
For each check function, drive these scenarios against a temp home:
1. **Missing file**: function returns the "absent (no …)" `ok` result.
2. **Empty / parseable file**: returns an `ok` result with the right counts (0 plugins / 0 subdirs / etc.).
3. **Malformed TOML**: write garbage bytes to the file, expect an `err` checkResult whose `detail` mentions the path.
4. **Validation failure** (plugins-only): write a `plugins.toml` whose entry collides with a config action, expect `err`.
5. `checkInstalledPlugins`:
   - No plugins → returns nil slice
   - Plugin declared but binary missing → `err` result mentioning the bin path
   - Plugin declared, binary present (touch a stub file), unpinned → `warn` result
   - Plugin declared, binary present, pinned → `ok` result with short SHA

**Effort:** ~2h. Straightforward fixture-based tests.

---

## Phase 7 — `plugin_install.go` git helpers

**Target:** `gitFetch`, `gitCheckout`, `gitHeadSHA`, `gitHeadMessage`, `buildPlugin`, `confirmInstall`, `resolveRepoURL`, `readPluginManifest`.
**Coverage gain:** ~30 statements.

### Refactor
Beyond Phase 0.1's `execCommand` indirection, the git helpers each call `exec.Command` (not `CommandContext`). Add a `var gitRunner = runGitCmd` indirection where `runGitCmd(args ...string)` wraps `exec.Command("git", args...)`. Tests fake it to return canned stdout / fake error.

### What to test in `plugin_install_test.go` (new file)
1. `resolveRepoURL`:
   - `user/repo` → `https://github.com/user/repo.git`
   - `https://example.com/foo` → unchanged
   - `git@github.com:user/repo.git` → unchanged
   - `C:\path` → unchanged
   - `/abs/path` → unchanged
2. `readPluginManifest`:
   - Missing file → `(nil, nil)`
   - Valid manifest → returns the entries
   - Malformed TOML → returns an error mentioning the path
3. `gitCheckout` with `ref=""` → faked git returns a branch name, then a reset succeeds → returns nil.
4. `gitCheckout` with `ref="abc123"` → faked git records `["reset", "--hard", "abc123"]`.
5. `gitHeadSHA` / `gitHeadMessage` → trim whitespace from faked output.
6. `confirmInstall` with stdin `"y\n"` → true; with `"n\n"` → false; with EOF → false; with `"Y\n"` and spaces → true (we use `EqualFold(TrimSpace(...))`).
7. `buildPlugin` with a temp dir missing `go.mod` → returns the "only Go plugins" error. With a stub `go.mod`, the faked exec succeeds.

**Effort:** ~2.5h. The fake-runner pattern is the bulk; once it exists each test is short.

---

## Phase 8 — `plugin_cmd.go` happy paths

**Target:** `PluginAddCmd.Run`, `PluginUpdateCmd.Run`, `PluginRemoveCmd.Run`, `PluginListCmd.Run` (already partially covered).
**Coverage gain:** ~70 statements. This is the biggest single file.

### What to test in `plugin_cmd_test.go` (new file)
Each test drives the command with `--yes` to skip confirmation (the confirmation path is covered by Phase 7). Fake the git runner and `buildPlugin`.

1. `PluginAddCmd.Run`:
   - Neither `--sha` nor `--unpinned` → "either --sha or --unpinned is required"
   - `--unpinned` happy path: fakes succeed, `plugins.toml` ends up with the plugin entry, snippet regen runs.
   - `--sha abc123` happy path: same, with sha pinned.
   - Existing `.git` in srcDir → `gitFetch` path; absent → `gitClone` path.
2. `PluginUpdateCmd.Run`:
   - No plugins installed → prints "no plugins installed", returns nil.
   - `--sha` without `Name` → error.
   - With a registered plugin and `--yes`, faked git returns a new SHA → plugin's SHA gets updated in plugins.toml.
3. `PluginRemoveCmd.Run`:
   - Unknown plugin → `unknown plugin` error.
   - Registered plugin → removed from plugins.toml; the snippet is regenerated.
4. `PluginListCmd.Run`:
   - Empty list → "no plugins installed".
   - Empty list with `e.JSON=true` → `[]`.
   - With one installed plugin + missing binary → the table includes `"missing binary"`.

**Effort:** ~3h. Lots of fixture wiring (a fake plugin repo on disk, a fake git runner with scripted responses). Worthwhile because this file is 32% of the remaining gap.

---

## Phase 9 — `search.go` builders

**Target:** `GrepCmd.Run` and `FindCmd.Run` (argv construction and the LookPath fallback chain).
**Coverage gain:** ~20 statements. Probably optional once we hit 80% — keep as a stretch goal.

### What to test in `search_test.go` (new file)
Each `Run` calls `exec.LookPath` for `rg` / `fzf` / `fd` / `es`. Wrap those behind a package-level `var lookPath = exec.LookPath` so tests can fake them.

1. `GrepCmd.Run`:
   - `len(c.Args) < 1` → usage error.
   - Missing `rg` (faked LookPath returns ErrNotFound) → "ripgrep not found".
   - Missing `fzf` → "fzf not found".
   - With both present and faked exec → assert `rg` argv includes `--smart-case` and ends in `query` then `.`.
2. `FindCmd.Run`:
   - On Windows with `es` present → `es` is chosen.
   - On Windows without `es` but with `fd` → `fd` is chosen.
   - On Unix without `fd` → `find` is chosen.
   - In each branch, assert the argv built before stdin piping is correct.

`openSelectionsInEditor` is the tail call that opens the chosen lines — it's harder to drive because it spawns the editor. Skip with `//coverage:ignore` equivalent (or just leave at 0% — the budget already accounts for that).

**Effort:** ~1.5h.

---

## Out-of-scope (accept below 80%)

These statements stay uncovered. Documented here so reviewers understand the gap is intentional.

| Where                            | Stmts | Why                                                                 |
|----------------------------------|------:|---------------------------------------------------------------------|
| `main.go:37` panic-recover block | ~12   | Tested implicitly by E2E if it ever fires; not worth the fake-panic harness. |
| `main.go` `os.Exit(1)` plumbing  | ~3    | Behaviour proven by `run()` returning 1; the final `os.Exit` is trivial. |
| `explorer_windows.go` `openInExplorer` | ~5 | Single `exec.Command` call; Phase 5 covers the dispatch into it but the OS-level launch itself is platform-bound. |
| `search.go` `openSelectionsInEditor` | ~12 | Spawns the user's editor as the final UI step; testing argv would be redundant with `RunCmd` and the editor side effects aren't worth mocking. |

Total ~32 statements (~2% of the package). Subtracted from the 1573-statement denominator, we have headroom: the budget hits 1266 / 1573 = 80.5% even with these excluded from the "covered" column.

---

## CI gate

Once the package is at 80%, wire the gate. Two options — start with the first, escalate to the second only if regressions slip through:

1. **Per-package threshold (cheap):** add a step in `test.yml` that parses `go test -cover` output and fails when `main` < 80%. One-shot, no extra deps.
2. **Coverprofile + threshold per file** (later): generate `cover.out`, then a small Go program that asserts each package meets its target. More robust but adds a script to maintain.

Open a follow-up PR for the gate once the threshold is consistently met locally — don't gate the same PR that pushes coverage over the line, since a failing benchmark would block the very change that fixes it.

---

## Backlog (unrelated to the coverage push)

- **Use fzf when prompting for a segment source.** `promptSegmentDefinition` in `navigate.go` currently prints a numbered list (`[1] template / [2] exec / [3] file`) and reads a digit from stdin. Replace the picker with an fzf invocation (mirror the auto-detect-and-fall-back pattern already in `promptSelection`) so the choice feels consistent with other onix pickers. Numeric prompt stays as the fallback when fzf isn't on PATH.

- **Segments should be per-alias by default, with global as opt-in.** Today every `[[contexts]]` entry in `~/.onix/segments.toml` is implicitly global — a segment named `tasks` matches `@tasks` under any alias, so two projects can't have their own `tasks` shape without clobbering each other. Move the default to per-alias scope (e.g. a `[aliases.<name>.contexts]` block, or an equivalent per-alias contexts file) and require an explicit `scope = "global"` marker on entries that should remain shared across every alias (`notes`, etc.). Resolution rule for a segmented invocation `seg1@seg2@...@alias`: look up each segment name against the **terminal alias**'s local contexts first, then fall back to global contexts, then error or invoke `promptSegmentDefinition` as today. The `Pick a source:` prompt must also ask where to save (local-to-alias vs global) — see the fzf item above; the two should land together since the picker is the natural place to surface scope.

---

## Tracking

When a phase lands, strike it out here (or delete) and note the new coverage % in the budget table. Roadmap line "`[M]` Coverage gate at 80%" stays open until both the 80% number AND the CI gate are in.
