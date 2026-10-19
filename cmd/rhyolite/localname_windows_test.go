package main

import (
	"errors"
	"testing"
)

func TestLocalNameWindows(t *testing.T) {
	old := hostname
	defer func() { hostname = old }()

	for _, c := range []struct {
		host string
		err  error
		want string
	}{
		{"DESKTOP-ABC", nil, "DESKTOP-ABC.local"},
		{"desk.corp.example", nil, "desk.local"},
		{"", errors.New("no hostname"), ""},
	} {
		hostname = func() (string, error) { return c.host, c.err }
		if got := localName(); got != c.want {
			t.Errorf("hostname %q, %v: got %q, want %q", c.host, c.err, got, c.want)
		}
	}
}
