package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opencode2api/internal/config"
)

func TestAnonymousCursorCyclesPoolEntries(t *testing.T) {
	transports, err := transportPool(
		[]string{"http://pool.example:8080#pool", "http://static.example:8080"},
		config.PerformanceConfig{
			MaxIdleConns: 10, MaxIdleConnsPerHost: 10, IdleConnTimeoutSeconds: 30, ConnectTimeoutSeconds: 1,
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	pool := anonymousPool(true, transports, time.Minute, anonymousBackoff{})
	if !pool.HasPool() {
		t.Fatal("expected a rotating #pool transport")
	}
	cursor := pool.CursorFor("stable-session")
	seenPool := 0
	for i := 0; i < len(pool.nodes)+2; i++ {
		node := cursor.Next()
		if node == nil {
			t.Fatalf("cursor exhausted at step %d", i)
		}
		if node.proxy.pool {
			seenPool++
		}
	}
	if seenPool < 2 {
		t.Fatalf("pool transport was returned %d times; want it to keep cycling", seenPool)
	}
}

func TestKeyPoolAffinityCooldownAndRetryAfter(t *testing.T) {
	transports, err := transportPool(
		[]string{"http://a.example:1", "http://b.example:1"},
		config.PerformanceConfig{
			MaxIdleConns: 10, MaxIdleConnsPerHost: 10, IdleConnTimeoutSeconds: 30, ConnectTimeoutSeconds: 1,
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := nodePool([]string{"key-a", "key-b"}, transports, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cursor := nodes.CursorFor("conversation")
	first := cursor.Next()
	if first == nil {
		t.Fatal("no key selected")
	}
	againCursor := nodes.CursorFor("conversation")
	if again := againCursor.Next(); again != first {
		t.Fatal("the same affinity selected a different key")
	}
	nodes.MarkFailure(first, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"2"}}}, nil)
	until := time.Until(time.Unix(0, first.cooldownUntil.Load()))
	if until < time.Second {
		t.Fatalf("Retry-After was not honored: cooldown %s", until)
	}
	nextCursor := nodes.CursorFor("conversation")
	if next := nextCursor.Next(); next == first {
		t.Fatal("a cooling key was selected while another healthy key was available")
	}
}

func TestProxyHealthRestoresAndRebinds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	transports, err := transportPool([]string{"direct"}, config.PerformanceConfig{
		MaxIdleConns: 10, MaxIdleConnsPerHost: 10, IdleConnTimeoutSeconds: 30, ConnectTimeoutSeconds: 1,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	proxy := transports.items[0]
	proxy.healthy.Store(false)
	transports.CheckHealth(context.Background(), server.URL, time.Second)
	if !proxy.healthy.Load() {
		t.Fatal("a reachable proxy was not restored")
	}

	rebindTransports, err := transportPool([]string{"http://a.example:1", "http://b.example:1"}, config.PerformanceConfig{
		MaxIdleConns: 10, MaxIdleConnsPerHost: 10, IdleConnTimeoutSeconds: 30, ConnectTimeoutSeconds: 1,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := nodePool([]string{"key-a", "key-b"}, rebindTransports, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rebindTransports.items[0].healthy.Store(false)
	if moved := nodes.RebindProxy(0); moved != 1 {
		t.Fatalf("rebind moved %d keys, want 1", moved)
	}
	if got := nodes.Proxy(nodes.nodes[0]); got != rebindTransports.items[1] {
		t.Fatal("the key did not move to the healthy proxy")
	}
}

func TestMaxAnonymousExitsDefault(t *testing.T) {
	g := &Gateway{}
	if got := g.maxAnonymousExits(); got != defaultMaxAnonymousExits {
		t.Fatalf("default cap = %d, want %d", got, defaultMaxAnonymousExits)
	}
	g.cfg.Performance.MaxAnonymousExits = 3
	if got := g.maxAnonymousExits(); got != 3 {
		t.Fatalf("configured cap = %d, want 3", got)
	}
}

func TestPoolTransportIsNeverCooledOrEvicted(t *testing.T) {
	transports, err := transportPool(
		[]string{"http://pool.example:8080#pool", "http://static.example:8080"},
		config.PerformanceConfig{
			MaxIdleConns: 10, MaxIdleConnsPerHost: 10, IdleConnTimeoutSeconds: 30, ConnectTimeoutSeconds: 1,
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	pool := anonymousPool(true, transports, time.Minute, anonymousBackoff{})
	var poolNode, staticNode *anonymousNode
	for _, node := range pool.nodes {
		if node.proxy.pool {
			poolNode = node
		} else {
			staticNode = node
		}
	}
	if poolNode == nil || staticNode == nil {
		t.Fatal("expected one pool node and one static node")
	}
	pool.MarkFailure(poolNode, &http.Response{StatusCode: http.StatusTooManyRequests}, nil)
	pool.MarkFailure(staticNode, &http.Response{StatusCode: http.StatusTooManyRequests}, nil)
	if poolNode.cooldownUntil.Load() != 0 {
		t.Fatal("#pool node was cooled down")
	}
	if staticNode.cooldownUntil.Load() == 0 {
		t.Fatal("static node was not cooled down")
	}
}
