# Onix — Manual Test Plan

Work through sections in order. Each test builds on the state left by the previous ones.

---

## Test Environment Setup

Before starting, create the following directory structure once:

```
mkdir C:\temp\onix-test
mkdir C:\temp\onix-test\src
echo hello world > C:\temp\onix-test\hello.txt
echo package main > C:\temp\onix-test\src\main.go
echo func handleRequest() {} >> C:\temp\onix-test\src\main.go
echo func handleAuth() {} > C:\temp\onix-test\src\auth.go
```

Everything is tested against alias `tst` → `C:\temp\onix-test`.

---

## 1 — Init & Shortcuts

- [ ] **T-01** `onix init`
  - Creates `~/.onix/`, `~/.onix/modules/`, `~/.onix/bin/`
  - Writes `~/.onix/config.toml` with starter content
  - Prints "Created" path and PATH instructions
  - Running again: prints "Config already exists"

- [ ] **T-02** `onix init` (second run)
  - Prints "Config already exists: ..." instead of "Created"
  - Directories already exist — no error

- [ ] **T-03** `onix shortcuts`
  - Creates `o.cmd c.cmd s.cmd n.cmd y.cmd f.cmd r.cmd sg.cmd ff.cmd` in `~/.onix/bin/`
  - Prints each shortcut name as it's written
  - Prints "Shortcuts installed in ..."
  - Attempts to add `~/.onix/bin/` to user PATH via PowerShell

- [ ] **T-04** `onix shortcuts` (second run)
  - Same output — idempotent, overwrites existing wrappers without error

---

## 2 — Alias Registration

- [ ] **T-05** `onix -a tst -d C:\temp\onix-test`
  - Writes `tst=C:\temp\onix-test` to `~/.onix/aliases`
  - Prints `Registered "tst" -> "C:\temp\onix-test"`
  - No shell opens (invoked as `onix`, not `o`/`c`)

- [ ] **T-06** `o -a tst2 -d C:\temp\onix-test`
  - Writes `tst2=C:\temp\onix-test` to `~/.onix/aliases`
  - Prints `Registered "tst2" -> "C:\temp\onix-test"`
  - **Also opens a new cmd.exe shell at `C:\temp\onix-test`** — unique to `o`/`c`

- [ ] **T-07** `c -a tst3 -d C:\temp\onix-test`
  - Same as T-06 but invoked as `c`
  - Registers and opens shell — `c` and `o` behave identically on registration

- [ ] **T-08** `o -a tst -d C:\temp\onix-test -s src`
  - Overwrites `tst` entry: `tst=C:\temp\onix-test\src`
  - Prints updated path
  - Opens shell at `C:\temp\onix-test\src`

- [ ] **T-09** `onix -a tst -d C:\temp\onix-test`
  - Restores `tst` to the root — re-registration works as upsert

- [ ] **T-10** `onix -a tst` (missing `-d`)
  - Prints: `onix: usage: onix -a <alias> -d <destination>`
  - Exits non-zero

- [ ] **T-11** `o` (no args)
  - Opens `~/.onix/aliases` in `$EDITOR` (defaults to nvim)
  - The file should contain `tst`, `tst2`, `tst3` entries from prior tests

---

## 3 — Open Shell

- [ ] **T-12** `o tst`
  - Opens `cmd.exe /K` at `C:\temp\onix-test`
  - The shell prompt shows that directory
  - Closing the shell returns to original context

- [ ] **T-13** `c tst`
  - Identical to T-12 — `o` and `c` are the same

- [ ] **T-14** `o tst -s src`
  - Opens `cmd.exe /K` at `C:\temp\onix-test\src`

