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
}

type Pool struct {
	mu    sync.RWMutex
	nodes []*nodeState

	maxFailures int32
}

func New(maxFailures int) *Pool {
	if maxFailures <= 0 {
		maxFailures = 3
	}
	return &Pool{
		maxFailures: int32(maxFailures),
	}
}

func (p *Pool) Update(proxies []C.Proxy) {
	nodes := make([]*nodeState, len(proxies))
	for i, proxy := range proxies {
		ns := &nodeState{proxy: proxy}
		ns.alive.Store(true)
		nodes[i] = ns
	}

	p.mu.Lock()
	p.nodes = nodes
	p.mu.Unlock()
}

func (p *Pool) Random() (C.Proxy, error) {
	alive := p.aliveNodes()
	if len(alive) == 0 {
		return nil, ErrNoAliveProxy
	}
	return alive[rand.IntN(len(alive))].proxy, nil
}

func (p *Pool) MarkFailed(name string) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, ns := range p.nodes {
		if ns.proxy.Name() == name {
			count := ns.failures.Add(1)
			if count >= p.maxFailures {
				ns.alive.Store(false)
			}
			return
		}
	}
}

func (p *Pool) MarkAlive(name string) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, ns := range p.nodes {
		if ns.proxy.Name() == name {
			ns.failures.Store(0)
			ns.alive.Store(true)
			return
		}
	}
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
			Name:     ns.proxy.Name(),
			Type:     ns.proxy.Type().String(),
			Alive:    ns.alive.Load(),
			Failures: int(ns.failures.Load()),
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
	return len(p.aliveNodes())
}

func (p *Pool) aliveNodes() []*nodeState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	alive := make([]*nodeState, 0, len(p.nodes))
	for _, ns := range p.nodes {
		if ns.alive.Load() {
			alive = append(alive, ns)
		}
	}
	return alive
}

type NodeInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Alive    bool   `json:"alive"`
	Failures int    `json:"failures"`
}
