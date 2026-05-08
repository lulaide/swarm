// +build ignore

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

func main() {
	proxyAddr := flag.String("proxy", "", "代理地址，为空则直连")
	rounds := flag.Int("n", 10, "请求次数")
	flag.Parse()

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	if *proxyAddr != "" {
		proxyURL, _ := url.Parse(*proxyAddr)
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	mode := "直连"
	if *proxyAddr != "" {
		mode = "Swarm 代理"
	}

	// --- 延迟测试 ---
	fmt.Printf("\n=== %s 延迟测试 (%d 次) ===\n", mode, *rounds)
	latencies := make([]time.Duration, 0, *rounds)
	for i := 0; i < *rounds; i++ {
		start := time.Now()
		resp, err := client.Get("https://httpbin.org/ip")
		if err != nil {
			fmt.Printf("  #%d  ERROR: %v\n", i+1, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		d := time.Since(start)
		latencies = append(latencies, d)
		fmt.Printf("  #%d  %dms\n", i+1, d.Milliseconds())
	}

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}
		avg := sum / time.Duration(len(latencies))
		p50 := latencies[len(latencies)/2]
		p95idx := int(float64(len(latencies)) * 0.95)
		if p95idx >= len(latencies) {
			p95idx = len(latencies) - 1
		}
		p95 := latencies[p95idx]
		fmt.Printf("\n  最小: %dms  平均: %dms  P50: %dms  P95: %dms  最大: %dms\n",
			latencies[0].Milliseconds(), avg.Milliseconds(), p50.Milliseconds(),
			p95.Milliseconds(), latencies[len(latencies)-1].Milliseconds())
	}

	// --- 下载速度测试 ---
	fmt.Printf("\n=== %s 下载速度测试 ===\n", mode)
	// 用 1MB 的测试文件
	urls := []struct {
		name string
		url  string
	}{
		{"100KB", "https://speed.cloudflare.com/__down?bytes=102400"},
		{"1MB", "https://speed.cloudflare.com/__down?bytes=1048576"},
	}

	for _, u := range urls {
		start := time.Now()
		resp, err := client.Get(u.url)
		if err != nil {
			fmt.Printf("  %s  ERROR: %v\n", u.name, err)
			continue
		}
		n, _ := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		elapsed := time.Since(start)
		speed := float64(n) / elapsed.Seconds() / 1024
		fmt.Printf("  %s  %d bytes  %.0fms  %.0f KB/s\n", u.name, n, float64(elapsed.Milliseconds()), speed)
	}
}
