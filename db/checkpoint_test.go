package db

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
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

	state := json.RawMessage(`{"positions":{},"balances":{}}`)

	err := client.SaveCheckpoint(ctx, accountID, state, 44, 1700000000000, 1700000001000, 1700000002000, 1700000003000)
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

	err := client.SaveCheckpoint(ctx, accountID, json.RawMessage(`{}`), 44, 0, 0, 0, 0)
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
					"exchange_account_id":        accountID.String(),
					"state":                      `{"positions":{}}`,
					"schema_version":             float64(44),
					"last_trade_timestamp":       float64(1700000000000),
					"last_transfer_timestamp":    float64(1700000001000),
					"last_settlement_timestamp":  float64(1700000002000),
					"last_snapshot_timestamp":    float64(1700000003000),
					"updated_at":                "2024-01-01T00:00:00Z",
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

	if checkpoint.SchemaVersion != 44 {
		t.Errorf("Expected schema_version 44, got %d", checkpoint.SchemaVersion)
	}

	if checkpoint.LastTradeTimestamp != 1700000000000 {
		t.Errorf("Expected last_trade_timestamp 1700000000000, got %d", checkpoint.LastTradeTimestamp)
	}

	if checkpoint.LastTransferTimestamp != 1700000001000 {
		t.Errorf("Expected last_transfer_timestamp 1700000001000, got %d", checkpoint.LastTransferTimestamp)
	}

	if checkpoint.LastSettlementTimestamp != 1700000002000 {
		t.Errorf("Expected last_settlement_timestamp 1700000002000, got %d", checkpoint.LastSettlementTimestamp)
	}

	if checkpoint.LastSnapshotTimestamp != 1700000003000 {
		t.Errorf("Expected last_snapshot_timestamp 1700000003000, got %d", checkpoint.LastSnapshotTimestamp)
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
		name               string
		jsonData           string
		wantSchemaVersion  int
		wantTradeTs        int64
		wantTransferTs     int64
		wantSettlementTs   int64
		wantSnapshotTs     int64
	}{
		{
			name:              "float64 values",
			jsonData:          `{"exchange_account_id":"00000000-0000-0000-0000-000000000001","state":{"test":true},"schema_version":44,"last_trade_timestamp":1700000000000,"last_transfer_timestamp":1700000001000,"last_settlement_timestamp":1700000002000,"last_snapshot_timestamp":1700000003000,"updated_at":"2024-01-01"}`,
			wantSchemaVersion: 44,
			wantTradeTs:       1700000000000,
			wantTransferTs:    1700000001000,
			wantSettlementTs:  1700000002000,
			wantSnapshotTs:    1700000003000,
		},
		{
			name:              "string values (Hasura BIGINT format)",
			jsonData:          `{"exchange_account_id":"00000000-0000-0000-0000-000000000001","state":"{\"test\":true}","schema_version":"44","last_trade_timestamp":"1700000000000","last_transfer_timestamp":"1700000001000","last_settlement_timestamp":"1700000002000","last_snapshot_timestamp":"1700000003000","updated_at":"2024-01-01"}`,
			wantSchemaVersion: 44,
			wantTradeTs:       1700000000000,
			wantTransferTs:    1700000001000,
			wantSettlementTs:  1700000002000,
			wantSnapshotTs:    1700000003000,
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

			if cp.LastTransferTimestamp != tt.wantTransferTs {
				t.Errorf("Expected last_transfer_timestamp %d, got %d", tt.wantTransferTs, cp.LastTransferTimestamp)
			}

			if cp.LastSettlementTimestamp != tt.wantSettlementTs {
				t.Errorf("Expected last_settlement_timestamp %d, got %d", tt.wantSettlementTs, cp.LastSettlementTimestamp)
			}

			if cp.LastSnapshotTimestamp != tt.wantSnapshotTs {
				t.Errorf("Expected last_snapshot_timestamp %d, got %d", tt.wantSnapshotTs, cp.LastSnapshotTimestamp)
			}

			if cp.State == nil {
				t.Error("Expected state to be non-nil")
			}
		})
	}
}
