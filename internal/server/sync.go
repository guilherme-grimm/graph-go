package server

import (
	"reflect"
	"sync"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/discovery"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/redact"
)

// registerConcurrency bounds parallel adapter Connect calls during a sync.
// Connect is a network round trip, so serialising it made a refresh with a
// few hundred containers take as long as the sum of their timeouts.
const registerConcurrency = 8

// knownService is what a sync registered for one adapter name.
type knownService struct {
	connType string
	source   string
	config   adapters.ConnectionConfig
}

// serviceSync pushes discovered services into the registry and remembers what
// it registered, so later refreshes skip unchanged services instead of
// reconnecting them and prune services whose container went away.
type serviceSync struct {
	reg    adapters.Registry
	cat    adapters.Catalog
	yaml   []discovery.YAMLEntry
	logger *zap.SugaredLogger

	mu    sync.Mutex
	known map[string]knownService
}

func newServiceSync(reg adapters.Registry, cat adapters.Catalog, yaml []discovery.YAMLEntry, logger *zap.SugaredLogger) *serviceSync {
	return &serviceSync{reg: reg, cat: cat, yaml: yaml, logger: logger, known: make(map[string]knownService)}
}

// syncResult is what one apply attempted: every adapter it tried to register
// (unchanged services are skipped, so they are absent) and every topology
// source it replaced.
type syncResult struct {
	adapters []adapterResult
	topology []topologyResult
}

type adapterResult struct {
	name, connType string
	err            error
}

type topologyResult struct {
	source       string
	nodes, edges int
}

// apply splits ServiceInfo entries into topology (ones carrying pre-built
// Nodes, like Kubernetes resources) and adapter-bound ones, then pushes each
// into the registry appropriately. Topology entries are grouped by Source so
// a later refresh can replace them atomically.
//
// YAML entries are merged in on every call so a refresh never reverts a YAML
// override to the discovered config. When pruneSource is non-empty, adapters
// previously registered from that source but absent from services are
// unregistered.
func (s *serviceSync) apply(services []discovery.ServiceInfo, pruneSource string) syncResult {
	services = discovery.MergeWithYAML(services, s.yaml)

	type bucket struct {
		nodes []nodes.Node
		edges []edges.Edge
	}
	topologyBySource := make(map[string]*bucket)
	seen := make(map[string]bool, len(services))

	s.mu.Lock()
	defer s.mu.Unlock()

	// Workers report outcomes here; s.known is only touched by this
	// goroutine, so the loop can read it while workers run.
	type registered struct {
		name string
		svc  knownService
		err  error
	}
	var (
		res     syncResult
		wg      sync.WaitGroup
		sem     = make(chan struct{}, registerConcurrency)
		results = make(chan registered, len(services))
	)
	for _, svc := range services {
		if len(svc.Nodes) > 0 || len(svc.Edges) > 0 {
			b, ok := topologyBySource[svc.Source]
			if !ok {
				b = &bucket{}
				topologyBySource[svc.Source] = b
			}
			b.nodes = append(b.nodes, svc.Nodes...)
			b.edges = append(b.edges, svc.Edges...)
			continue
		}

		seen[svc.Name] = true
		if k, ok := s.known[svc.Name]; ok && k.connType == svc.Type && reflect.DeepEqual(k.config, svc.Config) {
			continue
		}

		adapter, err := s.cat.New(svc.Type, s.logger)
		if err != nil {
			s.logger.Warnw("skipping service: unknown adapter type", "service", svc.Name, "type", svc.Type, "err", err)
			res.adapters = append(res.adapters, adapterResult{svc.Name, svc.Type, err})
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(svc discovery.ServiceInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			k := knownService{connType: svc.Type, source: svc.Source, config: svc.Config}
			if err := s.reg.Register(svc.Name, svc.Type, adapter, svc.Config); err != nil {
				s.logger.Warnw("adapter register failed", "service", svc.Name, "type", svc.Type, "err", redact.String(err.Error()))
				results <- registered{svc.Name, k, err}
				return
			}
			s.logger.Infow("adapter registered", "service", svc.Name, "type", svc.Type)
			results <- registered{svc.Name, k, nil}
		}(svc)
	}
	wg.Wait()
	close(results)
	for r := range results {
		res.adapters = append(res.adapters, adapterResult{r.name, r.svc.connType, r.err})
		if r.err == nil {
			s.known[r.name] = r.svc
		}
	}

	if pruneSource != "" {
		for name, k := range s.known {
			if seen[name] || k.source != pruneSource {
				continue
			}
			if err := s.reg.Unregister(name); err != nil {
				s.logger.Warnw("adapter unregister failed", "service", name, "err", err)
			}
			delete(s.known, name)
			s.logger.Infow("adapter removed", "service", name, "source", pruneSource)
		}
	}

	for source, b := range topologyBySource {
		s.reg.SetTopology(source, b.nodes, b.edges)
		s.logger.Infow("topology applied", "source", source, "nodes", len(b.nodes), "edges", len(b.edges))
		res.topology = append(res.topology, topologyResult{source, len(b.nodes), len(b.edges)})
	}
	return res
}
