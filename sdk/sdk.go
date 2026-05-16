package sdk

import (
	"encoding/json"
	"fmt"
	"os"
)

// Metadata holds the execution context provided by the parent onix process.
type Metadata struct {
	// Target is the absolute path to the directory the alias resolved to.
	Target string
	// Alias is the name of the alias the user typed.
	Alias string
	// Module is the name of the module as declared in the onix configuration.
	Module string
	// Entry is the specific entry point being invoked (for multi-entry modules).
	Entry string
	// Home is the path to the onix home directory (~/.onix).
	Home string
	// Editor is the user's preferred editor (e.g., "nvim", "code").
	Editor string
}

// Load reads the context from environment variables.
// If ONIX_TARGET is missing, it falls back to the current working directory.
func Load() (*Metadata, error) {
	target := os.Getenv("ONIX_TARGET")
	if target == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("could not determine target directory: %w", err)
		}
		target = cwd
	}

	return &Metadata{
		Target: target,
		Alias:  os.Getenv("ONIX_ALIAS"),
		Module: os.Getenv("ONIX_MODULE"),
		Entry:  os.Getenv("ONIX_ENTRY"),
		Home:   os.Getenv("ONIX_HOME"),
		Editor: os.Getenv("ONIX_EDITOR"),
	}, nil
}

// UnmarshalConfig decodes the ONIX_MODULE_CONFIG JSON string into v.
// If the environment variable is empty, it unmarshals "{}" (an empty object).
func UnmarshalConfig(v any) error {
	confStr := os.Getenv("ONIX_MODULE_CONFIG")
	if confStr == "" {
		confStr = "{}"
	}
	if err := json.Unmarshal([]byte(confStr), v); err != nil {
		return fmt.Errorf("could not parse ONIX_MODULE_CONFIG: %w", err)
	}
	return nil
}
