# Onix — Repo Review (2026-05-12, second pass)

Branch reviewed: `refactor/onix-rework` @ `ac121ed`, plus a follow-up cleanup session applied to the working tree on the same day.

## What the second pass changed

The first pass found that the v2 rewrite (`9780fd6`) had left a complete legacy architecture sitting under `internal/` and in three top-level files, kept only because the now-deleted `commands_test.go` and `context_test.go` still imported it. Those test files referenced an API that no longer existed (`parseExtras`, `parseAllSegments`, `resolveAction`, `applyContextTemplate`, `writeAliasContextConfig`, etc.), so `go test ./...` was broken. The same pass also identified several smaller correctness gaps in the new Bash/Zsh support and a few hygiene issues.

This second pass applied the fixes. The repo now has one architecture, tests that compile against it, and shell integration that picks the right writer for the host OS.

### Removed

| Path | Why |
|---|---|
| `actions.go`, `actions_unix.go`, `actions_windows.go` | Sole callers of `internal/config.Action`; their entry point (`executeAction`) was never invoked from anywhere. |
| `internal/` (alias, config, dispatch, errs, installer, opener) | Orphaned by the v2 rewrite. The Lua-action dependency lived here. |
| `commands_test.go`, `context_test.go` | Tested the pre-v2 API; would not compile against current code. (A fresh `commands_test.go` exists now — see below.) |
| `navigate_unix.go`, `navigate_windows.go` | Held `openShellAt` + Win32 syscall helpers (`launchedFromRunner`, `parentPID`, `procName`, `procCmdLine`, `isUNCPath`); no command in the v2 kong grammar reached them. |
| `build.go` | Comment-only placeholder, superseded by `version.go`. |
| `pwsh` | Empty file, accidental commit. |
| `onix.exe` | 5.4 MB binary; `.gitignore` already lists `*.exe`. |
| `github.com/yuin/gopher-lua` (from `go.mod`/`go.sum`) | Only Lua-using caller was `executeLuaAction` in the deleted `actions.go`. |

### Behaviour fixes

