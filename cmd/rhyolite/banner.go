package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"slices"
	"sort"
	"strconv"

	"github.com/folletto/rhyolite/internal/config"
)

// lanIPs returns the machine's non-loopback IPv4 addresses, private ones first.
func lanIPs() []string {
	addrs, _ := net.InterfaceAddrs()
	var private, other []string
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.To4() == nil || n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() {
			continue
		}
		if n.IP.IsPrivate() {
			private = append(private, n.IP.String())
		} else {
			other = append(other, n.IP.String())
		}
	}
	sort.Strings(private)
	sort.Strings(other)
	return append(private, other...)
}

// hostsFor lists the addresses clients can use, best first: the configured
// advertise host, then the mDNS name and the LAN IPs when listening on all
// interfaces, otherwise the listen host itself.
func hostsFor(cfg *config.Config, name string, ips []string) []string {
	var hosts []string
	add := func(h string) {
		if h != "" && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	add(cfg.Advertise)
	ip := net.ParseIP(cfg.Host)
	if cfg.Host == "" || (ip != nil && ip.IsUnspecified()) {
		add(name)
		for _, h := range ips {
			add(h)
		}
	} else {
		add(cfg.Host)
	}
	if len(hosts) == 0 {
		add("localhost")
	}
	return hosts
}

func pretty(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// mcpRemoteConfig is the mcp.json entry for clients that need HTTPS: mcp-remote
// bridges to our plain-HTTP endpoint. The header is passed through an env var
// because some clients mangle spaces inside args.
func mcpRemoteConfig(url, token string) any {
	return map[string]any{"mcpServers": map[string]any{"rhyolite": map[string]any{
		"command": "npx",
		"args":    []string{"-y", "mcp-remote", url, "--allow-http", "--header", "Authorization:${AUTH_HEADER}"},
		"env":     map[string]string{"AUTH_HEADER": "Bearer " + token},
	}}}
}

// nativeConfig is the entry for clients that speak Streamable HTTP directly.
func nativeConfig(url, token string) any {
	return map[string]any{"mcpServers": map[string]any{"rhyolite": map[string]any{
		"type":    "http",
		"url":     url,
		"headers": map[string]string{"Authorization": "Bearer " + token},
	}}}
}

func printBanner(w io.Writer, cfg *config.Config, port int) {
	hosts := hostsFor(cfg, localName(), lanIPs())
	url := func(h string) string { return "http://" + net.JoinHostPort(h, strconv.Itoa(port)) + "/mcp" }

	fmt.Fprintln(w)
	fmt.Fprintln(w, "● Listening")
	for i, h := range hosts {
		label := "on"
		if i > 0 {
			label = "or"
		}
		fmt.Fprintf(w, "  %s: %s\n", label, url(h))
	}
	access := "› Access: local + LAN"
	if cfg.Access == config.AccessLocal {
		access = "› Access: local only"
	}
	fmt.Fprintln(w, access)
	fmt.Fprintln(w, "› Log: rhyolite.log")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "■ Config")
	fmt.Fprintln(w, "  mcp.json (if client requires https):")
	fmt.Fprintln(w, pretty(mcpRemoteConfig(url(hosts[0]), cfg.Token)))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  mcp.json (if client supports http):")
	fmt.Fprintln(w, pretty(nativeConfig(url(hosts[0]), cfg.Token)))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "∙ Press Ctrl+C to stop")
}
