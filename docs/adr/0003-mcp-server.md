# ADR-0003: MCP server — warm stdio process, read-only tools, summary by default

- **Status:** Accepted (merged in #53; implemented in #54)
- **Date:** 2026-10-30
- **Extends:** ADR-0001 (stdout reserved for machine-readable data), ADR-0002 (composition roots assemble their own Catalog)
- **Implements:** issue #20 (M3, first slice). #29 and #30 add tools later.

## Context

M3 gives coding agents deterministic visibility into infrastructure. Without
it, an agent works out a stack by grepping compose files and manifests,
running `docker ps` / `kubectl get`, and guessing which env var points at which
database. That costs tokens and goes wrong easily (#43 is that failure mode
inside graph-go itself). One tool call that returns what exists, how it is
connected and whether it is healthy replaces the guessing with facts.

Issue #20 asked for a stdio server whose `get_topology` returns the
`scan --format=json` output. Two facts change that:

1. **Size.** At 5k pods the graph is about 6k nodes and 10k edges, megabytes of
   JSON. Agent clients cap tool output (Claude Code warns at roughly 25k
   tokens), and a capped dump is useless. The UI hit the same wall and solved
   it with namespace collapse above 300 nodes (v0.4.0-rc.1).
2. **Where the data goes.** Tool output enters the model's context and so
   reaches a model provider. Node metadata and error strings could carry
   credentials, which #52 fixes for every output at the registry.

## Decision

`graph-go mcp` runs an MCP server over **stdio**, built on the official
**`github.com/modelcontextprotocol/go-sdk`** (v1.8.0, Go 1.25).

### Process model

- **Long-lived and warm.** Clients start one server per session. On the first
  tool call (lazily, so a misconfigured client does not block session start)
  it builds the registry exactly as `serve` does (`server.BuildRegistry`,
  Docker and Kubernetes discoverers, YAML connections) and starts the
  discoverers' `Watch` loops. Later calls are served from the registry's
  cache, which the watchers keep fresh.
- **Health** comes from a background loop on the same 5s tick the WebSocket
  uses, not from a sweep per call. Every response carries `observed_at`, the
  time of the health sweep it reflects.
- **Logs** go to stderr (ADR-0001; stdout is the protocol). The default level
  for `mcp` is `warn`, because clients surface the server's stderr and
  discovery chatter is noise there. `--log-level` still overrides it.
- **Config** uses the same global `--config` flag as `serve` and `scan`.

### Lifecycle

- **Watchers** run under a context owned by the registry
  (`server.StartRegistry`), separate from any request.
- **Shutdown.** The session ends when the client closes stdin, or on SIGINT
  or SIGTERM. The server then stops the health loop, cancels the watchers'
  context, closes every discoverer and calls `Registry.CloseAll`. That is the
  same cleanup `serve` runs.
- **A watcher that dies** (its `Watch` returns an error) is logged at warn,
  as in `serve`, and is **not restarted**. That source's topology then stays
  at its last snapshot while the others keep updating. The health loop keeps
  running, so `observed_at` stays accurate for health. It does **not** prove
  the topology of a dead source is current. v1 accepts this, because Docker
  and Kubernetes watchers only return on cancellation or a failed start in
  practice. Restarting with backoff, or marking the source stale in responses,
  is a follow-up that `serve` needs as much as `mcp` does.

### Tools (v1, read-only)

Three tools. The node and edge shapes are exactly `scan`'s (`nodes.Node`,
`edges.Edge`), so anything that already parses `scan` output parses tool
output. Descriptions are written for the model, which picks tools from them.

**`get_topology`**

> Get the infrastructure topology graph-go discovered: services, databases,
> caches, storage, Kubernetes workloads and pods, how they connect, and their
> current health. On large systems this returns a summary (counts per
> namespace and type, plus every unhealthy or degraded node in full). Use the
> filters to get full detail for a namespace, node type or health state, or
> use find_nodes / get_node for specific services.

| Input | Type | Meaning |
|---|---|---|
| `namespaces` | string[] | Only nodes in these Kubernetes namespaces |
| `types` | string[] | Only these node types (`postgres`, `pod`, `k8s_service`, …) |
| `health` | string[] | Only these states (`healthy`, `degraded`, `unhealthy`, `unknown`) |
| `name` | string | Case-insensitive substring of node name or id |
| `detail` | `auto` (default) \| `summary` \| `full` | `auto`: full when the filtered result has ≤ 300 nodes, summary above. `full` still respects the byte cap. |

Output: `{observed_at, total: {nodes, edges}, matched, truncated, nodes, edges, summary?, hint?}`.
Each field is counted at a defined stage:

| Field | Counted |
|---|---|
| `total` | over the whole graph, **before** filters |
| `matched` | after filters, **before** summarising or the byte cap |
| `summary` | over the `matched` nodes, so it describes everything the filters selected |
| `nodes` / `edges` | what is actually returned, after summarising and the byte cap. `edges` are those between the returned nodes. |
| `truncated` | `true` whenever `len(nodes) < matched`, for either reason |

So a client tells "filtered out" from "omitted" by comparing `matched` with
`total` (filters) and `len(nodes)` with `matched` (summary or cap). `hint`
says which one happened and how to narrow the query.

**`get_node`**

