package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Position represents a position record (open or closed) in the database.
// Matches the 'positions' table schema.
type Position struct {
	ID                uuid.UUID        `json:"id"`
	ExchangeAccountID uuid.UUID        `json:"exchange_account_id"`
	Market            string           `json:"market"`       // "SOL-PERP", "SOL", "USDC", "wBTC"
	MarketType        string           `json:"market_type"`  // "perp" or "spot"
	Side              string           `json:"side"`         // "long" or "short"
	Status            string           `json:"status"`       // "open" or "closed"
	Quantity          string           `json:"quantity"`     // NUMERIC as string
	EntryPrice        string           `json:"entry_price"`  // weighted avg entry (USD)
	ExitPrice         *string          `json:"exit_price"`   // weighted avg exit; nil if open
	QuoteAsset        string           `json:"quote_asset"`  // what entry/exit prices are denominated in
	TotalFees         string           `json:"total_fees"`
	CumulativeFunding string           `json:"cumulative_funding"`
	StartTime         time.Time        `json:"start_time"`
	EndTime           *time.Time       `json:"end_time"`  // nil if open
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	ExchangeAccount   *ExchangeAccount `json:"exchange_account,omitempty"`
}

// UnmarshalJSON handles Hasura BIGINT timestamps and NUMERIC fields.
func (p *Position) UnmarshalJSON(data []byte) error {
	type Alias Position
	aux := &struct {
		StartTime         interface{} `json:"start_time"`
		EndTime           interface{} `json:"end_time"`
		Quantity          interface{} `json:"quantity"`
		EntryPrice        interface{} `json:"entry_price"`
		ExitPrice         interface{} `json:"exit_price"`
		TotalFees         interface{} `json:"total_fees"`
		CumulativeFunding interface{} `json:"cumulative_funding"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.StartTime != nil {
		ts, err := parseTimestamp(aux.StartTime)
		if err != nil {
			return fmt.Errorf("failed to parse start_time: %w", err)
		}
		p.StartTime = ts
	}

	if aux.EndTime != nil {
		ts, err := parseTimestamp(aux.EndTime)
		if err != nil {
			return fmt.Errorf("failed to parse end_time: %w", err)
		}
		p.EndTime = &ts
	}

	if aux.Quantity != nil {
		p.Quantity = convertToString(aux.Quantity)
	}
	if aux.EntryPrice != nil {
		p.EntryPrice = convertToString(aux.EntryPrice)
	}
	if aux.ExitPrice != nil {
		s := convertToString(aux.ExitPrice)
		p.ExitPrice = &s
	}
	if aux.TotalFees != nil {
		p.TotalFees = convertToString(aux.TotalFees)
	}
	if aux.CumulativeFunding != nil {
		p.CumulativeFunding = convertToString(aux.CumulativeFunding)
	}

	return nil
}

// PositionInput represents input for batch-inserting positions.
type PositionInput struct {
	ExchangeAccountID uuid.UUID
	Market            string
	MarketType        string
	Side              string
	Status            string // "open" or "closed"
	Quantity          string
	EntryPrice        string
	ExitPrice         string // "" if open
	TotalFees         string
	CumulativeFunding string
	QuoteAsset        string // What the entry/exit prices are denominated in
	StartTime         int64  // Unix ms
	EndTime           int64  // 0 if open
}

// PositionTrade links a position to a source event (trade, transfer, settlement)
type PositionTrade struct {
	ID         uuid.UUID `json:"id"`
	PositionID uuid.UUID `json:"position_id"`
	EventID    uuid.UUID `json:"event_id"`
	EventType  string    `json:"event_type"`  // "trade", "transfer", "settlement", "funding"
	Direction  string    `json:"direction"`   // "entry", "exit", "received", "paid"
	Quantity   string    `json:"quantity"`
	CreatedAt  time.Time `json:"created_at"`
}

// PositionTradeInput for batch inserts
type PositionTradeInput struct {
	PositionID uuid.UUID
	EventID    uuid.UUID
	EventType  string
	Direction  string
	Quantity   string
}

// PositionFilter represents filtering options for listing positions.
type PositionFilter struct {
	ExchangeAccountIDs []uuid.UUID
	Status             *string // "open" or "closed"
	MarketType         *string // "perp" or "spot"
	Market             *string
}
