package models

import (
	"encoding/json"
)

// AccountType represents an account type in the database
// Matches the 'exchange_account_types' table schema
type AccountType struct {
	Code string `json:"code" db:"code"`
}

// ExchangeAccount represents a user's account on an exchange in the database
// Matches the 'exchange_accounts' table schema
type ExchangeAccount struct {
	ID                  string          `json:"id" db:"id"`
	UserID              string          `json:"user_id" db:"user_id"`
	ExchangeID          string          `json:"exchange_id" db:"exchange_id"`
	AccountIdentifier   string          `json:"account_identifier" db:"account_identifier"`
	AccountType         string          `json:"account_type" db:"account_type"` // "main", "sub_account", "vault" - FK to exchange_account_types.code
	AccountTypeMetadata json.RawMessage `json:"account_type_metadata" db:"account_type_metadata"` // JSONB
}

// ExchangeAccountInput is used for GraphQL mutations
type ExchangeAccountInput struct {
	UserID              string          `json:"user_id"`
	ExchangeID          string          `json:"exchange_id"`
	AccountIdentifier   string          `json:"account_identifier"`
	AccountType         string          `json:"account_type"` // Uses code string ('main', 'sub_account', 'vault')
	AccountTypeMetadata json.RawMessage `json:"account_type_metadata,omitempty"`
}
