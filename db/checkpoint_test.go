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

func TestClient_SaveCheckpoint(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_processor_checkpoints_one": map[string]interface{}{
					"exchange_account_id": accountID.String(),
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

	err := client.SaveCheckpoint(ctx, &ProcessorCheckpoint{
		ExchangeAccountID:      accountID,
		State:                  models.NewAccountState(),
		SchemaVersion:          7,
		LastTradeTimestamp:      1700000000000,
		LastTransferTimestamp:   1700000001000,
		LastSettlementTimestamp: 1700000002000,
		LastSnapshotTimestamp:   1700000003000,
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}
}

func TestClient_SaveCheckpoint_Error(t *testing.T) {
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

	err := client.SaveCheckpoint(ctx, &ProcessorCheckpoint{
		ExchangeAccountID: accountID,
		State:             models.NewAccountState(),
		SchemaVersion:     7,
	})
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_LoadCheckpoint(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"processor_checkpoints_by_pk": map[string]interface{}{
					"exchange_account_id":       accountID.String(),
					"state":                     `{"assets":{"SOL":{"cumulative_deposits":"100","balance":"legacy-ignored"}},"positions":{},"closed_positions":[],"trading":{"SOL":{"cumulative_funding":"0","cumulative_fee_paid":"0","cumulative_settled_pnl":"0"}}}`,
					"schema_version":            float64(7),
					"last_trade_timestamp":      float64(1700000000000),
					"last_transfer_timestamp":   float64(1700000001000),
					"last_settlement_timestamp": float64(1700000002000),
					"last_snapshot_timestamp":   float64(1700000003000),
					"updated_at":               "2024-01-01T00:00:00Z",
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

	checkpoint, err := client.LoadCheckpoint(ctx, accountID)
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if checkpoint == nil {
		t.Fatal("Expected checkpoint, got nil")
	}

	if checkpoint.SchemaVersion != 7 {
		t.Errorf("Expected schema_version 7, got %d", checkpoint.SchemaVersion)
	}

	if checkpoint.LastTradeTimestamp != 1700000000000 {
		t.Errorf("Expected last_trade_timestamp 1700000000000, got %d", checkpoint.LastTradeTimestamp)
	}

	if checkpoint.State == nil {
		t.Fatal("Expected state to be non-nil")
	}

	if checkpoint.State.Assets["SOL"] == nil {
		t.Error("Expected SOL asset in state")
	} else if checkpoint.State.Assets["SOL"].CumulativeDeposits != "100" {
		// Balance is no longer a stored field — it's computed from spot
		// positions. The legacy "balance" JSON key on the wire is silently
		// dropped on load. CumulativeDeposits acts as the round-trip pin.
		t.Errorf("Expected SOL CumulativeDeposits '100', got %q", checkpoint.State.Assets["SOL"].CumulativeDeposits)
	}
	// Derived balance is zero on an empty Positions map regardless of the
	// legacy "balance" JSON key being present in the persisted state.
	if got := checkpoint.State.Balance("SOL"); got != "0.0" && got != "0" {
		t.Errorf("Expected derived Balance(SOL) = 0 on empty positions, got %q", got)
	}

	if checkpoint.State.Trading["SOL"] == nil {
		t.Error("Expected SOL trading state")
	}
}

func TestClient_LoadCheckpoint_NotFound(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"processor_checkpoints_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	checkpoint, err := client.LoadCheckpoint(ctx, accountID)
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if checkpoint != nil {
		t.Errorf("Expected nil, got %v", checkpoint)
	}
}

func TestProcessorCheckpoint_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name              string
		jsonData          string
		wantSchemaVersion int
		wantTradeTs       int64
	}{
		{
			name:              "float64 values with valid state",
			jsonData:          `{"exchange_account_id":"00000000-0000-0000-0000-000000000001","state":{"assets":{},"positions":{},"closed_positions":[],"trading":{}},"schema_version":7,"last_trade_timestamp":1700000000000,"last_transfer_timestamp":1700000001000,"last_settlement_timestamp":1700000002000,"last_snapshot_timestamp":1700000003000,"updated_at":"2024-01-01"}`,
			wantSchemaVersion: 7,
			wantTradeTs:       1700000000000,
		},
		{
			name:              "string values (Hasura BIGINT format)",
			jsonData:          `{"exchange_account_id":"00000000-0000-0000-0000-000000000001","state":"{\"assets\":{},\"positions\":{},\"closed_positions\":[],\"trading\":{}}","schema_version":"7","last_trade_timestamp":"1700000000000","last_transfer_timestamp":"1700000001000","last_settlement_timestamp":"1700000002000","last_snapshot_timestamp":"1700000003000","updated_at":"2024-01-01"}`,
			wantSchemaVersion: 7,
			wantTradeTs:       1700000000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cp ProcessorCheckpoint
			err := json.Unmarshal([]byte(tt.jsonData), &cp)
			if err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}

			if cp.SchemaVersion != tt.wantSchemaVersion {
				t.Errorf("Expected schema_version %d, got %d", tt.wantSchemaVersion, cp.SchemaVersion)
			}

			if cp.LastTradeTimestamp != tt.wantTradeTs {
				t.Errorf("Expected last_trade_timestamp %d, got %d", tt.wantTradeTs, cp.LastTradeTimestamp)
			}

			if cp.State == nil {
				t.Error("Expected state to be non-nil")
			}
		})
	}
}

func TestClient_DeleteCheckpoint(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_processor_checkpoints_by_pk": map[string]interface{}{
					"exchange_account_id": accountID.String(),
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

	err := client.DeleteCheckpoint(ctx, accountID)
	if err != nil {
		t.Fatalf("DeleteCheckpoint failed: %v", err)
	}
}

func TestClient_DeleteCheckpoint_Error(t *testing.T) {
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

	err := client.DeleteCheckpoint(ctx, uuid.New())
	if err == nil {
		t.Fatal("Expected error")
	}
}
