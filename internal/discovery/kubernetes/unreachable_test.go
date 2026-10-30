package kubernetes_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/guilherme-grimm/graph-go/internal/discovery/kubernetes"
)

// writeKubeconfig points a kubeconfig at server, the way a stale context for
// a stopped kind cluster does.
func writeKubeconfig(t *testing.T, server string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	body := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: gone
  cluster:
    server: %s
    insecure-skip-tls-verify: true
contexts:
- name: gone
  context:
    cluster: gone
    user: gone
current-context: gone
users:
- name: gone
  user:
    token: x
`, server)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// closedAddr returns a localhost address nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestNewFailsFastOnUnreachableCluster(t *testing.T) {
	hanging := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(hanging.Close)
	kubernetes.SetProbeTimeout(t, 300*time.Millisecond)

	tests := []struct {
		name   string
		server string
	}{
		{name: "connection refused", server: "https://" + closedAddr(t)},
		{name: "server never answers", server: hanging.URL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			d, err := kubernetes.New(kubernetes.Config{KubeconfigPath: writeKubeconfig(t, tt.server)})
			if err == nil {
				_ = d.Close()
				t.Fatal("expected an error for an unreachable cluster")
			}
			if !strings.Contains(err.Error(), "unreachable") {
				t.Errorf("error %q does not say the cluster is unreachable", err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("New took %s, want it bounded by the probe timeout", elapsed)
			}
		})
	}
}

// When informers cannot sync (API server gone after New, or RBAC forbids a
// list), Discover must still return once its ctx ends instead of hanging.
func TestDiscoverHonoursContextWhenCacheNeverSyncs(t *testing.T) {
	release := make(chan struct{})
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		<-release
		return true, nil, fmt.Errorf("released")
	})

	d := kubernetes.NewWithClient(client, kubernetes.Config{})
	t.Cleanup(func() {
		_ = d.Close()
		close(release)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := d.Discover(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error when caches never sync")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Discover ignored its context and hung")
	}
}
