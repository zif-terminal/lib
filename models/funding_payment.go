package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// FundingPayment represents a funding payment record in the database
// Matches the 'funding_payments' table schema
type FundingPayment struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	BaseAsset         string    `json:"base_asset"`
	QuoteAsset        string    `json:"quote_asset"`
	Amount            string    `json:"amount"` // Using string for precision (NUMERIC in DB), signed: positive = received, negative = paid
	Timestamp         time.Time `json:"timestamp"`
	PaymentID         string    `json:"payment_id"`
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamp (Unix milliseconds) and NUMERIC as numbers
func (f *FundingPayment) UnmarshalJSON(data []byte) error {
	type Alias FundingPayment
	aux := &struct {
		Timestamp interface{} `json:"timestamp"` // Can be number (Unix milliseconds) or string
		Amount    interface{} `json:"amount"`   // Can be string or number
		*Alias
	}{
		Alias: (*Alias)(f),
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
		f.Timestamp = time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
	}

	// Convert NUMERIC field (can be number or string) to string
	if aux.Amount != nil {
		f.Amount = convertToString(aux.Amount)
	}

	return nil
}

// FundingPaymentInput represents input for creating a funding payment
// Used for GraphQL mutations
type FundingPaymentInput struct {
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	BaseAsset         string    `json:"base_asset"`
	QuoteAsset        string    `json:"quote_asset"`
	Amount            string    `json:"amount"`
	Timestamp         time.Time `json:"timestamp"`
	PaymentID         string    `json:"payment_id"`
}