- [ ] **T-15** `o tst -s nonexistent`
  - Creates `C:\temp\onix-test\nonexistent\` (MkdirAll)
  - Opens shell there

---

## 4 — Explorer

- [ ] **T-16** `s tst`
  - Opens Explorer window at `C:\temp\onix-test`
  - Exits immediately (fire-and-forget, no wait)

- [ ] **T-17** `s tst -s src`
  - Opens Explorer at `C:\temp\onix-test\src`

---

## 5 — Editor

- [ ] **T-18** `n tst`
  - Runs `nvim .` (or `$EDITOR .`) with working dir `C:\temp\onix-test`
  - Waits for editor to close before returning

- [ ] **T-19** `n tst -s src`
  - Runs editor with working dir `C:\temp\onix-test\src`

---

## 6 — Open File

- [ ] **T-20** `f tst hello.txt`
  - Runs `nvim hello.txt` with working dir `C:\temp\onix-test`
  - Opens `C:\temp\onix-test\hello.txt`

- [ ] **T-21** `f tst -s src main.go`
  - Runs `nvim main.go` with working dir `C:\temp\onix-test\src`
  - Opens `C:\temp\onix-test\src\main.go`

- [ ] **T-22** `f tst` (no file)
  - Falls back to `nvim .` — identical to `n tst`
  - No error

- [ ] **T-23** `f tst src\main.go`
  - Works with relative path containing separator
  - Opens `C:\temp\onix-test\src\main.go`

---

## 7 — Run Command

- [ ] **T-24** `r tst "dir"`
  - Runs `cmd /C dir` with working dir `C:\temp\onix-test`
  - Output (hello.txt, src\) is printed to the terminal
  - Exits 0

- [ ] **T-25** `r tst "echo test"`
  - Prints `test` to terminal, exits 0

- [ ] **T-26** `r tst -s src "dir"`
  - Runs `dir` in `C:\temp\onix-test\src`
  - Shows main.go and auth.go

- [ ] **T-27** `r tst "exit 42"`
  - Command exits with code 42
  - onix exits with the same code 42 (propagated)

- [ ] **T-28** `r tst` (no command)
  - Prints: `onix: usage: onix <alias> -r "<command>"`
  - Exits non-zero

---

## 8 — Print Path

- [ ] **T-29** `y tst`
  - Prints `C:\temp\onix-test` to stdout
  - Clipboard contains `C:\temp\onix-test` (paste to confirm — no extra output)
  - `%ONIX_LAST%` is set to that path (open a new shell and check `echo %ONIX_LAST%`)

- [ ] **T-30** `y tst -s src`
  - Prints `C:\temp\onix-test\src`
  - Clipboard contains `C:\temp\onix-test\src`

- [ ] **T-31** `y tst | clip`
  - Path is copied to clipboard via both the builtin (silent) and the pipe
  - Paste confirms correct value

---

## 9 — Sub-Alias Navigation

Setup for this section (run once):

```
mkdir C:\temp\onix-test\anexos
mkdir C:\temp\onix-test\testes
mkdir "C:\temp\onix-test\task\12345"
echo an=anexos > %USERPROFILE%\.onix\subdirs.env
echo ts=testes >> %USERPROFILE%\.onix\subdirs.env
```

Ensure `tst` → `C:\temp\onix-test` is registered.

### 9a — Subdir Registry

- [ ] **T-32a** `y an@tst`
  - Prints `C:\temp\onix-test\anexos`
  - Creates the directory if it doesn't exist (MkdirAll)

- [ ] **T-32b** `y ts@tst`
  - Prints `C:\temp\onix-test\testes`

- [ ] **T-32c** `y outros@tst`
  - Prints `C:\temp\onix-test\outros` (literal fallback — not in registry)

- [ ] **T-32d** `y an@tst -s sub`
  - Prints `C:\temp\onix-test\anexos\sub` (`-s` stacks after `@` segment)

- [ ] **T-32e** `s an@tst`
  - Opens Explorer at `C:\temp\onix-test\anexos`

- [ ] **T-32f** `n an@tst`
  - Opens editor at `C:\temp\onix-test\anexos`

- [ ] **T-32g** Local subdir registry override
  - Create `C:\temp\onix-test\subdirs.env` with content `an=local-anexos`
  - `y an@tst` → `C:\temp\onix-test\local-anexos` (local file wins over global)
  - Remove the local file to restore global behaviour

### 9b — Context Segments

Setup:

```
set CLIENT_ID=acme
set TASK_ID=12345
onix ctx client env CLIENT_ID {value}
onix ctx task   env TASK_ID   task/{value}
```

- [ ] **T-33a** `onix ctx client`
  - Prints: `source=env`, `var=CLIENT_ID`, `template={value}`

- [ ] **T-33b** `y client@tst`
  - Prints `C:\temp\onix-test\acme`

- [ ] **T-33c** `y task@client@tst`
  - Prints `C:\temp\onix-test\acme\task\12345`

- [ ] **T-33d** `s task@client@tst`
  - Opens shell at `C:\temp\onix-test\acme\task\12345`

- [ ] **T-33e** `n task@client@tst`
  - Opens editor at `C:\temp\onix-test\acme\task\12345`

- [ ] **T-33f** `y task@client@tst -s config`
  - Prints `C:\temp\onix-test\acme\task\12345\config`

- [ ] **T-33g** `ONIX_DEBUG=1 y task@client@tst`
  - Stderr includes lines like:
    `[ONIX] segment "client" → template="{value}" value="acme" → "acme"`
    `[ONIX] segment "task" → template="task/{value}" value="12345" → "task/12345"`

- [ ] **T-33h** `onix ctx client --clear`
  - Prints: `Context for "client" cleared`
  - `onix ctx client` → prints: `no context configured for "client"`
  - `y client@tst` → falls back to subdir registry (or literal `client` dir)

- [ ] **T-33i** cmd source
  - `onix ctx branch cmd "git rev-parse --abbrev-ref HEAD" {value}`
  - Run inside a git repo: `y branch@<alias>` → current branch name as path segment

- [ ] **T-33j** file source
  - `echo feature-x > %USERPROFILE%\.onix\current-sprint`
  - `onix ctx sprint file ~/.onix/current-sprint {value}`
  - `y sprint@tst` → `C:\temp\onix-test\feature-x`

- [ ] **T-33k** Unset env var → error
  - `set CLIENT_ID=` (clear the variable)
  - `y client@tst` with env source configured → `onix: resolve context: context env var "CLIENT_ID" is not set`
  - Exits non-zero

---

## 10 — Interactive Alias Creation

These tests require fzf to be installed. If not, fall through to the plain-text prompt.

- [ ] **T-32** `o newone` (alias `newone` not registered)
  - fzf picker opens pre-seeded with "newone"
  - Navigate and select `C:\temp\onix-test`
  - Prints `Registered "newone" -> "C:\temp\onix-test"`
  - Opens cmd.exe there (action defaults to shell)

- [ ] **T-33** `o newone` (Esc in fzf, no fzf fallback: blank input at prompt)
  - If fzf: Esc dismisses picker, falls back to text prompt
  - Blank input at prompt → `onix: no destination provided`, exits non-zero

- [ ] **T-34** `y newone` (after T-32)
  - Resolves to `C:\temp\onix-test` — alias is now persisted

---

## 11 — Content Search (`sg`)

Requires `rg` (ripgrep) and `fzf` in PATH.

- [ ] **T-35** `sg tst handleRequest`
  - ripgrep searches `C:\temp\onix-test` for "handleRequest"
  - fzf opens with match: `src/main.go:1:...`
  - Preview shows `src/main.go` syntax-highlighted at line 1 (requires `bat`)
  - Press Enter → editor opens at that exact line

- [ ] **T-36** `sg tst handle` (multiple matches)
  - Both handleRequest and handleAuth appear in fzf
  - Tab to multi-select, Enter → editor opens both

- [ ] **T-37** `sg tst -s src handle`
  - Searches only `C:\temp\onix-test\src`
  - Same matches as T-36 but scope is narrowed

- [ ] **T-38** `sg tst nomatch`
  - ripgrep exits 1 (no matches)
  - Prints `No matches found.`, exits 0

- [ ] **T-39** `sg tst` (no query)
  - Prints: `onix: usage: sg <alias> <query>`
  - Exits non-zero

- [ ] **T-40** `sg tst handle` then Esc in fzf
  - fzf is dismissed, nothing opens, exits 0

---

## 12 — File Search (`ff`)

Requires `fzf`. `es` (Everything) gives instant results; walk fallback is used if absent.

- [ ] **T-41** `ff tst`
  - fzf opens listing all files under `C:\temp\onix-test`
  - At least: `hello.txt`, `src\main.go`, `src\auth.go`
  - Press Enter → opens selected file(s) in editor

- [ ] **T-42** `ff tst auth`
  - fzf opens pre-filtered to "auth"
  - `src\auth.go` prominently listed
  - Press Enter → editor opens `auth.go`

- [ ] **T-43** `ff tst -s src`
  - Searches only `C:\temp\onix-test\src`
  - Lists `main.go` and `auth.go`

- [ ] **T-44** `ff tst auth` then Ctrl+E
  - Explorer opens with `auth.go` selected in its containing folder (`src\`)

- [ ] **T-45** `ff tst` then Esc
  - Dismissed, nothing opens, exits 0

---

## 13 — Subdir Combinations Matrix

Quick sweep to confirm `-s` works uniformly across all actions.

| Test | Command | Expected |
|------|---------|----------|
| T-46 | `o tst -s src` | shell at `\onix-test\src` |
| T-47 | `s tst -s src` | Explorer at `\onix-test\src` |
| T-48 | `n tst -s src` | editor at `\onix-test\src` |
| T-49 | `y tst -s src` | prints `C:\temp\onix-test\src` |
| T-50 | `f tst -s src auth.go` | opens `\onix-test\src\auth.go` |
| T-51 | `r tst -s src "dir"` | lists `\onix-test\src` contents |
| T-52 | `sg tst -s src handle` | searches only `\onix-test\src` |
| T-53 | `ff tst -s src` | files only from `\onix-test\src` |

---

## 14 — Visual Themes

- [ ] **T-54** `onix themes list`
  - Lists all `onix.visual.*.toml` files found next to `onix.exe`
  - Current active theme is marked `(current)`

- [ ] **T-55** `onix theme cinematic-wide`
  - Copies `onix.visual.cinematic-wide.toml` → `onix.visual.toml`
  - Prints `Applied onix.visual.cinematic-wide.toml`

- [ ] **T-58** `onix theme` (no name — interactive)
  - fzf picker opens listing theme filenames
  - Preview pane shows theme file contents
  - Select a theme, Enter → applied

- [ ] **T-59** `onix theme nosuchtheme`
  - Prints: `onix: theme "nosuchtheme" not found`
  - Exits non-zero

- [ ] **T-60** `ff tst` after changing theme
  - fzf opens with the newly applied prompt/layout style visible

---

## 15 — Module Management

These tests require network access and Go installed. Use a real public repo for install tests.

- [ ] **T-61** `onix list` (empty config)
  - Prints `No modules declared. Add one with: onix add <user/repo>`

- [ ] **T-62** `onix add sadirano/onix-img`
  - Appends `[[module]]` block to `~/.onix/config.toml`
  - Prints `Added "onix-img" (sadirano/onix-img) — run 'onix install onix-img'`

- [ ] **T-63** `onix list` (after T-62)
  - Shows one row: `onix-img  sadirano/onix-img  main  not installed`

- [ ] **T-64** `onix add sadirano/onix-img` (duplicate)
  - Prints: `onix: module "onix-img" already in config`
  - Config is unchanged

- [ ] **T-65** `onix install onix-img`
  - Clones `https://github.com/sadirano/onix-img` into `~/.onix/modules/onix-img/`
  - Runs `go build -o onix-img.exe .`
  - Writes `~/.onix/bin/onix-img.cmd` wrapper
  - Prints `✓ onix-img -> ...`

