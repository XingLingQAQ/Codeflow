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

## Dependencies

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
