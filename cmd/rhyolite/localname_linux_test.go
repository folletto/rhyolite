package main

import (
	"errors"
	"testing"
)

func TestLocalNameLinux(t *testing.T) {
	oldHost, oldAvahi := hostname, avahiRunning
	defer func() { hostname, avahiRunning = oldHost, oldAvahi }()

	avahi := true
	avahiRunning = func() bool { return avahi }
	host, hostErr := "studio.example.com", error(nil)
	hostname = func() (string, error) { return host, hostErr }

	if got := localName(); got != "studio.local" {
		t.Errorf("with Avahi: %q", got)
	}
	avahi = false
	if got := localName(); got != "" {
		t.Errorf("without Avahi: %q", got)
	}
	avahi, hostErr = true, errors.New("no hostname")
	if got := localName(); got != "" {
		t.Errorf("hostname error: %q", got)
	}
	host, hostErr = "bad_name", nil
	if got := localName(); got != "" {
		t.Errorf("invalid hostname: %q", got)
	}
}
