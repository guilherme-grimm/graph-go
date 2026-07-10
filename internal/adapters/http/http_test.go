package http

import (
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/adapters/adaptertest"
)

func TestContract(t *testing.T) {
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/ready" {
			nethttp.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"healthy"}`)
	}))
	t.Cleanup(server.Close)

	config := adapters.ConnectionConfig{
		"endpoint":    server.URL,
		"health_path": "/ready",
		"node_type":   "api",
		"name":        "orders",
	}
	connected := New(zap.NewNop().Sugar())
	if err := connected.Connect(config); err != nil {
		t.Fatalf("failed to connect HTTP adapter: %v", err)
	}

	adaptertest.RunContractTests(
		t,
		connected,
		func() adapters.Adapter { return New(zap.NewNop().Sugar()) },
		config,
		adaptertest.ContractOpts{
			MinNodes:           1,
			RootNodeType:       "api",
			RequiredHealthKeys: []string{"status"},
		},
	)
}

func TestConnectServerDown(t *testing.T) {
	server := httptest.NewServer(nethttp.HandlerFunc(func(nethttp.ResponseWriter, *nethttp.Request) {}))
	endpoint := server.URL
	server.Close()

	a := New(zap.NewNop().Sugar())
	err := a.Connect(adapters.ConnectionConfig{"endpoint": endpoint, "name": "offline"})
	if err == nil {
		t.Fatal("Connect should fail when the server is down")
	}
	if !strings.Contains(err.Error(), "health check failed") {
		t.Fatalf("Connect error = %q, want health check context", err)
	}
}

func TestConnectTimeout(t *testing.T) {
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(nethttp.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	a := New(zap.NewNop().Sugar())
	a.client.Timeout = 10 * time.Millisecond
	err := a.Connect(adapters.ConnectionConfig{"endpoint": server.URL, "name": "slow"})
	if err == nil {
		t.Fatal("Connect should fail when the health request times out")
	}
}

func TestHealthStatusClasses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantStatus string
	}{
		{name: "success", statusCode: nethttp.StatusNoContent, wantStatus: "healthy"},
		{name: "client error", statusCode: nethttp.StatusNotFound, wantStatus: "degraded"},
		{name: "server error", statusCode: nethttp.StatusServiceUnavailable, wantStatus: "unhealthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusCode := nethttp.StatusNoContent
			server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
				w.WriteHeader(statusCode)
			}))
			t.Cleanup(server.Close)

			a := New(zap.NewNop().Sugar())
			if err := a.Connect(adapters.ConnectionConfig{"endpoint": server.URL, "name": "status"}); err != nil {
				t.Fatalf("Connect returned error: %v", err)
			}

			statusCode = tt.statusCode
			metrics, err := a.Health()
			if err != nil {
				t.Fatalf("Health returned error: %v", err)
			}
			if got := metrics["status"]; got != tt.wantStatus {
				t.Errorf("status = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}
