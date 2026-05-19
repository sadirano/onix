# Segments — spec

**Scope:** the `seg@seg@...@alias` syntax and `segments.toml` schema.

This document is the authoritative description of how segments resolve.

---

## Why this exists

Today, `o test@play` silently resolves to `<play>/test` even when `test` is
not a registered subdir anywhere. Combined with `fastResolve`'s
unconditional `MkdirAll`, this means typos like `o tst@play` quietly create
junk directories. `[[contexts]]` exists but is invisible to the resolver —
it only emits post-CD env vars and shell commands. There is no mechanism for
a context to contribute to the path itself.

The redesign:

- Removes the silent literal-name fallback. Unknown segments prompt the user
  to define them.
- Makes `[[contexts]]` the single place a segment is defined, with new
  fields that drive path resolution.
- Adds inline values (`seg:value`) so callers can pass arguments through
  the segment syntax — e.g. `o task:432@client:bob@projb` resolves an
  arbitrary task/client combination without registering each one.
- Hands separator control to the template author. Templates that start with
  `/` are directory components; templates that don't are direct appendages.

---

## Mental model

Three concepts, kept distinct:

| Concept   | What it is                                | Lives in                       |
|-----------|-------------------------------------------|--------------------------------|
| Alias     | A name bound to an absolute base path     | `aliases.toml`                 |
| Segment   | A token between `@`s, always context-resolved | Parsed from CLI invocation |
| Context   | The definition of how a segment resolves and what shell state it sets | `segments.toml` `[[contexts]]` |

There is no longer any notion of a "static subdir map" in config. If you
want to navigate to a literal subdirectory of an alias, type it: the alias
form `o <alias> <path>` or just `o <alias>/sub/dir`. The `@` syntax is
reserved for context-driven, dynamic, or parameterised resolution.

---

## Grammar

A segmented invocation has the shape:

```
[<seg>[:<value>]@]...<alias>
```

- Segments are separated by `@`. The rightmost token is the alias.
- A segment may carry an inline value via colon: `seg:value`.
- The empty inline value (`seg:`) is treated as "no inline value" — fall
  back to the configured source.
- Multiple `@`s with empty segments between them (`a@@b`) are an error —
  matches today's `ParseSegmentedAlias` behaviour of dropping empty
  segments via `strings.TrimSpace`.

Innermost-to-outermost order is right-to-left: in `task:432@client:bob@projb`,
`projb` is the alias, `client` is the innermost segment, `task` is outermost.
This matches today's `resolver.go:156` loop direction.

---

## Schema — `segments.toml`

```toml
version = 3

[[contexts]]
segment = "client"             # required: the segment name

# Optional: name to bind inline value under. Defaults to the segment name.
# Lets templates use `${clientId}` while the CLI says `client:bob`.
param = "clientId"

# Resolution — exactly ONE of source-template / source-exec / source-file.
# Omitted means resolution always requires an inline value (else hard error).
source-template = "/${clientId}"

# Post-CD scripting — unchanged, optional. env values also overlay into the
# resolve-time variable lookup (see "Variable resolution order" below).
env  = { CLIENT_ID = "${clientId}" }
exec = ["pwsh", "-c", "Write-Host 'switched client'"]
```

### Source types

Exactly one of the following may be set on a context. Setting more than one
is a hard error on load.

| Field             | Value type      | Semantics                                          |
|-------------------|-----------------|----------------------------------------------------|
| `source-template` | string          | Templated string. `${VAR}` references expanded.    |
| `source-exec`     | array of string | Command + args. Cwd = alias base path. Trimmed stdout becomes the fragment. Each arg is template-expanded. |
| `source-file`     | string          | Path to a file. Contents are read and trimmed. Path is template-expanded and supports the prefixes below. |

#### `source-file` path prefixes

| Prefix             | Resolves to                              |
|--------------------|------------------------------------------|
| `/...` or `C:\...` | Absolute path (after `~` and `$ENV` expansion) |
| `@alias/...`       | Relative to the current alias's base path |
| `@home/...`        | Relative to onix home (`$ONIX_HOME` or `~/.onix`) |

### Removed schema

- `[subdirs]` (top-level): **removed**. Silently ignored on load. Users who
  relied on this see the new unknown-segment prompt on first use.
- `subdirs = {...}` (per-alias in `aliases.toml`): **removed**. Same
  treatment.

---

## Variable resolution order

For any `${name}` reference inside a template, exec arg, or file path:

1. **Segment-bound inline value.** If the current segment was invoked with
   `seg:value`, `${<param>}` (default `${<segment>}`) is bound to `value`.
2. **Context's static `env` map.** If the context declares
   `env = { FOO = "..." }`, `${FOO}` reads from there.
3. **Parent shell env.** Whatever was set in the calling process.
4. **Unbound → hard error.** Resolution stops, the user sees the unresolved
   variable name and where in the template it appeared.

The context's static `env` is overlaid onto the shell env, not the other
way around — so a context-declared `env` value beats a same-named shell
env var during the resolve. (Note: the same `env` map is also written to
the shell post-CD by `apply-context`; that path is unchanged.)

