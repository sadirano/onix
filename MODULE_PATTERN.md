# Onix Module Pattern

This document defines the standard pattern for creating new Onix modules. Follow this structure to ensure your module integrates seamlessly with the Onix ecosystem.

## 1. Project Structure

A standard Onix module is a standalone Go repository with the following structure:

```text
onix-my-module/
├── internal/           # Private implementation details
│   └── logic/
├── go.mod              # Go module definition
├── main.go             # Entry point (parses environment and args)
├── onix.toml           # (Optional) Multi-entry point manifest
├── README.md           # Documentation
└── LICENSE             # License file
```

## 2. The Module Contract

Onix modules are executed as independent binaries. The core `onix` binary resolves the target directory, sets up the environment, and then executes the module binary.

### Environment Variables

Your module should read these variables to understand its context:

| Variable | Description |
|----------|-------------|
| `ONIX_TARGET` | Absolute path to the resolved target directory. |
| `ONIX_ALIAS` | The alias string the user typed (e.g., `acme`). |
| `ONIX_MODULE` | The name of your module as declared in config. |
| `ONIX_ENTRY` | The specific entry point being invoked (for multi-entry modules). |
| `ONIX_MODULE_CONFIG` | JSON-encoded string from the `[module.config]` block in `config.toml`. |
| `ONIX_HOME` | Path to the Onix home directory (`~/.onix`). |
| `ONIX_EDITOR` | The user's preferred editor (e.g., `nvim`, `code`). |

### Execution Context

- **Working Directory**: The module binary is executed with the current working directory (CWD) set to `ONIX_TARGET`.
- **Arguments**: Any arguments passed after the alias are forwarded to the module binary.
- **Input/Output**: `stdin`, `stdout`, and `stderr` are attached to the parent shell.

## 3. Implementation Pattern (`main.go`)

Use this boilerplate as a starting point for your `main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	// Define your module-specific config here
	DefaultOption string `json:"default_option"`
}

func main() {
	// 1. Gather context from environment
	target := os.Getenv("ONIX_TARGET")
	if target == "" {
		// Fallback for direct execution/testing
		var err error
		target, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: could not determine target directory\n")
			os.Exit(1)
		}
	}

	// 2. Parse module-specific configuration
	var cfg Config
	if confStr := os.Getenv("ONIX_MODULE_CONFIG"); confStr != "" {
		if err := json.Unmarshal([]byte(confStr), &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not parse ONIX_MODULE_CONFIG: %v\n", err)
		}
	}

	// 3. Handle arguments
	args := os.Args[1:]
	
	// For multi-entry modules, ONIX_ENTRY will be set.
	// The entry name is also prepended to args by the dispatcher.
	entry := os.Getenv("ONIX_ENTRY")

	// 4. Execute logic
	if err := run(target, entry, args, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(target, entry string, args []string, cfg Config) error {
	fmt.Printf("Running module in: %s\n", target)
	// Your logic here
	return nil
}
```

## 4. Multi-Entry Modules (`onix.toml`)

If your module provides multiple distinct commands (e.g., `git` has `pull`, `push`), use an `onix.toml` manifest:

```toml
# onix.toml in the module root
[[entry]]
name = "subcommand1"
cmd  = "s1"           # Optional: the actual command name to use in the shell

[[entry]]
name = "subcommand2"
cmd  = "s2"
```

Onix will generate separate `.cmd` wrappers for each entry. When invoked via a wrapper, `ONIX_ENTRY` will be set to the `name` field, and that name will also be passed as the first argument to your binary.

## 5. Internal Package Pattern

When contributing to the core `onix` codebase (under `internal/`), follow these rules:

1.  **Surgical Responsibility**: Each package should do exactly one thing (e.g., `alias` for resolution, `config` for TOML).
2.  **Error Handling**:
    - Return `error` from functions.
    - Use `%w` to wrap errors.
    - Use `internal/errs` for fatal exits only in `main.go` or top-level dispatchers.
3.  **No Global State**: Prefer structs and methods over global variables to keep packages testable.
4.  **Testing**: Every package MUST have a `_test.go` file with high coverage.
5.  **Paths**: Use `internal/config` to get standard paths (`config.Dir()`, `config.BinDir()`) instead of hardcoding.
