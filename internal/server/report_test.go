package server

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/discovery"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// scriptedAdapter connects and discovers as told, so one real registry can
// hold a mix of working and failing adapters.
type scriptedAdapter struct {
	connectErr  error
	discoverErr error
	nodes       []nodes.Node
}

func (a *scriptedAdapter) Connect(adapters.ConnectionConfig) error { return a.connectErr }
func (a *scriptedAdapter) Discover() ([]nodes.Node, []edges.Edge, error) {
	return a.nodes, nil, a.discoverErr
}
func (a *scriptedAdapter) Health() (adapters.HealthMetrics, error) { return nil, nil }
func (a *scriptedAdapter) Close() error                            { return nil }

func scriptedCatalog() adapters.Catalog {
	return adapters.NewCatalog(map[string]adapters.AdapterConstructor{
		"postgres": func(*zap.SugaredLogger) adapters.Adapter {
			return &scriptedAdapter{nodes: []nodes.Node{
				{Id: "pg-db", Type: "database", Name: "orders"},
				{Id: "pg-t1", Type: "table", Name: "users", Parent: "pg-db"},
				{Id: "pg-t2", Type: "table", Name: "orders", Parent: "pg-db"},
			}}
		},
		"redis": func(*zap.SugaredLogger) adapters.Adapter {
			return &scriptedAdapter{connectErr: errors.New("dial tcp 10.0.0.4:6379: connect: connection refused")}
		},
		"mongodb": func(*zap.SugaredLogger) adapters.Adapter {
			return &scriptedAdapter{discoverErr: errors.New("list databases: unauthorized")}
		},
		"s3": func(*zap.SugaredLogger) adapters.Adapter { return &scriptedAdapter{} },
	})
}

func TestStartupReportMixedRegistry(t *testing.T) {
	reg := adapters.NewRegistry(testLogger)
	ss := newServiceSync(reg, scriptedCatalog(), nil, testLogger)

	res := ss.apply([]discovery.ServiceInfo{
		{Name: "orders-db", Type: "postgres", Source: "docker"},
		{Name: "cache", Type: "redis", Source: "docker"},
		{Name: "docs", Type: "mongodb", Source: "docker"},
		{Name: "assets", Type: "s3", Source: "docker"},
		{Name: "queue", Type: "kafka", Source: "docker"},
		{Name: "ns", Source: "kubernetes", Nodes: []nodes.Node{{Id: "n1"}, {Id: "n2"}}, Edges: []edges.Edge{{Id: "e1"}}},
	}, "")

	g, err := reg.DiscoverAll()
	require.NoError(t, err, "DiscoverAll")

	var out strings.Builder
	require.NoError(t, writeStartupReport(&out, startupReport(res, g)), "writeStartupReport")
	report := out.String()

	lines := strings.Split(strings.TrimRight(report, "\n"), "\n")
	want := []struct {
		line  int
		parts []string
	}{
		{0, []string{"graph-go: 2 of 5 adapters connected"}},
		{1, []string{"✓ assets", "s3", "connected"}},
		{2, []string{"✗ cache", "redis", "connection refused"}},
		{3, []string{"✗ docs", "mongodb", "list databases: unauthorized"}},
		{4, []string{"✓ orders-db", "postgres", "2 tables, 1 database"}},
		{5, []string{"✗ queue", "kafka", "kafka"}},
		{6, []string{"✓ kubernetes", "topology", "2 nodes, 1 edge"}},
	}
	require.Len(t, lines, len(want), report)
	assert.NotContains(t, lines[2], "registry:", "registry prefix should be trimmed")
	for _, w := range want {
		for _, p := range w.parts {
			assert.Contains(t, lines[w.line], p, "line %d", w.line)
		}
	}

	// Columns line up: every adapter row has its type at the same offset.
	assert.Equal(t, strings.Index(lines[1], "s3"), strings.Index(lines[4], "postgres"), "type column misaligned:\n%s", report)
}

func TestStartupReportEmptyIsQuiet(t *testing.T) {
	var out strings.Builder
	require.NoError(t, writeStartupReport(&out, nil))
	assert.Empty(t, out.String())
}

func TestPlural(t *testing.T) {
	tests := []struct {
		n    int
		noun string
		want string
	}{
		{1, "table", "1 table"},
		{3, "table", "3 tables"},
		{2, "index", "2 indexes"},
		{0, "bucket", "0 buckets"},
		{4, "k8s service", "4 k8s services"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, plural(tt.n, tt.noun), "plural(%d, %q)", tt.n, tt.noun)
	}
}

// A connect error that echoes a connection string must not leak through the
// startup report or the registration-failure log.
func TestStartupReportAndLogsRedactConnectErrors(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()
	cat := adapters.NewCatalog(map[string]adapters.AdapterConstructor{
		"mysql": func(*zap.SugaredLogger) adapters.Adapter {
			return &scriptedAdapter{connectErr: errors.New("dial mysql://root:hunter2@db:3306/shop: refused")}
		},
	})
	reg := adapters.NewRegistry(logger)
	res := newServiceSync(reg, cat, nil, logger).apply([]discovery.ServiceInfo{
		{Name: "shop-db", Type: "mysql", Source: "docker"},
	}, "")
	g, err := reg.DiscoverAll()
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, writeStartupReport(&out, startupReport(res, g)))
	assert.NotContains(t, out.String(), "hunter2")
	assert.Contains(t, out.String(), "mysql://root:***@db:3306/shop")

	for _, e := range logs.All() {
		// ContextMap renders error fields as text, so a raw error is caught too.
		assert.NotContains(t, fmt.Sprint(e.ContextMap()), "hunter2", "log %q", e.Message)
	}
	require.NotZero(t, logs.FilterMessage("adapter register failed").Len())
}
