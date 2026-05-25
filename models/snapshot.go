package models

// BalanceSnapshot represents a spot balance at a point in time.
// Balance is a decimal string (parsed as big.Rat by consumers) so that
// precision-sensitive math — e.g., the activity processor's interest
// derivation — doesn't go through float64.
//
// OraclePrice and UsdValue are optional decimal strings populated by exchange
// clients whose APIs expose per-asset pricing (Hyperliquid spotMetaAndAssetCtxs,
// stablecoin-only entries with implicit price=1, …). Both must be set together
// or both left nil — a snapshot with a price but no USD value (or vice versa)
// is treated as a programming error by the snapshot syncer.
type BalanceSnapshot struct {
	Asset       string  `json:"asset"`
	Balance     string  `json:"balance"`
	OraclePrice *string `json:"oracle_price,omitempty"`
	UsdValue    *string `json:"usd_value,omitempty"`
	TimestampMs int64   `json:"timestamp_ms"` // Unix milliseconds of the snapshot
}

// HistoricalBalanceSnapshots represents a set of balance snapshots at a single timestamp.
type HistoricalBalanceSnapshots struct {
	TimestampMs int64
	Balances    []*BalanceSnapshot
}
