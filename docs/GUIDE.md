# onix — the guide

Your project lives at `C:\Users\dev\projects\client-work\acme\backend\api\v2`.

You type `o acme` instead.

---

## First-time Setup

```
onix --init
```

`--init` creates `~/.onix/`, writes a starter `config.toml`, drops `.cmd`
wrappers (`o`, `n`, `s`, `f`, `r`, `y`, `sg`, `ff`) into `~/.onix/bin/`,
and sources the shell snippet from your profile.

Add `~/.onix/bin/` to your `PATH` once and every shortcut becomes a
first-class command. If you later move the onix binary, run
`onix --sync` from the new location to update the pin.

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
registering a new alias for every path combination you visit. Each `@`-separated
segment is defined as a `[[contexts]]` entry in `~/.onix/segments.toml`.

### Define a segment

```toml
# ~/.onix/segments.toml
version = 3

[[contexts]]
segment = "docs"
source-template = "/documentation"   # the leading `/` makes it a directory

[[contexts]]
segment = "src"
source-template = "/source"
```

```
s docs@sms       → shell in <sms>/documentation
n src@sms        → editor in <sms>/source
```

If you invoke a segment that isn't defined, onix opens an interactive prompt that
walks you through creating the `[[contexts]]` entry and saves it for you. Segments
without a `source-template` / `source-exec` / `source-file` field never contribute
a path fragment — they can still set env vars or run a shell command after `cd`
(see "Context scripting" below).

### Inline values — `seg:value@alias`

When the dimension you want to inject changes per invocation (a task ID, client
name, branch, sprint…), pass it as the segment's inline value:

```
s tasks:432@acme
```

```toml
[[contexts]]
segment = "tasks"
source-template = "/tickets/${tasks}"   # ${tasks} binds to the inline value
```

Result: `<acme>/tickets/432`.

The variable name defaults to the segment name. Set `param = "..."` on the context
to use a different `${...}` reference inside the template.

### Composing segments — `seg1@seg2@alias`

Segments are processed right-to-left (innermost first). Templates own their
separators: a leading `/` joins as a directory, no leading `/` appends directly.

```toml
[[contexts]]
segment = "client"
source-template = "/${client}"

[[contexts]]
segment = "task"
source-template = "_${task}.md"   # no leading / — appends to the previous fragment
```

```
f task:432@client:bob@projb     → opens <projb>/bob_432.md in $EDITOR
```

### Source kinds

Each `[[contexts]]` entry uses one of three source kinds. Mixing more than one is a
load-time error.

| Field | Behaviour |
|-------|-----------|
| `source-template` | A string with `${VAR}` references. Vars resolve in order: segment inline value → context's `env` map → process env → error. |
| `source-exec`     | `["cmd", "arg", ...]`. Each arg is template-expanded, the command runs in the alias base directory, and trimmed stdout becomes the fragment. |
| `source-file`     | A path. Accepts `@home/...`, `@alias/...`, `~/...`, or absolute. File contents (trimmed) are the fragment. |

```toml
[[contexts]]
segment = "branch"
source-exec = ["pwsh", "-c", "'/' + (git rev-parse --abbrev-ref HEAD)"]

[[contexts]]
segment = "current"
source-file = "@home/state/current-task"
```

### Context scripting

The same `[[contexts]]` entry can also drive shell-side side effects when the user
navigates to the alias — env vars to export and a command to run on `cd`:

```toml
[[contexts]]
segment = "prod"
source-template = "/prod"
env = { DEPLOY_ENV = "production", KUBECTL_CTX = "prod-cluster" }
exec = ["kubectl", "config", "use-context", "prod-cluster"]
```

`o web@prod` resolves to `<web>/prod`, exports `DEPLOY_ENV` / `KUBECTL_CTX`, and
runs the `kubectl` switch — all from one shortcut.

### Migrating from `[subdirs]`

The pre-3.0 `[subdirs]` table and per-alias `subdirs = {...}` blocks are no longer
read. The recipe is one `[[contexts]]` block per former entry:

```toml
# before
[subdirs]
docs = "documentation"

# after
[[contexts]]
segment = "docs"
source-template = "/documentation"
```

On first use of an undefined segment, the interactive prompt picks the right form
for you. See [SEGMENTS.md](SEGMENTS.md) for the full grammar and
traversal-guard rules.

