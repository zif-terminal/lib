package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// InterestPayment represents a derived interest record (DB model).
type InterestPayment struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"`
	OraclePrice       string    `json:"oracle_price"`
	USDValue          string    `json:"usd_value"`
	Timestamp         time.Time `json:"timestamp"`
	SnapshotFrom      int64     `json:"snapshot_from"`
	SnapshotTo        int64     `json:"snapshot_to"`
	IsApproximate     bool      `json:"is_approximate"`
}

// UnmarshalJSON handles Hasura returning BIGINT as number and NUMERIC as numbers.
func (ip *InterestPayment) UnmarshalJSON(data []byte) error {
	type Alias InterestPayment
	aux := &struct {
		Timestamp    interface{} `json:"timestamp"`
		SnapshotFrom interface{} `json:"snapshot_from"`
		SnapshotTo   interface{} `json:"snapshot_to"`
		Amount       interface{} `json:"amount"`
		OraclePrice  interface{} `json:"oracle_price"`
		USDValue     interface{} `json:"usd_value"`
		*Alias
	}{
		Alias: (*Alias)(ip),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.Timestamp != nil {
		ts, err := parseTimestamp(aux.Timestamp)
		if err != nil {
			return fmt.Errorf("failed to parse interest payment timestamp: %w", err)
		}
		ip.Timestamp = ts
	}
	if aux.SnapshotFrom != nil {
		ip.SnapshotFrom = parseIntField(aux.SnapshotFrom)
	}
	if aux.SnapshotTo != nil {
		ip.SnapshotTo = parseIntField(aux.SnapshotTo)
	}
	if aux.Amount != nil {
		ip.Amount = convertToString(aux.Amount)
	}
	if aux.OraclePrice != nil {
		ip.OraclePrice = convertToString(aux.OraclePrice)
	}
	if aux.USDValue != nil {
		ip.USDValue = convertToString(aux.USDValue)
	}

	return nil
}

// InterestPaymentInput is input for creating an interest payment record.
type InterestPaymentInput struct {
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"`
	OraclePrice       string    `json:"oracle_price"`
	USDValue          string    `json:"usd_value"`
	Timestamp         time.Time `json:"timestamp"`
	SnapshotFrom      int64     `json:"snapshot_from"`
	SnapshotTo        int64     `json:"snapshot_to"`
	IsApproximate     bool      `json:"is_approximate"`
}
