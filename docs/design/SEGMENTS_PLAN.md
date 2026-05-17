# Segments redesign — implementation plan

**Status:** implemented. PR 1 = cbe2b37 (parser), PR 2 = 88b1bf7 (schema),
PR 3 = e866fc7 (library), PR 4 = f6c9d85 (resolver + prompt), PR 5 = this
doc-refresh commit.
**Spec:** [SEGMENTS_SPEC.md](SEGMENTS_SPEC.md) (2026-05-17).
**Scope:** sequence the PRs that take us from the current `Subdirs`-map model
to the spec'd `[[contexts]]`-driven resolver, and define the test/bench
strategy each PR carries.

This plan exists because the spec is a breaking change with several
independently-shaped pieces (grammar, schema, resolver, prompts, traversal
guard). Landing them as one PR would be hard to review and impossible to
bisect. Each PR below is sized to be reviewable on its own and to leave
`master` in a buildable, passing state.

---

## Ground rules

- **Order:** PRs land in the sequence below. Each builds on the previous
  PR's exports without changing them.
- **Atomicity:** every PR keeps `go build ./...`, `go test ./...`,
  `gofumpt -l .` (empty), and `go vet ./...` green. CI gates are the
  same gates that run today.
- **Behaviour:** PRs 1–3 are pure additions or carefully gated swaps —
  nothing user-visible changes. The behaviour break (silent literal
  fallback → prompt; `/`-joiner drop) lands in PR 4 and is called out in
  the PR description and CHANGELOG.
- **No legacy compat.** Per the existing project memory, `Subdirs` is
  dropped, not deprecated. PR 2 wipes it from the loader; PR 4 wipes it
  from `ContextDef`.
- **Hot path:** the resolver micro-benchmark
  (`BenchmarkScanForAlias`, `BenchmarkHotPath_LookupOnly`) is recorded
  before PR 1 and re-run after PR 4. The plain-alias path must not
  regress (the spec only touches `@`-containing inputs).
- **Test placement:** unit tests sit next to the file under test in the
  same package. `internal/segments` and `internal/resolver` carry the
  bulk; `e2e_test.go` gets one happy-path scenario per source type.

---

## PR sequence

### PR 1 — Parser: inline `seg:value` (additive)

**Goal:** extend `ParseSegmentedAlias` to return inline values alongside
segment names, without yet changing how segments resolve.

**Touches:**

- `internal/segments/segments.go` — `ParseSegmentedAlias` returns
  `([]ParsedSegment, alias string)` where `ParsedSegment` is
  `{Name string; Value string; HasValue bool}`. The old two-return-value
  signature is replaced; callers update in this PR.
- `internal/resolver/resolver.go` — `resolveSegmented` adopts the new
  return type. Inline values are accepted by the parser but **ignored**
  by the resolver in this PR (preserving today's behaviour).
- `context.go` — `applyContexts` switches to the new return type;
  inline values are ignored.

**Tests:**

- `internal/segments/segments_test.go`:
  - `TestParseSegmentedAlias` table extended with the colon cases:
    - `tasks:123@proja` → `[{tasks, 123, true}], proja`
    - `client:bob@projb` → `[{client, bob, true}], projb`
    - `seg:@a` (empty value) → `[{seg, "", false}], a` (per spec: empty
      inline value is "no inline value")
    - `task:432@client:bob@projb` (both segments carry values)
    - `acme` (no `@`) → `nil, acme`
    - `a:b:c@d` → `[{a, "b:c", true}], d` (split on first colon only)
  - `TestParseSegmentedAlias_DropsEmptySegments` — locks `a@@b` behaviour
    (still drops the empty middle segment).
- `internal/resolver/resolver_test.go`:
  - Extend `TestResolve_Segmented` with an inline-value input and assert
    the value is ignored under PR 1's behaviour (the test is updated in
    PR 4 to assert the new behaviour).
- `context_test.go`: confirm `applyContexts` is unchanged.

**Risk:** low — pure refactor of a parser return type. The "ignored
inline value" rule is in-tree for one PR cycle so the wire-up stays
small.

