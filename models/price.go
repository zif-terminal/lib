package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PriceRecord represents a price observation from an exchange (e.g., oracle price at fill time).
// Exchange clients return these alongside events — the denomination is declared by the exchange,
// not assumed (Drift oracles are in USDC, other exchanges may differ).
type PriceRecord struct {
	Asset        string
	Denomination string
	Timestamp    time.Time
	Price        string
	Source       string
}

// PriceInput is the input for writing to price_cache via GraphQL.
type PriceInput struct {
	Asset        string
	Denomination string
	TimestampMs  int64
	Price        string
	Source       string
}

// Price represents a row from the price_cache table.
type Price struct {
	Asset        string `json:"asset"`
	Denomination string `json:"denomination"`
	Timestamp    int64  `json:"timestamp"`
	Price        string `json:"price"`
	Source       string `json:"source"`
}

// UnmarshalJSON handles Hasura returning NUMERIC fields as JSON numbers.
func (p *Price) UnmarshalJSON(data []byte) error {
	type Alias Price
	aux := &struct {
		Price interface{} `json:"price"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.Price != nil {
		switch val := aux.Price.(type) {
		case string:
			p.Price = val
		case float64:
			p.Price = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.18f", val), "0"), ".")
		default:
			p.Price = fmt.Sprintf("%v", val)
		}
	}

	return nil
}
