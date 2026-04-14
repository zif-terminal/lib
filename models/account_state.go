package models

import (
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

// AccountState represents the in-memory state of an account.
// Built by processing all transactions chronologically.
type AccountState struct {
	Assets          map[string]*AssetState        `json:"assets"`
	Positions       map[string]*PositionState     `json:"positions"`        // Open positions keyed by "marketType:baseAsset"
	ClosedPositions []*PositionState              `json:"closed_positions"`
	Trading         map[string]*TradingState      `json:"trading"`          // Keyed by quote asset (e.g., "USDC", "SOL")
	HasSeenSnapshot bool                          `json:"has_seen_snapshot"` // True after first snapshot baseline applied
}

// AssetState tracks the state of a single asset (USDC, SOL, etc.)
type AssetState struct {
	Balance             string `json:"balance"`              // Current balance
	CumulativeDeposits  string `json:"cumulative_deposits"`  // Sum of deposits
	CumulativeWithdraws string `json:"cumulative_withdraws"` // Sum of withdrawals
	CumulativeInterest  string `json:"cumulative_interest"`  // All interest (explicit transfers + snapshot-derived)
	NetSpotInflow       string `json:"net_spot_inflow"`      // Net balance change from spot trades (buys - sells)
}

// PositionState tracks in-memory position state (perp or spot), open or closed.
// A position is closed when EndTime > 0.
type PositionState struct {
	Market             string         `json:"market"`              // e.g., "SOL" or "SOL-PERP"
	MarketType         string         `json:"market_type"`         // "perp" or "spot"
	Side               string         `json:"side"`                // "long" or "short"
	Quantity           string         `json:"quantity"`            // Current position size (0 when closed)
	TotalFees          string         `json:"total_fees"`          // Accumulated fees
	CumulativeFunding  string         `json:"cumulative_funding"`  // Funding paid/received for this position
	ContributingTrades []string       `json:"contributing_trades"` // Exchange trade IDs
	EventEntries       []EventEntry   `json:"event_entries"`       // Per-event qty for FIFO allocation
	FundingEntries     []FundingEntry `json:"funding_entries"`     // Individual funding payments
	StartTime          int64          `json:"start_time"`          // Unix ms
	EndTime            int64          `json:"end_time,omitempty"`  // Unix ms, 0 = still open
	ExitEventID        uuid.UUID      `json:"exit_event_id,omitempty"`    // DB UUID of closing event
	ExitEventType      string         `json:"exit_event_type,omitempty"`  // "trade", "interest", "transfer", etc.
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

// GetOrCreateAsset returns the asset state, creating it if it doesn't exist
func (s *AccountState) GetOrCreateAsset(symbol string) *AssetState {
	if s.Assets[symbol] == nil {
		s.Assets[symbol] = &AssetState{
			Balance:             "0",
			CumulativeDeposits:  "0",
			CumulativeWithdraws: "0",
			CumulativeInterest:  "0",
			NetSpotInflow:       "0",
		}
	}
	return s.Assets[symbol]
}
