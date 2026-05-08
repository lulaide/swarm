package subscribe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/common/convert"
	C "github.com/metacubex/mihomo/constant"

	"gopkg.in/yaml.v3"
)

type proxySchema struct {
	Proxies []map[string]any `yaml:"proxies"`
}

type Subscriber struct {
	name     string
	url      string
	interval time.Duration

	mu      sync.RWMutex
	proxies []C.Proxy

	stopCh chan struct{}
}

func New(name, url string, interval time.Duration) *Subscriber {
	return &Subscriber{
		name:     name,
		url:      url,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

func (s *Subscriber) Name() string {
	return s.name
}

func (s *Subscriber) Proxies() []C.Proxy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]C.Proxy, len(s.proxies))
	copy(result, s.proxies)
	return result
}

func (s *Subscriber) Refresh(ctx context.Context) error {
	proxies, err := fetchAndParse(ctx, s.url)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", s.name, err)
	}
	s.mu.Lock()
	s.proxies = proxies
	s.mu.Unlock()
	return nil
}

func (s *Subscriber) Start(ctx context.Context) error {
	if err := s.Refresh(ctx); err != nil {
		return err
	}
	if s.interval <= 0 {
		return nil
	}
	go s.loop()
	return nil
}

func (s *Subscriber) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = s.Refresh(ctx)
			cancel()
		}
	}
}

func (s *Subscriber) Close() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func fetchAndParse(ctx context.Context, url string) ([]C.Proxy, error) {
	body, err := fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return parseProxies(body)
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clash/swarm")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func parseProxies(buf []byte) ([]C.Proxy, error) {
	// try Clash YAML format first
	schema := &proxySchema{}
	if err := yaml.Unmarshal(buf, schema); err != nil || len(schema.Proxies) == 0 {
		// fallback to V2Ray base64 format
		mappings, err := convert.ConvertsV2Ray(buf)
		if err != nil {
			return nil, fmt.Errorf("failed to parse subscription: %w", err)
		}
		schema.Proxies = mappings
	}

	if len(schema.Proxies) == 0 {
		return nil, errors.New("no proxies found in subscription")
	}

	proxies := make([]C.Proxy, 0, len(schema.Proxies))
	for i, mapping := range schema.Proxies {
		proxy, err := adapter.ParseProxy(mapping)
		if err != nil {
			fmt.Printf("[subscribe] skip proxy %d: %v\n", i, err)
			continue
		}
		proxies = append(proxies, proxy)
	}

	if len(proxies) == 0 {
		return nil, errors.New("all proxies failed to parse")
	}
	return proxies, nil
}

// ParseProxies is exported for testing
func ParseProxies(buf []byte) ([]C.Proxy, error) {
	return parseProxies(buf)
}
