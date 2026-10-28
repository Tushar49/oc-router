package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"opencode2api/internal/config"
	"opencode2api/internal/parity"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func ipReply(body string) parity.UpstreamResponse {
	return parity.UpstreamResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		JSON:    json.RawMessage(body),
	}
}

func loadFixtureByName(t *testing.T, name string) (parity.Fixture, error) {
	t.Helper()
	fixtures, err := parity.LoadDir(parity.FixturesDir())
	if err != nil {
		return parity.Fixture{}, err
	}
	for _, fixture := range fixtures {
		if fixture.Name == name {
			return fixture, nil
		}
	}
	t.Fatalf("fixture %q not found", name)
	return parity.Fixture{}, nil
}

// fixtureProtocols declares a protocol for every committed fixture model. The
// catalog is empty before the first refresh, so an override also keeps routing
// working after a test populates the catalog.
func fixtureProtocols() map[string]string {
	return map[string]string{
		"test/paid-chat":          "chat",
		"test/paid-responses":     "responses",
		"test/paid-anthropic":     "anthropic",
		"test/paid-anthropic-sse": "anthropic",
		"test/paid-responses-sse": "responses",
		"test/paid-error":         "chat",
		"test/paid-tools":         "chat",
		"test/free-chat":          "chat",
		"test/free-collapse":      "chat",
		"test/cross-anthropic":    "anthropic",
		"test/cross-chat":         "chat",
		"test/systemone":          "systemone",
	}
}

func fixtureConfig(t *testing.T, upstream string) config.Config {
	t.Helper()
	payload := map[string]any{
		"listen":      "127.0.0.1:0",
		"server_keys": []string{"test-key"},
		"zen_keys":    []string{"test-upstream-key"},
		"anonymous":   true,
		"upstream":    map[string]string{"zen": upstream, "go": upstream, "anonymous": upstream},
		"retry":       map[string]int{"max_attempts": 1, "timeout_seconds": 5},
		"models":      map[string]any{"refresh_seconds": 300, "protocols": fixtureProtocols()},
		"logging":     map[string]any{"level": "error", "ring_size": 100},
		"webui":       map[string]any{"enabled": false, "listen": "127.0.0.1:0", "session_ttl_minutes": 60},
		"prefer":      "zen",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

type recordedUpstreamRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type mockUpstream struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedUpstreamRequest
}

func newMockUpstream(t *testing.T, reply parity.UpstreamResponse) *mockUpstream {
	t.Helper()
	mock := &mockUpstream{}
	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mock.mu.Lock()
		mock.requests = append(mock.requests, recordedUpstreamRequest{
			Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone(), Body: body,
		})
		mock.mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"test/free-chat"}]}`))
			return
		}
		for key, value := range reply.Headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(reply.Status)
		if reply.SSE != "" {
			_, _ = w.Write([]byte(reply.SSE))
			return
		}
		_, _ = w.Write(reply.JSON)
	}))
	t.Cleanup(mock.server.Close)
	return mock
}

func (m *mockUpstream) last(t *testing.T) recordedUpstreamRequest {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		t.Fatal("mock upstream received no request")
	}
	return m.requests[len(m.requests)-1]
}

func newFixtureGateway(t *testing.T, upstream string, model string) *Gateway {
	t.Helper()
	cfg := fixtureConfig(t, upstream)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := New(cfg, logger, nil, nil)
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}
	g.catalog.Replace([]string{model}, []string{model})
	return g
}

func serveFixture(t *testing.T, fixture parity.Fixture) (*httptest.ResponseRecorder, *Gateway, *mockUpstream) {
	t.Helper()
	mock := newMockUpstream(t, fixture.Upstream)
	var payload map[string]any
	if err := json.Unmarshal(fixture.Client.Body, &payload); err != nil {
		t.Fatalf("fixture client body: %v", err)
	}
	model, _ := payload["model"].(string)
	if model == "" {
		t.Fatal("fixture client body has no model")
	}
	g := newFixtureGateway(t, mock.server.URL, model)
	req := httptest.NewRequest(http.MethodPost, fixture.Client.Path, bytes.NewReader(fixture.Client.Body))
	for key, value := range fixture.Client.Headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec, g, mock
}

func TestFixtureGolden(t *testing.T) {
	fixtures, err := parity.LoadDir(parity.FixturesDir())
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures found")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			rec, _, mock := serveFixture(t, fixture)
			if rec.Code != fixture.Expect.Status {
				t.Fatalf("status: got %d, want %d; body=%s", rec.Code, fixture.Expect.Status, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != fixture.Expect.ContentType {
				t.Fatalf("content-type: got %q, want %q", got, fixture.Expect.ContentType)
			}
			for key, want := range fixture.Expect.Headers {
				if got := rec.Header().Get(key); got != want {
					t.Errorf("header %s: got %q, want %q", key, got, want)
				}
			}
			if fixture.Expect.SSE != "" {
				if got, want := parity.NormalizeSSE(rec.Body.String()), parity.NormalizeSSE(fixture.Expect.SSE); got != want {
					t.Fatalf("SSE mismatch:\ngot:\n%s\nwant:\n%s", got, want)
				}
			} else {
				got, err := parity.NormalizeJSON(rec.Body.Bytes())
				if err != nil {
					t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
				}
				want, err := parity.NormalizeJSON(fixture.Expect.JSON)
				if err != nil {
					t.Fatalf("decode expected response: %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					gotJSON, _ := json.MarshalIndent(got, "", "  ")
					wantJSON, _ := json.MarshalIndent(want, "", "  ")
					t.Fatalf("response mismatch:\ngot: %s\nwant: %s", gotJSON, wantJSON)
				}
			}
			upstream := mock.last(t)
			if upstream.Method != http.MethodPost {
				t.Errorf("upstream method: got %s, want POST", upstream.Method)
			}
			if upstream.Path != fixture.ExpectUpstream.Path {
				t.Errorf("upstream path: got %q, want %q", upstream.Path, fixture.ExpectUpstream.Path)
			}
			if len(fixture.ExpectUpstream.JSON) > 0 {
				got, err := parity.NormalizeJSON(upstream.Body)
				if err != nil {
					t.Fatalf("decode upstream body: %v; body=%s", err, upstream.Body)
				}
				want, err := parity.NormalizeJSON(fixture.ExpectUpstream.JSON)
				if err != nil {
					t.Fatalf("decode expected upstream body: %v", err)
				}
				if err := parity.MatchesSubset(want, got); err != nil {
					t.Fatalf("upstream body mismatch: %v", err)
				}
			}
		})
	}
}
