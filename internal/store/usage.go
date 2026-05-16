package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RecordUsage appends a timestamped usage entry for an alias to usage.log.
func RecordUsage(home, alias string) error {
	p := filepath.Join(home, "usage.log")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%d,%s\n", time.Now().Unix(), strings.ToLower(strings.TrimSpace(alias)))
	return err
}

// GetFrecencyScores reads usage.log and returns a map of alias names to
// frecency scores.
func GetFrecencyScores(home string) map[string]float64 {
	p := filepath.Join(home, "usage.log")
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()

	scores := make(map[string]float64)
	now := time.Now().Unix()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}

		ts, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		alias := parts[1]

		// Scoring based on recency:
		// < 1 hour: 10 pts
		// < 1 day:  5 pts
		// < 1 week: 2 pts
		// older:    1 pt
		age := now - ts
		var weight float64
		switch {
		case age < 3600:
			weight = 10
		case age < 86400:
			weight = 5
		case age < 604800:
			weight = 2
		default:
			weight = 1
		}
		scores[alias] += weight
	}
	return scores
}
