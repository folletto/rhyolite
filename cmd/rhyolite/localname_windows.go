package main

import (
	"os"
	"strings"
)

var hostname = os.Hostname

// localName returns the machine's mDNS name: Windows 10 and later answer
// mDNS queries for the computer name.
func localName() string {
	h, err := hostname()
	if err != nil {
		return ""
	}
	h, _, _ = strings.Cut(h, ".")
	return mdnsName(h)
}
