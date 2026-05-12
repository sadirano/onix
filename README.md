# onix

Fast directory alias resolver for Windows PowerShell. Type `o acme` from any prompt; your shell jumps to the project root. One TOML file holds every alias, one binary serves every command, and the hot path adds about 0.6 ms on top of the OS process-spawn floor.

## Install

```powershell
go install github.com/sadirano/onix@latest
onix init
```

`onix init` creates `~/.onix/`, writes a PowerShell snippet to `~/.onix/shell/onix.ps1`, and sources it from your `$PROFILE`. Restart PowerShell (or run `. $PROFILE`) once and the short commands below are live.

## Use

```powershell
onix add acme C:\Users\dev\projects\acme   # register an alias
o acme                                     # cd into it (in your current shell)
n acme                                     # open it in your editor
s acme                                     # open it in Explorer
y acme                                     # print the path and copy to clipboard
r acme go test ./...                       # run a command at that path
onix list                                  # show every alias
onix remove acme                           # forget it
```

The `o` command changes the **current** shell's working directory — it does not spawn a new shell. Everything else (`n`, `s`, `y`, `r`) invokes `onix` directly, so they don't need shell integration to work.

## Configuration

Aliases live in `~/.onix/aliases.toml`. The format is one TOML table per alias:

```toml
[acme]
path = "C:/Users/dev/projects/acme"
```

You can hand-edit the file (`onix list` and resolve will pick up changes immediately) or use `onix add` / `onix remove`. Alias lookups are case-insensitive.

Editor is taken from `$EDITOR` (falls back to `nvim`). Override the onix home location with `$ONIX_HOME`.

## Custom actions

Declare your own shortcuts in `~/.onix/config.toml`. Each becomes a real PowerShell function that takes an alias and remaining args:

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

After editing, run `onix install-actions` and `. $PROFILE` (or restart PowerShell). Then `test acme` runs `go test ./...` at the resolved acme path, and `pr acme 42` runs `gh pr view 42 --web`.

Template variables: `{target}` is the resolved path, `{alias}` is the alias name, `{extras}` is the rest of the args (variadic when used as a whole arg). Extras are appended automatically when `{extras}` isn't present in `args`.

## Sub-aliases (`@`-segments)

Append subdirectory shortcuts to any alias with `@`:

```powershell
o docs@acme              # cd into <acme-path>/documentation
n src@acme               # editor at <acme-path>/source
o ts@acme                # tests subdir
o anything@acme          # falls back to literal: <acme-path>/anything
o sub1@sub2@acme         # multi-segment, innermost first: <acme-path>/sub2/sub1
```

Define the global registry in `~/.onix/segments.toml`:

```toml
[subdirs]
docs = "documentation"
src  = "source"
ts   = "tests"
```

Per-alias overrides live next to the alias entry:

```toml
[acme]
path = "C:/projects/acme"

[acme.subdirs]
docs = "doc-internal"    # this acme uses a different docs layout
```

Lookup order is per-alias subdirs → global registry → literal fallback, so unregistered segments still navigate sensibly without setup. Lookups are case-insensitive.

## Plugins

External plugins extend onix beyond what custom actions can do. A plugin is its own Go binary, cloned from a GitHub repo, pinned to a commit, and dispatched against an alias the same way built-in actions are.

```powershell
onix plugin add sadirano/onix-tts --sha abc123def456   # install at a pinned SHA
onix plugin add C:\path\to\local-checkout --unpinned   # local source for development
onix plugin list                                       # show installed plugins + SHAs
onix plugin update tts                                 # refetch and rebuild
onix plugin update tts --sha <newhash>                 # bump the pin (re-confirms)
onix plugin remove tts                                 # uninstall
```

Each install prompts before building — you see the repo URL, resolved SHA, commit message, and the wrapper names that will land in your shell. `--yes` skips the prompt for automation. `--unpinned` tracks the default branch (rebuilds may install new upstream code without re-prompting; use with caution).

Plugin authors put an optional `onix.toml` in their repo declaring entry points; each entry becomes its own shell wrapper, with `ONIX_ENTRY=<name>` set when the plugin runs. The plugin receives the resolved path via `ONIX_TARGET`, the alias name via `ONIX_ALIAS`, the onix home via `ONIX_HOME`, and any `config = {…}` block from `plugins.toml` as JSON in `ONIX_MODULE_CONFIG`.

Plugin wrappers participate in tab completion just like built-ins and custom actions.

## Tab completion

Every command that takes an alias (`o`, `n`, `s`, `y`, `r`, plus your custom actions and plugins) supports tab-completion of alias names. The completer calls `onix list-names` under the hood — a dedicated hot path that bypasses kong and go-toml for sub-millisecond Tab response.

## Commands

`onix init` initialises `~/.onix` and installs the PowerShell snippet (re-run any time; it's idempotent). `onix doctor` reports any installation issues. `onix version` prints the build version, Go runtime, and OS/arch. `onix --help` lists everything.

## Diagnostics

If `onix doctor` warns that PowerShell `$PROFILE` does not source the snippet, run `onix init` again without `--skip-profile`. If it warns that `onix` is not on `PATH`, add `$env:USERPROFILE\go\bin` (or wherever your `go install` puts binaries) to PATH and restart PowerShell.

Set `$env:ONIX_HOME` to a different directory for sandboxed testing. The included `scripts/smoke.ps1` does exactly that — it builds, runs every command against a throwaway home, and measures the hot path.

## Status and scope

This release covers Windows + PowerShell, with built-in actions, custom actions from `config.toml`, SHA-pinned external plugins from `plugins.toml`, sub-alias subdir shortcuts from `segments.toml`, and PowerShell tab completion. Linux/macOS shells, sub-alias context resolvers (env/cmd/file segments with templates), search shortcuts (`sg`, `ff`) as first-party features, and an optional daemon mode for sub-millisecond resolution are tracked but not in this build. Existing plugins like `onix-search`, `onix-find`, `onix-timer`, and `onix-tts` work as-is — they read the same `ONIX_TARGET`/`ONIX_ALIAS`/`ONIX_MODULE_CONFIG` env vars the v1 onix exposed.

## License

MIT.