**Effort:** ~1.5h.

---

### PR 2 — Schema: new `ContextDef` fields + load-time mutex check

**Goal:** make `segments.toml` parseable in the spec's new shape.
`Subdirs` is dropped from the loader. The resolver doesn't read the new
fields yet.

**Touches:**

- `internal/segments/segments.go`:
  - `ContextDef` gains `Param`, `SourceTemplate`, `SourceExec`,
    `SourceFile` fields (with the spec'd TOML tags: `param`,
    `source-template`, `source-exec`, `source-file`).
  - `SegmentsFile.Subdirs` is **removed** (the field, not just the
    behaviour). `LoadSegments` silently ignores the `[subdirs]` table
    on load (using a discarded map during decode).
  - `LoadSegments` enforces "exactly one of `source-template`,
    `source-exec`, `source-file` per context" — more than one is a hard
    error mentioning the offending segment name and the file path.
    Zero source-\* fields is allowed (env-/exec-only contexts still
    work for `apply-context`).
  - `Version` bumped to `3` in `CurrentVersion`. Older `version = 2`
    files are read without complaint; the `[subdirs]` table they may
    contain is dropped on load.
- `internal/resolver/resolver.go`: `resolveSegmented` no longer reads
  `Subdirs` (the field is gone). For PR 2 it falls back to the literal
  segment name with the auto-`/` joiner — same behaviour as today for
  any segment that doesn't have a source-template. (This breaks aliases
  that relied on `[subdirs]`; PR 2 ships with that break called out, OR
  we hold this PR until PR 4 lands. Decision: hold. See "PR 2 landing
  order" below.)
- `internal/store/store.go` — `Alias.Subdirs` field is **removed**.
  Per-alias `subdirs = {...}` blocks in `aliases.toml` are silently
  dropped on load (same mechanism: a discarded decode target).

**Tests:**

- `internal/segments/segments_test.go`:
  - `TestLoadSegments_AllSourceTypes` — a TOML with three contexts,
    each using a different `source-*` field, loads cleanly.
  - `TestLoadSegments_MultipleSourcesError` — two `source-*` set on
    one context → error mentions the segment name.
  - `TestLoadSegments_IgnoresLegacySubdirs` — a TOML with a top-level
    `[subdirs]` table loads without error and produces a `SegmentsFile`
    whose contexts list is empty.
  - `TestLoadSegments_Version3RoundTrip` — write a v3 file, read it,
    assert all fields preserved.
- `internal/store/store_test.go`:
  - `TestLoadStore_IgnoresLegacyAliasSubdirs` — alias entry with a
    `subdirs` map loads cleanly; `Alias` no longer carries the field.

**PR 2 landing order:** PR 2 ships **bundled with PR 4** as a single
"new resolver" commit, OR PR 2 lands first and the resolver
temporarily literalises unknown segments. The bundle is cleaner;
the split is more reviewable. Recommendation: **split**, with PR 2
shipping the schema-only break and PR 4 shipping the resolver
behaviour. Users who land PR 2 in isolation see the `[subdirs]` map
silently drop — same end state as the bundle, just one commit
earlier.

**Risk:** medium — touches store schema. The "exactly one source"
check is the only new validation logic.

**Effort:** ~2h.

---

### PR 3 — Template expansion + traversal guard (library code only)

**Goal:** add the `${VAR}` expander and the traversal guard as pure
functions, with extensive unit tests. Nothing in the resolver calls
them yet.

**Touches:**

