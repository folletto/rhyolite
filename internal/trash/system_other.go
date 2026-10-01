//go:build !linux && !darwin && !windows

package trash

// System has no trash on this platform; callers use their own fallback.
func System() Trasher { return Func(func(string) error { return ErrUnavailable }) }
