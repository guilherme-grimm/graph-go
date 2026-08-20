# ADR-0002: Explicit adapter registration instead of `init()` self-registration

- **Status:** Accepted
- **Date:** 2026-08-20
- **Extends:** ADR-0001's "no globals, thread dependencies through constructors" stance
- **Closes:** issue #14

## Context

Every adapter used to self-register through an `init()` side effect:

```go
// internal/adapters/postgres/postgres.go
func init() {
    adapters.RegisterFactory("postgres", func(l *zap.SugaredLogger) adapters.Adapter { return New(l) })
}
```

Those registrations only fired because `internal/server/server.go` blank-imported
all seven adapter packages. The set of adapters a binary supported was therefore
determined by import side effects, written into a package-level `map` guarded by
a `sync.RWMutex`, and readable nowhere.

This is the canonical Go registry pattern — `database/sql` drivers and `image`
decoders both work this way — which is exactly why it needs an ADR to say we
looked at it and chose otherwise.

## Decision

The adapter set is now a **Catalog**: an immutable map from connection type to
constructor, built once at the composition root and passed down as a required
dependency.

- `adapters.Catalog` (in `internal/adapters`) wraps the constructor map. Built
  via `adapters.NewCatalog(map[string]AdapterConstructor)`; looked up via
  `cat.New(connType, logger)`. No `Register` method — it is data, not a mutable
  service.
- `internal/adapters/catalog` holds `Default()`, the one file listing all seven
  adapters. It imports the adapter packages; nothing imports it except
  composition roots. (`internal/adapters` cannot hold this list — the adapter
  packages import it, so the list would be an import cycle.)
- `server.BuildRegistry(cfg, logger, cat)` and `server.NewServer(cfg, logger, cat)`
  take the Catalog as a **required** parameter.
- `internal/adapters/catalog` is the single list of concrete adapters the binary
  ships with; `cmd/app` is the only place that *builds* one and passes it in.
- The seven `func init()` blocks, the seven blank imports, the package-level
  `factories` map, `factoryMu`, `RegisterFactory`, and `NewAdapter` are gone.

**The Catalog is compile-time only.** It will never become a user-facing list of
which adapters to enable — no `adapters: [postgres, redis]` block in YAML. The
moment a user has to declare which adapters exist, "Zero Config" is dead. The
escape hatches stay what they are: `graphgo.ignore` labels and the `connections:`
block for services discovery cannot reach.

## Why not `init()` self-registration

1. **It contradicts ADR-0001.** That ADR bans the `zap.L()` global and requires
   dependencies to be threaded through constructors. A package-level mutable
   registry populated by import side effects is the same class of thing.
2. **A missing blank import fails silently, at runtime, in front of the user.**
   Delete one line from `server.go` and nothing breaks at compile time. The user
   runs `graph-go serve`, their Postgres container is discovered and classified
   correctly, and then adapter construction fails with
   `unknown adapter type "postgres"`. Zero Config visibly broken, cause invisible.

   Two separate compile-time checks now stand where that single deletable line
   used to be, and it is worth being precise about which does what:

   - The **required parameter** means a composition root cannot forget to supply
     a Catalog at all. It says nothing about the Catalog's contents.
   - **Orphaned imports** cover the contents. `catalog.go` imports each adapter
     package solely to name its constructor in the map, so deleting an entry
     leaves `"…/internal/adapters/postgres" imported and not used` — a build
     failure, not a runtime surprise.

   Neither check stops someone from deliberately removing an entry *and* its
   import together, which is as it should be: that is how an adapter gets
   retired. The property gained is that no single careless deletion can silently
   ship a binary missing an adapter. Explicit registration makes the zero-config
   promise *structurally* safer; it does not weaken it.
3. **`internal/server` stops deciding what a binary supports.** It now imports
   only the `Adapter` interface, never a concrete adapter. That is the seam the
   M1 flow work and the M3 MCP server need — either can compose its own adapter
   set without dragging in the HTTP server.
4. **Tests get real isolation.** Every test can build its own Catalog from a map
   literal instead of sharing one process-wide registry. `server_test.go`'s own
   `init()` block — registering a `"test-type"` factory globally — dies with it.
5. **The mutex disappears.** `factoryMu` existed only to guard `init()` writes
   against concurrent reads. A value built once and never mutated needs no lock.
6. Uber Go style guide: avoid `init()`.

The costs, honestly: `catalog.Default()` is seven lines of boilerplate that
`init()` gave us for free, adding an adapter now touches two files instead of
one, and `postgres.New` returning an unexported `*adapter` means each entry needs
a wrapper closure rather than a bare function reference. We accept all three —
the seven lines live in the one file whose entire job is being explicit about
what ships.

## Consequences

- **Adding an adapter** = new package → implement `Adapter` → contract +
  integration tests → **one entry in `catalog.Default()`** → node type in
  `nodes.go` → frontend type + icon. The blank import step is gone.
- **A nil Catalog yields zero adapters**, consistent with `BuildRegistry`'s
  no-error contract: every construction fails, logs, and is skipped rather than
  panicking. Because the parameter is required, this only happens if someone
  passes nil deliberately.
- **Adapter packages no longer import `adapters` for registration** — only for
  the interface and `ConnectionConfig`. `var _ adapters.Adapter = (*adapter)(nil)`
  remains the compile-time conformance check.
- **Runtime adapter registration is now a deliberate non-feature.** A plugin
  system would need a new API rather than reaching for an existing `Register`
  call. That is the intended outcome.
- **The startup visibility gap is unchanged and still open** — a correctly wired
  adapter can still fail to connect at runtime. That is issue #16 (per-adapter
  ✓/✗ startup report), not this ADR.

## References

- `internal/adapters/adapters.go` — `Catalog`, `NewCatalog`, `AdapterConstructor`.
- `internal/adapters/catalog/catalog.go` — `Default()`, the adapter list.
- `internal/server/bootstrap.go` — `BuildRegistry(cfg, logger, cat)`.
- `cmd/app/main.go`, `cmd/app/scan.go` — composition roots.
- `CONTEXT.md` — glossary entries for **Catalog**, **Registry**, **connection type**.
- ADR-0001 — the no-globals precedent this extends.
