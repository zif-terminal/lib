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
// Uses Hasura relationship to fetch nested Exchange object
type ExchangeAccount struct {
	ID                  string          `json:"id" db:"id"`
	UserID              string          `json:"user_id" db:"user_id"`
	Exchange            *Exchange       `json:"exchange"` // Nested via Hasura relationship
	AccountIdentifier   string          `json:"account_identifier" db:"account_identifier"`
	AccountType         string          `json:"account_type" db:"account_type"` // "main", "sub_account", "vault" - FK to exchange_account_types.code
	AccountTypeMetadata json.RawMessage `json:"account_type_metadata" db:"account_type_metadata"` // JSONB
	WalletID            *string         `json:"wallet_id,omitempty" db:"wallet_id"`               // FK to wallets.id
	Status              string          `json:"status" db:"status"`                               // "active", "needs_token", "disabled"
	SyncEnabled         bool            `json:"sync_enabled" db:"sync_enabled"`
	ProcessingEnabled   bool            `json:"processing_enabled" db:"processing_enabled"`
	DetectedAt          *string         `json:"detected_at,omitempty" db:"detected_at"`           // When account was detected
	LastSyncedAt        *string         `json:"last_synced_at,omitempty" db:"last_synced_at"`     // When account was last synced
	Tags                []string        `json:"tags" db:"tags"`
}

// ExchangeAccountInput is used for GraphQL mutations
type ExchangeAccountInput struct {
	UserID              string          `json:"user_id"`
	ExchangeID          string          `json:"exchange_id"`
	AccountIdentifier   string          `json:"account_identifier"`
	AccountType         string          `json:"account_type"` // Uses code string ('main', 'sub_account', 'vault')
	AccountTypeMetadata json.RawMessage `json:"account_type_metadata,omitempty"`
	WalletID            *string         `json:"wallet_id,omitempty"`
	Status              string          `json:"status,omitempty"` // "active", "needs_token", "disabled"
}

// DiscoverableAccount represents an account discovered from an exchange
// Used by the DiscoverAccounts interface method
type DiscoverableAccount struct {
	AccountIdentifier string                 `json:"account_identifier"` // The ID to use for syncing (subaccount address/pubkey)
	AccountType       string                 `json:"account_type"`       // "main", "sub_account", "vault"
	Name              string                 `json:"name"`               // Display name (e.g., "Main Account", "Trading Sub")
	Metadata          map[string]interface{} `json:"metadata,omitempty"` // Exchange-specific extra data
}
