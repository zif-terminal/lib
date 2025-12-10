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
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp (Unix milliseconds) and NUMERIC as numbers
func (t *Trade) UnmarshalJSON(data []byte) error {
	type Alias Trade
	aux := &struct {
		Timestamp interface{} `json:"timestamp"` // Can be number (Unix milliseconds) or string
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

	// Parse timestamp (BIGINT Unix milliseconds from PostgreSQL)
	if aux.Timestamp != nil {
		var unixMillis int64
		switch v := aux.Timestamp.(type) {
		case float64:
			// JSON numbers come as float64
			unixMillis = int64(v)
		case int64:
			unixMillis = v
		case int:
			unixMillis = int64(v)
		case string:
			// Try parsing as number string first
			var err error
			unixMillis, err = parseInt64(v)
			if err != nil {
				return fmt.Errorf("failed to parse timestamp: %w", err)
			}
		default:
			return fmt.Errorf("unexpected timestamp type: %T", aux.Timestamp)
		}
		// Convert Unix milliseconds to time.Time
		t.Timestamp = time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
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

// parseInt64 parses a string to int64
func parseInt64(s string) (int64, error) {
	var result int64
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
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
