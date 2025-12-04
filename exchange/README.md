# Exchange Interface

This package provides a standardized interface for integrating with cryptocurrency exchanges. It allows the `account_sync` service to fetch trades from multiple exchanges using a consistent API.

## Overview

The exchange interface consists of:

- **`ExchangeClient` interface**: Defines the contract all exchange implementations must satisfy
- **Exchange implementations**: Specific implementations for each exchange (e.g., `hyperliquid`, `lighter`, `drift`)
- **Contract tests**: Automated tests that verify implementations satisfy the interface contract
- **Error types**: Standardized error types (e.g., `RateLimitError`)

## Architecture

```
exchange/
├── client.go              # ExchangeClient interface
├── errors.go              # Error types (RateLimitError, etc.)
├── contract_test.go       # Contract test suite
├── README.md              # This file
├── hyperliquid/
│   ├── client.go          # Hyperliquid implementation
│   ├── types.go           # Hyperliquid-specific types
│   ├── client_test.go     # Unit tests (mocked HTTP)
│   └── integration_test.go # Integration tests (real API)
├── lighter/               # Future: Lighter implementation
└── drift/                 # Future: Drift implementation
```

## Interface Contract

All exchange implementations must implement the `ExchangeClient` interface:

```go
type ExchangeClient interface {
    // Name returns the exchange identifier (e.g., "hyperliquid", "lighter")
    Name() string

    // FetchTrades fetches trades for a given account since a specific timestamp
    // Returns trades as TradeInput (ready for database insertion), sorted by timestamp (oldest first)
    // ctx can be cancelled or have a timeout set by the caller (sync service)
    FetchTrades(
        ctx context.Context,
        account *models.ExchangeAccount,
        since time.Time,
    ) ([]*models.TradeInput, error)
}
```

### Requirements

1. **Name()**: Must return a non-empty string identifying the exchange
2. **FetchTrades()**: 
   - Must respect `context.Context` cancellation and timeouts
   - Must return trades sorted by timestamp (oldest first) for incremental syncing
   - Must filter trades where `timestamp >= since` (if `since` is not zero)
   - Must return `TradeInput` structs ready for database insertion
   - Must handle rate limits by returning `RateLimitError`
   - Must handle invalid accounts by returning an error

## Adding a New Exchange Implementation

Follow these steps to add a new exchange:

### 1. Create Exchange Directory

```bash
mkdir -p exchange/newexchange
cd exchange/newexchange
```

### 2. Implement the Client

Create `client.go`:

```go
package newexchange

import (
    "context"
    "net/http"
    "time"
    "github.com/google/uuid"
    "github.com/zif-terminal/lib/exchange"
    "github.com/zif-terminal/lib/models"
)

type Client struct {
    baseURL    string
    httpClient *http.Client
}

func NewClient() *Client {
    return &Client{
        baseURL:    "https://api.newexchange.com",
        httpClient: &http.Client{Timeout: 30 * time.Second},
    }
}

func (c *Client) Name() string {
    return "newexchange"
}

func (c *Client) FetchTrades(
    ctx context.Context,
    account *models.ExchangeAccount,
    since time.Time,
) ([]*models.TradeInput, error) {
    // 1. Check context cancellation
    if ctx.Err() != nil {
        return nil, ctx.Err()
    }

    // 2. Parse account ID to UUID
    accountUUID, err := uuid.Parse(account.ID)
    if err != nil {
        return nil, fmt.Errorf("invalid account ID: %w", err)
    }

    // 3. Extract credentials/identifier from account
    address := account.AccountIdentifier
    // Or extract from account.AccountTypeMetadata if needed

    // 4. Build API request
    // ... (exchange-specific logic)

    // 5. Make HTTP request with context
    req, err := http.NewRequestWithContext(ctx, "POST", url, body)
    // ...

    // 6. Check for rate limit (HTTP 429)
    if resp.StatusCode == http.StatusTooManyRequests {
        return nil, &exchange.RateLimitError{
            Exchange:   "newexchange",
            Message:    "rate limit exceeded",
            RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
        }
    }

    // 7. Parse response and transform to []*models.TradeInput
    // ... (exchange-specific parsing)

    // 8. Filter by since timestamp
    // ... (filter trades where timestamp >= since)

    // 9. Sort by timestamp (oldest first)
    sort.Slice(trades, func(i, j int) bool {
        return trades[i].Timestamp.Before(trades[j].Timestamp)
    })

    // 10. Return trades
    return trades, nil
}
```

