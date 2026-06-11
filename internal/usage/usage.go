// Package usage tracks per-alias use counts and last-use times for
// frecency-driven features (the --prune ranking today; pickers and
// suggestions later). The data lives in its own file, not aliases.toml:
// recording happens on the resolve hot path and must never rewrite the
// alias DB. Everything here is best-effort by design — a lost update
// costs a statistic, never a navigation.
package usage

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Entry is one alias's accumulated usage.
type Entry struct {
	Count int   // debounced use count (at most one bump per debounce window)
	Last  int64 // unix seconds of the last counted use
}

// debounce caps how often a single alias's entry is rewritten. Inside the
// window Record is a pure read, so the hot path pays one small file read
// and no write. The count therefore approximates "distinct active hours",
// which ranks better than raw call counts — a tight loop of resolves is
// one use, not fifty.
const debounce = time.Hour

// now is a seam for the debounce tests.
var now = time.Now

// Path returns home/usage.
func Path(home string) string {
	return filepath.Join(home, "usage")
}

// Load reads the usage file into a map keyed by alias name. A missing
// file is an empty map; malformed lines are skipped — the data is
// advisory and must never block a command.
func Load(home string) map[string]Entry {
	out := map[string]Entry{}
	data, err := os.ReadFile(Path(home))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		count, err1 := strconv.Atoi(fields[1])
		last, err2 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil || count < 0 || last < 0 {
			continue
		}
		out[fields[0]] = Entry{Count: count, Last: last}
	}
	return out
}

// Record bumps the alias's entry unless it was already counted inside the
// debounce window. Errors are swallowed (see the package comment).
func Record(home, alias string) {
	alias = strings.ToLower(strings.TrimSpace(alias))
	if alias == "" {
		return
	}
	entries := Load(home)
	e := entries[alias]
	n := now().Unix()
	if e.Last != 0 && n-e.Last < int64(debounce/time.Second) {
		return
	}
	e.Count++
	e.Last = n
	entries[alias] = e
	_ = save(home, entries)
}

// Remove drops the named entries, e.g. for aliases that were just
// deleted. Best-effort, like Record.
func Remove(home string, names []string) {
	entries := Load(home)
	changed := false
	for _, n := range names {
		key := strings.ToLower(strings.TrimSpace(n))
		if _, ok := entries[key]; ok {
			delete(entries, key)
			changed = true
		}
	}
	if changed {
		_ = save(home, entries)
	}
}

// save writes the whole file atomically (temp + rename), one
// "<alias> <count> <last-unix>" line per alias, sorted for stable
// content. Alias names cannot contain whitespace (store validation), so
// space-separated fields parse back unambiguously.
func save(home string, entries map[string]Entry) error {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		e := entries[name]
		fmt.Fprintf(&b, "%s %d %d\n", name, e.Count, e.Last)
	}

	tmp, err := os.CreateTemp(home, ".usage.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, Path(home))
}
