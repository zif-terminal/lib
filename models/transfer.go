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
)

// Transfer represents a unified transfer event (deposit, withdrawal, interest, etc.)
// This is a superset that combines deposits table + interest_payments table
type Transfer struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Type              string    `json:"type"`   // TypeDeposit, TypeWithdraw, TypeInterest, etc.
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"` // Always positive; direction determined by Type
	Timestamp         time.Time `json:"timestamp"`
}

// TransferFilter represents filtering options for listing transfers
type TransferFilter struct {
	ExchangeAccountIDs []uuid.UUID
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp
func (t *Transfer) UnmarshalJSON(data []byte) error {
	type Alias Transfer
	aux := &struct {
		Timestamp interface{} `json:"timestamp"`
		Amount    interface{} `json:"amount"`
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

	return nil
}