### 3. Transform Exchange-Specific Format

Your implementation must transform the exchange's API response format to `models.TradeInput`:

```go
func transformTrade(apiTrade exchangeSpecificTrade, accountUUID uuid.UUID) (*models.TradeInput, error) {
    return &models.TradeInput{
        TradeID:          apiTrade.TradeID,        // Exchange-specific trade ID
        OrderID:         apiTrade.OrderID,         // Exchange-specific order ID
        BaseAsset:        normalizeAsset(...),     // Normalize asset symbol
        QuoteAsset:       normalizeAsset(...),     // Normalize asset symbol
        Side:             normalizeSide(...),       // Convert to "buy" or "sell"
        Price:            convertToString(...),     // Convert to string for precision
        Quantity:         convertToString(...),     // Convert to string for precision
        Fee:              convertToString(...),     // Convert to string for precision
        Timestamp:        parseTimestamp(...),      // Parse to time.Time
        ExchangeAccountID: accountUUID,
    }, nil
}
```

**Important transformations:**

- **Side**: Must normalize to `"buy"` or `"sell"` (e.g., "B"/"S", "LONG"/"SHORT" → "buy"/"sell")
- **Assets**: Must extract base and quote assets (e.g., "BTC-USDC" → base: "BTC", quote: "USDC")
- **Numeric fields**: Convert `price`, `quantity`, `fee` to strings for precision
- **Timestamp**: Parse exchange timestamp format to `time.Time`

### 4. Write Unit Tests

Create `client_test.go` with mocked HTTP server:

```go
package newexchange

import (
    "net/http"
    "net/http/httptest"
    "testing"
    // ...
)

func TestNewExchangeClient_FetchTrades_Success(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Mock API response
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"trades": [...]}`))
    }))
    defer server.Close()

    client := &Client{
        baseURL:    server.URL,
        httpClient: &http.Client{},
    }

    // Test FetchTrades...
}

func TestNewExchangeClient_RateLimit(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusTooManyRequests)
        w.Header().Set("Retry-After", "60")
    }))
    defer server.Close()

    // Test rate limit error...
}

func TestNewExchangeClient_ContextCancellation(t *testing.T) {
    // Test context cancellation...
}
```

### 5. Write Integration Tests

Create `integration_test.go`:

```go
// +build integration

package newexchange

import (
    "context"
    "os"
    "testing"
    "time"
    "github.com/zif-terminal/lib/exchange"
    "github.com/zif-terminal/lib/models"
)