---

## Resolution algorithm

Given `seg1[:v1]@seg2[:v2]@...@alias`:

1. Look up the alias. If unknown → existing alias-not-found prompt /
   fuzzy-match flow (unchanged from `resolver.go:46-133`).
2. Strip any trailing `/` from the alias path. Call this `target`.
3. For each segment, **innermost-first** (right-to-left in the segments
   list):
   - Find a `[[contexts]]` entry whose `segment` matches (case-insensitive).
   - If no context exists → invoke the **unknown-segment prompt** (see
     below). The prompt creates a context and saves it. Resume with the new
     context.
   - If context exists:
     - If an inline value was given, bind `${<param>}` to it for this
       segment's evaluation.
     - If both `source-*` is set and an inline value was given: the inline
       value drives the template via `${<param>}`; the configured
       `source-*` evaluation still runs normally (so a `source-template`
       can reference `${<param>}` and other vars).
     - If no inline value and no `source-*` is set → hard error.
   - Evaluate the source:
     - `source-template`: expand `${VAR}` refs.
     - `source-exec`: expand `${VAR}` in each arg, run command in alias
       base dir, trim stdout. Non-zero exit → hard error including stderr.
     - `source-file`: expand `${VAR}` in path, resolve prefix, read file,
       trim. Missing/unreadable → hard error.
   - The result is a **fragment**. Run the traversal guard on it (below).
   - Append the fragment directly to `target` with **no auto-inserted
     separator**. Templates own their separators.
4. `MkdirAll target` (unchanged from today).
5. Return `target`.

### Path joiner

Replaces today's `resolver.go:155-159` `target + "/" + part` loop. New rule:
`target` becomes `target + fragment`, verbatim. The template author chooses
whether to lead with `/` (directory) or not (filename suffix).

A common simple context will look like:

```toml
[[contexts]]
segment = "prod"
source-template = "/prod"
```

That `/` is **load-bearing**; without it the fragment appends directly,
producing `<alias>prod` rather than `<alias>/prod`. There is no automatic
lint or warning for this — explicit was preferred over magic.

### Traversal guard

After each fragment is computed and before it joins `target`:

- Split the fragment on `/` and `\`.
- Reject any component that equals `..` → hard error
  `segment '<name>' escaped its alias`.
- Reject any fragment that begins with `/`, `\`, `~`, or a drive letter
  pattern (`[A-Za-z]:`) **other than** the single leading `/` that the
  template uses for path separation. (I.e., `/foo/bar` is fine — `/foo` is
  the separator-bearing prefix, `bar` is the next component.)
- Reject any fragment containing a null byte.

The guard runs *after* template expansion, so a template like
`${USER_INPUT}` that resolves to `../../etc/passwd` is caught.

---

## Unknown-segment prompt

When a segment has no `[[contexts]]` entry, fire an interactive prompt
(skip if `--no-prompt` / `-q` is set; in that case → hard error).

```
segment 'task' is not defined.
[inline value: 432]                 # shown only if invoked as task:432

