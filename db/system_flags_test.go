package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/machinebox/graphql"
)

func TestClient_GetFlag(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"system_flags_by_pk": map[string]interface{}{
					"name":       "processor_force_reset",
					"value":      true,
					"updated_at": time.Now().Format(time.RFC3339Nano),
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

	flag, err := client.GetFlag(ctx, "processor_force_reset")
	if err != nil {
		t.Fatalf("GetFlag failed: %v", err)
	}
	if flag == nil {
		t.Fatal("Expected non-nil flag")
	}
	if flag.Name != "processor_force_reset" {
		t.Errorf("Expected name processor_force_reset, got %s", flag.Name)
	}
}

func TestClient_GetFlag_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"system_flags_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	flag, err := client.GetFlag(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetFlag failed: %v", err)
	}
	if flag != nil {
		t.Error("Expected nil flag for nonexistent key")
	}
}

func TestClient_SetFlag(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_system_flags_one": map[string]interface{}{
					"name": "processor_force_reset",
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

	err := client.SetFlag(ctx, "processor_force_reset", true)
	if err != nil {
		t.Fatalf("SetFlag failed: %v", err)
	}
}

func TestClient_CompareAndSetFlag(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_system_flags": map[string]interface{}{
					"affected_rows": 1,
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

	flag := &SystemFlag{
		Name:      "processor_force_reset",
		UpdatedAt: now,
	}

	ok, err := client.CompareAndSetFlag(ctx, "processor_force_reset", false, flag)
	if err != nil {
		t.Fatalf("CompareAndSetFlag failed: %v", err)
	}
	if !ok {
		t.Error("Expected CAS to succeed")
	}
}

func TestClient_CompareAndSetFlag_Conflict(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_system_flags": map[string]interface{}{
					"affected_rows": 0,
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

	flag := &SystemFlag{
		Name:      "processor_force_reset",
		UpdatedAt: now,
	}

	ok, err := client.CompareAndSetFlag(ctx, "processor_force_reset", false, flag)
	if err != nil {
		t.Fatalf("CompareAndSetFlag failed: %v", err)
	}
	if ok {
		t.Error("Expected CAS to fail (conflict)")
	}
}
