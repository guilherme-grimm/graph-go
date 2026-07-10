# ADR-0001: Use zap SugaredLogger instead of stdlib log/slog

- **Status:** Accepted
- **Date:** 2026-06-29
- **Supersedes:** the "Structured Logging — Use `log/slog`" line in `Roadmap.MD`

## Context

`Roadmap.MD` originally specified stdlib `log/slog` ("no extra dependency") as the
logging layer for graph-go. During the logging-rework phase the team adopted
`go.uber.org/zap`'s `SugaredLogger` instead: it is constructed once in
`cmd/app` (`internal/logging.New(level, format)`) and threaded down through every
constructor — `adapters.NewAdapter(... *zap.SugaredLogger)`,
`server.NewServer(... *zap.SugaredLogger)`, every adapter `New(logger)`, and the
discoverers. A nil logger argument falls back to a no-op logger, so the contract
is "you always get a logger".

This decision is older than the ADR itself; we're recording it now so the
roadmap and future contributors agree on the baseline.

## Decision

Use `zap.SugaredLogger` as the single logger for graph-go.

- **Construction:** `internal/logging.New(level, format string) (*zap.SugaredLogger, error)`.
  Valid levels: `debug`, `info`, `warn`, `error`. Valid formats: `console`, `json`.
- **Threading:** the logger is passed via constructor, never read from a global.
  New modules must accept `*zap.SugaredLogger` as a constructor argument.
- **Output discipline:** logs go to **stderr**. `stdout` is reserved for
  machine-readable data (e.g. `graph-go scan` JSON) so it can be piped.
- **Sugar usage:** prefer the `w` suffix (`logger.Infow("msg", "key", val)`)
  for structured fields; the `f` suffix (`Infof`) is acceptable for formatting
  with no structured fields.
- **`--log-level` / `--log-format`** are global cobra flags; env vars
  `LOG_LEVEL`, `LOG_FORMAT` override defaults.
- **`defer logger.Sync()`** runs at the CLI boundary; sync failures on TTYs are
  ignored (`//nolint:errcheck`).

## Why zap and not slog

`log/slog` would have been dependency-free and is a fine default for new
projects. We chose zap because:

1. **Structured sugar pattern already prototyped.** Before the rework landed,
   the codebase had a SugaredLogger threading pattern in flight. Standardizing
   on it avoided a second rewrite.
2. **Ergonomic structured fields.** `logger.Debugw("discovery complete", "adapter", "postgres", "tables", 12)`
   reads better than the slog equivalent for the per-event adapter/discovery
   logging that runs at debug level on the hot path.
3. **Concurrency-safe core by construction.** The `SugaredLogger` is safe to
   share across goroutines (the registry, the WS handler, and per-adapter
   discovery all share one).
4. **Cheap child loggers via `Named(...)`.** Adapters name subloggers off the
   injected logger (`logger.Named("postgres")`), giving every line a stable
   component tag without extra plumbing.
5. **Performance headroom.** zap's zero-allocation core leaves room for the
   verbose debug logging the discovery loop emits without GC pressure at scale.

`slog` would have delivered 3 and 5 with stdlib purity; the deciding factors
were 2 (sugar ergonomics the team is fluent in) and the existing prototype.

## Consequences

- **One extra dependency** (`go.uber.org/zap`) on the hot path; carried in
  `go.mod`.
- **No `zap.L()` global.** All new modules must accept and thread the logger
  via constructor. Do not introduce package-level globals or the default
  zap global logger.
- **Output discipline is strict.** Anything writing JSON/data writes to
  `stdout`; everything else logs to `stderr`. This keeps `graph-go scan | jq`
  and pipe-based automation clean.
- **Tests receive a logger too** (or a no-op when nil). Integration tests pass
  the real CLI logger; pure unit tests may pass `zap.NewNop().Sugar()`.
- **If we ever want to drop the dependency**, the `internal/logging` package is
  the only construction site, so a swap to slog (or any other logger) is a
  single-file change plus constructor signatures — but signatures touch every
  constructor, so it's still a coordinated change, not free.

## References

- `internal/logging/logging.go` — `New(level, format)`.
- `cmd/app/main.go`, `cmd/app/scan.go` — logger construction + `defer Sync()`.
- `internal/adapters/adapters.go` — `AdapterConstructor` carries `*zap.SugaredLogger`.
- `internal/server/server.go` — `NewServer(cfg, logger)`.
- `internal/discovery/{docker,kubernetes}/*.go` — discoverers thread the logger.