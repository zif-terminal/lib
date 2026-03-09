package db

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
	"github.com/zif-terminal/lib/models"
)

func TestClient_DeletePositionsForAccount(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_positions": map[string]interface{}{
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

	count, err := client.DeletePositionsForAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("DeletePositionsForAccount failed: %v", err)
	}

	if count != 5 {
		t.Errorf("Expected 5 affected rows, got %d", count)
	}
}

func TestClient_DeletePositionsForAccount_Error(t *testing.T) {
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

	_, err := client.DeletePositionsForAccount(ctx, accountID)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_AddPositions(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	expectedPositions := []*models.Position{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Market:            "SOL-PERP",
			MarketType:        "perp",
			Side:              "long",
			Status:            "closed",
			Quantity:          "100.5",
			EntryPrice:        "150.25",
			TotalFees:         "1.5",
			CumulativeFunding: "0.5",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_positions": map[string]interface{}{
					"returning": expectedPositions,
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

	inputs := []*PositionInput{
		{
			ExchangeAccountID: accountID,
			Market:            "SOL-PERP",
			MarketType:        "perp",
			Side:              "long",
			Status:            "closed",
			Quantity:          "100.5",
			EntryPrice:        "150.25",
			ExitPrice:         "160.00",
			RealizedPnL:       "975.0",
			TotalFees:         "1.5",
			CumulativeFunding: "0.5",
			QuoteAsset:        "USDC",
			StartTime:         1700000000000,
			EndTime:           1700100000000,
			OrderID:           "order-123",
		},
	}

	positions, err := client.AddPositions(ctx, inputs)
	if err != nil {
		t.Fatalf("AddPositions failed: %v", err)
	}

	if len(positions) != 1 {
		t.Fatalf("Expected 1 position, got %d", len(positions))
	}

	if positions[0].Market != "SOL-PERP" {
		t.Errorf("Expected market SOL-PERP, got %s", positions[0].Market)
	}
}

func TestClient_AddPositions_Empty(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			t.Error("AddPositions should not call GraphQL with empty input")
			return nil
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	positions, err := client.AddPositions(ctx, []*PositionInput{})
	if err != nil {
		t.Fatalf("AddPositions failed: %v", err)
	}

	if positions != nil {
		t.Errorf("Expected nil, got %v", positions)
	}
}

func TestClient_AddPositions_OpenPosition(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_positions": map[string]interface{}{
					"returning": []*models.Position{
						{
							ID:                uuid.New(),
							ExchangeAccountID: accountID,
							Market:            "BTC-PERP",
							MarketType:        "perp",
							Side:              "short",
							Status:            "open",
							Quantity:          "0.5",
							EntryPrice:        "60000",
							TotalFees:         "3.0",
							CumulativeFunding: "1.2",
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

	// Open position — no ExitPrice, RealizedPnL, EndTime, or OrderID
	inputs := []*PositionInput{
		{
			ExchangeAccountID: accountID,
			Market:            "BTC-PERP",
			MarketType:        "perp",
			Side:              "short",
			Status:            "open",
			Quantity:          "0.5",
			EntryPrice:        "60000",
			TotalFees:         "3.0",
			CumulativeFunding: "1.2",
			QuoteAsset:        "USDC",
			StartTime:         1700000000000,
		},
	}

	positions, err := client.AddPositions(ctx, inputs)
	if err != nil {
		t.Fatalf("AddPositions failed: %v", err)
	}

	if len(positions) != 1 {
		t.Fatalf("Expected 1 position, got %d", len(positions))
	}

	if positions[0].Status != "open" {
		t.Errorf("Expected status open, got %s", positions[0].Status)
	}
}

func TestClient_AddPositionEvents(t *testing.T) {
	ctx := context.Background()
	positionID := uuid.New()
	eventID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_position_events": map[string]interface{}{
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

	inputs := []*PositionEventInput{
		{
			PositionID: positionID,
			EventType:  "trade",
			EventID:    eventID,
			Direction:  "entry",
			Quantity:   "100.5",
			Price:      "150.25",
			Timestamp:  1700000000000,
		},
		{
			PositionID: positionID,
			EventType:  "funding",
			EventID:    uuid.New(),
			Direction:  "received",
			Quantity:   "0.5",
			Timestamp:  1700050000000,
		},
		{
			PositionID: positionID,
			EventType:  "trade",
			EventID:    uuid.New(),
			Direction:  "exit",
			Quantity:   "100.5",
			Price:      "160.00",
			Timestamp:  1700100000000,
		},
	}

	count, err := client.AddPositionEvents(ctx, inputs)
	if err != nil {
		t.Fatalf("AddPositionEvents failed: %v", err)
	}

	if count != 3 {
		t.Errorf("Expected 3 affected rows, got %d", count)
	}
}

func TestClient_AddPositionEvents_Empty(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			t.Error("AddPositionEvents should not call GraphQL with empty input")
			return nil
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	count, err := client.AddPositionEvents(ctx, []*PositionEventInput{})
	if err != nil {
		t.Fatalf("AddPositionEvents failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 affected rows, got %d", count)
	}
}

func TestClient_AddPositionEvents_NoPriceField(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_position_events": map[string]interface{}{
					"affected_rows": 1,
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

	inputs := []*PositionEventInput{
		{
			PositionID: uuid.New(),
			EventType:  "funding",
			EventID:    uuid.New(),
			Direction:  "received",
			Quantity:   "0.5",
			// Price intentionally empty
			Timestamp: 1700000000000,
		},
	}

	count, err := client.AddPositionEvents(ctx, inputs)
	if err != nil {
		t.Fatalf("AddPositionEvents failed: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 affected row, got %d", count)
	}
}

func TestClient_GetPositions(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	expectedPositions := []*models.Position{
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Market:            "SOL-PERP",
			MarketType:        "perp",
			Side:              "long",
			Status:            "closed",
			Quantity:          "100",
			EntryPrice:        "150",
			TotalFees:         "1.5",
			CumulativeFunding: "0.5",
		},
		{
			ID:                uuid.New(),
			ExchangeAccountID: accountID,
			Market:            "BTC-PERP",
			MarketType:        "perp",
			Side:              "short",
			Status:            "open",
			Quantity:          "0.5",
			EntryPrice:        "60000",
			TotalFees:         "3.0",
			CumulativeFunding: "1.2",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"positions": expectedPositions,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	status := "closed"
	marketType := "perp"
	market := "SOL-PERP"
	positions, err := client.GetPositions(ctx, PositionFilter{
		ExchangeAccountIDs: []uuid.UUID{accountID},
		Status:             &status,
		MarketType:         &marketType,
		Market:             &market,
	})
	if err != nil {
		t.Fatalf("GetPositions failed: %v", err)
	}

	if len(positions) != 2 {
		t.Errorf("Expected 2 positions, got %d", len(positions))
	}
}

func TestClient_GetPositions_NoFilter(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"positions": []*models.Position{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	positions, err := client.GetPositions(ctx, PositionFilter{})
	if err != nil {
		t.Fatalf("GetPositions failed: %v", err)
	}

	if len(positions) != 0 {
		t.Errorf("Expected 0 positions, got %d", len(positions))
	}
}
