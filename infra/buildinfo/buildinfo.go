// Package buildinfo identifies the source revision of the running binary.
package buildinfo

import "runtime/debug"

// Set at build time with -ldflags "-X budgetmatch-sim/infra/buildinfo.commit=<sha>".
var commit string

func Commit() string {
	if commit != "" {
		return commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
	}
	return "unknown"
}
