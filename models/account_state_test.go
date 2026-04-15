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
	if asset.Balance != "0" {
		t.Errorf("Expected Balance 0, got %s", asset.Balance)
	}
	if asset.CumulativeDeposits != "0" {
		t.Errorf("Expected CumulativeDeposits 0, got %s", asset.CumulativeDeposits)
	}

	// Modify and re-fetch
	asset.Balance = "500"
	asset2 := state.GetOrCreateAsset("SOL")
	if asset2.Balance != "500" {
		t.Errorf("Expected Balance 500, got %s", asset2.Balance)
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
