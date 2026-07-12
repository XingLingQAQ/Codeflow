# CodeFlow Backend (Go)

Go-based backend for CodeFlow: Next-generation Agentic IDE

## Project Structure

```
backend/
├── cmd/
│   └── codeflow-server/    # Main application entry point
├── internal/               # Private application code
│   ├── memory/            # Vector storage & memory management
│   ├── privacy/           # Encryption & privacy-aware RAG
│   ├── audit/             # Audit logging & compliance
│   ├── config/            # Multi-level configuration
│   ├── git/               # Git operations
│   ├── adapters/          # LLM adapters (Claude/Gemini/Codex)
│   ├── hotswap/           # Model hot-swapping
│   └── storage/           # Generic storage abstractions
├── pkg/                    # Public libraries
│   ├── types/             # Shared types
│   └── utils/             # Utility functions
├── go.mod                  # Go module definition
├── Makefile                # Build automation
└── .golangci.yml           # Linter configuration
```

## Requirements

- Go 1.23+
- GCC (for CGO - required by go-sqlite3)
- Make

## Quick Start

### Build

```bash
make build
```

### Run

```bash
make run
```

### Development Mode

```bash
make dev
```

### Test

```bash
make test
```

### Lint

```bash
make lint
```

## API Documentation

The backend provides a RESTful API on port 8080. Full OpenAPI documentation is available at `docs/openapi.yaml`.

Root-level scripts keep the API contract and generated frontend types in sync:

```bash
pnpm check:api-contracts
pnpm generate:api-types
pnpm check:api-types
```

Generated TypeScript contract types are written to `apps/workbench/generated/openapi-types.ts` and are checked in CI.

### Quick API Examples

#### Health Check
```bash
curl http://localhost:8080/health
```

#### Memory Management
```bash
# List memory items
curl http://localhost:8080/api/v1/memory/items

# Create memory item
curl -X POST http://localhost:8080/api/v1/memory/items \
  -H "Content-Type: application/json" \
  -d '{"content":"Important note","type":"stm","tags":["note"]}'

# Archive to LTM
curl -X POST http://localhost:8080/api/v1/memory/items/{id}/archive
```

#### Search
```bash
# Vector search
curl -X POST http://localhost:8080/api/v1/search/vector \
  -H "Content-Type: application/json" \
  -d '{"query":"authentication flow","limit":10}'

# Hybrid search
curl -X POST http://localhost:8080/api/v1/search/hybrid \
  -H "Content-Type: application/json" \
  -d '{"query":"login","vector_weight":0.5,"fulltext_weight":0.3,"graph_weight":0.2}'
```

#### Context Analysis
```bash
# Get file tree
curl "http://localhost:8080/api/v1/context/files?path=./src"

# Parse AST
curl -X POST http://localhost:8080/api/v1/context/ast \
  -H "Content-Type: application/json" \
  -d '{"file_path":"main.go","language":"go"}'

# Calculate tokens
curl -X POST http://localhost:8080/api/v1/context/tokens \
  -H "Content-Type: application/json" \
  -d '{"content":"func main() { fmt.Println(\"hello\") }"}'
```

#### Agent Management
```bash
# List agents
curl http://localhost:8080/api/v1/agents

# Get agent logs
curl http://localhost:8080/api/v1/agents/{id}/logs?limit=100
```

#### Blackboard Collaboration
```bash
# Create entry
curl -X POST http://localhost:8080/api/v1/blackboard/entries \
  -H "Content-Type: application/json" \
  -d '{"type":"proposal","content":"New feature proposal","author":"agent-1"}'

# Create vote
curl -X POST http://localhost:8080/api/v1/votes \
  -H "Content-Type: application/json" \
  -d '{"entry_id":"xxx","title":"Approve proposal?","options":["yes","no"]}'
```

#### Debate System
```bash
# Create debate
curl -X POST http://localhost:8080/api/v1/debates \
  -H "Content-Type: application/json" \
  -d '{"title":"Architecture Decision","topic":"Should we use microservices?"}'

# Advance round
curl -X POST http://localhost:8080/api/v1/debates/{id}/next-round \
  -H "Content-Type: application/json" \
  -d '{"generator_output":"Proposal...","critic_feedback":"Concerns..."}'
```

#### Task Planning
```bash
# Create plan
curl -X POST http://localhost:8080/api/v1/plans \
  -H "Content-Type: application/json" \
  -d '{"title":"Sprint 1","description":"Initial implementation"}'

# Create task
curl -X POST http://localhost:8080/api/v1/plans/{id}/tasks \
  -H "Content-Type: application/json" \
  -d '{"title":"Setup CI/CD","priority":"P0"}'

# Batch update models
curl -X POST http://localhost:8080/api/v1/plans/{id}/tasks/batch-model \
  -H "Content-Type: application/json" \
  -d '{"task_ids":["task1","task2"],"model":"claude-3-opus"}'
```

## Dependencies

- **gin-gonic/gin**: HTTP web framework
- **gin-contrib/cors**: CORS middleware
- **gorilla/websocket**: WebSocket support
- **google/uuid**: UUID generation
- **mattn/go-sqlite3**: SQLite database driver (CGO required)
- **golang.org/x/crypto**: Cryptographic utilities (scrypt, etc.)
- **gopkg.in/yaml.v3**: YAML configuration parsing

## Migration Status

| Module | Status | Priority |
|---|---|---|
| Memory (Vector Store) | 🔄 Planned | P0 |
| Privacy (Encryption) | 🔄 Planned | P0 |
| Audit (Logging) | 🔄 Planned | P0 |
| Git Operations | 🔄 Planned | P1 |
| Config Management | 🔄 Planned | P1 |
| LLM Adapters | 🔄 Planned | P1 |
| Hot-Swap | 🔄 Planned | P1 |

## Development

### Install golangci-lint

```bash
make install-lint
```

### Format Code

```bash
make fmt
```

### Clean Build Artifacts

```bash
make clean
```

### Run with Coverage

```bash
make test-coverage
```

## License

ISC
