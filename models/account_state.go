package models

import (
	"github.com/google/uuid"
)

// TradeEntry tracks a single event's contribution to a position (for FIFO allocation).
// Despite the name, this tracks trades, transfers, interest, and rewards — any event
// that changes position quantity. The EventType field distinguishes them.
type TradeEntry struct {
	TradeDBID uuid.UUID `json:"trade_db_id"`
	EventType string    `json:"event_type,omitempty"` // "trade", "transfer", "interest", "reward" (empty = "trade" for backward compat)
	Quantity  string    `json:"quantity"`              // How much this event contributed
}

// FundingEntry tracks a single funding payment linked to a position
type FundingEntry struct {
	FundingDBID uuid.UUID `json:"funding_db_id"`
	Amount      string    `json:"amount"`    // Signed: positive = received, negative = paid
	Timestamp   int64     `json:"timestamp"` // Unix ms
}

// AccountState represents the in-memory state of an account.
// Built by processing all transactions chronologically.
type AccountState struct {
	Assets          map[string]*AssetState   `json:"assets"`
	Positions       map[string]*OpenPosition `json:"positions"`        // All positions keyed by "marketType:baseAsset"
	ClosedPositions []*ClosedPosition        `json:"closed_positions"`
	Trading         map[string]*TradingState `json:"trading"`          // Keyed by quote asset (e.g., "USDC", "SOL")
}

// AssetState tracks the state of a single asset (USDC, SOL, etc.)
type AssetState struct {
	Balance              string `json:"balance"`               // Current balance
	CumulativeDeposits   string `json:"cumulative_deposits"`   // Sum of deposits
	CumulativeWithdraws  string `json:"cumulative_withdraws"`  // Sum of withdrawals
	CumulativeInterest   string `json:"cumulative_interest"`   // Interest from explicit transfer records (type="interest")
	CumulativeAdjustment string `json:"cumulative_adjustment"` // Balance corrections from snapshot reconciliation (precision errors)
	NetSpotInflow        string `json:"net_spot_inflow"`       // Net balance change from spot trades (buys - sells)
}

// OpenPosition tracks an open position (perp or spot)
type OpenPosition struct {
	Market             string         `json:"market"`              // e.g., "SOL" or "SOL-PERP"
	MarketType         string         `json:"market_type"`         // "perp" or "spot"
	QuoteAsset         string         `json:"quote_asset"`         // What the position is quoted in
	Side               string         `json:"side"`                // "long" or "short"
	Quantity           string         `json:"quantity"`            // Current position size
	TotalFees          string         `json:"total_fees"`          // Accumulated fees
	CumulativeFunding  string         `json:"cumulative_funding"`  // Funding paid/received for this position
	ContributingTrades []string       `json:"contributing_trades"` // Exchange trade IDs
	TradeEntries       []TradeEntry   `json:"trade_entries"`       // Per-trade qty for FIFO allocation
	FundingEntries     []FundingEntry `json:"funding_entries"`     // Individual funding payments
	StartTime          int64          `json:"start_time"`          // Unix ms
}

// ClosedPosition represents a fully closed position (perp or spot)
type ClosedPosition struct {
	Market            string         `json:"market"`
	MarketType        string         `json:"market_type"` // "perp" or "spot"
	QuoteAsset        string         `json:"quote_asset"` // What the position is quoted in
	Side              string         `json:"side"`         // "long" or "short"
	Quantity          string         `json:"quantity"`     // Total qty that was closed
	TotalFees         string         `json:"total_fees"`
	CumulativeFunding string         `json:"cumulative_funding"`
	StartTime         int64          `json:"start_time"`
	EndTime           int64          `json:"end_time"`
	EntryTradeEntries []TradeEntry   `json:"entry_trade_entries"` // Per-trade qty (FIFO allocated)
	ExitTradeDBID     uuid.UUID      `json:"exit_trade_db_id"`   // DB UUID of exit trade
	FundingEntries    []FundingEntry `json:"funding_entries"`     // Funding payments during this position
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
		Positions:       make(map[string]*OpenPosition),
		ClosedPositions: []*ClosedPosition{},
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
			Balance:              "0",
			CumulativeDeposits:   "0",
			CumulativeWithdraws:  "0",
			CumulativeInterest:   "0",
			CumulativeAdjustment: "0",
			NetSpotInflow:        "0",
		}
	}
	return s.Assets[symbol]
}