### Legacy `onix ctx` reference (pre-3.0)

The earlier `onix ctx <seg> env|cmd|file ...` subcommand was retired with the
redesign. Anything you used to set via that command should now be a single
`[[contexts]]` block in `segments.toml`.

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

The default layout is a top split that auto-scrolls the preview to the
match line. Configure via `~/.onix/config.toml`:

```toml
[grep]
preview_window  = "up:60%:border-bottom:+{2}+3/3:~1"
preview_command = "bat --style=numbers,header --color=always {1} --highlight-line {2}"
fzf_colors      = ""   # extra --color flags layered on top of the theme
rg_colors       = [    # each entry becomes `--colors <spec>` on rg
    "path:fg:blue",
    "line:fg:green",
    "match:fg:red",
    "match:style:bold",
]
```

In the preview command, `{1}` is the file and `{2}` is the line number
(rg emits `file:line:text`; fzf splits on `:`). rg case behaviour is
`--smart-case` — pass `--ignore-case` or `--case-sensitive` as part of
your query to override.

fzf inherits `FZF_DEFAULT_OPTS` from the environment if you've set one;
otherwise onix applies a Tokyo Night palette by default.

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

## Extending with Plugins

onix separates concerns: the core binary handles alias resolution and dispatch, and capabilities are added as independent Go plugins installed from GitHub.

For a detailed guide and template for creating your own plugins, see [MODULE_PATTERN.md](./MODULE_PATTERN.md).

**Install a plugin:**
```
onix plugin add sadirano/onix-img --sha <commit>      # pin to a commit
onix plugin add sadirano/onix-img --unpinned          # track default branch
```

That clones the repo, builds it, and drops a `.cmd` wrapper in `~/.onix/bin/`. Add that
directory to your PATH once and every installed plugin becomes a first-class command.

**What every plugin receives automatically** — no argument parsing needed in the plugin itself:
```
ONIX_TARGET         = C:\Users\dev\projects\client-work\acme\backend\api\v2
ONIX_ALIAS          = acme
ONIX_MODULE         = img
ONIX_MODULE_CONFIG  = {"default_subdir":"assets/screenshots/{today}"}
```

A plugin is just a Go binary that reads `ONIX_TARGET` and acts on it. The directory
resolution is already done before your plugin runs.

**Plugin registry** lives in `~/.onix/plugins.toml`:
```toml
[[plugins]]
name = "img"
repo = "sadirano/onix-img"
sha  = "abc123def456"             # required unless `unpinned = true`

  [plugins.config]
  default_subdir = "assets/screenshots/{today}"
```

### Example: `img` — clipboard image saver

Save whatever is on your clipboard directly into a project, named and organised automatically.

```
img acme ui-auth-flow
```

What happens:
1. onix resolves `acme` → `C:\Users\dev\projects\client-work\acme\backend\api\v2`
2. The `img` plugin reads its own `img.env` to check if `acme` has a registered default
   image subdirectory (e.g. `assets\screenshots`).
3. The `ONIX_MODULE_CONFIG` JSON provides a `default_subdir` template as a fallback.
4. Variables in the path are expanded at runtime:
   - `{today}` → `2026-04-06`
   - `{time}`  → `14-30-25`
5. The clipboard image is written to the resolved path as `ui-auth-flow.png`
   (or `ui-auth-flow-{time}.png` if the plugin is configured to de-duplicate by time).

`img.env` lives next to the plugin binary and stores per-alias overrides:
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

**Plugin lifecycle:**
```
onix plugin list           # see installed plugins, their pinned SHA, and binary status
onix plugin update         # refetch + rebuild every plugin
onix plugin update img     # update one plugin
onix plugin remove img     # uninstall and remove from plugins.toml
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

# Sub-alias navigation (segments defined as [[contexts]] in segments.toml)
s docs@sms                      # shell in <sms>/documentation
n src@sms                       # editor in <sms>/source
s tasks:432@acme                # inline value: <acme>/tickets/432
f task:432@client:bob@projb     # multi-segment: <projb>/bob_432.md

# Define new segments by editing ~/.onix/segments.toml or letting the
# unknown-segment prompt walk you through it on first use.
```
