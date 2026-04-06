# Onix

Modular directory navigator for Windows — jump to any project in one word.

## Install

```powershell
scoop bucket add sadirano https://github.com/sadirano/onix
scoop install onix
```

`post_install` runs `onix init` and `onix shortcuts` automatically.
Add `~/.onix/bin/` to your PATH to activate the shortcut commands (`o`, `n`, `s`, `f`, `r`, `y`, `sg`, `ff`). Restart your terminal after install.

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

---

For command usage and workflow examples, see `GUIDE.md`.

## Visual Configuration (`onix.visual.toml`)

Onix now reads visual/fzf UI settings from a file named `onix.visual.toml` located in the **same directory as `onix.exe`**.

- If the file does not exist, Onix creates it automatically with defaults.
- This file controls prompts, previews, and preview window layout for:
  - destination picker
  - `sg`
  - `ff`

### Where the file is created

- If you run the repo binary directly: `C:\...\onix\onix.visual.toml`
- If you run the deployed binary: `%USERPROFILE%\.onix\onix.visual.toml`

### Supported keys

#### `[fzf.destination]`
- `prompt`
- `preview`
- `preview_window`
- `header`
- `height`

#### `[fzf.sg]`
- `prompt`
- `color`
- `preview`
- `preview_window`

#### `[fzf.ff]`
- `prompt`
- `preview`
- `preview_window`

### Default file

```toml
[fzf.destination]
prompt = "Destination > "
preview = "dir /b \"{}\" 2>nul"
preview_window = "right:40%,border-left"
header = "Enter to confirm  |  Esc to type manually"
height = "60%"

[fzf.sg]
prompt = "> "
color = "hl:-1:underline,hl+:-1:underline:reverse"
preview = "bat --color=always {1} --highlight-line {2}"
preview_window = "up,60%,border-bottom,+{2}+3/3,~3"

[fzf.ff]
prompt = "> "
```

### Notes for customization

- `sg` uses ripgrep vimgrep format, so in previews:
  - `{1}` = file path
  - `{2}` = line number
- Omni-style default for `sg` preview centering is `+{2}+3/3,~3`.
- If `bat` is not installed, Onix falls back to plain file preview (`type`).
- `ff` defaults to no preview (same baseline style as omni defaults); add `preview` / `preview_window` if you want it.
- Restart the command you are running (`sg`, `ff`, etc.) to pick up changes.
