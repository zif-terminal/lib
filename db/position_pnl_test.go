package db

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
)

func TestClient_ListMissingPositionPnL(t *testing.T) {
	ctx := context.Background()
	posID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"missing_position_pnl": []map[string]interface{}{
					{
						"position_id":  posID.String(),
						"denomination": "USDC",
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

	missing, err := client.ListMissingPositionPnL(ctx, 100)
	if err != nil {
		t.Fatalf("ListMissingPositionPnL failed: %v", err)
	}

	if len(missing) != 1 {
		t.Fatalf("Expected 1 missing position pnl, got %d", len(missing))
	}

	if missing[0].PositionID != posID {
		t.Errorf("Expected position_id %s, got %s", posID, missing[0].PositionID)
	}

	if missing[0].Denomination != "USDC" {
		t.Errorf("Expected denomination USDC, got %s", missing[0].Denomination)
	}
}

func TestClient_ListMissingPositionPnL_Empty(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"missing_position_pnl": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	missing, err := client.ListMissingPositionPnL(ctx, 100)
	if err != nil {
		t.Fatalf("ListMissingPositionPnL failed: %v", err)
	}

	if len(missing) != 0 {
		t.Errorf("Expected 0 missing position pnl, got %d", len(missing))
	}
}

func TestClient_ListMissingPositionPnL_Error(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			return fmt.Errorf("connection refused")
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	_, err := client.ListMissingPositionPnL(ctx, 100)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_AddPositionPnL(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_position_pnl": map[string]interface{}{
					"affected_rows": float64(2),
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

	inputs := []*PositionPnLInput{
		{PositionID: uuid.New(), Denomination: "USDC", RealizedPnL: "150.50"},
		{PositionID: uuid.New(), Denomination: "BTC", RealizedPnL: "-25.00"},
	}

	rows, err := client.AddPositionPnL(ctx, inputs)
	if err != nil {
		t.Fatalf("AddPositionPnL failed: %v", err)
	}

	if rows != 2 {
		t.Errorf("Expected 2 affected rows, got %d", rows)
	}
}

func TestClient_AddPositionPnL_Empty(t *testing.T) {
	ctx := context.Background()

	client := NewClientWithGraphQL(nil, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	rows, err := client.AddPositionPnL(ctx, []*PositionPnLInput{})
	if err != nil {
		t.Fatalf("AddPositionPnL failed: %v", err)
	}

	if rows != 0 {
		t.Errorf("Expected 0 affected rows, got %d", rows)
	}
}

func TestClient_AddPositionPnL_Error(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			return fmt.Errorf("connection refused")
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	inputs := []*PositionPnLInput{
		{PositionID: uuid.New(), Denomination: "USDC", RealizedPnL: "100.00"},
	}

	_, err := client.AddPositionPnL(ctx, inputs)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_GetPositionsByIDs(t *testing.T) {
	ctx := context.Background()
	id1 := uuid.New()
	id2 := uuid.New()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"positions": []map[string]interface{}{
					{
						"id":                  id1.String(),
						"exchange_account_id": accountID.String(),
						"market":              "SOL-PERP",
						"market_type":         "perp",
						"side":                "long",
						"status":              "closed",
						"quantity":            "100.5",
						"start_time":          float64(1700000000000),
						"end_time":            float64(1700100000000),
						"updated_at":          "2023-11-14T22:13:20Z",
					},
					{
						"id":                  id2.String(),
						"exchange_account_id": accountID.String(),
						"market":              "BTC-PERP",
						"market_type":         "perp",
						"side":                "short",
						"status":              "closed",
						"quantity":            "0.5",
						"start_time":          float64(1700000000000),
						"end_time":            float64(1700200000000),
						"updated_at":          "2023-11-16T01:46:40Z",
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

	positions, err := client.GetPositionsByIDs(ctx, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetPositionsByIDs failed: %v", err)
	}

	if len(positions) != 2 {
		t.Fatalf("Expected 2 positions, got %d", len(positions))
	}

	if positions[0].Market != "SOL-PERP" {
		t.Errorf("Expected market SOL-PERP, got %s", positions[0].Market)
	}

	if positions[1].Side != "short" {
		t.Errorf("Expected side short, got %s", positions[1].Side)
	}
}

func TestClient_GetPositionsByIDs_Empty(t *testing.T) {
	ctx := context.Background()

	client := NewClientWithGraphQL(nil, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	positions, err := client.GetPositionsByIDs(ctx, []uuid.UUID{})
	if err != nil {
		t.Fatalf("GetPositionsByIDs failed: %v", err)
	}

	if len(positions) != 0 {
		t.Errorf("Expected 0 positions, got %d", len(positions))
	}
}

func TestClient_GetPositionsByIDs_Error(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			return fmt.Errorf("connection refused")
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	_, err := client.GetPositionsByIDs(ctx, []uuid.UUID{uuid.New()})
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_GetEventValuesByEventIDs(t *testing.T) {
	ctx := context.Background()
	eventID1 := uuid.New()
	eventID2 := uuid.New()
	valueID1 := uuid.New()
	valueID2 := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"event_values": []map[string]interface{}{
					{
						"id":           valueID1.String(),
						"event_id":     eventID1.String(),
						"event_type":   "trade",
						"denomination": "USDC",
						"quantity":     "500.25",
					},
					{
						"id":           valueID2.String(),
						"event_id":     eventID2.String(),
						"event_type":   "settlement",
						"denomination": "USDC",
						"quantity":     "-100.50",
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

	values, err := client.GetEventValuesByEventIDs(ctx, []uuid.UUID{eventID1, eventID2}, "USDC")
	if err != nil {
		t.Fatalf("GetEventValuesByEventIDs failed: %v", err)
	}

	if len(values) != 2 {
		t.Fatalf("Expected 2 event values, got %d", len(values))
	}

	if values[0].EventType != "trade" {
		t.Errorf("Expected event_type trade, got %s", values[0].EventType)
	}

	if values[0].Quantity != "500.25" {
		t.Errorf("Expected quantity 500.25, got %s", values[0].Quantity)
	}

	if values[1].Denomination != "USDC" {
		t.Errorf("Expected denomination USDC, got %s", values[1].Denomination)
	}
}

func TestClient_GetEventValuesByEventIDs_Empty(t *testing.T) {
	ctx := context.Background()

	client := NewClientWithGraphQL(nil, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	values, err := client.GetEventValuesByEventIDs(ctx, []uuid.UUID{}, "USDC")
	if err != nil {
		t.Fatalf("GetEventValuesByEventIDs failed: %v", err)
	}

	if len(values) != 0 {
		t.Errorf("Expected 0 event values, got %d", len(values))
	}
}

func TestClient_GetEventValuesByEventIDs_Error(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			return fmt.Errorf("connection refused")
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	_, err := client.GetEventValuesByEventIDs(ctx, []uuid.UUID{uuid.New()}, "USDC")
	if err == nil {
		t.Fatal("Expected error")
	}
}
