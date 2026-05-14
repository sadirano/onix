# Road to 10/10

Current score: **6.2/10** (verified 2026-05-14 by running `go test ./...` and `go test -coverprofile=coverage.out ./...`).

Previous self-reported score was 9.9/10, but actual test run shows 4 failing tests, 8.7% total statement coverage, and 0.0% coverage across all five `internal/` packages. The items below are ordered by urgency: fix the broken tests first, then pursue the quality improvements.

A 10/10 here means: a stranger could clone the repo, build it on Windows, Linux, or macOS, and trust that everything works the way the README says — including under hostile inputs and in the presence of pre-existing aliases-tool installs. The CI verifies that property on every PR; releases are reproducible and signed; the hot path has a measured ceiling that nobody can silently regress. The list below walks each scoring axis from where it is now to that bar.

Pick from this list in order of cost/value. Items are grouped by axis and labelled `[S]` small (under an hour), `[M]` medium (one to two hours), `[L]` large (half-day plus). The total is roughly two engineer-days of focused work.

---

## Code quality (8 → 10)

### `[S]` Run `go vet ./...` and `golangci-lint run` clean

The current code probably already vets clean, but it hasn't been verified in this sandbox. Add `staticcheck`, `errcheck`, `gosec`, and `revive` via `golangci-lint` and resolve every warning. Most likely findings: unchecked `os.WriteFile` errors in tests, missing `defer file.Close()` patterns, lint warnings about `Errorf` strings.

### `[S]` Sort imports and run `gofumpt`

Stricter than `gofmt`. Avoids drift over time. Add a pre-commit hook plus a CI check that fails on diff.

### `[M]` Replace `os/exec` shell-out for `clip.exe` with a Go-native clipboard library

`commands.go::copyToClipboard` shells out to `clip.exe` (Windows). On Linux/macOS the `y` (yank) action errors. Pull in `golang.design/x/clipboard` or `github.com/atotto/clipboard` so yank works on every supported OS without extra dependencies on the user's system.

### `[M]` Tighten error messages with hints

Several errors are bare wrapping (`fmt.Errorf("read %s: %w", path, err)`). For the top user-visible ones — "unknown alias", "directory not found", "config parse failed" — include the next step in the same line ("run: onix list", "did you mean: <closest>", "edit line N of <file>"). A `findClosestAlias` helper using Levenshtein over the alias names is twenty lines.

### `[L]` Add a `--verbose` / `ONIX_DEBUG=1` mode

Threads a `slog.Logger` through `env` so every command can emit a structured trace on demand. The hot path (`fastResolve`) writes nothing unless the flag is set, so cost is zero in the common case. Helpful for diagnosing weird behaviour reports without asking users for screenshots.

---

## Architecture coherence (8 → 10)

### `[M]` Move `Store`, `Segments`, and `Config` types into `internal/` packages

The repo currently has everything in `package main` because the v2 rewrite collapsed a previous over-decomposition. That was the right correction. Reaching 10 means re-introducing thin `internal/` packages where they reduce coupling without re-creating the situation the cleanup just fixed. A natural split:

- `internal/store` — `Alias`, `Store`, `LoadStore`, `SaveStore`, `ValidateAliasName`
- `internal/segments` — segment registry + parser
- `internal/snippet` — `writePwshShellSnippet`, `writeBashShellSnippet`, the templates

Each package gets its own test file and exports a minimal surface. The `main` package becomes a thin kong wrapper and the hot path. Done carefully (with end-to-end tests as a safety net), this improves testability without re-fragmenting like the v1 code did.

### `[S]` Drop the `env` struct in favour of a context

`env` carries only `Home string`. A `context.Context` with a typed value works fine and lets cancellation flow into long-running plugin invocations. Small refactor, but it removes a single-purpose type from the public surface.

### `[M]` Define a stable plugin-author API

