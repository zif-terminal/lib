package models

import (
	"encoding/json"
	"testing"
)

func TestPrice_UnmarshalJSON_NumericAsFloat(t *testing.T) {
	data := `{"asset":"SOL","denomination":"USDC","timestamp":1700000000000,"price":100.5,"source":"drift"}`
	var p Price
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if p.Price != "100.5" {
		t.Errorf("Expected price '100.5', got '%s'", p.Price)
	}
	if p.Asset != "SOL" {
		t.Errorf("Expected asset SOL, got %s", p.Asset)
	}
	if p.Denomination != "USDC" {
		t.Errorf("Expected denomination USDC, got %s", p.Denomination)
	}
}

func TestPrice_UnmarshalJSON_NumericAsString(t *testing.T) {
	data := `{"asset":"BTC","denomination":"USDC","timestamp":1700000000000,"price":"43000.50","source":"drift"}`
	var p Price
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if p.Price != "43000.50" {
		t.Errorf("Expected price '43000.50', got '%s'", p.Price)
	}
}

func TestPrice_UnmarshalJSON_MixedFields(t *testing.T) {
	// price as float64, other fields normal
	data := `{"asset":"ETH","denomination":"BTC","timestamp":1700000000000,"price":0.05123,"source":"hyperliquid"}`
	var p Price
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if p.Price == "" {
		t.Error("Expected non-empty price")
	}
	if p.Source != "hyperliquid" {
		t.Errorf("Expected source hyperliquid, got %s", p.Source)
	}
	if p.Timestamp != 1700000000000 {
		t.Errorf("Expected timestamp 1700000000000, got %d", p.Timestamp)
	}
}

func TestPrice_UnmarshalJSON_WholeNumber(t *testing.T) {
	// Whole number float should not have trailing decimals
	data := `{"asset":"BTC","denomination":"USDC","timestamp":1700000000000,"price":50000.0,"source":"drift"}`
	var p Price
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if p.Price != "50000" {
		t.Errorf("Expected price '50000', got '%s'", p.Price)
	}
}

func TestPrice_UnmarshalJSON_TimestampAsString(t *testing.T) {
	// HASURA_GRAPHQL_STRINGIFY_NUMERIC_TYPES=true causes bigint to come back as a string
	data := `{"asset":"mSOL","denomination":"USDC","timestamp":"1700000000000","price":"150.25","source":"drift"}`
	var p Price
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if p.Timestamp != 1700000000000 {
		t.Errorf("Expected timestamp 1700000000000, got %d", p.Timestamp)
	}
	if p.Price != "150.25" {
		t.Errorf("Expected price '150.25', got '%s'", p.Price)
	}
	if p.Asset != "mSOL" {
		t.Errorf("Expected asset mSOL, got %s", p.Asset)
	}
}

func TestPositionEvent_UnmarshalJSON_QuantityAsFloat(t *testing.T) {
	data := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"position_id": "00000000-0000-0000-0000-000000000002",
		"event_id": "00000000-0000-0000-0000-000000000003",
		"event_type": "trade",
		"direction": "entry",
		"quantity": 25.5
	}`
	var pe PositionEvent
	if err := json.Unmarshal([]byte(data), &pe); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if pe.Quantity != "25.5" {
		t.Errorf("Expected quantity '25.5', got '%s'", pe.Quantity)
	}
	if pe.EventType != "trade" {
		t.Errorf("Expected event_type trade, got %s", pe.EventType)
	}
	if pe.Direction != "entry" {
		t.Errorf("Expected direction entry, got %s", pe.Direction)
	}
}

func TestPositionEvent_UnmarshalJSON_QuantityAsString(t *testing.T) {
	data := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"position_id": "00000000-0000-0000-0000-000000000002",
		"event_id": "00000000-0000-0000-0000-000000000003",
		"event_type": "transfer",
		"direction": "exit",
		"quantity": "100.75"
	}`
	var pe PositionEvent
	if err := json.Unmarshal([]byte(data), &pe); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if pe.Quantity != "100.75" {
		t.Errorf("Expected quantity '100.75', got '%s'", pe.Quantity)
	}
}

func TestPositionEvent_UnmarshalJSON_WholeNumberQuantity(t *testing.T) {
	data := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"position_id": "00000000-0000-0000-0000-000000000002",
		"event_id": "00000000-0000-0000-0000-000000000003",
		"event_type": "funding",
		"direction": "paid",
		"quantity": 100.0
	}`
	var pe PositionEvent
	if err := json.Unmarshal([]byte(data), &pe); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if pe.Quantity != "100" {
		t.Errorf("Expected quantity '100', got '%s'", pe.Quantity)
	}
}

func TestEventValue_UnmarshalJSON_QuantityAsFloat(t *testing.T) {
	data := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"event_id": "00000000-0000-0000-0000-000000000002",
		"event_type": "trade",
		"denomination": "USDC",
		"quantity": 500.25
	}`
	var ev EventValue
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if ev.Quantity != "500.25" {
		t.Errorf("Expected quantity '500.25', got '%s'", ev.Quantity)
	}
	if ev.EventType != "trade" {
		t.Errorf("Expected event_type trade, got %s", ev.EventType)
	}
	if ev.Denomination != "USDC" {
		t.Errorf("Expected denomination USDC, got %s", ev.Denomination)
	}
}

func TestEventValue_UnmarshalJSON_QuantityAsString(t *testing.T) {
	data := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"event_id": "00000000-0000-0000-0000-000000000002",
		"event_type": "settlement",
		"denomination": "BTC",
		"quantity": "-150.75"
	}`
	var ev EventValue
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if ev.Quantity != "-150.75" {
		t.Errorf("Expected quantity '-150.75', got '%s'", ev.Quantity)
	}
}

func TestEventValue_UnmarshalJSON_WholeNumberQuantity(t *testing.T) {
	data := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"event_id": "00000000-0000-0000-0000-000000000002",
		"event_type": "transfer",
		"denomination": "USDC",
		"quantity": 1000.0
	}`
	var ev EventValue
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if ev.Quantity != "1000" {
		t.Errorf("Expected quantity '1000', got '%s'", ev.Quantity)
	}
}
