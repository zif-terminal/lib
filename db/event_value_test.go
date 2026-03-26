package db

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
)

func TestClient_ListSupportedDenominations(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"supported_denominations": []map[string]interface{}{
					{"asset": "USDC"},
					{"asset": "BTC"},
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

	denoms, err := client.ListSupportedDenominations(ctx)
	if err != nil {
		t.Fatalf("ListSupportedDenominations failed: %v", err)
	}

	if len(denoms) != 2 {
		t.Fatalf("Expected 2 denominations, got %d", len(denoms))
	}

	if denoms[0] != "USDC" {
		t.Errorf("Expected first denomination USDC, got %s", denoms[0])
	}

	if denoms[1] != "BTC" {
		t.Errorf("Expected second denomination BTC, got %s", denoms[1])
	}
}

func TestClient_ListSupportedDenominations_Error(t *testing.T) {
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

	_, err := client.ListSupportedDenominations(ctx)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_ListSupportedDenominations_Empty(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"supported_denominations": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	denoms, err := client.ListSupportedDenominations(ctx)
	if err != nil {
		t.Fatalf("ListSupportedDenominations failed: %v", err)
	}

	if len(denoms) != 0 {
		t.Errorf("Expected 0 denominations, got %d", len(denoms))
	}
}

func TestClient_ListMissingEventValues(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"missing_event_values": []map[string]interface{}{
					{
						"event_id":     eventID.String(),
						"event_type":   "trade",
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

	missing, err := client.ListMissingEventValues(ctx, 100)
	if err != nil {
		t.Fatalf("ListMissingEventValues failed: %v", err)
	}

	if len(missing) != 1 {
		t.Fatalf("Expected 1 missing event value, got %d", len(missing))
	}

	if missing[0].EventID != eventID {
		t.Errorf("Expected event_id %s, got %s", eventID, missing[0].EventID)
	}

	if missing[0].EventType != "trade" {
		t.Errorf("Expected event_type trade, got %s", missing[0].EventType)
	}

	if missing[0].Denomination != "USDC" {
		t.Errorf("Expected denomination USDC, got %s", missing[0].Denomination)
	}
}

func TestClient_ListMissingEventValues_Error(t *testing.T) {
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

	_, err := client.ListMissingEventValues(ctx, 100)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestClient_AddEventValues(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_event_values": map[string]interface{}{
					"affected_rows": float64(3),
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

	inputs := []*EventValueInput{
		{EventID: uuid.New(), EventType: "trade", Denomination: "USDC", Quantity: "100.50"},
		{EventID: uuid.New(), EventType: "transfer", Denomination: "USDC", Quantity: "200.00"},
		{EventID: uuid.New(), EventType: "settlement", Denomination: "USDC", Quantity: "50.25"},
	}

	rows, err := client.AddEventValues(ctx, inputs)
	if err != nil {
		t.Fatalf("AddEventValues failed: %v", err)
	}

	if rows != 3 {
		t.Errorf("Expected 3 affected rows, got %d", rows)
	}
}

func TestClient_AddEventValues_Empty(t *testing.T) {
	ctx := context.Background()

	client := NewClientWithGraphQL(nil, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	rows, err := client.AddEventValues(ctx, []*EventValueInput{})
	if err != nil {
		t.Fatalf("AddEventValues failed: %v", err)
	}

	if rows != 0 {
		t.Errorf("Expected 0 affected rows, got %d", rows)
	}
}

func TestClient_AddEventValues_Error(t *testing.T) {
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

	inputs := []*EventValueInput{
		{EventID: uuid.New(), EventType: "trade", Denomination: "USDC", Quantity: "100.50"},
	}

	_, err := client.AddEventValues(ctx, inputs)
	if err == nil {
		t.Fatal("Expected error")
	}
}
