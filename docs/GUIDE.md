# onix — the guide

Your project lives at `C:\Users\dev\projects\client-work\acme\backend\api\v2`.

You type `o acme` instead.

---

## First-time Setup

```
onix --init
```

`--init` creates `~/.onix/`, writes a starter `config.toml` and `aliases.toml`,
generates the shell snippet at `~/.onix/shell/onix.ps1` (and `onix.sh` on Linux),
and sources it from your shell profile. On Windows it also writes `.cmd` wrappers
into `~/.onix/bin/` so the same shortcuts work from cmd.exe and Win+R — you only
need to add that directory to PATH if you use cmd.exe regularly.

Restart PowerShell (or run `. $PROFILE`) once to activate `o`, `e`, `s`, `y`,
`r`, `sg`, and `ff`. Run `onix --sync` any time you move the onix binary or change
`config.toml` to regenerate the snippet.

---

## Register an Alias

One command, one time. The alias is yours forever.

```
onix acme C:\Users\dev\projects\client-work\acme\backend\api\v2
```

That writes the alias to `~/.onix/aliases.toml`. The directory is created
automatically if it doesn't exist. To update an existing alias, run the same
command with the new path.

Run `o` with no arguments to open `~/.onix/` in your editor if you ever want to
hand-edit the files directly.

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

Changes the **current shell's** working directory to the alias target — no new
window, no subprocess. Works from anywhere.

Need to land in a subdirectory? Define a segment for it (see *Sub-Alias Navigation*
below) and call `o handlers@acme`.

### Unknown alias — interactive pick

If you type an alias that hasn't been registered yet, onix opens an fzf directory
picker pre-seeded with your query (uses Everything's `es` if installed, drive-walk
otherwise). Pick a folder, press Enter — the alias is registered automatically. No
separate register step needed.

---

## Sub-Alias Navigation

Sub-alias navigation lets you jump into a specific subdirectory inside an alias
without registering a new alias for every path combination you visit. Each
`@`-separated segment is defined as a `[[contexts]]` entry in
`~/.onix/segments.toml`.

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
s docs@sms       → Explorer at <sms>/documentation
e src@sms        → editor at <sms>/source
```

If you invoke a segment that isn't defined, onix opens an interactive prompt that
walks you through creating the `[[contexts]]` entry and saves it for you. Segments
without a `source-template` / `source-exec` / `source-file` field never contribute
a path fragment — they can still set env vars consulted during resolve-time
template expansion (see "Context env" below).

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
e task:432@client:bob@projb     → opens <projb>/bob_432.md in $EDITOR
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

### Context env

A `[[contexts]]` entry may also declare an `env` map. These keys are
consulted during resolve-time variable lookup (as a fallback after inline
values, before process env). They are **not** exported to the shell after
`cd` — if you want shell-side side effects, drive them yourself.

```toml
[[contexts]]
segment = "branch"
source-template = "/${BRANCH}"
env = { BRANCH = "main" }    # default when $BRANCH is unset in the shell
```

See [SEGMENTS.md](SEGMENTS.md) for the full grammar and traversal-guard rules.

---

## Open Explorer Here

**Manual:** Win+R, paste the full path, press Enter. Or click through six folders.

**With onix:**
```
s acme
```

`s` is the shortcut for "show in Explorer."

---

## Open Your Editor Here

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2
nvim .
```

**With onix:**
```
e acme
```

Goes straight to the project root in your editor. Editor resolution order:
`$EDITOR` → `$VISUAL` → first of `nvim`, `vim`, `code`, `nano`, `notepad` found
on PATH.

---

## Open a Specific File

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2
nvim src\handlers\auth.go
```

**With onix:**
```
e acme src\handlers\auth.go
```

If you only remember part of the filename, use `ff` (fd+fzf or Everything+fzf):

```
ff acme auth
```

One command from anywhere. No `cd` required.

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
r acme git pull
```

The command runs in the target directory. Your current shell location never changes.

```
r acme git pull
r acme go build ./...
r acme npm run test
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

What happens: ripgrep runs across the entire project tree. fzf opens with a preview
pane — syntax-highlighted, scrolled to the match line using `bat`. Navigate the
results. Press Enter. You land in your editor at that exact line. Nothing to copy,
nothing to type.

Multi-select is supported: select several matches with Tab, press Enter, they all
open.

The default layout is a top split that auto-scrolls the preview to the match line.
Configure via `~/.onix/config.toml`:

```toml
[grep]
preview_window  = "up:60%:border-bottom:+{2}+3/3:~3"
preview_command = "bat --style=numbers,header,grid --color=always {1} --highlight-line {2}"
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

