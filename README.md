# onix

Fast directory alias resolver for Windows and Linux command line. Type `o acme` from any prompt; your shell jumps to the project root. One TOML file holds every alias, one binary serves every command, and the hot path adds about 0.6 ms on top of the OS process-spawn floor.

## Install

### Windows (Scoop)

```powershell
scoop bucket add sadirano https://github.com/sadirano/bucket
scoop install onix
onix --init
```

### From source (Windows or Linux)

```bash
go install github.com/sadirano/onix@latest
onix --init
```

### Prebuilt binaries

Each tagged release publishes a Windows `.zip` and Linux `.tar.gz` (amd64/arm64) on the [Releases](https://github.com/sadirano/onix/releases) page — download, unpack, put `onix` on your `PATH`, then run `onix --init`.

`onix --init` creates `~/.onix/`, writes a shell snippet to `~/.onix/shell/`, and sources it from your shell profile (`$PROFILE` on Windows, `.bashrc`, `.zshrc`, or `.profile` on Unix-likes). Restart your shell (or source your profile) once and the short commands below are live.

## Use

```powershell
onix acme C:\Users\dev\projects\acme       # register an alias (auto-creates the dir if missing)
o acme                                     # cd into it (in your current shell)
o acme C:\Users\dev\projects\acme          # register + cd in one step (dir auto-created)
o                                          # no args: open aliases.toml in your editor
e acme                                     # open it in your editor
s acme                                     # open it in Explorer
s acme report.pdf                          # open a file with its default app (PDF→viewer, .zip→archiver…)
y acme                                     # print the path and copy to clipboard
p acme                                     # save clipboard content into the alias dir, copy the saved path back
p acme shot                                # …with a name (image→shot.png, text→shot.md)
r acme go test ./...                       # run a command at that path
o docs@acme                                # jump to a sub-alias segment (see Sub-aliases below)
onix --list                                # show every alias
onix --edit                                # open ~/.onix in your editor
onix acme --remove                         # forget the alias
```

The `o` command changes the **current** shell's working directory — it does not spawn a new shell. Three forms:

- `o <alias>` — resolve and cd. If the alias is unknown, `o` prompts for a destination, registers, and cds.
- `o <alias> <path>` — register (or update) the alias to point at `<path>` and cd there. The directory is auto-created if it doesn't exist.
- `o` (no args) — open `aliases.toml` in `$EDITOR`. Use `onix --list` if you want a tabular dump to stdout instead.

Everything else (`e`, `s`, `y`, `p`, `r`) invokes `onix` directly, so those don't need shell integration to work.

`s <alias> <file>` opens a single file with its registered default application instead of the file manager — a PDF in your viewer, a `.zip` in your archiver, and so on. The file is resolved against the alias directory and opened by the OS handler (`explorer.exe` / `xdg-open`).

`p <alias> [name]` saves the current clipboard contents into the alias directory and copies the saved file's absolute path back to the clipboard — handy for parking a screenshot and pasting its path into an agent. An image saves as `.png`, text as `.md`; an explicit extension on `<name>` is honoured, otherwise the default follows the clipboard content type. With no name it uses a timestamp, and a name collision auto-increments (`shot.png`, `shot-1.png`).

## Configuration

Aliases live in `~/.onix/aliases.toml`. The format is one TOML table per alias:

```toml
[acme]
path = "C:/Users/dev/projects/acme"
```

You can hand-edit the file (`onix --list` and resolve will pick up changes immediately) or use `onix <name> <path>` to register and `onix <name> --remove` to forget. Alias lookups are case-insensitive.

When the list grows crusty, `onix --prune` opens an fzf multi-select of every alias ranked prune-first: dead targets (directory gone), then never-used, then least-recently used. Tab marks, Enter removes the marked aliases, Esc cancels; `onix --prune --no-prompt` just prints the ranking. The ranking comes from `~/.onix/usage`, a small file the resolve paths maintain automatically (debounced to at most one write per alias per hour, so the hot path stays hot; delete it any time to start fresh).

Editor is taken from `$EDITOR`, then `$VISUAL`, then the first of `nvim`, `vim`, `code`, `nano`, or `notepad` found on PATH. Override the onix home location with `$ONIX_HOME`.

## Configuring shortcuts and search

`~/.onix/config.toml` holds two optional sections.

`[shortcuts]` renames the built-in command functions. The keys are the built-in names (`o`, `e`, `s`, `y`, `p`, `r`, `sg`, `ff`); the value is the name you'd rather type:

```toml
[shortcuts]
s = "show"     # type `show acme` instead of `s acme`
ff = "fzf"
```

`[grep]` tunes the `sg` search UI — the fzf preview window and command, fzf colors, ripgrep `--colors`, and whether non-ASCII query characters are matched literally:

```toml
[grep]
preview_window = "right:50%"
rg_colors = ["match:fg:yellow", "path:fg:cyan"]
```

After editing, run `onix --sync` and `. $PROFILE` (or restart PowerShell) to pick up renamed shortcuts.

## Sub-aliases (`@`-segments)

Append subdirectory shortcuts to any alias with `@`. Each segment is defined as a `[[contexts]]` entry. A segment is resolved by searching three places, first match wins:

1. **Per-alias, local:** `<alias-path>/.onix/segments.toml`
2. **Per-alias, central:** `~/.onix/segments/<alias>.toml`
3. **Global:** `~/.onix/segments.toml` — but only entries marked `scope = "global"` are visible here; unscoped entries in the global file are ignored.

```powershell
o docs@acme              # cd into <acme-path>/documentation
e src@acme               # editor at <acme-path>/source
o tasks:432@acme         # inline value: cd into <acme-path>/tickets/432
o client:bob@projb       # multi-segment, innermost first
```

```toml
# ~/.onix/segments.toml — entries in the global file must opt in with scope = "global"
[[contexts]]
segment = "docs"
scope = "global"
source-template = "/documentation"   # leading `/` makes it a subdirectory

[[contexts]]
segment = "src"
scope = "global"
source-template = "/source"

[[contexts]]
segment = "tasks"
scope = "global"
source-template = "/tickets/${tasks}"   # ${tasks} binds to the inline value
```

Per-alias files (`<alias-path>/.onix/segments.toml`, `~/.onix/segments/<alias>.toml`) need **no** `scope` — every entry there is implicitly scoped to that alias. Only the shared global file requires the opt-in.

A segment resolves through its `source-template`: a string with `${VAR}` references. For each `${name}`, onix looks up, in order, (1) the segment's inline value (`seg:value`), bound under `${<segment>}` — or `${param}` if the context sets `param`; (2) the context's `env` map; (3) the process environment. Templates own their separators — `"/foo"` appends as a subdirectory, `"_${task}.md"` appends as a filename suffix. A context with no `source-template` contributes nothing to the path.

Encountering an unknown segment opens your editor on the central per-alias file (`~/.onix/segments/<alias>.toml`) seeded with a `[[contexts]]` skeleton to fill in. Lookups are case-insensitive, and `onix --contexts` prints the contexts defined in the global `~/.onix/segments.toml`. See [docs/SEGMENTS.md](docs/SEGMENTS.md) for the traversal-guard rules.

## Tab completion

Every command that takes an alias (`o`, `e`, `s`, `y`, `p`, `r`, `sg`, `ff`) supports tab-completion of alias names. The completer calls `onix --list-names` under the hood — a dedicated hot path that bypasses TOML parsing for sub-millisecond Tab response.

## cmd.exe via clink

With [clink](https://chrisant996.github.io/clink/) installed, plain cmd.exe gets the same treatment: `onix --init` drops an `onix.lua` into clink's profile directory (`%LOCALAPPDATA%\clink`) that prepends `~/.onix/bin` to each session's PATH (so the short commands work without global PATH edits) and tab-completes alias names for every shortcut. Scoop installs wire this up automatically — the package depends on clink and registers its cmd.exe autorun. From source, install clink yourself and re-run `onix --init`; `onix --sync` keeps the script fresh afterwards.

## Commands

`onix --init` initialises `~/.onix` and installs the PowerShell snippet (re-run any time; it's idempotent). `onix --sync` regenerates the snippet and `.cmd` shims after you move the binary or edit `config.toml`. `onix --prune` interactively removes stale aliases. `onix --version` prints the build version, Go runtime, and OS/arch. `onix --help` lists everything.

## Diagnostics

If your shell profile does not source the snippet, run `onix --init` again without `--skip-profile`. On Windows this updates `$PROFILE`; on Linux it appends a `[ -f ... ] && . ...` line to `.bashrc` and/or `.zshrc`.

If `onix` is not on `PATH`, add `$env:USERPROFILE\go\bin` (Windows) or `~/go/bin` (Linux) to PATH and restart your shell. Shortcuts (`o`, `e`, `s`, `y`, `p`, `r`, `sg`, `ff`) work without `onix` on PATH because the snippet pins the binary location at install time; `PATH` only matters when you type `onix` directly. Zsh tab completion additionally requires `compinit` to be loaded in `.zshrc` before sourcing the snippet — without it, completion silently skips registration rather than erroring.

Set `$env:ONIX_HOME` to a different directory for sandboxed testing. The included `scripts/smoke.ps1` does exactly that — it builds, runs every command against a throwaway home, and measures the hot path.

## Architecture

Onix is designed for extreme performance on the hot path (`resolve`) while maintaining a clean, modular structure for management commands.

### Core Packages

- **`internal/store`**: Manages `aliases.toml`, the primary database of name-to-path mappings. Includes atomic write logic and name validation.
- **`internal/segments`**: Parses `@`-segment grammar, expands `${VAR}` templates, evaluates each context's `source-template`, and enforces the traversal guard on the resulting fragments before they join the alias path.
- **`internal/config`**: Manages `config.toml` — the optional `[shortcuts]` map (rename built-in commands) and `[grep]` section (tune the `sg` search UI).
- **`internal/snippet`**: Generates the PowerShell and Bash/Zsh glue code that integrates Onix into your shell.

## License

MIT.

CI: vet, govulncheck, full test suite, plus golangci-lint and gofumpt in the lint workflow. No coverage or bench gates — they were removed when the project entered maintenance mode.
