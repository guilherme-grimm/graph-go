# graph-go: Engineering Roadmap

> **Scope split.** This file is the engineering plan: sequencing, deferred
> polish, and the "why" behind each workstream. The README's *Roadmap* section
> is the user-facing capability status (what's shipped / what's next, ticked
> boxes). They overlap on capabilities but not on intent — check both before
> planning work.

## Done

### Adapters
- PostgreSQL, MongoDB, MySQL, Redis, Elasticsearch, S3/MinIO, HTTP adapters.

### Integration & Contract Tests
- Integration tests using `testcontainers-go` for all container-backed adapters (PostgreSQL, MongoDB, MySQL, Redis, Elasticsearch, S3/MinIO). The HTTP adapter has no tests yet — see Deferred.
- Shared contract test suite (`adaptertest.RunContractTests`) — validates Adapter interface compliance.
- Build tag `//go:build integration` keeps tests out of `go test ./...`; CI runs them separately.
- Contract tests are a documented requirement for all new adapters.

### CLI
- `graph-go serve` (default), `graph-go scan` (JSON output, `--health`, `--pretty`), `graph-go demo`, `graph-go version`, `graph-go --health-check`.
- Global flags: `--config`, `--log-level`, `--log-format`.
- Single self-contained binary with the SPA bundle embedded (`webui` via `//go:embed`).

### Structured Logging
- **`zap.SugaredLogger`** — see [ADR-0001](docs/adr/0001-zap-over-slog.md).
- `internal/logging.New(level, format)` constructed in `cmd/app` and threaded via constructors.
- `--log-level` (debug/info/warn/error), `--log-format` (console/json).
- `stdout` reserved for machine-readable data; all logs go to **stderr**.

### Orchestrator Support
- **Kubernetes** (Phase 1) — informer-based real-time watching of Namespaces, Deployments, StatefulSets, DaemonSets, Pods, Services. Debounced event handling (2s window). In-cluster → kubeconfig fallback; `New` returns `(nil, nil)` if no cluster, so discovery silently no-ops.
- Docker stays the core discovery source; Docker events drive live topology changes.
- Scope: read-only discovery against the current user's context.

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

### Startup Report
- On `graph-go serve`, print a structured connection summary to `stderr`:
  ```
  graph-go: discovery complete
    ✓ postgres (mydb) — 12 tables, 8 edges
    ✓ redis (cache) — 3 keyspaces
    ✗ elasticsearch (search) — connection refused: dial tcp 127.0.0.1:9200
  ```

### CLI (`graph-go` command)
- Export subcommand: `graph-go export --output mermaid|dot|json`
  - `stdout` for data output (pipeable), `stderr` for all logs and status
- Validate subcommand: `graph-go validate` — parse and schema-check config, report missing fields
  - `graph-go validate --connect` — additionally test connectivity per service, no server started

### Kubernetes Adapter Bridging
- Classify pods by image; connect adapters to the databases running inside pods.
- Bridges Docker-style adapter discovery into the K8s-topology graph (currently the two run in parallel but don't cross-link).
- Required before M1 flow is meaningful inside a cluster.

### Explicit Adapter Registration (no `init()`)
- Replace the 7 `func init()` self-registrations + blank imports with explicit
  registration at the composition root ([#14](https://github.com/guilherme-grimm/graph-go/issues/14)).
- Also gives M1/M3 a clean seam for wiring adapters programmatically.

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

### HTTP Adapter Tests
- `internal/adapters/http/` has no test coverage today ([#13](https://github.com/guilherme-grimm/graph-go/issues/13)).
- Run the shared contract suite (`adaptertest.RunContractTests`) against an `httptest.Server`; no container needed.

### Bearer Token Auth
- Optional static token via `GRAPHGO_TOKEN` env var.
- If set: all `/api/*` and `/websocket` routes require `Authorization: Bearer <token>`.
- If unset: open access (current behavior, safe for local dev).
- No sessions or OAuth — one env var for shared environments.

---

## Implementation Priority (Recommended)

Ordered by dependency and DX impact — earlier items unblock later ones.

1. **Startup report** — immediately improves DX, low effort; builds on the zap logger already threaded through.
2. **`graph-go validate`** — unblocks safer config iteration; shares code with startup report's per-adapter probe.
3. **CLI export (`graph-go export`)** — unlocks CI/docs use cases; `scan` JSON already done, add mermaid + dot.
4. **MCP server over topology (M3, first slice)** — thin on top of `scan --format=json`; independently launchable, grows tools as M1/M2 land.
5. **Explicit adapter registration (#14)** — kills `init()` side effects; clean seam before flow work touches the adapters.
6. **K8s adapter bridging** — closes the gap between K8s topology and adapter-bridged discovery; required before M1 is meaningful inside a cluster.
7. **M1 — Data flow** — adapter-stats polling + entry-point heuristic; Grafana cross-check is the acceptance bar.
8. **M2 — Stress testing** — load driver + live health push, paired with the M1 view.
9. **Message broker adapters (Phase 1)** — after visualization (decision 2026-07-09); Kafka first, structural topology.
10. **Message broker flow inference (Phase 2)** — feeds the M1 flow view.
11. **Graph persistence** — usability for teams over time.
12. **Multi-region visualization** — scale-out usability.
13. **Alert configuration per node** — closes the monitoring loop.
14. **Polish phase**: hot reload, bearer token auth, HTTP adapter tests (#13).

---

## Out of Scope (Intentionally Not Carried)

- Write-side operations (mutation of infrastructure) — graph-go is read-only.
- Multi-tenant isolation — single-context, single-user trust model.
- Agent-side instrumentation requirements — flow inference leans on what broker/orchestrator APIs already expose.