fzf inherits `FZF_DEFAULT_OPTS` from the environment if you've set one; otherwise
onix applies a Tokyo Night palette by default.

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
ff acme migration
```

Uses Everything (the `es` CLI) + fzf on Windows, `fd` + fzf on Linux. Results are
instant. Press Enter to open in your editor, `Ctrl+E` to open the file's containing
folder in Explorer.

> Requires Everything installed and running. `scoop install everything` if you don't
> have it.

---

## Print the Resolved Path

Useful when you need the actual path for a script, another command, or your
clipboard.

```
y acme
```

Prints the resolved path to stdout and copies it to the clipboard automatically.

```
C:\Users\dev\projects\client-work\acme\backend\api\v2
```

Practical uses:
```
y acme                       # print + copy to clipboard
y acme | Set-Content out.txt # pipe the path into a file
```

---

## Managing Aliases

```powershell
# Register or update
onix acme C:\Users\dev\projects\client-work\acme\backend\api\v2

# Register + cd in one step
o acme C:\Users\dev\projects\client-work\acme\backend\api\v2

# Open ~/.onix/ in your editor
o

# List all aliases
onix --list

# Remove an alias
onix acme --remove
```

Aliases live in `~/.onix/aliases.toml` as a plain TOML file. Lookups are
case-insensitive.

---

## Extending with Plugins

onix separates concerns: the core binary handles alias resolution and dispatch, and
capabilities are added as independent Go binaries installed from GitHub.

For a detailed guide and template for creating your own plugins, see
[MODULE_PATTERN.md](./MODULE_PATTERN.md).

**Install a plugin:**
```
onix plugin add sadirano/onix-img --sha <commit>      # pin to a commit
onix plugin add sadirano/onix-img --unpinned          # track default branch
```

That clones the repo, builds it, and registers a shell function (and a `.cmd`
wrapper in `~/.onix/bin/`) for every entry point the plugin declares. Re-source
your profile (or open a new shell) to use them.

**What every plugin receives automatically** — no argument parsing needed in the
plugin itself:
```
ONIX_TARGET         = C:\Users\dev\projects\client-work\acme\backend\api\v2
ONIX_ALIAS          = acme
ONIX_HOME           = C:\Users\dev\.onix
ONIX_EDITOR         = nvim
ONIX_MODULE         = img
ONIX_MODULE_CONFIG  = {"default_subdir":"assets/screenshots/{today}"}
ONIX_ENTRY          = <entry name, if the plugin declares multiple entry points>
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

Save whatever is on your clipboard directly into a project, named and organised
automatically.

```
img acme ui-auth-flow
```

What happens:
1. onix resolves `acme` → `C:\Users\dev\projects\client-work\acme\backend\api\v2`
2. The `img` plugin reads `ONIX_MODULE_CONFIG` for a `default_subdir` template.
3. Variables in the path are expanded at runtime:
   - `{today}` → `2026-04-06`
   - `{time}`  → `14-30-25`
4. The clipboard image is written to the resolved path as `ui-auth-flow.png`.

**More examples:**
```
img acme dark-mode-toggle          # → acme\assets\screenshots\2026-04-06\dark-mode-toggle.png
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

| Variable        | Effect                                                         |
|-----------------|----------------------------------------------------------------|
| `ONIX_TIMING=1` | Print phase timings to stderr on every invocation              |
| `ONIX_HOME`     | Override `~/.onix` location (also `--config-dir` on the CLI)  |
| `EDITOR`        | Preferred editor; fallback chain: nvim → vim → code → nano → notepad |

---

## Quick Reference

```
sg acme handleAuth              # search contents → jump to line in editor
e acme                          # open project root in editor
r acme go test ./...            # run tests without leaving your current shell
e acme README.md                # open a known file from anywhere
ff acme migration               # fuzzy-find a filename, open it
y acme                          # resolved path → print + clipboard
img acme screenshot-name        # paste clipboard image into project

# Sub-alias navigation (segments defined as [[contexts]] in segments.toml)
s docs@sms                      # Explorer at <sms>/documentation
e src@sms                       # editor at <sms>/source
s tasks:432@acme                # inline value: <acme>/tickets/432
e task:432@client:bob@projb     # multi-segment: <projb>/bob_432.md

# Define new segments by editing ~/.onix/segments.toml or letting the
# unknown-segment prompt walk you through it on first use.
```
