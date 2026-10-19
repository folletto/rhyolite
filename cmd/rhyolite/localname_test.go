package main

import (
	"slices"
	"testing"

	"github.com/folletto/rhyolite/internal/config"
)

func TestMDNSName(t *testing.T) {
	for in, want := range map[string]string{
		"Studio":                 "Studio.local",
		"my-mac-2":               "my-mac-2.local",
		"":                       "",
		"-studio":                "",
		"studio-":                "",
		"my mac":                 "",
		"studio.home":            "",
		"Café":                   "",
		string(make([]byte, 64)): "",
	} {
		if got := mdnsName(in); got != want {
			t.Errorf("mdnsName(%q) = %q, want %q", in, got, want)
		}
	}
	long := "a" + string(slices.Repeat([]byte("b"), 62))
	if mdnsName(long) != long+".local" {
		t.Error("63-character label rejected")
	}
	if mdnsName(long+"c") != "" {
		t.Error("64-character label accepted")
	}
}

func TestHostsFor(t *testing.T) {
	ips := []string{"192.168.1.20", "10.0.0.5"}
	for _, c := range []struct {
		desc      string
		host      string
		advertise string
		name      string
		ips       []string
		want      []string
	}{
		{"name first, then IPs", "0.0.0.0", "", "Studio.local", ips, []string{"Studio.local", "192.168.1.20", "10.0.0.5"}},
		{"empty host listens everywhere", "", "", "Studio.local", ips, []string{"Studio.local", "192.168.1.20", "10.0.0.5"}},
		{"IPv6 wildcard", "::", "", "Studio.local", nil, []string{"Studio.local"}},
		{"no name: IPs only", "0.0.0.0", "", "", ips, ips},
		{"advertise wins", "0.0.0.0", "studio.lan", "Studio.local", ips, []string{"studio.lan", "Studio.local", "192.168.1.20", "10.0.0.5"}},
		{"advertise de-duplicated", "0.0.0.0", "10.0.0.5", "", ips, []string{"10.0.0.5", "192.168.1.20"}},
		{"specific host only", "127.0.0.1", "", "Studio.local", ips, []string{"127.0.0.1"}},
		{"specific host with advertise", "127.0.0.1", "studio.lan", "Studio.local", ips, []string{"studio.lan", "127.0.0.1"}},
		{"nothing found", "0.0.0.0", "", "", nil, []string{"localhost"}},
	} {
		cfg := &config.Config{Host: c.host, Advertise: c.advertise}
		if got := hostsFor(cfg, c.name, c.ips); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.desc, got, c.want)
		}
	}
}
