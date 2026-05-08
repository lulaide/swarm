package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metacubex/mihomo/adapter"
	C "github.com/metacubex/mihomo/constant"

	"github.com/lulaide/swarm/dispatcher"
	"github.com/lulaide/swarm/pool"
)

func makeFakeProxies(n int) []C.Proxy {
	proxies := make([]C.Proxy, n)
	for i := 0; i < n; i++ {
		mapping := map[string]any{
			"name":     fmt.Sprintf("node-%d", i),
			"type":     "ss",
			"server":   fmt.Sprintf("10.0.0.%d", i+1),
			"port":     8388,
			"cipher":   "aes-256-gcm",
			"password": "test",
		}
		proxy, err := adapter.ParseProxy(mapping)
		if err != nil {
			panic(err)
		}
		proxies[i] = proxy
	}
	return proxies
}

func TestHandleNodes(t *testing.T) {
	p := pool.New(3)
	p.Update(makeFakeProxies(3))
	d := dispatcher.New(p)
	s := New(p, d, nil)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if int(resp["total"].(float64)) != 3 {
		t.Errorf("expected total 3, got %v", resp["total"])
	}
	if int(resp["alive"].(float64)) != 3 {
		t.Errorf("expected alive 3, got %v", resp["alive"])
	}
}

func TestHandleStats(t *testing.T) {
	p := pool.New(3)
	d := dispatcher.New(p)
	s := New(p, d, nil)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var stats dispatcher.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.TotalRequests != 0 {
		t.Errorf("expected 0 total requests, got %d", stats.TotalRequests)
	}
}

func TestHandleNodesMethodNotAllowed(t *testing.T) {
	p := pool.New(3)
	d := dispatcher.New(p)
	s := New(p, d, nil)

	req := httptest.NewRequest(http.MethodPost, "/nodes", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
