# Development Guide

This guide covers development workflows, testing, and contribution guidelines for HAL-O-SWARM.

## Table of Contents

- [Development Environment Setup](#development-environment-setup)
- [Project Structure](#project-structure)
- [Building from Source](#building-from-source)
- [Testing](#testing)
- [Code Style and Standards](#code-style-and-standards)
- [Contributing](#contributing)
- [Release Process](#release-process)

## Development Environment Setup

### Prerequisites

- **Go**: 1.21 or later
- **Git**: 2.30 or later
- **SQLite**: 3.35 or later (for local testing)
- **Make**: For build automation (optional)

### Clone and Setup

```bash
# Clone repository
git clone https://github.com/code-yeongyu/hal-o-swarm.git
cd hal-o-swarm

# Install dependencies
go mod download

# Verify setup
go test ./...
```

### IDE Setup

#### VS Code

Recommended extensions:
- Go (golang.go)
- Go Test Explorer (premparihar.gotestexplorer)
- SQLite Viewer (alexcvzz.vscode-sqlite)

Settings (`.vscode/settings.json`):
```json
{
  "go.useLanguageServer": true,
  "go.lintTool": "golangci-lint",
  "go.testFlags": ["-race"],
  "go.coverOnSave": true
}
```

#### GoLand

- Enable Go Modules integration
- Set test flags: `-race`
- Enable coverage on test

## Project Structure

```
hal-o-swarm/
├── cmd/                    # Entry points
│   ├── supervisor/         # Supervisor daemon
│   │   └── main.go
│   ├── agent/              # Agent daemon
│   │   └── main.go
│   └── halctl/             # CLI tool
│       └── main.go
│
├── internal/               # Private application code
│   ├── supervisor/         # Supervisor implementation
│   │   ├── server.go       # Main server lifecycle
│   │   ├── hub.go          # WebSocket hub
│   │   ├── registry.go     # Node registry
│   │   ├── tracker.go      # Session tracker
│   │   ├── event_pipeline.go  # Event processing
│   │   ├── command_dispatcher.go  # Command handling
│   │   ├── policy_engine.go    # Auto-intervention
│   │   ├── cost.go         # Cost aggregation
│   │   ├── discord.go      # Discord integration
│   │   ├── http_api.go     # HTTP API
│   │   ├── security.go     # Security utilities
│   │   ├── audit.go        # Audit logging
│   │   ├── metrics.go      # Prometheus metrics
│   │   └── health.go       # Health checks
│   │
│   ├── agent/              # Agent implementation
│   │   ├── agent.go        # Main agent lifecycle
│   │   ├── wsclient.go     # WebSocket client
│   │   ├── opencode_adapter.go  # SDK adapter
│   │   ├── envcheck.go     # Environment checker
│   │   └── provision.go    # Auto-provisioner
│   │
│   ├── halctl/             # CLI implementation
│   │   ├── client.go       # HTTP client
│   │   ├── session.go      # Session commands
│   │   ├── node.go         # Node commands
│   │   ├── cost.go         # Cost commands
│   │   ├── env.go          # Environment commands
│   │   └── agentmd.go      # Agent-md commands
│   │
│   ├── shared/             # Shared code
│   │   ├── protocol.go     # Protocol definitions
│   │   ├── types.go        # Common types
│   │   ├── envtypes.go     # Environment types
│   │   └── logging.go      # Logging utilities
│   │
│   ├── config/             # Configuration
│   │   ├── supervisor.go   # Supervisor config
│   │   ├── agent.go        # Agent config
│   │   └── env_manifest.go # Environment manifest
│   │
│   └── storage/            # Data persistence
│       ├── migrations.go   # Migration runner
│       ├── schema.go       # Domain types
│       └── migrations/     # SQL migrations
│           ├── 001_initial_schema.sql
│           └── 002_audit_log.sql
│
├── integration/            # Integration tests
│   ├── e2e_test.go         # End-to-end tests
│   ├── chaos_test.go       # Chaos tests
│   └── helpers.go          # Test utilities
│
├── deploy/                 # Deployment artifacts
│   ├── systemd/            # Systemd units
│   ├── install.sh          # Installation script
│   └── uninstall.sh        # Uninstallation script
│
├── docs/                   # Documentation
│   ├── DEPLOYMENT.md       # Deployment guide
│   ├── RUNBOOK.md          # Operations guide
│   ├── ROLLBACK.md         # Rollback procedures
│   └── DEVELOPMENT.md      # This file
│
└── templates/              # File templates
    ├── AGENT.md            # Agent config template
    └── hooks/              # Git hooks
        └── pre-commit
```

## Building from Source

### Build All Components

```bash
# Build supervisor
go build -o bin/supervisor ./cmd/supervisor

# Build agent
go build -o bin/agent ./cmd/agent

# Build CLI tool
go build -o bin/halctl ./cmd/halctl

# Build all at once
go build -o bin/ ./cmd/...
```

### Build with Version Info

```bash
VERSION=$(git describe --tags --always --dirty)
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(git rev-parse HEAD)

go build -ldflags "\
  -X main.Version=${VERSION} \
  -X main.BuildTime=${BUILD_TIME} \
  -X main.GitCommit=${GIT_COMMIT}" \
  -o bin/supervisor ./cmd/supervisor
```

### Cross-Compilation

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o bin/supervisor-linux-amd64 ./cmd/supervisor

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o bin/supervisor-linux-arm64 ./cmd/supervisor

# macOS AMD64
GOOS=darwin GOARCH=amd64 go build -o bin/supervisor-darwin-amd64 ./cmd/supervisor

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o bin/supervisor-darwin-arm64 ./cmd/supervisor
```

## Testing

### Unit Tests

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Run with coverage
go test -race -cover ./...

# Generate coverage report
go test -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Package-Specific Tests

```bash
# Test supervisor package
go test -race ./internal/supervisor/...

# Test agent package
go test -race ./internal/agent/...

# Test specific file
go test -race ./internal/supervisor/hub_test.go

# Run specific test
go test -race -run TestWebSocketHub ./internal/supervisor/...
```

### Integration Tests

```bash
# Run integration tests
go test -race ./integration/...

# Run specific integration test
go test -race -run TestMultiNodeLifecycle ./integration/...

# Run with verbose output
go test -race -v ./integration/...
```

### Benchmarks

```bash
# Run benchmarks
go test -bench=. ./internal/supervisor/...

# Run specific benchmark
go test -bench=BenchmarkEventPipeline ./internal/supervisor/...

# With memory profiling
go test -bench=. -benchmem ./internal/supervisor/...
```

### Test Coverage Goals

- **Unit Tests**: 80%+ coverage for core packages
- **Integration Tests**: All major workflows covered
- **Race Detector**: All tests must pass with `-race`

## Code Style and Standards

### Go Style Guide

Follow the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) with these additions:

#### Naming Conventions

```go
// Good: Clear, descriptive names
type SessionTracker struct { ... }
func (t *SessionTracker) GetSession(id string) (*Session, error) { ... }

// Bad: Unclear abbreviations
type SessTrkr struct { ... }
func (t *SessTrkr) GetSes(i string) (*Ses, error) { ... }
```

#### Error Handling

```go
// Good: Wrap errors with context
if err := db.Insert(session); err != nil {
    return fmt.Errorf("failed to insert session %s: %w", session.ID, err)
}

// Bad: Lose error context
if err := db.Insert(session); err != nil {
    return err
}
```

#### Logging

```go
// Good: Structured logging with context
logger.Info("Session created",
    zap.String("session_id", session.ID),
    zap.String("project", session.Project),
    zap.String("correlation_id", correlationID))

// Bad: Unstructured logging
logger.Info(fmt.Sprintf("Session %s created for project %s", session.ID, session.Project))
```

#### Context Propagation

```go
// Good: Pass context as first parameter
func (s *Server) Start(ctx context.Context) error { ... }

// Bad: No context
func (s *Server) Start() error { ... }
```

### Linting

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run

# Run with auto-fix
golangci-lint run --fix
```

### Formatting

```bash
# Format code
go fmt ./...

# Or use goimports (recommended)
go install golang.org/x/tools/cmd/goimports@latest
goimports -w .
```

## Contributing

### Workflow

1. **Fork the repository**
2. **Create a feature branch**
   ```bash
   git checkout -b feature/my-feature
   ```

3. **Make your changes**
   - Write tests first (TDD)
   - Implement feature
   - Update documentation

4. **Run tests and linters**
   ```bash
   go test -race ./...
   golangci-lint run
   ```

5. **Commit with conventional commits**
   ```bash
   git commit -m "feat(supervisor): add session filtering by project"
   git commit -m "fix(agent): handle reconnection edge case"
   git commit -m "docs: update deployment guide"
   ```

6. **Push and create PR**
   ```bash
   git push origin feature/my-feature
   ```

### Commit Message Format

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

**Examples**:
```
feat(supervisor): add cost aggregation for OpenAI
fix(agent): prevent reconnection loop on auth failure
docs(deployment): add TLS configuration examples
test(integration): add chaos test for network partition
```

### Pull Request Guidelines

- **Title**: Use conventional commit format
- **Description**: Explain what and why, not how
- **Tests**: Include tests for new features
- **Documentation**: Update relevant docs
- **Breaking Changes**: Clearly mark and explain

### Code Review Checklist

- [ ] Tests pass with `-race` flag
- [ ] Code follows style guide
- [ ] Documentation updated
- [ ] No unnecessary dependencies added
- [ ] Error handling is comprehensive
- [ ] Logging is structured and meaningful
- [ ] Security considerations addressed

## Release Process

### Version Numbering

Follow [Semantic Versioning](https://semver.org/):
- **MAJOR**: Breaking changes
- **MINOR**: New features (backward compatible)
- **PATCH**: Bug fixes (backward compatible)

### Creating a Release

1. **Update version**
   ```bash
   # Update version in relevant files
   VERSION=1.1.0
   ```

2. **Update CHANGELOG.md**
   ```markdown
   ## [1.1.0] - 2026-03-15
   
   ### Added
   - Slack integration
   - Multi-region support
   
   ### Fixed
   - Memory leak in event pipeline
   ```

3. **Create tag**
   ```bash
   git tag -a v1.1.0 -m "Release v1.1.0"
   git push origin v1.1.0
   ```

4. **Build release binaries**
   ```bash
   ./scripts/build-release.sh v1.1.0
   ```

5. **Create GitHub release**
   - Attach binaries
   - Copy CHANGELOG entry
   - Mark as pre-release if applicable

### Hotfix Process

1. **Create hotfix branch from tag**
   ```bash
   git checkout -b hotfix/1.0.1 v1.0.0
   ```

2. **Fix the issue**
   ```bash
   git commit -m "fix(supervisor): critical security patch"
   ```

3. **Tag and release**
   ```bash
   git tag -a v1.0.1 -m "Hotfix v1.0.1"
   git push origin v1.0.1
   ```

4. **Merge back to main**
   ```bash
   git checkout main
   git merge hotfix/1.0.1
   ```

## Debugging

### Local Development

```bash
# Run supervisor with debug logging
go run ./cmd/supervisor --config supervisor.config.json --log-level debug

# Run agent with debug logging
go run ./cmd/agent --config agent.config.json --log-level debug

# Use delve debugger
dlv debug ./cmd/supervisor -- --config supervisor.config.json
```

### Profiling

```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=. ./internal/supervisor/...
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=. ./internal/supervisor/...
go tool pprof mem.prof

# Live profiling (add to main.go)
import _ "net/http/pprof"
go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

### Common Issues

#### Race Conditions

```bash
# Always run tests with race detector
go test -race ./...

# If race detected, use GORACE for details
GORACE="log_path=/tmp/race" go test -race ./...
```

#### Memory Leaks

```bash
# Check for goroutine leaks
go test -run TestMyTest -v 2>&1 | grep "goroutine"

# Use pprof to analyze
go tool pprof http://localhost:6060/debug/pprof/heap
```

## Additional Resources

- [Go Documentation](https://golang.org/doc/)
- [Effective Go](https://golang.org/doc/effective_go)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [WebSocket Protocol](https://datatracker.ietf.org/doc/html/rfc6455)
- [Prometheus Best Practices](https://prometheus.io/docs/practices/)

## Getting Help

- **Documentation**: Check [docs/](../docs/) directory
- **Issues**: Search existing GitHub issues
- **Discord**: Join #hal-o-swarm-dev channel
- **Email**: dev@example.com

---

**Happy coding! 🚀**
