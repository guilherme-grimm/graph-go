//go:build integration

package docker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
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
