package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// InterestPayment represents a derived interest payment from balance reconciliation
// Matches the 'interest_payments' table schema
type InterestPayment struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"`        // signed: positive=earned, negative=charged
	OraclePrice       string    `json:"oracle_price"`  // USD price at reconciliation time
	USDValue          string    `json:"usd_value"`     // amount * oracle_price
	Timestamp         time.Time `json:"timestamp"`     // midpoint of interval
	SnapshotFrom      int64     `json:"snapshot_from"` // start snapshot timestamp (Unix ms)
	SnapshotTo        int64     `json:"snapshot_to"`   // end snapshot timestamp (Unix ms)
	IsApproximate     bool      `json:"is_approximate"` // true for USDC (perp fees/funding affect it)
	CreatedAt         time.Time `json:"created_at"`
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp (Unix milliseconds)
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
		unixMillis, err := parseInterfaceToInt64(aux.Timestamp)
		if err != nil {
			return fmt.Errorf("failed to parse timestamp: %w", err)
		}
		ip.Timestamp = time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
	}

	if aux.SnapshotFrom != nil {
		v, err := parseInterfaceToInt64(aux.SnapshotFrom)
		if err != nil {
			return fmt.Errorf("failed to parse snapshot_from: %w", err)
		}
		ip.SnapshotFrom = v
	}

	if aux.SnapshotTo != nil {
		v, err := parseInterfaceToInt64(aux.SnapshotTo)
		if err != nil {
			return fmt.Errorf("failed to parse snapshot_to: %w", err)
		}
		ip.SnapshotTo = v
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

// parseInterfaceToInt64 converts a JSON value (float64, string, int) to int64
func parseInterfaceToInt64(v interface{}) (int64, error) {
	switch val := v.(type) {
	case float64:
		return int64(val), nil
	case int64:
		return val, nil
	case int:
		return int64(val), nil
	case string:
		return parseInt64(val)
	default:
		return 0, fmt.Errorf("unexpected type: %T", v)
	}
}

// InterestPaymentInput represents input for creating an interest payment record
type InterestPaymentInput struct {
	ExchangeAccountID uuid.UUID
	Asset             string
	Amount            string // signed decimal string
	OraclePrice       string
	USDValue          string
	Timestamp         time.Time
	SnapshotFrom      int64
	SnapshotTo        int64
	IsApproximate     bool
}
