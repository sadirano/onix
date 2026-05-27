package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sadirano/onix/internal/store"
)

// StatsCmd reports local navigation patterns from usage.log + aliases.toml.
// All data is read from the user's onix home; nothing leaves the machine.
//
// --full and --top are orthogonal: --full controls which sections render
// (top + cold + by-hour histogram, vs. just one of them), --top controls
// how many entries appear in the top list. Passing --top 5 --full shows
// 5 top entries plus cold + histogram.
type StatsCmd struct {
	Top   int    `help:"Number of top aliases to show." default:"10"`
	Since string `help:"Only count activity within this duration (e.g. 7d, 24h)."`
	Cold  bool   `help:"Show aliases not used in the window instead of top-used."`
	Full  bool   `help:"Show the maximal dashboard (top + cold + by-hour histogram). Use --top to control how many top entries."`
}

type aliasStat struct {
	Name     string
	Count    int
	LastUsed time.Time
}

type statsReport struct {
	Today    int             `json:"today"`
	Week     int             `json:"week"`
	Total    int             `json:"total"`
	Distinct int             `json:"distinct_aliases"`
	Top      []aliasStatJSON `json:"top,omitempty"`
	Cold     []string        `json:"cold,omitempty"`
	ByHour   *[24]int        `json:"by_hour,omitempty"`
	SinceTS  int64           `json:"since_ts,omitempty"`
}

type aliasStatJSON struct {
	Name        string `json:"name"`
	Count       int    `json:"count"`
	LastUsedTS  int64  `json:"last_used_ts"`
	LastUsedFmt string `json:"last_used_human"`
}

func (c *StatsCmd) Run(ctx context.Context, e *env) error {
	since, err := parseSince(c.Since)
	if err != nil {
		return err
	}

	entries, err := readUsageLog(filepath.Join(e.Home, "usage.log"))
	if err != nil {
		return err
	}

	now := time.Now()
	report := buildReport(entries, now, since, c.Top, c.Cold, c.Full)

	// Cold view needs the registered-alias set even when no usage is present.
	if c.Cold || c.Full {
		registered, err := registeredAliases(e.Home)
		if err != nil {
			return err
		}
		report.Cold = coldAliases(registered, entries, sinceCutoff(now, since, c.Cold))
	}

	if e.JSON {
		enc := json.NewEncoder(e.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	return renderText(e.Stdout, report, c.Cold, c.Full, now)
}

// usageEntry is one timestamped resolve.
type usageEntry struct {
	ts    time.Time
	alias string
}

func readUsageLog(path string) ([]usageEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []usageEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), ",", 2)
		if len(parts) != 2 {
			continue
		}
		ts, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, usageEntry{
			ts:    time.Unix(ts, 0),
			alias: parts[1],
		})
	}
	return out, nil
}

func parseSince(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	// Accept Go duration syntax plus a few human shortcuts.
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	if strings.HasSuffix(s, "w") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "w"))
		if err == nil && n > 0 {
			return time.Duration(n) * 7 * 24 * time.Hour, nil
		}
	}
	return 0, fmt.Errorf("unrecognised --since value %q (use e.g. 7d, 24h, 30m)", s)
}

// sinceCutoff returns the cutoff time for a window. The cold view defaults to
// 30 days if --since wasn't given so the user gets a meaningful list.
func sinceCutoff(now time.Time, since time.Duration, coldDefault bool) time.Time {
	if since == 0 {
		if coldDefault {
			return now.Add(-30 * 24 * time.Hour)
		}
		return time.Time{} // zero = no cutoff
	}
	return now.Add(-since)
}

