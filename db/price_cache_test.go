package db

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/machinebox/graphql"
)

func TestClient_AddPrices(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_price_cache": map[string]interface{}{
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

	inputs := []*PriceInput{
		{Asset: "SOL", Denomination: "USDC", TimestampMs: 1700000000000, Price: "100.50", Source: "drift"},
		{Asset: "BTC", Denomination: "USDC", TimestampMs: 1700000000000, Price: "43000.00", Source: "drift"},
	}

	rows, err := client.AddPrices(ctx, inputs)
	if err != nil {
		t.Fatalf("AddPrices failed: %v", err)
	}

	if rows != 2 {
		t.Errorf("Expected 2 affected rows, got %d", rows)
	}
}

func TestClient_AddPrices_Empty(t *testing.T) {
	ctx := context.Background()

	client := NewClientWithGraphQL(nil, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	rows, err := client.AddPrices(ctx, []*PriceInput{})
	if err != nil {
		t.Fatalf("AddPrices failed: %v", err)
	}

	if rows != 0 {
		t.Errorf("Expected 0 affected rows, got %d", rows)
	}
}

func TestClient_AddPrices_Error(t *testing.T) {
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

	inputs := []*PriceInput{
		{Asset: "SOL", Denomination: "USDC", TimestampMs: 1700000000000, Price: "100.50", Source: "drift"},
	}

	_, err := client.AddPrices(ctx, inputs)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_GetNearestPrice(t *testing.T) {
	ctx := context.Background()

	targetTs := int64(1700000005000)

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			// Return two prices: one before and one after the target
			respData := map[string]interface{}{
				"price_cache": []map[string]interface{}{
					{
						"asset":        "SOL",
						"denomination": "USDC",
						"timestamp":    float64(1700000003000), // 2s before target
						"price":        "100.00",
						"source":       "drift",
					},
					{
						"asset":        "SOL",
						"denomination": "USDC",
						"timestamp":    float64(1700000006000), // 1s after target
						"price":        "101.00",
						"source":       "drift",
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

	price, err := client.GetNearestPrice(ctx, "SOL", "USDC", targetTs, 10000)
	if err != nil {
		t.Fatalf("GetNearestPrice failed: %v", err)
	}

	if price == nil {
		t.Fatal("Expected a price, got nil")
	}

	// The closer price is 1700000006000 (1s away vs 2s away)
	if price.Timestamp != 1700000006000 {
		t.Errorf("Expected timestamp 1700000006000 (closest), got %d", price.Timestamp)
	}

	if price.Price != "101.00" {
		t.Errorf("Expected price 101.00, got %s", price.Price)
	}

	if price.Asset != "SOL" {
		t.Errorf("Expected asset SOL, got %s", price.Asset)
	}
}

func TestClient_GetNearestPrice_NoResult(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"price_cache": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	price, err := client.GetNearestPrice(ctx, "SOL", "USDC", 1700000000000, 5000)
	if err != nil {
		t.Fatalf("GetNearestPrice failed: %v", err)
	}

	if price != nil {
		t.Errorf("Expected nil price, got %+v", price)
	}
}

func TestClient_GetNearestPrice_Error(t *testing.T) {
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

	_, err := client.GetNearestPrice(ctx, "SOL", "USDC", 1700000000000, 5000)
	if err == nil {
		t.Fatal("Expected error")
	}
}
