package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestInterestPayment_UnmarshalJSON_Float64Values(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"asset": "USDC",
		"amount": "5.25",
		"oracle_price": "1.0",
		"usd_value": "5.25",
		"timestamp": 1700050000000,
		"snapshot_from": 1700000000000,
		"snapshot_to": 1700100000000,
		"is_approximate": true,
		"created_at": "2024-01-01T00:00:00Z"
	}`

	var ip InterestPayment
	err := json.Unmarshal([]byte(jsonData), &ip)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if ip.Asset != "USDC" {
		t.Errorf("Expected USDC, got %s", ip.Asset)
	}
	if ip.Amount != "5.25" {
		t.Errorf("Expected 5.25, got %s", ip.Amount)
	}
	if ip.SnapshotFrom != 1700000000000 {
		t.Errorf("Expected snapshot_from 1700000000000, got %d", ip.SnapshotFrom)
	}
	if ip.SnapshotTo != 1700100000000 {
		t.Errorf("Expected snapshot_to 1700100000000, got %d", ip.SnapshotTo)
	}
	if !ip.IsApproximate {
		t.Error("Expected is_approximate to be true")
	}
	expectedTime := time.Unix(0, 1700050000000*int64(time.Millisecond)).UTC()
	if !ip.Timestamp.Equal(expectedTime) {
		t.Errorf("Expected %v, got %v", expectedTime, ip.Timestamp)
	}
}

func TestInterestPayment_UnmarshalJSON_StringValues(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"asset": "SOL",
		"amount": "-0.001",
		"oracle_price": "150.5",
		"usd_value": "-0.1505",
		"timestamp": "1700050000000",
		"snapshot_from": "1700000000000",
		"snapshot_to": "1700100000000",
		"is_approximate": false
	}`

	var ip InterestPayment
	err := json.Unmarshal([]byte(jsonData), &ip)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if ip.Amount != "-0.001" {
		t.Errorf("Expected -0.001, got %s", ip.Amount)
	}
	if ip.SnapshotFrom != 1700000000000 {
		t.Errorf("Expected 1700000000000, got %d", ip.SnapshotFrom)
	}
	if ip.IsApproximate {
		t.Error("Expected is_approximate to be false")
	}
}

func TestInterestPayment_UnmarshalJSON_FloatNumericValues(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"asset": "USDC",
		"amount": 5.25,
		"oracle_price": 1.0,
		"usd_value": 5.25,
		"timestamp": 1700050000000,
		"snapshot_from": 1700000000000,
		"snapshot_to": 1700100000000,
		"is_approximate": false
	}`

	var ip InterestPayment
	err := json.Unmarshal([]byte(jsonData), &ip)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if ip.Amount == "" {
		t.Error("Expected non-empty amount")
	}
	if ip.OraclePrice == "" {
		t.Error("Expected non-empty oracle_price")
	}
}

func TestParseInterfaceToInt64(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    int64
		wantErr bool
	}{
		{"float64", float64(1700000000000), 1700000000000, false},
		{"string", "1700000000000", 1700000000000, false},
		{"int", int(42), 42, false},
		{"unsupported type", true, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInterfaceToInt64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInterfaceToInt64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseInterfaceToInt64() = %v, want %v", got, tt.want)
			}
		})
	}
}
