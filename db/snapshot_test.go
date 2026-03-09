package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
)

func TestClient_GetLatestBalanceSnapshots(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"spot_balance_snapshots": []map[string]interface{}{
					{
						"asset":        "USDC",
						"balance":      "1500.123456",
						"oracle_price": "1.0",
						"usd_value":    "1500.123456",
					},
					{
						"asset":        "SOL",
						"balance":      "25.5",
						"oracle_price": "150.75",
						"usd_value":    "3844.125",
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

	// Check USDC
	if balances[0].Asset != "USDC" {
		t.Errorf("Expected first asset USDC, got %s", balances[0].Asset)
	}
	if balances[0].Balance != 1500.123456 {
		t.Errorf("Expected USDC balance 1500.123456, got %f", balances[0].Balance)
	}
	if balances[0].OraclePrice != 1.0 {
		t.Errorf("Expected USDC oracle price 1.0, got %f", balances[0].OraclePrice)
	}

	// Check SOL
	if balances[1].Asset != "SOL" {
		t.Errorf("Expected second asset SOL, got %s", balances[1].Asset)
	}
	if balances[1].Balance != 25.5 {
		t.Errorf("Expected SOL balance 25.5, got %f", balances[1].Balance)
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

func TestParseFloat64(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"100.5", 100.5},
		{"-50.25", -50.25},
		{"0", 0},
		{"invalid", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseFloat64(tt.input)
			if got != tt.want {
				t.Errorf("parseFloat64(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}