> Get one node by id with its direct neighbours: the node itself, every edge
> into or out of it, and the nodes on the other end of those edges. Use this
> to answer "what does X depend on / what depends on X".

| Input | Type |
|---|---|
| `id` | string (required), as returned by the other tools |

Output: `{observed_at, node, edges, neighbors, truncated, hint?}`. For an
unknown id the shape stays the same: `node` is **omitted**, `edges` and
`neighbors` are empty arrays, `truncated` is `false`, and `hint` says to use
`find_nodes`. It is not a protocol or tool error.

```json
{"observed_at": "…", "edges": [], "neighbors": [], "truncated": false,
 "hint": "No node with id \"x\". Use find_nodes to look ids up by name."}
```

**`find_nodes`**

> Search nodes by name or id (case-insensitive substring). Returns compact
> matches (id, name, type, namespace, health), best matches first. Use the
> ids with get_node.

| Input | Type |
|---|---|
| `query` | string (required) |
| `limit` | int, default 20, max 100 |

Output: `{observed_at, matches, total, hint?}`, the same envelope style as the
other tools, never a bare array. `matches` holds `{id, name, type, namespace?,
health}` entries, best first. `total` counts every match, including those past
`limit` or the byte cap. With no match, `matches` is an empty array,
`total` is 0, and `hint` suggests `get_topology` to see what exists. An empty
`query` is a tool error.

**Rejected inputs** (tool errors, never empty results): an empty
`find_nodes` query, a `detail` other than `auto`/`summary`/`full`, and
arguments that fail the advertised input schema (the SDK validates those
before the handler runs). Everything else, including unknown ids, filters
that match nothing and an empty graph, is an answer with a hint.

### Size control

- The 300-node threshold matches the UI's `LARGE_GRAPH_NODES`.
- **A hard byte cap** (default 80 KB, about 20k tokens; `--max-response-bytes`)
  backs up every tool. It bounds the **whole result as sent**: the SDK puts
  the output in `structuredContent` *and* again as JSON text content, so the
  cap is measured on both together, hint included. When a response would
  exceed it, the server drops nodes (or `find_nodes` matches) lowest priority
  first: healthy, then unknown, then degraded, then unhealthy. It sets
  `truncated: true` with a hint, so the agent always gets valid JSON and a way
  to narrow the query.
- **The cap takes precedence over the summary's guarantee.** A summary lists
  every unhealthy and degraded match, *unless* those alone exceed the cap.
  Then the cap drops degraded before unhealthy, and `truncated` plus the hint
  say so. `summary` still counts all of them, so the totals are never lost.

### Empty results are answers

With no Docker socket and no reachable cluster, the tools return an empty
graph plus a `hint` saying what was looked for (socket path, kube context).
They do not return an error: agents tend to retry or abandon a tool that
errors.

### Read-only, enforced structurally

v1 registers only the three tools above. A test asserts the registered tool
set is exactly that, so adding a tool that acts (#30's stress runs) is a
deliberate change. Action tools will need an explicit `--allow-actions` flag
**and** #30's config-side opt-in and rate/duration limits. Callers never set
those limits.

### Redaction

The tools add no redaction of their own. They read from the `Registry`, which
masks credentials in all node metadata and health metrics before caching
(#52, `internal/redact`). Keeping that in one place means MCP, the UI and
`scan` cannot drift.

## Alternatives considered

- **Fresh discovery per call (`scan`'s model).** Simplest, but every call pays
  adapter connects and up to 5s of Kubernetes probing. Rejected for v1.
- **Thin client to a running `serve` (`--server <url>`).** Cheap and useful
  when graph-go already runs as a shared instance. **Follow-up PR,** shipped
  together with the HTTP transport, which has the same audience.
- **Always return the full `scan` JSON.** Breaks on any real cluster.
- **`mark3labs/mcp-go` or hand-rolled JSON-RPC.** The official SDK is
  maintained alongside the spec and matches our Go version.
- **Google's agent stack.** *MCP Toolbox for Databases* complements graph-go
  (it queries *inside* databases; graph-go maps what exists). Generating its
  `tools.yaml` from discovery is a possible later integration, but it would
  need credentials, which conflicts with redaction. *A2A* and *ADK* are for
  agent-to-agent communication and for building agents; graph-go is a tool,
  so MCP is the right layer.
- **MCP resources/prompts.** Not needed for v1. Tools cover the questions
  agents ask.

## Consequences

- Issue #20's acceptance criterion "output matches the `scan --format=json`
  schema" becomes "node and edge shapes match `scan`; above 300 nodes the
  default is a summary".
- `cmd/app` gains an `mcp` subcommand and `internal/mcp` (tool handlers over
  `adapters.Registry`). `internal/server` is not touched beyond reusing
  `BuildRegistry` and the health tick.
- Tests drive the real server through the SDK's in-memory transport with a
  real registry. Only the discoverer is fed fixture services (no mocks).
- The README gains a client setup section (`claude mcp add graph-go -- graph-go mcp`
  and the JSON config shape other clients use).

## References

- Issue #20, #29, #30; PR #52 (redaction)
- `webui/src/utils/graphView.ts` (`LARGE_GRAPH_NODES = 300`)
- https://github.com/modelcontextprotocol/go-sdk
