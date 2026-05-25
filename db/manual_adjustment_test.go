package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
)

func TestClient_FetchActiveManualAdjustments(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	adjID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"manual_adjustments": []map[string]interface{}{
					{
						"id":                  adjID.String(),
						"exchange_account_id": accountID.String(),
						"exchange":            "hyperliquid",
						"event_type":          "funding",
						"payload":             json.RawMessage(`{"time":1702598400000,"delta":{"type":"funding"}}`),
						"reason":              "test backfill",
						"created_by":          "tester@example.com",
						"created_at":          time.Now().UTC().Format(time.RFC3339Nano),
						"deactivated_at":      nil,
						"deactivated_reason":  nil,
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

	results, err := client.FetchActiveManualAdjustments(ctx, accountID, "hyperliquid")
	if err != nil {
		t.Fatalf("FetchActiveManualAdjustments failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(results))
	}
	got := results[0]
	if got.ID != adjID {
		t.Errorf("id mismatch: want %s got %s", adjID, got.ID)
	}
	if got.ExchangeAccountID != accountID {
		t.Errorf("account id mismatch: want %s got %s", accountID, got.ExchangeAccountID)
	}
	if got.Exchange != "hyperliquid" {
		t.Errorf("exchange mismatch: got %s", got.Exchange)
	}
	if got.EventType != "funding" {
		t.Errorf("event_type mismatch: got %s", got.EventType)
	}
	if got.CreatedBy != "tester@example.com" {
		t.Errorf("created_by mismatch: got %s", got.CreatedBy)
	}
	if got.DeactivatedAt != nil {
		t.Errorf("expected active adjustment, got deactivated_at=%v", got.DeactivatedAt)
	}
}

func TestClient_FetchActiveManualAdjustments_Empty(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"manual_adjustments": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})

	results, err := client.FetchActiveManualAdjustments(ctx, accountID, "hyperliquid")
	if err != nil {
		t.Fatalf("FetchActiveManualAdjustments failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 adjustments, got %d", len(results))
	}
}

func TestClient_InsertManualAdjustment(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	newID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_manual_adjustments_one": map[string]interface{}{
					"id": newID.String(),
				},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})

	adj := &ManualAdjustment{
		ExchangeAccountID: accountID,
		Exchange:          "hyperliquid",
		EventType:         "funding",
		Payload:           json.RawMessage(`{"time":1702598400000,"hash":"0xabc","delta":{"type":"funding","usdc":"39.42"}}`),
		Reason:            "Dec 2023 funding gap backfill",
		CreatedBy:         "tester@example.com",
	}
	got, err := client.InsertManualAdjustment(ctx, adj)
	if err != nil {
		t.Fatalf("InsertManualAdjustment failed: %v", err)
	}
	if got != newID {
		t.Errorf("returned id mismatch: want %s got %s", newID, got)
	}
}

func TestClient_InsertManualAdjustment_Validation(t *testing.T) {
	ctx := context.Background()
	client := NewClientWithGraphQL(&mockGraphQLClient{}, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})

	cases := []struct {
		name string
		adj  *ManualAdjustment
	}{
		{"nil", nil},
		{"missing account", &ManualAdjustment{Exchange: "hyperliquid", EventType: "funding", Payload: json.RawMessage("{}"), Reason: "x"}},
		{"missing exchange", &ManualAdjustment{ExchangeAccountID: uuid.New(), EventType: "funding", Payload: json.RawMessage("{}"), Reason: "x"}},
		{"missing event_type", &ManualAdjustment{ExchangeAccountID: uuid.New(), Exchange: "hyperliquid", Payload: json.RawMessage("{}"), Reason: "x"}},
		{"missing payload", &ManualAdjustment{ExchangeAccountID: uuid.New(), Exchange: "hyperliquid", EventType: "funding", Reason: "x"}},
		{"missing reason", &ManualAdjustment{ExchangeAccountID: uuid.New(), Exchange: "hyperliquid", EventType: "funding", Payload: json.RawMessage("{}")}},
		{"invalid json", &ManualAdjustment{ExchangeAccountID: uuid.New(), Exchange: "hyperliquid", EventType: "funding", Payload: json.RawMessage("not json"), Reason: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.InsertManualAdjustment(ctx, tc.adj)
			if err == nil {
				t.Errorf("expected validation error for case %s", tc.name)
			}
		})
	}
}

func TestClient_DeactivateManualAdjustment(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_manual_adjustments_by_pk": map[string]interface{}{
					"id": id.String(),
				},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})

	if err := client.DeactivateManualAdjustment(ctx, id, "exchange backfilled the data"); err != nil {
		t.Fatalf("DeactivateManualAdjustment failed: %v", err)
	}
}

func TestClient_DeactivateManualAdjustment_NotFound(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_manual_adjustments_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})

	err := client.DeactivateManualAdjustment(ctx, uuid.New(), "test")
	if err == nil {
		t.Fatal("expected error for missing row")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestClient_DeactivateManualAdjustment_RequiresReason(t *testing.T) {
	ctx := context.Background()
	client := NewClientWithGraphQL(&mockGraphQLClient{}, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})
	if err := client.DeactivateManualAdjustment(ctx, uuid.New(), ""); err == nil {
		t.Error("expected validation error when reason is empty")
	}
}

func TestClient_GetManualAdjustment(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	adjID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"manual_adjustments_by_pk": map[string]interface{}{
					"id":                  adjID.String(),
					"exchange_account_id": accountID.String(),
					"exchange":            "hyperliquid",
					"event_type":          "funding",
					"payload":             json.RawMessage(`{"time":1702598400000}`),
					"reason":              "x",
					"created_by":          nil,
					"created_at":          time.Now().UTC().Format(time.RFC3339Nano),
				},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})

	got, err := client.GetManualAdjustment(ctx, adjID)
	if err != nil {
		t.Fatalf("GetManualAdjustment failed: %v", err)
	}
	if got.ID != adjID {
		t.Errorf("id mismatch: want %s got %s", adjID, got.ID)
	}
	if got.CreatedBy != "" {
		t.Errorf("expected empty CreatedBy when null in response, got %q", got.CreatedBy)
	}
}

func TestClient_GetManualAdjustment_NotFound(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{"manual_adjustments_by_pk": nil}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}
	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})
	_, err := client.GetManualAdjustment(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error for missing row")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestClient_ListManualAdjustments_Filters(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"manual_adjustments": []map[string]interface{}{},
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL: "http://localhost:8080/v1/graphql", AdminSecret: "test-secret",
	})

	// Default filter excludes deactivated
	_, err := client.ListManualAdjustments(ctx, ListAdjustmentsFilter{
		ExchangeAccountID: accountID,
		Exchange:          "hyperliquid",
		EventType:         "funding",
	})
	if err != nil {
		t.Fatalf("ListManualAdjustments failed: %v", err)
	}

	// With IncludeDeactivated, no deactivated_at filter is applied
	_, err = client.ListManualAdjustments(ctx, ListAdjustmentsFilter{
		IncludeDeactivated: true,
	})
	if err != nil {
		t.Fatalf("ListManualAdjustments failed: %v", err)
	}
}
