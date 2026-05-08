package pool

import (
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"

	C "github.com/metacubex/mihomo/constant"
)

var ErrNoAliveProxy = errors.New("no alive proxy available")

type nodeState struct {
	proxy    C.Proxy
	alive    atomic.Bool
	failures atomic.Int32
	latency  atomic.Int64 // EWMA latency in microseconds
}

type Pool struct {
	mu      sync.RWMutex
	nodes   []*nodeState
	nodeMap map[string]*nodeState // O(1) lookup by name

	maxFailures int32
}

func New(maxFailures int) *Pool {
	if maxFailures <= 0 {
		maxFailures = 3
	}
	return &Pool{
		maxFailures: int32(maxFailures),
		nodeMap:     make(map[string]*nodeState),
	}
}

func (p *Pool) Update(proxies []C.Proxy) {
	nodes := make([]*nodeState, len(proxies))
	nodeMap := make(map[string]*nodeState, len(proxies))
	for i, proxy := range proxies {
		ns := &nodeState{proxy: proxy}
		ns.alive.Store(true)
		nodes[i] = ns
		nodeMap[proxy.Name()] = ns
	}

	p.mu.Lock()
	p.nodes = nodes
	p.nodeMap = nodeMap
	p.mu.Unlock()
}

// Random picks a random alive node using rejection sampling.
// Avoids allocating a filtered slice on every call.
func (p *Pool) Random() (C.Proxy, error) {
	p.mu.RLock()
	n := len(p.nodes)
	if n == 0 {
		p.mu.RUnlock()
		return nil, ErrNoAliveProxy
	}

	// rejection sampling: try random indices up to 3*n times
	start := rand.IntN(n)
	for i := 0; i < 3*n; i++ {
		ns := p.nodes[(start+i)%n]
		if ns.alive.Load() {
			p.mu.RUnlock()
			return ns.proxy, nil
		}
	}
	p.mu.RUnlock()
	return nil, ErrNoAliveProxy
}

// P2C picks two random alive nodes and returns the one with lower latency.
func (p *Pool) P2C() (C.Proxy, error) {
	p.mu.RLock()
	n := len(p.nodes)
	if n == 0 {
		p.mu.RUnlock()
		return nil, ErrNoAliveProxy
	}

	// collect alive nodes inline with bounded scan
	var a, b *nodeState
	start := rand.IntN(n)
	for i := 0; i < n; i++ {
		ns := p.nodes[(start+i)%n]
		if ns.alive.Load() {
			if a == nil {
				a = ns
			} else {
				b = ns
				break
			}
		}
	}
	p.mu.RUnlock()

	if a == nil {
		return nil, ErrNoAliveProxy
	}
	if b == nil {
		return a.proxy, nil
	}

	// pick the one with lower latency (0 means no data, treat equally)
	la, lb := a.latency.Load(), b.latency.Load()
	if la == 0 && lb == 0 {
		// no latency data, random between the two
		if rand.IntN(2) == 0 {
			return a.proxy, nil
		}
		return b.proxy, nil
	}
	if la == 0 {
		return a.proxy, nil // unknown assumed good
	}
	if lb == 0 {
		return b.proxy, nil
	}
	if la <= lb {
		return a.proxy, nil
	}
	return b.proxy, nil
}

// UpdateLatency records an EWMA latency for a node (in microseconds).
func (p *Pool) UpdateLatency(name string, us int64) {
	p.mu.RLock()
	ns := p.nodeMap[name]
	p.mu.RUnlock()
	if ns == nil {
		return
	}
	old := ns.latency.Load()
	if old == 0 {
		ns.latency.Store(us)
	} else {
		// EWMA alpha = 0.3
		ns.latency.Store(old*7/10 + us*3/10)
	}
}

func (p *Pool) getNode(name string) *nodeState {
	p.mu.RLock()
	ns := p.nodeMap[name]
	p.mu.RUnlock()
	return ns
}

func (p *Pool) MarkFailed(name string) {
	ns := p.getNode(name)
	if ns == nil {
		return
	}
	count := ns.failures.Add(1)
	if count >= p.maxFailures {
		ns.alive.Store(false)
	}
}

func (p *Pool) MarkAlive(name string) {
	ns := p.getNode(name)
	if ns == nil {
		return
	}
	ns.failures.Store(0)
	ns.alive.Store(true)
}

func (p *Pool) ResetAll() {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, ns := range p.nodes {
		ns.failures.Store(0)
		ns.alive.Store(true)
	}
}

func (p *Pool) Nodes() []NodeInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	infos := make([]NodeInfo, len(p.nodes))
	for i, ns := range p.nodes {
		infos[i] = NodeInfo{
			Name:      ns.proxy.Name(),
			Type:      ns.proxy.Type().String(),
			Alive:     ns.alive.Load(),
			Failures:  int(ns.failures.Load()),
			LatencyUs: ns.latency.Load(),
		}
	}
	return infos
}

func (p *Pool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.nodes)
}

func (p *Pool) AliveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, ns := range p.nodes {
		if ns.alive.Load() {
			count++
		}
	}
	return count
}

type NodeInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Alive     bool   `json:"alive"`
	Failures  int    `json:"failures"`
	LatencyUs int64  `json:"latency_us"`
}
