// Package config loads and creates rhyolite.json, the server settings file.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the settings file kept in the vault root.
const FileName = "rhyolite.json"

// Defaults for a freshly created settings file.
const (
	DefaultHost = "0.0.0.0"
	DefaultPort = 6270 // 6-2-7 spells M-C-P on a phone keypad; unassigned by IANA.
)

// Config is the content of rhyolite.json.
type Config struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

// Status says what Load did with the file.
type Status int

const (
	Created Status = iota // a new file was written
	Read                  // an existing file was used as is
)

// NewToken returns 32 random bytes, base64url-encoded.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Load reads dir/rhyolite.json, creating it when missing. A file whose token
// is empty gets a new token, and the file is rewritten. Missing host/port
// fields get the defaults.
func Load(dir string) (*Config, Status, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := &Config{Version: 1, Host: DefaultHost, Port: DefaultPort}
		if c.Token, err = NewToken(); err != nil {
			return nil, Created, err
		}
		return c, Created, c.save(path)
	}
	if err != nil {
		return nil, Read, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, Read, fmt.Errorf("%s is not valid JSON: %w", FileName, err)
	}
	dirty := false
	if c.Version == 0 {
		c.Version, dirty = 1, true
	}
	if c.Host == "" {
		c.Host, dirty = DefaultHost, true
	}
	if c.Port == 0 {
		c.Port, dirty = DefaultPort, true
	}
	if c.Token == "" {
		if c.Token, err = NewToken(); err != nil {
			return nil, Read, err
		}
		dirty = true
	}
	if dirty {
		if err := c.save(path); err != nil {
			return nil, Read, err
		}
	}
	return &c, Read, nil
}

func (c *Config) save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
