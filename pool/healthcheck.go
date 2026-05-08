package pool

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

type HealthChecker struct {
	pool     *Pool
	url      string
	interval time.Duration
	timeout  time.Duration

	stopCh chan struct{}
}

func NewHealthChecker(pool *Pool, url string, interval, timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		pool:     pool,
		url:      url,
		interval: interval,
		timeout:  timeout,
		stopCh:   make(chan struct{}),
	}
}

func (hc *HealthChecker) Start() {
	go hc.loop()
}

func (hc *HealthChecker) loop() {
	hc.CheckAll()

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()
	for {
		select {
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.CheckAll()
		}
	}
}

func (hc *HealthChecker) CheckAll() {
	nodes := hc.pool.Nodes()
	var wg sync.WaitGroup
	for _, node := range nodes {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if hc.checkByName(name) {
				hc.pool.MarkAlive(name)
			} else {
				hc.pool.MarkFailed(name)
			}
		}(node.Name)
	}
	wg.Wait()
}

func (hc *HealthChecker) checkByName(name string) bool {
	hc.pool.mu.RLock()
	var proxy C.Proxy
	for _, ns := range hc.pool.nodes {
		if ns.proxy.Name() == name {
			proxy = ns.proxy
			break
		}
	}
	hc.pool.mu.RUnlock()

	if proxy == nil {
		return false
	}
	return CheckProxy(proxy, hc.url, hc.timeout)
}

// CheckProxy tests if a single proxy can reach the given URL.
func CheckProxy(proxy C.Proxy, url string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	addr := C.Metadata{NetWork: C.TCP}
	if err := addr.SetRemoteAddress(net.JoinHostPort("www.gstatic.com", "443")); err != nil {
		return false
	}

	conn, err := proxy.DialContext(ctx, &addr)
	if err != nil {
		return false
	}
	defer conn.Close()

	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return conn, nil
		},
		TLSHandshakeTimeout: timeout,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// FilterAlive tests all proxies concurrently and returns only the ones that pass.
func FilterAlive(proxies []C.Proxy, url string, timeout time.Duration) []C.Proxy {
	type result struct {
		proxy C.Proxy
		alive bool
	}

	results := make([]result, len(proxies))
	var wg sync.WaitGroup

	for i, p := range proxies {
		wg.Add(1)
		go func(idx int, proxy C.Proxy) {
			defer wg.Done()
			alive := CheckProxy(proxy, url, timeout)
			results[idx] = result{proxy: proxy, alive: alive}
			if alive {
				fmt.Printf("[check] %-40s OK\n", proxy.Name())
			} else {
				fmt.Printf("[check] %-40s FAIL\n", proxy.Name())
			}
		}(i, p)
	}
	wg.Wait()

	alive := make([]C.Proxy, 0, len(proxies))
	for _, r := range results {
		if r.alive {
			alive = append(alive, r.proxy)
		}
	}
	return alive
}

func (hc *HealthChecker) Close() {
	select {
	case <-hc.stopCh:
	default:
		close(hc.stopCh)
	}
}