Today the plugin contract is described in `MODULE_PATTERN.md` as a set of environment variables. Bundle that into a tiny `github.com/sadirano/onix-sdk` repo with helper functions (`Context() Ctx`, `Args()`, `ModuleConfig[T]() T`). Plugin authors get IDE autocomplete on the contract instead of having to remember env-var names. Backwards compatible — the env vars stay the source of truth.

---

## Test suite (3 → 10)

> **Blocker:** `go test ./...` currently fails with 4 errors and reports 8.7% total coverage. Fix the three items marked `[BUG]` before anything else — they break CI on every Linux and macOS runner.

### `[BUG][S]` Fix PowerShell golden-file path separator (3 tests failing on Linux/macOS)

`testdata/*.ps1.golden` files contain `'/ONIX_HOME\shell\onix.ps1'` (Windows `\`), but `internal/snippet` emits `filepath.Join` output, which uses `/` on Linux/macOS. This mismatch fails `TestWritePwshShellSnippet_NoActions`, `_WithActions`, and `_WithPlugins` on every non-Windows CI run.

Fix: normalize the path separator in the generated header comment to always use `/` (forward slash is valid in PowerShell paths), then regenerate the golden files with `go test ./... -update`. Alternatively, strip the separator from the golden comparison by replacing the home-placeholder line before diffing.

### `[BUG][S]` Fix E2E Bash test hardcoded binary path (1 test failing everywhere)

`TestE2E_ShellIntegration_Bash` in `e2e_test.go:206` sources a shell snippet that hardcodes `/usr/local/bin/onix`. The binary is not installed there in CI or in a fresh checkout. All other E2E tests use the `OnixExeOverride` mechanism to point at the freshly-built test binary — apply the same pattern here.

### `[BUG][M]` Add dedicated unit tests inside each `internal/` package

All test files (`store_test.go`, `segments_test.go`, `plugins_test.go`, `config_test.go`, `init_test.go`) sit in the root package. When `go test -coverprofile` measures each package independently, all five `internal/` packages (`store`, `segments`, `snippet`, `plugins`, `config`) report **0.0% coverage** — none of their code is counted as tested, even though the root tests exercise it indirectly.

Create `internal/store/store_test.go`, `internal/segments/segments_test.go`, `internal/snippet/snippet_test.go`, `internal/plugins/plugins_test.go`, and `internal/config/config_test.go`. Move the relevant test functions into the package they actually test. This is the single biggest lever on the 8.7% coverage figure.

### `[S]` Verify everything compiles and passes

The minimum bar before pushing for 10/10: `go vet ./... && go test ./... && go test -bench=. -benchmem -run=^$ ./...` on Windows, Linux, and macOS. The three `[BUG]` items above must be resolved first.

### `[M]` Golden-file tests for the shell snippets

The PowerShell and Bash snippet bodies are emitted from templates plus generated wrappers. The golden files already exist in `testdata/`; the fix above resolves the current failures. Extend coverage to the Bash snippet variants (`testdata/bash-no-actions.sh.golden` etc.) using the same `-update` flag pattern. Catches whitespace and quoting regressions that substring tests miss.

### `[M]` Coverage gate at 80%

Add `go test -coverprofile=coverage.out ./...` to the CI workflow and fail the job if any package drops below 80%. The internal package test files (see `[BUG]` above) must exist first or the gate will immediately fail. The plugin-installer code path (`plugin_install.go`, `plugin_cmd.go`) is the second-lowest area after the internal packages — write a fake-git harness so install/update/remove can be exercised hermetically.

### `[L]` End-to-end shell tests

Spawn an actual `pwsh` (on Windows runners) and `bash` (on Linux runners) subprocess, source the generated snippet, and assert on side effects (cwd changed, file opened, tab-completion suggestions returned). The current PowerShell `smoke.ps1` does part of this on Windows; lift it into a `TestE2E` Go test that runs on every CI invocation, not just smoke. Bash variant is straightforward (`expect`-style with `exec.Cmd` writing to stdin).

### `[M]` Benchmark regression gate

The current CI runs `go test -bench=.` but doesn't fail on regressions. Add `benchstat` comparison against `master` (or the last release tag) and gate at 20% slowdown for `BenchmarkHotPath_LookupOnly`. Hot-path regressions land silently today; this catches them at PR time.

### `[S]` Property-based tests for the validators

`ValidateAliasName` rejects a list of characters. A `quick.Check`-style test that generates random strings and asserts that:

- every rejected string contains at least one disallowed character
- every accepted string survives a round-trip through TOML and the alias file

…would have caught the missing-`@` gap before it shipped.

---

## Cross-platform parity (8 → 10)

### `[M]` Wire the explore action on Linux and macOS

`explorer_unix.go::openInExplorer` currently returns "not implemented". Use `xdg-open` on Linux and `open` on macOS:

```go
//go:build !windows

func openInExplorer(target string) error {
    bin := "xdg-open"
    if runtime.GOOS == "darwin" {
        bin = "open"
    }
    return exec.Command(bin, target).Start()
}
```

Then `s acme` works on every supported OS. Add a `doctor` check that warns if neither is on PATH on Linux.

### `[M]` macOS shell-integration support

The Bash/Zsh integration is currently labelled "Linux" in the README but should work on macOS unchanged. Verify on a Mac runner, update the README to say "Linux/macOS", and add `darwin-amd64` and `darwin-arm64` to the release matrix. The Zsh path is more important on macOS (system default) than on Linux — make sure `compinit` guidance is prominent.

### ~~`[M]` Add a CI matrix~~ ✓ DONE

`.github/workflows/test.yml` already runs `windows-latest`, `ubuntu-latest`, `macos-latest` × Go 1.23/1.24 with no branch restriction. No action needed.

### `[S]` Doctor: fish, nushell awareness

Doctor warns "neither .bashrc nor .zshrc found" on Linux but doesn't notice if the user runs fish or nu. Add a soft hint ("you appear to be running fish — see CONTRIBUTING.md for how to source the snippet manually") so users get a path forward.

### `[L]` Daemon mode

`README.md` mentions "an optional daemon mode for sub-millisecond resolution" as future work. The shape is: a long-running `onix` process listens on a named pipe (Windows) or Unix socket (everything else); the shell's `o` function pipes the alias name to it and reads the resolved path. Eliminates the process-spawn floor and brings tab-completion under 50µs. Largest item on the roadmap, but it's the clear path past the current 0.6 ms hot-path ceiling.

---

## Docs (8 → 10)

### `[S]` Add per-command examples to `--help`

kong supports `examples:""` tags. Drop one example per subcommand so `onix add --help` shows `onix add acme C:\projects\acme` inline. Discoverability for first-time users.

### `[M]` Write a CONTRIBUTING.md

What this should cover, in order:

- prerequisites (Go 1.23+, git, optionally golangci-lint)
- build / test / bench commands
- code organisation (one file per concern, see MODULE_PATTERN.md)
- the hot-path constraint (under 1 ms for `BenchmarkHotPath_LookupOnly`)
- the snippet-generation contract and how to add new built-in functions
- how to write a plugin (link to MODULE_PATTERN.md)
- release process

### `[M]` Architecture diagram in the README

A simple mermaid diagram showing: shell ⇄ snippet (generated) ⇄ onix binary ⇄ aliases.toml/config.toml/segments.toml/plugins.toml. Plus the plugin dispatch path. Eight boxes, ten arrows. Worth ten paragraphs.

### `[S]` A troubleshooting section indexed by symptom

"`o foo` does nothing" → check the snippet pin. "Tab completion suggests stale names" → run `onix install-actions`. "Plugin install hangs" → check git is on PATH. Five common ones cover 90% of the support load.

### `[S]` Godoc-style examples on public functions

Functions like `ParseSegmentedAlias`, `ResolveSegment`, `ValidateAliasName` benefit from runnable `Example` tests that double as docs. Increases coverage and surfaces them on pkg.go.dev.

---

## Repo hygiene (7 → 10)

### ~~`[S]` Configure dependabot~~ ✓ DONE

`.github/dependabot.yml` already exists. No action needed.

### ~~`[M]` Set up `govulncheck` in CI~~ ✓ DONE

`govulncheck ./...` already runs as a CI step. `gosec` is still missing — add it to the `golangci-lint` run (see Code quality section).

### `[M]` Signed releases

`cosign sign-blob` on the binary, publish the signature alongside the zip. Document the verification command in the README. Becomes more important if anyone ever distributes onix through Scoop or Homebrew.

### ~~`[S]` Multi-arch release matrix~~ ✓ DONE

`release.yml` already builds `windows-amd64`, `linux-amd64`, `linux-arm64`, `darwin-amd64`, `darwin-arm64`. No action needed.

### `[S]` Tag the schema versions

`aliases.toml`, `config.toml`, `segments.toml`, `plugins.toml` are user-facing files. Each should have a `version = N` field that the loader checks against; a future schema change writes a migration step rather than silently changing meaning. Cheap insurance.

### `[S]` `.editorconfig`

Two-space indent for YAML, tabs for Go, LF line endings. Avoids the "my editor reformatted everything" PR.

### `[S]` `CODEOWNERS` and `SECURITY.md`

Standard GitHub-recommended files. CODEOWNERS auto-requests review; SECURITY.md tells users where to report vulnerabilities without filing a public issue.

### `[S]` `git rm --cached onix.exe` and commit

The first cleanup pass removed it from the working tree, but a stale `.git/index.lock` blocked the index update. Run this on a clean checkout to close the loop.

---

## Polish items (across all axes)

### `[S]` A `--json` flag on `list`, `version`, `doctor`

Scripts can parse onix output reliably. The current human-friendly tabwriter format is unscriptable. Five lines per command via `encoding/json`.

### `[M]` Configurable name conventions

The README hard-codes the `o`, `n`, `s`, `y`, `r` shortcuts. Some users want different ones. Allow `config.toml` to declare alternate built-in names:

```toml
[shortcuts]
o = "go"     # use 'go' instead of 'o' as the navigate shortcut
```

Re-generate the snippet against the override. Defensive: refuse names that shadow common commands like `cd`, `ls`, `git`.

### `[M]` `onix import` from common alternatives

`zoxide`, `autojump`, `wd`, `j`. Each stores aliases in a known format; a one-shot import makes onix a "try this and migrate later" proposition for users of those tools.

### `[L]` Telemetry-free crash reporter

When onix panics, print a short message with a `--report` flag suggestion that bundles up the redacted state (alias names hashed, paths replaced with `<home>/...`, OS, Go version) into a snippet the user can paste into a GitHub issue. No network calls — the user has to opt in by pasting. Removes the "I have no repro" support load.

---

## Order of operations

If you want to take this in chunks:

0. **Right now (30 min) — unblock CI:** fix the PowerShell golden-file path separator (`[BUG][S]`), fix the E2E Bash hardcoded path (`[BUG][S]`). These two items cause 4 test failures on every Linux/macOS run. Without them, CI is broken on two of three OS runners. Score: 6.2 → 7.0.
1. **This week (half day):** add dedicated test files inside each `internal/` package (`[BUG][M]`); wire `xdg-open` and macOS `open` for the explore action; add the coverage gate. Score: 7.0 → 8.0.
2. **Within a sprint (two days):** end-to-end shell tests, benchmark regression gate, `golangci-lint` clean, `gofumpt`, CONTRIBUTING.md updates, troubleshooting section, schema versioning, signed releases. Score: 8.0 → 9.2.
3. **Long-tail (sometime):** daemon mode, plugin SDK, `--json` outputs, importers, crash reporter, Levenshtein hint on unknown alias. Score: 9.2 → 10.

Items in tier 0–2 are mostly mechanical; tier 3 is product work that should land when there's a concrete user pulling for it.
