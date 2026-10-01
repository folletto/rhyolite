//go:build linux

package trash

import (
	"os"
	"path/filepath"
	"time"
)

// System returns the freedesktop.org trash for the current user.
func System() Trasher {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Func(func(string) error { return ErrUnavailable })
		}
		data = filepath.Join(home, ".local", "share")
	}
	return freedesktop{dataHome: data, uid: os.Getuid(), device: deviceOf, now: time.Now}
}
