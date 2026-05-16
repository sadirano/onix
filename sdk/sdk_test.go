package sdk

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// 1. Normal case: all variables set.
	t.Setenv("ONIX_TARGET", "/path/to/target")
	t.Setenv("ONIX_ALIAS", "acme")
	t.Setenv("ONIX_MODULE", "my-module")
	t.Setenv("ONIX_ENTRY", "start")
	t.Setenv("ONIX_HOME", "/home/user/.onix")
	t.Setenv("ONIX_EDITOR", "nvim")

	m, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if m.Target != "/path/to/target" {
		t.Errorf("Target = %q, want %q", m.Target, "/path/to/target")
	}
	if m.Alias != "acme" {
		t.Errorf("Alias = %q, want %q", m.Alias, "acme")
	}
	if m.Module != "my-module" {
		t.Errorf("Module = %q, want %q", m.Module, "my-module")
	}
	if m.Entry != "start" {
		t.Errorf("Entry = %q, want %q", m.Entry, "start")
	}
	if m.Home != "/home/user/.onix" {
		t.Errorf("Home = %q, want %q", m.Home, "/home/user/.onix")
	}
	if m.Editor != "nvim" {
		t.Errorf("Editor = %q, want %q", m.Editor, "nvim")
	}

	// 2. Fallback case: ONIX_TARGET missing.
	t.Setenv("ONIX_TARGET", "")
	cwd, _ := os.Getwd()
	m2, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if m2.Target != cwd {
		t.Errorf("Target = %q, want cwd %q", m2.Target, cwd)
	}
}

func TestUnmarshalConfig(t *testing.T) {
	type Config struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	// 1. Valid JSON
	t.Setenv("ONIX_MODULE_CONFIG", `{"name": "test", "count": 42}`)
	var cfg1 Config
	if err := UnmarshalConfig(&cfg1); err != nil {
		t.Errorf("UnmarshalConfig failed: %v", err)
	}
	if cfg1.Name != "test" || cfg1.Count != 42 {
		t.Errorf("Config = %+v, want {Name: test, Count: 42}", cfg1)
	}

	// 2. Empty string
	t.Setenv("ONIX_MODULE_CONFIG", "")
	var cfg2 Config
	if err := UnmarshalConfig(&cfg2); err != nil {
		t.Errorf("UnmarshalConfig failed for empty string: %v", err)
	}

	// 3. Invalid JSON
	t.Setenv("ONIX_MODULE_CONFIG", `{"name": "test",`)
	var cfg3 Config
	if err := UnmarshalConfig(&cfg3); err == nil {
		t.Error("UnmarshalConfig should have failed for invalid JSON")
	}
}
