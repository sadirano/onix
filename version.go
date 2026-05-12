package main

import (
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

func (c *VersionCmd) Run() error {
	fmt.Printf("onix:    %s\n", resolveBuildVersion())
	fmt.Printf("go:      %s\n", runtime.Version())
	fmt.Printf("os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
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
