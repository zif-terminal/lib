package models

// BalanceSnapshot represents a spot balance at a point in time.
// Balance is a decimal string (parsed as big.Rat by consumers) so that
// precision-sensitive math — e.g., the activity processor's interest
// derivation — doesn't go through float64.
type BalanceSnapshot struct {
	Asset       string `json:"asset"`
	Balance     string `json:"balance"`
	TimestampMs int64  `json:"timestamp_ms"` // Unix milliseconds of the snapshot
}

// HistoricalBalanceSnapshots represents a set of balance snapshots at a single timestamp.
type HistoricalBalanceSnapshots struct {
	TimestampMs int64
	Balances    []*BalanceSnapshot
}
