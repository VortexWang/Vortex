package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSKeyringPrivateVersionedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-keys.json")
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	valid := `{"current":2,"keys":{"1":"` + key + `","2":"` + key + `"}}`
	if e := os.WriteFile(path, []byte(valid), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := dnsCryptoFile(path); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{`{}`, `{"current":1,"keys":{"1":"SECRET_INVALID_KEY"}}`, strings.Replace(valid, `"current":2`, `"current":3`, 1), strings.Replace(valid, `"1":`, `"01":`, 1), valid + `{}`, strings.TrimSuffix(valid, "}") + `,"endpoint":"https://untrusted.invalid"}`} {
		if e := os.WriteFile(path, []byte(bad), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := dnsCryptoFile(path); e == nil || strings.Contains(e.Error(), "SECRET_") || strings.Contains(e.Error(), "untrusted") {
			t.Fatal("unsafe keyring accepted or echoed", e)
		}
	}
	if e := os.WriteFile(path, []byte(valid), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := dnsCryptoFile(path); e == nil {
		t.Fatal("publicly readable encryption key accepted")
	}
}
