package proxy

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	C "github.com/metacubex/mihomo/constant"

	"github.com/lulaide/swarm/pool"
)

const (
	maxRetries = 3
	raceCount  = 2 // dial race parallelism
	maxConns   = 4096
	bufSize    = 32 * 1024
)

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, bufSize)
		return &b
	},
}

type Server struct {
	pool     *pool.Pool
	listener net.Listener
	addr     string
	sem      chan struct{}
	dialRace bool
}

func New(addr string, p *pool.Pool, dialRace bool) (*Server, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{
		pool:     p,
		listener: l,
		addr:     addr,
		sem:      make(chan struct{}, maxConns),
		dialRace: dialRace,
	}
	go s.serve()
	return s, nil
}

func (s *Server) Address() string {
	return s.listener.Addr().String()
}

func (s *Server) Close() error {
	return s.listener.Close()
}

func (s *Server) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		select {
		case s.sem <- struct{}{}:
			go func() {
				defer func() { <-s.sem }()
				s.handleConn(conn)
			}()
		default:
			conn.Close()
		}
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, 4096)

	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		conn.SetReadDeadline(time.Time{})

		if req.Method == http.MethodConnect {
			s.handleConnect(conn, req)
			return
		}

		if !s.handleHTTP(conn, req) {
			return
		}
	}
}

// dialResult holds the outcome of a single dial attempt.
type dialResult struct {
	conn    C.Conn
	proxy   C.Proxy
	latency time.Duration
	err     error
}

// dial picks one proxy and dials. Used as the unit of work for both serial and race modes.
func (s *Server) dial(ctx context.Context, metadata *C.Metadata) dialResult {
	proxy, err := s.pool.Random()
	if err != nil {
		return dialResult{err: err}
	}
	start := time.Now()
	conn, err := proxy.DialContext(ctx, metadata)
	return dialResult{conn: conn, proxy: proxy, latency: time.Since(start), err: err}
}

// dialBest dials using the configured strategy (serial retry or parallel race).
func (s *Server) dialBest(metadata *C.Metadata) (C.Conn, C.Proxy, error) {
	if s.dialRace {
		return s.dialWithRace(metadata)
	}
	return s.dialWithRetry(metadata)
}

// dialWithRetry is the original serial retry logic.
func (s *Server) dialWithRetry(metadata *C.Metadata) (C.Conn, C.Proxy, error) {
	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		r := s.dial(ctx, metadata)
		if r.err == nil {
			s.pool.UpdateLatency(r.proxy.Name(), r.latency.Microseconds())
			s.pool.MarkAlive(r.proxy.Name())
			cancel()
			return r.conn, r.proxy, nil
		}
		cancel()
		if r.proxy != nil {
			s.pool.MarkFailed(r.proxy.Name())
			slog.Warn("dial failed", "node", r.proxy.Name(), "attempt", i+1, "err", r.err)
		}
	}
	return nil, nil, pool.ErrNoAliveProxy
}

// dialWithRace dials N nodes in parallel, takes the fastest, closes the rest.
func (s *Server) dialWithRace(metadata *C.Metadata) (C.Conn, C.Proxy, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch := make(chan dialResult, raceCount)
	for i := 0; i < raceCount; i++ {
		go func() { ch <- s.dial(ctx, metadata) }()
	}

	var winner dialResult
	var losers []dialResult

	for i := 0; i < raceCount; i++ {
		r := <-ch
		if r.err != nil {
			if r.proxy != nil {
				s.pool.MarkFailed(r.proxy.Name())
			}
			losers = append(losers, r)
			continue
		}
		if winner.conn == nil {
			winner = r
			s.pool.UpdateLatency(r.proxy.Name(), r.latency.Microseconds())
			s.pool.MarkAlive(r.proxy.Name())
		} else {
			// close the slower winner
			r.conn.Close()
			s.pool.UpdateLatency(r.proxy.Name(), r.latency.Microseconds())
			s.pool.MarkAlive(r.proxy.Name())
		}
	}

	if winner.conn != nil {
		return winner.conn, winner.proxy, nil
	}
	return nil, nil, pool.ErrNoAliveProxy
}

func (s *Server) handleConnect(conn net.Conn, req *http.Request) {
	host := req.Host
	if !strings.Contains(host, ":") {
		host += ":443"
	}

	metadata := &C.Metadata{NetWork: C.TCP}
	if err := metadata.SetRemoteAddress(host); err != nil {
		io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}

	remote, proxy, err := s.dialBest(metadata)
	if err != nil {
		io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		slog.Error("dial failed", "host", host, "err", err)
		return
	}
	defer remote.Close()

	io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	slog.Info("CONNECT", "host", host, "node", proxy.Name())
	relay(conn, remote)
}

func (s *Server) handleHTTP(conn net.Conn, req *http.Request) bool {
	host := req.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}

	wantKeepAlive := strings.EqualFold(req.Header.Get("Proxy-Connection"), "keep-alive") ||
		(req.ProtoAtLeast(1, 1) && !strings.EqualFold(req.Header.Get("Proxy-Connection"), "close"))

	metadata := &C.Metadata{NetWork: C.TCP}
	if err := metadata.SetRemoteAddress(host); err != nil {
		return false
	}

	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	req.RequestURI = ""

	remote, proxy, err := s.dialBest(metadata)
	if err != nil {
		io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return false
	}
	defer remote.Close()

	slog.Info("HTTP", "method", req.Method, "host", host, "node", proxy.Name())

	if err := req.WriteProxy(remote); err != nil {
		return false
	}

	resp, err := http.ReadResponse(bufio.NewReaderSize(remote, 4096), req)
	if err != nil {
		return false
	}

	if wantKeepAlive {
		resp.Header.Set("Connection", "keep-alive")
	} else {
		resp.Header.Set("Connection", "close")
	}

	resp.Write(conn)
	resp.Body.Close()
	return wantKeepAlive
}

func relay(left, right net.Conn) {
	ch := make(chan struct{})
	go func() {
		bp := bufPool.Get().(*[]byte)
		io.CopyBuffer(right, left, *bp)
		bufPool.Put(bp)
		if tc, ok := right.(interface{ SetReadDeadline(time.Time) error }); ok {
			tc.SetReadDeadline(time.Now())
		}
		close(ch)
	}()
	bp := bufPool.Get().(*[]byte)
	io.CopyBuffer(left, right, *bp)
	bufPool.Put(bp)
	if tc, ok := left.(interface{ SetReadDeadline(time.Time) error }); ok {
		tc.SetReadDeadline(time.Now())
	}
	<-ch
}
