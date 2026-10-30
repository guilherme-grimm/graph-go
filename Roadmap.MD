# graph-go: Engineering Roadmap

> **Scope split.** This file is the engineering plan: sequencing, deferred
> polish, and the "why" behind each workstream. The README's *Roadmap* section
> is the user-facing capability status (what's shipped / what's next, ticked
> boxes). They overlap on capabilities but not on intent — check both before
> planning work.

## Done

### Adapters
- PostgreSQL, MongoDB, MySQL, Redis, Elasticsearch, S3 (and S3-compatible stores), HTTP adapters.

### Integration & Contract Tests
- Integration tests using `testcontainers-go` for all container-backed adapters (PostgreSQL, MongoDB, MySQL, Redis, Elasticsearch, S3 via RustFS). The HTTP adapter runs the contract suite against an `httptest.Server` ([#13](https://github.com/guilherme-grimm/graph-go/issues/13)).
- Shared contract test suite (`adaptertest.RunContractTests`) — validates Adapter interface compliance.
- Build tag `//go:build integration` keeps tests out of `go test ./...`; CI runs them separately.
- Contract tests are a documented requirement for all new adapters.

### CLI
- `graph-go serve` (default), `graph-go scan`, `graph-go validate`, `graph-go demo`, `graph-go version`, `graph-go --health-check`.
- `scan --format json|mermaid|markdown|dot` (`--health`, `--pretty`); `graph-go export` is an alias. Diagrams nest namespaces/databases as subgraphs and are deterministic ([#19](https://github.com/guilherme-grimm/graph-go/issues/19)).
- `validate` reports every config problem: missing fields, unknown types, unknown keys `serve` would ignore, extra YAML documents ([#17](https://github.com/guilherme-grimm/graph-go/issues/17)).
- Startup report: once the first discovery finishes, `serve` prints a ✓/✗ line per adapter to stderr with what it found or why it failed ([#16](https://github.com/guilherme-grimm/graph-go/issues/16)).
- Global flags: `--config`, `--log-level`, `--log-format`.
- Single self-contained binary with the SPA bundle embedded (`webui` via `//go:embed`).

### Structured Logging
- **`zap.SugaredLogger`** — see [ADR-0001](docs/adr/0001-zap-over-slog.md).
- `internal/logging.New(level, format)` constructed in `cmd/app` and threaded via constructors.
- `--log-level` (debug/info/warn/error), `--log-format` (console/json).
- `stdout` reserved for machine-readable data; all logs go to **stderr**.

### Orchestrator Support
- **Kubernetes** (Phase 1) — informer-based real-time watching of Namespaces, Deployments, StatefulSets, DaemonSets, Pods, Services. Debounced event handling (2s window). In-cluster → kubeconfig fallback; `New` returns `(nil, nil)` if no cluster, so discovery silently no-ops; a configured cluster that doesn't answer `/version` within 5s is reported as unavailable instead of hanging discovery.
- Docker stays the core discovery source; Docker events drive live topology changes.
- Scope: read-only discovery against the current user's context.

### Explicit Adapter Registration
- `init()` self-registration replaced by an explicit `Catalog` built at the composition root ([#14](https://github.com/guilherme-grimm/graph-go/issues/14), [ADR-0002](docs/adr/0002-explicit-adapter-registration.md)).

### Large-cluster performance (v0.4.0-rc.1)
- WebSocket sends one `health_batch` diff per tick instead of a frame per node; stale Docker services are pruned (`Registry.Unregister`).
- UI: namespace collapse, name/namespace filters, auto-switch to swimlane above 300 nodes, viewport virtualization, filter state in the URL.
- `hack/scale/` harness (kind + KWOK, Docker container generator, `measure.mjs`). At 5k pods: first render 8.3s → 0.43s, heap 1.1GB → 16MB.
- Next: server-side namespace filtering (rc.2).

### Delivery
- Single Docker image on `ghcr.io/guilherme-grimm/graph-go`; cross-platform binaries via GoReleaser on tag pushes.

---

## V1 Milestones

V1 is defined by three milestones (decided 2026-07-09), each a shippable unit.
Everything else in this file either enables them or comes after them.

### M1 — Data Flow
From "static topology map" to "behavior-under-load visualizer": see where
requests enter, which backends they hit, and how load moves through the graph.
Web view first; TUI is an optional track below.

- **Flow source: adapter-side stats polling.** graph-go already holds live
  connections to every backend; sample their native counters
  (`pg_stat_database`, Redis `INFO` deltas, Mongo `serverStatus`, …) for
  per-edge throughput with zero app-side instrumentation. No OTel/eBPF in v1.
- **Entry-point heuristic:** scan what's exposed (published ports, K8s
  Services/ingresses — discovery already sees most of it) so flow is observed
  at both boundaries of the graph — edges in, backends out — and the middle is
  inferred rather than instrumented.
- **Acceptance criterion — metrics cross-check:** numbers displayed must match
  what a Grafana/Prometheus stack reports for the same window. This doubles as
  the optional Grafana integration.
- *(opt)* Proper TUI/CLI flow view — current CLI ergonomics feel lacking.

### M2 — Stress Testing
Pair load generation with M1's flow view: fire load at a node, watch
degradation and recovery live, with Grafana alongside for full visibility.

- Load driver: k6 or vegeta — leaning **vegeta** (Go library, embeds in the
  single self-contained binary; design a pluggable `LoadDriver` so k6-exec
  support can come later). Final call at implementation time.
- Target: the discovered services themselves (Postgres, Redis, etc.),
  triggered from the UI per node.
- Health metrics pushed via WebSocket at sub-second/1-second intervals during
  stress (bypasses normal 10s cache); real-time visualization of degradation
  and recovery in the graph UI.
- Safety: opt-in per service via config flag or Docker label; confirmation
  step in UI.
- **Resource scaling/descaling actuation is out of v1** — graph-go stays
  read-only (see Out of Scope). Instead, show per-node resource saturation
  (CPU/mem) next to per-edge flow so "is it a resource problem?" is answerable
  visually.

### M3 — MCP Server
Deterministic infra visibility for agents: one tool call instead of an agent
burning tokens grepping compose files and guessing.

- Tools grow with the milestones: `get_topology` (available today via `scan
  --format=json`), then `get_flow_snapshot` (M1), `run_stress_test` (M2).
- A topology-only MCP server can ship **ahead of** M1/M2 as an early launch
  moment — the plumbing is thin on top of `scan`.

---

## Enablers (Confirmed, pre- or intra-milestone)

### `graph-go validate --connect`
- Additionally test connectivity per service, no server started ([#18](https://github.com/guilherme-grimm/graph-go/issues/18)).

### Kubernetes Adapter Bridging
- Classify pods by image; connect adapters to the databases running inside pods.
- Bridges Docker-style adapter discovery into the K8s-topology graph (currently the two run in parallel but don't cross-link).
- Required before M1 flow is meaningful inside a cluster.

---

## Post-V1 (Confirmed direction, sequenced after the milestones)

### Message Broker Adapters
- **Decision 2026-07-09: broker adapters come after M1 visualization** — event
  tracking is far more useful once flow can be seen.
- **Phase 1:** Structural topology only — topics, partitions, consumer groups (Kafka); exchanges, queues, bindings (RabbitMQ); subjects/streams (NATS). Planned first adapter: **Kafka**.
- **Phase 2:** Infer producer/consumer relationships and feed them into the M1 flow view. Most broker APIs expose enough for reasonable inference without app-side instrumentation.

### Graph Persistence
- Save/load named graph views (snapshots of the discovered topology + saved layout state) so teams can keep a reference of a moment-in-time architecture.

### Multi-region Visualization
- Render multiple clusters/regions in one view without conflating namespaces.

### Alert Configuration Per Node
- User-defined alert rules per node (status threshold, latency), surfaced in the UI and optionally pushed out of band.

---

## Deferred (Polish Phase)

### Hot Reload
- Watch `config.yaml` for changes via `fsnotify`.
- Re-merge YAML config + Docker discovery and re-register adapters without restart.
- Mirrors the existing Docker event-watcher pattern in `internal/discovery/docker/events.go`.

### Bearer Token Auth
- Optional static token via `GRAPHGO_TOKEN` env var.
- If set: all `/api/*` and `/websocket` routes require `Authorization: Bearer <token>`.
- If unset: open access (current behavior, safe for local dev).
- No sessions or OAuth — one env var for shared environments.

---

## Implementation Priority (Recommended)

Ordered by dependency and DX impact — earlier items unblock later ones.

1. **MCP server over topology (M3, first slice)** — thin on top of `scan --format=json`; independently launchable, grows tools as M1/M2 land.
2. **K8s adapter bridging** — closes the gap between K8s topology and adapter-bridged discovery; required before M1 is meaningful inside a cluster.
3. **M1 — Data flow** — adapter-stats polling + entry-point heuristic; Grafana cross-check is the acceptance bar.
4. **M2 — Stress testing** — load driver + live health push, paired with the M1 view.
5. **Message broker adapters (Phase 1)** — after visualization (decision 2026-07-09); Kafka first, structural topology.
6. **Message broker flow inference (Phase 2)** — feeds the M1 flow view.
7. **Graph persistence** — usability for teams over time.
8. **Multi-region visualization** — scale-out usability.
9. **Alert configuration per node** — closes the monitoring loop.
10. **Polish phase**: hot reload, bearer token auth.

---

## Out of Scope (Intentionally Not Carried)

- Write-side operations (mutation of infrastructure) — graph-go is read-only.
- Multi-tenant isolation — single-context, single-user trust model.
- Agent-side instrumentation requirements — flow inference leans on what broker/orchestrator APIs already expose.