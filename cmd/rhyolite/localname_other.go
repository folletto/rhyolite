//go:build !darwin && !linux && !windows

package main

// localName returns "": there is no reliable mDNS name on this platform.
func localName() string { return "" }
