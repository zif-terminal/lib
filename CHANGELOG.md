# Changelog

## [Unreleased] - ExchangeAccount Model Refactoring

### Breaking Changes

#### `ExchangeAccount` Model - Nested Exchange Object

The `ExchangeAccount` model has been refactored to expose a nested `Exchange` object instead of a flat `ExchangeID` string field.

**Before:**
```go
type ExchangeAccount struct {
    ID                string
    UserID            string
    ExchangeID        string  // UUID string
    AccountIdentifier string
    AccountType       string
    AccountTypeMetadata json.RawMessage
}
```

**After:**
```go
type ExchangeAccount struct {
    ID                string
    UserID            string
    Exchange          *Exchange  // Nested object (can be nil)
    AccountIdentifier string
    AccountType       string
    AccountTypeMetadata json.RawMessage
}
```

### Migration Guide

#### Accessing Exchange Information

**Before:**
```go
account, err := client.GetAccount(ctx, accountID)
if err != nil {
    return err
}

// Access exchange ID
exchangeID := account.ExchangeID

// To get exchange name, you needed a separate query
exchange, err := client.GetExchange(ctx, account.ExchangeID)
exchangeName := exchange.Name
```

**After:**
```go
account, err := client.GetAccount(ctx, accountID)
if err != nil {
    return err
}

// Access exchange information directly
if account.Exchange == nil {
    return errors.New("exchange not found")
}

exchangeID := account.Exchange.ID
exchangeName := account.Exchange.Name
displayName := account.Exchange.DisplayName
```

#### Checking for Exchange Presence

Always check for `nil` before accessing the `Exchange` field:

```go
account, err := client.GetAccount(ctx, accountID)
if err != nil {
    return err
}

if account.Exchange == nil {
    // Handle missing exchange relationship
    return errors.New("account has no associated exchange")
}

// Safe to access account.Exchange.*
```

#### Mutations (CreateAccount, UpdateAccount)

The `ExchangeAccountInput` struct remains unchanged - it still uses `ExchangeID` for mutations:

```go
input := &models.ExchangeAccountInput{
    ExchangeID:        "exchange-uuid-here",  // Still required for mutations
    AccountIdentifier: "0x123...",
    AccountType:       "main",
}

account, err := client.CreateAccount(ctx, input)
// account.Exchange is now populated automatically
if account.Exchange != nil {
    fmt.Printf("Created account for exchange: %s\n", account.Exchange.DisplayName)
}
```

### Benefits

- **Reduced API Calls**: Exchange information is fetched in a single query instead of requiring separate lookups
- **Better Type Safety**: Access to `Exchange` fields (`id`, `name`, `display_name`) directly from the account object
- **Consistent with GraphQL**: Leverages Hasura's relationship capabilities for nested data fetching

### Affected APIs

All methods that return `*ExchangeAccount` or `[]*ExchangeAccount`:

- `GetAccount(ctx, id string) (*ExchangeAccount, error)`
- `ListAccounts(ctx) ([]*ExchangeAccount, error)`
- `CreateAccount(ctx, input *ExchangeAccountInput) (*ExchangeAccount, error)`
- `UpdateAccount(ctx, id string, input *ExchangeAccountInput) (*ExchangeAccount, error)`

### Notes

- The database schema remains unchanged - `exchange_accounts.exchange_id` still exists as a foreign key
- GraphQL queries now automatically fetch the nested `exchange` relationship
- The `Exchange` field can be `nil` if the relationship is missing (e.g., data inconsistency)
