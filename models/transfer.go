package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Transfer type constants
const (
	TypeDeposit   = "deposit"
	TypeWithdraw  = "withdraw"
	TypeIFStake   = "if_stake"
	TypeIFUnstake = "if_unstake"
	TypeInterest  = "interest"
	TypeReward    = "reward"
	TypeFunding   = "funding"
)

// Transfer represents a unified transfer event (deposit, withdrawal, interest, etc.)
// Maps to the 'transfers' table
type Transfer struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Type              string    `json:"type"`   // TypeDeposit, TypeWithdraw, TypeInterest, etc.
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"` // Always positive; direction determined by Type
	Timestamp         time.Time `json:"timestamp"`
	CostBasis         string            `json:"cost_basis"`          // USD cost basis per unit (from user_cost_basis on deposits)
	Metadata          map[string]string `json:"metadata,omitempty"`  // Extensible metadata (e.g., market, payment_id for funding)
}

// TransferFilter represents filtering options for listing transfers
type TransferFilter struct {
	ExchangeAccountIDs []uuid.UUID
}

// TransferInput represents input for creating a transfer record
type TransferInput struct {
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Type              string    `json:"type"`
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"`
	Timestamp         time.Time `json:"timestamp"`
	CostBasis         string            `json:"cost_basis,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp
func (t *Transfer) UnmarshalJSON(data []byte) error {
	type Alias Transfer
	aux := &struct {
		Timestamp interface{} `json:"timestamp"`
		Amount    interface{} `json:"amount"`
		CostBasis interface{} `json:"cost_basis"`
		*Alias
	}{
		Alias: (*Alias)(t),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.Timestamp != nil {
		var unixMillis int64
		switch v := aux.Timestamp.(type) {
		case float64:
			unixMillis = int64(v)
		case int64:
			unixMillis = v
		case int:
			unixMillis = int64(v)
		case string:
			var err error
			unixMillis, err = parseInt64(v)
			if err != nil {
				return fmt.Errorf("failed to parse timestamp: %w", err)
			}
		default:
			return fmt.Errorf("unexpected timestamp type: %T", aux.Timestamp)
		}
		t.Timestamp = time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
	}

	if aux.Amount != nil {
		t.Amount = convertToString(aux.Amount)
	}

	if aux.CostBasis != nil {
		t.CostBasis = convertToString(aux.CostBasis)
	}

	return nil
}
