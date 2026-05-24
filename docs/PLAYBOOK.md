# Onix Playbook (Quick Command Matrix)

Use this as a fast copy/paste sheet to try every core Onix flow.

## 1) Base Pattern

```powershell
# Resolve an alias (print its path)
onix <alias>

# Register or update an alias
onix <alias> <path>

# Run an action against an alias
onix <alias> --<action> [args...]
```

Examples:

```powershell
onix api                         # print resolved path
onix api C:\temp\api             # register alias (or update)
onix api --edit                  # open in editor
onix api --edit README.md        # open specific file in editor
```

To land in a subdirectory of an alias, define a segment in `~/.onix/segments.toml`
and use `@`-syntax (`e src@api`). See section 9.

## 2) Alias Management

```powershell
# Open ~/.onix/ in your editor
o

# Register alias
onix api C:\temp\api

# Register + cd in one step (o is a shell function that also does Set-Location)
o api C:\temp\api

# List all aliases
onix --list

# Remove an alias
onix api --remove
```

## 3) Navigation / Action Combos

| Goal | Direct form | Shell shortcut |
|---|---|---|
| cd into alias | `onix api` (print path) + `o api` | `o api` |
| Open Explorer | `onix api --explore` | `s api` |
| Open editor in folder | `onix api --edit` | `e api` |
| Open specific file(s) in editor | `onix api --edit README.md` | `e api README.md` |
| Print resolved path | `onix api --yank` | `y api` |
| Run command in target dir | `onix api --run go test ./...` | `r api go test ./...` |
| Search content (rg+fzf) | `onix api --grep handler` | `sg api handler` |
| Find file by name (fd+fzf) | `onix api --find migration` | `ff api migration` |

To scope these to a subdirectory, use a segment: `e src@api`, `sg src@api router`,
etc.

## 4) sg / ff Quick Usage

```powershell
# Content search → jump in editor
sg api auth middleware

# File search + open
ff api migration
```

`sg` calls ripgrep + fzf with a bat preview; results open in `$EDITOR` at the
matched line. `ff` uses Everything (`es`) on Windows or `fd` on Linux, both with
fzf. Configure the layout in `~/.onix/config.toml` under `[grep]`.

## 5) Custom Actions

Declare shortcuts in `~/.onix/config.toml` and run `onix --sync` + `. $PROFILE`:

```toml
[[actions]]
name = "test"
exec = "go"
args = ["test", "./..."]

[[actions]]
name = "pr"
exec = "gh"
args = ["pr", "view", "{extras}", "--web"]
```

```powershell
test api             # runs: go test ./... in the api directory
pr api 42            # runs: gh pr view 42 --web in the api directory
```

## 6) Onboarding / Plugin Lifecycle

```powershell
onix --init                                  # set up ~/.onix and shell integration
onix --doctor                                # health check
onix --version                               # build info

# Plugin management
onix plugin add user/repo --sha <hash>       # pin to commit
onix plugin add user/repo --unpinned         # track default branch
onix plugin list
onix plugin update <name>
onix plugin remove <name>
```

## 7) Snippet Utilities

```powershell
# After moving the onix binary or changing config.toml, regenerate the
# shell snippet so functions and completions point at the right binary.
onix --sync
```

## 8) Debug / Timing

```powershell
# Current shell only
$env:ONIX_TIMING = "1"

# Then run any command
onix api --edit
```

Phase timings are printed to stderr. Unset or set to anything other than `"1"` to
disable.

## 9) Sub-Alias Navigation

Segments are defined as `[[contexts]]` entries in `~/.onix/segments.toml`.
On first use of an undefined segment, an interactive prompt walks you through
creating one.

Static template:

```powershell
# segments.toml:
# [[contexts]]
# segment = "docs"
# source-template = "/documentation"

s docs@sms       # Explorer at <sms>/documentation
y src@sms        # print path of <sms>/source
```

Inline value (`seg:value`):

```powershell
# segments.toml:
# [[contexts]]
# segment = "tasks"
# source-template = "/tickets/${tasks}"

s tasks:432@acme         # <acme>/tickets/432
```

Multi-segment composition:

```powershell
# segments.toml:
# [[contexts]]
# segment = "client"
# source-template = "/${client}"
#
# [[contexts]]
# segment = "task"
# source-template = "_${task}.md"     # no leading / — appends as filename

e task:432@client:bob@projb     # opens <projb>/bob_432.md
```

Source kinds (exactly one per `[[contexts]]`):

| Field             | Behaviour |
|-------------------|-----------|
| `source-template` | `${VAR}` expansion; inline value → context env → process env. |
| `source-exec`     | Run cmd in alias base; trimmed stdout is the fragment. |
| `source-file`     | Read file (supports `@home/...`, `@alias/...`, `~/...`). |

A `[[contexts]]` block may also carry `env = {...}`; those keys are consulted during
resolve-time template variable lookup. They are **not** exported to the shell after
`cd`.

## 10) Fast Smoke Run

```powershell
onix demo C:\temp\demo       # register
o demo                       # cd into it
s demo                       # open in Explorer
e demo                       # open in editor
y demo                       # print + copy path
ff demo README               # fuzzy-find a file
r demo dir                   # run a command there
onix --list                  # confirm alias is registered
onix demo --remove           # clean up
```
