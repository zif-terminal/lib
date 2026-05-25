package models

import (
	"testing"
)

func TestNewAccountState(t *testing.T) {
	state := NewAccountState()

	if state.Assets == nil {
		t.Error("Assets map should be initialized")
	}
	if state.Positions == nil {
		t.Error("Positions map should be initialized")
	}
	if state.ClosedPositions == nil {
		t.Error("ClosedPositions should be initialized")
	}
	if state.Trading == nil {
		t.Error("Trading map should be initialized")
	}
	if state.HasSeenSnapshot {
		t.Error("HasSeenSnapshot should be false initially")
	}
}

func TestGetOrCreateTrading(t *testing.T) {
	state := NewAccountState()

	// First call creates the trading state
	trading := state.GetOrCreateTrading("USDC")
	if trading == nil {
		t.Fatal("Expected non-nil trading state")
	}
	if trading.CumulativeFunding != "0" {
		t.Errorf("Expected CumulativeFunding 0, got %s", trading.CumulativeFunding)
	}
	if trading.CumulativeFeePaid != "0" {
		t.Errorf("Expected CumulativeFeePaid 0, got %s", trading.CumulativeFeePaid)
	}
	if trading.CumulativeSettledPnl != "0" {
		t.Errorf("Expected CumulativeSettledPnl 0, got %s", trading.CumulativeSettledPnl)
	}

	// Modify and re-fetch — should return same instance
	trading.CumulativeFunding = "100"
	trading2 := state.GetOrCreateTrading("USDC")
	if trading2.CumulativeFunding != "100" {
		t.Errorf("Expected CumulativeFunding 100, got %s", trading2.CumulativeFunding)
	}
}

func TestGetOrCreateAsset(t *testing.T) {
	state := NewAccountState()

	asset := state.GetOrCreateAsset("SOL")
	if asset == nil {
		t.Fatal("Expected non-nil asset state")
	}
	if asset.CumulativeDeposits != "0" {
		t.Errorf("Expected CumulativeDeposits 0, got %s", asset.CumulativeDeposits)
	}

	// Modify and re-fetch — re-fetch must return the same instance, not a fresh one
	asset.CumulativeDeposits = "500"
	asset2 := state.GetOrCreateAsset("SOL")
	if asset2.CumulativeDeposits != "500" {
		t.Errorf("Expected CumulativeDeposits 500 on re-fetch (same instance), got %s", asset2.CumulativeDeposits)
	}
}

// TestBalance_DerivedFromPositions verifies the computed Balance() method:
// long positions contribute +Quantity, short positions contribute -Quantity,
// closed positions contribute 0, and per-asset / per-market filtering works.
func TestBalance_DerivedFromPositions(t *testing.T) {
	state := NewAccountState()

	// No positions → balance 0
	if got := state.Balance("SOL"); got != "0.0" && got != "0" {
		t.Errorf("Empty state Balance(SOL) = %q, want 0", got)
	}

	// Open long spot:SOL 100 → balance +100
	state.Positions["spot:SOL"] = &PositionState{
		Market: "SOL", MarketType: "spot", Side: "long", Quantity: "100",
	}
	if got := state.Balance("SOL"); got != "100.0" {
		t.Errorf("After long 100 Balance(SOL) = %q, want 100.0", got)
	}

	// Open short spot:USDC 50 → balance -50
	state.Positions["spot:USDC"] = &PositionState{
		Market: "USDC", MarketType: "spot", Side: "short", Quantity: "50",
	}
	if got := state.Balance("USDC"); got != "-50.0" {
		t.Errorf("After short 50 Balance(USDC) = %q, want -50.0", got)
	}

	// Perp positions are ignored (only spot contributes to spot balance)
	state.Positions["perp:SOL"] = &PositionState{
		Market: "SOL-PERP", MarketType: "perp", Side: "long", Quantity: "5",
	}
	if got := state.Balance("SOL"); got != "100.0" {
		t.Errorf("Perp position must not affect spot balance, got %q want 100.0", got)
	}

	// Closed positions ignored (they live in ClosedPositions and shouldn't
	// be in the live map by construction, but explicitly check by emptying)
	delete(state.Positions, "spot:SOL")
	state.ClosedPositions = append(state.ClosedPositions, &PositionState{
		Market: "SOL", MarketType: "spot", Side: "long", Quantity: "0", EndTime: 1,
	})
	if got := state.Balance("SOL"); got != "0.0" && got != "0" {
		t.Errorf("Closed positions must not contribute, got %q want 0", got)
	}
}

func TestPositionState_IsClosed(t *testing.T) {
	open := &PositionState{EndTime: 0}
	if open.IsClosed() {
		t.Error("Position with EndTime=0 should not be closed")
	}

	closed := &PositionState{EndTime: 1700000000000}
	if !closed.IsClosed() {
		t.Error("Position with non-zero EndTime should be closed")
	}
}
