package pool

import (
	"fmt"
	"testing"

	"github.com/metacubex/mihomo/adapter"
	C "github.com/metacubex/mihomo/constant"
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

func TestPoolRandom(t *testing.T) {
	p := New(3)
	proxies := makeFakeProxies(5)
	p.Update(proxies)

	if p.Len() != 5 {
		t.Fatalf("expected 5 nodes, got %d", p.Len())
	}
	if p.AliveCount() != 5 {
		t.Fatalf("expected 5 alive, got %d", p.AliveCount())
	}

	// should be able to get a random proxy
	proxy, err := p.Random()
	if err != nil {
		t.Fatal(err)
	}
	if proxy == nil {
		t.Fatal("expected non-nil proxy")
	}
}

func TestPoolMarkFailed(t *testing.T) {
	p := New(2) // 2 failures = dead
	proxies := makeFakeProxies(3)
	p.Update(proxies)

	// fail node-0 twice
	p.MarkFailed("node-0")
	if p.AliveCount() != 3 {
		t.Fatalf("expected 3 alive after 1 failure, got %d", p.AliveCount())
	}

	p.MarkFailed("node-0")
	if p.AliveCount() != 2 {
		t.Fatalf("expected 2 alive after 2 failures, got %d", p.AliveCount())
	}

	// recover
	p.MarkAlive("node-0")
	if p.AliveCount() != 3 {
		t.Fatalf("expected 3 alive after recovery, got %d", p.AliveCount())
	}
}

func TestPoolNoAlive(t *testing.T) {
	p := New(1)
	proxies := makeFakeProxies(1)
	p.Update(proxies)

	p.MarkFailed("node-0")

	_, err := p.Random()
	if err != ErrNoAliveProxy {
		t.Fatalf("expected ErrNoAliveProxy, got %v", err)
	}
}

func TestPoolResetAll(t *testing.T) {
	p := New(1)
	proxies := makeFakeProxies(3)
	p.Update(proxies)

	p.MarkFailed("node-0")
	p.MarkFailed("node-1")

	if p.AliveCount() != 1 {
		t.Fatalf("expected 1 alive, got %d", p.AliveCount())
	}

	p.ResetAll()
	if p.AliveCount() != 3 {
		t.Fatalf("expected 3 alive after reset, got %d", p.AliveCount())
	}
}

func TestPoolNodes(t *testing.T) {
	p := New(3)
	proxies := makeFakeProxies(2)
	p.Update(proxies)

	nodes := p.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Name != "node-0" {
		t.Errorf("expected name node-0, got %s", nodes[0].Name)
	}
	if !nodes[0].Alive {
		t.Error("expected node-0 to be alive")
	}
}

func TestPoolEmpty(t *testing.T) {
	p := New(3)
	_, err := p.Random()
	if err != ErrNoAliveProxy {
		t.Fatalf("expected ErrNoAliveProxy on empty pool, got %v", err)
	}
}

func TestPoolP2C(t *testing.T) {
	p := New(3)
	proxies := makeFakeProxies(5)
	p.Update(proxies)

	// set different latencies
	p.UpdateLatency("node-0", 100000)  // 100ms
	p.UpdateLatency("node-1", 500000)  // 500ms
	p.UpdateLatency("node-2", 50000)   // 50ms

	// P2C should work
	proxy, err := p.P2C()
	if err != nil {
		t.Fatal(err)
	}
	if proxy == nil {
		t.Fatal("expected non-nil proxy")
	}
}

func TestPoolP2CEmpty(t *testing.T) {
	p := New(3)
	_, err := p.P2C()
	if err != ErrNoAliveProxy {
		t.Fatalf("expected ErrNoAliveProxy, got %v", err)
	}
}

func TestPoolUpdateLatency(t *testing.T) {
	p := New(3)
	proxies := makeFakeProxies(2)
	p.Update(proxies)

	p.UpdateLatency("node-0", 100000)
	nodes := p.Nodes()
	if nodes[0].LatencyUs != 100000 {
		t.Errorf("expected 100000, got %d", nodes[0].LatencyUs)
	}

	// EWMA update
	p.UpdateLatency("node-0", 200000)
	nodes = p.Nodes()
	expected := int64(100000*7/10 + 200000*3/10) // 130000
	if nodes[0].LatencyUs != expected {
		t.Errorf("expected %d, got %d", expected, nodes[0].LatencyUs)
	}
}

func BenchmarkPoolRandom(b *testing.B) {
	p := New(3)
	p.Update(makeFakeProxies(100))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Random()
	}
}

func BenchmarkPoolP2C(b *testing.B) {
	p := New(3)
	proxies := makeFakeProxies(100)
	p.Update(proxies)
	for i, proxy := range proxies {
		p.UpdateLatency(proxy.Name(), int64(i*10000))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.P2C()
	}
}
