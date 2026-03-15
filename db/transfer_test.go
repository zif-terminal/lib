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

