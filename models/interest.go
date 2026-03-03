package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SpotBalanceSnapshot represents a point-in-time spot balance capture
// Matches the 'spot_balance_snapshots' table schema
type SpotBalanceSnapshot struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Balance           string    `json:"balance"`     // signed: positive=long, negative=short/borrow
	OraclePrice       string    `json:"oracle_price"` // USD price at snapshot time
	USDValue          string    `json:"usd_value"`   // balance * oracle_price
	Timestamp         time.Time `json:"timestamp"`   // when snapshot was taken
	CreatedAt         time.Time `json:"created_at"`
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp (Unix milliseconds)
func (s *SpotBalanceSnapshot) UnmarshalJSON(data []byte) error {
	type Alias SpotBalanceSnapshot
	aux := &struct {
		Timestamp   interface{} `json:"timestamp"`
		Balance     interface{} `json:"balance"`
		OraclePrice interface{} `json:"oracle_price"`
		USDValue    interface{} `json:"usd_value"`
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
		s.Timestamp = time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
	}

	if aux.Balance != nil {
		s.Balance = convertToString(aux.Balance)
	}
	if aux.OraclePrice != nil {
		s.OraclePrice = convertToString(aux.OraclePrice)
	}
	if aux.USDValue != nil {
		s.USDValue = convertToString(aux.USDValue)
	}

	return nil
}

// SpotBalanceSnapshotInput represents input for creating a spot balance snapshot
type SpotBalanceSnapshotInput struct {
	ExchangeAccountID uuid.UUID
	Asset             string
	Balance           string // signed decimal string
	OraclePrice       string // optional, "0" if unknown
	USDValue          string // optional, "0" if unknown
	Timestamp         time.Time
}

// InterestPayment represents a derived interest payment from balance reconciliation
// Matches the 'interest_payments' table schema
type InterestPayment struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"`      // signed: positive=earned, negative=charged
	OraclePrice       string    `json:"oracle_price"` // USD price at reconciliation time
	USDValue          string    `json:"usd_value"`   // amount * oracle_price
	Timestamp         time.Time `json:"timestamp"`   // midpoint of interval
	SnapshotFrom      int64     `json:"snapshot_from"` // start snapshot timestamp (Unix ms)
	SnapshotTo        int64     `json:"snapshot_to"`   // end snapshot timestamp (Unix ms)
	IsApproximate     bool      `json:"is_approximate"` // true for USDC (perp fees/funding affect it)
	CreatedAt         time.Time `json:"created_at"`
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp (Unix milliseconds)
func (ip *InterestPayment) UnmarshalJSON(data []byte) error {
	type Alias InterestPayment
	aux := &struct {
		Timestamp interface{} `json:"timestamp"`
		Amount    interface{} `json:"amount"`
		OraclePrice interface{} `json:"oracle_price"`
		USDValue  interface{} `json:"usd_value"`
		*Alias
	}{
		Alias: (*Alias)(ip),
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
		ip.Timestamp = time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
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

