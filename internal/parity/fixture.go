// Package parity provides the golden fixture format, normalization helpers, and
// the optional pinned-upstream binary comparison used by the Phase 1 gate.
package parity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Fixture is one sanitized request/response transcript. Client is what the
// local gateway receives. Upstream is what the mock upstream returns. Expect is
// the client-facing result, and ExpectUpstream asserts the parts of the request
// the gateway sends upstream.
type Fixture struct {
	Name           string           `json:"name"`
	Protocol       string           `json:"protocol"`
	Client         ClientRequest    `json:"client"`
	Upstream       UpstreamResponse `json:"upstream"`
	Expect         ExpectedResponse `json:"expect"`
	ExpectUpstream ExpectedUpstream `json:"expect_upstream"`
}

// ClientRequest is the local request sent to the gateway.
type ClientRequest struct {
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

// UpstreamResponse is the mock upstream reply. Exactly one of JSON and SSE is
// used; SSE is a complete text/event-stream body.
type UpstreamResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	JSON    json.RawMessage   `json:"json,omitempty"`
	SSE     string            `json:"sse,omitempty"`
}

// ExpectedResponse is the expected local reply.
type ExpectedResponse struct {
	Status      int               `json:"status"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers,omitempty"`
	JSON        json.RawMessage   `json:"json,omitempty"`
	SSE         string            `json:"sse,omitempty"`
}

// ExpectedUpstream is a subset assertion over the upstream request. JSON is
// matched recursively: every expected key and array element must be present and
// equal, while extra keys and array elements are allowed.
type ExpectedUpstream struct {
	Path string          `json:"path"`
	JSON json.RawMessage `json:"json,omitempty"`
}

// RepoRoot returns the repository root from this package's source location.
func RepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("parity: cannot locate source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// FixturesDir returns the committed fixture directory.
func FixturesDir() string {
	return filepath.Join(RepoRoot(), "tests", "fixtures")
}

// LoadDir reads every .json fixture under dir, sorted by path. It rejects a
// duplicate name and a fixture that names an unknown protocol or an empty client
// path.
func LoadDir(dir string) ([]Fixture, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	seen := make(map[string]string, len(paths))
	fixtures := make([]Fixture, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var fixture Fixture
		if err := json.Unmarshal(data, &fixture); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if fixture.Name == "" {
			fixture.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		if previous, exists := seen[fixture.Name]; exists {
			return nil, fmt.Errorf("%s: duplicate fixture name %q (also %s)", path, fixture.Name, previous)
		}
		seen[fixture.Name] = path
		if err := fixture.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, nil
}

func (f Fixture) validate() error {
	switch f.Protocol {
	case "chat", "responses", "anthropic", "systemone":
	default:
		return fmt.Errorf("unsupported protocol %q", f.Protocol)
	}
	if !strings.HasPrefix(f.Client.Path, "/v1/") {
		return fmt.Errorf("client.path must start with /v1/: %q", f.Client.Path)
	}
	if f.ExpectUpstream.Path == "" {
		return fmt.Errorf("expect_upstream.path is required")
	}
	if f.Upstream.Status == 0 {
		return fmt.Errorf("upstream.status is required")
	}
	if f.Expect.Status == 0 {
		return fmt.Errorf("expect.status is required")
	}
	if len(f.Upstream.JSON) == 0 && f.Upstream.SSE == "" {
		return fmt.Errorf("upstream.json or upstream.sse is required")
	}
	return nil
}

// NormalizeJSON decodes JSON into a generic value with volatile timestamps
// zeroed. Both sides of a parity comparison run through it.
func NormalizeJSON(data []byte) (any, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return NormalizeValue(value), nil
}

// NormalizeValue zeroes timestamp fields that a live response naturally
// changes. Keys are compared case-sensitively because the protocols use
// lowercase JSON names.
func NormalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "created" || key == "created_at" {
				typed[key] = float64(0)
				continue
			}
			typed[key] = NormalizeValue(item)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = NormalizeValue(item)
		}
		return typed
	default:
		return value
	}
}

// NormalizeSSE makes line endings deterministic without changing event order or
// field values.
func NormalizeSSE(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.ReplaceAll(body, "\r", "\n")
}

// MatchesSubset reports whether got contains every value in want. Object keys
// are recursive; arrays must have at least the expected length and match by
// position; scalars must be deeply equal.
func MatchesSubset(want, got any) error {
	switch expected := want.(type) {
	case map[string]any:
		actual, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object, got %T", got)
		}
		for key, item := range expected {
			actualItem, exists := actual[key]
			if !exists {
				return fmt.Errorf("missing key %q", key)
			}
			if err := MatchesSubset(item, actualItem); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
		return nil
	case []any:
		actual, ok := got.([]any)
		if !ok {
			return fmt.Errorf("expected array, got %T", got)
		}
		if len(actual) < len(expected) {
			return fmt.Errorf("expected at least %d array elements, got %d", len(expected), len(actual))
		}
		for i, item := range expected {
			if err := MatchesSubset(item, actual[i]); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		return nil
	default:
		if fmt.Sprint(expected) != fmt.Sprint(got) {
			return fmt.Errorf("expected %v, got %v", expected, got)
		}
		return nil
	}
}
