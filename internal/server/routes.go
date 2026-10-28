package server

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/gorilla/mux"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/health"
	"github.com/guilherme-grimm/graph-go/internal/webui"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	r.Use(s.corsMiddleware)

	r.HandleFunc("/health", s.healthHandler)

	// API routes
	r.HandleFunc("/api/graph", s.graphHandler).Methods("GET")
	r.HandleFunc("/api/node/{id}", s.nodeHandler).Methods("GET")
	r.HandleFunc("/api/health", s.apiHealthHandler).Methods("GET")

	r.HandleFunc("/websocket", s.websocketHandler)

	// SPA: serve embedded frontend on any unmatched path. Registered last so
	// API and WebSocket routes take precedence.
	r.PathPrefix("/").Handler(webui.Handler())

	return r
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if allowed, ok := matchOrigin(origin, s.allowedOrigins); ok {
				w.Header().Set("Access-Control-Allow-Origin", allowed)
				w.Header().Set("Vary", "Origin")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Credentials", "false")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// matchOrigin returns the value to send in Access-Control-Allow-Origin.
// A patterns entry of "*" matches any origin and echoes "*".
func matchOrigin(origin string, patterns []string) (string, bool) {
	for _, p := range patterns {
		if p == "*" {
			return "*", true
		}
		if p == origin {
			return origin, true
		}
	}
	return "", false
}

// wsPatterns converts CORS-style origins (http://host:port) into coder/websocket
// OriginPatterns (host:port). Preserves "*" and bare host patterns unchanged.
func wsPatterns(origins []string) []string {
	if len(origins) == 0 {
		return []string{"*"}
	}
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if o == "*" {
			out = append(out, "*")
			continue
		}
		host := o
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		host = strings.TrimSuffix(host, "/")
		out = append(out, host)
	}
	return out
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) graphHandler(w http.ResponseWriter, r *http.Request) {
	g, err := s.registry.DiscoverAll()
	if err != nil {
		s.logger.Errorw("graphHandler discover failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": g}); err != nil {
		s.logger.Errorw("graphHandler encode failed", "err", err)
	}
}

func (s *Server) nodeHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	g, err := s.registry.DiscoverAll()
	if err != nil {
		s.logger.Errorw("nodeHandler discover failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	for _, node := range g.Nodes {
		if node.Id == id {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{"data": node}); err != nil {
				s.logger.Errorw("nodeHandler encode failed", "err", err)
			}
			return
		}
	}

	http.Error(w, "node not found", http.StatusNotFound)
}

func (s *Server) apiHealthHandler(w http.ResponseWriter, r *http.Request) {
	status := "ok"

	metrics := s.registry.HealthAll()
	for _, m := range metrics {
		if m.Status == health.Unhealthy {
			status = "error"
			break
		}
		if m.Status == health.Degraded {
			status = "degraded"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":    status,
		"timestamp": time.Now().Format(time.RFC3339),
	}); err != nil {
		s.logger.Errorw("apiHealthHandler encode failed", "err", err)
	}
}

func (s *Server) websocketHandler(w http.ResponseWriter, r *http.Request) {
	patterns := wsPatterns(s.allowedOrigins)

	socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: patterns,
	})
	if err != nil {
		s.logger.Warnw("websocket accept failed", "err", err)
		return
	}

	defer socket.Close(websocket.StatusGoingAway, "server closing websocket")

	ctx := r.Context()
	socketCtx := socket.CloseRead(ctx)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var (
		seeded     bool
		prevPrint  uint64
		prevHealth = map[string]string{}
	)

	for {
		// Get the current graph to map adapter health to actual node IDs
		g, err := s.registry.DiscoverAll()
		if err != nil {
			s.logger.Warnw("websocket discover failed", "err", err)
		}

		if g != nil {
			// Detect graph topology changes (nodes added/removed).
			fp := nodeSetFingerprint(g)
			if seeded && fp != prevPrint {
				if err := writeJSON(socketCtx, socket, wsMessage{Type: "graph_update", Payload: struct{}{}}); err != nil {
					return
				}
			}
			prevPrint = fp

			// Ship only the health values that changed since the last tick, in
			// one message. Per-node messages meant thousands of frames (and
			// thousands of client re-renders) every tick on large clusters.
			current := nodeHealths(g, s.registry.HealthAll())
			updates := make([]healthUpdate, 0)
			for id, h := range current {
				if prevHealth[id] != h {
					updates = append(updates, healthUpdate{NodeID: id, Health: h})
				}
			}
			prevHealth = current
			if len(updates) > 0 {
				sort.Slice(updates, func(i, j int) bool { return updates[i].NodeID < updates[j].NodeID })
				if err := writeJSON(socketCtx, socket, wsMessage{Type: "health_batch", Payload: healthBatch{Updates: updates}}); err != nil {
					return
				}
			}
			seeded = true
		}

		select {
		case <-socketCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

type wsMessage struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type healthUpdate struct {
	NodeID string `json:"nodeId"`
	Health string `json:"health"`
}

type healthBatch struct {
	Updates []healthUpdate `json:"updates"`
}

func writeJSON(ctx context.Context, socket *websocket.Conn, msg wsMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil
	}
	return socket.Write(ctx, websocket.MessageText, data)
}

// nodeHealths resolves the health of every node in g. Adapter-owned nodes get
// their health via the adapter lookup; topology nodes (e.g. Kubernetes
// resources) carry health directly on Node.Health. Nodes with neither are
// left out.
func nodeHealths(g *graph.Graph, metrics []health.HealthMetrics) map[string]string {
	adapterHealth := make(map[string]string, len(metrics))
	for _, m := range metrics {
		adapterHealth[m.NodeID] = string(m.Status)
	}
	out := make(map[string]string, len(g.Nodes))
	for _, node := range g.Nodes {
		adapterName, _ := node.Metadata["adapter"].(string)
		h, ok := adapterHealth[adapterName]
		if !ok {
			if node.Health == "" {
				continue
			}
			h = node.Health
		}
		out[node.Id] = h
	}
	return out
}

// nodeSetFingerprint is an order-independent hash of g's node IDs, so a
// changed node set can be detected without sorting and joining every ID.
func nodeSetFingerprint(g *graph.Graph) uint64 {
	var sum uint64
	for _, node := range g.Nodes {
		h := fnv.New64a()
		h.Write([]byte(node.Id))
		sum += h.Sum64()
	}
	return sum ^ uint64(len(g.Nodes))
}
