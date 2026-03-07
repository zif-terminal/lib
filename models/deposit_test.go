package models

import (
	"encoding/json"
	"testing"
)

func TestDeposit_UnmarshalJSON(t *testing.T) {
	t.Run("NumericTimestamp", func(t *testing.T) {
		data := `{"id":"00000000-0000-0000-0000-000000000001","exchange_account_id":"00000000-0000-0000-0000-000000000002","asset":"USDC","direction":"deposit","amount":1000.5,"timestamp":1700000000000,"deposit_id":"dep-1"}`
		var d Deposit
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if d.Asset != "USDC" {
			t.Errorf("Expected asset USDC, got %s", d.Asset)
		}
		if d.Amount != "1000.5" {
			t.Errorf("Expected amount '1000.5', got '%s'", d.Amount)
		}
		if d.Timestamp.IsZero() {
			t.Error("Expected non-zero timestamp")
		}
	})

	t.Run("StringTimestamp", func(t *testing.T) {
		data := `{"asset":"SOL","amount":"-10.5","timestamp":"1700000000000","user_cost_basis":"25.50"}`
		var d Deposit
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if d.Amount != "-10.5" {
			t.Errorf("Expected amount '-10.5', got '%s'", d.Amount)
		}
		if d.UserCostBasis != "25.50" {
			t.Errorf("Expected user_cost_basis '25.50', got '%s'", d.UserCostBasis)
		}
	})
}
