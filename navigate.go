package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/segments"
	"github.com/sadirano/onix/internal/store"
)

// autoDefineSegment registers a [[contexts]] entry for a segment that wasn't
// found — with no editor in the loop. The onix flow is "user types intent ->
// user gets where they need to be -> onix executes intent", so an undefined
// segment is created on the spot as a subdirectory rather than dumping the
// user into a TOML editor:
//
//   - no inline value (e.g. `free@play`):   source-template = "/free/"   (literal)
//   - inline value   (e.g. `task:42@play`): source-template = "/${task}/" — this
//     run resolves to /42/, and the segment stays reusable with other values.
//
// The entry is appended to the central per-alias file so any hand-written
// content and comments there are preserved; the resolver re-reads the file and
// proceeds with the resolved fragment. The user can refine the template later
// by editing that file (`onix --edit`), but never has to in order to navigate.
func autoDefineSegment(home, segmentName, inlineValue string, stderr io.Writer, aliasName string) (*segments.ContextDef, error) {
	if err := store.ValidateSegmentName(segmentName); err != nil {
		return nil, err
	}

	template := "/" + segmentName + "/"
	if inlineValue != "" {
		template = "/${" + segmentName + "}/"
	}

	filePath := segments.CentralPath(home, aliasName)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(filePath), err)
	}

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}
	var sb strings.Builder
	sb.WriteString("\n[[contexts]]\n")
	sb.WriteString("segment = \"" + segmentName + "\"\n")
	sb.WriteString("source-template = \"" + template + "\"\n")
	if _, err := f.WriteString(sb.String()); err != nil {
		f.Close()
		return nil, fmt.Errorf("write %s: %w", filePath, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close %s: %w", filePath, err)
	}

	sf, err := segments.LoadSegmentsFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("load segments: %w", err)
	}
	cd, ok := segments.LookupContext(sf, segmentName)
	if !ok {
		return nil, fmt.Errorf("segment %q not found in %s after write (internal error)", segmentName, filePath)
	}

	fmt.Fprintf(stderr, "created segment %q -> %s in %s\n", segmentName, template, filePath)
	return cd, nil
}