- [ ] **T-66** `onix list` (after T-65)
  - Status column shows `installed`

- [ ] **T-67** `onix install onix-img` (re-install)
  - Pulls latest, rebuilds, overwrites wrapper — idempotent

- [ ] **T-68** `onix install nonexistent`
  - Prints: `onix: module "nonexistent" not found in config`
  - Exits non-zero

- [ ] **T-69** `onix update onix-img`
  - Fetches + pulls latest, rebuilds
  - Prints `update onix-img`

- [ ] **T-70** `onix update` (all)
  - Updates every enabled module
  - Skips disabled ones with `skip <name> (disabled)`

- [ ] **T-71** `onix update nonexistent`
  - Prints: `onix: module "nonexistent" not in config`
  - Exits non-zero

- [ ] **T-72** `onix remove onix-img`
  - Deletes `~/.onix/modules/onix-img/`
  - Deletes `~/.onix/bin/onix-img.cmd`
  - Removes the `[[module]]` block from config
  - Prints `Removed onix-img.`

- [ ] **T-73** `onix list` (after T-72)
  - Prints `No modules declared.` again

- [ ] **T-74** `onix remove nonexistent`
  - Prints: `onix: module "nonexistent" not found in config`
  - Exits non-zero

---

## 16 — Disabled Module

