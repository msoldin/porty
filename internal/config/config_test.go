package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaultsFileEnvironmentAndFlagsInOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "porty.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listen: 127.0.0.1:7000\ndata_dir: /from/file\nlog_format: text\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{
		"PORTY_DATA_DIR":   "/from/environment",
		"PORTY_LOG_FORMAT": "json",
	}
	lookup := func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}

	got, err := Load([]string{"--config", path, "--listen", "0.0.0.0:8080"}, lookup)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Server.Listen != "0.0.0.0:8080" {
		t.Fatalf("listen = %q, want flag value", got.Server.Listen)
	}
	if got.DataDir != "/from/environment" {
		t.Fatalf("data dir = %q, want environment value", got.DataDir)
	}
	if got.LogFormat != "json" {
		t.Fatalf("log format = %q, want environment value", got.LogFormat)
	}
	if got.MaxEditableFileBytes != 1<<20 {
		t.Fatalf("max editable bytes = %d, want default 1048576", got.MaxEditableFileBytes)
	}
}

func TestLoadRejectsPartialTLSConfiguration(t *testing.T) {
	_, err := Load([]string{"--tls-cert", "/cert.pem"}, func(string) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("Load() error = nil, want partial TLS configuration rejected")
	}
}
