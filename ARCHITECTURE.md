<!-- markdownlint-disable -->
# Graph-Info: Architecture Reference

## The Idea

A tool that visualizes infrastructure as an interactive graph. Connect to databases, storage, services - see them as nodes in a network. Click around, explore, draw your own connections between things.

Like those old tech movie interfaces, but for your actual systems.

## Key Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| What depth? | Container-level | Tables, collections, buckets - not individual records. Structure, not data. |
| What relationships? | Hierarchy + user-defined | Auto-discover parent-child, let users draw their own links. |
| Updates? | Hybrid | Snapshot the structure, stream health/metrics live. |
| Platform? | CLI + Web UI | Single binary serves localhost. No deployment complexity. |
| Stack? | Go + TypeScript/React | Go for the backend (single binary, fast). React Flow for the graph. |
| Config? | YAML | Human-readable, version-controllable. |
| Real-time? | WebSockets | Bi-directional, push health updates to UI. |
| Connectors? | Built-in adapters | Start with what you use. Design for extensibility, add plugin system when needed. |

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                        Go Binary                             │
│                                                              │
│   ┌─────────────┐    ┌─────────────────────────────────┐     │
│   │   Config    │───▶│        Adapter Registry         │     │
│   │   (YAML)    │    │                                 │     │
│   └─────────────┘    │  ┌─────────┐ ┌─────────┐        │     │
│                      │  │Postgres │ │ MongoDB │ ...    │     │
│                      │  └─────────┘ └─────────┘        │     │
│                      └─────────────────────────────────┘     │
│                                     │                        │
│                                     ▼                        │
│                            ┌───────────────┐                 │
│                            │  Graph Model  │                 │
│                            │ (nodes/edges) │                 │
│                            └───────────────┘                 │
│                                     │                        │
│              ┌──────────────────────┼──────────────────────┐ │
│              ▼                      ▼                      ▼ │
│      ┌─────────────┐       ┌──────────────┐      ┌──────────┐│
│      │ HTTP Server │       │  WebSocket   │      │ Embedded ││
│      │  (REST API) │       │   Server     │      │ UI Files ││
│      └─────────────┘       └──────────────┘      └──────────┘│
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼ localhost:3000
┌──────────────────────────────────────────────────────────────┐
│                     React + React Flow                       │
│                                                              │
│   ┌─────────────────────────────────────────────────────┐    │
│   │                   Graph Canvas                      │    │
│   │                                                     │    │
│   │      [Postgres]─────────[MongoDB]                   │    │
│   │          │                   │                      │    │
│   │      [users]  [posts]    [logs]                     │    │
│   │                                                     │    │
│   └─────────────────────────────────────────────────────┘    │
│                                                              │
│   ┌──────────────────┐  ┌───────────────────────────────┐    │
│   │  Node Inspector  │  │     Connection Panel          │    │
│   │  (selected node) │  │     (add/edit sources)        │    │
│   └──────────────────┘  └───────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

## Core Interface: Adapter

Every data source implements this contract:

```go
type Adapter interface {
    // Connect establishes connection to the service
    Connect(config ConnectionConfig) error

    // Discover returns the graph nodes for this source
    // (the service itself + its children: tables, collections, buckets, etc.)
    Discover() ([]Node, []Edge, error)

    // Health returns current metrics for the service
    Health() (HealthMetrics, error)

    // Close cleans up the connection
    Close() error
}
```

## Graph Model

```
Node:
  - id: unique identifier
  - type: "postgres" | "mongodb" | "redis" | "s3" | "table" | "collection" | "bucket" | ...
  - name: display name
  - parent: parent node id (or null for root)
  - metadata: type-specific info (columns, indexes, size, etc.)

Edge:
  - id: unique identifier
  - source: node id
  - target: node id
  - type: "contains" | "references" | "user-defined"
  - label: optional description
```

## Config Shape

```yaml
connections:
  - name: "my-db"
    type: postgres
    host: localhost
    port: 5432
    # ... connection-specific fields

links:  # user-defined relationships
  - from: "my-db.users"
    to: "storage.avatars"
    label: "profile images"
```

## Initial Adapters

| Type | Discovers | Health Metrics |
|------|-----------|----------------|
| **PostgreSQL** | schemas, tables, columns, foreign keys, indexes | connections, db size, table sizes |
| **MongoDB** | databases, collections, indexes | connections, ops/sec, storage |
| **Redis** | databases, key patterns | memory, clients, ops/sec |
| **S3/MinIO** | buckets, prefixes | bucket sizes, object counts |

## API Shape

```
REST:
  GET  /api/graph         → full graph snapshot
  GET  /api/node/:id      → node details
  POST /api/links         → create user link

WebSocket:
  /api/ws → server pushes health updates
           { type: "health", node: "my-db", metrics: {...} }
```

## Open Questions (figure out as you go)

- How to handle connection failures gracefully in the UI?
    - Use a default state and shows in the interface that the node is not responding as expected
- Should user-defined links persist in the YAML or separate storage?
    - For now, I'll be defaulting for a sqlite local database. Simple and alterable
- What's the right polling interval for health checks?
    - User defined. Default to1s
- How to visualize large graphs without becoming unusable?
    - Lazy loading and priority queues.
- Should adapters run discovery in parallel or sequentially?
    - We shall see.

---

*Build it your way. This is just a map, not a prescription.*
