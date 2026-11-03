package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
)

func names(svcs []DiscoveredService) []string {
	out := make([]string, len(svcs))
	for i, s := range svcs {
		out[i] = s.Name
	}
	return out
}

func compose(project, service, container string) DiscoveredService {
	return DiscoveredService{Name: service, Project: project, Service: service, ContainerName: container}
}

func plain(container string) DiscoveredService {
	return DiscoveredService{Name: container, ContainerName: container}
}

func TestDisambiguateNames(t *testing.T) {
	tests := []struct {
		name string
		in   []DiscoveredService
		want []string
	}{
		{
			name: "unique names stay bare",
			in:   []DiscoveredService{compose("trader", "db", "trader-db-1"), compose("trader", "app", "trader-app-1"), plain("redis")},
			want: []string{"db", "app", "redis"},
		},
		{
			// Regression for #43: two Compose projects with a `db` service
			// registered under the same adapter name.
			name: "same service in two projects is project-qualified",
			in:   []DiscoveredService{compose("trader", "db", "trader-db-1"), compose("basics", "db", "postgres_db"), compose("trader", "app", "trader-app-1")},
			want: []string{"trader.db", "basics.db", "app"},
		},
		{
			name: "compose service colliding with a plain container",
			in:   []DiscoveredService{compose("trader", "db", "trader-db-1"), plain("db")},
			want: []string{"trader.db", "db"},
		},
		{
			name: "container-name fallback that collides is resolved too",
			in:   []DiscoveredService{compose("trader", "db", "trader-db-1"), compose("trader", "db", "trader-db-2"), compose("other", "trader-db-1", "other-x-1")},
			want: []string{"trader-db-1", "trader-db-2", "other-x-1"},
		},
		{
			name: "scaled replicas fall back to container names",
			in:   []DiscoveredService{compose("trader", "db", "trader-db-1"), compose("trader", "db", "trader-db-2"), compose("basics", "db", "basics-db-1")},
			want: []string{"trader-db-1", "trader-db-2", "basics.db"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disambiguateNames(tt.in)
			assert.Equal(t, tt.want, names(tt.in))
		})
	}
}

func TestDisambiguateNames_KeepsHTTPConfigNameInStep(t *testing.T) {
	// The HTTP adapter uses config["name"] as its node ID, so it must follow
	// the rename or two `api` services still emit the same node.
	a := compose("trader", "api", "trader-api-1")
	a.Config = adapters.ConnectionConfig{"name": "api"}
	b := compose("basics", "api", "basics-api-1")
	b.Config = adapters.ConnectionConfig{"name": "custom"}
	b.ConfigNameFromLabel = true
	svcs := []DiscoveredService{a, b}

	disambiguateNames(svcs)

	assert.Equal(t, []string{"trader.api", "basics.api"}, names(svcs))
	assert.Equal(t, "trader.api", svcs[0].Config["name"])
	assert.Equal(t, "custom", svcs[1].Config["name"])
}

func TestDisambiguateNames_KeepsLabelNameEqualToService(t *testing.T) {
	// A graphgo.name label that happens to equal the service name is still
	// the user's choice and must survive the rename.
	a := compose("trader", "db", "trader-db-1")
	a.Config = adapters.ConnectionConfig{"name": "db"}
	a.ConfigNameFromLabel = true
	b := compose("basics", "db", "basics-db-1")
	b.Config = adapters.ConnectionConfig{"name": "db"}
	svcs := []DiscoveredService{a, b}

	disambiguateNames(svcs)

	assert.Equal(t, []string{"trader.db", "basics.db"}, names(svcs))
	assert.Equal(t, "db", svcs[0].Config["name"])
	assert.Equal(t, "basics.db", svcs[1].Config["name"])
}

func TestNameFromLabel(t *testing.T) {
	assert.True(t, nameFromLabel(map[string]string{LabelName: "db"}))
	assert.True(t, nameFromLabel(map[string]string{LegacyLabelName: "db"}))
	assert.False(t, nameFromLabel(map[string]string{LabelName: ""}))
	assert.False(t, nameFromLabel(map[string]string{}))
}

func TestDisambiguateNames_OrderIndependent(t *testing.T) {
	a := []DiscoveredService{compose("trader", "db", "trader-db-1"), compose("basics", "db", "postgres_db"), plain("redis")}
	b := []DiscoveredService{plain("redis"), compose("basics", "db", "postgres_db"), compose("trader", "db", "trader-db-1")}

	disambiguateNames(a)
	disambiguateNames(b)

	assert.ElementsMatch(t, names(a), names(b))
}
