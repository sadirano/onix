# omni / onix — the guide

Your project lives at `C:\Users\dev\projects\client-work\acme\backend\api\v2`.

You type `o acme` instead.

---

## Register an Alias

One command, one time. The alias is yours forever.

```
o -a acme -d C:\Users\dev\projects\client-work\acme\backend\api\v2
```

That writes `acme=C:\Users\dev\projects\client-work\acme\backend\api\v2` to `~/.omni/.env`.
Run `o` with no arguments to open that file in your editor if you ever want to edit it by hand.

---

## Jump Into a Directory

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2
```

**With omni:**
```
o acme
```

Opens `cmd.exe` in that directory. Works from anywhere — no matter where your current shell is.

Need to land in a subdirectory?

**Manual:**
```
cd C:\Users\dev\projects\client-work\acme\backend\api\v2\src\handlers
```

**With omni:**
```
o acme -s src\handlers
```

The `-s` flag appends a subpath to the resolved alias before opening the shell.

---

## Open Explorer Here

**Manual:** Win+R, paste the full path, press Enter. Or click through six folders in Explorer.

**With omni:**
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

**With omni:**
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

**With omni:**
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

**With omni:**
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

**With omni:**
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

**With omni:**
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

Prints:
```
C:\Users\dev\projects\client-work\acme\backend\api\v2
```

Practical uses:
```
y acme | clip
robocopy (y acme) D:\backup\acme /MIR
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

Aliases live in `~/.omni/.env` as plain `KEY=VALUE` pairs. Both omni and onix read from the
same file — switching between them doesn't break anything.

---

## Extending with Modules (onix)

omni packed everything into a single PowerShell script. onix separates concerns: the core
binary handles alias resolution and dispatch, and capabilities are added as independent Go
modules installed from GitHub.

**Install a module:**
```
onix add sadirano/onix-sg
onix install
```

That clones the repo, builds it, and drops a `.cmd` wrapper in `~/.onix/bin/`. Add that
directory to your PATH once and every installed module becomes a first-class command.

**What every module receives automatically** — no argument parsing needed in the module itself:
```
ONIX_TARGET         = C:\Users\dev\projects\client-work\acme\backend\api\v2
ONIX_ALIAS          = acme
ONIX_MODULE         = sg
ONIX_MODULE_CONFIG  = {"default_flags":"--type go"}
```

A module is just a Go binary that reads `ONIX_TARGET` and acts on it. The directory
resolution is already done before your module runs.

**Declaring modules is declarative**, lazy.nvim-style, in `~/.onix/config.toml`:
```toml
[[module]]
name    = "sg"
repo    = "sadirano/onix-sg"
ref     = "main"
enabled = true

[module.config]
default_flags = "--type go"
```

**Module lifecycle:**
```
onix list              # see all declared modules and their install status
onix update            # pull latest and rebuild all
onix update sg         # update one module
onix remove sg         # uninstall and remove from config
```

---

## Quick Reference

```
sg acme handleAuth          # search contents → jump to line in editor
n acme -s src               # open editor directly in a subdirectory
r acme "go test ./..."      # run tests without leaving your current shell
f acme README.md            # open a known file from anywhere
ff acme migration           # find a filename, open it
y acme | clip               # resolved path to clipboard
o acme -s internal -n       # land in a subdir and open editor in one shot
```
