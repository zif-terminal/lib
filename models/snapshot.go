package models

// PositionSnapshot represents a current open position (perp or spot)
type PositionSnapshot struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`              // "long" or "short"
	Size             float64 `json:"size"`              // Position size (signed for perps)
	EntryPrice       float64 `json:"entry_price"`       // Average entry price
	MarkPrice        float64 `json:"mark_price"`        // Current mark/oracle price
	LiquidationPrice float64 `json:"liquidation_price"` // 0 if not available
	UnrealizedPnl    float64 `json:"unrealized_pnl"`
	Leverage         float64 `json:"leverage"`
	MarketType       string  `json:"market_type"` // "perp" or "spot"
}

// BalanceSnapshot represents a current spot balance
type BalanceSnapshot struct {
	Asset       string  `json:"asset"`
	Balance     float64 `json:"balance"`
	TimestampMs int64   `json:"timestamp_ms"` // Unix milliseconds of the snapshot
}

// OpenOrder represents a currently active order
type OpenOrder struct {
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"` // "buy" or "sell"
	Size       float64 `json:"size"`
	Price      float64 `json:"price"`
	OrderType  string  `json:"order_type"`
	ReduceOnly bool    `json:"reduce_only"`
}

// AccountValueSnapshot represents the total account value summary
type AccountValueSnapshot struct {
	TotalEquity    float64 `json:"total_equity"`
	FreeCollateral float64 `json:"free_collateral"`
	MarginUsed     float64 `json:"margin_used"`
}
