package main

import (
	"runtime/debug"
	"strings"
)

// version is set at build time with -ldflags "-X main.version=...". When it is
// empty, the version is the one Go embeds in the binary from git: the tag on
// the built commit, or a pseudo-version naming the commit.
var version string

// resolveVersion returns the CLI version without a leading "v", or "dev" when
// none is known.
func resolveVersion() string {
	if version != "" {
		return strings.TrimPrefix(version, "v")
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return versionFromBuildInfo(info)
}

func versionFromBuildInfo(info *debug.BuildInfo) string {
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return strings.TrimPrefix(v, "v")
	}
	return "dev"
}
