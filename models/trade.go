package models

import (
	"encoding/json"
	"fmt"
	"strings"
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

// UnmarshalJSON custom unmarshaler to handle TIMESTAMP without timezone and NUMERIC as numbers
func (t *Trade) UnmarshalJSON(data []byte) error {
	type Alias Trade
	aux := &struct {
		Timestamp string      `json:"timestamp"`
		CreatedAt string      `json:"created_at"`
		Price     interface{} `json:"price"`     // Can be string or number
		Quantity  interface{} `json:"quantity"`  // Can be string or number
		Fee       interface{} `json:"fee"`       // Can be string or number
		*Alias
	}{
		Alias: (*Alias)(t),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Parse timestamp (TIMESTAMP without timezone from PostgreSQL)
	if aux.Timestamp != "" {
		// Try RFC3339 first, then try without timezone
		ts, err := time.Parse(time.RFC3339, aux.Timestamp)
		if err != nil {
			// Try parsing as TIMESTAMP without timezone (YYYY-MM-DDTHH:MM:SS)
			ts, err = time.Parse("2006-01-02T15:04:05", strings.TrimSpace(aux.Timestamp))
			if err != nil {
				return err
			}
		}
		t.Timestamp = ts
	}

	// Parse created_at
	if aux.CreatedAt != "" {
		ca, err := time.Parse(time.RFC3339, aux.CreatedAt)
		if err != nil {
			ca, err = time.Parse("2006-01-02T15:04:05", strings.TrimSpace(aux.CreatedAt))
			if err != nil {
				return err
			}
		}
		t.CreatedAt = ca
	}

	// Convert NUMERIC fields (can be number or string) to string
	if aux.Price != nil {
		t.Price = convertToString(aux.Price)
	}
	if aux.Quantity != nil {
		t.Quantity = convertToString(aux.Quantity)
	}
	if aux.Fee != nil {
		t.Fee = convertToString(aux.Fee)
	}

	return nil
}

// convertToString converts numeric or string values to string
func convertToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		// GraphQL NUMERIC comes as float64
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.18f", val), "0"), ".")
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	default:
		return fmt.Sprintf("%v", val)
	}
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
