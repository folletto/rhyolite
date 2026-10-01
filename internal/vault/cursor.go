package vault

import (
	"encoding/base64"
)

func encodeCursor(lastPath string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(lastPath))
}

// decodeCursor returns the last path of the previous page. It is validated
// like any client path and must lie inside scope.
func decodeCursor(c, scope string) (string, error) {
	if c == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", errf("invalid cursor")
	}
	p, err := Clean(string(b))
	if err != nil || p == "" || !isWithin(p, scope) {
		return "", errf("invalid cursor")
	}
	return p, nil
}
