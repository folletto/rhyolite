package main

import (
	"errors"
	"testing"
)

func TestLocalNameDarwin(t *testing.T) {
	old := localHostName
	defer func() { localHostName = old }()

	for _, c := range []struct {
		out  string
		err  error
		want string
	}{
		{"Studio\n", nil, "Studio.local"},
		{"", errors.New("scutil failed"), ""},
		{"My Mac\n", nil, ""},
	} {
		localHostName = func() (string, error) { return c.out, c.err }
		if got := localName(); got != c.want {
			t.Errorf("scutil %q, %v: got %q, want %q", c.out, c.err, got, c.want)
		}
	}
}
