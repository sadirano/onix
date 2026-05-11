package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/errs"
	"github.com/sadirano/onix/internal/opener"
)

// splitContextKey splits a key of the form "seg@alias" into (aliasName, segName).
// When there is no "@", the whole key is treated as the alias name and segName is "".
func splitContextKey(key string) (aliasName, segName string) {
	if i := strings.Index(key, "@"); i >= 0 {
		return key[i+1:], key[:i]
	}
	return key, ""
}

// writeAliasContextConfig persists cc into the alias's Lua file.
// key is either "seg@alias" (segment-level) or "alias" (alias-level context).
func writeAliasContextConfig(key string, cc config.ContextConfig) error {
	aliasName, segName := splitContextKey(key)
	e, err := alias.Load(aliasName)
	if err != nil {
		return err
	}
	if e == nil {
		e = &alias.Entry{}
	}
	if segName != "" {
		if e.Segments == nil {
			e.Segments = make(map[string]config.ContextConfig)
		}
		e.Segments[segName] = cc
	} else {
		e.Context = cc
	}
	return alias.Save(aliasName, e)
}

// loadAliasContextConfig reads context config from the alias's Lua file.
// key is either "seg@alias" (segment-level) or "alias" (alias-level context).
// Returns (zero, false) when no config exists.
func loadAliasContextConfig(key string) (config.ContextConfig, bool) {
	aliasName, segName := splitContextKey(key)
	e, err := alias.Load(aliasName)
	if err != nil || e == nil {
		return config.ContextConfig{}, false
	}
	if segName != "" {
		cc, ok := e.Segments[segName]
		return cc, ok
	}
	if e.Context == (config.ContextConfig{}) {
		return config.ContextConfig{}, false
	}
	return e.Context, true
}

// clearAliasContext removes the context config for key from the alias's Lua file.
func clearAliasContext(key string) error {
	aliasName, segName := splitContextKey(key)
	e, err := alias.Load(aliasName)
	if err != nil || e == nil {
		return nil
	}
	if segName != "" {
		delete(e.Segments, segName)
	} else {
		e.Context = config.ContextConfig{}
	}
	return alias.Save(aliasName, e)
}

// aliasFilePath returns the path to the Lua file for the given alias.
// key may be "seg@alias" or a plain alias name.
func aliasFilePath(key string) string {
	aliasName, _ := splitContextKey(key)
	return filepath.Join(alias.Dir(), aliasName+".lua")
}


// contextVarName returns the identifier to use as a named placeholder in
// templates: the var name for env source, the file path for file source, and
// the cmd string for cmd source. Returns "" when no meaningful name exists.
func contextVarName(cc config.ContextConfig) string {
	switch cc.Source {
	case "file":
		return cc.File
	case "cmd":
		return cc.Cmd
	default:
		return cc.Var
	}
}

// applyContextTemplate substitutes placeholders in template with value.
// Supports both {value} (generic) and {varName} (the configured var/cmd/file name).
// Leading/trailing path separators are stripped so the result can be safely
// joined with filepath.Join. When template is empty, value is returned as-is.
func applyContextTemplate(template, varName, value string) string {
	if template == "" {
		return value
	}
	result := strings.ReplaceAll(template, "{value}", value)
	if varName != "" {
		result = strings.ReplaceAll(result, "{"+varName+"}", value)
	}
	return strings.Trim(result, "/\\")
}

// resolveContext returns the active context string for a segment.
// Checks the per-segment config file first, then falls back to the global
// context section in config.lua.
func resolveContext(segmentName string, cfg *config.Config) (string, error) {
	if cc, ok := loadAliasContextConfig(segmentName); ok {
		return resolveContextConfig(cc)
	}
	if !cfg.HasContext() {
		return "", nil
	}
	return resolveContextConfig(cfg.Context)
}

// resolveContextConfig resolves a context value from a ContextConfig.
func resolveContextConfig(cc config.ContextConfig) (string, error) {
	var val string
	var err error

	switch cc.Source {
	case "file":
		val, err = contextFromFile(cc.File)
	case "cmd":
		val, err = contextFromCmd(cc.Cmd)
	case "alias":
		val, err = contextFromAlias(cc.Path)
	default: // "env"
		val, err = contextFromEnv(cc.Var)
	}

	if err != nil {
		return "", err
	}
	return sanitizeContextValue(val), nil
}

// sanitizeContextValue removes whitespace, wrapping quotes, and characters
// that are illegal in Windows path segments.
func sanitizeContextValue(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`)
	v = strings.TrimSpace(v)
	// Strip characters that are illegal in Windows filenames/paths.
	// We keep / and \ as they may be intentional subdirectories.
	bad := []string{`"`, `*`, `?`, `<`, `>`, `|`}
	for _, b := range bad {
		v = strings.ReplaceAll(v, b, "")
	}
	return v
}

