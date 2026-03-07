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

func TestClient_AddSettlements(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_settlements": map[string]interface{}{
					"affected_rows": 2,
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

	inputs := []*SettlementInput{
		{
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Amount:            "150.25",
			Market:            "SOL-PERP",
			Timestamp:         time.Now(),
			SettlementID:      "settle-123",
		},
		{
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Amount:            "-50.10",
			Market:            "BTC-PERP",
			Timestamp:         time.Now(),
			SettlementID:      "settle-456",
		},
	}

	count, err := client.AddSettlements(ctx, inputs)
	if err != nil {
		t.Fatalf("AddSettlements failed: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 affected rows, got %d", count)
	}
}

func TestClient_AddSettlements_Empty(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			t.Error("AddSettlements should not call GraphQL with empty input")
			return nil
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	count, err := client.AddSettlements(ctx, []*SettlementInput{})
	if err != nil {
		t.Fatalf("AddSettlements failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0, got %d", count)
	}
}

func TestClient_AddSettlements_Error(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			return fmt.Errorf("duplicate key")
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	inputs := []*SettlementInput{
		{
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Amount:            "100",
			Market:            "SOL-PERP",
			Timestamp:         time.Now(),
			SettlementID:      "settle-789",
		},
	}

	_, err := client.AddSettlements(ctx, inputs)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_ListSettlements(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	expectedSettlements := []*models.Settlement{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Asset:             "USDC",
			Amount:            "150.25",
			Market:            "SOL-PERP",
			Timestamp:         time.Now(),
			SettlementID:      "settle-123",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"settlements": expectedSettlements,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	settlements, err := client.ListSettlements(ctx, SettlementFilter{
		ExchangeAccountIDs: []uuid.UUID{accountID},
	})
	if err != nil {
		t.Fatalf("ListSettlements failed: %v", err)
	}

	if len(settlements) != 1 {
		t.Fatalf("Expected 1 settlement, got %d", len(settlements))
	}

	if settlements[0].SettlementID != "settle-123" {
		t.Errorf("Expected settle-123, got %s", settlements[0].SettlementID)
	}
}

func TestClient_ListSettlements_NoFilter(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"settlements": []*models.Settlement{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	settlements, err := client.ListSettlements(ctx, SettlementFilter{})
	if err != nil {
		t.Fatalf("ListSettlements failed: %v", err)
	}

	if len(settlements) != 0 {
		t.Errorf("Expected 0 settlements, got %d", len(settlements))
	}
}

func TestClient_GetLatestSettlement(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	expectedSettlement := &models.Settlement{
		ID:                uuid.New(),
		ExchangeAccountID: accountID,
		Asset:             "USDC",
		Amount:            "200.50",
		Market:            "ETH-PERP",
		Timestamp:         time.Now(),
		SettlementID:      "settle-latest",
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"settlements": []*models.Settlement{expectedSettlement},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	settlement, err := client.GetLatestSettlement(ctx, accountID)
	if err != nil {
		t.Fatalf("GetLatestSettlement failed: %v", err)
	}

	if settlement.SettlementID != "settle-latest" {
		t.Errorf("Expected settle-latest, got %s", settlement.SettlementID)
	}
}

func TestClient_GetLatestSettlement_NotFound(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"settlements": []*models.Settlement{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	settlement, err := client.GetLatestSettlement(ctx, accountID)
	if err != nil {
		t.Fatalf("GetLatestSettlement failed: %v", err)
	}

	if settlement != nil {
		t.Errorf("Expected nil, got %v", settlement)
	}
}
