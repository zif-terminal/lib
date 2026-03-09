package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSettlement_UnmarshalJSON_Float64Timestamp(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"asset": "USDC",
		"amount": "150.25",
		"market": "SOL-PERP",
		"timestamp": 1700000000000,
		"settlement_id": "settle-123"
	}`

	var s Settlement
	err := json.Unmarshal([]byte(jsonData), &s)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if s.Asset != "USDC" {
		t.Errorf("Expected USDC, got %s", s.Asset)
	}
	if s.Amount != "150.25" {
		t.Errorf("Expected 150.25, got %s", s.Amount)
	}
	expectedTime := time.Unix(0, 1700000000000*int64(time.Millisecond)).UTC()
	if !s.Timestamp.Equal(expectedTime) {
		t.Errorf("Expected timestamp %v, got %v", expectedTime, s.Timestamp)
	}
}

func TestSettlement_UnmarshalJSON_StringTimestamp(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"asset": "USDC",
		"amount": "-50.10",
		"market": "BTC-PERP",
		"timestamp": "1700000000000",
		"settlement_id": "settle-456"
	}`

	var s Settlement
	err := json.Unmarshal([]byte(jsonData), &s)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if s.Amount != "-50.10" {
		t.Errorf("Expected -50.10, got %s", s.Amount)
	}
	if s.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}
}

func TestSettlement_UnmarshalJSON_FloatAmount(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"asset": "USDC",
		"amount": 200.75,
		"market": "ETH-PERP",
		"timestamp": 1700000000000,
		"settlement_id": "settle-789"
	}`

	var s Settlement
	err := json.Unmarshal([]byte(jsonData), &s)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if s.Amount == "" {
		t.Error("Expected non-empty amount")
	}
}
