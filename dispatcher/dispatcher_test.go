package dispatcher

import (
	"fmt"
	"net"
	"testing"

	"github.com/metacubex/mihomo/adapter"
	C "github.com/metacubex/mihomo/constant"

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

func TestDispatcherImplementsTunnel(t *testing.T) {
	p := pool.New(3)
	d := New(p)

	// verify it implements C.Tunnel interface
	var _ C.Tunnel = d
	_ = d
}

func TestDispatcherStats(t *testing.T) {
	p := pool.New(3)
	d := New(p)

	stats := d.Stats()
	if stats.TotalRequests != 0 {
		t.Errorf("expected 0 total requests, got %d", stats.TotalRequests)
	}
	if stats.ActiveConns != 0 {
		t.Errorf("expected 0 active conns, got %d", stats.ActiveConns)
	}
}

func TestDispatcherNatTable(t *testing.T) {
	p := pool.New(3)
	d := New(p)

	if d.NatTable() == nil {
		t.Error("expected non-nil NatTable")
	}
}

func TestDispatcherHandleTCPNoProxy(t *testing.T) {
	p := pool.New(3)
	// empty pool, no proxies
	d := New(p)

	// create a pair of connected sockets
	client, server := createPipe()
	defer client.Close()
	defer server.Close()

	metadata := &C.Metadata{}
	_ = metadata.SetRemoteAddress("1.2.3.4:80")

	// should not panic, just close
	done := make(chan struct{})
	go func() {
		d.HandleTCPConn(server, metadata)
		close(done)
	}()
	<-done

	stats := d.Stats()
	if stats.TotalRequests != 1 {
		t.Errorf("expected 1 total request, got %d", stats.TotalRequests)
	}
}

func createPipe() (net.Conn, net.Conn) {
	return net.Pipe()
}
