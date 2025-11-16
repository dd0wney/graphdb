# CI/CD Pipeline Documentation

Cluso GraphDB uses **GitHub Actions** for a modern, cloud-native CI/CD pipeline that leverages Go's native tooling.

## 🚀 Overview

Our CI/CD pipeline is split into multiple specialized workflows:

| Workflow | Trigger | Purpose | Duration |
|----------|---------|---------|----------|
| **Tests** | Push/PR | Run tests, race detection, coverage | ~5-10 min |
| **Lint** | Push/PR | Code quality, formatting, security | ~2-3 min |
| **Benchmark** | Push to main | Performance tracking | ~3-5 min |
| **Release** | Version tag | Build & publish releases | ~5-10 min |

---

## 📋 Workflows

### 1. Test Workflow (`.github/workflows/test.yml`)

**Runs on:** Every push and pull request to `main` or `develop`

**Jobs:**

#### a) Matrix Testing
- Tests across **3 Go versions** (1.23, 1.24, 1.25)
- Tests on **2 operating systems** (Ubuntu, macOS)
- Total: **6 test combinations**

```yaml
Strategy:
  - Go 1.23 + Ubuntu
  - Go 1.23 + macOS
  - Go 1.24 + Ubuntu
  - Go 1.24 + macOS
  - Go 1.25 + Ubuntu
  - Go 1.25 + macOS
```

**Steps:**
1. ✅ Verify dependencies (`go mod verify`)
2. ✅ Run `go vet` (static analysis)
3. ✅ Check formatting (`go fmt`)
4. ✅ Run all tests (`make test-verbose`)
5. ✅ Run race detector (`make test-race`)

#### b) Coverage Analysis
- Generates coverage report
- Uploads to **Codecov** for tracking
- Archives report as artifact (30 days retention)

**Coverage Targets:**
- Storage: 77%
- LSM: 68%
- Query: 65%
- Algorithms: 95%
- Parallel: 94%
- WAL: 78%
- **Overall: 73.5%**

#### c) Benchmarks
- Runs performance benchmarks
- Archives results for comparison

#### d) Build Verification
- Builds all binaries
- Uploads artifacts (7 days retention)

---

### 2. Lint Workflow (`.github/workflows/lint.yml`)

**Runs on:** Every push and pull request

**Jobs:**

#### a) golangci-lint
Uses the official **golangci-lint-action** with:
- 20+ enabled linters
- Security scanning (gosec)
- Code duplication detection
- Cyclomatic complexity checks
- See `.golangci.yml` for full configuration

#### b) go.mod verification
Ensures `go.mod` and `go.sum` are clean:
```bash
go mod tidy
git diff --exit-code go.mod go.sum
```

#### c) Security Scanning
- Runs **Gosec** security scanner
- Uploads results as SARIF format
- Integrates with GitHub Security tab

---

### 3. Benchmark Workflow (`.github/workflows/benchmark.yml`)

**Runs on:** Push to main, PRs to main

**Features:**
- Continuous performance tracking
- Compares against previous benchmarks
- **Alerts on 150% performance regression**
- Tracks memory allocations
- Generates CPU/memory profiles

**Example Output:**
```
BenchmarkCreateNode-8       500000    3.7 μs/op    2400 B/op    20 allocs/op
BenchmarkCreateEdge-8      1000000    1.3 μs/op    1100 B/op     6 allocs/op
BenchmarkGetNode-8        15000000    82 ns/op      128 B/op     3 allocs/op
```

---

### 4. Release Workflow (`.github/workflows/release.yml`)

**Runs on:** Version tags (e.g., `v1.0.0`)

**Jobs:**

#### a) Release Build
Uses **GoReleaser** to:
- Build for multiple platforms (Linux, macOS, Windows)
- Build for multiple architectures (amd64, arm64)
- Generate checksums
- Create GitHub release with changelog
- Upload binaries as release assets

**Artifacts:**
```
cluso-graphdb_1.0.0_Linux_x86_64.tar.gz
cluso-graphdb_1.0.0_Linux_arm64.tar.gz
cluso-graphdb_1.0.0_Darwin_x86_64.tar.gz
cluso-graphdb_1.0.0_Darwin_arm64.tar.gz
cluso-graphdb_1.0.0_Windows_x86_64.zip
cluso-graphdb_1.0.0_Windows_arm64.zip
```

#### b) Docker Build & Push
- Multi-platform Docker images (amd64, arm64)
- Pushed to Docker Hub
- Tags: `latest`, `1.0.0`, `1.0`, `1`
- Uses BuildKit for caching

---

## 🔧 Local Development

