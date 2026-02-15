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

func TestClient_GetLatestDeposit(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedDeposit := &models.Deposit{
		ID:                uuid.New(),
		ExchangeAccountID: accountID,
		Asset:             "USDC",
		Direction:         "deposit",
		Amount:            "100.00",
		UserCostBasis:     "1.00",
		Timestamp:         time.Now(),
		DepositID:         "deposit-123",
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"deposits": []*models.Deposit{expectedDeposit},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	deposit, err := client.GetLatestDeposit(ctx, accountID)
	if err != nil {
		t.Fatalf("GetLatestDeposit failed: %v", err)
	}

	if deposit.ID != expectedDeposit.ID {
		t.Errorf("Expected ID %s, got %s", expectedDeposit.ID, deposit.ID)
	}
	if deposit.Asset != expectedDeposit.Asset {
		t.Errorf("Expected Asset %s, got %s", expectedDeposit.Asset, deposit.Asset)
	}
	if deposit.Amount != expectedDeposit.Amount {
		t.Errorf("Expected Amount %s, got %s", expectedDeposit.Amount, deposit.Amount)
	}
	if deposit.Direction != expectedDeposit.Direction {
		t.Errorf("Expected Direction %s, got %s", expectedDeposit.Direction, deposit.Direction)
	}
}

func TestClient_GetLatestDeposit_NotFound(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"deposits": []*models.Deposit{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	deposit, err := client.GetLatestDeposit(ctx, accountID)
	if err != nil {
		t.Fatalf("GetLatestDeposit failed: %v", err)
	}

	if deposit != nil {
		t.Errorf("Expected nil deposit, got %v", deposit)
	}
}

func TestClient_AddDeposits_Single(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedDeposit := &models.Deposit{
		ID:                uuid.New(),
		ExchangeAccountID: accountID,
		Asset:             "USDC",
		Direction:         "deposit",
		Amount:            "100.00",
		UserCostBasis:     "1.00",
		Timestamp:         time.Now(),
		DepositID:         "deposit-123",
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_deposits": map[string]interface{}{
					"returning": []*models.Deposit{expectedDeposit},
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

	input := &models.DepositInput{
		ExchangeAccountID: accountID,
		Asset:             "USDC",
		Direction:         "deposit",
		Amount:            "100.00",
		UserCostBasis:     "1.00",
		Timestamp:         time.Now(),
		DepositID:         "deposit-123",
	}

	deposits, err := client.AddDeposits(ctx, []*DepositInput{input})
	if err != nil {
		t.Fatalf("AddDeposits failed: %v", err)
	}

	if len(deposits) != 1 {
		t.Fatalf("Expected 1 deposit, got %d", len(deposits))
	}

	if deposits[0].ID != expectedDeposit.ID {
		t.Errorf("Expected ID %s, got %s", expectedDeposit.ID, deposits[0].ID)
	}
	if deposits[0].Asset != expectedDeposit.Asset {
		t.Errorf("Expected Asset %s, got %s", expectedDeposit.Asset, deposits[0].Asset)
	}
}

func TestClient_AddDeposits_Multiple(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedDeposits := []*models.Deposit{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Direction:         "deposit",
			Amount:            "100.00",
			UserCostBasis:     "1.00",
			Timestamp:         time.Now(),
			DepositID:         "deposit-123",
		},
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Asset:             "SOL",
			Direction:         "withdraw",
			Amount:            "50.00",
			UserCostBasis:     "80.00",
			Timestamp:         time.Now(),
			DepositID:         "withdraw-456",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_deposits": map[string]interface{}{
					"returning": expectedDeposits,
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

	inputs := []*DepositInput{
		{
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Direction:         "deposit",
			Amount:            "100.00",
			UserCostBasis:     "1.00",
			Timestamp:         time.Now(),
			DepositID:         "deposit-123",
		},
		{
			ExchangeAccountID: accountID,
			Asset:             "SOL",
			Direction:         "withdraw",
			Amount:            "50.00",
			UserCostBasis:     "80.00",
			Timestamp:         time.Now(),
			DepositID:         "withdraw-456",
		},
	}

	deposits, err := client.AddDeposits(ctx, inputs)
	if err != nil {
		t.Fatalf("AddDeposits failed: %v", err)
	}

	if len(deposits) != len(expectedDeposits) {
		t.Fatalf("Expected %d deposits, got %d", len(expectedDeposits), len(deposits))
	}

	for i, expected := range expectedDeposits {
		if deposits[i].DepositID != expected.DepositID {
			t.Errorf("Deposit %d: Expected DepositID %s, got %s", i, expected.DepositID, deposits[i].DepositID)
		}
	}
}

func TestClient_AddDeposits_EmptyInput(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			t.Error("AddDeposits should not call GraphQL with empty input")
			return nil
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	deposits, err := client.AddDeposits(ctx, []*DepositInput{})
	if err != nil {
		t.Fatalf("AddDeposits failed: %v", err)
	}

	if len(deposits) != 0 {
		t.Errorf("Expected empty slice, got %d deposits", len(deposits))
	}
}

func TestClient_AddDeposits_Duplicate(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			return fmt.Errorf("duplicate key value violates unique constraint")
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	inputs := []*DepositInput{
		{
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Direction:         "deposit",
			Amount:            "100.00",
			UserCostBasis:     "1.00",
			Timestamp:         time.Now(),
			DepositID:         "deposit-123",
		},
	}

	_, err := client.AddDeposits(ctx, inputs)
	if err == nil {
		t.Fatal("Expected error for duplicate deposit")
	}
	if err.Error() == "" {
		t.Error("Error message should not be empty")
	}
}

func TestClient_UpdateDepositCostBasis(t *testing.T) {
	ctx := context.Background()
	depositID := uuid.New()
	accountID := uuid.New()
	expectedDeposit := &models.Deposit{
		ID:                depositID,
		ExchangeAccountID: accountID,
		Asset:             "SOL",
		Direction:         "deposit",
		Amount:            "10.00",
		UserCostBasis:     "150.00",
		Timestamp:         time.Now(),
		DepositID:         "deposit-789",
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_deposits_by_pk": expectedDeposit,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	deposit, err := client.UpdateDepositCostBasis(ctx, depositID, "150.00")
	if err != nil {
		t.Fatalf("UpdateDepositCostBasis failed: %v", err)
	}

	if deposit.UserCostBasis != "150.00" {
		t.Errorf("Expected UserCostBasis '150.00', got '%s'", deposit.UserCostBasis)
	}
}

func TestClient_ListDeposits(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedDeposits := []*models.Deposit{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Direction:         "deposit",
			Amount:            "100.00",
			UserCostBasis:     "1.00",
			Timestamp:         time.Now(),
			DepositID:         "deposit-123",
		},
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Asset:             "SOL",
			Direction:         "withdraw",
			Amount:            "50.00",
			UserCostBasis:     "80.00",
			Timestamp:         time.Now(),
			DepositID:         "withdraw-456",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"deposits": expectedDeposits,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	deposits, err := client.ListDeposits(ctx, DepositFilter{
		ExchangeAccountIDs: []uuid.UUID{accountID},
	})
	if err != nil {
		t.Fatalf("ListDeposits failed: %v", err)
	}

	if len(deposits) != len(expectedDeposits) {
		t.Fatalf("Expected %d deposits, got %d", len(expectedDeposits), len(deposits))
	}
}

func TestClient_ListDeposits_WithAssetFilter(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedDeposits := []*models.Deposit{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Direction:         "deposit",
			Amount:            "100.00",
			Timestamp:         time.Now(),
			DepositID:         "deposit-123",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"deposits": expectedDeposits,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	deposits, err := client.ListDeposits(ctx, DepositFilter{
		ExchangeAccountIDs: []uuid.UUID{accountID},
		Assets:             []string{"USDC"},
	})
	if err != nil {
		t.Fatalf("ListDeposits failed: %v", err)
	}

	if len(deposits) != 1 {
		t.Fatalf("Expected 1 deposit, got %d", len(deposits))
	}
	if deposits[0].Asset != "USDC" {
		t.Errorf("Expected Asset 'USDC', got '%s'", deposits[0].Asset)
	}
}

func TestClient_ListDeposits_WithDirectionFilter(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedDeposits := []*models.Deposit{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Direction:         "deposit",
			Amount:            "100.00",
			Timestamp:         time.Now(),
			DepositID:         "deposit-123",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"deposits": expectedDeposits,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	deposits, err := client.ListDeposits(ctx, DepositFilter{
		Directions: []string{"deposit"},
	})
	if err != nil {
		t.Fatalf("ListDeposits failed: %v", err)
	}

	if len(deposits) != 1 {
		t.Fatalf("Expected 1 deposit, got %d", len(deposits))
	}
	if deposits[0].Direction != "deposit" {
		t.Errorf("Expected Direction 'deposit', got '%s'", deposits[0].Direction)
	}
}

func TestClient_ListDeposits_EmptyResult(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"deposits": []*models.Deposit{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	deposits, err := client.ListDeposits(ctx, DepositFilter{})
	if err != nil {
		t.Fatalf("ListDeposits failed: %v", err)
	}

	if len(deposits) != 0 {
		t.Errorf("Expected empty slice, got %d deposits", len(deposits))
	}
}

func TestJoinConditions(t *testing.T) {
	tests := []struct {
		name       string
		conditions []string
		expected   string
	}{
		{
			name:       "empty",
			conditions: []string{},
			expected:   "",
		},
		{
			name:       "single condition",
			conditions: []string{"foo: { _eq: 1 }"},
			expected:   "foo: { _eq: 1 }",
		},
		{
			name:       "multiple conditions",
			conditions: []string{"foo: { _eq: 1 }", "bar: { _eq: 2 }"},
			expected:   "foo: { _eq: 1 }, bar: { _eq: 2 }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinConditions(tt.conditions)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
