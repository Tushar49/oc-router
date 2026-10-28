package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sendJSON(t *testing.T, g *Gateway, path string, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAuthRejectsMissingKey(t *testing.T) {
	g := newFixtureGateway(t, "http://127.0.0.1:1", "test/paid-chat")
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			rec := sendJSON(t, g, path, `{"model":"test/paid-chat"}`, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want 401; body=%s", rec.Code, rec.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode body: %v; body=%s", err, rec.Body.String())
			}
			if path == "/v1/messages" {
				if payload["type"] != "error" {
					t.Fatalf("anthropic envelope: %#v", payload)
				}
				inner, _ := payload["error"].(map[string]any)
				if inner["type"] != "authentication_error" {
					t.Fatalf("anthropic error type: %#v", payload)
				}
				return
			}
			inner, _ := payload["error"].(map[string]any)
			if inner["type"] != "authentication_error" {
				t.Fatalf("openai error type: %#v", payload)
			}
		})
	}
}

func TestAuthAcceptsBearerAndAPIKey(t *testing.T) {
	g := newFixtureGateway(t, "http://127.0.0.1:1", "test/paid-chat")
	for name, headers := range map[string]map[string]string{
		"bearer":    {"Authorization": "Bearer test-key"},
		"x-api-key": {"x-api-key": "test-key"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := sendJSON(t, g, "/v1/chat/completions", `{}`, headers)
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("valid credential was rejected: %s", rec.Body.String())
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400 for missing model; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAuthRejectsWrongKey(t *testing.T) {
	g := newFixtureGateway(t, "http://127.0.0.1:1", "test/paid-chat")
	rec := sendJSON(t, g, "/v1/chat/completions", `{"model":"test/paid-chat"}`, map[string]string{"Authorization": "Bearer wrong-key"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRoutingRequiresModel(t *testing.T) {
	g := newFixtureGateway(t, "http://127.0.0.1:1", "test/paid-chat")
	rec := sendJSON(t, g, "/v1/chat/completions", `{}`, map[string]string{"Authorization": "Bearer test-key"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	inner, _ := payload["error"].(map[string]any)
	if inner["message"] != "model is required" || inner["type"] != "invalid_request_error" {
		t.Fatalf("error envelope: %#v", payload)
	}
}

func TestRoutingRejectsUnknownModel(t *testing.T) {
	g := newFixtureGateway(t, "http://127.0.0.1:1", "test/paid-chat")
	rec := sendJSON(t, g, "/v1/chat/completions", `{"model":"test/not-listed","messages":[{"role":"user","content":"hi"}]}`, map[string]string{"Authorization": "Bearer test-key"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRoutingPaidUsesKeyedLane(t *testing.T) {
	mock := newMockUpstream(t, ipReply(`{"id":"chatcmpl_lane","object":"chat.completion","created":1700000000,"model":"test/paid-chat","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	g := newFixtureGateway(t, mock.server.URL, "test/paid-chat")
	rec := sendJSON(t, g, "/v1/chat/completions", `{"model":"test/paid-chat","messages":[{"role":"user","content":"hi"}]}`, map[string]string{"Authorization": "Bearer test-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", rec.Code, rec.Body.String())
	}
	if got := mock.last(t).Header.Get("Authorization"); got != "Bearer test-upstream-key" {
		t.Fatalf("upstream authorization: got %q, want keyed credential", got)
	}
}

func TestRoutingFreeUsesAnonymousLane(t *testing.T) {
	fixtures, err := loadFixtureByName(t, "free-chat-sse")
	if err != nil {
		t.Fatal(err)
	}
	rec, _, mock := serveFixture(t, fixtures)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", rec.Code, rec.Body.String())
	}
	if got := mock.last(t).Header.Get("Authorization"); got != "Bearer public" {
		t.Fatalf("upstream authorization: got %q, want the public credential", got)
	}
}

func TestModelsListShape(t *testing.T) {
	g := newFixtureGateway(t, "http://127.0.0.1:1", "test/paid-chat")
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Object != "list" || len(payload.Data) != 1 {
		t.Fatalf("payload: %#v", payload)
	}
	entry := payload.Data[0]
	if entry["id"] != "test/paid-chat" || entry["object"] != "model" || entry["owned_by"] != "opencode" {
		t.Fatalf("entry: %#v", entry)
	}
	if entry["native_protocol"] != "chat" {
		t.Fatalf("native_protocol: %#v", entry["native_protocol"])
	}
	endpoints, _ := entry["supported_endpoints"].([]any)
	for _, want := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		if !containsString(endpoints, want) {
			t.Fatalf("supported_endpoints missing %q: %#v", want, endpoints)
		}
	}
}

func TestHealthzStartingAndReady(t *testing.T) {
	t.Run("starting", func(t *testing.T) {
		cfg := fixtureConfig(t, "http://127.0.0.1:1")
		g, err := New(cfg, discardLogger(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status: got %d, want 503; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["status"] != "starting" || payload["ready"] != false {
			t.Fatalf("payload: %#v", payload)
		}
		issues, _ := payload["issues"].([]any)
		if !containsString(issues, "model_catalog_pending") {
			t.Fatalf("issues: %#v", issues)
		}
	})

	t.Run("ready", func(t *testing.T) {
		g := newFixtureGateway(t, "http://127.0.0.1:1", "test/paid-chat")
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["status"] != "ok" || payload["ready"] != true {
			t.Fatalf("payload: %#v", payload)
		}
		models, _ := payload["models"].(map[string]any)
		if models["total"] != float64(1) || models["exposed"] != float64(1) {
			t.Fatalf("models: %#v", models)
		}
		keys, _ := payload["keys"].(map[string]any)
		if keys["total"] != float64(1) || keys["zen"] != float64(1) {
			t.Fatalf("keys: %#v", keys)
		}
		proxies, _ := payload["proxies"].(map[string]any)
		if proxies["total"] != float64(1) || proxies["healthy"] != float64(1) {
			t.Fatalf("proxies: %#v", proxies)
		}
		if payload["version"] != "dev" {
			t.Fatalf("version: %#v", payload["version"])
		}
	})
}

func containsString(items []any, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
