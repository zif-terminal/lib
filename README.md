# zif-lib

[![CI](https://github.com/zif-terminal/lib/actions/workflows/ci.yml/badge.svg)](https://github.com/zif-terminal/lib/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/badge/coverage-69.5%25-yellow)](https://github.com/zif-terminal/lib)
[![Go Version](https://img.shields.io/badge/go-1.21+-blue)](https://go.dev/)

Shared Go library for the Zif platform. Provides database models, GraphQL client, and exchange integrations.

## Prerequisites

- Go 1.21+
- Git

## Quick Start

```bash
# Clone the repository
git clone git@github.com:zif-terminal/lib.git
cd lib

# Setup git hooks (enforces conventional commits)
./.githooks/setup.sh

# Run tests
go test ./...
```

## Development Workflow

### 1. Setup Git Hooks

Run this once after cloning:

```bash
./.githooks/setup.sh
```

This installs two hooks:
- **commit-msg** - Validates [conventional commit](#commit-message-format) format
- **pre-push** - Runs tests and blocks direct pushes to `main`

### 2. Make Changes

Create a branch and make your changes:

```bash
git checkout -b feat/my-feature
# ... make changes ...
go test ./...
```

### 3. Commit with Conventional Format

```bash
git commit -m "feat: add new exchange client"
```

If your commit message doesn't follow the format, the hook will reject it:

```
❌ Invalid commit message format!

Expected: <type>[scope][!]: <description>
Types: feat, fix, docs, style, refactor, perf, test, chore, ci, build
```

### 4. Push and Create PR

```bash
git push origin feat/my-feature
# Create PR on GitHub
```

The CI will validate your commits and run tests.

### 5. Merge and Auto-Release

When your PR is merged to `main`, the CI automatically:
1. Determines version bump from commit messages
2. Creates and pushes a new version tag

## Commit Message Format

This repo uses [Conventional Commits](https://www.conventionalcommits.org/) for automatic semantic versioning.

### Format

```
<type>[optional scope][!]: <description>

[optional body]

[optional footer(s)]
```

### Types and Version Bumps

| Type | Description | Version Bump |
|------|-------------|--------------|
| `feat` | New feature | MINOR (1.0.0 → 1.1.0) |
| `fix` | Bug fix | PATCH (1.0.0 → 1.0.1) |
| `feat!` or `fix!` | Breaking change | MAJOR (1.0.0 → 2.0.0) |
| `docs` | Documentation | PATCH |
| `style` | Code style (formatting) | PATCH |
| `refactor` | Code refactoring | PATCH |
| `perf` | Performance improvement | PATCH |
| `test` | Adding/updating tests | PATCH |
| `chore` | Maintenance | PATCH |
| `ci` | CI/CD changes | PATCH |
| `build` | Build system changes | PATCH |

### Examples

```bash
# New feature (MINOR bump: v1.5.0 → v1.6.0)
git commit -m "feat: add drift exchange client"

# Bug fix (PATCH bump: v1.5.0 → v1.5.1)
git commit -m "fix: correct funding payment calculation"

# Feature with scope
git commit -m "feat(db): add batch insert for trades"

# Breaking change (MAJOR bump: v1.5.0 → v2.0.0)
git commit -m "feat!: redesign exchange client interface"

# Documentation (PATCH bump)
git commit -m "docs: update API examples in README"
```

## CI/CD Pipeline

```
┌───────────────────────────────────────────────────────────┐
│ Pull Request                                              │
├───────────────────────────────────────────────────────────┤
│ ✓ test             - Runs tests + checks coverage        │
│ ✓ validate-commits - Checks conventional commit format   │
└───────────────────────────────────────────────────────────┘
                           │
                           ▼ merge
┌───────────────────────────────────────────────────────────┐
│ Main Branch                                               │
├───────────────────────────────────────────────────────────┤
│ ✓ test             - Runs tests + checks coverage        │
│ ✓ release          - Creates version tag from commits    │
└───────────────────────────────────────────────────────────┘
```

## Package Structure

### `models/` - Data Structures

Go structs matching the database schema:

- `Exchange` - Exchange entity
- `ExchangeAccount` - User's exchange account
- `Trade` - Individual trade record
- `Position` - Aggregated position
- `FundingPayment` - Funding payment record

### `db/` - Database Client

GraphQL client for Hasura with CRUD operations:

```go
import "github.com/zif-terminal/lib/db"

client := db.NewClient(db.ClientConfig{
    URL:         "http://localhost:8080/v1/graphql",
    AdminSecret: "your-admin-secret",
})

// Create an account
account, err := client.CreateAccount(ctx, &db.ExchangeAccountInput{
    ExchangeID:        exchangeID,
    AccountIdentifier: "0x123...",
    AccountType:       "main",
})
```

### `exchange/` - Exchange Clients

Unified interface for fetching data from exchanges:

```go
import (
    "github.com/zif-terminal/lib/exchange"
    "github.com/zif-terminal/lib/exchange/iface"
)

// Get a client for an exchange
client, err := exchange.GetClient("hyperliquid")

// Fetch trades
trades, err := client.FetchTrades(ctx, account, &iface.FetchOptions{
    Since: time.Now().Add(-24 * time.Hour),
})
```

**Supported Exchanges:**
- Hyperliquid
- Drift
- Lighter

### `logger/` - Structured Logging

Simple structured logger for consistent output across services.

## Repository Structure

```
lib/
├── .github/
│   └── workflows/
│       └── ci.yml            # CI pipeline + auto-release
├── .githooks/
│   ├── commit-msg            # Conventional commit validation
│   ├── pre-push              # Runs tests, blocks push to main
│   └── setup.sh              # Hook installation script
├── db/                       # Database client
│   ├── client.go
│   ├── accounts.go
│   ├── exchanges.go
│   ├── trades.go
│   ├── funding_payment.go
│   └── *_test.go
├── exchange/                 # Exchange integrations
│   ├── client.go             # Factory for exchange clients
│   ├── iface/                # Interfaces
│   ├── hyperliquid/
│   ├── drift/
│   └── lighter/
├── models/                   # Data structures
│   ├── account.go
│   ├── exchange.go
│   ├── trade.go
│   ├── position.go
│   └── funding_payment.go
├── logger/                   # Structured logging
├── .coverage-baseline        # Coverage ratchet baseline
├── go.mod
└── README.md
```

## Running Tests

```bash
# All tests
go test ./...

# Verbose output
go test -v ./...

# Specific package
go test -v ./db/...
go test -v ./exchange/hyperliquid/...

# With coverage
go test -cover ./...

# Detailed coverage report
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out        # Summary
go tool cover -html=coverage.out        # HTML report in browser
```

## Test Coverage

This repo uses **coverage ratcheting** - coverage can only go up, never down.

- Baseline is stored in `.coverage-baseline`
- PRs that reduce coverage are blocked
- When you improve coverage, update the baseline in your PR

```
PR reduces coverage:   60.5% → 58%   ❌ Blocked
PR maintains coverage: 60.5% → 60.5% ✓ Allowed
PR improves coverage:  60.5% → 65%   ✓ Update baseline in PR
```

Check and update coverage:

```bash
# Check current coverage
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep total

# If coverage improved, update the baseline
echo "70.0" > .coverage-baseline
```

When updating the baseline, also update the coverage badge in this README:
```markdown
[![Coverage](https://img.shields.io/badge/coverage-70.0%25-yellow)]
```

Badge colors: `red` (<50%), `yellow` (50-80%), `green` (>80%)

## Using This Library

Add to your Go module:

```bash
go get github.com/zif-terminal/lib@latest
```

Or pin to a specific version:

```bash
go get github.com/zif-terminal/lib@v1.5.0
```

## Dependencies

- `github.com/machinebox/graphql` - GraphQL client
- `github.com/google/uuid` - UUID generation
- `github.com/stretchr/testify` - Test assertions
