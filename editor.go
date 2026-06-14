package main

import (
	"path/filepath"
	"strings"
)

// -----------------------------------------------------------------------------
// editor dispatch — translate "open file at line" requests into the argv each
// editor family actually understands.
//
// Two families are supported:
//
//	vim / nvim lineage : "+<line> <file>"   (the default)
//	VS Code lineage    : "--goto <file>:<line>"
//
// The grep/find pickers feed us a line number for every selection, so handing
// the vim convention to VS Code makes it open a bogus "+123" file and land the
// real file at line 1. classifyEditor + editorArgs keep each family honest.
// -----------------------------------------------------------------------------

// editTarget is one file the editor should open, optionally at a line.
type editTarget struct {
	file string
	line string // empty when no line is known
}

type editorFamily int

const (
	// familyPlus is the vim lineage: "+<line> <file>". It is the default for
	// anything we do not recognise.
	familyPlus editorFamily = iota
	// familyGoto is the VS Code lineage: "--goto <file>:<line>".
	familyGoto
)

// classifyEditor maps an editor binary to its argument family by base name,
// ignoring any directory prefix and a Windows ".exe" suffix.
func classifyEditor(editor string) editorFamily {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(editor)))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "code", "code-insiders", "codium", "vscodium", "cursor", "windsurf":
		return familyGoto
	default:
		return familyPlus
	}
}

// editorArgs builds the argv tail for the given editor binary, formatting each
// target's line jump in that editor's dialect. Targets without a line are
// passed through verbatim regardless of family.
func editorArgs(editor string, targets []editTarget) []string {
	family := classifyEditor(editor)
	argv := make([]string, 0, len(targets)*2)
	for _, t := range targets {
		switch {
		case t.line == "":
			argv = append(argv, t.file)
		case family == familyGoto:
			argv = append(argv, "--goto", t.file+":"+t.line)
		default: // familyPlus
			argv = append(argv, "+"+t.line, t.file)
		}
	}
	return argv
}
