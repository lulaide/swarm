package subscribe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var clashYAML = `
proxies:
  - name: "test-ss"
    type: ss
    server: 1.2.3.4
    port: 8388
    cipher: aes-256-gcm
    password: "testpass"
  - name: "test-trojan"
    type: trojan
    server: 5.6.7.8
    port: 443
    password: "trojanpass"
`

func TestParseProxiesClashYAML(t *testing.T) {
	proxies, err := ParseProxies([]byte(clashYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(proxies))
	}
	if proxies[0].Name() != "test-ss" {
		t.Errorf("expected name test-ss, got %s", proxies[0].Name())
	}
	if proxies[1].Name() != "test-trojan" {
		t.Errorf("expected name test-trojan, got %s", proxies[1].Name())
	}
}

func TestParseProxiesEmpty(t *testing.T) {
	_, err := ParseProxies([]byte(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestSubscriberRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(clashYAML))
	}))
	defer server.Close()

	sub := New("test", server.URL, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sub.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	proxies := sub.Proxies()
	if len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(proxies))
	}
}

func TestSubscriberRefreshBadURL(t *testing.T) {
	sub := New("test", "http://127.0.0.1:1", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := sub.Refresh(ctx)
	if err == nil {
		t.Error("expected error for bad URL")
	}
}
