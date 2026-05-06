# onix — the guide

Your project lives at `C:\Users\dev\projects\client-work\acme\backend\api\v2`.

You type `o acme` instead.

---

## First-time Setup

```
onix init
onix shortcuts
```

`init` creates `~/.onix/` and writes a starter `config.toml`.
`shortcuts` drops `.cmd` wrappers (`o`, `n`, `s`, `f`, `r`, `y`, `sg`, `ff`) into `~/.onix/bin/`.
Add `~/.onix/bin/` to your `PATH` once and every shortcut becomes a first-class command.

---

## Register an Alias

One command, one time. The alias is yours forever.

```
o -a acme -d C:\Users\dev\projects\client-work\acme\backend\api\v2
```

That writes `acme=C:\Users\dev\projects\client-work\acme\backend\api\v2` to `~/.onix/aliases`.
Run `o` with no arguments to open that file in your editor if you ever want to edit it by hand.

> Override the path with `ONIX_ENV` or `alias_file` in `~/.onix/config.toml`.

---

## Jump Into a Directory

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2
```

**With onix:**
```
o acme
```

Opens `cmd.exe` in that directory. Works from anywhere — no matter where your current shell is.

Need to land in a subdirectory?

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2\src\handlers
```

**With onix:**
```
o acme -s src\handlers
```

The `-s` flag appends a subpath to the resolved alias before opening the shell.

### Unknown alias — interactive pick