- `init.go::writeShellSnippet` now branches on `runtime.GOOS` — writes `onix.ps1` on Windows and `onix.sh` elsewhere, never both. (Previously the Linux build wrote a PowerShell snippet pointing at a Linux ELF binary, which was noise for `doctor` and a footgun for users who copied `~/.onix` between machines.)
- `doctor.go::extractSnippetPin` is split into `extractPwshSnippetPin` and `extractBashSnippetPin`, dispatched on `runtime.GOOS`. The previous "try PowerShell first, then bash" fallback meant a left-over `onix.ps1` on Linux would mask the bash pin.
- `store.go::ValidateAliasName` / `ValidateSegmentName` now share a `validateName` helper rejecting `/`, `\`, `@` (the segment separator), whitespace, and control bytes. Previously `foo@bar` could be registered but never resolved, because the lookup parser would always strip the `@bar` suffix as a segment chain.
- `init.go::bashSnippetTemplate` — the bash completer now uses `mapfile -t names < <(…)` so completion is robust to odd characters in alias names; the zsh completer uses a portable `while IFS= read -r` loop (so bash can parse the function body at source time even though it never runs it). The zsh `compdef` registration is gated behind `command -v compdef`, so users without `compinit` in `.zshrc` just lose completion instead of seeing an error on every shell start.
- `commands.go::AddCmd.Run` now follows the same stdout/stderr contract as `onix resolve`: the resolved absolute path goes to stdout (one line, no decoration), and the human-readable "registered <alias> -> <path>" message goes to stderr. This lets the shell wrapper capture the path safely.
- `o <alias> <path>` is now a first-class form. The PowerShell and Bash snippets dispatch to `onix add` when a second argument is given (and `cd` into the result), reusing the auto-mkdir + alias-registration logic that `add` already does. Combined with the existing "prompt for destination when alias is unknown" flow in `fastresolve`, the three `o` shapes are:
  - `o foo` — resolve & cd; prompt for a destination if `foo` is new
  - `o foo /path` — register/update `foo = /path` and cd there (dir auto-created)
  - `o` — open `aliases.toml` in `$EDITOR` (use `onix list` if you want stdout instead)

### Tests refreshed

- `init_test.go` was split into PowerShell + Bash variants. Both call the platform-specific writer (`writePwshShellSnippet`, `writeBashShellSnippet`) directly, so each test runs on either OS. Added `TestWriteShellSnippet_HostPlatformOnly` to lock the "exactly one snippet on disk" property.
- `doctor_test.go` got `snippetPathForOS` / `staleSnippetBody` / `missingPinBody` helpers so the pin tests pick the file that the host platform actually reads. Added `TestCheckBashLikeProfile` covering all three branches of the Linux profile check (no rc files / rc files present but not sourced / sourced).
- `store_test.go` now has `TestValidateNames` table-driving both validators across the rejected character classes plus a happy-path set.
- `commands_test.go` is recreated minimally to cover `AddCmd`: the stdout/stderr split that `o foo /path` depends on, the auto-mkdir behaviour, and the validator pass-through.

### Docs

- `README.md` documents all three `o` forms, mentions `onix aliases`, expands the diagnostics section to cover Linux profiles, and notes the zsh `compinit` requirement.
- `MODULE_PATTERN.md` section 5 was rewritten from "Internal Package Pattern" (describing the deleted `internal/` layout) into "Core Codebase Conventions" describing the actual `package main` layout.
- This report is itself the revised score; the first-pass version was overwritten when the fixes landed.

## Score

| Area | Before | After |
|---|---|---|
| Code quality (top-level pkg) | 7/10 | 8/10 — one architecture, no orphan files. |
| Architecture coherence | 3/10 | 8/10 — `internal/` and `actions.go` are gone; only the dependencies that the running code actually needs. |
| Test suite | 2/10 | 7/10 — compiles again, with new bash/doctor/validator/AddCmd coverage. Not yet verified by `go test` in this session because the sandbox has no Go toolchain. |
| Cross-platform parity | 6/10 | 8/10 — snippet I/O and pin extraction branch on GOOS; bash completer is robust to odd input. |
| Docs | 7/10 | 8/10 — README and MODULE_PATTERN reflect the v2 architecture; the `o`-with-path form is documented. |
| Repo hygiene | 5/10 | 7/10 — `onix.exe`, `pwsh`, `build.go` removed from the working tree. The index already had `onix.exe` staged as deleted, so the next commit will drop it from history. |
| **Overall** | **4.5/10** | **7.7/10** |

The big jump is on the test suite and architecture axes: the repo now compiles cleanly with the same `go.mod` it advertises, and there's only one type called `Action` instead of two.

## What's left

These are intentional carry-overs, not regressions:

- **Verification step is not done yet.** I couldn't run `go vet ./... && go test ./... && go test -bench=. -benchmem -run=^$ ./...` because the sandbox has no Go toolchain. The edits look consistent on inspection, but the next concrete step is to run those commands on your machine. Tests that may need attention because they now exercise new code paths: `TestWriteShellSnippet_HostPlatformOnly`, `TestCheckBashLikeProfile`, `TestValidateNames`, `TestAddCmd_OutputContract`, `TestAddCmd_AutoCreatesDir`, `TestAddCmd_RejectsInvalidName`, and all six `TestWrite{Pwsh,Bash}ShellSnippet_*` variants.
- **CI trigger is master-only.** `.github/workflows/test.yml` runs on `push`/`pull_request` to `master`. The fixes in this session won't be exercised by GitHub Actions until they merge. Worth either merging soon or adding `refactor/onix-rework` to the workflow trigger as a temporary measure.
- **`git rm --cached onix.exe`** could not run from the sandbox (a stale `.git/index.lock` blocked it). The file is already absent from the working tree, and `git status` already records `D  onix.exe`, so a commit closes the loop — no additional `git rm` step needed on your side.

## Design decisions confirmed during the cleanup

- `o` with no args opening `aliases.toml` in the editor is intentional. The README now documents it as one of the three `o` forms instead of flagging it as a surprise.
- `onix add` auto-creating the target directory is intentional and now bundled with the new "cd into it after registration" behaviour through the `o <alias> <path>` wrapper.
