# `zif-lib` - Shared Go Library

## Purpose

Shared Go module providing:
1. **Database models** - Go structs matching the database schema
2. **Database client** - GraphQL client for Hasura with CRUD methods
3. **Exchange abstractions** - Interfaces and implementations (future)
4. **Common utilities** - Rate limiting, retry logic, error handling (future)

**This is a pure library** - no services, no main functions. It's imported by other services.

---

## Getting Started

### Clone and Setup

```bash
git clone <repo-url>
cd lib

# Configure Git hooks (one-time setup)
./scripts/setup-hooks.sh
```

### Run Tests

```bash
# Run all unit tests
./scripts/run_tests.sh

# Or directly with Go
go test ./...
```

---

## Package Structure

### `models/` - Data Structures

Pure data structures matching the database schema:

- **`models/exchange.go`**
  - `Exchange` - Exchange entity struct
  - `ExchangeInput` - Input struct for mutations

- **`models/account.go`**
  - `ExchangeAccount` - Exchange account entity struct
  - `ExchangeAccountInput` - Input struct for mutations

### `db/` - Database Client

GraphQL client for interacting with Hasura. Provides CRUD operations for exchanges and accounts.

#### Client Creation

```go
import "github.com/zif-terminal/lib/db"

client := db.NewClient(db.ClientConfig{
    URL:         "http://localhost:8080/v1/graphql",
    AdminSecret: "your-admin-secret",
})
```

#### Exchange Methods

- **`GetExchange(ctx, id)`** - Get single exchange by ID
- **`ListExchanges(ctx)`** - List all exchanges
- **`CreateExchange(ctx, input)`** - Create new exchange
- **`UpdateExchange(ctx, id, input)`** - Update existing exchange

#### Account Methods

- **`GetAccount(ctx, id)`** - Get single account by ID
- **`ListAccounts(ctx)`** - List all accounts
- **`CreateAccount(ctx, input)`** - Create new account
- **`UpdateAccount(ctx, id, input)`** - Update existing account
- **`DeleteAccount(ctx, id)`** - Delete account

#### Example Usage

```go
ctx := context.Background()

// Create an exchange
exchange, err := client.CreateExchange(ctx, &db.ExchangeInput{
    Name:        "hyperliquid",
    DisplayName: "Hyperliquid",
})

// Create an account
account, err := client.CreateAccount(ctx, &db.ExchangeAccountInput{
    ExchangeID:        exchange.ID,
    AccountIdentifier: "0x123...",
    AccountType:       "main",
})
```

---

## Testing

### Unit Tests

All database methods have unit tests with mocked GraphQL responses:

```bash
go test ./db/... -v
```

**Test Coverage:**
- 5 exchange tests (Get, List, Create, Update, Update NotFound)
- 10 account tests (Get, List, Create, Update, Delete, and NotFound variants)

### Git Pre-Push Hook

Unit tests run automatically before every `git push`:

- **Setup:** Run `./scripts/setup-hooks.sh` once (already done)
- **Automatic:** Tests run on every push
- **Bypass:** Use `git push --no-verify` (not recommended)

---

## Repository Structure

```
lib/
├── models/           # Data structures
│   ├── exchange.go
│   └── account.go
├── db/               # Database client
│   ├── client.go     # GraphQL client wrapper
│   ├── exchanges.go  # Exchange CRUD methods
│   ├── accounts.go   # Account CRUD methods
│   ├── exchanges_test.go
│   └── accounts_test.go
├── scripts/
│   ├── run_tests.sh      # Test runner script
│   └── setup-hooks.sh    # Git hooks setup
├── hooks/
│   └── pre-push          # Pre-push hook (runs tests)
├── go.mod
└── README.md
```

---

## Dependencies

- `github.com/machinebox/graphql` - GraphQL client library
