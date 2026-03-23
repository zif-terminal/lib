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
	if balances[0].Balance != 25.5 {
		t.Errorf("Expected SOL balance 25.5, got %f", balances[0].Balance)
	}
	// Check BTC
	if balances[1].Asset != "BTC" {
		t.Errorf("Expected second asset BTC, got %s", balances[1].Asset)
	}
	if balances[1].Balance != 0.5 {
		t.Errorf("Expected BTC balance 0.5, got %f", balances[1].Balance)
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
	t.Run("valid values", func(t *testing.T) {
		tests := []struct {
			input string
			want  float64
		}{
			{"100.5", 100.5},
			{"-50.25", -50.25},
			{"0", 0},
			{"", 0},
		}
		for _, tt := range tests {
			got, err := parseFloat64(tt.input)
			if err != nil {
				t.Errorf("parseFloat64(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseFloat64(%q) = %f, want %f", tt.input, got, tt.want)
			}
		}
	})

	t.Run("invalid values return error", func(t *testing.T) {
		invalid := []string{"invalid", "NaN", "Inf", "-Inf"}
		for _, input := range invalid {
			_, err := parseFloat64(input)
			if err == nil {
				t.Errorf("parseFloat64(%q) expected error, got nil", input)
			}
		}
	})
}
