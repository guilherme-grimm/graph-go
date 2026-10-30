package kubernetes_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// closedAddr returns a localhost address nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
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
			if d != nil {
				t.Cleanup(func() { _ = d.Close() })
			}
			require.Error(t, err, "unreachable cluster")
			assert.ErrorContains(t, err, "unreachable")
			assert.Less(t, time.Since(start), 5*time.Second, "New should be bounded by the probe timeout")
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
		require.Error(t, err, "caches never sync")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Discover ignored its context and hung")
	}
}
