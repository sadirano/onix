package alias

import (
	"os"
	"path/filepath"
	"strings"
)

// SubdirFilePath returns the global subdir registry path (~/.onix/subdirs.env).
func SubdirFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".onix", "subdirs.env")
}

// ResolveSubdir looks up name in the local registry (<base>/subdirs.env) first,
// then the global registry (~/.onix/subdirs.env). Falls back to name as-is when
// no entry is found in either — raw directory names always work without
// registration, just like raw filesystem paths work without alias registration.
//
// base is the resolved alias path (ONIX_TARGET equivalent in module context),
// used to locate the local registry.
func ResolveSubdir(name, base string) string {
	if v, ok := lookupSubdir(name, filepath.Join(base, "subdirs.env")); ok {
		return v
	}
	if v, ok := lookupSubdir(name, SubdirFilePath()); ok {
		return v
	}
	return name
}

func lookupSubdir(name, path string) (string, bool) {
	for k, v := range parseSubdirFile(path) {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

// parseSubdirFile reads a key=value file in the same format as the alias file.
// Returns nil silently when the file does not exist — both registries are optional.
func parseSubdirFile(path string) map[string]string {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\xef\xbb\xbf") // strip BOM

	result := make(map[string]string)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k != "" && v != "" {
			result[k] = v
		}
	}
	return result
}