- New file `internal/segments/template.go`:
  - `ExpandTemplate(tmpl string, lookup func(name string) (string, bool)) (string, error)`
    — walks the string, finds `${name}` references, calls `lookup` for
    each. Unbound → returns
    `unresolved variable ${<name>} in <where-the-caller-supplies>`.
    Lookup-not-found is distinct from lookup-returns-empty (spec is
    silent; choice: empty value is allowed and substitutes empty
    string; _missing_ is the hard error).
  - `GuardFragment(fragment string) error` — implements the spec's
    traversal guard: splits on `/` and `\`, rejects `..` components,
    null bytes, leading `~`, drive letters, and absolute-path prefixes
    beyond the single leading `/`.
- New file `internal/segments/sources.go`:
  - `EvalTemplateSource(tmpl string, lookup ...) (string, error)` —
    thin wrapper around `ExpandTemplate`.
  - `EvalExecSource(args []string, cwd string, lookup ...) (string, error)`
    — expands each arg, runs `exec.Command`, captures stdout, trims.
    Uses the package-level `execCommand` (added in `internal/segments`,
    not the `main` package one) so tests can inject a fake.
  - `EvalFileSource(path, home, aliasBase string, lookup ...) (string, error)`
    — expands path, resolves `@home/...` / `@alias/...` / absolute /
    `~`-tilde prefixes, reads, trims.

**Tests:**

- `internal/segments/template_test.go`:
  - Single-var, multi-var, missing-var (asserts error mentions name),
    nested-braces-not-supported, dollar-without-brace literal.
- `internal/segments/guard_test.go`:
  - `..` in any component → error with segment name placeholder.
  - Leading `/` alone OK; leading `//` → error.
  - `/foo/../bar` → error.
  - Drive letter `C:foo` → error; bare `foo/C:bar` → error.
  - Null byte → error.
  - `~` at start → error; `~` mid-string → OK (it's a literal char in
    the fragment, not a path prefix).
- `internal/segments/sources_test.go`:
  - Template source: happy path, missing-var error.
  - Exec source: fake `execCommand` returns canned stdout; assert
    trim. Non-zero exit → error mentions stderr.
  - File source: temp file with `@home/` prefix; missing file →
    error mentions the resolved path.

**Risk:** low — all new code, no integration with the rest of the
resolver until PR 4.

**Effort:** ~3h.

---

### PR 4 — Resolver rewrite + unknown-segment prompt

**Goal:** wire the new path. This is the user-visible behaviour change.

**Touches:**

- `internal/segments/segments.go` — `ResolveSegment` is **removed**
  (or kept as a deprecated shim that the resolver no longer calls;
  recommendation: removed, no callers left). Add
  `LookupContext(sf *SegmentsFile, name string) (*ContextDef, bool)`
  for the resolver.