### Running CI Checks Locally

```bash
# Full CI check
make ci

# Individual checks
make fmt              # Format code
make vet              # Static analysis
make test             # Run tests
make test-race        # Race detection
make test-cover       # Coverage report
make lint             # Lint (requires golangci-lint)
```

### Install Development Tools

```bash
make install-tools
```

This installs:
- `golangci-lint` - Linter aggregator
- `gofumpt` - Stricter formatter

---

## 🐳 Docker

### Build Docker Image Locally

```bash
docker build -t cluso-graphdb:dev .
```

### Run Docker Container

```bash
docker run -p 8080:8080 -v $(pwd)/data:/data cluso-graphdb:dev
```

### Docker Compose (Optional)

Create `docker-compose.yml`:

```yaml
version: '3.8'
services:
  graphdb:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    environment:
      - LOG_LEVEL=info
    healthcheck:
      test: ["CMD", "wget", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 3s
      retries: 3
```

---

## 📊 Monitoring & Metrics

### Coverage Tracking
- **Codecov Integration**: Automatic coverage reports on PRs
- **Coverage Badge**: Shows current coverage percentage
- **Trend Tracking**: Monitor coverage over time

### Performance Tracking
- **Benchmark Action**: Tracks performance across commits
- **Automatic Alerts**: Warns on significant regressions
- **Historical Data**: Compare performance over time

### Security
- **Gosec Scanner**: Identifies security vulnerabilities
- **Dependabot**: Automatic dependency updates
- **SARIF Upload**: GitHub Security tab integration

---

## 🔖 Release Process

### Creating a Release

1. **Update version** (if using version file)
2. **Commit changes**
   ```bash
   git commit -am "chore: prepare for v1.0.0"
   ```
3. **Create and push tag**
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```
4. **Wait for CI** - GitHub Actions will:
   - Run all tests
   - Build binaries for all platforms
   - Create GitHub release
   - Build and push Docker images
   - Generate changelog

### Versioning Strategy

We follow **Semantic Versioning** (SemVer):

- `MAJOR.MINOR.PATCH`
- `v1.0.0` - Initial release
- `v1.1.0` - New features (backward compatible)
- `v1.1.1` - Bug fixes
- `v2.0.0` - Breaking changes

---

## 🎯 Best Practices

### Pull Request Workflow

1. **Create feature branch**
   ```bash
   git checkout -b feature/query-optimizer
   ```

2. **Make changes with TDD**
   ```bash
   # Write failing test
   make test

   # Implement feature
   make test

   # Verify all checks pass
   make dev
   ```

3. **Push and create PR**
   - All CI checks must pass
   - Code review required
   - Coverage should not decrease

### Commit Message Format

Follow **Conventional Commits**:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types:**
- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation
- `test:` - Adding tests
- `refactor:` - Code refactoring
- `perf:` - Performance improvement
- `ci:` - CI/CD changes

**Examples:**
```
feat(query): add cost-based query optimizer
fix(storage): resolve race condition in node creation
test(optimizer): add comprehensive optimizer tests
perf(lsm): optimize sstable compaction
```

---

## 🚨 Troubleshooting

### Tests Failing Locally But Passing in CI
- Ensure Go version matches CI (1.25)
- Check for OS-specific issues
- Run with race detector: `make test-race`

### Coverage Decreased
- Add tests for new code
- Check coverage report: `make test-cover-html`
- Target: maintain >70% overall coverage

### Linter Errors
```bash
# Run locally
make lint

# Auto-fix some issues
make fmt

# Check specific linter
golangci-lint run --enable-only=gosec
```

### Docker Build Fails
- Check `.dockerignore`
- Verify all dependencies in `go.mod`
- Test locally: `docker build -t test .`

---

## 📚 Resources

- [GitHub Actions Docs](https://docs.github.com/actions)
- [GoReleaser Docs](https://goreleaser.com)
- [golangci-lint Docs](https://golangci-lint.run)
- [Codecov Docs](https://docs.codecov.com)
- [Semantic Versioning](https://semver.org)
- [Conventional Commits](https://www.conventionalcommits.org)

---

## 🎉 Summary

Our CI/CD pipeline provides:

✅ **Automated Testing** - Every commit tested across multiple environments
✅ **Code Quality** - Automated linting and formatting checks
✅ **Security** - Continuous security scanning
✅ **Performance** - Benchmark tracking with regression alerts
✅ **Coverage** - Automatic coverage reporting and tracking
✅ **Releases** - One-command multi-platform releases
✅ **Docker** - Automated container builds and publishing

All using **native Go tooling** and industry-standard practices!
