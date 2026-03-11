package models

import (
	"encoding/json"
	"testing"
)

func TestPosition_UnmarshalJSON_BigintTimestamps(t *testing.T) {
	// Hasura returns BIGINT as float64 and NUMERIC as string
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"market": "SOL-PERP",
		"market_type": "perp",
		"side": "long",
		"status": "closed",
		"quantity": "100.5",
		"entry_price": "150.25",
		"exit_price": "160.00",
		"total_fees": "1.5",
		"cumulative_funding": "0.5",
		"start_time": 1700000000000,
		"end_time": 1700100000000
	}`

	var pos Position
	err := json.Unmarshal([]byte(jsonData), &pos)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if pos.Market != "SOL-PERP" {
		t.Errorf("Expected SOL-PERP, got %s", pos.Market)
	}
	if pos.Quantity != "100.5" {
		t.Errorf("Expected quantity 100.5, got %s", pos.Quantity)
	}
	if pos.ExitPrice == nil {
		t.Fatal("Expected exit_price to be non-nil")
	}
	if *pos.ExitPrice != "160.00" {
		t.Errorf("Expected exit_price 160.00, got %s", *pos.ExitPrice)
	}
	if pos.StartTime.IsZero() {
		t.Error("Expected start_time to be non-zero")
	}
	if pos.EndTime == nil {
		t.Fatal("Expected end_time to be non-nil")
	}
}

func TestPosition_UnmarshalJSON_StringTimestamps(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"market": "BTC-PERP",
		"market_type": "perp",
		"side": "short",
		"status": "open",
		"quantity": "0.5",
		"entry_price": "60000",
		"total_fees": "3.0",
		"cumulative_funding": "1.2",
		"start_time": "1700000000000"
	}`

	var pos Position
	err := json.Unmarshal([]byte(jsonData), &pos)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if pos.Status != "open" {
		t.Errorf("Expected open, got %s", pos.Status)
	}
	if pos.ExitPrice != nil {
		t.Errorf("Expected nil exit_price, got %v", pos.ExitPrice)
	}
	if pos.EndTime != nil {
		t.Errorf("Expected nil end_time, got %v", pos.EndTime)
	}
	if pos.StartTime.IsZero() {
		t.Error("Expected start_time to be non-zero")
	}
}

func TestPosition_UnmarshalJSON_NumericAsFloat(t *testing.T) {
	// When Hasura returns NUMERIC fields as float64
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"market": "ETH-PERP",
		"market_type": "perp",
		"side": "long",
		"status": "closed",
		"quantity": 50.0,
		"entry_price": 3000.5,
		"exit_price": 3100.25,
		"total_fees": 10.5,
		"cumulative_funding": 2.3,
		"start_time": 1700000000000,
		"end_time": 1700100000000
	}`

	var pos Position
	err := json.Unmarshal([]byte(jsonData), &pos)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if pos.Quantity == "" {
		t.Error("Expected quantity to be non-empty")
	}
	if pos.EntryPrice == "" {
		t.Error("Expected entry_price to be non-empty")
	}
}

func TestPosition_UnmarshalJSON_WithRealizedPnl(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"market": "SOL-PERP",
		"market_type": "perp",
		"side": "long",
		"status": "closed",
		"quantity": "100",
		"entry_price": "150",
		"exit_price": "160",
		"total_fees": "1.5",
		"cumulative_funding": "0.5",
		"realized_pnl": 998.5,
		"pnl_denomination": "USDC",
		"start_time": 1700000000000,
		"end_time": 1700100000000
	}`

	var pos Position
	err := json.Unmarshal([]byte(jsonData), &pos)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if pos.RealizedPnl == nil {
		t.Fatal("Expected realized_pnl to be non-nil")
	}
	if *pos.RealizedPnl != 998.5 {
		t.Errorf("Expected realized_pnl 998.5, got %f", *pos.RealizedPnl)
	}
	if pos.PnlDenomination == nil {
		t.Fatal("Expected pnl_denomination to be non-nil")
	}
	if *pos.PnlDenomination != "USDC" {
		t.Errorf("Expected pnl_denomination USDC, got %s", *pos.PnlDenomination)
	}
}

func TestPosition_UnmarshalJSON_WithNullPnl(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"market": "SOL-PERP",
		"market_type": "perp",
		"side": "long",
		"status": "open",
		"quantity": "100",
		"entry_price": "150",
		"total_fees": "1.5",
		"cumulative_funding": "0.5",
		"realized_pnl": null,
		"pnl_denomination": null,
		"start_time": 1700000000000
	}`

	var pos Position
	err := json.Unmarshal([]byte(jsonData), &pos)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if pos.RealizedPnl != nil {
		t.Errorf("Expected nil realized_pnl, got %v", *pos.RealizedPnl)
	}
	if pos.PnlDenomination != nil {
		t.Errorf("Expected nil pnl_denomination, got %v", *pos.PnlDenomination)
	}
}

func TestPosition_UnmarshalJSON_RealizedPnlAsString(t *testing.T) {
	// Hasura may return NUMERIC(36,18) as a string
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"market": "SOL-PERP",
		"market_type": "perp",
		"side": "long",
		"status": "closed",
		"quantity": "100",
		"entry_price": "150",
		"exit_price": "160",
		"total_fees": "1.5",
		"cumulative_funding": "0.5",
		"realized_pnl": "1234.567890",
		"pnl_denomination": "USDC",
		"start_time": 1700000000000,
		"end_time": 1700100000000
	}`

	var pos Position
	err := json.Unmarshal([]byte(jsonData), &pos)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if pos.RealizedPnl == nil {
		t.Fatal("Expected realized_pnl to be non-nil")
	}
	if *pos.RealizedPnl != 1234.567890 {
		t.Errorf("Expected realized_pnl 1234.567890, got %f", *pos.RealizedPnl)
	}
}
