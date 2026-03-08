package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
	"github.com/zif-terminal/lib/models"
)

func TestAbsNumeric(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"100.5", "100.500000000000000000"},
		{"-50.25", "50.250000000000000000"},
		{"0", "0.000000000000000000"},
		{"-0", "0.000000000000000000"},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := absNumeric(tt.input)
			if got != tt.want {
				t.Errorf("absNumeric(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClient_ListTransfers(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	expectedTransfers := []*models.Transfer{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Type:              models.TypeDeposit,
			Asset:             "USDC",
			Amount:            "1000.50",
			Timestamp:         time.Now(),
		},
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Type:              models.TypeWithdraw,
			Asset:             "SOL",
			Amount:            "10.5",
			Timestamp:         time.Now(),
		},
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Type:              models.TypeInterest,
			Asset:             "USDC",
			Amount:            "2.50",
			Timestamp:         time.Now(),
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"transfers": expectedTransfers,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	transfers, err := client.ListTransfers(ctx, TransferFilter{
		ExchangeAccountIDs: []uuid.UUID{accountID},
	})
	if err != nil {
		t.Fatalf("ListTransfers failed: %v", err)
	}

	if len(transfers) != 3 {
		t.Fatalf("Expected 3 transfers, got %d", len(transfers))
	}

	// Check types
	typeCount := map[string]int{}
	for _, tr := range transfers {
		typeCount[tr.Type]++
	}
	if typeCount["deposit"] != 1 {
		t.Errorf("Expected 1 deposit, got %d", typeCount["deposit"])
	}
	if typeCount["withdraw"] != 1 {
		t.Errorf("Expected 1 withdraw, got %d", typeCount["withdraw"])
	}
	if typeCount["interest"] != 1 {
		t.Errorf("Expected 1 interest, got %d", typeCount["interest"])
	}
}

func TestClient_ListFundingPayments(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	expectedPayments := []*models.FundingPayment{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			BaseAsset:         "SOL",
			QuoteAsset:        "USDC",
			Amount:            "10.5",
			Timestamp:         time.Now(),
			PaymentID:         "payment-123",
		},
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			BaseAsset:         "BTC",
			QuoteAsset:        "USDC",
			Amount:            "-3.25",
			Timestamp:         time.Now(),
			PaymentID:         "payment-456",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"funding_payments": expectedPayments,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	payments, err := client.ListFundingPayments(ctx, FundingPaymentFilter{
		ExchangeAccountIDs: []uuid.UUID{accountID},
	})
	if err != nil {
		t.Fatalf("ListFundingPayments failed: %v", err)
	}

	if len(payments) != 2 {
		t.Fatalf("Expected 2 payments, got %d", len(payments))
	}
}

func TestClient_ListFundingPayments_NoFilter(t *testing.T) {
	ctx := context.Background()

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

	payments, err := client.ListFundingPayments(ctx, FundingPaymentFilter{})
	if err != nil {
		t.Fatalf("ListFundingPayments failed: %v", err)
	}

	if len(payments) != 0 {
		t.Errorf("Expected 0 payments, got %d", len(payments))
	}
}
