package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SpotBalanceSnapshot represents a point-in-time spot balance for one asset (DB model).
type SpotBalanceSnapshot struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Balance           string    `json:"balance"`
	Timestamp         time.Time `json:"timestamp"`
}

// UnmarshalJSON handles Hasura returning BIGINT timestamp as number and NUMERIC as numbers.
func (s *SpotBalanceSnapshot) UnmarshalJSON(data []byte) error {
	type Alias SpotBalanceSnapshot
	aux := &struct {
		Timestamp interface{} `json:"timestamp"`
		Balance   interface{} `json:"balance"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.Timestamp != nil {
		ts, err := parseTimestamp(aux.Timestamp)
		if err != nil {
			return fmt.Errorf("failed to parse spot balance snapshot timestamp: %w", err)
		}
		s.Timestamp = ts
	}
	if aux.Balance != nil {
		s.Balance = convertToString(aux.Balance)
	}

	return nil
}

// SpotBalanceSnapshotInput is input for creating a snapshot record.
type SpotBalanceSnapshotInput struct {
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Balance           string    `json:"balance"`
	Timestamp         time.Time `json:"timestamp"`
}
