package db

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/machinebox/graphql"
	"github.com/zif-terminal/lib/models"
)

func TestClient_GetCachedPrice(t *testing.T) {
	ctx := context.Background()
	ts := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"price_cache": []map[string]interface{}{
					{
						"id":           "test-id",
						"asset":        "SOL",
						"denomination": "USDC",
						"timestamp":    ts.Format(time.RFC3339),
						"price":        150.25,
						"source":       "coingecko",
						"created_at":   ts.Format(time.RFC3339),
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

	result, err := client.GetCachedPrice(ctx, "SOL", "USDC", ts, 5*time.Minute)
	if err != nil {
		t.Fatalf("GetCachedPrice failed: %v", err)
	}
	if result == nil {
		t.Fatal("Expected a price cache entry, got nil")
	}
	if result.Asset != "SOL" {
		t.Errorf("Expected asset SOL, got %s", result.Asset)
	}
	if result.Price != 150.25 {
		t.Errorf("Expected price 150.25, got %f", result.Price)
	}
}

func TestClient_GetCachedPrice_NotFound(t *testing.T) {
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

	result, err := client.GetCachedPrice(ctx, "SOL", "USDC", time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("GetCachedPrice failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}
}

func TestClient_GetCachedPrice_Error(t *testing.T) {
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

	_, err := client.GetCachedPrice(ctx, "SOL", "USDC", time.Now(), 5*time.Minute)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_AddPriceCache(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_price_cache_one": map[string]interface{}{
					"id": "new-id",
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

	err := client.AddPriceCache(ctx, &models.PriceCacheInput{
		Asset:        "SOL",
		Denomination: "USDC",
		Timestamp:    time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		Price:        150.25,
		Source:       "coingecko",
	})
	if err != nil {
		t.Fatalf("AddPriceCache failed: %v", err)
	}
}

func TestClient_AddPriceCache_Error(t *testing.T) {
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

	err := client.AddPriceCache(ctx, &models.PriceCacheInput{
		Asset:        "SOL",
		Denomination: "USDC",
		Timestamp:    time.Now(),
		Price:        150.25,
		Source:       "coingecko",
	})
	if err == nil {
		t.Fatal("Expected error")
	}
}
