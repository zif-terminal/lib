package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/machinebox/graphql"
	"github.com/zif-terminal/lib/models"
)

func TestClient_GetAccount(t *testing.T) {
	ctx := context.Background()
	expectedAccount := &models.ExchangeAccount{
		ID:                "test-account-id",
		ExchangeID:        "test-exchange-id",
		AccountIdentifier: "0x123",
		AccountType:       "main",
		AccountTypeMetadata: json.RawMessage(`{"key": "value"}`),
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"exchange_accounts_by_pk": expectedAccount,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	account, err := client.GetAccount(ctx, "test-account-id")
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}

	if account.ID != expectedAccount.ID {
		t.Errorf("Expected ID %s, got %s", expectedAccount.ID, account.ID)
	}
	if account.ExchangeID != expectedAccount.ExchangeID {
		t.Errorf("Expected ExchangeID %s, got %s", expectedAccount.ExchangeID, account.ExchangeID)
	}
	if account.AccountIdentifier != expectedAccount.AccountIdentifier {
		t.Errorf("Expected AccountIdentifier %s, got %s", expectedAccount.AccountIdentifier, account.AccountIdentifier)
	}
	if account.AccountType != expectedAccount.AccountType {
		t.Errorf("Expected AccountType %s, got %s", expectedAccount.AccountType, account.AccountType)
	}
}

func TestClient_GetAccount_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"exchange_accounts_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	_, err := client.GetAccount(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("Expected error for non-existent account")
	}
	if err.Error() != "account not found: non-existent-id" {
		t.Errorf("Expected 'account not found' error, got: %v", err)
	}
}

func TestClient_ListAccounts(t *testing.T) {
	ctx := context.Background()
	expectedAccounts := []*models.ExchangeAccount{
		{
			ID:                "id1",
			ExchangeID:        "exchange1",
			AccountIdentifier: "0x111",
			AccountType:       "main",
		},
		{
			ID:                "id2",
			ExchangeID:        "exchange2",
			AccountIdentifier: "0x222",
			AccountType:       "sub_account",
		},
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"exchange_accounts": expectedAccounts,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	accounts, err := client.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}

	if len(accounts) != len(expectedAccounts) {
		t.Fatalf("Expected %d accounts, got %d", len(expectedAccounts), len(accounts))
	}

	for i, exp := range expectedAccounts {
		if accounts[i].ID != exp.ID {
			t.Errorf("Account %d: Expected ID %s, got %s", i, exp.ID, accounts[i].ID)
		}
		if accounts[i].AccountIdentifier != exp.AccountIdentifier {
			t.Errorf("Account %d: Expected AccountIdentifier %s, got %s", i, exp.AccountIdentifier, accounts[i].AccountIdentifier)
		}
	}
}

func TestClient_CreateAccount(t *testing.T) {
	ctx := context.Background()
	input := &models.ExchangeAccountInput{
		ExchangeID:        "test-exchange-id",
		AccountIdentifier: "0x123",
		AccountType:       "main",
		AccountTypeMetadata: json.RawMessage(`{"address": "0x123"}`),
	}
	expectedAccount := &models.ExchangeAccount{
		ID:                "new-account-id",
		ExchangeID:        input.ExchangeID,
		AccountIdentifier: input.AccountIdentifier,
		AccountType:       input.AccountType,
		AccountTypeMetadata: input.AccountTypeMetadata,
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_exchange_accounts_one": expectedAccount,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	account, err := client.CreateAccount(ctx, input)
	if err != nil {
		t.Fatalf("CreateAccount failed: %v", err)
	}

	if account.ID != expectedAccount.ID {
		t.Errorf("Expected ID %s, got %s", expectedAccount.ID, account.ID)
	}
	if account.ExchangeID != input.ExchangeID {
		t.Errorf("Expected ExchangeID %s, got %s", input.ExchangeID, account.ExchangeID)
	}
	if account.AccountIdentifier != input.AccountIdentifier {
		t.Errorf("Expected AccountIdentifier %s, got %s", input.AccountIdentifier, account.AccountIdentifier)
	}
}

func TestClient_CreateAccount_WithoutMetadata(t *testing.T) {
	ctx := context.Background()
	input := &models.ExchangeAccountInput{
		ExchangeID:        "test-exchange-id",
		AccountIdentifier: "0x123",
		AccountType:       "main",
		// No metadata
	}
	expectedAccount := &models.ExchangeAccount{
		ID:                "new-account-id",
		ExchangeID:        input.ExchangeID,
		AccountIdentifier: input.AccountIdentifier,
		AccountType:       input.AccountType,
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"insert_exchange_accounts_one": expectedAccount,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	account, err := client.CreateAccount(ctx, input)
	if err != nil {
		t.Fatalf("CreateAccount failed: %v", err)
	}

	if account.ID != expectedAccount.ID {
		t.Errorf("Expected ID %s, got %s", expectedAccount.ID, account.ID)
	}
}

func TestClient_UpdateAccount(t *testing.T) {
	ctx := context.Background()
	id := "test-account-id"
	input := &models.ExchangeAccountInput{
		ExchangeID:        "updated-exchange-id",
		AccountIdentifier: "0x456",
		AccountType:       "sub_account",
		AccountTypeMetadata: json.RawMessage(`{"index": 1}`),
	}
	expectedAccount := &models.ExchangeAccount{
		ID:                id,
		ExchangeID:        input.ExchangeID,
		AccountIdentifier: input.AccountIdentifier,
		AccountType:       input.AccountType,
		AccountTypeMetadata: input.AccountTypeMetadata,
	}

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_exchange_accounts_by_pk": expectedAccount,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	account, err := client.UpdateAccount(ctx, id, input)
	if err != nil {
		t.Fatalf("UpdateAccount failed: %v", err)
	}

	if account.ID != id {
		t.Errorf("Expected ID %s, got %s", id, account.ID)
	}
	if account.AccountType != input.AccountType {
		t.Errorf("Expected AccountType %s, got %s", input.AccountType, account.AccountType)
	}
}

func TestClient_UpdateAccount_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"update_exchange_accounts_by_pk": nil,
			}
			data, _ := json.Marshal(respData)
			return json.Unmarshal(data, resp)
		},
	}

	client := NewClientWithGraphQL(mockClient, ClientConfig{
		URL:         "http://localhost:8080/v1/graphql",
		AdminSecret: "test-secret",
	})

	input := &models.ExchangeAccountInput{
		ExchangeID:        "test",
		AccountIdentifier: "0x123",
		AccountType:       "main",
	}

	_, err := client.UpdateAccount(ctx, "non-existent-id", input)
	if err == nil {
		t.Fatal("Expected error for non-existent account")
	}
	if err.Error() != "account not found: non-existent-id" {
		t.Errorf("Expected 'account not found' error, got: %v", err)
	}
}

func TestClient_DeleteAccount(t *testing.T) {
	ctx := context.Background()
	id := "test-account-id"

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_exchange_accounts_by_pk": map[string]interface{}{
					"id": id,
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

	err := client.DeleteAccount(ctx, id)
	if err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}
}

func TestClient_DeleteAccount_NotFound(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockGraphQLClient{
		runFunc: func(ctx context.Context, req *graphql.Request, resp interface{}) error {
			respData := map[string]interface{}{
				"delete_exchange_accounts_by_pk": map[string]interface{}{
					"id": "",
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

	err := client.DeleteAccount(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("Expected error for non-existent account")
	}
	if err.Error() != "account not found: non-existent-id" {
		t.Errorf("Expected 'account not found' error, got: %v", err)
	}
}
