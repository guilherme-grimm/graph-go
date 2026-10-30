package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/health"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// dialTestWebSocket creates an httptest server from s.RegisterRoutes(),
// dials the /websocket endpoint, and returns the connection plus a cleanup
// function that closes everything.
func dialTestWebSocket(t *testing.T, s *Server) (*websocket.Conn, func()) {
	t.Helper()
	srv := httptest.NewServer(s.RegisterRoutes())
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/websocket"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	return conn, func() { conn.CloseNow(); cancel(); srv.Close() }
}

// wsEnvelope is the structure sent over the WebSocket.
type wsEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// healthMsg is one node's health, flattened out of a health_batch message.
type healthMsg struct {
	Type    string
	Payload struct {
		NodeID string `json:"nodeId"`
		Health string `json:"health"`
	}
}

// readAllMessages reads WebSocket messages until the context deadline is
// reached, flattening each health_batch into one healthMsg per update.
func readAllMessages(t *testing.T, conn *websocket.Conn, timeout time.Duration) []healthMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var msgs []healthMsg
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var env wsEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("unmarshal ws message: %v", err)
		}
		if env.Type != "health_batch" {
			msgs = append(msgs, healthMsg{Type: env.Type})
			continue
		}
		var batch struct {
			Updates []struct {
				NodeID string `json:"nodeId"`
				Health string `json:"health"`
			} `json:"updates"`
		}
		if err := json.Unmarshal(env.Payload, &batch); err != nil {
			t.Fatalf("unmarshal health_batch: %v", err)
		}
		for _, u := range batch.Updates {
			var m healthMsg
			m.Type = env.Type
			m.Payload.NodeID = u.NodeID
			m.Payload.Health = u.Health
			msgs = append(msgs, m)
		}
	}
	return msgs
}

func TestWebSocket_HealthUpdates(t *testing.T) {
	reg := &mockRegistry{
		graph: &graph.Graph{
			Nodes: []nodes.Node{
				{Id: "service-pg", Type: "postgres", Name: "pg", Metadata: map[string]any{"adapter": "pg"}},
			},
		},
		health: []health.HealthMetrics{
			{NodeID: "pg", Status: health.Healthy},
		},
	}
	s := newTestServer(reg)
	conn, cleanup := dialTestWebSocket(t, s)
	defer cleanup()

	msgs := readAllMessages(t, conn, 2*time.Second)

	if len(msgs) == 0 {
		t.Fatal("expected at least one health update")
	}

	found := false
	for _, m := range msgs {
		if m.Type != "health_batch" {
			t.Errorf("unexpected message type %q", m.Type)
		}
		if m.Payload.NodeID == "service-pg" && m.Payload.Health == "healthy" {
			found = true
		}
	}
	if !found {
		t.Errorf("did not find health update for service-pg with health=healthy; got %+v", msgs)
	}
}

func TestNodeHealths_OnlyChangesAreResent(t *testing.T) {
	g := &graph.Graph{Nodes: []nodes.Node{
		{Id: "pod-a", Health: "healthy"},
		{Id: "pod-b", Health: "healthy"},
		{Id: "no-health"},
	}}
	first := NodeHealths(g, nil)
	if len(first) != 2 {
		t.Fatalf("expected 2 nodes with health, got %v", first)
	}
	g.Nodes[1].Health = "unhealthy"
	second := NodeHealths(g, nil)
	var changed []string
	for id, h := range second {
		if first[id] != h {
			changed = append(changed, id)
		}
	}
	if len(changed) != 1 || changed[0] != "pod-b" {
		t.Errorf("expected only pod-b to change, got %v", changed)
	}
}

func TestNodeSetFingerprint(t *testing.T) {
	a := &graph.Graph{Nodes: []nodes.Node{{Id: "x"}, {Id: "y"}}}
	b := &graph.Graph{Nodes: []nodes.Node{{Id: "y"}, {Id: "x"}}}
	c := &graph.Graph{Nodes: []nodes.Node{{Id: "x"}}}
	if nodeSetFingerprint(a) != nodeSetFingerprint(b) {
		t.Error("fingerprint should not depend on node order")
	}
	if nodeSetFingerprint(a) == nodeSetFingerprint(c) {
		t.Error("fingerprint should change when a node is removed")
	}
}

