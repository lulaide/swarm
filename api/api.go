package api

import (
	"encoding/json"
	"net/http"

	C "github.com/metacubex/mihomo/constant"

	"github.com/lulaide/swarm/dispatcher"
	"github.com/lulaide/swarm/pool"
	"github.com/lulaide/swarm/subscribe"
)

type Server struct {
	pool        *pool.Pool
	dispatcher  *dispatcher.Dispatcher
	subscribers []*subscribe.Subscriber
	mux         *http.ServeMux
}

func New(p *pool.Pool, d *dispatcher.Dispatcher, subs []*subscribe.Subscriber) *Server {
	s := &Server{
		pool:        p,
		dispatcher:  d,
		subscribers: subs,
		mux:         http.NewServeMux(),
	}
	s.mux.HandleFunc("/nodes", s.handleNodes)
	s.mux.HandleFunc("/stats", s.handleStats)
	s.mux.HandleFunc("/subscribe/refresh", s.handleRefresh)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"total": s.pool.Len(),
		"alive": s.pool.AliveCount(),
		"nodes": s.pool.Nodes(),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.dispatcher.Stats())
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	for _, sub := range s.subscribers {
		if err := sub.Refresh(r.Context()); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
	}

	s.pool.Update(collectProxies(s.subscribers))

	writeJSON(w, map[string]any{
		"message": "refreshed",
		"total":   s.pool.Len(),
	})
}

func collectProxies(subs []*subscribe.Subscriber) []C.Proxy {
	var all []C.Proxy
	for _, sub := range subs {
		all = append(all, sub.Proxies()...)
	}
	return all
}

// CollectProxies is exported for use in main.go
func CollectProxies(subs []*subscribe.Subscriber) []C.Proxy {
	return collectProxies(subs)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
