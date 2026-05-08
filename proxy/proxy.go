package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	C "github.com/metacubex/mihomo/constant"

	"github.com/lulaide/swarm/pool"
)

type Server struct {
	pool     *pool.Pool
	listener net.Listener
	addr     string
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
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)

	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}

	if req.Method == http.MethodConnect {
		s.handleConnect(conn, req)
	} else {
		s.handleHTTP(conn, br, req)
	}
}

const maxRetries = 3

func (s *Server) handleConnect(conn net.Conn, req *http.Request) {
	host := req.Host
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}

	metadata := &C.Metadata{NetWork: C.TCP}
	if err := metadata.SetRemoteAddress(host); err != nil {
		fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}

	for i := 0; i < maxRetries; i++ {
		proxy, err := s.pool.Random()
		if err != nil {
			fmt.Fprintf(conn, "HTTP/1.1 503 Service Unavailable\r\n\r\n")
			fmt.Printf("[proxy] CONNECT %s failed: %v\n", host, err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		remote, err := proxy.DialContext(ctx, metadata)
		if err != nil {
			cancel()
			s.pool.MarkFailed(proxy.Name())
			fmt.Printf("[proxy] CONNECT %s via %s failed (attempt %d): %v\n", host, proxy.Name(), i+1, err)
			continue
		}

		s.pool.MarkAlive(proxy.Name())
		fmt.Fprintf(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		fmt.Printf("[proxy] CONNECT %s via %s\n", host, proxy.Name())
		relay(conn, remote)
		remote.Close()
		cancel()
		return
	}

	fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
	fmt.Printf("[proxy] CONNECT %s failed after %d retries\n", host, maxRetries)
}

func (s *Server) handleHTTP(conn net.Conn, br *bufio.Reader, req *http.Request) {
	host := req.Host
	if !strings.Contains(host, ":") {
		host = host + ":80"
	}

	metadata := &C.Metadata{NetWork: C.TCP}
	if err := metadata.SetRemoteAddress(host); err != nil {
		return
	}

	// Remove proxy headers before forwarding
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	req.RequestURI = ""

	for i := 0; i < maxRetries; i++ {
		proxy, err := s.pool.Random()
		if err != nil {
			resp := &http.Response{StatusCode: 503, ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)}
			resp.Write(conn)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		remote, err := proxy.DialContext(ctx, metadata)
		if err != nil {
			cancel()
			s.pool.MarkFailed(proxy.Name())
			fmt.Printf("[proxy] HTTP %s via %s failed (attempt %d): %v\n", host, proxy.Name(), i+1, err)
			continue
		}

		s.pool.MarkAlive(proxy.Name())
		fmt.Printf("[proxy] HTTP %s %s via %s\n", req.Method, req.URL, proxy.Name())

		if err := req.WriteProxy(remote); err != nil {
			remote.Close()
			cancel()
			continue
		}

		resp, err := http.ReadResponse(bufio.NewReader(remote), req)
		if err != nil {
			remote.Close()
			cancel()
			continue
		}

		resp.Write(conn)
		resp.Body.Close()
		remote.Close()
		cancel()
		return
	}

	fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
}

func relay(left, right net.Conn) {
	ch := make(chan struct{})
	go func() {
		io.Copy(right, left)
		if tc, ok := right.(interface{ SetReadDeadline(time.Time) error }); ok {
			tc.SetReadDeadline(time.Now())
		}
		close(ch)
	}()
	io.Copy(left, right)
	if tc, ok := left.(interface{ SetReadDeadline(time.Time) error }); ok {
		tc.SetReadDeadline(time.Now())
	}
	<-ch
}

// parseBasicAuth extracts username from Proxy-Authorization header
func parseBasicAuth(header string) (string, bool) {
	if !strings.HasPrefix(header, "Basic ") {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(header[6:])
	if err != nil {
		return "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	return parts[0], true
}
