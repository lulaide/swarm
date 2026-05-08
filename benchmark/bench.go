package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	target := flag.String("url", "https://httpbin.org/ip", "目标 URL")
	proxyAddr := flag.String("proxy", "", "代理地址，为空则直连")
	concurrency := flag.Int("c", 10, "并发数")
	duration := flag.Int("d", 15, "持续时间（秒）")
	flag.Parse()

	var total, success, rateLimit, otherErr atomic.Int64
	_ = sync.Map{} // reserved for future IP tracking

	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConnsPerHost: *concurrency,
		MaxIdleConns:        *concurrency * 2,
	}

	if *proxyAddr != "" {
		proxyURL, err := url.Parse(*proxyAddr)
		if err != nil {
			fmt.Printf("代理地址错误: %v\n", err)
			return
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	mode := "直连"
	if *proxyAddr != "" {
		mode = fmt.Sprintf("代理 (%s)", *proxyAddr)
	}
	fmt.Printf("=== 压测开始 ===\n")
	fmt.Printf("目标: %s\n", *target)
	fmt.Printf("模式: %s\n", mode)
	fmt.Printf("并发: %d\n", *concurrency)
	fmt.Printf("时长: %ds\n\n", *duration)

	deadline := time.After(time.Duration(*duration) * time.Second)
	stop := make(chan struct{})

	go func() {
		<-deadline
		close(stop)
	}()

	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				total.Add(1)
				req, _ := http.NewRequest("GET", *target, nil)
				resp, err := client.Do(req)
				if err != nil {
					otherErr.Add(1)
					continue
				}

				switch {
				case resp.StatusCode == 200:
					success.Add(1)
				case resp.StatusCode == 429:
					rateLimit.Add(1)
				default:
					otherErr.Add(1)
				}
				resp.Body.Close()
			}
		}()
	}

	// live stats
	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for {
			select {
			case <-stop:
				ticker.Stop()
				return
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				t := total.Load()
				fmt.Printf("  [%.0fs] 总请求=%d  成功=%d  429=%d  失败=%d  QPS=%.1f\n",
					elapsed, t, success.Load(), rateLimit.Load(), otherErr.Load(), float64(t)/elapsed)
			}
		}
	}()

	wg.Wait()
	elapsed := time.Since(start).Seconds()

	fmt.Printf("\n=== 结果 ===\n")
	fmt.Printf("总请求:    %d\n", total.Load())
	fmt.Printf("成功(200): %d\n", success.Load())
	fmt.Printf("限速(429): %d\n", rateLimit.Load())
	fmt.Printf("其他错误:  %d\n", otherErr.Load())
	fmt.Printf("总 QPS:    %.1f\n", float64(total.Load())/elapsed)
	fmt.Printf("成功 QPS:  %.1f\n", float64(success.Load())/elapsed)
	fmt.Printf("成功率:    %.1f%%\n", float64(success.Load())/float64(total.Load())*100)
}
