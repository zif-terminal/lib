package models

import (
	"time"
)

// Wallet represents a blockchain wallet in the database
type Wallet struct {
	ID             string     `json:"id" db:"id"`
	Address        string     `json:"address" db:"address"`
	Chain          string     `json:"chain" db:"chain"` // "solana", "ethereum"
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	LastDetectedAt *time.Time `json:"last_detected_at,omitempty" db:"last_detected_at"`
	Tags           []string   `json:"tags" db:"tags"`
}

// WalletInput is used for creating wallets via GraphQL mutations
type WalletInput struct {
	Address string `json:"address"`
	Chain   string `json:"chain"`
}
