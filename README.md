# Onix

Jump to any project in one word. Open a shell, your editor, Explorer, or run a command — all from wherever you are, no `cd` required. Extends via Go modules installed straight from GitHub.

```
o acme                      # open a shell in your project
n acme                      # open editor there
sg acme handleAuth          # search contents → jump to line
r acme "go test ./..."      # run a command without leaving your shell
y acme                      # print path + copy to clipboard + set ONIX_LAST

s an@sms                    # subdir shortcut: shell in <sms>/anexos
s task@client@place         # multi-segment: <place>/{CLIENT_ID}/task/{TASK_ID}
```

For the full command reference and workflow examples, see [GUIDE.md](GUIDE.md).

## Install

```powershell
scoop bucket add sadirano https://github.com/sadirano/onix
scoop install onix
```

`post_install` runs `onix init` and `onix shortcuts` automatically.  
Add `~/.onix/bin/` to your PATH to activate the shortcut commands. Restart your terminal after install.

### Manual install

Download the zip from [Releases](https://github.com/sadirano/onix/releases), extract next to a folder on your PATH, then run:

```
onix init
onix shortcuts
```

### Build from source

```
git clone https://github.com/sadirano/onix
cd onix
build.cmd
```

## Modules

Onix extends via independent Go modules installed from GitHub:

```
onix add sadirano/onix-img   # clone, build, and wire up a module
onix list                    # see installed modules
onix update                  # pull and rebuild all modules
onix remove img              # uninstall
```

Modules receive the resolved project path and config via environment variables — no argument parsing needed in the module itself. See [GUIDE.md](GUIDE.md#extending-with-modules) for details.

## Visual Configuration

fzf UI settings (prompts, preview layout, colours) are controlled by `onix.visual.toml`, located next to `onix.exe`. The file is created automatically with defaults on first run.

## License

MIT