- [ ] **T-75** Add a module, then in `~/.onix/config.toml` set `enabled = false`
  - `onix install` → prints `skip <name> (disabled)`, skips it
  - `onix update` → same skip behaviour
  - `onix list` → status column shows `disabled`

---

## 17 — Module Dispatch

Requires a module to be installed (repeat T-62–T-65 if removed).

- [ ] **T-76** `onix-img tst` (via generated wrapper)
  - `.cmd` wrapper sets `ONIX_MODULE=onix-img`, calls `onix tst`
  - onix resolves `tst` → `C:\temp\onix-test`
  - Module binary runs with `ONIX_TARGET`, `ONIX_ALIAS`, `ONIX_MODULE`, `ONIX_MODULE_CONFIG` set
  - Module behaviour depends on the module itself

- [ ] **T-77** `onix-img` (no alias)
  - Prints: `onix: usage: onix-img <alias> [args...]`
  - Exits non-zero

---

## 18 — Config & Environment Overrides

- [ ] **T-78** `set ONIX_DEBUG=1 && o tst`
  - Prints to stderr: `[ONIX] build_version=... onix_exe=... modified_at=...`
  - Prints to stderr: `[ONIX] module=... alias=...` if dispatch path
  - Normal action still executes

- [ ] **T-79** `set ONIX_TIMING=1 && o tst`
  - After the action, prints timing table to stderr:
    ```
    [ONIX TIMING] ---
      phase               delta     elapsed
      config loaded       ...       ...
      ...
    [ONIX TIMING] ---
    ```

