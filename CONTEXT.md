# CONTEXT.md

Domain context for graph-go. Read this before working in the codebase; it defines the vocabulary every issue, test, and proposal should use. Engineering skills consume it via `docs/agents/domain.md`.

For architectural decisions, see `docs/adr/`.

---

## What graph-go is

A CLI-first infrastructure mapper. It auto-discovers running infrastructure — from Docker containers and Kubernetes clusters — probes the databases and storage services behind them, and serves an interactive graph (nodes + edges) over a REST API and WebSocket with live health. Zero config when the Docker socket and/or in-cluster Kubernetes credentials are present; a YAML config file is an escape hatch for services unreachable via discovery.

Tagline: **"See your infrastructure. Zero Config."**

---

## Package map (bounded contexts)

| Package | Role |
|---|---|
| `cmd/app` | CLI entrypoint (cobra). Subcommands: `serve`, `scan`, `demo`, `version`, `--health-check`. Logger construction + `defer Sync()`. |
| `internal/config` | YAML config load + validate. `Config`, `ConnectionEntry`, Docker/K8s toggles. Env overrides populate zero fields only. |
| `internal/logging` | The single zap construction site (`New(level, format)`). See ADR-0001. |
| `internal/discovery` | The `Discoverer` contract + `ServiceInfo`. Backends live in subpackages. |
| `internal/discovery/docker` | Docker discoverer: classifies containers, extracts credentials, watches Docker events, honors `graphgo.*` labels. |
| `internal/discovery/kubernetes` | K8s discoverer: informers with debounced events, maps cluster snapshot to topology nodes/edges. |
| `internal/adapters` | The `Adapter` interface, factory registry, `Registry` (the aggregator), cache, singleflight, topology merge. |
| `internal/adapters/{postgres,mongodb,mysql,redis,elasticsearch,s3,http}` | One package per datastore. Self-registers via `init()`. |
| `internal/adapters/adaptertest` | The contract test suite every adapter must pass. |
| `internal/graph` | The graph model: `Graph`, `nodes.Node`, `edges.Edge`, `health.Status`. |
| `internal/server` | HTTP server, routes, CORS, WebSocket, registry/bootstrap wiring. |
| `internal/webui` | Embedded frontend (React build) served as SPA fallback. |

---

## Glossary

Use these terms exactly. Don't drift to synonyms.

- **Discoverer** — A backend that produces a `[]ServiceInfo` snapshot of what's running. Implementations: `docker`, `kubernetes`. Contract: `Discover`, `Watch`, `Close`. Lives in `internal/discovery/{name}/`.
- **Adapter** — A backend that probes a single datastore and returns nodes + edges. Implementations: `postgres`, `mongodb`, `mysql`, `redis`, `elasticsearch`, `s3`, `http`. Contract: `Connect`, `Discover`, `Health`, `Close`. Lives in `internal/adapters/{name}/`.
- **Registry** — The aggregator. Owns adapters + topology sets, runs `DiscoverAll`, caches the merged graph. `internal/adapters/registry.go`.
- **ServiceInfo** — The uniform output of a Discoverer. Two flavors: topology-producing (populates `Nodes`/`Edges` directly, e.g. K8s) and adapter-oriented (populates `Config` for adapter bridging, e.g. Docker).
- **Node** — A vertex in the graph. Has `Id`, `Type`, `Name`, `Parent`, `Metadata`, `Health`. `internal/graph/nodes/nodes.go`.
- **Edge** — A relationship between two nodes. `Id`, `Source`, `Target`, `Type`, `Label`. `internal/graph/edges/edges.go`.
- **Service node** — The top-level parent node the Registry synthesizes for each registered adapter. ID pattern: `service-<name>`. Children are re-parented under it with a `contains` edge.
- **Topology** — Pre-built nodes/edges contributed by a discoverer (not the adapter path). Injected via `Registry.SetTopology(source, nodes, edges)`. Merged into `DiscoverAll` output.
- **Health status** — One of `healthy`, `degraded`, `unhealthy`, `unknown`. `health.Status`. Adapter `Health()` returns a `HealthMetrics` map that must include a `status` key.
- **Cache** — 30s TTL, whole-graph, guarded by singleflight to prevent stampede. `InvalidateCache()` drops everything.
- **Contract tests** — `adaptertest.RunContractTests`: validates Connect/disconnect lifecycle, node/edge discovery (unique IDs, valid parent refs, correct types), and health metrics. Every adapter MUST pass.
- **Classification** — Docker discoverer maps an image string to a `ServiceType` via ordered rules (`classifier.go`). `graphgo.*` labels override classification.
- **`graphgo.*` labels** — Docker container labels: `ignore`, `type`, `dsn`, `node-type`, `name`. Escape hatch for misclassified or hidden containers.

