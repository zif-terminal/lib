package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Deposit represents a deposit or withdrawal record in the database
// Matches the 'deposits' table schema
type Deposit struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Direction         string    `json:"direction"`       // "deposit" or "withdraw"
	Amount            string    `json:"amount"`          // Using string for precision (NUMERIC in DB)
	UserCostBasis     string    `json:"user_cost_basis"` // Using string for precision (NUMERIC in DB)
	Timestamp         time.Time `json:"timestamp"`
	DepositID         string    `json:"deposit_id"`
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp (Unix milliseconds) and NUMERIC as numbers
func (d *Deposit) UnmarshalJSON(data []byte) error {
	type Alias Deposit
	aux := &struct {
		Timestamp     interface{} `json:"timestamp"`       // Can be number (Unix milliseconds) or string
		Amount        interface{} `json:"amount"`          // Can be string or number
		UserCostBasis interface{} `json:"user_cost_basis"` // Can be string or number
		*Alias
	}{
		Alias: (*Alias)(d),
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
		d.Timestamp = time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
	}

	// Convert NUMERIC fields (can be number or string) to string
	if aux.Amount != nil {
		d.Amount = convertToString(aux.Amount)
	}
	if aux.UserCostBasis != nil {
		d.UserCostBasis = convertToString(aux.UserCostBasis)
	}

	return nil
}

// DepositInput represents input for creating a deposit
// Used for GraphQL mutations
type DepositInput struct {
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Direction         string    `json:"direction"`       // "deposit" or "withdraw"
	Amount            string    `json:"amount"`
	UserCostBasis     string    `json:"user_cost_basis"`
	Timestamp         time.Time `json:"timestamp"`
	DepositID         string    `json:"deposit_id"`
}

// DepositFilter represents filtering options for listing deposits
type DepositFilter struct {
	ExchangeAccountIDs []uuid.UUID // Empty slice = all accounts, non-empty = filter by these IDs
	Assets             []string    // Optional: filter by asset names
	Directions         []string    // Optional: ["deposit"], ["withdraw"], or both
}
