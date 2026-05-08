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
	maxConns   = 4096 // max concurrent connections
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
	sem      chan struct{} // concurrency semaphore
}

func New(addr string, p *pool.Pool) (*Server, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{
		pool:     p,
		listener: l,
		addr:     addr,
		sem:      make(chan struct{}, maxConns),
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
			// at capacity, reject
			conn.Close()
		}
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, 4096)

	// keep-alive loop: handle multiple requests on the same connection
	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		conn.SetReadDeadline(time.Time{})

		if req.Method == http.MethodConnect {
			s.handleConnect(conn, req)
			return // CONNECT hijacks the connection
		}

		keepAlive := s.handleHTTP(conn, req)
		if !keepAlive {
			return
		}
	}
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

	for i := 0; i < maxRetries; i++ {
		proxy, err := s.pool.Random()
		if err != nil {
			io.WriteString(conn, "HTTP/1.1 503 Service Unavailable\r\n\r\n")
			slog.Warn("no proxy available", "host", host, "err", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := time.Now()
		remote, err := proxy.DialContext(ctx, metadata)
		if err != nil {
			cancel()
			s.pool.MarkFailed(proxy.Name())
			slog.Warn("dial failed", "host", host, "node", proxy.Name(), "attempt", i+1, "err", err)
			continue
		}

		latency := time.Since(start)
		s.pool.UpdateLatency(proxy.Name(), latency.Microseconds())
		s.pool.MarkAlive(proxy.Name())
		io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		slog.Info("CONNECT", "host", host, "node", proxy.Name(), "dial_ms", latency.Milliseconds())
		relay(conn, remote)
		remote.Close()
		cancel()
		return
	}

	io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
	slog.Error("all retries failed", "host", host, "retries", maxRetries)
}

// handleHTTP handles a plain HTTP request. Returns true if the connection should be kept alive.
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

	for i := 0; i < maxRetries; i++ {
		proxy, err := s.pool.Random()
		if err != nil {
			io.WriteString(conn, "HTTP/1.1 503 Service Unavailable\r\n\r\n")
			return false
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := time.Now()
		remote, err := proxy.DialContext(ctx, metadata)
		if err != nil {
			cancel()
			s.pool.MarkFailed(proxy.Name())
			slog.Warn("dial failed", "host", host, "node", proxy.Name(), "attempt", i+1, "err", err)
			continue
		}

		latency := time.Since(start)
		s.pool.UpdateLatency(proxy.Name(), latency.Microseconds())
		s.pool.MarkAlive(proxy.Name())
		slog.Info("HTTP", "method", req.Method, "host", host, "node", proxy.Name(), "dial_ms", latency.Milliseconds())

		if err := req.WriteProxy(remote); err != nil {
			remote.Close()
			cancel()
			continue
		}

		resp, err := http.ReadResponse(bufio.NewReaderSize(remote, 4096), req)
		if err != nil {
			remote.Close()
			cancel()
			continue
		}

		// set Connection header for client
		if wantKeepAlive {
			resp.Header.Set("Connection", "keep-alive")
		} else {
			resp.Header.Set("Connection", "close")
		}

		resp.Write(conn)
		resp.Body.Close()
		remote.Close()
		cancel()
		return wantKeepAlive
	}

	io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
	return false
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
