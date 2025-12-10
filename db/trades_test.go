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

func TestClient_GetTrade(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedTrade := &models.Trade{
		ID:                uuid.New(),
		BaseAsset:         "BTC",
		QuoteAsset:        "USDC",
		Side:              "buy",
		Price:             "50000.50",
		Quantity:          "0.1",
		Timestamp:         time.Now(),
		Fee:               "5.00",
		OrderID:           "order-123",
		TradeID:           "trade-456",
		ExchangeAccountID: accountID,
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"trades_by_pk": expectedTrade,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	trade, err := client.GetTrade(ctx, expectedTrade.ID.String())
	if err != nil {
		t.Fatalf("GetTrade failed: %v", err)
	}

	if trade.ID != expectedTrade.ID {
		t.Errorf("Expected ID %s, got %s", expectedTrade.ID, trade.ID)
	}
	if trade.BaseAsset != expectedTrade.BaseAsset {
		t.Errorf("Expected BaseAsset %s, got %s", expectedTrade.BaseAsset, trade.BaseAsset)
	}
	if trade.Side != expectedTrade.Side {
		t.Errorf("Expected Side %s, got %s", expectedTrade.Side, trade.Side)
	}
}

func TestClient_GetTrade_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"trades_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	_, err := client.GetTrade(ctx, uuid.New().String())
	if err == nil {
		t.Fatal("Expected error for non-existent trade")
	}
	if err.Error() != "trade not found: "+uuid.New().String() {
		// Just check that error contains "trade not found"
		if err.Error()[:17] != "trade not found: " {
			t.Errorf("Expected 'trade not found' error, got: %v", err)
		}
	}
}

