package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppliesDefaultsAndComments(t *testing.T) {
	path := writeConfig(t, `{
  // Minimal configuration; everything else comes from the defaults.
  "server_keys": ["test-key"],
  "anonymous": true, /* keyless lane */
  "zen_keys": ["sk-your-zen-key"]
}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Fatalf("listen: got %q", cfg.Listen)
	}
	if cfg.Upstream.Zen != "https://opencode.ai/zen" || cfg.Upstream.Go != "https://opencode.ai/zen/go" || cfg.Upstream.Anonymous != "https://opencode.ai/inference" {
		t.Fatalf("upstream defaults: %#v", cfg.Upstream)
	}
	if cfg.Retry.MaxAttempts != 3 || cfg.Retry.TimeoutSeconds != 300 {
		t.Fatalf("retry defaults: %#v", cfg.Retry)
	}
	if cfg.Models.RefreshSeconds != 300 || cfg.Performance.MaxIdleConns != 2048 || cfg.Prefer != TierGo {
		t.Fatalf("defaults: %#v", cfg)
	}
	if got := cfg.RuntimeProxies(); len(got) != 1 || got[0] != "direct" {
		t.Fatalf("runtime proxies: %#v", got)
	}
	if len(cfg.ZenKeys) != 0 {
		t.Fatalf("placeholder key was kept: %#v", cfg.ZenKeys)
	}
}

func TestLoadRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	cases := map[string]string{
		"unknown field": `{"server_keys":["test-key"],"anonymous":true,"unknown":1}`,
		"trailing":      `{"server_keys":["test-key"],"anonymous":true} {"again":true}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body)); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

func TestLoadValidation(t *testing.T) {
	cases := map[string]string{
		"no server keys":        `{"anonymous":true}`,
		"no upstream key":       `{"server_keys":["test-key"]}`,
		"bad proxy scheme":      `{"server_keys":["test-key"],"anonymous":true,"proxies":["ftp://proxy.example:21"]}`,
		"bad prefer":            `{"server_keys":["test-key"],"anonymous":true,"prefer":"other"}`,
		"bad retry":             `{"server_keys":["test-key"],"anonymous":true,"retry":{"max_attempts":0}}`,
		"bad logging level":     `{"server_keys":["test-key"],"anonymous":true,"logging":{"level":"trace"}}`,
		"bad upstream scheme":   `{"server_keys":["test-key"],"anonymous":true,"upstream":{"zen":"ftp://example"}}`,
		"bad protocol override": `{"server_keys":["test-key"],"anonymous":true,"models":{"protocols":{"m":"grpc"}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, body))
			if err == nil {
				t.Fatal("expected a configuration error")
			}
			if strings.Contains(err.Error(), "panic") {
				t.Fatalf("unexpected panic text: %v", err)
			}
		})
	}
}
