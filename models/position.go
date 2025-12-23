package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Position represents a closed position record in the database
// Matches the 'positions' table schema
type Position struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	BaseAsset         string    `json:"base_asset"`
	QuoteAsset        string    `json:"quote_asset"`
	Side              string    `json:"side"` // "long" or "short"
	StartTime         time.Time `json:"start_time"`
	EndTime           time.Time `json:"end_time"`
	EntryAvgPrice     string    `json:"entry_avg_price"` // NUMERIC as string
	ExitAvgPrice      string    `json:"exit_avg_price"`  // NUMERIC as string
	TotalQuantity     string    `json:"total_quantity"`  // NUMERIC as string
	TotalFees         string    `json:"total_fees"`      // NUMERIC as string
	RealizedPnL       string    `json:"realized_pnl"`    // NUMERIC as string
}

// UnmarshalJSON custom unmarshaler to handle BIGINT timestamps and NUMERIC fields
func (p *Position) UnmarshalJSON(data []byte) error {
	type Alias Position
	aux := &struct {
		StartTime     interface{} `json:"start_time"`
		EndTime       interface{} `json:"end_time"`
		EntryAvgPrice interface{} `json:"entry_avg_price"`
		ExitAvgPrice  interface{} `json:"exit_avg_price"`
		TotalQuantity interface{} `json:"total_quantity"`
		TotalFees     interface{} `json:"total_fees"`
		RealizedPnL   interface{} `json:"realized_pnl"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Parse start_time (BIGINT Unix milliseconds)
	if aux.StartTime != nil {
		ts, err := parseTimestamp(aux.StartTime)
		if err != nil {
			return fmt.Errorf("failed to parse start_time: %w", err)
		}
		p.StartTime = ts
	}

	// Parse end_time (BIGINT Unix milliseconds)
	if aux.EndTime != nil {
		ts, err := parseTimestamp(aux.EndTime)
		if err != nil {
			return fmt.Errorf("failed to parse end_time: %w", err)
		}
		p.EndTime = ts
	}

	// Convert NUMERIC fields to string
	if aux.EntryAvgPrice != nil {
		p.EntryAvgPrice = convertToString(aux.EntryAvgPrice)
	}
	if aux.ExitAvgPrice != nil {
		p.ExitAvgPrice = convertToString(aux.ExitAvgPrice)
	}
	if aux.TotalQuantity != nil {
		p.TotalQuantity = convertToString(aux.TotalQuantity)
	}
	if aux.TotalFees != nil {
		p.TotalFees = convertToString(aux.TotalFees)
	}
	if aux.RealizedPnL != nil {
		p.RealizedPnL = convertToString(aux.RealizedPnL)
	}

	return nil
}

// parseTimestamp parses a timestamp from various formats (BIGINT Unix milliseconds)
func parseTimestamp(v interface{}) (time.Time, error) {
	var unixMillis int64
	switch val := v.(type) {
	case float64:
		unixMillis = int64(val)
	case int64:
		unixMillis = val
	case int:
		unixMillis = int64(val)
	case string:
		var err error
		unixMillis, err = parseInt64(val)
		if err != nil {
			return time.Time{}, fmt.Errorf("failed to parse timestamp string: %w", err)
		}
	default:
		return time.Time{}, fmt.Errorf("unexpected timestamp type: %T", v)
	}
	return time.Unix(0, unixMillis*int64(time.Millisecond)).UTC(), nil
}

// PositionInput represents input for creating a position
type PositionInput struct {
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	BaseAsset         string    `json:"base_asset"`
	QuoteAsset        string    `json:"quote_asset"`
	Side              string    `json:"side"`
	StartTime         time.Time `json:"start_time"`
	EndTime           time.Time `json:"end_time"`
	EntryAvgPrice     string    `json:"entry_avg_price"`
	ExitAvgPrice      string    `json:"exit_avg_price"`
	TotalQuantity     string    `json:"total_quantity"`
	TotalFees         string    `json:"total_fees"`
	RealizedPnL       string    `json:"realized_pnl"`
}

// PositionTrade represents a trade allocation for a position
// Matches the 'position_trades' junction table
type PositionTrade struct {
	PositionID           uuid.UUID `json:"position_id"`
	TradeID              uuid.UUID `json:"trade_id"`
	AllocationPercentage string    `json:"allocation_percentage"` // NUMERIC as string
	AllocatedQuantity    string    `json:"allocated_quantity"`    // NUMERIC as string
	AllocatedFees        string    `json:"allocated_fees"`        // NUMERIC as string
}

// UnmarshalJSON custom unmarshaler to handle NUMERIC fields
func (pt *PositionTrade) UnmarshalJSON(data []byte) error {
	type Alias PositionTrade
	aux := &struct {
		AllocationPercentage interface{} `json:"allocation_percentage"`
		AllocatedQuantity    interface{} `json:"allocated_quantity"`
		AllocatedFees        interface{} `json:"allocated_fees"`
		*Alias
	}{
		Alias: (*Alias)(pt),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.AllocationPercentage != nil {
		pt.AllocationPercentage = convertToString(aux.AllocationPercentage)
	}
	if aux.AllocatedQuantity != nil {
		pt.AllocatedQuantity = convertToString(aux.AllocatedQuantity)
	}
	if aux.AllocatedFees != nil {
		pt.AllocatedFees = convertToString(aux.AllocatedFees)
	}

	return nil
}

// PositionTradeInput represents input for creating a position trade link
type PositionTradeInput struct {
	PositionID           uuid.UUID `json:"position_id"`
	TradeID              uuid.UUID `json:"trade_id"`
	AllocationPercentage string    `json:"allocation_percentage"`
	AllocatedQuantity    string    `json:"allocated_quantity"`
	AllocatedFees        string    `json:"allocated_fees"`
}

// PositionFilter represents filtering options for listing positions
type PositionFilter struct {
	ExchangeAccountIDs []uuid.UUID
	BaseAsset          *string
	QuoteAsset         *string
	Side               *string // "long" or "short"
	StartTimeGte       *time.Time
	StartTimeLte       *time.Time
	EndTimeGte         *time.Time
	EndTimeLte         *time.Time
}
