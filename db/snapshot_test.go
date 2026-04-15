package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
)

func TestClient_GetAllBalanceSnapshots(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{
					{"asset": "USDC", "balance": "1000.50", "timestamp": 1700000000000},
					{"asset": "SOL", "balance": "25.5", "timestamp": 1700000001000},
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

	balances, err := client.GetAllBalanceSnapshots(ctx, accountID, 0)
	if err != nil {
		t.Fatalf("GetAllBalanceSnapshots failed: %v", err)
	}
	if len(balances) != 2 {
		t.Fatalf("Expected 2 balances, got %d", len(balances))
	}
	if balances[0].Asset != "USDC" {
		t.Errorf("Expected first asset USDC, got %s", balances[0].Asset)
	}
	if balances[0].Balance != "1000.50" {
		t.Errorf("Expected balance 1000.50, got %s", balances[0].Balance)
	}
}

func TestClient_AddSpotBalanceSnapshots(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_spot_balance_snapshots": map[string]interface{}{
					"returning": []map[string]interface{}{
						{
							"id":                  uuid.New().String(),
							"exchange_account_id": accountID.String(),
							"asset":               "USDC",
							"balance":             "500",
							"timestamp":           1700000000000,
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

	inputs := []*SpotBalanceSnapshotInput{
		{ExchangeAccountID: accountID, Asset: "USDC", Balance: "500", Timestamp: time.UnixMilli(1700000000000)},
	}
	snapshots, err := client.AddSpotBalanceSnapshots(ctx, inputs)
	if err != nil {
		t.Fatalf("AddSpotBalanceSnapshots failed: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("Expected 1 snapshot, got %d", len(snapshots))
	}
}

func TestClient_AddSpotBalanceSnapshots_Empty(t *testing.T) {
	ctx := context.Background()
	client := NewClientWithGraphQL(&mockGraphQLClient{}, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})
	snapshots, err := client.AddSpotBalanceSnapshots(ctx, []*SpotBalanceSnapshotInput{})
	if err != nil {
		t.Fatalf("AddSpotBalanceSnapshots failed: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("Expected 0 snapshots, got %d", len(snapshots))
	}
}

func TestClient_GetLatestSpotBalanceSnapshot(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{
					{
						"id":                  uuid.New().String(),
						"exchange_account_id": accountID.String(),
						"asset":               "USDC",
						"balance":             "1500",
						"timestamp":           1700000000000,
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

	snapshot, err := client.GetLatestSpotBalanceSnapshot(ctx, accountID, "USDC")
	if err != nil {
		t.Fatalf("GetLatestSpotBalanceSnapshot failed: %v", err)
	}
	if snapshot == nil {
		t.Fatal("Expected non-nil snapshot")
	}
	if snapshot.Asset != "USDC" {
		t.Errorf("Expected asset USDC, got %s", snapshot.Asset)
	}
}

func TestClient_GetLatestSpotBalanceSnapshot_NotFound(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	snapshot, err := client.GetLatestSpotBalanceSnapshot(ctx, accountID, "USDC")
	if err != nil {
		t.Fatalf("GetLatestSpotBalanceSnapshot failed: %v", err)
	}
	if snapshot != nil {
		t.Error("Expected nil snapshot")
	}
}

func TestClient_GetSpotBalanceSnapshotsBefore(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{
					{
						"id":                  uuid.New().String(),
						"exchange_account_id": accountID.String(),
						"asset":               "USDC",
						"balance":             "800",
						"timestamp":           1699999999000,
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

	snapshot, err := client.GetSpotBalanceSnapshotsBefore(ctx, accountID, "USDC", 1700000000000)
	if err != nil {
		t.Fatalf("GetSpotBalanceSnapshotsBefore failed: %v", err)
	}
	if snapshot == nil {
		t.Fatal("Expected non-nil snapshot")
	}
}

func TestClient_GetSpotBalanceSnapshotsBefore_NotFound(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	snapshot, err := client.GetSpotBalanceSnapshotsBefore(ctx, accountID, "USDC", 1700000000000)
	if err != nil {
		t.Fatalf("GetSpotBalanceSnapshotsBefore failed: %v", err)
	}
	if snapshot != nil {
		t.Error("Expected nil snapshot")
	}
}

func TestClient_ListSpotBalanceSnapshots(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{
					{"id": uuid.New().String(), "exchange_account_id": accountID.String(), "asset": "USDC", "balance": "100", "timestamp": 1700000000000},
					{"id": uuid.New().String(), "exchange_account_id": accountID.String(), "asset": "USDC", "balance": "200", "timestamp": 1700000001000},
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

	snapshots, err := client.ListSpotBalanceSnapshots(ctx, accountID, "USDC")
	if err != nil {
		t.Fatalf("ListSpotBalanceSnapshots failed: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("Expected 2 snapshots, got %d", len(snapshots))
	}
}

func TestClient_PruneOldSpotBalanceSnapshots(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_spot_balance_snapshots": map[string]interface{}{
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

	count, err := client.PruneOldSpotBalanceSnapshots(ctx, 1700000000000)
	if err != nil {
		t.Fatalf("PruneOldSpotBalanceSnapshots failed: %v", err)
	}
	if count != 10 {
		t.Errorf("Expected 10 affected rows, got %d", count)
	}
}

func TestClient_UpdateAccountTypeMetadata(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_exchange_accounts_by_pk": map[string]interface{}{
					"id": accountID.String(),
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

	metadata := json.RawMessage(`{"key": "value"}`)
	err := client.UpdateAccountTypeMetadata(ctx, accountID, metadata)
	if err != nil {
		t.Fatalf("UpdateAccountTypeMetadata failed: %v", err)
	}
}

func TestClient_GetDistinctTransferAssets(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"transfers": []map[string]interface{}{
					{"asset": "USDC"},
					{"asset": "SOL"},
					{"asset": ""},
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

	assets, err := client.GetDistinctTransferAssets(ctx, accountID)
	if err != nil {
		t.Fatalf("GetDistinctTransferAssets failed: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("Expected 2 assets (empty filtered), got %d", len(assets))
	}
}

func TestClient_GetLatestBalanceSnapshots(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{
					{
						"asset":        "SOL",
						"balance":      "25.5",
					},
					{
						"asset":        "BTC",
						"balance":      "0.5",
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

	balances, err := client.GetLatestBalanceSnapshots(ctx, accountID)
	if err != nil {
		t.Fatalf("GetLatestBalanceSnapshots failed: %v", err)
	}

	if len(balances) != 2 {
		t.Fatalf("Expected 2 balances, got %d", len(balances))
	}

	// Check SOL
	if balances[0].Asset != "SOL" {
		t.Errorf("Expected first asset SOL, got %s", balances[0].Asset)
	}
	if balances[0].Balance != "25.5" {
		t.Errorf("Expected SOL balance 25.5, got %s", balances[0].Balance)
	}
	// Check BTC
	if balances[1].Asset != "BTC" {
		t.Errorf("Expected second asset BTC, got %s", balances[1].Asset)
	}
	if balances[1].Balance != "0.5" {
		t.Errorf("Expected BTC balance 0.5, got %s", balances[1].Balance)
	}
}

func TestClient_GetLatestBalanceSnapshots_Empty(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	balances, err := client.GetLatestBalanceSnapshots(ctx, accountID)
	if err != nil {
		t.Fatalf("GetLatestBalanceSnapshots failed: %v", err)
	}

	if len(balances) != 0 {
		t.Errorf("Expected 0 balances, got %d", len(balances))
	}
}

