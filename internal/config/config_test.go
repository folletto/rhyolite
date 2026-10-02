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