func TestClient_ListTrades_NoFilter(t *testing.T) {
	ctx := context.Background()
	accountID1 := uuid.New()
	accountID2 := uuid.New()

	expectedTrades := []*models.Trade{
		{
			ID:                uuid.New(),
			BaseAsset:         "BTC",
			QuoteAsset:        "USDC",
			Side:              "buy",
			Price:             "50000.50",
			Quantity:          "0.1",
			Timestamp:         time.Now(),
			Fee:               "5.00",
			OrderID:           "order-123",
			TradeID:           "trade-456",
			ExchangeAccountID: accountID1,
		},
		{
			ID:                uuid.New(),
			BaseAsset:         "ETH",
			QuoteAsset:        "USDC",
			Side:              "sell",
			Price:             "3000.25",
			Quantity:          "1.0",
			Timestamp:         time.Now(),
			Fee:               "3.00",
			OrderID:           "order-789",
			TradeID:           "trade-101",
			ExchangeAccountID: accountID2,
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"trades": expectedTrades,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	trades, err := client.ListTrades(ctx, models.TradeFilter{})
	if err != nil {
		t.Fatalf("ListTrades failed: %v", err)
	}

	if len(trades) != len(expectedTrades) {
		t.Errorf("Expected %d trades, got %d", len(expectedTrades), len(trades))
	}
}

func TestClient_ListTrades_WithFilter(t *testing.T) {
	ctx := context.Background()
	accountID1 := uuid.New()
	accountID2 := uuid.New()

	expectedTrades := []*models.Trade{
		{
			ID:                uuid.New(),
			BaseAsset:         "BTC",
			QuoteAsset:        "USDC",
			Side:              "buy",
			Price:             "50000.50",
			Quantity:          "0.1",
			Timestamp:         time.Now(),
			Fee:               "5.00",
			OrderID:           "order-123",
			TradeID:           "trade-456",
			ExchangeAccountID: accountID1,
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"trades": expectedTrades,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	filter := models.TradeFilter{
		ExchangeAccountIDs: []uuid.UUID{accountID1, accountID2},
	}

	trades, err := client.ListTrades(ctx, filter)
	if err != nil {
		t.Fatalf("ListTrades failed: %v", err)
	}

	if len(trades) != len(expectedTrades) {
		t.Errorf("Expected %d trades, got %d", len(expectedTrades), len(trades))
	}
}

func TestClient_CreateTrade(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	expectedTrade := &models.Trade{
		ID:                uuid.New(),
		BaseAsset:         "BTC",
		QuoteAsset:        "USDC",
		Side:              "buy",
		Price:             "50000.50",
		Quantity:          "0.1",
		Timestamp:         time.Now(),
		Fee:               "5.00",
		OrderID:           "order-123",
		TradeID:           "trade-456",
		ExchangeAccountID: accountID,
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_trades_one": expectedTrade,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	input := &models.TradeInput{
		BaseAsset:         "BTC",
		QuoteAsset:        "USDC",
		Side:              "buy",
		Price:             "50000.50",
		Quantity:          "0.1",
		Timestamp:         time.Now(),
		Fee:               "5.00",
		OrderID:           "order-123",
		TradeID:           "trade-456",
		ExchangeAccountID: accountID,
	}

	trade, err := client.CreateTrade(ctx, input)
	if err != nil {
		t.Fatalf("CreateTrade failed: %v", err)
	}

	if trade.ID != expectedTrade.ID {
		t.Errorf("Expected ID %s, got %s", expectedTrade.ID, trade.ID)
	}
	if trade.BaseAsset != expectedTrade.BaseAsset {
		t.Errorf("Expected BaseAsset %s, got %s", expectedTrade.BaseAsset, trade.BaseAsset)
	}
}

func TestClient_UpdateTrade(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	tradeID := uuid.New()
	expectedTrade := &models.Trade{
		ID:                tradeID,
		BaseAsset:         "ETH",
		QuoteAsset:        "USDC",
		Side:              "sell",
		Price:             "3000.25",
		Quantity:          "1.0",
		Timestamp:         time.Now(),
		Fee:               "3.00",
		OrderID:           "order-789",
		TradeID:           "trade-101",
		ExchangeAccountID: accountID,
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_trades_by_pk": expectedTrade,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	input := &models.TradeInput{
		BaseAsset:         "ETH",
		QuoteAsset:        "USDC",
		Side:              "sell",
		Price:             "3000.25",
		Quantity:          "1.0",
		Timestamp:         time.Now(),
		Fee:               "3.00",
		OrderID:           "order-789",
		TradeID:           "trade-101",
		ExchangeAccountID: accountID,
	}

	trade, err := client.UpdateTrade(ctx, tradeID.String(), input)
	if err != nil {
		t.Fatalf("UpdateTrade failed: %v", err)
	}

	if trade.ID != expectedTrade.ID {
		t.Errorf("Expected ID %s, got %s", expectedTrade.ID, trade.ID)
	}
	if trade.BaseAsset != expectedTrade.BaseAsset {
		t.Errorf("Expected BaseAsset %s, got %s", expectedTrade.BaseAsset, trade.BaseAsset)
	}
}

func TestClient_DeleteTrade(t *testing.T) {
	ctx := context.Background()
	tradeID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_trades_by_pk": map[string]interface{}{
					"id": tradeID.String(),
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

	err := client.DeleteTrade(ctx, tradeID.String())
	if err != nil {
		t.Fatalf("DeleteTrade failed: %v", err)
	}
}

func TestClient_LatestTrade(t *testing.T) {
	ctx := context.Background()
	accountID1 := uuid.New()
	accountID2 := uuid.New()

	// Return trades ordered by timestamp desc - first one per account is latest
	expectedTrades := []*models.Trade{
		{
			ID:                uuid.New(),
			BaseAsset:         "BTC",
			QuoteAsset:        "USDC",
			Side:              "buy",
			Price:             "50000.50",
			Quantity:          "0.1",
			Timestamp:         time.Now().Add(1 * time.Hour), // Latest for account1
			Fee:               "5.00",
			OrderID:           "order-123",
			TradeID:           "trade-456",
			ExchangeAccountID: accountID1,
		},
		{
			ID:                uuid.New(),
			BaseAsset:         "ETH",
			QuoteAsset:        "USDC",
			Side:              "sell",
			Price:             "3000.25",
			Quantity:          "1.0",
			Timestamp:         time.Now().Add(2 * time.Hour), // Latest for account2
			Fee:               "3.00",
			OrderID:           "order-789",
			TradeID:           "trade-101",
			ExchangeAccountID: accountID2,
		},
		{
			ID:                uuid.New(),
			BaseAsset:         "BTC",
			QuoteAsset:        "USDC",
			Side:              "buy",
			Price:             "49000.00",
			Quantity:          "0.05",
			Timestamp:         time.Now(), // Older for account1
			Fee:               "2.50",
			OrderID:           "order-111",
			TradeID:           "trade-222",
			ExchangeAccountID: accountID1,
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"trades": expectedTrades,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	latestTrades, err := client.LatestTrade(ctx, []uuid.UUID{accountID1, accountID2})
	if err != nil {
		t.Fatalf("LatestTrade failed: %v", err)
	}

	if len(latestTrades) != 2 {
		t.Errorf("Expected 2 latest trades, got %d", len(latestTrades))
	}

	// Check account1's latest trade
	if latestTrades[accountID1].TradeID != "trade-456" {
		t.Errorf("Expected account1 latest trade ID 'trade-456', got '%s'", latestTrades[accountID1].TradeID)
	}

	// Check account2's latest trade
	if latestTrades[accountID2].TradeID != "trade-101" {
		t.Errorf("Expected account2 latest trade ID 'trade-101', got '%s'", latestTrades[accountID2].TradeID)
	}
}

func TestClient_LatestTrade_EmptyInput(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			// Should not be called
			t.Error("LatestTrade should not call GraphQL with empty input")
			return nil
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	latestTrades, err := client.LatestTrade(ctx, []uuid.UUID{})
	if err != nil {
		t.Fatalf("LatestTrade failed: %v", err)
	}

	if len(latestTrades) != 0 {
		t.Errorf("Expected empty map, got %d entries", len(latestTrades))
	}
}
