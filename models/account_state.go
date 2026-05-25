package models

import (
	"math/big"
	"strings"

	"github.com/google/uuid"
)

// EventEntry tracks a single event's contribution to a position (for FIFO allocation).
// This covers trades, transfers, interest, settlements, and rewards — any event
// that changes position quantity. The EventType field distinguishes them.
type EventEntry struct {
	EventID   uuid.UUID `json:"event_id"`
	EventType string    `json:"event_type,omitempty"` // "trade", "transfer", "interest", "settlement", "reward"
	Quantity  string    `json:"quantity"`              // How much this event contributed
	Price     string    `json:"price,omitempty"`       // Entry price for PnL computation
}

// TradeEntry is a deprecated alias for EventEntry. Use EventEntry instead.
type TradeEntry = EventEntry

// FundingEntry tracks a single funding payment linked to a position
type FundingEntry struct {
	FundingDBID uuid.UUID `json:"funding_db_id"`
	Amount      string    `json:"amount"`    // Signed: positive = received, negative = paid
	Timestamp   int64     `json:"timestamp"` // Unix ms
}

// PendingFundingEntry tracks a funding event waiting for a matching perp position to open.
// Held in-memory only; NOT persisted to the checkpoint. At the end of a processor run,
// any remaining entries indicate a fail-loud error — funding could not be matched.
type PendingFundingEntry struct {
	EventID   uuid.UUID
	Market    string
	Amount    string
	Timestamp int64
}

// AccountState represents the in-memory state of an account.
// Built by processing all transactions chronologically.
type AccountState struct {
	Assets          map[string]*AssetState    `json:"assets"`
	Positions       map[string]*PositionState `json:"positions"` // Open positions keyed by "marketType:baseAsset"
	ClosedPositions []*PositionState          `json:"closed_positions"`
	Trading         map[string]*TradingState  `json:"trading"`           // Keyed by quote asset (e.g., "USDC", "SOL")
	HasSeenSnapshot bool                      `json:"has_seen_snapshot"` // True after first snapshot baseline applied
	// PendingFunding is an in-memory buffer for funding events whose matching perp
	// position has not yet been opened. Flushed when the matching position opens.
	// Not JSON-serialized — the processor fails loudly at the end of the run if
	// this buffer is non-empty.
	PendingFunding []PendingFundingEntry `json:"-"`
}

// AssetState tracks the cumulative bookkeeping fields for a single asset
// (USDC, SOL, etc.). The asset's current Balance is NOT stored here — it is a
// derived view over the spot positions in AccountState, computed by
// AccountState.Balance(symbol). Positions are the single source of truth so
// the balance can never diverge from the position history.
type AssetState struct {
	CumulativeDeposits  string `json:"cumulative_deposits"`  // Sum of deposits
	CumulativeWithdraws string `json:"cumulative_withdraws"` // Sum of withdrawals
	CumulativeInterest  string `json:"cumulative_interest"`  // All interest (explicit transfers + snapshot-derived)
	NetSpotInflow       string `json:"net_spot_inflow"`      // Net balance change from spot trades (buys - sells)
}

// PositionState tracks in-memory position state (perp or spot), open or closed.
// A position is closed when EndTime > 0.
//
// The EventEntries slice is the immutable history of ENTRY events (deposits,
// buys on the current side, interest credits, etc.) for the position. It is
// never shrunk by FIFO consumption — instead, the running "available for
// matching" quantity is computed as (sum(EventEntries) - sum(ExitEntries)),
// which equals the Quantity field. ExitEntries is the immutable history of
// EXIT events (withdrawals, opposite-side trades, fees) that have been
// matched against the entries via FIFO. Both slices are written to the DB
// as position_events rows (direction=entry vs direction=exit).
type PositionState struct {
	Market             string         `json:"market"`              // e.g., "SOL" or "SOL-PERP"
	MarketType         string         `json:"market_type"`         // "perp" or "spot"
	Side               string         `json:"side"`                // "long" or "short"
	Quantity           string         `json:"quantity"`            // Current position size (0 when closed)
	ContributingTrades []string       `json:"contributing_trades"` // Exchange trade IDs
	EventEntries       []EventEntry   `json:"event_entries"`       // Immutable history of entry events
	ExitEntries        []EventEntry   `json:"exit_entries,omitempty"` // Immutable history of exit events
	FundingEntries     []FundingEntry `json:"funding_entries"`     // Individual funding payments
	StartTime          int64          `json:"start_time"`          // Unix ms
	EndTime            int64          `json:"end_time,omitempty"`  // Unix ms, 0 = still open
}

