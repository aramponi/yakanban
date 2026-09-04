// Package version carries the build identity, stamped at link time.
package version

import "runtime/debug"

// Version is overridden with -ldflags "-X .../version.Version=v1.2.3".
var Version = "dev"

// Commit is overridden at build time when available.
var Commit = ""

// String renders the version for --version and the API User-Agent.
func String() string {
	v := Version
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
	}
	if Commit != "" {
		return v + " (" + Commit + ")"
	}
	return v
}

// UserAgent is sent with every API call, so a rate-limited admin can tell
// which tool the traffic came from.
func UserAgent() string { return "yakanban/" + String() }
