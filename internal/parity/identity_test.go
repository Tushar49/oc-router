package parity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type identityManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Algorithm     string `json:"algorithm"`
	Generated     string `json:"generated"`
	Upstream      struct {
		Repo   string `json:"repo"`
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
		Date   string `json:"date"`
	} `json:"upstream"`
	Files []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

func TestIdentityManifestMatchesTree(t *testing.T) {
	root := RepoRoot()
	data, err := os.ReadFile(filepath.Join(root, "docs", "identity-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest identityManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", manifest.SchemaVersion)
	}
	if manifest.Algorithm != "sha256" {
		t.Fatalf("algorithm = %q, want sha256", manifest.Algorithm)
	}
	if len(manifest.Files) == 0 {
		t.Fatal("identity manifest has no files")
	}
	seen := make(map[string]bool, len(manifest.Files))
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		clean := filepath.ToSlash(file.Path)
		if seen[clean] {
			t.Fatalf("duplicate identity path %q", clean)
		}
		seen[clean] = true
		paths = append(paths, clean)
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(clean)))
		if err != nil {
			t.Fatalf("%s: %v", clean, err)
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != file.SHA256 {
			t.Fatalf("%s: hash %s, want %s", clean, got, file.SHA256)
		}
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("identity paths are not sorted: %v", paths)
	}

	base, err := os.ReadFile(filepath.Join(root, "UPSTREAM_BASE"))
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(base), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[key] = value
		}
	}
	if got, want := manifest.Upstream.Commit, values["commit"]; got != want {
		t.Fatalf("manifest commit = %q, UPSTREAM_BASE = %q", got, want)
	}
	if got, want := manifest.Upstream.Tag, values["tag"]; got != want {
		t.Fatalf("manifest tag = %q, UPSTREAM_BASE = %q", got, want)
	}
}
