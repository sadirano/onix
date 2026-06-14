# Segments — spec

**Scope:** the `seg@seg@...@alias` syntax and the `[[contexts]]` schema across
the global and per-alias `segments.toml` files.

This document is the authoritative description of how segments resolve. It
reflects the current implementation in `internal/segments` and
`internal/resolver`.

---

## Mental model

Three concepts, kept distinct:

| Concept   | What it is                                | Lives in                       |
|-----------|-------------------------------------------|--------------------------------|
| Alias     | A name bound to an absolute base path     | `aliases.toml`                 |
| Segment   | A token between `@`s, always context-resolved | Parsed from the CLI invocation |
| Context   | The definition of how a segment resolves  | A `[[contexts]]` entry in a `segments.toml` (see below) |

There is no static "subdir map" in config. To navigate to a literal
subdirectory of an alias, type it: `o <alias> <path>` or `o <alias>/sub/dir`.
The `@` syntax is reserved for context-driven, dynamic, or parameterised
resolution.

---

## Where contexts live

A `[[contexts]]` entry can be defined in three places. For each segment they are
consulted in this precedence order, and the first match wins:

1. **Per-alias, local:** `<alias-path>/.onix/segments.toml` — travels with the
   project directory.
2. **Per-alias, central:** `~/.onix/segments/<alias>.toml` — kept under the onix
   home, named by the lowercase alias.
3. **Global:** `~/.onix/segments.toml` — shared across every alias, but **only**
   entries carrying `scope = "global"` are visible here. An unscoped entry in the
   global file is ignored during lookup.

The `scope` gate exists so the shared global file does not leak segment names
into every alias by accident. **Per-alias files (1 and 2) need no `scope`** —
every entry in them is implicitly scoped to that one alias. Only the global
file requires the opt-in.

`onix --contexts` lists the contexts defined in the global `~/.onix/segments.toml`.

---

## Grammar

A segmented invocation has the shape:

```
[<seg>[:<value>]@]...<alias>
```

- Segments are separated by `@`. The rightmost token is the alias.
- A segment may carry an inline value via colon: `seg:value`. Only the first
  `:` splits; `a:b:c` parses as name `a`, value `b:c`.
- The empty inline value (`seg:`) is treated as "no inline value" — fall back to
  the configured source.
- Empty segments from consecutive `@`s (`a@@b`) are dropped (trimmed away).

Order is right-to-left, innermost first: in `task:432@client:bob@projb`,
`projb` is the alias, `client` is the innermost segment, `task` is outermost.

---

## Schema — `segments.toml`

```toml
[[contexts]]
segment = "client"             # required: the segment name

# Optional. In the GLOBAL ~/.onix/segments.toml an entry must set
# scope = "global" to be visible. Ignored (harmless) in per-alias files.
scope = "global"

# Optional: name to bind the inline value under. Defaults to the segment name.
# Lets a template use `${clientId}` while the CLI says `client:bob`.
param = "clientId"

# The path fragment, as a template. `${VAR}` references are expanded
# (see "Variable resolution order"). Omitted → the context contributes no
# path fragment (it can still supply env vars, below).
source-template = "/${clientId}"

# Optional env map: supplies ${VAR} values during resolve-time lookup. A value
# here takes precedence over a same-named shell variable, and is used verbatim
# (not re-expanded). Not exported to the shell after cd.
env = { REGION = "us-east-1" }
```

### The source field

`source-template` is the only source kind. It is a string with `${VAR}`
references; after expansion the result is the path fragment.

