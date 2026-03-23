package models

// BalanceSnapshot represents a current spot balance
type BalanceSnapshot struct {
	Asset       string  `json:"asset"`
	Balance     float64 `json:"balance"`
	TimestampMs int64   `json:"timestamp_ms"` // Unix milliseconds of the snapshot
}

// HistoricalBalanceSnapshots represents a set of balance snapshots at a single timestamp.
type HistoricalBalanceSnapshots struct {
	TimestampMs int64
	Balances    []*BalanceSnapshot
}