func TestWebSocket_MultipleNodes(t *testing.T) {
	reg := &mockRegistry{
		graph: &graph.Graph{
			Nodes: []nodes.Node{
				{Id: "service-pg", Type: "postgres", Name: "pg", Metadata: map[string]any{"adapter": "pg"}},
				{Id: "service-mongo", Type: "mongodb", Name: "mongo", Metadata: map[string]any{"adapter": "mongo"}},
			},
		},
		health: []health.HealthMetrics{
			{NodeID: "pg", Status: health.Healthy},
			{NodeID: "mongo", Status: health.Degraded},
		},
	}
	s := newTestServer(reg)
	conn, cleanup := dialTestWebSocket(t, s)
	defer cleanup()

	msgs := readAllMessages(t, conn, 2*time.Second)

	seen := make(map[string]string) // nodeId -> health
	for _, m := range msgs {
		seen[m.Payload.NodeID] = m.Payload.Health
	}

	if seen["service-pg"] != "healthy" {
		t.Errorf("expected service-pg=healthy, got %q", seen["service-pg"])
	}
	if seen["service-mongo"] != "degraded" {
		t.Errorf("expected service-mongo=degraded, got %q", seen["service-mongo"])
	}
}

func TestWebSocket_NoAdapterMetadata(t *testing.T) {
	reg := &mockRegistry{
		graph: &graph.Graph{
			Nodes: []nodes.Node{
				{Id: "orphan-node", Type: "unknown", Name: "orphan", Metadata: map[string]any{}},
			},
		},
		health: []health.HealthMetrics{
			{NodeID: "pg", Status: health.Healthy},
		},
	}
	s := newTestServer(reg)
	conn, cleanup := dialTestWebSocket(t, s)
	defer cleanup()

	msgs := readAllMessages(t, conn, 2*time.Second)

	for _, m := range msgs {
		if m.Payload.NodeID == "orphan-node" {
			t.Errorf("should not have received message for node without adapter metadata")
		}
	}
}

func TestWebSocket_AdapterNotInHealth(t *testing.T) {
	reg := &mockRegistry{
		graph: &graph.Graph{
			Nodes: []nodes.Node{
				{Id: "service-redis", Type: "redis", Name: "redis", Metadata: map[string]any{"adapter": "redis"}},
			},
		},
		health: []health.HealthMetrics{}, // no health for "redis"
	}
	s := newTestServer(reg)
	conn, cleanup := dialTestWebSocket(t, s)
	defer cleanup()

	msgs := readAllMessages(t, conn, 2*time.Second)

	for _, m := range msgs {
		if m.Payload.NodeID == "service-redis" {
			t.Errorf("should not have received message for adapter with no health metric")
		}
	}
}

func TestWebSocket_DiscoveryError(t *testing.T) {
	reg := &mockRegistry{
		graph:   nil,
		discErr: fmt.Errorf("docker daemon unreachable"),
		health:  []health.HealthMetrics{},
	}
	s := newTestServer(reg)
	conn, cleanup := dialTestWebSocket(t, s)
	defer cleanup()

	// Should not crash; just no messages sent.
	msgs := readAllMessages(t, conn, 2*time.Second)

	for _, m := range msgs {
		t.Errorf("unexpected message when DiscoverAll errors: %+v", m)
	}
}

func TestWebSocket_NilGraph(t *testing.T) {
	reg := &mockRegistry{
		graph:  nil,
		health: []health.HealthMetrics{{NodeID: "pg", Status: health.Healthy}},
	}
	s := newTestServer(reg)
	conn, cleanup := dialTestWebSocket(t, s)
	defer cleanup()

	msgs := readAllMessages(t, conn, 2*time.Second)

	for _, m := range msgs {
		t.Errorf("unexpected message when graph is nil: %+v", m)
	}
}

func TestWebSocket_ClientClose(t *testing.T) {
	reg := &mockRegistry{
		graph: &graph.Graph{
			Nodes: []nodes.Node{
				{Id: "service-pg", Type: "postgres", Name: "pg", Metadata: map[string]any{"adapter": "pg"}},
			},
		},
		health: []health.HealthMetrics{
			{NodeID: "pg", Status: health.Healthy},
		},
	}
	s := newTestServer(reg)
	conn, cleanup := dialTestWebSocket(t, s)
	defer cleanup()

	// Close from client side immediately.
	conn.Close(websocket.StatusNormalClosure, "bye")

	// If the handler leaks a goroutine this test will eventually be
	// caught by the race detector or -count flag. Here we just verify
	// the close doesn't panic.
}

func TestWebSocket_ResendsFullSnapshotAfterFirstTick(t *testing.T) {
	reg := &mockRegistry{
		graph: &graph.Graph{
			Nodes: []nodes.Node{{Id: "pod-a", Health: "healthy"}},
		},
	}
	s := newTestServer(reg)
	conn, cleanup := dialTestWebSocket(t, s)
	defer cleanup()

	// Ticks are 5s apart: the first tick and the full resend on the second.
	msgs := readAllMessages(t, conn, 7*time.Second)
	count := 0
	for _, m := range msgs {
		if m.Payload.NodeID == "pod-a" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected pod-a health on the first tick and again on the resend, got %d", count)
	}
}
