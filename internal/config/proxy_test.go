package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseProxyEntry(t *testing.T) {
	cases := []struct {
		raw   string
		clean string
		pool  bool
	}{
		{"direct", "direct", false},
		{"http://proxy.example:8080", "http://proxy.example:8080", false},
		{"http://proxy.example:8080#pool", "http://proxy.example:8080", true},
		{"http://proxy.example:8080/#pool", "http://proxy.example:8080/", true},
		{"pool+http://proxy.example:8080", "http://proxy.example:8080", true},
		{"pool+socks5://proxy.example:1080", "socks5://proxy.example:1080", true},
		{"http://proxy.example:8080#fragment", "http://proxy.example:8080#fragment", false},
		{"  ", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			clean, pool := ParseProxyEntry(tc.raw)
			if clean != tc.clean || pool != tc.pool {
				t.Fatalf("ParseProxyEntry(%q) = (%q, %v), want (%q, %v)", tc.raw, clean, pool, tc.clean, tc.pool)
			}
		})
	}
}

func TestStripProxyLineComment(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"http://proxy.example:8080#pool", "http://proxy.example:8080#pool"},
		{"http://proxy.example:8080#pool # comment", "http://proxy.example:8080#pool"},
		{"socks5://proxy.example:1080 ; comment", "socks5://proxy.example:1080"},
		{"http://proxy.example:8080 // comment", "http://proxy.example:8080"},
		{"# whole line", ""},
	}
	for _, tc := range cases {
		if got := strings.TrimSpace(stripProxyLineComment(tc.line)); got != tc.want {
			t.Fatalf("stripProxyLineComment(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestReadProxyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxies.txt")
	content := "\ufeffhttp://proxy.example:8080#pool\r\n\r\n# comment\r\nsocks5://proxy.example:1080 ; trailing\r\ndirect\r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readProxyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://proxy.example:8080#pool", "socks5://proxy.example:1080", "direct"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readProxyFile = %#v, want %#v", got, want)
	}
}

func TestUniqueProxyEntries(t *testing.T) {
	items := []string{
		"http://proxy.example:8080",
		"http://proxy.example:8080#pool",
		"pool+http://proxy.example:8080",
		"http://proxy.example:8080",
		"  ",
		"http://other.example:8080#pool",
	}
	got := uniqueProxyEntries(items)
	want := []string{"http://proxy.example:8080", "http://proxy.example:8080#pool", "http://other.example:8080#pool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueProxyEntries = %#v, want %#v", got, want)
	}
}

func TestResolveProxyFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	proxyPath := filepath.Join(dir, "proxies.txt")
	if err := os.WriteFile(proxyPath, []byte("http://file.example:8080#pool\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Proxies:   []string{"http://static.example:8080", "http://file.example:8080"},
		ProxyFile: "proxies.txt",
	}
	if err := resolveProxyFiles(configPath, &cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{"http://static.example:8080", "http://file.example:8080", "http://file.example:8080#pool"}
	if !reflect.DeepEqual(cfg.RuntimeProxies(), want) {
		t.Fatalf("runtime proxies = %#v, want %#v", cfg.RuntimeProxies(), want)
	}
}