// IsClosed returns true if the position has been fully closed
func (p *PositionState) IsClosed() bool {
	return p.EndTime > 0
}

// TradingState tracks cumulative trading metrics for a single quote asset
type TradingState struct {
	CumulativeFunding    string `json:"cumulative_funding"`      // Net funding for positions in this quote asset
	CumulativeFeePaid    string `json:"cumulative_fee_paid"`     // Total fees in this quote asset
	CumulativeSettledPnl string `json:"cumulative_settled_pnl"`  // Total settled PnL in this quote asset
}

// NewAccountState creates a new empty account state
func NewAccountState() *AccountState {
	return &AccountState{
		Assets:          make(map[string]*AssetState),
		Positions:       make(map[string]*PositionState),
		ClosedPositions: []*PositionState{},
		Trading:         make(map[string]*TradingState),
	}
}

// GetOrCreateTrading returns the trading state for a quote asset, creating it if it doesn't exist
func (s *AccountState) GetOrCreateTrading(quoteAsset string) *TradingState {
	if s.Trading[quoteAsset] == nil {
		s.Trading[quoteAsset] = &TradingState{
			CumulativeFunding:    "0",
			CumulativeFeePaid:    "0",
			CumulativeSettledPnl: "0",
		}
	}
	return s.Trading[quoteAsset]
}

// GetOrCreateAsset returns the asset state, creating it if it doesn't exist.
// Note: the per-asset Balance is NOT stored here — it is computed by
// AccountState.Balance(symbol) from the open spot positions for the asset.
func (s *AccountState) GetOrCreateAsset(symbol string) *AssetState {
	if s.Assets[symbol] == nil {
		s.Assets[symbol] = &AssetState{
			CumulativeDeposits:  "0",
			CumulativeWithdraws: "0",
			CumulativeInterest:  "0",
			NetSpotInflow:       "0",
		}
	}
	return s.Assets[symbol]
}

// Balance returns the derived cumulative balance of the named asset by summing
// the signed quantities of the open spot positions for that asset:
//
//	long  contributes +Quantity
//	short contributes -Quantity
//
// Closed positions contribute zero by FIFO construction (EventEntries == ExitEntries
// at the moment of close) and so are not iterated.
//
// Positions are the single source of truth — there is no separately-maintained
// Balance field, so the derived balance and the position history can never
// diverge from one another.
//
// The returned string uses the same 18-decimal fixed-point format as the rest
// of the processor (parseNumeric / formatNumeric round-trip).
func (s *AccountState) Balance(symbol string) string {
	sum := new(big.Rat)
	for _, pos := range s.Positions {
		if pos == nil || pos.MarketType != "spot" || pos.Market != symbol {
			continue
		}
		q, ok := new(big.Rat).SetString(pos.Quantity)
		if !ok || q == nil {
			continue
		}
		if pos.Side == "long" {
			sum.Add(sum, q)
		} else {
			sum.Sub(sum, q)
		}
	}
	return formatBalance(sum)
}

// formatBalance renders a big.Rat as an 18-decimal fixed-point string with
// trailing zeros after the decimal point trimmed (keeping at least ".0"). This
// matches the processor's formatNumeric helper so balance strings produced by
// AccountState.Balance() round-trip cleanly through parseNumeric.
func formatBalance(r *big.Rat) string {
	if r == nil {
		return "0"
	}
	s := r.FloatString(18)
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		s = strings.TrimRight(s, "0")
		if s[len(s)-1] == '.' {
			s += "0"
		}
	}
	return s
}
