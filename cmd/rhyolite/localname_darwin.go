package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// localHostName returns the Bonjour name from System Settings > General >
// Sharing. os.Hostname is not used: routers often override it with
// DHCP-supplied names.
var localHostName = func() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--get", "LocalHostName").Output()
	return string(out), err
}

// localName returns the machine's mDNS name, or "" if it has none.
func localName() string {
	name, err := localHostName()
	if err != nil {
		return ""
	}
	return mdnsName(strings.TrimSpace(name))
}
