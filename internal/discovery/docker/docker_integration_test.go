//go:build integration

package docker

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestDiscover_ComposeProjectsDoNotCollide is the regression test for #43:
// two Compose projects that both define a `db` service must produce two
// distinct adapter names instead of one replacing the other.
func TestDiscover_ComposeProjectsDoNotCollide(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	suffix := time.Now().UnixNano()
	projects := []string{fmt.Sprintf("gg43a%d", suffix), fmt.Sprintf("gg43b%d", suffix)}

	wantByID := make(map[string]string, len(projects))
	for _, project := range projects {
		ctr, err := testcontainers.Run(ctx, "alpine:3.20",
			testcontainers.WithCmd("sleep", "300"),
			testcontainers.WithLabels(map[string]string{
				labelComposeProject: project,
				labelComposeService: "db",
			}),
		)
		testcontainers.CleanupContainer(t, ctr)
		require.NoError(t, err)
		wantByID[ctr.GetContainerID()] = project + ".db"
	}

	d, err := NewDockerDiscovery(DockerDiscoveryConfig{}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	services, err := d.Discover(ctx)
	require.NoError(t, err)

	got := make(map[string]string, len(wantByID))
	for _, svc := range services {
		id, _ := svc.Metadata["container_id"].(string)
		if _, ok := wantByID[id]; ok {
			got[id] = svc.Name
		}
	}
	assert.Equal(t, wantByID, got)
}

// TestDiscover_AddressesReachableFromHost runs graph-go's discovery from the
// test process, i.e. on the host, the way the release binary and
// `graph-go mcp` run. Compose service names don't resolve there, so each
// container must come back with an address that can actually be dialed:
// the published port for one that publishes, the container IP (Linux) for
// one that doesn't.
func TestDiscover_AddressesReachableFromHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	project := fmt.Sprintf("ggaddr%d", time.Now().UnixNano())
	run := func(service string, publish bool) string {
		opts := []testcontainers.ContainerCustomizer{
			testcontainers.WithLabels(map[string]string{
				labelComposeProject: project,
				labelComposeService: service,
			}),
			testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections")),
		}
		if publish {
			opts = append(opts, testcontainers.WithExposedPorts("6379/tcp"))
		}
		ctr, err := testcontainers.Run(ctx, "redis:7-alpine", opts...)
		testcontainers.CleanupContainer(t, ctr)
		require.NoError(t, err)
		return ctr.GetContainerID()
	}
	ids := map[string]string{
		run("cache-published", true):    "cache-published",
		run("cache-unpublished", false): "cache-unpublished",
	}

	d, err := NewDockerDiscovery(DockerDiscoveryConfig{}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.False(t, d.self.InContainer, "this test must run on the host")

	services, err := d.Discover(ctx)
	require.NoError(t, err)

	seen := 0
	for _, svc := range services {
		id, _ := svc.Metadata["container_id"].(string)
		name, ok := ids[id]
		if !ok {
			continue
		}
		seen++
		host, _ := svc.Config["host"].(string)
		port, _ := svc.Config["port"].(uint16)
		assert.NotEqual(t, name, host, "%s: Compose service names don't resolve from the host", name)

		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))), 5*time.Second)
		if assert.NoError(t, err, "%s: resolved address must be reachable from the host", name) {
			_ = conn.Close()
		}
	}
	assert.Equal(t, len(ids), seen)
}