---

## Core contracts

### Adapter (`internal/adapters/adapters.go`)

```go
type Adapter interface {
    Connect(config ConnectionConfig) error
    Discover() ([]nodes.Node, []edges.Edge, error)
    Health() (HealthMetrics, error)
    Close() error
}
```

- `ConnectionConfig` and `HealthMetrics` are both `map[string]any`.
- `Health()` must include a `"status"` key (`healthy`/`degraded`/`unhealthy`); the error return is reserved for "health cannot be determined at all" (e.g. not initialized), not for an unhealthy service.
- Self-registers: `adapters.RegisterFactory("redis", func(l *zap.SugaredLogger) Adapter { return New(l) })` in `init()`.
- The logger argument is always non-nil (falls back to no-op).

### Discoverer (`internal/discovery/discovery.go`)

```go
type Discoverer interface {
    Name() string
    Discover(ctx context.Context) ([]ServiceInfo, error)
    Watch(ctx context.Context, onChange func()) error
    Close() error
}
```

- Implementations must be safe for concurrent `Discover` + `Watch` callbacks.
- `Watch` returns when `ctx` is cancelled or an unrecoverable error occurs.

### Registry (`internal/adapters/registry.go`)

```go
type Registry interface {
    Register(name string, connType string, adapter Adapter, config ConnectionConfig) error
    Get(name string) (Adapter, bool)
    Names() []string
    DiscoverAll() (*graph.Graph, error)
    HealthAll() []health.HealthMetrics
    InvalidateCache()
    SetTopology(source string, n []nodes.Node, e []edges.Edge)
    CloseAll() error
}
```

---

## Data model

### Node (`internal/graph/nodes/nodes.go`)

```go
type Node struct {
    Id       string
    Type     string         // NodeType consts exist but the field is string
    Name     string
    Parent   string         // empty = top-level
    Metadata map[string]any
    Health   string         // health.Status as string
}
```

Node types: `postgres`, `mongodb`, `redis`, `s3`, `mysql`, `elasticsearch`, `database`, `table`, `collection`, `bucket`, `storage`, `service`, `api`, `gateway`, `auth`, `queue`, `cache`, `index`, plus K8s: `namespace`, `deployment`, `statefulset`, `daemonset`, `pod`, `k8s_service`.

### Edge (`internal/graph/edges/edges.go`)

```go
type Edge struct {
    Id, Source, Target, Type, Label string
}
```

Only `routes_to` is a typed const (`TypeRoutesTo`). `contains`, `foreign_key`, and others are string literals used directly in adapters — promoted to consts incrementally as they become shared vocabulary.

### Health (`internal/graph/health/health.go`)

`Status`: `healthy`, `degraded`, `unhealthy`, `unknown`. `HealthMetrics`: `{ NodeID, Status, Metrics, Timestamp }`.

### Graph

`{ Nodes []Node, Edges []Edge }` — JSON-serialized over the API.

---

## Node hierarchy

