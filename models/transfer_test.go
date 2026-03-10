package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTransfer_UnmarshalJSON_Float64Timestamp(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"type": "deposit",
		"asset": "USDC",
		"amount": "1000.50",
		"timestamp": 1700000000000
	}`

	var tr Transfer
	err := json.Unmarshal([]byte(jsonData), &tr)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if tr.Type != TypeDeposit {
		t.Errorf("Expected deposit, got %s", tr.Type)
	}
	if tr.Amount != "1000.50" {
		t.Errorf("Expected 1000.50, got %s", tr.Amount)
	}
	expectedTime := time.Unix(0, 1700000000000*int64(time.Millisecond)).UTC()
	if !tr.Timestamp.Equal(expectedTime) {
		t.Errorf("Expected %v, got %v", expectedTime, tr.Timestamp)
	}
}

func TestTransfer_UnmarshalJSON_StringTimestamp(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"type": "withdraw",
		"asset": "SOL",
		"amount": "10.0",
		"timestamp": "1700000000000"
	}`

	var tr Transfer
	err := json.Unmarshal([]byte(jsonData), &tr)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if tr.Type != TypeWithdraw {
		t.Errorf("Expected withdraw, got %s", tr.Type)
	}
	if tr.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}
}

func TestTransfer_UnmarshalJSON_FloatAmount(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"type": "interest",
		"asset": "USDC",
		"amount": 5.25,
		"timestamp": 1700000000000
	}`

	var tr Transfer
	err := json.Unmarshal([]byte(jsonData), &tr)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if tr.Amount == "" {
		t.Error("Expected non-empty amount")
	}
}

func TestTransfer_UnmarshalJSON_FloatCostBasis(t *testing.T) {
	jsonData := `{
		"id": "00000000-0000-0000-0000-000000000001",
		"exchange_account_id": "00000000-0000-0000-0000-000000000002",
		"type": "deposit",
		"asset": "USDC",
		"amount": "1000.50",
		"cost_basis": 1.0012,
		"timestamp": 1700000000000
	}`

	var tr Transfer
	err := json.Unmarshal([]byte(jsonData), &tr)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if tr.CostBasis == "" {
		t.Error("Expected non-empty cost_basis")
	}
}

func TestTransferTypeConstants(t *testing.T) {
	if TypeDeposit != "deposit" {
		t.Errorf("Expected deposit, got %s", TypeDeposit)
	}
	if TypeWithdraw != "withdraw" {
		t.Errorf("Expected withdraw, got %s", TypeWithdraw)
	}
	if TypeInterest != "interest" {
		t.Errorf("Expected interest, got %s", TypeInterest)
	}
	if TypeIFStake != "if_stake" {
		t.Errorf("Expected if_stake, got %s", TypeIFStake)
	}
	if TypeIFUnstake != "if_unstake" {
		t.Errorf("Expected if_unstake, got %s", TypeIFUnstake)
	}
	if TypeReward != "reward" {
		t.Errorf("Expected reward, got %s", TypeReward)
	}
}
