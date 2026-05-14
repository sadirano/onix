# ROADMAP

This document outlines the current cleanup and improvement phases for Onix.

## Phase 1: Documentation & Testing Architecture
- [x] Delete outdated `report.md` and `ROADMAP_TO_10.md`.
- [x] Move existing tests from root to their respective `internal/` packages.
- [x] Verify test suite integrity.

## Phase 2: Persistence & TOML Refactor
- [x] Refactor `internal/store` and `internal/plugins` to use robust TOML encoding instead of manual string construction.

## Phase 3: Logic Refactor & Deduplication
- [x] Extract a shared `resolver` abstraction for alias resolution.
- [x] Move side effects (e.g., `os.MkdirAll`) from resolution logic to command execution layer.
- [x] Unify `commands.go` and `fastresolve.go` logic.

## Phase 4: Platform Compatibility & Coverage
- [x] Remove OS-specific hardcodings (e.g., `.exe`, `powershell.exe`).
- [x] Improve Bash/Zsh context support to match PowerShell parity.
- [x] Add unit tests for subcommands: `List`, `Remove`, `Yank`, `Run`, `Exec`.

## Phase 5: Search Shortcuts
- [x] Implement `sg` (grep) and `ff` (find) as first-party subcommands.
- [x] Add visual interactive experience using `fzf` and `bat`.
- [x] Provide full shell integration for POSIX and PowerShell.
