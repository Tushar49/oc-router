package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBinaries compares the local binary with the pinned upstream binary on the
// same fixtures. It is skipped in ordinary test runs and enabled by
// scripts/parity.mjs, which builds both binaries and sets PARITY_OUR_BIN and
// PARITY_UPSTREAM_BIN.
func TestBinaries(t *testing.T) {
	ourBinary := os.Getenv("PARITY_OUR_BIN")
	upstreamBinary := os.Getenv("PARITY_UPSTREAM_BIN")
	fixtureDir := os.Getenv("PARITY_FIXTURES")
	if ourBinary == "" || upstreamBinary == "" {
		t.Skip("set PARITY_OUR_BIN and PARITY_UPSTREAM_BIN to run the binary parity comparison")
	}
	if fixtureDir == "" {
		fixtureDir = FixturesDir()
	}
	fixtures, err := LoadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures found")
	}

	mock := newBinaryMock(t, fixtures)
	configPath := writeBinaryConfig(t, mock.server.URL, fixtures)
	our := startBinary(t, ourBinary, configPath)
	upstream := startBinary(t, upstreamBinary, configPath)
	defer our.stop()
	defer upstream.stop()
	our.waitForListener(t)
	upstream.waitForListener(t)

	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			ourResult, ourUpstream := our.exchange(t, fixture, mock)
			upstreamResult, upstreamUpstream := upstream.exchange(t, fixture, mock)

			if ourResult.status != upstreamResult.status {
				t.Fatalf("status: local=%d upstream=%d", ourResult.status, upstreamResult.status)
			}
			if ourResult.contentType != upstreamResult.contentType {
				t.Fatalf("content-type: local=%q upstream=%q", ourResult.contentType, upstreamResult.contentType)
			}
			if fixture.Expect.SSE != "" {
				got, want := NormalizeSSE(string(ourResult.body)), NormalizeSSE(string(upstreamResult.body))
				if got != want {
					t.Fatalf("SSE differs:\nlocal:\n%s\nupstream:\n%s", got, want)
				}
			} else {
				got, err := NormalizeJSON(ourResult.body)
				if err != nil {
					t.Fatalf("local response JSON: %v", err)
				}
				want, err := NormalizeJSON(upstreamResult.body)
				if err != nil {
					t.Fatalf("upstream response JSON: %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("response JSON differs:\nlocal: %s\nupstream: %s", ourResult.body, upstreamResult.body)
				}
			}

			if ourUpstream.Method != upstreamUpstream.Method || ourUpstream.Path != upstreamUpstream.Path {
				t.Fatalf("upstream request line differs: local=%s %s upstream=%s %s", ourUpstream.Method, ourUpstream.Path, upstreamUpstream.Method, upstreamUpstream.Path)
			}
			for _, header := range identityHeaders {
				if got, want := ourUpstream.Header.Get(header), upstreamUpstream.Header.Get(header); got != want {
					t.Fatalf("upstream header %s: local=%q upstream=%q", header, got, want)
				}
			}
			gotBody, err := NormalizeJSON(ourUpstream.Body)
			if err != nil {
				t.Fatalf("local upstream JSON: %v", err)
			}
			wantBody, err := NormalizeJSON(upstreamUpstream.Body)
			if err != nil {
				t.Fatalf("upstream upstream JSON: %v", err)
			}
			if !reflect.DeepEqual(gotBody, wantBody) {
				t.Fatalf("upstream body differs:\nlocal: %s\nupstream: %s", ourUpstream.Body, upstreamUpstream.Body)
			}
		})
	}
}

var identityHeaders = []string{
	"Accept", "Authorization", "Content-Type", "User-Agent",
	"x-api-key", "anthropic-beta", "anthropic-version",
	"x-opencode-client", "x-opencode-session", "x-opencode-session-id",
	"x-opencode-request", "x-opencode-project",
	"x-opencode-parent-session-id", "x-parent-session-id",
	"x-session-affinity", "X-Session-Id",
}

type recordedUpstreamRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type binaryMock struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedUpstreamRequest
}

