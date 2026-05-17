package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	rdebug "runtime/debug"
)

// buildVersion is set at link time via -ldflags "-X main.buildVersion=<tag>".
// When the user builds with `go build` the variable stays "dev" and we fall
// back to the VCS revision embedded in the build info.
var buildVersion = "dev"

// VersionCmd prints the version, Go runtime, and (when available) the
// commit SHA the binary was built from. Three lines, machine-parseable
// if needed via the `key: value` convention.
type VersionCmd struct{}

func (c *VersionCmd) Run(ctx context.Context, e *env) error {
	v := resolveBuildVersion()
	commit := resolveBuildCommit()

	if e.JSON {
		res := struct {
			Onix   string `json:"onix"`
			Commit string `json:"commit,omitempty"`
			Go     string `json:"go"`
			OSArch string `json:"os_arch"`
		}{v, commit, runtime.Version(), fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)}
		enc := json.NewEncoder(e.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	fmt.Fprintf(e.Stdout, "onix:    %s\n", v)
	if commit != "" {
		fmt.Fprintf(e.Stdout, "commit:  %s\n", commit)
	}
	fmt.Fprintf(e.Stdout, "go:      %s\n", runtime.Version())
	fmt.Fprintf(e.Stdout, "os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}

// resolveBuildVersion returns the best available version identifier.
// Order: -X linker flag, then go.mod version, then VCS revision short SHA,
// then "dev". The short SHA is what `go build` from a git checkout gives
// us by default, which is helpful for bug reports.
func resolveBuildVersion() string {
	if buildVersion != "" && buildVersion != "dev" {
		return buildVersion
	}
	bi, ok := rdebug.ReadBuildInfo()
	if !ok || bi == nil {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return "dev"
}

// resolveBuildCommit returns the full VCS revision if available.
func resolveBuildCommit() string {
	bi, ok := rdebug.ReadBuildInfo()
	if !ok || bi == nil {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}
