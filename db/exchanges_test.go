package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/machinebox/graphql"
	"github.com/zif-terminal/lib/models"
)

// mockGraphQLClient is a mock implementation of GraphQLClient for testing
type mockGraphQLClient struct {
	runFunc func(ctx context.Context, req *graphql.Request, resp interface{}) error
}

func (m *mockGraphQLClient) Run(ctx context.Context, req *graphql.Request, resp interface{}) error {
	if m.runFunc != nil {
		return m.runFunc(ctx, req, resp)
	}
	return nil
}

func TestClient_GetExchange(t *testing.T) {
	ctx := context.Background()
	expectedExchange := &models.Exchange{
		ID:          "test-id",
		Name:        "hyperliquid",
		DisplayName: "Hyperliquid",
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			// Set mock response
			respData := map[string]interface{}{
				"exchanges_by_pk": expectedExchange,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	exchange, err := client.GetExchange(ctx, "test-id")
	if err != nil {
		t.Fatalf("GetExchange failed: %v", err)
	}

	if exchange.ID != expectedExchange.ID {
		t.Errorf("Expected ID %s, got %s", expectedExchange.ID, exchange.ID)
	}
	if exchange.Name != expectedExchange.Name {
		t.Errorf("Expected Name %s, got %s", expectedExchange.Name, exchange.Name)
	}
	if exchange.DisplayName != expectedExchange.DisplayName {
		t.Errorf("Expected DisplayName %s, got %s", expectedExchange.DisplayName, exchange.DisplayName)
	}
}

func TestClient_GetExchange_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			// Return null response
			respData := map[string]interface{}{
				"exchanges_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	_, err := client.GetExchange(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("Expected error for non-existent exchange")
	}
	if err.Error() != "exchange not found: non-existent-id" {
		t.Errorf("Expected 'exchange not found' error, got: %v", err)
	}
}

func TestClient_ListExchanges(t *testing.T) {
	ctx := context.Background()
	expectedExchanges := []*models.Exchange{
		{ID: "id1", Name: "hyperliquid", DisplayName: "Hyperliquid"},
		{ID: "id2", Name: "lighter", DisplayName: "Lighter"},
		{ID: "id3", Name: "drift", DisplayName: "Drift"},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"exchanges": expectedExchanges,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	exchanges, err := client.ListExchanges(ctx)
	if err != nil {
		t.Fatalf("ListExchanges failed: %v", err)
	}

	if len(exchanges) != len(expectedExchanges) {
		t.Fatalf("Expected %d exchanges, got %d", len(expectedExchanges), len(exchanges))
	}

	for i, exp := range expectedExchanges {
		if exchanges[i].ID != exp.ID {
			t.Errorf("Exchange %d: Expected ID %s, got %s", i, exp.ID, exchanges[i].ID)
		}
		if exchanges[i].Name != exp.Name {
			t.Errorf("Exchange %d: Expected Name %s, got %s", i, exp.Name, exchanges[i].Name)
		}
	}
}

func TestClient_CreateExchange(t *testing.T) {
	ctx := context.Background()
	input := &models.ExchangeInput{
		Name:        "test-exchange",
		DisplayName: "Test Exchange",
	}
	expectedExchange := &models.Exchange{
		ID:          "new-id",
		Name:        input.Name,
		DisplayName: input.DisplayName,
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_exchanges_one": expectedExchange,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	exchange, err := client.CreateExchange(ctx, input)
	if err != nil {
		t.Fatalf("CreateExchange failed: %v", err)
	}

	if exchange.ID != expectedExchange.ID {
		t.Errorf("Expected ID %s, got %s", expectedExchange.ID, exchange.ID)
	}
	if exchange.Name != input.Name {
		t.Errorf("Expected Name %s, got %s", input.Name, exchange.Name)
	}
	if exchange.DisplayName != input.DisplayName {
		t.Errorf("Expected DisplayName %s, got %s", input.DisplayName, exchange.DisplayName)
	}
}

func TestClient_UpdateExchange(t *testing.T) {
	ctx := context.Background()
	id := "test-id"
	input := &models.ExchangeInput{
		Name:        "updated-name",
		DisplayName: "Updated Display Name",
	}
	expectedExchange := &models.Exchange{
		ID:          id,
		Name:        input.Name,
		DisplayName: input.DisplayName,
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_exchanges_by_pk": expectedExchange,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	exchange, err := client.UpdateExchange(ctx, id, input)
	if err != nil {
		t.Fatalf("UpdateExchange failed: %v", err)
	}

	if exchange.ID != id {
		t.Errorf("Expected ID %s, got %s", id, exchange.ID)
	}
	if exchange.Name != input.Name {
		t.Errorf("Expected Name %s, got %s", input.Name, exchange.Name)
	}
	if exchange.DisplayName != input.DisplayName {
		t.Errorf("Expected DisplayName %s, got %s", input.DisplayName, exchange.DisplayName)
	}
}

func TestClient_UpdateExchange_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_exchanges_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	input := &models.ExchangeInput{
		Name:        "test",
		DisplayName: "Test",
	}

	_, err := client.UpdateExchange(ctx, "non-existent-id", input)
	if err == nil {
		t.Fatal("Expected error for non-existent exchange")
	}
	if err.Error() != "exchange not found: non-existent-id" {
		t.Errorf("Expected 'exchange not found' error, got: %v", err)
	}
}
