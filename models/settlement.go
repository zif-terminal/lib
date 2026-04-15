package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Settlement represents a PnL settlement event.
// On Drift, PnL from trade closes and accrued funding is settled
// separately by keeper bots.
type Settlement struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`         // Settlement asset (e.g., "USDC")
	Amount            string    `json:"amount"`        // Signed: positive = credit, negative = debit
	Market            string    `json:"market"`        // Perp market that triggered settlement (e.g., "SOL-PERP")
	Timestamp         time.Time `json:"timestamp"`
	SettlementID      string    `json:"settlement_id"` // Unique identifier
	ExternalID        string    `json:"external_id"`   // Native exchange ID for dedup (backfilled from settlement_id)
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp and NUMERIC amount
func (s *Settlement) UnmarshalJSON(data []byte) error {
	type Alias Settlement
	aux := &struct {
		Timestamp interface{} `json:"timestamp"`
		Amount    interface{} `json:"amount"`
		*Alias
	}{
		Alias: (*Alias)(s),
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
		case string:
			_, err := fmt.Sscanf(v, "%d", &unixMillis)
			if err != nil {
				return fmt.Errorf("failed to parse timestamp: %w", err)
			}
		default:
			return fmt.Errorf("unexpected timestamp type: %T", aux.Timestamp)
		}
		s.Timestamp = time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
	}

	if aux.Amount != nil {
		s.Amount = convertToString(aux.Amount)
	}

	return nil
}

// SettlementInput represents input for creating a settlement record
type SettlementInput struct {
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"`
	Market            string    `json:"market"`
	Timestamp         time.Time `json:"timestamp"`
	SettlementID      string    `json:"settlement_id"`
	ExternalID        string    `json:"external_id"` // Native exchange ID for dedup (defaults to settlement_id)
}

// SettlementFilter represents filtering options for listing settlements
type SettlementFilter struct {
	ExchangeAccountIDs []uuid.UUID
}
