package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Error is a client-facing error. Messages never contain host paths.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errf(format string, a ...any) error { return &Error{fmt.Sprintf(format, a...)} }

func errNotFound(rel string) error { return errf("not found: %q", display(rel)) }

func display(rel string) string {
	if rel == "" {
		return "/"
	}
	return rel
}

// Messages returned for the stale-file rules (see PLAN.md §7).
var (
	errModified = &Error{"File has been modified since it was read. Read it again before writing to it."}
	errPartial  = &Error{"File was only partially read. Read the full file before overwriting it."}
)

// sanitize converts any error into a client-safe one. Unknown errors lose
// their text, because *PathError messages contain absolute host paths.
func (v *Vault) sanitize(rel string, err error) error {
	if err == nil {
		return nil
	}
	var ve *Error
	if errors.As(err, &ve) {
		return ve
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errNotFound(rel)
	case errors.Is(err, fs.ErrExist):
		return errf("already exists: %q", display(rel))
	case errors.Is(err, fs.ErrPermission):
		return errf("permission denied: %q", display(rel))
	}
	msg := err.Error()
	if strings.Contains(msg, "not a directory") {
		return errf("a parent of %q is not a folder", display(rel))
	}
	if strings.Contains(msg, "is a directory") {
		return errf("%q is a folder", display(rel))
	}
	if strings.Contains(msg, "directory not empty") {
		return errf("folder is not empty: %q", display(rel))
	}
	return errf("operation failed on %q", display(rel))
}