- [ ] **T-80** `set ONIX_ENV=C:\temp\custom.env && o tst`
  - onix reads aliases from `C:\temp\custom.env` instead of `~/.onix/aliases`
  - `tst` not found → interactive picker (alias only exists in `~/.onix/aliases`)
  - Unset `ONIX_ENV` afterward to restore

- [ ] **T-81** Set `alias_file = "C:\temp\custom.env"` in `~/.onix/config.toml [settings]`
  - Same effect as T-80 via config instead of env var
  - `ONIX_ENV` still takes precedence if both are set — verify by setting both to different paths

- [ ] **T-82** Set `editor = "notepad"` in `~/.onix/config.toml [settings]`
  - `n tst` opens Notepad instead of nvim
  - Restore to `""` when done

- [ ] **T-83** `set EDITOR=notepad && n tst`
  - Same result as T-82 via env var
  - Config `editor` takes precedence over `$EDITOR` — verify by setting both

---

## 19 — Edge Cases

- [ ] **T-84** `o unknownalias` with no fzf and blank prompt input
  - Prompt: `Destination for "unknownalias": `
  - Press Enter with no input
  - Prints: `onix: no destination provided`, exits non-zero

- [ ] **T-85** `o tst` when `~/.onix/aliases` does not exist
  - onix creates an empty alias map, proceeds to interactive picker
  - (Delete or rename the file to test this)

- [ ] **T-86** Corrupt `~/.onix/config.toml` (add invalid TOML)
  - `o tst` prints: `onix: load config: ...` and exits non-zero
  - Restore valid config before continuing

- [ ] **T-87** `n` (shortcut with no alias)
  - The `-n` flag is injected but no alias is provided
  - onix tries to resolve `"-n"` as an alias, fails
  - Falls back to interactive picker seeded with "-n" — expected but not useful
  - Note: `o` with no args is the correct no-arg shortcut (opens alias file)

- [ ] **T-88** `r tst "exit 0"`
  - onix exits with code 0 — propagation works for success

- [ ] **T-89** `r tst "nonexistentcmd"`
  - `cmd /C nonexistentcmd` fails
  - onix exits with that non-zero code

- [ ] **T-90** `sg tst handle` when `rg` is not in PATH
  - Prints: `onix: sg requires ripgrep (rg) in PATH`
  - Exits non-zero

- [ ] **T-91** `ff tst` when `fzf` is not in PATH
  - Prints: `onix: ff requires fzf in PATH`
  - Exits non-zero

- [ ] **T-92** `onix theme` when no `onix.visual.*.toml` files exist next to `onix.exe`
  - Prints: `onix: no theme files found in <dir>`
  - Exits non-zero

---

## Quick Smoke Run (All-in-One)

If you want a fast sanity check without running every test, run these in order — they cover all major code paths in under two minutes:

```
onix init
onix shortcuts
onix -a tst -d C:\temp\onix-test
y tst
o tst -s src
s tst
n tst
f tst hello.txt
r tst "dir"
sg tst handle
ff tst auth
onix add sadirano/onix-img
onix list
onix install onix-img
onix remove onix-img
```
