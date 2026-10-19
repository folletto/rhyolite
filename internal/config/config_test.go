package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCreates(t *testing.T) {
	dir := t.TempDir()
	c, st, err := Load(dir)
	if err != nil || st != Created {
		t.Fatalf("Load = %v, %v", st, err)
	}
	if len(c.Token) < 40 || c.Port != 6270 || c.Host != "0.0.0.0" {
		t.Fatalf("bad defaults: %+v", c)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestLoadReadsExisting(t *testing.T) {
	dir := t.TempDir()
	first, _, _ := Load(dir)
	second, st, err := Load(dir)
	if err != nil || st != Read {
		t.Fatalf("Load = %v, %v", st, err)
	}
	if second.Token != first.Token {
		t.Error("token changed on re-read")
	}
}

func TestEmptyTokenIsRegeneratedAndOtherFieldsKept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	os.WriteFile(path, []byte(`{"version":1,"token":"","host":"127.0.0.1","port":9999}`), 0o600)
	c, st, err := Load(dir)
	if err != nil || st != Read {
		t.Fatalf("Load = %v, %v", st, err)
	}
	if c.Token == "" || c.Host != "127.0.0.1" || c.Port != 9999 {
		t.Fatalf("unexpected: %+v", c)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), c.Token) {
		t.Error("new token not written back to file")
	}
}

func TestMissingFieldsGetDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{"token":"abc"}`), 0o600)
	c, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "abc" || c.Port != DefaultPort || c.Host != DefaultHost {
		t.Fatalf("unexpected: %+v", c)
	}
}

func TestMalformedIsAnErrorAndNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	os.WriteFile(path, []byte(`{not json`), 0o600)
	if _, _, err := Load(dir); err == nil {
		t.Fatal("expected error")
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{not json` {
		t.Error("malformed file was overwritten")
	}
}

func TestTokensAreUnique(t *testing.T) {
	a, _ := NewToken()
	b, _ := NewToken()
	if a == b {
		t.Fatal("tokens equal")
	}
}

func TestAdvertiseIsOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	Load(dir)
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "advertise") {
		t.Errorf("empty advertise written:\n%s", data)
	}
	content := `{"version":1,"token":"abc","host":"0.0.0.0","port":6270,"advertise":"studio.lan"}`
	os.WriteFile(path, []byte(content), 0o600)
	c, _, err := Load(dir)
	if err != nil || c.Advertise != "studio.lan" {
		t.Fatalf("Load = %+v, %v", c, err)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Errorf("complete file rewritten:\n%s", data)
	}
}

func TestAdvertiseValidation(t *testing.T) {
	for v, ok := range map[string]bool{
		"studio.lan":        true,
		"Studio.local":      true,
		"192.168.1.20":      true,
		"fe80::1":           true,
		"http://studio.lan": false,
		"studio.lan:6270":   false,
		"[fe80::1]":         false,
		"studio.lan/mcp":    false,
		"my mac":            false,
		".studio":           false,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, FileName), []byte(`{"token":"abc","advertise":"`+v+`"}`), 0o600)
		_, _, err := Load(dir)
		if (err == nil) != ok {
			t.Errorf("advertise %q: err = %v", v, err)
		}
		if err != nil && !strings.Contains(err.Error(), "advertise") {
			t.Errorf("advertise %q: error does not name the field: %v", v, err)
		}
	}
}
