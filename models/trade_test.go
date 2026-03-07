package models

import (
	"encoding/json"
	"testing"
)

func TestTrade_UnmarshalJSON(t *testing.T) {
	t.Run("NumericFields", func(t *testing.T) {
		data := `{"id":"00000000-0000-0000-0000-000000000001","base_asset":"BTC","quote_asset":"USDC","side":"buy","price":50000.5,"quantity":0.1,"fee":5.0,"timestamp":1700000000000,"trade_id":"t-1","market_type":"perp"}`
		var tr Trade
		if err := json.Unmarshal([]byte(data), &tr); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if tr.BaseAsset != "BTC" {
			t.Errorf("Expected base_asset BTC, got %s", tr.BaseAsset)
		}
		if tr.Price != "50000.5" {
			t.Errorf("Expected price '50000.5', got '%s'", tr.Price)
		}
		if tr.Timestamp.IsZero() {
			t.Error("Expected non-zero timestamp")
		}
	})

	t.Run("StringFields", func(t *testing.T) {
		data := `{"base_asset":"ETH","quote_asset":"USDC","side":"sell","price":"3000.25","quantity":"1.5","fee":"4.5","timestamp":"1700000000000"}`
		var tr Trade
		if err := json.Unmarshal([]byte(data), &tr); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if tr.Price != "3000.25" {
			t.Errorf("Expected price '3000.25', got '%s'", tr.Price)
		}
		if tr.Quantity != "1.5" {
			t.Errorf("Expected quantity '1.5', got '%s'", tr.Quantity)
		}
	})
}

func TestConvertToString(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{"hello", "hello"},
		{float64(100.5), "100.5"},
		{int(42), "42"},
		{int64(12345), "12345"},
		{true, "true"},
	}
	for _, tt := range tests {
		got := convertToString(tt.input)
		if got != tt.want {
			t.Errorf("convertToString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{"float64", float64(1700000000000), false},
		{"int64", int64(1700000000000), false},
		{"string", "1700000000000", false},
		{"invalid_string", "not-a-number", true},
		{"unsupported_type", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, err := parseTimestamp(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimestamp(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && ts.IsZero() {
				t.Error("Expected non-zero timestamp")
			}
		})
	}
}