// applySegment resolves a single @ segment using its context config, returning
// the path part to append. key is the storage key (seg@alias); seg is used only
// for display. Returns an error when no context config exists.
func applySegment(seg, key string, cfg *config.Config, debugEnabled bool) (string, error) {
	cc, ok := loadAliasContextConfig(key)
	if !ok {
		return "", fmt.Errorf("segment %q: no context configured", seg)
	}
	if cc.Source == "alias" {
		part := strings.Trim(sanitizeContextValue(cc.Path), "/\\")
		if debugEnabled {
			fmt.Printf("[ONIX] segment %q → alias=%q\n", seg, part)
		}
		return part, nil
	}
	val, err := resolveContextConfig(cc)
	if err != nil {
		return "", fmt.Errorf("segment %q: %w", seg, err)
	}
	part := applyContextTemplate(cc.Template, contextVarName(cc), val)
	if debugEnabled {
		fmt.Printf("[ONIX] segment %q → template=%q value=%q → %q\n", seg, cc.Template, val, part)
	}
	return part, nil
}

func contextFromAlias(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("[context] alias path is empty")
	}
	return path, nil
}

func contextFromEnv(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("[context] var not configured")
	}
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return "", fmt.Errorf("context env var %q is not set", name)
	}
	return v, nil
}

func contextFromFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("[context] file not configured")
	}
	b, err := os.ReadFile(expandTilde(path))
	if err != nil {
		return "", fmt.Errorf("read context file %q: %w", path, err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("context file %q is empty", path)
	}
	return v, nil
}

func contextFromCmd(command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("[context] cmd not configured")
	}
	out, err := exec.Command("cmd.exe", "/C", command).Output()
	if err != nil {
		return "", fmt.Errorf("context command %q: %w", command, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("context command %q produced no output", command)
	}
	return v, nil
}

// walkSegments resolves each @ segment against target right-to-left, returning
// the final target path. aliasName is the root alias (after the last @) and is
// combined with each segment name to form the storage key (e.g. "sg@play"),
// keeping configs for the same segment name under different aliases separate.
// When a segment has no context config the user is prompted to configure one,
// which is then saved for future invocations.
func walkSegments(segments []string, aliasName string, target string, cfg *config.Config, debugEnabled bool) string {
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		key := seg + "@" + aliasName
		if _, ok := loadAliasContextConfig(key); !ok {
			cc, ok := promptContextConfig(seg)
			if !ok {
				os.Exit(errs.ExitOK)
			}
			if err := writeAliasContextConfig(key, cc); err != nil {
				errs.FatalCode(errs.ExitErr, "save context for %q: %v", key, err)
			}
		}
		part, err := applySegment(seg, key, cfg, debugEnabled)
		if err != nil {
			errs.FatalCode(errs.ExitErr, "%v", err)
		}
		target = filepath.Join(target, part)
	}
	return target
}

// handleCtxCommand executes a context operation for key (e.g. "sg@play").
// seg is the segment name used in messages. args are the sub-arguments after "ctx":
//
//	(none)                      open context file in editor
//	--clear                     remove context config
//	env <var> [tmpl]            write env-source config
//	cmd <command> [tmpl]        write cmd-source config
//	file <path> [tmpl]          write file-source config
//	alias <subdir>              write alias-source config
func handleCtxCommand(key, seg string, args []string, cfg *config.Config) {
	switch {
	case len(args) == 0:
		p := aliasFilePath(key)
		if err := opener.RunEditorCommand(cfg.ResolveEditor(), filepath.Dir(p), filepath.Base(p)); err != nil {
			errs.Fatal("%v", err)
		}
	case args[0] == "--clear":
		if err := clearAliasContext(key); err != nil {
			errs.Fatal("clear context: %v", err)
		}
		fmt.Printf("Context for %q cleared\n", key)
	case args[0] == "env" || args[0] == "cmd" || args[0] == "file" || args[0] == "alias":
		if len(args) < 2 {
			errs.FatalCode(errs.ExitUsage, "usage: ctx %s <value> [template]", args[0])
		}
		cc := config.ContextConfig{Source: args[0]}
		switch args[0] {
		case "env":
			cc.Var = args[1]
		case "cmd":
			cc.Cmd = args[1]
		case "file":
			cc.File = args[1]
		case "alias":
			cc.Path = args[1]
		}
		if len(args) >= 3 {
			cc.Template = args[2]
		}
		if err := writeAliasContextConfig(key, cc); err != nil {
			errs.Fatal("write context: %v", err)
		}
		fmt.Printf("Context for %q: source=%s value=%s template=%s\n", key, cc.Source, args[1], cc.Template)
	default:
		errs.FatalCode(errs.ExitUsage, "unknown ctx arg %q — use env, cmd, file, alias, or --clear", args[0])
	}
}

func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return home + path[1:]
}