Pick a source:
  [1] template (e.g. /${task}, /tickets/${task}/notes)
  [2] exec     (run a command, capture stdout)
  [3] file     (read a file's contents)
  [4] literal  (alias for template with no ${...} refs)

> 1
Template:
> /tickets/${task}

Save to segments.toml? [Y/n] y
Saved [[contexts]] segment = "task", source-template = "/tickets/${task}"
```

- The inline value (`432` above) is recalled in the banner but isn't itself
  written to config — it's just the current invocation's value.
- Selecting `[4] literal` is sugar for picking `template` with no `${...}`
  refs. The stored field is still `source-template`.
- The save step is implicit (the design picked "always save"); the `[Y/n]`
  is shown only to allow cancel, not to allow declining the save.
- After save, resolution resumes from step 3 with the new context.

The prompt uses the same I/O surface as today's unknown-alias prompt
(`promptDestination` in `fastresolve.go`).

---

## Backward compatibility

| Today                              | After                                       |
|------------------------------------|---------------------------------------------|
| `[subdirs]` in `segments.toml`     | Ignored on load. Silent — no warning.       |
| `subdirs = {...}` in `aliases.toml`| Ignored on load. Silent.                    |
| `[[contexts]]` with only `env`/`exec` | Unchanged. Continues to drive post-CD scripting only; segment has no resolution role until a `source-*` field is added. |
| `o unknown@alias` (silent literal) | Prompt fires; user defines the segment.     |
| Path joiner inserts `/` between segments | Joiner appends directly; templates control separators. |
| `ResolveSegment` falls back to literal name | `ResolveSegment` errors / prompts.    |
| `MkdirAll` always                  | Unchanged.                                  |

Users with no segmented aliases see no behaviour change. Users with
`subdirs` config will see the unknown-segment prompt on first use of each
segment; defining them in `[[contexts]]` (typically with `source-template
= "/<the-old-value>"`) restores the old behaviour.

There is no automatic migration tool. The design picked "ignore" over
"warn", "error", or "auto-convert".

---

## Examples

### Example 1 — inline value, simple

```toml
# segments.toml
[[contexts]]
segment = "tasks"
source-template = "/${tasks}"

# aliases.toml
[proja]
path = "C:/proja/"
```

```sh
$ o tasks:123@proja
C:/proja/123
```

### Example 2 — inline values, filename composition

```toml
# segments.toml
[[contexts]]
segment = "client"
source-template = "/${client}"

[[contexts]]
segment = "task"
source-template = "_${task}.md"     # no leading / — appends to previous

# aliases.toml
[projb]
path = "C:/projectb/"
```

```sh
$ e task:432@client:bob@projb
# resolves to: C:/projectb/bob_432.md
# `e` (--edit) opens it in $EDITOR
```

Trace:
- `target = "C:/projectb"` (trailing `/` stripped)
- innermost segment `client:bob` → template `/${client}` → `/bob` →
  `target = "C:/projectb/bob"`
- outermost segment `task:432` → template `_${task}.md` → `_432.md` →
  `target = "C:/projectb/bob_432.md"`

### Example 3 — `source-exec` capturing git branch

```toml
[[contexts]]
segment = "branch"
source-exec = ["git", "rev-parse", "--abbrev-ref", "HEAD"]
```

```sh
$ o branch@code
# runs `git rev-parse --abbrev-ref HEAD` in <code>'s base path
# captures stdout (e.g. "feature/foo"), appends as-is — produces
# <code>feature/foo (almost certainly NOT what you want)
```

The author needs a leading `/` somewhere. Either wrap with a template
context layer, or use a template directly:

```toml
[[contexts]]
segment = "branch"
source-template = "/${BRANCH}"      # reads $BRANCH from shell env
env = { BRANCH = "main" }           # default if unset

# Or, if you must compute it at resolve time, source-exec + a wrapper
# script that prepends '/':
[[contexts]]
segment = "branch"
source-exec = ["pwsh", "-c", "'/' + (git rev-parse --abbrev-ref HEAD)"]
```

The leading-`/` discipline is the price of "templates own separators".

### Example 4 — `source-file` with `@home` prefix

```toml
[[contexts]]
segment = "task"
source-file = "@home/state/current-task"
```

```sh
$ echo "TASK-9001" > ~/.onix/state/current-task
$ o task@work
# reads ~/.onix/state/current-task → "TASK-9001"
# fragment = "TASK-9001" → appended directly (probably wants a /)
```

### Example 5 — context with both resolution and post-CD scripting

```toml
[[contexts]]
segment = "prod"
source-template = "/prod"

env = { DEPLOY_ENV = "production", KUBECTL_CTX = "prod-cluster" }
exec = ["kubectl", "config", "use-context", "prod-cluster"]
```

```sh
$ o web@prod
# resolves to <web>/prod, then sets DEPLOY_ENV and switches kubectl context
```

---

## Out of scope (for now)

- **Multi-source contexts** (try env → fall back to exec → fall back to
  prompt). Rejected for simplicity; users compose fallbacks inside their
  exec script.
- **Auto-export of resolved values** as shell env vars. Opt-in: user adds
  the var to the context's `env` map themselves.
- **TTL caching** of exec/file results. Each resolve re-evaluates.
- **Lint / warning** for templates that start with an alphanumeric char
  (likely-missing leading `/`). Trusted to be explicit.
- **`onix segment <verb>` CLI surface** for managing contexts (add, edit,
  invalidate, list). Today the user edits `segments.toml` directly or uses
  the unknown-segment prompt; a higher-level UX may follow later.
- **`source-exec` per-context cwd override.** Default = alias base. If a
  use case for shell-cwd-based exec emerges, add a `cwd = "shell" | "alias" | "home" | "<path>"` field then.

---

## Implementation pointers

Where the work lands:

| Concern                              | File / function                                    |
|--------------------------------------|----------------------------------------------------|
| Parse `seg:value` token              | `internal/segments/segments.go:101` `ParseSegmentedAlias` (extend to return inline values) |
| Resolve segments                     | `internal/segments/segments.go:74` `ResolveSegment` (rewrite for context lookup + source eval) |
| Path joiner                          | `internal/resolver/resolver.go:155-159` `resolveSegmented` (drop the `/` insert) |
| Schema for sources                   | `internal/segments/segments.go:15-19` `ContextDef` (add `Param`, `SourceTemplate`, `SourceExec`, `SourceFile`) |
| Schema loader (subdirs ignore, source mutex check) | `internal/segments/segments.go:37` `LoadSegments` |
| Unknown-segment prompt               | new function alongside `promptDestination` in `fastresolve.go` |
| `apply-context` env overlay (resolve-time vs post-CD) | `context.go:57-131` (overlay logic, unchanged shell-side emission) |
| `--no-prompt` propagation            | already plumbed via `fastResolve(..., noPrompt, ...)` — extend the same flag |
| Traversal guard                      | new helper in `internal/segments` or `internal/resolver` |

A separate implementation plan (which PRs in which order, with test
strategy and benchmark coverage) will be drafted when this spec is signed
off.