If you type an alias that hasn't been registered yet, onix opens an fzf directory picker
pre-seeded with your query (uses Everything's `es` if installed, drive-walk otherwise).
Pick a folder, press Enter — the alias is registered automatically. No separate `-a` step needed.

---

## Sub-Alias Navigation

Sub-alias navigation lets you jump into a specific subdirectory inside an alias without
registering a new alias for every path combination you visit.

### Subdir shortcuts — `sub@alias`

```
s an@sms
```

Resolves `sms` (the alias), then looks up `an` in the two-level subdir registry and
appends the result. If `an=anexos` is in the registry, you land in `<sms>/anexos`.

**Subdir registry** — two levels, case-insensitive lookup:

| File | Scope |
|------|-------|
| `~/.onix/subdirs.env` | Global — available for all aliases |
| `<alias_dir>/subdirs.env` | Local — overrides global for entries with the same key |

Both use the same `key=value` format as the alias file. Local entries win when a key
appears in both files. If a name isn't in either registry, it's used literally as the
directory name — raw directory names always work without registration.

```
# ~/.onix/subdirs.env
an=anexos
doc=documentacao
ts=testes
cfg=configuracoes
```

```
s an@sms        → shell in <sms>/anexos
n doc@sms       → editor in <sms>/documentacao
y ts@sms        → print path of <sms>/testes
o outros@sms    → explorer in <sms>/outros  (literal fallback)
```

All existing action shortcuts (`s`, `n`, `o`, `y`, `r`, `f`) work with this form. The
`-s` flag still stacks on top if needed:

```
s an@sms -s subdir      → <sms>/anexos/subdir
```

### Context layers — `seg1@seg2@alias`

When your directory tree has a rotating dimension (a current task, client, branch, sprint…)
you can inject its value into the path without typing it every time.

```
s task@client@place
```

Segments are processed right-to-left (closest to the alias first). Each segment either:
- Has a **context config** → resolves a runtime value and applies a path template
- Has no context config → falls back to the subdir registry (or literal name)

**Configure a segment's context:**

```
onix ctx client env CLIENT_ID {value}         # read from %CLIENT_ID% env var
onix ctx task   env TASK_ID   task/{value}    # template: /task/<taskID>
```

| Subcommand | Syntax | Effect |
|------------|--------|--------|
| Set (env)  | `onix ctx <seg> env <var> [template]` | Read context from environment variable |
| Set (cmd)  | `onix ctx <seg> cmd <command> [template]` | Run command, use its stdout |
| Set (file) | `onix ctx <seg> file <path> [template]` | Read first line of a file |
| Show       | `onix ctx <seg>` | Print the current config for this segment |
| Clear      | `onix ctx <seg> --clear` | Remove the config |

**Template syntax** — the `[template]` argument controls what the segment contributes to
the path. `{value}` is replaced with the resolved context value:

| Template | Resolved value | Path contribution |
|----------|---------------|-------------------|
| *(omitted)* | `12345` | `12345` |
| `{value}` | `12345` | `12345` |
| `task/{value}` | `12345` | `task/12345` |
| `client/{value}/docs` | `abc` | `client/abc/docs` |

Leading and trailing slashes in the template are stripped automatically.

**Full example:**

Setup:
```
onix ctx client env CLIENT_ID {value}
onix ctx task   env TASK_ID   task/{value}
```

With `CLIENT_ID=abc` and `TASK_ID=12345`:

```
s task@client@place
→ <place>/abc/task/12345

n task@client@place -s config
→ editor at <place>/abc/task/12345/config
```

**Context sources:**

| Source | Config key | Reads from |
|--------|-----------|------------|
| `env`  | `var`     | Environment variable |
| `file` | `file`    | First line of a file (supports `~`) |
| `cmd`  | `cmd`     | stdout of a shell command |

```
onix ctx branch cmd "git rev-parse --abbrev-ref HEAD"
onix ctx sprint file ~/.onix/current-sprint
```

Context configs are stored in `~/.onix/contexts/<segment>` as plain key=value files.
They are per-segment (identified by the name before the `@`), not per-alias.

---

## Open Explorer Here

**Manual:** Win+R, paste the full path, press Enter. Or click through six folders in Explorer.

**With onix:**
```
o acme -e
```
```
s acme
```

Both are equivalent. `s` is the shortcut for "show in Explorer."

---

## Open Your Editor Here

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2
nvim .
```

**With onix:**
```
o acme -n
```
```
n acme
```

Goes straight to the project root in your editor (respects `$EDITOR`, defaults to nvim).

Want to open a subdirectory directly?
```
n acme -s src
```

---

## Open a Specific File

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2
nvim src\handlers\auth.go
```

**With onix:**
```
o acme -f src\handlers\auth.go
```
```
f acme src\handlers\auth.go
```

One command from anywhere. No cd required.

---

## Run a Command in a Project

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2
git pull
cd C:\wherever\you\were
```

**With onix:**
```
o acme -r "git pull"
```
```
r acme "git pull"
```

The command runs in the target directory. Your current shell location never changes.

```
r acme "git pull"
r acme "go build ./..."
r acme "npm run test"
```

---

## Search File Contents — Jump to the Line

This is where the tool earns its keep.

**Manual:**
```
# Step 1: get to the directory
cd C:\Users\dev\projects\client-work\acme\backend\api\v2

# Step 2: search
rg "handleAuthRequest"

# Step 3: read the output, note the file and line number
#   src/handlers/auth.go:47:func handleAuthRequest(w http.ResponseWriter ...

# Step 4: open your editor there
nvim +47 -- src\handlers\auth.go
```

Four steps. You're the bridge between ripgrep and your editor.

**With onix:**
```
sg acme handleAuthRequest
```

What happens: ripgrep runs across the entire project tree. fzf opens with a preview pane —
syntax-highlighted, scrolled to the match line using `bat`. Navigate the results. Press Enter.
You land in your editor at that exact line. Nothing to copy, nothing to type.

Multi-select is supported: select several matches with Tab, press Enter, they all open.

---

## Search Filenames

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2
dir /s /b *migration*
```

Then manually open what you find.

**With onix:**
```
o acme -ff migration
```
```
ff acme migration
```

Uses Everything (the `es` CLI) + fzf. Results are instant. Press Enter to open in your editor,
`Ctrl+E` to open the file's containing folder in Explorer.

> Requires Everything installed and running. `scoop install everything` if you don't have it.

---

## Print the Resolved Path

Useful when you need the actual path for a script, another command, or your clipboard.

```
o acme -y
```
```
y acme
```

Prints the resolved path to stdout and also:
- Copies it to the clipboard via `clip.exe` (silent — no extra output)
- Persists it as `%ONIX_LAST%` via `setx` so new shells and scripts can read it

```
C:\Users\dev\projects\client-work\acme\backend\api\v2
```

`setx` does not affect the current shell session (Windows limitation). Open a new terminal
window to use `%ONIX_LAST%`.

Practical uses:
```
y acme                          # print + auto-clip + set ONIX_LAST
robocopy %ONIX_LAST% D:\backup /MIR    # use the persisted path in a new shell
```

---

## Managing Aliases

```
# Register
o -a acme -d C:\Users\dev\projects\client-work\acme\backend\api\v2

# Open the alias file in your editor
o

# Re-register to update an existing alias
o -a acme -d C:\Users\dev\projects\client-work\acme\v3
```

Aliases live in `~/.onix/aliases` as plain `KEY=VALUE` pairs.

---

## Extending with Modules

onix separates concerns: the core binary handles alias resolution and dispatch, and capabilities are added as independent Go modules installed from GitHub.

**Install a module:**
```
onix add sadirano/onix-img
onix install
```

That clones the repo, builds it, and drops a `.cmd` wrapper in `~/.onix/bin/`. Add that
directory to your PATH once and every installed module becomes a first-class command.

**What every module receives automatically** — no argument parsing needed in the module itself:
```
ONIX_TARGET         = C:\Users\dev\projects\client-work\acme\backend\api\v2
ONIX_ALIAS          = acme
ONIX_MODULE         = img
ONIX_MODULE_CONFIG  = {"default_subdir":"assets/screenshots/{today}"}
```

A module is just a Go binary that reads `ONIX_TARGET` and acts on it. The directory
resolution is already done before your module runs.

**Declaring modules is declarative**, lazy.nvim-style, in `~/.onix/config.toml`:
```toml
[[module]]
name    = "img"
repo    = "sadirano/onix-img"
ref     = "main"
enabled = true

[module.config]
default_subdir = "assets/screenshots/{today}"
```

### Example: `img` — clipboard image saver

Save whatever is on your clipboard directly into a project, named and organised automatically.

```
img acme ui-auth-flow
```

What happens:
1. onix resolves `acme` → `C:\Users\dev\projects\client-work\acme\backend\api\v2`
2. The `img` module reads its own `img.env` to check if `acme` has a registered default
   image subdirectory (e.g. `assets\screenshots`).
3. The `ONIX_MODULE_CONFIG` JSON provides a `default_subdir` template as a fallback.
4. Variables in the path are expanded at runtime:
   - `{today}` → `2026-04-06`
   - `{time}`  → `14-30-25`
5. The clipboard image is written to the resolved path as `ui-auth-flow.png`
   (or `ui-auth-flow-{time}.png` if the module is configured to de-duplicate by time).

`img.env` lives next to the module binary and stores per-alias overrides:
```
# img.env
acme=assets\screenshots\{today}
mysite=docs\images
```

If no entry exists for the alias, `default_subdir` from `ONIX_MODULE_CONFIG` is used.
If that's also empty, the image lands directly in `ONIX_TARGET`.

**More examples:**
```
img acme dark-mode-toggle          # → acme\assets\screenshots\2026-04-06\dark-mode-toggle.png
img acme ui-flow -s reviews        # -s overrides the subdir for this one call
img mysite hero-banner             # → mysite\docs\images\hero-banner.png
```

**Module lifecycle:**
```
onix list              # see all declared modules and their install status
onix update            # pull latest and rebuild all
onix update img        # update one module
onix remove img        # uninstall and remove from config
```

---

## Environment Variables

| Variable          | Effect                                                    |
|-------------------|-----------------------------------------------------------|
| `ONIX_DEBUG=1`    | Print trace lines to stderr on every invocation           |
| `ONIX_TIMING=1`   | Print phase timings to stderr                             |
| `ONIX_ENV`        | Override alias file path (highest precedence)             |
| `ONIX_ALIAS_FILE` | Override alias file path (second precedence)              |
| `EDITOR`          | Preferred editor (fallback: `nvim`)                       |

Config-file equivalents (in `~/.onix/config.toml` under `[settings]`):
```toml
[settings]
alias_file    = ""     # same as ONIX_ENV, lower precedence than env var
editor        = ""     # same as EDITOR
debug         = false
timing        = false```

---

## Quick Reference

```
sg acme handleAuth              # search contents → jump to line in editor
n acme -s src                   # open editor directly in a subdirectory
r acme "go test ./..."          # run tests without leaving your current shell
f acme README.md                # open a known file from anywhere
ff acme migration               # find a filename, open it
y acme                          # resolved path → print + clipboard + ONIX_LAST
o acme -s internal -n           # land in a subdir and open editor in one shot
img acme screenshot-name        # paste clipboard image into project

# Sub-alias navigation
s an@sms                        # shell in <sms>/anexos  (subdir registry)
n doc@sms                       # editor in <sms>/documentacao
s task@client@place             # multi-segment: <place>/{clientID}/task/{taskID}

# Context segment setup
onix ctx client env CLIENT_ID {value}
onix ctx task   env TASK_ID   task/{value}
onix ctx branch cmd "git rev-parse --abbrev-ref HEAD"
onix ctx sprint file ~/.onix/current-sprint
```
