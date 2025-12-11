package models

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFundingPayment_UnmarshalJSON_TimestampAsNumber(t *testing.T) {
	now := time.Now()
	unixMillis := now.UnixMilli()

	jsonData := []byte(fmt.Sprintf(`{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"exchange_account_id": "123e4567-e89b-12d3-a456-426614174001",
		"base_asset": "BTC",
		"quote_asset": "USDC",
		"amount": "10.5",
		"timestamp": %d,
		"payment_id": "payment-123"
	}`, unixMillis))

	var fp FundingPayment
	err := json.Unmarshal(jsonData, &fp)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	// Check timestamp (allowing for small rounding differences)
	expectedTime := time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
	if fp.Timestamp.UnixMilli() != expectedTime.UnixMilli() {
		t.Errorf("Expected timestamp %d, got %d", expectedTime.UnixMilli(), fp.Timestamp.UnixMilli())
	}
}

func TestFundingPayment_UnmarshalJSON_TimestampAsString(t *testing.T) {
	now := time.Now()
	unixMillis := now.UnixMilli()

	jsonData := []byte(fmt.Sprintf(`{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"exchange_account_id": "123e4567-e89b-12d3-a456-426614174001",
		"base_asset": "BTC",
		"quote_asset": "USDC",
		"amount": "10.5",
		"timestamp": "%d",
		"payment_id": "payment-123"
	}`, unixMillis))

	var fp FundingPayment
	err := json.Unmarshal(jsonData, &fp)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	expectedTime := time.Unix(0, unixMillis*int64(time.Millisecond)).UTC()
	if fp.Timestamp.UnixMilli() != expectedTime.UnixMilli() {
		t.Errorf("Expected timestamp %d, got %d", expectedTime.UnixMilli(), fp.Timestamp.UnixMilli())
	}
}

func TestFundingPayment_UnmarshalJSON_AmountAsNumber(t *testing.T) {
	jsonData := []byte(`{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"exchange_account_id": "123e4567-e89b-12d3-a456-426614174001",
		"base_asset": "BTC",
		"quote_asset": "USDC",
		"amount": 10.5,
		"timestamp": 1609459200000,
		"payment_id": "payment-123"
	}`)

	var fp FundingPayment
	err := json.Unmarshal(jsonData, &fp)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if fp.Amount != "10.5" {
		t.Errorf("Expected amount '10.5', got '%s'", fp.Amount)
	}
}

func TestFundingPayment_UnmarshalJSON_AmountAsString(t *testing.T) {
	jsonData := []byte(`{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"exchange_account_id": "123e4567-e89b-12d3-a456-426614174001",
		"base_asset": "BTC",
		"quote_asset": "USDC",
		"amount": "10.5",
		"timestamp": 1609459200000,
		"payment_id": "payment-123"
	}`)

	var fp FundingPayment
	err := json.Unmarshal(jsonData, &fp)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if fp.Amount != "10.5" {
		t.Errorf("Expected amount '10.5', got '%s'", fp.Amount)
	}
}

func TestFundingPayment_UnmarshalJSON_NegativeAmount(t *testing.T) {
	jsonData := []byte(`{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"exchange_account_id": "123e4567-e89b-12d3-a456-426614174001",
		"base_asset": "BTC",
		"quote_asset": "USDC",
		"amount": -10.5,
		"timestamp": 1609459200000,
		"payment_id": "payment-123"
	}`)

	var fp FundingPayment
	err := json.Unmarshal(jsonData, &fp)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if fp.Amount != "-10.5" {
		t.Errorf("Expected amount '-10.5', got '%s'", fp.Amount)
	}
}

func TestFundingPayment_UnmarshalJSON_AllFields(t *testing.T) {
	accountID := uuid.New()
	paymentID := uuid.New()

	jsonData := []byte(`{
		"id": "` + paymentID.String() + `",
		"exchange_account_id": "` + accountID.String() + `",
		"base_asset": "BTC",
		"quote_asset": "USDC",
		"amount": "10.5",
		"timestamp": 1609459200000,
		"payment_id": "payment-123"
	}`)

	var fp FundingPayment
	err := json.Unmarshal(jsonData, &fp)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if fp.ID != paymentID {
		t.Errorf("Expected ID %s, got %s", paymentID, fp.ID)
	}
	if fp.ExchangeAccountID != accountID {
		t.Errorf("Expected ExchangeAccountID %s, got %s", accountID, fp.ExchangeAccountID)
	}
	if fp.BaseAsset != "BTC" {
		t.Errorf("Expected BaseAsset 'BTC', got '%s'", fp.BaseAsset)
	}
	if fp.QuoteAsset != "USDC" {
		t.Errorf("Expected QuoteAsset 'USDC', got '%s'", fp.QuoteAsset)
	}
	if fp.Amount != "10.5" {
		t.Errorf("Expected Amount '10.5', got '%s'", fp.Amount)
	}
	if fp.PaymentID != "payment-123" {
		t.Errorf("Expected PaymentID 'payment-123', got '%s'", fp.PaymentID)
	}
}

func TestFundingPayment_UnmarshalJSON_EmptyAmount(t *testing.T) {
	jsonData := []byte(`{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"exchange_account_id": "123e4567-e89b-12d3-a456-426614174001",
		"base_asset": "BTC",
		"quote_asset": "USDC",
		"timestamp": 1609459200000,
		"payment_id": "payment-123"
	}`)

	var fp FundingPayment
	err := json.Unmarshal(jsonData, &fp)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if fp.Amount != "" {
		t.Errorf("Expected empty amount, got '%s'", fp.Amount)
	}
}

func TestFundingPayment_UnmarshalJSON_InvalidTimestamp(t *testing.T) {
	jsonData := []byte(`{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"exchange_account_id": "123e4567-e89b-12d3-a456-426614174001",
		"base_asset": "BTC",
		"quote_asset": "USDC",
		"amount": "10.5",
		"timestamp": "invalid",
		"payment_id": "payment-123"
	}`)

	var fp FundingPayment
	err := json.Unmarshal(jsonData, &fp)
	if err == nil {
		t.Fatal("Expected error for invalid timestamp")
	}
}
