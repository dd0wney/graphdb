# CI/CD Status Badges

Add these badges to your `README.md` to show CI/CD status:

## GitHub Actions Badges

```markdown
<!-- Test Status -->
[![Tests](https://github.com/darraghdowney/graphdb/workflows/Tests/badge.svg)](https://github.com/darraghdowney/graphdb/actions/workflows/test.yml)

<!-- Lint Status -->
[![Lint](https://github.com/darraghdowney/graphdb/workflows/Lint/badge.svg)](https://github.com/darraghdowney/graphdb/actions/workflows/lint.yml)

<!-- Benchmark Status -->
[![Benchmark](https://github.com/darraghdowney/graphdb/workflows/Continuous%20Benchmarking/badge.svg)](https://github.com/darraghdowney/graphdb/actions/workflows/benchmark.yml)

<!-- Release Status -->
[![Release](https://github.com/darraghdowney/graphdb/workflows/Release/badge.svg)](https://github.com/darraghdowney/graphdb/actions/workflows/release.yml)
```

## Coverage Badge

```markdown
<!-- Codecov -->
[![codecov](https://codecov.io/gh/darraghdowney/graphdb/branch/main/graph/badge.svg)](https://codecov.io/gh/darraghdowney/graphdb)
```

## Go Report Card

```markdown
<!-- Go Report Card -->
[![Go Report Card](https://goreportcard.com/badge/github.com/darraghdowney/graphdb)](https://goreportcard.com/report/github.com/darraghdowney/graphdb)
```

## Go Version

```markdown
<!-- Go Version -->
[![Go Version](https://img.shields.io/github/go-mod/go-version/darraghdowney/graphdb)](go.mod)
```

## Release Version

```markdown
<!-- Latest Release -->
[![Release](https://img.shields.io/github/v/release/darraghdowney/graphdb)](https://github.com/darraghdowney/graphdb/releases/latest)

<!-- Latest Tag -->
[![Tag](https://img.shields.io/github/v/tag/darraghdowney/graphdb)](https://github.com/darraghdowney/graphdb/tags)
```

## Docker

```markdown
<!-- Docker Image Size -->
[![Docker Image Size](https://img.shields.io/docker/image-size/darraghdowney/graphdb/latest)](https://hub.docker.com/r/darraghdowney/graphdb)

<!-- Docker Pulls -->
[![Docker Pulls](https://img.shields.io/docker/pulls/darraghdowney/graphdb)](https://hub.docker.com/r/darraghdowney/graphdb)
```

## License

```markdown
<!-- License -->
[![License](https://img.shields.io/github/license/darraghdowney/graphdb)](LICENSE)
```

## All-in-One Example

Add this to the top of your README.md:

```markdown
# Cluso GraphDB

[![Tests](https://github.com/darraghdowney/graphdb/workflows/Tests/badge.svg)](https://github.com/darraghdowney/graphdb/actions/workflows/test.yml)
[![Lint](https://github.com/darraghdowney/graphdb/workflows/Lint/badge.svg)](https://github.com/darraghdowney/graphdb/actions/workflows/lint.yml)
[![codecov](https://codecov.io/gh/darraghdowney/graphdb/branch/main/graph/badge.svg)](https://codecov.io/gh/darraghdowney/graphdb)
[![Go Report Card](https://goreportcard.com/badge/github.com/darraghdowney/graphdb)](https://goreportcard.com/report/github.com/darraghdowney/graphdb)
[![Go Version](https://img.shields.io/github/go-mod/go-version/darraghdowney/graphdb)](go.mod)
[![Release](https://img.shields.io/github/v/release/darraghdowney/graphdb)](https://github.com/darraghdowney/graphdb/releases/latest)
[![License](https://img.shields.io/github/license/darraghdowney/graphdb)](LICENSE)

A high-performance, feature-rich graph database built from scratch in Go.
```

## Custom Shields.io Badges

```markdown
<!-- Custom badges with shields.io -->

<!-- Test Coverage Percentage -->
![Coverage](https://img.shields.io/badge/coverage-73.5%25-brightgreen)

<!-- Go Version -->
![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)

<!-- Code Style -->
![Code Style](https://img.shields.io/badge/code%20style-gofmt-blue)

<!-- Build Tool -->
![Build](https://img.shields.io/badge/build-make-green)
```

## Status Example

When all checks pass, your README will show:

![Tests](https://img.shields.io/badge/tests-passing-brightgreen)
![Lint](https://img.shields.io/badge/lint-passing-brightgreen)
![Coverage](https://img.shields.io/badge/coverage-73.5%25-brightgreen)
![Go Report](https://img.shields.io/badge/go%20report-A+-brightgreen)