func buildReport(entries []usageEntry, now time.Time, since time.Duration, top int, cold, full bool) *statsReport {
	cutoff := sinceCutoff(now, since, false)

	r := &statsReport{}
	if !cutoff.IsZero() {
		r.SinceTS = cutoff.Unix()
	}

	stats := make(map[string]*aliasStat)
	dayStart := now.Truncate(24 * time.Hour)
	weekStart := now.AddDate(0, 0, -7)

	var hist *[24]int
	if full {
		hist = &[24]int{}
	}

	for _, e := range entries {
		if !cutoff.IsZero() && e.ts.Before(cutoff) {
			continue
		}
		r.Total++
		if e.ts.After(dayStart) || e.ts.Equal(dayStart) {
			r.Today++
		}
		if e.ts.After(weekStart) {
			r.Week++
		}
		s, ok := stats[e.alias]
		if !ok {
			s = &aliasStat{Name: e.alias}
			stats[e.alias] = s
		}
		s.Count++
		if e.ts.After(s.LastUsed) {
			s.LastUsed = e.ts
		}
		if hist != nil {
			hist[e.ts.Hour()]++
		}
	}
	r.Distinct = len(stats)
	r.ByHour = hist

	if !cold && (top > 0 || full) {
		ranked := make([]*aliasStat, 0, len(stats))
		for _, s := range stats {
			ranked = append(ranked, s)
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].Count != ranked[j].Count {
				return ranked[i].Count > ranked[j].Count
			}
			return ranked[i].Name < ranked[j].Name
		})
		n := top
		if n > len(ranked) {
			n = len(ranked)
		}
		for _, s := range ranked[:n] {
			r.Top = append(r.Top, aliasStatJSON{
				Name:        s.Name,
				Count:       s.Count,
				LastUsedTS:  s.LastUsed.Unix(),
				LastUsedFmt: humanizeAgo(now.Sub(s.LastUsed)),
			})
		}
	}

	return r
}

func registeredAliases(home string) (map[string]struct{}, error) {
	s, err := store.LoadStore(home)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(s.Names()))
	for _, n := range s.Names() {
		out[strings.ToLower(n)] = struct{}{}
	}
	return out, nil
}

// coldAliases returns the registered aliases that have no usage entry after
// cutoff. An entry whose ts equals or precedes the cutoff counts as cold.
func coldAliases(registered map[string]struct{}, entries []usageEntry, cutoff time.Time) []string {
	used := make(map[string]struct{})
	for _, e := range entries {
		if cutoff.IsZero() || e.ts.After(cutoff) {
			// strip the segment prefix so `docs@acme` still warms `acme`.
			base := e.alias
			if idx := strings.LastIndex(base, "@"); idx >= 0 {
				base = base[idx+1:]
			}
			used[strings.ToLower(base)] = struct{}{}
		}
	}
	cold := make([]string, 0)
	for name := range registered {
		if _, ok := used[name]; !ok {
			cold = append(cold, name)
		}
	}
	sort.Strings(cold)
	return cold
}

func renderText(out io.Writer, r *statsReport, coldOnly, full bool, now time.Time) error {
	if coldOnly {
		if len(r.Cold) == 0 {
			fmt.Fprintln(out, "no cold aliases — every registered alias was used in the window")
			return nil
		}
		fmt.Fprintf(out, "Cold aliases (%d):\n", len(r.Cold))
		for _, name := range r.Cold {
			fmt.Fprintf(out, "  %s\n", name)
		}
		return nil
	}

	if r.Total == 0 {
		fmt.Fprintln(out, "no activity recorded yet — use `o <alias>` and check back")
		return nil
	}

	fmt.Fprintln(out, "Activity:")
	fmt.Fprintf(out, "  Today:      %d navs\n", r.Today)
	fmt.Fprintf(out, "  This week:  %d navs\n", r.Week)
	fmt.Fprintf(out, "  All time:   %d navs across %d aliases\n", r.Total, r.Distinct)
	if r.SinceTS > 0 {
		fmt.Fprintf(out, "  Window:     since %s\n", time.Unix(r.SinceTS, 0).Format("2006-01-02"))
	}

	if len(r.Top) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Top %d aliases:\n", len(r.Top))
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  RANK\tALIAS\tCOUNT\tLAST USED")
		for i, s := range r.Top {
			fmt.Fprintf(w, "  %d\t%s\t%d\t%s\n", i+1, s.Name, s.Count, s.LastUsedFmt)
		}
		_ = w.Flush()
	}

	if full {
		if len(r.Cold) > 0 {
			fmt.Fprintln(out)
			fmt.Fprintf(out, "Cold aliases (%d, not used in last 30d):\n  %s\n",
				len(r.Cold), strings.Join(r.Cold, ", "))
		}
		if r.ByHour != nil {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Activity by hour of day:")
			renderHourHistogram(out, r.ByHour)
		}
	}
	return nil
}

func renderHourHistogram(out io.Writer, h *[24]int) {
	max := 0
	for _, v := range h {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		return
	}
	const barWidth = 30
	for hour := 0; hour < 24; hour++ {
		bar := strings.Repeat("█", h[hour]*barWidth/max)
		fmt.Fprintf(out, "  %02d  %s %d\n", hour, bar, h[hour])
	}
}

// humanizeAgo formats a duration as a compact "N units ago" string.
func humanizeAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	}
}
