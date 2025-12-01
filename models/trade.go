package models

import (
	"time"

	"github.com/google/uuid"
)

// Trade represents a trade record in the database
// Matches the 'trades' table schema
type Trade struct {
	ID                uuid.UUID `json:"id"`
	BaseAsset         string    `json:"base_asset"`
	QuoteAsset        string    `json:"quote_asset"`
	Side              string    `json:"side"` // "buy" or "sell"
	Price             string    `json:"price"` // Using string for precision (NUMERIC in DB)
	Quantity          string    `json:"quantity"` // Using string for precision (NUMERIC in DB)
	Timestamp         time.Time `json:"timestamp"`
	Fee               string    `json:"fee"` // Using string for precision (NUMERIC in DB)
	OrderID           string    `json:"order_id"`
	TradeID           string    `json:"trade_id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	CreatedAt         time.Time `json:"created_at"`
}

// TradeInput represents input for creating/updating a trade
// Used for GraphQL mutations
type TradeInput struct {
	BaseAsset         string    `json:"base_asset"`
	QuoteAsset        string    `json:"quote_asset"`
	Side              string    `json:"side"`
	Price             string    `json:"price"`
	Quantity          string    `json:"quantity"`
	Timestamp         time.Time `json:"timestamp"`
	Fee               string    `json:"fee"`
	OrderID           string    `json:"order_id"`
	TradeID           string    `json:"trade_id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
}

// TradeFilter represents filtering options for listing trades
type TradeFilter struct {
	ExchangeAccountIDs []uuid.UUID // Empty slice = all accounts, non-empty = filter by these IDs
}
