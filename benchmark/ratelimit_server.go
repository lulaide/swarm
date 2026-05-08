// +build ignore

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// 模拟一个有 IP 限速的 API：每个 IP 每秒最多 5 次请求
type limiter struct {
	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	count int
	reset time.Time
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	w, ok := l.windows[ip]
	if !ok || now.After(w.reset) {
		l.windows[ip] = &window{count: 1, reset: now.Add(time.Second)}
		return true
	}
	w.count++
	return w.count <= 5 // 每秒每 IP 最多 5 次
}

func main() {
	l := &limiter{windows: make(map[string]*window)}

	http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("X-Real-IP")
		if ip == "" {
			ip = r.RemoteAddr
		}

		if !l.allow(ip) {
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "rate limit exceeded",
				"ip":    ip,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"ip":     ip,
		})
	})

	fmt.Println("限速服务器启动: :8888 (每 IP 每秒 5 次)")
	http.ListenAndServe(":8888", nil)
}
