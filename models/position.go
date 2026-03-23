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
		StartTime interface{} `json:"start_time"`
		EndTime   interface{} `json:"end_time"`
		Quantity  interface{} `json:"quantity"`
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
	StartTime         int64  // Unix ms
	EndTime           int64  // 0 if open
}

// PositionEvent links a position to a source event (trade, transfer, funding)
type PositionEvent struct {
	ID         uuid.UUID `json:"id"`
	PositionID uuid.UUID `json:"position_id"`
	EventID    uuid.UUID `json:"event_id"`
	EventType  string    `json:"event_type"`  // "trade", "transfer", "settlement", "funding"
	Direction  string    `json:"direction"`   // "entry", "exit", "received", "paid"
	Quantity   string    `json:"quantity"`
	CreatedAt  time.Time `json:"created_at"`
}

// PositionEventInput for batch inserts
type PositionEventInput struct {
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
