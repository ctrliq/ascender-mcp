# Contributing to Ascender MCP

Thanks for your interest in contributing to Ascender MCP. This document covers the development setup,
testing, and pull request guidelines.

## Development setup

Fork and clone the repository, then build from the root, where `go.mod` lives:

```bash
git clone https://github.com/<your-user>/ascender-mcp.git
cd ascender-mcp
go build ./...
```

To run the server against an Ascender install:

```bash
ASCENDER_HOST=https://ascender.example.com go run ./cmd/ascender-mcp
```

To build the container image the way a release does:

```bash
podman build -t ascender-mcp:dev -f Containerfile .
```

## Running tests

These are the checks CI runs on every pull request:

```bash
go vet ./...
gofmt -l .
go test -race ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
```

`gofmt -l` must print nothing. `govulncheck` is advisory in CI, so a finding there is reported rather than
blocking, but say so in the pull request when one appears.

## Making changes

### Branching

Create a feature branch from `main`:

```bash
git checkout -b my-feature main
```

### Tool names

Tool names follow `<verb>_<resource>` and are a stable contract with every client that has learnt them.
Add new tools freely, but treat renaming or removing one as a breaking change and say so in the pull request.

### Commit messages

Write clear, concise commit messages:

```
Short summary (under 72 characters)

Longer description of what changed and why, if needed.
```

## Submitting a PR

1. Make sure the checks above pass locally.
2. One logical change per PR. Do not bundle unrelated fixes.
3. Target the `main` branch.
4. Explain what changed and why in the PR description.

## Reporting issues

Open an issue at [github.com/ctrliq/ascender-mcp/issues](https://github.com/ctrliq/ascender-mcp/issues).
Include the version you are running, the MCP client, and the steps that reproduce the problem.

For security vulnerabilities, follow [SECURITY.md](./SECURITY.md) instead of opening a public issue.
