package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/machinebox/graphql"
	"github.com/zif-terminal/lib/models"
)

func TestClient_GetWallet(t *testing.T) {
	ctx := context.Background()
	expectedWallet := &models.Wallet{
		ID:        "test-wallet-id",
		Address:   "HN4xHDBPK7oSGGRafaJWS6jT8M7xyEk7Kos24xp27Kpq",
		Chain:     "solana",
		CreatedAt: time.Now(),
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"wallets_by_pk": map[string]interface{}{
					"id":         expectedWallet.ID,
					"address":    expectedWallet.Address,
					"chain":      expectedWallet.Chain,
					"created_at": expectedWallet.CreatedAt.Format(time.RFC3339),
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

	wallet, err := client.GetWallet(ctx, "test-wallet-id")
	if err != nil {
		t.Fatalf("GetWallet failed: %v", err)
	}

	if wallet.ID != expectedWallet.ID {
		t.Errorf("Expected ID %s, got %s", expectedWallet.ID, wallet.ID)
	}
	if wallet.Address != expectedWallet.Address {
		t.Errorf("Expected Address %s, got %s", expectedWallet.Address, wallet.Address)
	}
	if wallet.Chain != expectedWallet.Chain {
		t.Errorf("Expected Chain %s, got %s", expectedWallet.Chain, wallet.Chain)
	}
}

func TestClient_GetWallet_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"wallets_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	_, err := client.GetWallet(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("Expected error for non-existent wallet")
	}
	if err.Error() != "wallet not found: non-existent-id" {
		t.Errorf("Expected 'wallet not found' error, got: %v", err)
	}
}