```
Adapter-discovered:
  service-<name>            (Registry-synthesized service node)
      └─ database/bucket    (adapter top-level, re-parented)
          └─ table/collection/prefix/index

Kubernetes-discovered (topology):
  k8s-<ns>-namespace-<ns>
      └─ k8s-<ns>-<deployment|statefulset|daemonset>-<name>
          └─ k8s-<ns>-pod-<name>
      └─ k8s-<ns>-k8s_service-<name>  ──routes_to──▶  pod
```

## ID conventions

- Service node: `service-<name>` (name from config or Docker label).
- Kubernetes: `k8s-<namespace>-<kind>-<name>`; cluster-scoped resources (namespaces) use empty namespace.
- Adapter-internal IDs are adapter-specific; Redis sanitizes `:` and `/` to `-`.

---

## Runtime flow

1. CLI (`cmd/app`) builds config + logger, calls `server.NewServer`.
2. `NewServer` → `BuildRegistry` (in `bootstrap.go`): constructs adapters from config connections + Docker-discovered services, registers each, builds discoverers, starts a `Watch()` goroutine per discoverer.
3. `Watch` callback (`buildOnChange`): topology-producing discoverers re-read their snapshot and `SetTopology`; adapter-oriented discoverers just `InvalidateCache` so the next `DiscoverAll` re-queries.
4. `DiscoverAll`: cache check (RLock) → singleflight → double-check → snapshot adapters (RLock, release before I/O) → sequential `adapter.Discover()` per adapter → synthesize service nodes + re-parent children → merge topology → cache + return shallow copy.
5. HTTP routes: `GET /api/graph` (full graph), `GET /api/node/{id}`, `GET /api/health` (adapter health), `WS /websocket` (live updates), `GET /health` (liveness), SPA fallback for the embedded UI.
6. WebSocket: emits `health_update` (every node, every 5s) and `graph_update` (when the node-ID set changes; payload empty → client re-fetches `/api/graph`). Messages wrapped as `{ "type", "payload" }`; no timestamp field.
7. Graceful shutdown: signal → stop HTTP server → `cleanup()` (closes discoverers + `CloseAll` adapters).

---

## Invariants

- A failing adapter becomes an `unhealthy` service node with its error in `Metadata["error"]` — it never fails the whole `DiscoverAll`.
- `stdout` is reserved for machine-readable data (`scan` JSON output); all logging goes to `stderr`. (ADR-0001)
- The logger is threaded via constructor, never read from a global. (ADR-0001)
- Env overrides only populate zero-valued config fields; explicit YAML always wins.
- `shallowCopyGraph` copies the `Graph` struct but `Nodes`/`Edges` slices share backing arrays with the cached graph — callers must not mutate returned slices in place. (Current callers serialize to JSON immediately.)
- HTTP adapters manage their own top-level service node (no registry-synthesized parent); all other adapter types get one.
- Every adapter must pass `adaptertest.RunContractTests` and have integration tests with real testcontainers (build tag `integration`). No mocks for datastore adapters.

---

## Key decisions

- **ADR-0001** — zap `SugaredLogger` over stdlib `log/slog`. Logger constructed once in `internal/logging`, threaded via constructor, no globals.
- **Self-registration via `init()`** — adapters call `RegisterFactory` in `init()`; `server.go` lists them as blank imports. Adding an adapter = new package + one blank import + node type + frontend type/icon.
- **Two discoverer flavors** — topology-producing (K8s, emits nodes/edges directly) vs adapter-oriented (Docker, emits `Config` for the adapter bridge). Keeps the `Discoverer` contract uniform while letting each backend contribute at the right level.
- **Sequential adapter discovery** — `DiscoverAll` probes adapters one at a time. Acceptable at current adapter counts; the latency floor for many remote datastores.

---

## Adding to the system

- **New adapter**: `internal/adapters/{name}/` → implement `Adapter` → `init()` self-register → integration test with `adaptertest.RunContractTests` → blank import in `server.go` → node type in `nodes.go` → frontend type + icon.
- **New discoverer**: `internal/discovery/{name}/` → implement `Discoverer` → `build{Name}Discovery()` in `server.go` → integration tests with real infrastructure (no mocks).
