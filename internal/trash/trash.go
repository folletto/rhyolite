// Package trash moves files to the operating system's trash.
package trash

import "errors"

// ErrUnavailable means the system has no usable trash for that file (for
// example a network volume, or an unsupported OS). Callers may fall back to
// their own trash folder. Any other error is a real failure: the file must be
// left where it is.
var ErrUnavailable = errors.New("no system trash available")

// Trasher moves an absolute path to the trash.
type Trasher interface {
	Trash(absPath string) error
}

// Func adapts a function to Trasher.
type Func func(absPath string) error

func (f Func) Trash(p string) error { return f(p) }
