package main

import (
	"os"
	"strings"
)

var hostname = os.Hostname

// avahiRunning reports whether Avahi, which answers mDNS queries for the
// hostname, is running.
var avahiRunning = func() bool {
	_, err := os.Stat("/run/avahi-daemon/pid")
	return err == nil
}

// localName returns the machine's mDNS name, or "" if nothing answers for it.
func localName() string {
	if !avahiRunning() {
		return ""
	}
	h, err := hostname()
	if err != nil {
		return ""
	}
	h, _, _ = strings.Cut(h, ".")
	return mdnsName(h)
}
