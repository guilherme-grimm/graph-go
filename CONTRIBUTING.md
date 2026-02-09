# Contributing to graph-info

Thank you for your interest in contributing to graph-info! This document provides guidelines and instructions for contributing to the project.

---

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Code Style](#code-style)
- [Adding a New Adapter](#adding-a-new-adapter)
- [Testing](#testing)
- [Submitting Changes](#submitting-changes)
- [Use Scope](#use-scope)

---

## Code of Conduct

This project is intended for legitimate infrastructure visualization and monitoring purposes. Contributors must:

- Only develop features for authorized infrastructure access
- Not contribute code designed to circumvent authentication or authorization
- Follow responsible disclosure practices for any security issues discovered
- Respect the intended use case: helping engineers visualize and monitor systems they own or have permission to access

---

## Getting Started

### Prerequisites

- **Go**: 1.25.6 or higher
- **Node.js**: 18+ (or Bun for faster installs)
- **Docker**: For running test infrastructure (PostgreSQL, MongoDB, MinIO)
- **Git**: For version control

### Fork and Clone

```bash
# Fork the repository on GitHub, then clone your fork
git clone https://github.com/YOUR_USERNAME/graph-info.git
cd graph-info

# Add upstream remote
git remote add upstream https://github.com/original/graph-info.git
```

### Install Dependencies

```bash
# Backend
cd binary
go mod download

# Frontend
cd ../webui
npm install
```

### Run Locally

```bash
# Option 1: Docker Compose (includes test databases)
make docker-up

# Option 2: Local development
make dev  # Runs backend + frontend concurrently
```

---

## Development Workflow

### Branching Strategy

- `main` — Production-ready code
- `feature/your-feature-name` — New features
- `fix/bug-description` — Bug fixes

### Making Changes

1. **Create a feature branch:**
   ```bash
   git checkout -b feature/add-redis-adapter
   ```

2. **Make your changes** and commit frequently:
   ```bash
   git add .
   git commit -m "feat: add Redis adapter for key discovery"
   ```

3. **Keep your branch up to date:**
   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

4. **Run tests and linters:**
   ```bash
   make test
   make lint
   ```

5. **Push to your fork:**
   ```bash
   git push origin feature/add-redis-adapter
   ```

6. **Open a Pull Request** on GitHub

---

## Code Style

### Go Style

- Follow standard Go conventions (gofmt, go vet)
- Use descriptive variable names
- Document exported functions and types
- Keep functions small and focused (under 50 lines when possible)
- Use early returns to reduce nesting

**Example:**
```go
// Connect establishes a connection to the Redis server.
func (a *adapter) Connect(config adapters.ConnectionConfig) error {
    addr, ok := config["addr"].(string)
    if !ok || addr == "" {
        return fmt.Errorf("redis: missing or invalid 'addr' in config")
    }

    // Create Redis client...
}
```

**Run before committing:**
```bash
cd binary
go fmt ./...
go vet ./...
```

### TypeScript Style

- Use strict TypeScript (no `any` unless absolutely necessary)
- Prefer functional components with hooks
- Use descriptive prop names and interfaces
- Keep components under 200 lines

**Example:**
```typescript
interface RedisNodeProps {
  nodeId: string;
  metadata: NodeMetadata;
}

export function RedisNode({ nodeId, metadata }: RedisNodeProps) {
  // Component implementation
}
```

**Type-check before committing:**
```bash
cd webui
npx tsc --noEmit
```

---

## Adding a New Adapter

graph-info uses an **adapter pattern** to support different infrastructure types. Here's how to add a new adapter (e.g., Redis):

### Step 1: Create Adapter Package

Create `binary/internal/adapters/redis/redis.go`:

```go
package redis

import (
    "context"
    "fmt"
    "time"

    "binary/internal/adapters"
    "binary/internal/graph/edges"
    "binary/internal/graph/nodes"
)

type adapter struct {
    client *redis.Client
    addr   string
}

func New() *adapter {
    return &adapter{}
}

func (a *adapter) Connect(config adapters.ConnectionConfig) error {
    addr, ok := config["addr"].(string)
    if !ok || addr == "" {
        return fmt.Errorf("redis: missing 'addr' in config")
    }
    a.addr = addr

    // Create Redis client and test connection
    // ...

    return nil
}

func (a *adapter) Discover() ([]nodes.Node, []edges.Edge, error) {
    // Discover Redis keys, databases, or other structure
    // Return nodes and edges representing the topology
    // ...

    return allNodes, allEdges, nil
}

func (a *adapter) Health() (adapters.HealthMetrics, error) {
    // Check Redis health (PING command)
    // Return metrics like connected_clients, uptime, memory_usage
    // ...

    return adapters.HealthMetrics{
        "status": "healthy",
        "connected_clients": 42,
    }, nil
}

func (a *adapter) Close() error {
    // Close Redis connection
    return nil
}
```

### Step 2: Add Node Type Constant

In `binary/internal/graph/nodes/nodes.go`:

```go
const (
    TypeRedis      NodeType = "redis"
    // ... existing types
)
```

### Step 3: Register in Factory

In `binary/internal/config/factory.go`:

```go
import "binary/internal/adapters/redis"

func AdapterFactory(connType string) (adapters.Adapter, error) {
    switch connType {
    case "redis":
        return redis.New(), nil
    // ... existing cases
    }
}
```

### Step 4: Update Config Schema

In `binary/internal/config/config.go`, update `ToConnectionConfig()` if needed:

```go
case "redis":
    if e.Addr != "" {
        cfg["addr"] = e.Addr
    }
```

Add field to `ConnectionEntry` if needed:
```go
type ConnectionEntry struct {
    // ... existing fields
    Addr string `yaml:"addr,omitempty"`
}
```

### Step 5: Update Frontend Types

In `webui/src/types/graph.ts`:

```typescript
export type NodeType =
  | 'redis'
  | // ... existing types
```

### Step 6: Add Icon

In `webui/src/components/graph/CustomNode.tsx`, add an SVG icon:

```typescript
redis: (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
    <path d="M12 2L2 7l10 5 10-5-10-5z" />
    <path d="M2 17l10 5 10-5" />
    <path d="M2 12l10 5 10-5" />
  </svg>
),
```

### Step 7: Update Sample Config

In `conf/config.sample.yaml`:

```yaml
# Redis
# - name: redis
#   type: redis
#   addr: "localhost:6379"
```

### Step 8: Test

```bash
# Add your adapter to conf/config.yaml
# Run the app
make dev

# Verify nodes appear in the graph
curl http://localhost:8080/api/graph | jq
```

---

## Testing

### Backend Tests

```bash
cd binary
go test ./... -v
```

**Write tests for:**
- Adapter connection logic
- Discovery logic (use mocked clients)
- Health checks
- Error handling

**Example test:**
```go
func TestRedisAdapter_Connect(t *testing.T) {
    a := New()
    cfg := adapters.ConnectionConfig{"addr": "localhost:6379"}

    err := a.Connect(cfg)
    if err != nil {
        t.Fatalf("connect failed: %v", err)
    }
}
```

### Frontend Tests

```bash
cd webui
npx tsc --noEmit  # Type checking
```

---

## Submitting Changes

### Pull Request Checklist

Before submitting a PR, ensure:

- [ ] Code follows Go and TypeScript style guides
- [ ] `make lint` passes
- [ ] `make test` passes (all backend tests)
- [ ] TypeScript type-checking passes (`npx tsc --noEmit`)
- [ ] Commit messages are descriptive (use [Conventional Commits](https://www.conventionalcommits.org/))
- [ ] New adapters include discovery logic and health checks
- [ ] Documentation is updated (README, config.sample.yaml)

### Commit Message Format

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add Redis adapter with key discovery
fix: handle empty MongoDB collections correctly
docs: update README with Redis configuration
test: add unit tests for S3 adapter
refactor: simplify adapter registry logic
```

### PR Description Template

```markdown
## Description
Brief description of what this PR does.

## Type of Change
- [ ] Bug fix
- [ ] New feature (adapter, UI component, etc.)
- [ ] Documentation update
- [ ] Refactoring

## Testing
How did you test this? Include steps to reproduce.

## Checklist
- [ ] Tests pass
- [ ] Linters pass
- [ ] Documentation updated
```

---

## Use Scope

**Intended Contributions:**
- New adapters for infrastructure components (Redis, Kafka, Elasticsearch, etc.)
- UI improvements and visualization features
- Performance optimizations
- Bug fixes and error handling improvements
- Documentation and examples

**Not Accepted:**
- Features designed to bypass authentication or authorization
- Tools for unauthorized system scanning or reconnaissance
- Code that violates the intended use case (authorized infrastructure monitoring)

Contributors are expected to build features that help engineers visualize and monitor systems they own or have permission to access.

---

## Questions?

If you have questions about contributing:

1. Check existing [Issues](https://github.com/yourusername/graph-info/issues)
2. Start a [Discussion](https://github.com/yourusername/graph-info/discussions)
3. Reach out to maintainers

---

**Thank you for contributing to graph-info!** 🚀
