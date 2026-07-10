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

## Confirmed Roadmap

### CLI (`graph-go` command)
- Export subcommand: `graph-go export --output mermaid|dot|json`
  - `stdout` for data output (pipeable), `stderr` for all logs and status
- Validate subcommand: `graph-go validate` — parse and schema-check config, report missing fields
  - `graph-go validate --connect` — additionally test connectivity per service, no server started

### Startup Report
- On `graph-go serve`, print a structured connection summary to `stderr`:
  ```
  graph-go: discovery complete
    ✓ postgres (mydb) — 12 tables, 8 edges
    ✓ redis (cache) — 3 keyspaces
    ✗ elasticsearch (search) — connection refused: dial tcp 127.0.0.1:9200
  ```

### Kubernetes Adapter Bridging
- Classify pods by image; connect adapters to the databases running inside pods.
- Bridges Docker-style adapter discovery into the K8s-topology graph (currently the two run in parallel but don't cross-link).

### Flow Observability
- Real-time data-flow visualization — the long-arc goal: from "static topology map" to "behavior-under-load visualizer" for distributed systems debugging.
- See Stress Tests (below) as the load-generation substrate for this view.

### Message Broker Adapters
- **Phase 1:** Structural topology only — topics, partitions, consumer groups (Kafka); exchanges, queues, bindings (RabbitMQ); subjects/streams (NATS). Planned first adapter: **Kafka**.
- **Phase 2 (major milestone):** Infer producer/consumer relationships and visualize full application flow in a dedicated detailed view. Most broker APIs expose enough for reasonable inference without app-side instrumentation.

### Stress Tests
- Stress-test mode:
  - Target: the discovered services themselves (Postgres, Redis, etc.)
  - Triggered from the UI per node
  - Health metrics pushed via WebSocket at sub-second/1-second intervals during stress (bypasses normal 10s cache)
  - Real-time visualization of health degradation and recovery in the graph UI
  - Safety: opt-in per service via config flag or Docker label; confirmation step in UI

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
- `internal/adapters/http/` has no test coverage today (found 2026-07-09).
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
4. **K8s adapter bridging** — closes the gap between K8s topology and adapter-bridged discovery; required before flow observability is meaningful inside a cluster.
5. **Message broker adapters (Phase 1)** — Kafka first; structural topology.
6. **Stress test mode** — load substrate for flow observability.
7. **Message broker flow inference (Phase 2)** — deepens broker adapters into flow understanding.
8. **Flow observability** — the major milestone; consumes stress + broker flow inference + topology.
9. **Graph persistence** — usability for teams over time.
10. **Multi-region visualization** — scale-out usability.
11. **Alert configuration per node** — closes the monitoring loop.
12. **Polish phase**: hot reload, bearer token auth, HTTP adapter tests.

---

## Out of Scope (Intentionally Not Carried)

- Write-side operations (mutation of infrastructure) — graph-go is read-only.
- Multi-tenant isolation — single-context, single-user trust model.
- Agent-side instrumentation requirements — flow inference leans on what broker/orchestrator APIs already expose.