package db

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
	"github.com/zif-terminal/lib/models"
)

func TestClient_GetLatestFundingPayment(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedPayment := &models.FundingPayment{
		ID:                uuid.New(),
		ExchangeAccountID: accountID,
		BaseAsset:         "BTC",
		QuoteAsset:        "USDC",
		Amount:            "10.5",
		Timestamp:         time.Now(),
		PaymentID:         "payment-123",
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"funding_payments": []*models.FundingPayment{expectedPayment},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	payment, err := client.GetLatestFundingPayment(ctx, accountID)
	if err != nil {
		t.Fatalf("GetLatestFundingPayment failed: %v", err)
	}

	if payment.ID != expectedPayment.ID {
		t.Errorf("Expected ID %s, got %s", expectedPayment.ID, payment.ID)
	}
	if payment.BaseAsset != expectedPayment.BaseAsset {
		t.Errorf("Expected BaseAsset %s, got %s", expectedPayment.BaseAsset, payment.BaseAsset)
	}
	if payment.Amount != expectedPayment.Amount {
		t.Errorf("Expected Amount %s, got %s", expectedPayment.Amount, payment.Amount)
	}
}

func TestClient_GetLatestFundingPayment_NotFound(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"funding_payments": []*models.FundingPayment{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	payment, err := client.GetLatestFundingPayment(ctx, accountID)
	if err != nil {
		t.Fatalf("GetLatestFundingPayment failed: %v", err)
	}

	if payment != nil {
		t.Errorf("Expected nil payment, got %v", payment)
	}
}

func TestClient_AddFundingPayments_Single(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedPayment := &models.FundingPayment{
		ID:                uuid.New(),
		ExchangeAccountID: accountID,
		BaseAsset:         "BTC",
		QuoteAsset:        "USDC",
		Amount:            "10.5",
		Timestamp:         time.Now(),
		PaymentID:         "payment-123",
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_funding_payments": map[string]interface{}{
					"returning": []*models.FundingPayment{expectedPayment},
				},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	input := &models.FundingPaymentInput{
		ExchangeAccountID: accountID,
		BaseAsset:         "BTC",
		QuoteAsset:        "USDC",
		Amount:            "10.5",
		Timestamp:         time.Now(),
		PaymentID:         "payment-123",
	}

	payments, err := client.AddFundingPayments(ctx, []*FundingPaymentInput{input})
	if err != nil {
		t.Fatalf("AddFundingPayments failed: %v", err)
	}

	if len(payments) != 1 {
		t.Fatalf("Expected 1 payment, got %d", len(payments))
	}

	if payments[0].ID != expectedPayment.ID {
		t.Errorf("Expected ID %s, got %s", expectedPayment.ID, payments[0].ID)
	}
	if payments[0].BaseAsset != expectedPayment.BaseAsset {
		t.Errorf("Expected BaseAsset %s, got %s", expectedPayment.BaseAsset, payments[0].BaseAsset)
	}
}

func TestClient_AddFundingPayments_Multiple(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedPayments := []*models.FundingPayment{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			BaseAsset:         "BTC",
			QuoteAsset:        "USDC",
			Amount:            "10.5",
			Timestamp:         time.Now(),
			PaymentID:         "payment-123",
		},
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			BaseAsset:         "ETH",
			QuoteAsset:        "USDC",
			Amount:            "-5.25",
			Timestamp:         time.Now(),
			PaymentID:         "payment-456",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_funding_payments": map[string]interface{}{
					"returning": expectedPayments,
				},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	inputs := []*FundingPaymentInput{
		{
			ExchangeAccountID: accountID,
			BaseAsset:         "BTC",
			QuoteAsset:        "USDC",
			Amount:            "10.5",
			Timestamp:         time.Now(),
			PaymentID:         "payment-123",
		},
		{
			ExchangeAccountID: accountID,
			BaseAsset:         "ETH",
			QuoteAsset:        "USDC",
			Amount:            "-5.25",
			Timestamp:         time.Now(),
			PaymentID:         "payment-456",
		},
	}

	payments, err := client.AddFundingPayments(ctx, inputs)
	if err != nil {
		t.Fatalf("AddFundingPayments failed: %v", err)
	}

	if len(payments) != len(expectedPayments) {
		t.Fatalf("Expected %d payments, got %d", len(expectedPayments), len(payments))
	}

	for i, expected := range expectedPayments {
		if payments[i].PaymentID != expected.PaymentID {
			t.Errorf("Payment %d: Expected PaymentID %s, got %s", i, expected.PaymentID, payments[i].PaymentID)
		}
	}
}

func TestClient_AddFundingPayments_EmptyInput(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			t.Error("AddFundingPayments should not call GraphQL with empty input")
			return nil
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	payments, err := client.AddFundingPayments(ctx, []*FundingPaymentInput{})
	if err != nil {
		t.Fatalf("AddFundingPayments failed: %v", err)
	}

	if len(payments) != 0 {
		t.Errorf("Expected empty slice, got %d payments", len(payments))
	}
}

func TestClient_AddFundingPayments_Duplicate(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	// First call returns duplicate error, subsequent calls succeed
	callCount := 0
	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			callCount++
			if callCount == 1 {
				// Simulate duplicate error on batch insert
				return fmt.Errorf("duplicate key value violates unique constraint")
			}
			// Subsequent calls (individual inserts) succeed
			// Determine which payment based on call count
			var expectedPayment *models.FundingPayment
			if callCount == 2 {
				// First individual insert (payment-123) - duplicate, return empty
				return fmt.Errorf("duplicate key value violates unique constraint")
			}
			// Second individual insert (payment-456) - succeeds
			expectedPayment = &models.FundingPayment{
				ID:                uuid.New(),
				ExchangeAccountID: accountID,
				BaseAsset:         "ETH",
				QuoteAsset:        "USDC",
				Amount:            "-5.25",
				Timestamp:         time.Now(),
				PaymentID:         "payment-456",
			}
			respData := map[string]interface{}{
				"insert_funding_payments": map[string]interface{}{
					"returning": []*models.FundingPayment{expectedPayment},
				},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	inputs := []*FundingPaymentInput{
		{
			ExchangeAccountID: accountID,
			BaseAsset:         "BTC",
			QuoteAsset:        "USDC",
			Amount:            "10.5",
			Timestamp:         time.Now(),
			PaymentID:         "payment-123", // This will be duplicate
		},
		{
			ExchangeAccountID: accountID,
			BaseAsset:         "ETH",
			QuoteAsset:        "USDC",
			Amount:            "-5.25",
			Timestamp:         time.Now(),
			PaymentID:         "payment-456", // This will succeed
		},
	}

	payments, err := client.AddFundingPayments(ctx, inputs)
	// Should not return error, but may have partial success
	if err != nil {
		t.Fatalf("AddFundingPayments should handle duplicates gracefully, got error: %v", err)
	}

	// Should have at least one payment (the non-duplicate one)
	if len(payments) == 0 {
		t.Error("Expected at least one payment to be inserted despite duplicate")
	}
}
