package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
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

// hostsFor lists the addresses clients can use for a listen host.
func hostsFor(listenHost string) []string {
	ip := net.ParseIP(listenHost)
	if listenHost == "" || (ip != nil && ip.IsUnspecified()) {
		if ips := lanIPs(); len(ips) > 0 {
			return ips
		}
		return []string{"localhost"}
	}
	return []string{listenHost}
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

func printBanner(w io.Writer, dir string, cfg *config.Config, port int) {
	hosts := hostsFor(cfg.Host)
	url := func(h string) string { return "http://" + net.JoinHostPort(h, strconv.Itoa(port)) + "/mcp" }

	fmt.Fprintf(w, "▴ Vault: %s\n", dir)
	fmt.Fprintf(w, "▴ Listening on %s\n", url(hosts[0]))
	for _, h := range hosts[1:] {
		fmt.Fprintf(w, "           also %s\n", url(h))
	}
	fmt.Fprintln(w, "▴ Deletes go to the system trash (.trash/ in the vault if the volume has none)")
	fmt.Fprintln(w, "▴ Log: rhyolite.log")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "▴ mcp.json (via mcp-remote, for clients that require HTTPS):")
	fmt.Fprintln(w, pretty(mcpRemoteConfig(url(hosts[0]), cfg.Token)))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "▴ mcp.json (clients with native HTTP support, e.g. Claude Code):")
	fmt.Fprintln(w, pretty(nativeConfig(url(hosts[0]), cfg.Token)))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "▴ Plain HTTP: the token is visible to anyone who can sniff this network.")
	fmt.Fprintln(w, "▴ Press Ctrl+C to stop")
}