- `internal/resolver/resolver.go` — `resolveSegmented` is rewritten
  following the spec's algorithm:
  1. Load alias, strip trailing `/`, set `target`.
  2. Right-to-left walk over `[]ParsedSegment`.
  3. For each segment: look up context (case-insensitive); on miss,
     call the new prompt callback (added to `Resolve`'s signature) and
     persist; evaluate source; guard fragment; append verbatim.
  4. `MkdirAll target`, return.
  - The `/`-inserting joiner is gone (spec PR's headline change).
- `Resolve` signature: add `segmentPrompter func(segment, inlineValue string) (*segments.ContextDef, error)`.
  Callers in `main` plumb it; `nil` means "hard error on unknown".
- `fastresolve.go`:
  - New free function `promptSegmentDefinition(segment, inlineValue string) (*segments.ContextDef, error)`
    — interactive prompt described in spec §"Unknown-segment prompt".
  - `fastResolve` passes it through (and `nil` when `noPrompt`).
- `context.go` — `applyContexts` looks up contexts via
  `LookupContext`; behaviour unchanged for users with env/exec-only
  contexts.

**Tests:**

- `internal/resolver/resolver_test.go`:
  - `TestResolve_Segmented` rewritten: `docs@acme` with a
    `source-template = "/docs"` context → `<acme>/docs`.
  - `TestResolve_Segmented_InlineValue` — `tasks:123@proja` with
    `source-template = "/${tasks}"` → `<proja>/123`.
  - `TestResolve_Segmented_NoLeadingSlash` — `task:432@client:bob@projb`
    with `client.source-template = "/${client}"` and
    `task.source-template = "_${task}.md"` → `<projb>/bob_432.md`.
  - `TestResolve_Segmented_TraversalRejected` — context whose template
    expands to `../etc/passwd` → error mentions the segment.
  - `TestResolve_Segmented_UnknownNoPrompt` — unknown segment + nil
    prompter → error mentions the segment name.
  - `TestResolve_Segmented_UnknownWithPrompt` — fake prompter returns
    a `ContextDef`; resolver persists it (assert via reload) and
    completes resolution.
- `fastresolve_test.go`:
  - New test driving `fastResolve` against a temp home with a real
    `segments.toml` so the wire-up is exercised end-to-end.
- `e2e_test.go`:
  - One pwsh and one bash scenario for `tasks:123@proja` → `cd` to
    the expected absolute path.

**Risk:** medium-high — the headline behaviour change. The unknown
prompt is the trickiest piece because it has to round-trip a context
into `segments.toml` without disturbing comments or ordering. Lean on
`go-toml/v2`'s round-trip support; if it loses comments, we accept that
(`segments.toml` is config, not source) — confirm with a manual write
test on a commented file.

**Effort:** ~5h including tests.

---

### PR 5 — Docs, examples, migration note

**Goal:** the user-facing surface.

**Touches:**

- `docs/GUIDE.md` — segment section rewritten around `[[contexts]]`
  with the spec's five examples.
- `docs/PLAYBOOK.md` — "how to add a new segment" recipe.
- `README.md` — short note that `[subdirs]` is gone, link to the spec.
- `CHANGELOG.md` — breaking-change entry with the migration recipe
  (replace `[subdirs] foo = "bar"` with
  `[[contexts]] segment = "foo" source-template = "/bar"`).
- `docs/design/SEGMENTS_SPEC.md` — front-matter status flipped from
  "design-approved, not yet implemented" to "implemented in PRs
  #N–#N+3".

**Risk:** none.

**Effort:** ~1.5h.

---

## Benchmark coverage

Two benchmarks, both already in-tree:

- `BenchmarkScanForAlias` (in `internal/resolver`) — the plain-alias
  fast path. Must not regress; the segment rewrite doesn't touch it,
  but PR 2's `Subdirs` removal touches `Store.Alias` which is on this
  path.
- `BenchmarkHotPath_LookupOnly` (in `main`) — end-to-end `fastResolve`
  for a plain alias. Same as above.

Add one new benchmark in PR 4:

- `BenchmarkResolve_Segmented_Template` — `docs@acme` with a
  template-source context. Establishes a baseline so future segment
  changes can be benchmarked. Not gated on a threshold yet.

Run `benchstat` before PR 1 (against `master`) and after PR 4. Attach
the result to PR 4's description.

---

## Out of scope for this batch

These items from the spec's "Out of scope" section stay deferred:

- Multi-source contexts (try env → exec → prompt fallback).
- Auto-export of resolved values as shell env vars.
- TTL caching of exec/file results.
- `onix segment <verb>` CLI surface (add/edit/list/invalidate).
- `source-exec` cwd override.

Additionally:

- **Migration tool.** Per spec, none ships. The CHANGELOG carries the
  recipe.
- **Lint for missing leading `/`.** Trusted to be explicit. If users
  hit it repeatedly we add a warning in a follow-up.

---

## Open questions for review

1. **Bundle PR 2 with PR 4?** Splitting gives reviewable commits but
   leaves `master` in an awkward intermediate state for one merge
   cycle (literal-name resolution with the `/` joiner, but no
   `Subdirs` map to override anything). Bundling is one big PR.
   Default: split. Override?

Do a single PR.

2. **Empty inline value semantics.** Spec says `seg:` is "no inline
   value". Confirm: does that mean `${param}` is unbound (hard error
   if referenced) or bound to empty string (resolves to empty
   substitution)? Plan assumes **unbound** — same as omitting the
   `:`. Confirm?

Yes, omit the `:`.

3. **`ContextDef.Param` default.** Spec says it defaults to the
   segment name. Plan assumes case-sensitive use in templates
   (`${client}` not `${Client}`). Confirm?

insensitive.

4. **Saved-context format on prompt.** When the unknown-segment
   prompt writes a new `[[contexts]]` entry, it appends to the end of
   `segments.toml`. If TOML round-trip loses comments, we accept
   it. Acceptable?

yes

Once these are answered, PR 1 can begin.
