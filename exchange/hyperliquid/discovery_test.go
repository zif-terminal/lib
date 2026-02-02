package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHyperliquidClient_DiscoverAccounts_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/info" {
			t.Errorf("Expected path /info, got %s", r.URL.Path)
		}

		// Parse request to determine which endpoint is being called
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		reqType := reqBody["type"].(string)

		switch reqType {
		case "subAccounts":
			// Return mock subaccounts
			response := []subAccountResponse{
				{
					SubAccountUser: "0x1111111111111111111111111111111111111111",
					Name:           "Trading Bot",
					Master:         "0xc64cc00b46101bd40aa1c3121195e85c0b0918d8",
				},
				{
					SubAccountUser: "0x2222222222222222222222222222222222222222",
					Name:           "", // Empty name
					Master:         "0xc64cc00b46101bd40aa1c3121195e85c0b0918d8",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case "userVaultEquities":
			// Return mock vaults
			response := []vaultEquityResponse{
				{
					Vault:       "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					VaultName:   "Alpha Vault",
					Equity:      "1000.50",
					VaultEquity: "50000.00",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		default:
			t.Errorf("Unexpected request type: %s", reqType)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	accounts, err := client.DiscoverAccounts(ctx, "0xc64cc00b46101bd40aa1c3121195e85c0b0918d8")
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	// Should have: 1 main + 2 subaccounts + 1 vault = 4 accounts
	if len(accounts) != 4 {
		t.Fatalf("Expected 4 accounts, got %d", len(accounts))
	}

	// Verify main account
	if accounts[0].AccountIdentifier != "0xc64cc00b46101bd40aa1c3121195e85c0b0918d8" {
		t.Errorf("Expected main account identifier, got '%s'", accounts[0].AccountIdentifier)
	}
	if accounts[0].AccountType != "main" {
		t.Errorf("Expected account type 'main', got '%s'", accounts[0].AccountType)
	}
	if accounts[0].Name != "Main Account" {
		t.Errorf("Expected name 'Main Account', got '%s'", accounts[0].Name)
	}

	// Verify first subaccount
	if accounts[1].AccountType != "sub_account" {
		t.Errorf("Expected account type 'sub_account', got '%s'", accounts[1].AccountType)
	}
	if accounts[1].Name != "Trading Bot" {
		t.Errorf("Expected name 'Trading Bot', got '%s'", accounts[1].Name)
	}
	if accounts[1].Metadata["master"] != "0xc64cc00b46101bd40aa1c3121195e85c0b0918d8" {
		t.Errorf("Expected master in metadata")
	}

	// Verify second subaccount (with empty name - should have generated name)
	if accounts[2].AccountType != "sub_account" {
		t.Errorf("Expected account type 'sub_account', got '%s'", accounts[2].AccountType)
	}
	if accounts[2].Name == "" {
		t.Error("Expected generated name for subaccount with empty name")
	}

	// Verify vault
	if accounts[3].AccountType != "vault" {
		t.Errorf("Expected account type 'vault', got '%s'", accounts[3].AccountType)
	}
	if accounts[3].Name != "Alpha Vault" {
		t.Errorf("Expected name 'Alpha Vault', got '%s'", accounts[3].Name)
	}
	if accounts[3].Metadata["equity"] != "1000.50" {
		t.Errorf("Expected equity in metadata")
	}
}

func TestHyperliquidClient_DiscoverAccounts_EmptyUserIdentifier(t *testing.T) {
	client := NewClient()

	ctx := context.Background()
	_, err := client.DiscoverAccounts(ctx, "")
	if err == nil {
		t.Fatal("Expected error for empty user identifier")
	}
}

func TestHyperliquidClient_DiscoverAccounts_ContextCancellation(t *testing.T) {
	client := NewClient()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.DiscoverAccounts(ctx, "0xc64cc00b46101bd40aa1c3121195e85c0b0918d8")
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}
}

func TestHyperliquidClient_DiscoverAccounts_NoSubaccountsOrVaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty arrays for both subaccounts and vaults
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{})
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	accounts, err := client.DiscoverAccounts(ctx, "0xc64cc00b46101bd40aa1c3121195e85c0b0918d8")
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	// Should have just the main account
	if len(accounts) != 1 {
		t.Fatalf("Expected 1 account (main only), got %d", len(accounts))
	}

	if accounts[0].AccountType != "main" {
		t.Errorf("Expected account type 'main', got '%s'", accounts[0].AccountType)
	}
}

func TestHyperliquidClient_DiscoverAccounts_SubaccountAPIError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		reqType := reqBody["type"].(string)

		switch reqType {
		case "subAccounts":
			// Return error for subaccounts
			w.WriteHeader(http.StatusInternalServerError)
		case "userVaultEquities":
			// Return success for vaults
			response := []vaultEquityResponse{
				{
					Vault:     "0xaaaa",
					VaultName: "Test Vault",
					Equity:    "100",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	accounts, err := client.DiscoverAccounts(ctx, "0xc64cc00b46101bd40aa1c3121195e85c0b0918d8")
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	// Should still return main account + vault even if subaccounts fail
	// This tests graceful degradation
	if len(accounts) < 1 {
		t.Fatal("Expected at least main account")
	}

	// Main account should always be present
	if accounts[0].AccountType != "main" {
		t.Error("Expected main account to be first")
	}
}

func TestTruncateAddress(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0x1234567890abcdef1234567890abcdef12345678", "0x1234...5678"},
		{"0x1234", "0x1234"},           // Short address - no truncation
		{"short", "short"},             // Very short - no truncation
		{"", ""},                       // Empty
	}

	for _, tc := range tests {
		result := truncateAddress(tc.input)
		if result != tc.expected {
			t.Errorf("truncateAddress(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}
