# Onix Playbook (Quick Command Matrix)

Use this as a fast copy/paste sheet to try every core Onix flow.

## 1) Base Pattern

```powershell
onix <alias> [action flag] [action extras...]
```

Examples:

```powershell
onix api
onix api -e
onix api -e README.md
```

To land in a subdirectory of an alias, define a segment in `~/.onix/segments.toml` and use `@`-syntax (`onix src@api`). See section 9.

## 2) Alias Management

```powershell
# Open alias file
onix

# Register alias
onix -a api -d C:\temp\api
```

Shortcut behavior:

```powershell
# If run via o/c, registration also opens shell in destination
o -a api -d C:\temp\api
c -a api -d C:\temp\api
```

## 3) Navigation / Action Combos

| Goal | Full form | Shortcut |
|---|---|---|
| Open shell | `onix api` | `o api` |
| Open Explorer | `onix api -x` | `s api` |
| Open editor in folder | `onix api -e` | `e api` |
| Open specific file(s) in editor | `onix api -e README.md` | `e api README.md` |
| Print resolved path | `onix api -y` | `y api` |
| Run command in target dir | `onix api -r "go test ./..."` | `r api "go test ./..."` |
| Search content (rg+fzf) | `onix api -g handler` | `sg api handler` |
| Find file by name (fd+fzf) | `onix api -f migration` | `ff api migration` |

To scope these to a subdirectory, use a segment: `e src@api`, `sg src@api router`, etc.

## 4) sg / ff Quick Usage

```powershell
# Content search + jump in editor
sg api auth middleware

# File search + open
ff api migration
```

`sg` / `ff` are visual and controlled by `onix.visual.toml`.

## 6) Onboarding / Plugin Lifecycle

```powershell
onix --init                                  # set up ~/.onix and shell integration
onix --doctor                                # health check
onix --version                               # build info

# Plugin management (kong subtree — unchanged).
onix plugin add user/repo --sha <hash>       # pin to commit
onix plugin add user/repo --unpinned         # track default branch
onix plugin list
onix plugin update <name>
onix plugin remove <name>
```

## 7) Shortcut / PATH Utilities

```powershell
# After moving the onix binary, regenerate wrappers + snippet so the pin
# points at the new location.
onix --sync
```

## 8) Debug / Timing

```powershell
# Current shell only
set ONIX_DEBUG=1
set ONIX_TIMING=1

# Then run any command
onix api -e
```

## 9) Sub-Alias Navigation

Segments are defined as `[[contexts]]` entries in `~/.onix/segments.toml`.
On first use of an undefined segment, an interactive prompt walks you
through creating one.

Static template:

```powershell
# segments.toml
# [[contexts]]
# segment = "docs"
# source-template = "/documentation"

s docs@sms       # shell in <sms>/documentation
y src@sms        # print path of <sms>/source
```

Inline value (`seg:value`):

```powershell
# segments.toml
# [[contexts]]
# segment = "tasks"
# source-template = "/tickets/${tasks}"

s tasks:432@acme         # <acme>/tickets/432
```

Multi-segment composition:

```powershell
# segments.toml
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

The same `[[contexts]]` block can also carry `env = {...}` and `exec = [...]`
to script shell side effects on `cd`.

## 10) Fast Smoke Run

```powershell
onix -a demo -d C:\temp\demo
onix demo
onix demo -x
onix demo -e
onix demo -y
onix demo -f README.md
onix demo -r "dir"
onix theme
```
