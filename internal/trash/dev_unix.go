//go:build unix

package trash

import (
	"os"
	"syscall"
)

// deviceOf returns the device ID of path (not following a final symlink).
func deviceOf(path string) (uint64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	return uint64(info.Sys().(*syscall.Stat_t).Dev), nil
}
