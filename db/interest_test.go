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

func TestClient_AddInterestPayments(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	expectedPayments := []*models.InterestPayment{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Amount:            "5.25",
			OraclePrice:       "1.0",
			USDValue:          "5.25",
			Timestamp:         time.Now(),
			SnapshotFrom:      1700000000000,
			SnapshotTo:        1700100000000,
			IsApproximate:     true,
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_interest_payments": map[string]interface{}{
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

	inputs := []*InterestPaymentInput{
		{
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Amount:            "5.25",
			OraclePrice:       "1.0",
			USDValue:          "5.25",
			Timestamp:         time.Now(),
			SnapshotFrom:      1700000000000,
			SnapshotTo:        1700100000000,
			IsApproximate:     true,
		},
	}

	payments, err := client.AddInterestPayments(ctx, inputs)
	if err != nil {
		t.Fatalf("AddInterestPayments failed: %v", err)
	}

	if len(payments) != 1 {
		t.Fatalf("Expected 1 payment, got %d", len(payments))
	}

	if payments[0].Asset != "USDC" {
		t.Errorf("Expected USDC, got %s", payments[0].Asset)
	}
}

func TestClient_AddInterestPayments_Empty(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			t.Error("AddInterestPayments should not call GraphQL with empty input")
			return nil
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	payments, err := client.AddInterestPayments(ctx, []*InterestPaymentInput{})
	if err != nil {
		t.Fatalf("AddInterestPayments failed: %v", err)
	}

	if len(payments) != 0 {
		t.Errorf("Expected 0, got %d", len(payments))
	}
}

func TestClient_AddInterestPayments_WithOptionalFields(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_interest_payments": map[string]interface{}{
					"returning": []*models.InterestPayment{
						{
							ID:                uuid.New(),
							ExchangeAccountID: accountID,
							Asset:             "SOL",
							Amount:            "0.05",
						},
					},
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

	// Without oracle_price and usd_value
	inputs := []*InterestPaymentInput{
		{
			ExchangeAccountID: accountID,
			Asset:             "SOL",
			Amount:            "0.05",
			Timestamp:         time.Now(),
			SnapshotFrom:      1700000000000,
			SnapshotTo:        1700100000000,
		},
	}

	payments, err := client.AddInterestPayments(ctx, inputs)
	if err != nil {
		t.Fatalf("AddInterestPayments failed: %v", err)
	}

	if len(payments) != 1 {
		t.Fatalf("Expected 1, got %d", len(payments))
	}
}

func TestClient_DeleteInterestPaymentsForAccount(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_interest_payments": map[string]interface{}{
					"affected_rows": 10,
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

	count, err := client.DeleteInterestPaymentsForAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("DeleteInterestPaymentsForAccount failed: %v", err)
	}

	if count != 10 {
		t.Errorf("Expected 10, got %d", count)
	}
}

func TestClient_DeleteInterestPaymentsForAccount_Error(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			return fmt.Errorf("connection refused")
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	_, err := client.DeleteInterestPaymentsForAccount(ctx, accountID)
	if err == nil {
		t.Fatal("Expected error")
	}
}
