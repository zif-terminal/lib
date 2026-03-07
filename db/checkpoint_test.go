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
	eventCounts := json.RawMessage(`{"trades":100,"funding":50}`)

	err := client.SaveCheckpoint(ctx, accountID, state, 1700000000000, eventCounts)
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

	err := client.SaveCheckpoint(ctx, accountID, json.RawMessage(`{}`), 0, json.RawMessage(`{}`))
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
					"exchange_account_id":  accountID.String(),
					"state":                `{"positions":{}}`,
					"last_event_timestamp": float64(1700000000000),
					"event_counts":         `{"trades":100}`,
					"updated_at":           "2024-01-01T00:00:00Z",
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

	if checkpoint.LastEventTimestamp != 1700000000000 {
		t.Errorf("Expected timestamp 1700000000000, got %d", checkpoint.LastEventTimestamp)
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
		name          string
		jsonData      string
		wantTimestamp int64
	}{
		{
			name:          "float64 timestamp",
			jsonData:      `{"exchange_account_id":"00000000-0000-0000-0000-000000000001","state":{"test":true},"last_event_timestamp":1700000000000,"event_counts":{"trades":5},"updated_at":"2024-01-01"}`,
			wantTimestamp: 1700000000000,
		},
		{
			name:          "string timestamp",
			jsonData:      `{"exchange_account_id":"00000000-0000-0000-0000-000000000001","state":"{\"test\":true}","last_event_timestamp":"1700000000000","event_counts":"{\"trades\":5}","updated_at":"2024-01-01"}`,
			wantTimestamp: 1700000000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cp ProcessorCheckpoint
			err := json.Unmarshal([]byte(tt.jsonData), &cp)
			if err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}

			if cp.LastEventTimestamp != tt.wantTimestamp {
				t.Errorf("Expected timestamp %d, got %d", tt.wantTimestamp, cp.LastEventTimestamp)
			}

			if cp.State == nil {
				t.Error("Expected state to be non-nil")
			}

			if cp.EventCounts == nil {
				t.Error("Expected event_counts to be non-nil")
			}
		})
	}
}
