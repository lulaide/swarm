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
	// run initial check
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
			if hc.check(name) {
				hc.pool.MarkAlive(name)
			} else {
				hc.pool.MarkFailed(name)
			}
		}(node.Name)
	}
	wg.Wait()
}

func (hc *HealthChecker) check(name string) bool {
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

	ctx, cancel := context.WithTimeout(context.Background(), hc.timeout)
	defer cancel()

	addr := C.Metadata{}
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
		TLSHandshakeTimeout: hc.timeout,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   hc.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, hc.url, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[healthcheck] %s failed: %v\n", name, err)
		return false
	}
	_ = resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func (hc *HealthChecker) Close() {
	select {
	case <-hc.stopCh:
	default:
		close(hc.stopCh)
	}
}
