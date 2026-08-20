package catalog_test

import (
	"testing"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/adapters/catalog"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// stub is a do-nothing Adapter used to verify Catalog wiring.
type stub struct {
	logger *zap.SugaredLogger
}

func (a *stub) Connect(adapters.ConnectionConfig) error       { return nil }
func (a *stub) Discover() ([]nodes.Node, []edges.Edge, error) { return nil, nil, nil }
func (a *stub) Health() (adapters.HealthMetrics, error)       { return nil, nil }
func (a *stub) Close() error                                  { return nil }

func stubCtor(l *zap.SugaredLogger) adapters.Adapter { return &stub{logger: l} }

func stubCatalog() adapters.Catalog {
	return adapters.NewCatalog(map[string]adapters.AdapterConstructor{"stub": stubCtor})
}

// ── Default() ────────────────────────────────────────────────────────

// Default must carry every adapter the binary ships with. A missing entry is
// the failure mode this refactor exists to prevent: discovery classifies a
// container correctly, then adapter construction fails at runtime.
func TestDefault_ContainsEveryAdapter(t *testing.T) {
	cat := catalog.Default()

	for _, connType := range []string{
		"postgres", "mysql", "mongodb", "redis", "elasticsearch", "s3", "http",
	} {
		t.Run(connType, func(t *testing.T) {
			a, err := cat.New(connType, zap.NewNop().Sugar())
			if err != nil {
				t.Fatalf("Default() has no %q adapter: %v", connType, err)
			}
			if a == nil {
				t.Fatalf("Default() constructed a nil adapter for %q", connType)
			}
		})
	}
}

func TestDefault_UnknownTypeIsAnError(t *testing.T) {
	if _, err := catalog.Default().New("not-a-datastore", nil); err == nil {
		t.Fatal("New with an unknown type returned nil error, want error")
	}
}

// Default is a constructor, not shared state: two calls must not hand back
// Catalogs that can observe each other's adapters.
func TestDefault_ReturnsIndependentCatalogs(t *testing.T) {
	first, second := catalog.Default(), catalog.Default()

	a1, err := first.New("postgres", nil)
	if err != nil {
		t.Fatalf("first Default(): %v", err)
	}
	a2, err := second.New("postgres", nil)
	if err != nil {
		t.Fatalf("second Default(): %v", err)
	}
	if a1 == a2 {
		t.Fatal("two Default() catalogs returned the same adapter instance")
	}
}

// ── adapters.Catalog behaviour ───────────────────────────────────────

func TestCatalog_NewKnownType(t *testing.T) {
	a, err := stubCatalog().New("stub", zap.NewNop().Sugar())
	if err != nil {
		t.Fatalf("New(\"stub\") returned error: %v", err)
	}
	if _, ok := a.(*stub); !ok {
		t.Fatalf("New(\"stub\") returned %T, want *stub", a)
	}
}

func TestCatalog_NilLoggerFallsBackToNop(t *testing.T) {
	a, err := stubCatalog().New("stub", nil)
	if err != nil {
		t.Fatalf("New with nil logger returned error: %v", err)
	}
	if a.(*stub).logger == nil {
		t.Fatal("constructor received a nil logger; want a no-op logger")
	}
}

// A Catalog copies the map it is built from, so a caller that keeps and mutates
// the original cannot reach inside it afterwards. This is what makes a Catalog
// safe to share across goroutines without a lock.
func TestCatalog_IsolatedFromCallerMap(t *testing.T) {
	ctors := map[string]adapters.AdapterConstructor{"stub": stubCtor}
	cat := adapters.NewCatalog(ctors)

	ctors["late"] = stubCtor
	delete(ctors, "stub")

	if _, err := cat.New("late", nil); err == nil {
		t.Error("adapter added to the caller's map after NewCatalog leaked into the Catalog")
	}
	if _, err := cat.New("stub", nil); err != nil {
		t.Errorf("adapter deleted from the caller's map disappeared from the Catalog: %v", err)
	}
}

// A zero Catalog and one built from a nil map are both legal and empty, so a
// missing Catalog degrades to "no adapters" rather than panicking.
func TestCatalog_EmptyIsUsable(t *testing.T) {
	for name, cat := range map[string]adapters.Catalog{
		"zero value": {},
		"nil map":    adapters.NewCatalog(nil),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := cat.New("stub", nil); err == nil {
				t.Error("New on an empty Catalog returned nil error, want error")
			}
		})
	}
}
