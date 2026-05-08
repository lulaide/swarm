package dispatcher

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/component/nat"

	"github.com/lulaide/swarm/pool"
)

type Dispatcher struct {
	pool     *pool.Pool
	natTable C.NatTable

	// stats
	totalRequests atomic.Int64
	activeConns   atomic.Int64
	failedDials   atomic.Int64
}

func New(p *pool.Pool) *Dispatcher {
	return &Dispatcher{
		pool:     p,
		natTable: nat.New(),
	}
}

// HandleTCPConn implements C.Tunnel
func (d *Dispatcher) HandleTCPConn(conn net.Conn, metadata *C.Metadata) {
	d.totalRequests.Add(1)
	d.activeConns.Add(1)
	defer d.activeConns.Add(-1)
	defer conn.Close()

	proxy, err := d.pool.Random()
	if err != nil {
		fmt.Printf("[dispatcher] no proxy available: %v\n", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), C.DefaultTCPTimeout)
	defer cancel()

	remoteConn, err := proxy.DialContext(ctx, metadata)
	if err != nil {
		d.failedDials.Add(1)
		d.pool.MarkFailed(proxy.Name())
		fmt.Printf("[dispatcher] dial %s via %s failed: %v\n", metadata.RemoteAddress(), proxy.Name(), err)
		return
	}
	defer remoteConn.Close()

	d.pool.MarkAlive(proxy.Name())
	fmt.Printf("[dispatcher] %s --> %s via %s\n", metadata.SourceAddress(), metadata.RemoteAddress(), proxy.Name())

	relay(conn, remoteConn)
}

// HandleUDPPacket implements C.Tunnel
func (d *Dispatcher) HandleUDPPacket(packet C.UDPPacket, metadata *C.Metadata) {
	// UDP support is minimal for now - just drop
	packet.Drop()
}

// NatTable implements C.Tunnel
func (d *Dispatcher) NatTable() C.NatTable {
	return d.natTable
}

// Stats returns current dispatcher statistics
func (d *Dispatcher) Stats() Stats {
	return Stats{
		TotalRequests: d.totalRequests.Load(),
		ActiveConns:   d.activeConns.Load(),
		FailedDials:   d.failedDials.Load(),
	}
}

type Stats struct {
	TotalRequests int64 `json:"total_requests"`
	ActiveConns   int64 `json:"active_conns"`
	FailedDials   int64 `json:"failed_dials"`
}

func relay(left, right net.Conn) {
	ch := make(chan struct{})
	go func() {
		_, _ = io.Copy(right, left)
		if tc, ok := right.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = tc.SetReadDeadline(time.Now())
		}
		close(ch)
	}()
	_, _ = io.Copy(left, right)
	if tc, ok := left.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = tc.SetReadDeadline(time.Now())
	}
	<-ch
}
