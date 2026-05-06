# Onix Playbook (Quick Command Matrix)

Use this as a fast copy/paste sheet to try every core Onix flow.

## 1) Base Pattern

```powershell
onix <alias> [-s <subdir>] [action flag] [action extras...]
```

Examples:

```powershell
onix api
onix api -s src
onix api -s src -n
```

## 2) Alias Management

```powershell
# Open alias file
onix

# Register alias
onix -a api -d C:\temp\api

# Register alias + subdir baked in
onix -a api-src -d C:\temp\api -s src
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
| Open shell | `onix api` | `o api` / `c api` |
| Open Explorer | `onix api -e` | `s api` |
| Open editor in folder | `onix api -n` | `n api` |
| Print resolved path | `onix api -y` | `y api` |
| Open specific file(s) | `onix api -f README.md` | `f api README.md` |
| Run command in target dir | `onix api -r "go test ./..."` | `r api "go test ./..."` |
| Search content (rg+fzf) | `onix api -sg handler` | `sg api handler` |
| Search files (es+fzf) | `onix api -ff migration` | `ff api migration` |

With subdir:

```powershell
onix api -s src -n
onix api -s internal -f config.go
onix api -s cmd -r "go build ."
onix api -s src -sg router
onix api -s src -ff handler
```

## 4) sg / ff Quick Usage

```powershell
# Content search + jump in editor
sg api auth middleware

# File search + open
ff api migration
```

`sg` / `ff` are visual and controlled by `onix.visual.toml`.

## 6) Module Lifecycle

```powershell
onix init
onix add user/repo
onix install
onix install <name>
onix list
onix update
onix update <name>
onix remove <name>
```

## 7) Shortcut / PATH Utilities

```powershell
onix shortcuts
```

## 8) Debug / Timing

```powershell
# Current shell only
set ONIX_DEBUG=1
set ONIX_TIMING=1

# Then run any command
onix api -n
```

## 9) Sub-Alias Navigation

Single segment — subdir registry lookup:

```powershell
# Registry: ~/.onix/subdirs.env
# an=anexos   doc=documentacao   ts=testes

s an@sms        # shell in <sms>/anexos
n doc@sms       # editor in <sms>/documentacao
y ts@sms        # print path of <sms>/testes
o outros@sms    # literal fallback (not in registry)
s an@sms -s config   # stacks: <sms>/anexos/config
```

Multi-segment chain with context:

```powershell
# Setup (one time)
onix ctx client env CLIENT_ID {value}
onix ctx task   env TASK_ID   task/{value}
onix ctx branch cmd "git rev-parse --abbrev-ref HEAD" {value}
onix ctx sprint file ~/.onix/sprint {value}

# Use
s task@client@place     # <place>/{CLIENT_ID}/task/{TASK_ID}
n branch@proj           # editor in <proj>/<current-branch>
```

Context management:

```powershell
onix ctx <seg>                          # show current config
onix ctx <seg> env <VAR> [template]     # configure from env var
onix ctx <seg> cmd <command> [template] # configure from command output
onix ctx <seg> file <path> [template]   # configure from file
onix ctx <seg> --clear                  # remove config
```

## 10) Fast Smoke Run

```powershell
onix -a demo -d C:\temp\demo
onix demo
onix demo -e
onix demo -n
onix demo -y
onix demo -f README.md
onix demo -r "dir"
onix theme
```
