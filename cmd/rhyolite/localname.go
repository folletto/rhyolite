package main

import "regexp"

// dnsLabel matches a single DNS label: letters, digits and inner hyphens,
// at most 63 characters.
var dnsLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// mdnsName turns a bare machine name into its mDNS name ("Studio" ->
// "Studio.local"), or returns "" when the name is not a valid DNS label.
func mdnsName(label string) string {
	if !dnsLabel.MatchString(label) {
		return ""
	}
	return label + ".local"
}