func TestNewExchangeClient_Integration(t *testing.T) {
    // Skip if no test credentials
    testAddress := os.Getenv("NEWEXCHANGE_TEST_ADDRESS")
    if testAddress == "" {
        t.Skip("Skipping integration test: NEWEXCHANGE_TEST_ADDRESS not set")
    }

    client := NewClient()
    account := &models.ExchangeAccount{
        ID:                uuid.New().String(),
        AccountIdentifier: testAddress,
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    trades, err := client.FetchTrades(ctx, account, time.Time{})
    // Assertions...
}

func TestNewExchangeClient_Integration_Contract(t *testing.T) {
    testAddress := os.Getenv("NEWEXCHANGE_TEST_ADDRESS")
    if testAddress == "" {
        t.Skip("Skipping integration test: NEWEXCHANGE_TEST_ADDRESS not set")
    }

    contract := exchange.ExchangeClientContract{
        NewClient: func() exchange.ExchangeClient {
            return NewClient()
        },
        ValidAccount: &models.ExchangeAccount{
            ID:                uuid.New().String(),
            AccountIdentifier: testAddress,
        },
        InvalidAccount: &models.ExchangeAccount{
            ID:                uuid.New().String(),
            AccountIdentifier: "invalid",
        },
    }

    exchange.RunExchangeClientContractTests(t, contract)
}
```

### 6. Run Tests

```bash
# Unit tests (mocked HTTP)
go test ./exchange/newexchange

# Integration tests (real API)
go test -tags=integration ./exchange/newexchange

# Contract tests (via integration tests)
go test -tags=integration ./exchange/newexchange -run Contract
```

### 7. Test Checklist

Before submitting your implementation, ensure:

- [ ] `Name()` returns correct exchange identifier
- [ ] `FetchTrades()` respects context cancellation
- [ ] `FetchTrades()` respects context timeout
- [ ] `FetchTrades()` returns trades sorted by timestamp (oldest first)
- [ ] `FetchTrades()` filters by `since` timestamp correctly
- [ ] `FetchTrades()` handles rate limits (returns `RateLimitError`)
- [ ] `FetchTrades()` handles invalid accounts (returns error)
- [ ] Side normalization works correctly ("buy"/"sell")
- [ ] Asset normalization works correctly
- [ ] Numeric fields converted to strings correctly
- [ ] Timestamp parsing works correctly
- [ ] Unit tests pass (mocked HTTP)
- [ ] Integration tests pass (real API, if credentials available)
- [ ] Contract tests pass

## Testing Requirements

### Unit Tests (Required)

Unit tests must:
- Use `httptest.NewServer` to mock HTTP responses
- Test happy path (successful fetch)
- Test error cases (rate limit, invalid account, network errors)
- Test context cancellation/timeout
- Test filtering by `since` timestamp
- Test sorting (oldest first)
- **NOT** make real API calls

### Integration Tests (Optional but Recommended)

Integration tests:
- Must be tagged with `// +build integration`
- Must skip if test credentials not available (via environment variable)
- Should test against real exchange API
- Should run contract tests against real API
- Can be run with: `go test -tags=integration ./exchange/newexchange`

### Contract Tests

Contract tests verify your implementation satisfies the interface contract:

```go
contract := exchange.ExchangeClientContract{
    NewClient: func() exchange.ExchangeClient {
        return NewClient()
    },
    ValidAccount:   validAccount,
    InvalidAccount: invalidAccount,
}

exchange.TestExchangeClientContract(t, contract)
```

## Error Handling

### Rate Limit Errors

When the exchange API returns HTTP 429, return `RateLimitError`:

```go
if resp.StatusCode == http.StatusTooManyRequests {
    retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
    return nil, &exchange.RateLimitError{
        Exchange:   "newexchange",
        Message:    "rate limit exceeded",
        RetryAfter: retryAfter,
    }
}
```

The sync service will handle retries based on `RetryAfter`.

### Context Errors

Context cancellation/timeout errors are automatically handled by `http.NewRequestWithContext()`. Just check `ctx.Err()` at the start:

```go
if ctx.Err() != nil {
    return nil, ctx.Err()
}
```

### Invalid Account Errors

Return a descriptive error for invalid accounts:

```go
if account.AccountIdentifier == "" {
    return nil, fmt.Errorf("account identifier is required")
}
```

## Example: Hyperliquid Implementation

See `exchange/hyperliquid/` for a complete reference implementation:

- `client.go`: Full implementation with error handling
- `types.go`: Exchange-specific response types
- `client_test.go`: Comprehensive unit tests
- `integration_test.go`: Integration tests with contract tests

## Common Patterns

### Handling Credentials

**No credentials (address-only):**
```go
address := account.AccountIdentifier
```

**Credentials in metadata:**
```go
var metadata map[string]interface{}
json.Unmarshal(account.AccountTypeMetadata, &metadata)
apiKey := metadata["api_key"].(string)
```

### Parsing Timestamps

Exchange APIs return timestamps in various formats:

```go
// Unix timestamp (milliseconds)
timestamp := time.Unix(0, ms*int64(time.Millisecond))

// Unix timestamp (seconds)
timestamp := time.Unix(sec, 0)

// RFC3339 string
timestamp, err := time.Parse(time.RFC3339, str)
```

### Normalizing Side

```go
func normalizeSide(side string) string {
    side = strings.ToUpper(strings.TrimSpace(side))
    switch side {
    case "B", "BUY", "LONG":
        return "buy"
    case "S", "SELL", "SHORT":
        return "sell"
    default:
        return "buy" // Default fallback
    }
}
```

## Questions?

- Check `exchange/hyperliquid/` for a complete reference implementation
- Review `exchange/contract_test.go` for contract requirements
- See `exchange/errors.go` for error type definitions
