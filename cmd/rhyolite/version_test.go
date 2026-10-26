package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersionFromLdflags(t *testing.T) {
	defer func(v string) { version = v }(version)
	for _, v := range []string{"1.2.3", "v1.2.3"} {
		version = v
		if got := resolveVersion(); got != "1.2.3" {
			t.Errorf("version %q: got %q", v, got)
		}
	}
}

func TestVersionFromBuildInfo(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"v1.2.3", "1.2.3"},
		{"v0.0.0-20261026102300-abcdef123456+dirty", "0.0.0-20261026102300-abcdef123456+dirty"},
		{"(devel)", "dev"},
		{"", "dev"},
	} {
		info := &debug.BuildInfo{Main: debug.Module{Version: tc.in}}
		if got := versionFromBuildInfo(info); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}