> **Note:** earlier designs described `source-exec` (run a command, capture
> stdout) and `source-file` (read a file's contents). **Neither is
> implemented** — only `source-template` exists. To compute a fragment at
> resolve time, populate a variable in the environment and reference it from a
> template, or feed it through the context's `env` map.

A context with no `segment` is invalid. A context with no `source-template`
loads cleanly but contributes nothing to the path; if such a context is invoked
*with* an inline value (`seg:value`), that is a hard error — there is no template
to consume the value.

---

## Variable resolution order

For any `${name}` reference inside a `source-template`:

1. **Segment-bound inline value.** If the segment was invoked as `seg:value`,
   `${<param>}` (default `${<segment>}`) is bound to `value`.
2. **Context's static `env` map.** If the context declares `env = { FOO = "..." }`,
   `${FOO}` reads from there. Because this is checked before the process
   environment, a context `env` value **overrides** a same-named shell variable
   (it is a pinned value, not a fallback), and is used verbatim — env values are
   not themselves re-expanded.
3. **Process environment.** Consulted only if the name was bound by neither of
   the above.
4. **Unbound → hard error.** Resolution stops and reports the unresolved
   variable name.

The context's `env` is consulted only during resolve-time lookup; it is **not**
exported to the shell after cd.

---

## Resolution algorithm

Given `seg1[:v1]@seg2[:v2]@...@alias`:

1. Parse into the segments list and the alias. An empty alias or no segments is
   an error.
2. Look up the alias in `aliases.toml`. Unknown → hard error.
3. Load the global file and the two per-alias files (any missing file is fine).
4. Strip a trailing `/` from the alias path. Call this `target`.
5. For each segment, **innermost-first** (right-to-left):
   - Find a matching context, in precedence order: local → central → global
     (`scope = "global"` only). Matching is case-insensitive; within a file the
     first matching entry wins.
   - If no context is found → **auto-create** the segment (below). With
     `--no-prompt` / `-q` nothing is created and this is a hard error.
   - Evaluate `source-template` to a **fragment** (an empty result contributes
     nothing and is skipped).
   - Run the **traversal guard** on the fragment (below).
   - Append the fragment to `target` with **no auto-inserted separator** —
     templates own their separators.
6. Return `target` (host-native, via `filepath.FromSlash`).

`Resolve` does not create directories; whatever the resolved path feeds (a `cd`,
an editor open, an Explorer launch) decides that — identical to a plain alias.

### Path joiner

`target` becomes `target + fragment`, verbatim. The template author chooses
whether to lead with `/` (directory) or not (filename suffix). A common context:

```toml
[[contexts]]
segment = "prod"
scope = "global"
source-template = "/prod"
```

That leading `/` is **load-bearing**; without it the fragment appends directly,
producing `<alias>prod` rather than `<alias>/prod`. There is no lint for this —
explicit was preferred over magic.

### Traversal guard

After each fragment is computed and before it joins `target`:

- Reject any fragment containing a null byte.
- Split on `/` and `\`; reject any component equal to `..`.
- Allow at most one leading `/` (the template's directory-separator prefix). A
  second `/`, a leading `\`, a leading `~`, or a leading drive-letter pattern
  (`[A-Za-z]:`) is rejected.

The guard runs *after* template expansion, so a template like `${USER_INPUT}`
that resolves to `../../etc/passwd` is caught.

---

## Unknown-segment auto-create

The onix flow is *type intent → get where you need to be → onix executes intent*,
so an undefined segment is created on the spot — no editor, no interruption. When
a segment matches no context (and prompting is enabled), onix appends a
`[[contexts]]` entry to the **central per-alias file**
(`~/.onix/segments/<alias>.toml`) mapping the segment to a subdirectory, then
resumes resolution:

- **No inline value** (`free@play`) → a literal subdirectory:

  ```toml
  [[contexts]]
  segment = "free"
  source-template = "/free/"
  ```

- **Inline value** (`task:432@play`) → a parameterised template, so the segment
  stays reusable with other values; this run resolves to `/432/`:

  ```toml
  [[contexts]]
  segment = "task"
  source-template = "/${task}/"
  ```

The entry is appended (existing content and comments are preserved), and because
it lives in a per-alias file it needs no `scope`. Refine the template later with
`onix --edit` if you want something fancier than a subdirectory — but you never
have to in order to navigate. With `--no-prompt` / `-q`, nothing is created and
an undefined segment is a hard error.

---

## Examples

### Example 1 — inline value, simple

```toml
# ~/.onix/segments/proja.toml   (per-alias — no scope needed)
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
# ~/.onix/segments/projb.toml
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
# resolves to: C:/projectb/bob_432.md  →  `e` (--edit) opens it in $EDITOR
```

Trace:
- `target = "C:/projectb"` (trailing `/` stripped)
- innermost `client:bob` → `/${client}` → `/bob` → `target = "C:/projectb/bob"`
- outermost `task:432` → `_${task}.md` → `_432.md` → `target = "C:/projectb/bob_432.md"`

### Example 3 — a shared global context

```toml
# ~/.onix/segments.toml   (global — scope is required)
[[contexts]]
segment = "docs"
scope = "global"
source-template = "/documentation"
```

```sh
$ s docs@anyalias
# Explorer at <anyalias>/documentation — works for every alias
```

### Example 4 — pinned value via `env`

```toml
# ~/.onix/segments/code.toml
[[contexts]]
segment = "logs"
source-template = "/logs/${ENV}"
env = { ENV = "dev" }     # binds ${ENV} during resolve; wins over any shell $ENV
```

```sh
$ o logs@code              # → <code>/logs/dev
$ $env:ENV = "prod"; o logs@code   # still → <code>/logs/dev (env map overrides the shell)
```

To vary the value per call instead, drop `env` and pass an inline value —
`source-template = "/logs/${logs}"` with `o logs:prod@code`.

---

## Out of scope (not implemented)

- **`source-exec` / `source-file`.** Removed; only `source-template` resolves a
  fragment. Compute values outside onix and pass them via the environment or the
  context's `env` map.
- **Auto-export of resolved values** as shell env vars. The `env` map feeds
  resolve-time lookup only.
- **Multi-source contexts / fallback chains.**
- **A `onix segment <verb>` management CLI.** Edit the `segments.toml` files
  directly, or let the unknown-segment prompt create the entry.

---

## Implementation pointers

| Concern                              | File / function                                       |
|--------------------------------------|-------------------------------------------------------|
| Parse `seg:value` tokens             | `internal/segments/segments.go` — `ParseSegmentedAlias`, `parseSegmentToken` |
| Context schema                       | `internal/segments/segments.go` — `ContextDef`, `SegmentsFile` |
| File locations / loaders             | `internal/segments/segments.go` — `Path`, `LocalPath`, `CentralPath`, `LoadSegmentsFile` |
| Context lookup (+ global scope gate) | `internal/segments/segments.go` — `LookupContext`, `LookupGlobalContext` |
| Resolve segments                     | `internal/resolver/resolver.go` — `resolveSegmented`, `evalSegment` |
| Template expansion                   | `internal/segments/template.go` — `ExpandTemplate`, `EvalTemplateSource` |
| Traversal guard                      | `internal/segments/template.go` — `GuardFragment` |
| Unknown-segment auto-create          | `navigate.go` — `autoDefineSegment` |
