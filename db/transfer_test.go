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

func TestClient_GetTransfersByIDs(t *testing.T) {
	ctx := context.Background()
	id1 := uuid.New()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"transfers": []map[string]interface{}{
					{
						"id":                  id1.String(),
						"exchange_account_id": accountID.String(),
						"type":                "deposit",
						"asset":               "USDC",
						"amount":              "500",
						"timestamp":           time.Now().UnixMilli(),
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

	transfers, err := client.GetTransfersByIDs(ctx, []uuid.UUID{id1})
	if err != nil {
		t.Fatalf("GetTransfersByIDs failed: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("Expected 1 transfer, got %d", len(transfers))
	}
}

func TestClient_GetTransfersByIDs_Empty(t *testing.T) {
	ctx := context.Background()
	client := NewClientWithGraphQL(&mockGraphQLClient{}, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})
	transfers, err := client.GetTransfersByIDs(ctx, []uuid.UUID{})
	if err != nil {
		t.Fatalf("GetTransfersByIDs failed: %v", err)
	}
	if len(transfers) != 0 {
		t.Fatalf("Expected 0 transfers, got %d", len(transfers))
	}
}

func TestClient_AddTransfers(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_transfers": map[string]interface{}{
					"returning": []map[string]interface{}{
						{
							"id":                  uuid.New().String(),
							"exchange_account_id": accountID.String(),
							"type":                "deposit",
							"asset":               "USDC",
							"amount":              "1000",
							"timestamp":           time.Now().UnixMilli(),
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

	inputs := []*models.TransferInput{
		{
			ExchangeAccountID: accountID,
			Type:              models.TypeDeposit,
			Asset:             "USDC",
			Amount:            "1000",
			Timestamp:         time.Now(),
			Metadata:          map[string]string{"source": "test"},
		},
	}

	transfers, err := client.AddTransfers(ctx, inputs)
	if err != nil {
		t.Fatalf("AddTransfers failed: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("Expected 1 transfer, got %d", len(transfers))
	}
}

func TestClient_AddTransfers_WithExternalID(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	returnedID := uuid.New()

	callCount := 0
	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			callCount++
			if callCount == 1 {
				// First call: upgradeTransferExternalIDs update mutation
				respData := map[string]interface{}{
					"update_transfers": map[string]interface{}{
						"affected_rows": 1,
					},
				}
				data, _ := json.Marshal(respData)
				return json.Unmarshal(data, resp)
			}
			// Second call: insert mutation
			respData := map[string]interface{}{
				"insert_transfers": map[string]interface{}{
					"returning": []map[string]interface{}{},
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

	inputs := []*models.TransferInput{
		{
			ExchangeAccountID: accountID,
			Type:              models.TypeDeposit,
			Asset:             "USDC",
			Amount:            "1000",
			Timestamp:         time.Now(),
			ExternalID:        "ext-123",
		},
	}

	transfers, err := client.AddTransfers(ctx, inputs)
	if err != nil {
		t.Fatalf("AddTransfers failed: %v", err)
	}

	// The upgrade mutation upgraded the existing row, so insert returns nothing new
	if len(transfers) != 0 {
		t.Fatalf("Expected 0 transfers (upgraded existing), got %d", len(transfers))
	}

	// Verify both mutations were called: upgrade + insert
	if callCount != 2 {
		t.Errorf("Expected 2 GraphQL calls (upgrade + insert), got %d", callCount)
	}

	_ = returnedID // suppress unused
}

func TestClient_AddTransfers_Empty(t *testing.T) {
	ctx := context.Background()
	client := NewClientWithGraphQL(&mockGraphQLClient{}, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})
	transfers, err := client.AddTransfers(ctx, []*models.TransferInput{})
	if err != nil {
		t.Fatalf("AddTransfers failed: %v", err)
	}
	if len(transfers) != 0 {
		t.Fatalf("Expected 0 transfers, got %d", len(transfers))
	}
}

func TestClient_DeleteTransfersByAccountAndType(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_transfers": map[string]interface{}{
					"affected_rows": 3,
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

	count, err := client.DeleteTransfersByAccountAndType(ctx, accountID, "deposit")
	if err != nil {
		t.Fatalf("DeleteTransfersByAccountAndType failed: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 affected rows, got %d", count)
	}
}

func TestClient_DeleteDerivedTransfers(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_transfers": map[string]interface{}{
					"affected_rows": 5,
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

	count, err := client.DeleteDerivedTransfers(ctx, accountID)
	if err != nil {
		t.Fatalf("DeleteDerivedTransfers failed: %v", err)
	}
	if count != 5 {
		t.Errorf("Expected 5 affected rows, got %d", count)
	}
}

func TestClient_GetLatestTransferByType(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"transfers": []map[string]interface{}{
					{
						"id":                  uuid.New().String(),
						"exchange_account_id": accountID.String(),
						"type":                "deposit",
						"asset":               "USDC",
						"amount":              "2000",
						"timestamp":           time.Now().UnixMilli(),
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

	transfer, err := client.GetLatestTransferByType(ctx, accountID, "deposit")
	if err != nil {
		t.Fatalf("GetLatestTransferByType failed: %v", err)
	}
	if transfer == nil {
		t.Fatal("Expected non-nil transfer")
	}
	if transfer.Type != "deposit" {
		t.Errorf("Expected type deposit, got %s", transfer.Type)
	}
}

func TestClient_GetLatestTransferByType_NotFound(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"transfers": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	transfer, err := client.GetLatestTransferByType(ctx, accountID, "deposit")
	if err != nil {
		t.Fatalf("GetLatestTransferByType failed: %v", err)
	}
	if transfer != nil {
		t.Error("Expected nil transfer for not found")
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