func newBinaryMock(t *testing.T, fixtures []Fixture) *binaryMock {
	t.Helper()
	mock := &binaryMock{}
	models := make([]string, 0, len(fixtures))
	seen := map[string]bool{}
	replies := make(map[string]UpstreamResponse, len(fixtures))
	for _, fixture := range fixtures {
		var payload map[string]any
		if err := json.Unmarshal(fixture.Client.Body, &payload); err != nil {
			t.Fatalf("%s: client body: %v", fixture.Name, err)
		}
		model, _ := payload["model"].(string)
		if model == "" {
			t.Fatalf("%s: client body has no model", fixture.Name)
		}
		if !seen[model] {
			seen[model] = true
			models = append(models, model)
		}
		replies[model] = fixture.Upstream
	}
	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			items := make([]map[string]any, 0, len(models))
			for _, model := range models {
				items = append(items, map[string]any{"id": model})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": items})
			return
		}
		mock.mu.Lock()
		mock.requests = append(mock.requests, recordedUpstreamRequest{
			Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone(), Body: body,
		})
		mock.mu.Unlock()
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		model, _ := payload["model"].(string)
		reply, ok := replies[model]
		if !ok {
			http.Error(w, `{"error":{"message":"unknown fixture model"}}`, http.StatusBadRequest)
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

func (m *binaryMock) mark() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func (m *binaryMock) postSince(mark int) (recordedUpstreamRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, request := range m.requests[mark:] {
		if request.Method == http.MethodPost {
			return request, true
		}
	}
	return recordedUpstreamRequest{}, false
}

func writeBinaryConfig(t *testing.T, upstream string, fixtures []Fixture) string {
	t.Helper()
	protocols := make(map[string]string)
	for _, fixture := range fixtures {
		var payload map[string]any
		if err := json.Unmarshal(fixture.Client.Body, &payload); err != nil {
			t.Fatal(err)
		}
		model, _ := payload["model"].(string)
		if model != "" {
			protocols[model] = fixture.Protocol
		}
	}
	payload := map[string]any{
		"listen":      "127.0.0.1:0",
		"server_keys": []string{"test-key"},
		"zen_keys":    []string{"test-upstream-key"},
		"anonymous":   true,
		"upstream":    map[string]string{"zen": upstream, "go": upstream, "anonymous": upstream},
		"retry":       map[string]int{"max_attempts": 1, "timeout_seconds": 10},
		"models":      map[string]any{"refresh_seconds": 300, "protocols": protocols},
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
	return path
}

type binaryProcess struct {
	cmd  *exec.Cmd
	port int
	out  *bytes.Buffer
}

func startBinary(t *testing.T, binary string, configPath string) *binaryProcess {
	t.Helper()
	port := freePort(t)
	// Give each binary its own copy with the chosen port.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config["listen"] = fmt.Sprintf("127.0.0.1:%d", port)
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-config", path)
	cmd.Env = append(os.Environ(), "HTTP_PROXY=", "HTTPS_PROXY=", "ALL_PROXY=", "NO_PROXY=127.0.0.1,localhost")
	out := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", binary, err)
	}
	process := &binaryProcess{cmd: cmd, port: port, out: out}
	t.Cleanup(func() { process.stop() })
	return process
}

func (p *binaryProcess) stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Kill()
	_, _ = p.cmd.Process.Wait()
	p.cmd = nil
}

func (p *binaryProcess) waitForListener(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", p.port))
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("binary did not listen on %d: %s", p.port, stripProcessOutput(p.out.String()))
}

type exchangeResult struct {
	status      int
	contentType string
	body        []byte
}

type upstreamResult = recordedUpstreamRequest

func (p *binaryProcess) exchange(t *testing.T, fixture Fixture, mock *binaryMock) (exchangeResult, upstreamResult) {
	t.Helper()
	mark := mock.mark()
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", p.port, fixture.Client.Path), bytes.NewReader(fixture.Client.Body))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range fixture.Client.Headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request %s: %v; binary output: %s", fixture.Name, err, stripProcessOutput(p.out.String()))
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	upstream, ok := mock.postSince(mark)
	if !ok {
		t.Fatalf("no upstream POST recorded for %s; binary output: %s", fixture.Name, stripProcessOutput(p.out.String()))
	}
	return exchangeResult{status: resp.StatusCode, contentType: resp.Header.Get("Content-Type"), body: body}, upstream
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// stripProcessOutput keeps failure messages readable when a binary logged a lot.
func stripProcessOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4000 {
		return value[len(value)-4000:]
	}
	return value
}
