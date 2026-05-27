# Roadmap

> **Mode:** Maintenance. Onix is stable enough for daily use; no active
> friction has been reported. This document is a *parked-feature list*
> with explicit triggers, not a marching plan.

## Goals

* **Personal daily driver, polished.** Build only features that the
  primary user (the maintainer) hits friction on. Hypothetical-user
  features go in **Anti-goals** below, not Later.
* **Don't break what works.** Bench gate stays green; smoke test stays
  current; ISSUES.md audits drive unscheduled fix work.
* **Single binary, one machine at a time.** Cross-machine sync is an
  anti-goal — machines stay deliberately separate.

## Active queue

Nothing. Onix is in maintenance mode. If you find yourself reaching for
a feature listed below, promote it here — but only because *you* hit it,
not because it would be nice to have.

## Maintenance commitments

These keep the bar where it is. They're not "items" to ship; they're
standing obligations that any commit must respect.

* `scripts/smoke.ps1` stays current as features land or change behavior
  (most recent example: per-alias segment scope required updating the
  smoke fixture to carry `scope = "global"`).
* `go.mod` and CI Go versions stay aligned (currently `1.26`).
* `go vet`, `govulncheck`, and `go test ./...` stay green in CI (the
  remaining gates after the maintenance-mode trim).
* Periodic ISSUES.md sweeps when something feels stale; close fast.

## Parked features

Each item carries an explicit **trigger** — the concrete moment that
moves it from parked to active. If you're tempted to promote without a
trigger having happened, treat the temptation as a signal that the
trigger description is wrong, not that the item is ready.

### Reliability

#### `[M]` Concurrent-write safety
Cross-platform advisory lock around mutations of `aliases.toml`,
`config.toml`, `plugins.toml`, `segments.toml`. Atomic writes already
prevent corruption; this closes the lost-write race.

**Trigger:** You notice an alias has gone missing after running `onix add`
in two shells, OR a future feature makes write contention more likely
(e.g. a plugin that auto-registers aliases).

#### `[M]` Context segment teardown
`context apply` sets env vars on entry but never unsets them. A shell
that hops between aliases ends up with stale `${BRANCH}` / `${TASKS}` /
etc. from prior `cd`s.

**Trigger:** A real bug caused by a stale segment var (template reads
prior value and produces a wrong path / branch / etc.).

### Navigation UX

#### `[S]` Undo for the last destructive operation
One-deep journal under `~/.onix/undo.json` so `onix --undo` reverses the
most recent `--remove` (alias or file) or `plugin remove`.

**Trigger:** You typo a `--remove` and have to recreate the entry by hand.

#### `[M]` Cross-shell nav history (`o -` / `o +`)
Append-only nav log at `~/.onix/nav.log`, capped depth, per-shell
position keyed by PID.

**Trigger:** You reach for `o -` more than once in a single session and
notice the absence.

#### `[M]` Action composition
Let actions invoke other actions in their template. Cycle detection in
the validator.

**Trigger:** You catch yourself writing a wrapper script that just
chains two `onix -X` calls.

### Observability

#### `[L]` Structured trace mode (`ONIX_DEBUG=1` with `slog`)
Thread a `slog.Logger` through `env` for structured tracing. Default
off, zero allocations on the hot path when disabled. Complements the
existing `ONIX_TIMING=1` phase table.

**Trigger:** A real debugging session where `ONIX_TIMING` shows *when*
something is slow but not *why*.

### Performance

#### `[L]` Daemon mode
Long-running process behind a named pipe / Unix socket, eliminating
per-invocation process-spawn overhead.

**Trigger:** Resolve latency becomes a felt problem. Current floor per
smoke: ~8ms resolve, ~6ms for a no-op Go binary — Onix is 2ms over the
OS floor. Daemon mode shaves ~5ms in exchange for IPC, crash-recovery,
and a long-running process. Not worth it unless someone is actually
counting milliseconds.

### Plugin ecosystem

Treated as a single cluster, low-priority until a real third-party
plugin you want to install lands. You install your own plugins today,
so the trust story is "nice to have."

#### `[M]` Plugin verification strategy (research → proposal)
Survey lazy.nvim, vim-plug, asdf, mise. Produce `docs/PLUGIN_TRUST.md`
with what to verify on first install, what to re-verify on update, what
to defer.

**Trigger:** You want to install someone else's Onix plugin and feel
the gap, OR an OSS user submits a plugin to a (not-yet-existing)
registry.

#### `[L]` Plugin capability model (sandbox)
Manifest-declared capabilities (filesystem paths, network, env vars);
default-deny.

**Blocked on:** Plugin verification strategy landing first.

#### `[L]` Verified plugin registry
Discovery layer with provenance, signatures, reviews.

**Blocked on:** Plugin capability model.

---

## Anti-goals

Listed so they're not picked up by mistake. Each was actively considered
and rejected.

* **Multi-machine sync / workspace tier.** Machines are kept
  deliberately separate. Adding sync would make Onix more complex *for
  the maintainer* in exchange for a feature the maintainer doesn't want.
* **macOS support.** No CI runner, no maintainer story.
* **fish / nu / zsh integrations.** PowerShell + bash cover the shells
  we use.
* **Signed releases (cosign).** Would matter if Onix had a third-party
  user base verifying provenance. It doesn't.
* **Active OSS pursuit.** No growth target. The repo can be cloned and
  built; that's the support contract.
* **GUI / TUI dashboards.** `--stats` text output is enough.
* **General Go-binary package manager.** `onix plugin` is for Onix
  plugins, not general tooling.
* **Replacing `cd`.** Onix wraps it; doesn't try to be it.

## How this list grows

New items belong here only when:

1. A specific friction was felt (not imagined), and
2. The trigger that would unpark the item can be written concretely.

If you can't write the trigger, the item isn't ready to be parked. Drop
it into ISSUES.md as a §5 open question instead, or just live with the
gap until the trigger is articulable.

When a parked item *does* unpark, promote it to **Active queue** with a
short note on what triggered it. After shipping, move it to the
"recently shipped" prose in `_Index.md` of the vault and prune the
roadmap entry. Roadmap entries that have been parked for more than a
year without movement should be reviewed: is the trigger still
plausible, or has the codebase made the item obsolete?