func TestClient_GetWalletByAddress(t *testing.T) {
	ctx := context.Background()
	expectedWallet := &models.Wallet{
		ID:        "test-wallet-id",
		Address:   "HN4xHDBPK7oSGGRafaJWS6jT8M7xyEk7Kos24xp27Kpq",
		Chain:     "solana",
		CreatedAt: time.Now(),
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"wallets": []map[string]interface{}{
					{
						"id":         expectedWallet.ID,
						"address":    expectedWallet.Address,
						"chain":      expectedWallet.Chain,
						"created_at": expectedWallet.CreatedAt.Format(time.RFC3339),
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

	wallet, err := client.GetWalletByAddress(ctx, expectedWallet.Address, "solana")
	if err != nil {
		t.Fatalf("GetWalletByAddress failed: %v", err)
	}

	if wallet.ID != expectedWallet.ID {
		t.Errorf("Expected ID %s, got %s", expectedWallet.ID, wallet.ID)
	}
	if wallet.Address != expectedWallet.Address {
		t.Errorf("Expected Address %s, got %s", expectedWallet.Address, wallet.Address)
	}
	if wallet.Chain != expectedWallet.Chain {
		t.Errorf("Expected Chain %s, got %s", expectedWallet.Chain, wallet.Chain)
	}
}

func TestClient_GetWalletByAddress_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"wallets": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	wallet, err := client.GetWalletByAddress(ctx, "non-existent-address", "solana")
	if err != nil {
		t.Fatalf("GetWalletByAddress should not error for not found: %v", err)
	}
	if wallet != nil {
		t.Error("Expected nil wallet for non-existent address")
	}
}

func TestClient_ListWallets(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	expectedWallets := []*models.Wallet{
		{ID: "id1", Address: "HN4xHDBPK7oSGGRafaJWS6jT8M7xyEk7Kos24xp27Kpq", Chain: "solana", CreatedAt: now},
		{ID: "id2", Address: "0x1234567890abcdef1234567890abcdef12345678", Chain: "ethereum", CreatedAt: now},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			walletsData := make([]map[string]interface{}, len(expectedWallets))
			for i, w := range expectedWallets {
				walletsData[i] = map[string]interface{}{
					"id":         w.ID,
					"address":    w.Address,
					"chain":      w.Chain,
					"created_at": w.CreatedAt.Format(time.RFC3339),
				}
			}
			respData := map[string]interface{}{
				"wallets": walletsData,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	wallets, err := client.ListWallets(ctx)
	if err != nil {
		t.Fatalf("ListWallets failed: %v", err)
	}

	if len(wallets) != len(expectedWallets) {
		t.Fatalf("Expected %d wallets, got %d", len(expectedWallets), len(wallets))
	}

	for i, exp := range expectedWallets {
		if wallets[i].ID != exp.ID {
			t.Errorf("Wallet %d: Expected ID %s, got %s", i, exp.ID, wallets[i].ID)
		}
		if wallets[i].Address != exp.Address {
			t.Errorf("Wallet %d: Expected Address %s, got %s", i, exp.Address, wallets[i].Address)
		}
		if wallets[i].Chain != exp.Chain {
			t.Errorf("Wallet %d: Expected Chain %s, got %s", i, exp.Chain, wallets[i].Chain)
		}
	}
}

func TestClient_ListWalletsByChain(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	expectedWallets := []*models.Wallet{
		{ID: "id1", Address: "HN4xHDBPK7oSGGRafaJWS6jT8M7xyEk7Kos24xp27Kpq", Chain: "solana", CreatedAt: now},
		{ID: "id2", Address: "6arBDzSNk7J1H2pC9uWm5K7V9yjN3Z8xLx5W2yMm1234", Chain: "solana", CreatedAt: now},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			walletsData := make([]map[string]interface{}, len(expectedWallets))
			for i, w := range expectedWallets {
				walletsData[i] = map[string]interface{}{
					"id":         w.ID,
					"address":    w.Address,
					"chain":      w.Chain,
					"created_at": w.CreatedAt.Format(time.RFC3339),
				}
			}
			respData := map[string]interface{}{
				"wallets": walletsData,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	wallets, err := client.ListWalletsByChain(ctx, "solana")
	if err != nil {
		t.Fatalf("ListWalletsByChain failed: %v", err)
	}

	if len(wallets) != len(expectedWallets) {
		t.Fatalf("Expected %d wallets, got %d", len(expectedWallets), len(wallets))
	}

	for i, w := range wallets {
		if w.Chain != "solana" {
			t.Errorf("Wallet %d: Expected Chain solana, got %s", i, w.Chain)
		}
	}
}

func TestClient_CreateWallet(t *testing.T) {
	ctx := context.Background()
	input := &models.WalletInput{
		Address: "HN4xHDBPK7oSGGRafaJWS6jT8M7xyEk7Kos24xp27Kpq",
		Chain:   "solana",
	}
	expectedWallet := &models.Wallet{
		ID:        "new-wallet-id",
		Address:   input.Address,
		Chain:     input.Chain,
		CreatedAt: time.Now(),
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_wallets_one": map[string]interface{}{
					"id":         expectedWallet.ID,
					"address":    expectedWallet.Address,
					"chain":      expectedWallet.Chain,
					"created_at": expectedWallet.CreatedAt.Format(time.RFC3339),
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

	wallet, err := client.CreateWallet(ctx, input)
	if err != nil {
		t.Fatalf("CreateWallet failed: %v", err)
	}

	if wallet.ID != expectedWallet.ID {
		t.Errorf("Expected ID %s, got %s", expectedWallet.ID, wallet.ID)
	}
	if wallet.Address != input.Address {
		t.Errorf("Expected Address %s, got %s", input.Address, wallet.Address)
	}
	if wallet.Chain != input.Chain {
		t.Errorf("Expected Chain %s, got %s", input.Chain, wallet.Chain)
	}
}

func TestClient_CreateWallet_Duplicate(t *testing.T) {
	ctx := context.Background()
	input := &models.WalletInput{
		Address: "HN4xHDBPK7oSGGRafaJWS6jT8M7xyEk7Kos24xp27Kpq",
		Chain:   "solana",
	}
	existingWallet := &models.Wallet{
		ID:        "existing-wallet-id",
		Address:   input.Address,
		Chain:     input.Chain,
		CreatedAt: time.Now(),
	}

	callCount := 0
	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			callCount++
			if callCount == 1 {
				// First call: insert returns nil (on_conflict returns existing)
				respData := map[string]interface{}{
					"insert_wallets_one": nil,
				}
				data, _ := json.Marshal(respData)
				return json.Unmarshal(data, resp)
			}
			// Second call: GetWalletByAddress returns existing wallet
			respData := map[string]interface{}{
				"wallets": []map[string]interface{}{
					{
						"id":         existingWallet.ID,
						"address":    existingWallet.Address,
						"chain":      existingWallet.Chain,
						"created_at": existingWallet.CreatedAt.Format(time.RFC3339),
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

	wallet, err := client.CreateWallet(ctx, input)
	if err != nil {
		t.Fatalf("CreateWallet failed: %v", err)
	}

	if wallet.ID != existingWallet.ID {
		t.Errorf("Expected existing wallet ID %s, got %s", existingWallet.ID, wallet.ID)
	}
}

func TestClient_DeleteWallet(t *testing.T) {
	ctx := context.Background()
	id := "test-wallet-id"

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_wallets_by_pk": map[string]interface{}{
					"id": id,
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

	err := client.DeleteWallet(ctx, id)
	if err != nil {
		t.Fatalf("DeleteWallet failed: %v", err)
	}
}

func TestClient_DeleteWallet_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_wallets_by_pk": map[string]interface{}{
					"id": "",
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

	err := client.DeleteWallet(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("Expected error for non-existent wallet")
	}
	if err.Error() != "wallet not found: non-existent-id" {
		t.Errorf("Expected 'wallet not found' error, got: %v", err)
	}
}
