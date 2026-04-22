package drift

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

func TestDriftClient_DiscoverAccounts_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}

		// Should be calling /authority/{wallet}/accounts
		expectedPath := "/authority/ELoqJYcFTc8qjGhJShUupEkRomed8rykb3CUhrzAT4Q1/accounts"
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		response := driftAuthorityAccountsResponse{
			Success: true,
			Accounts: []driftAuthorityAccount{
				{
					AccountID:    "C13FZykQ123abc",
					SubAccountID: 0,
					Name:         "Main Trading",
					Authority:    "ELoqJYcFTc8qjGhJShUupEkRomed8rykb3CUhrzAT4Q1",
				},
				{
					AccountID:    "H7s1cVn4456def",
					SubAccountID: 1,
					Name:         "Bot Account",
					Authority:    "ELoqJYcFTc8qjGhJShUupEkRomed8rykb3CUhrzAT4Q1",
				},
				{
					AccountID:    "BYhwfY1b789ghi",
					SubAccountID: 2,
					Name:         "", // Empty name
					Authority:    "ELoqJYcFTc8qjGhJShUupEkRomed8rykb3CUhrzAT4Q1",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	ctx := context.Background()
	accounts, err := client.DiscoverAccounts(ctx, "ELoqJYcFTc8qjGhJShUupEkRomed8rykb3CUhrzAT4Q1")
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	if len(accounts) != 3 {
		t.Fatalf("Expected 3 accounts, got %d", len(accounts))
	}

	// Verify first account (main)
	if accounts[0].AccountIdentifier != "C13FZykQ123abc" {
		t.Errorf("Expected account identifier 'C13FZykQ123abc', got '%s'", accounts[0].AccountIdentifier)
	}
	if accounts[0].AccountType != "main" {
		t.Errorf("Expected account type 'main', got '%s'", accounts[0].AccountType)
	}
	if accounts[0].Name != "Main Trading" {
		t.Errorf("Expected name 'Main Trading', got '%s'", accounts[0].Name)
	}

	// Verify second account (subaccount)
	if accounts[1].AccountType != "sub_account" {
		t.Errorf("Expected account type 'sub_account', got '%s'", accounts[1].AccountType)
	}
	if accounts[1].Name != "Bot Account" {
		t.Errorf("Expected name 'Bot Account', got '%s'", accounts[1].Name)
	}

	// Verify third account (subaccount with empty name)
	if accounts[2].AccountType != "sub_account" {
		t.Errorf("Expected account type 'sub_account', got '%s'", accounts[2].AccountType)
	}
	if accounts[2].Name != "Subaccount 2" {
		t.Errorf("Expected default name 'Subaccount 2', got '%s'", accounts[2].Name)
	}

	// Verify metadata
	if accounts[0].Metadata["authority"] != "ELoqJYcFTc8qjGhJShUupEkRomed8rykb3CUhrzAT4Q1" {
		t.Errorf("Expected authority in metadata")
	}
	if accounts[0].Metadata["sub_account_id"] != 0 {
		t.Errorf("Expected sub_account_id 0 in metadata")
	}
}

func TestDriftClient_DiscoverAccounts_EmptyUserIdentifier(t *testing.T) {
	client := NewClient()

	ctx := context.Background()
	_, err := client.DiscoverAccounts(ctx, "")
	if err == nil {
		t.Fatal("Expected error for empty user identifier")
	}
}

func TestDriftClient_DiscoverAccounts_ContextCancellation(t *testing.T) {
	client := NewClient()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.DiscoverAccounts(ctx, "ELoqJYcFTc8qjGhJShUupEkRomed8rykb3CUhrzAT4Q1")
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}
}

func TestDriftClient_DiscoverAccounts_NoAccounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := driftAuthorityAccountsResponse{
			Success:  true,
			Accounts: []driftAuthorityAccount{},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	ctx := context.Background()
	accounts, err := client.DiscoverAccounts(ctx, "some-wallet")
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	if len(accounts) != 0 {
		t.Errorf("Expected 0 accounts, got %d", len(accounts))
	}
}

func TestDriftClient_DiscoverAccounts_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	ctx := context.Background()
	_, err := client.DiscoverAccounts(ctx, "some-wallet")
	if err == nil {
		t.Fatal("Expected error for API error")
	}
}

// FetchAccountName on Drift is a stub that always returns "" without making
// any HTTP calls. We verify both behaviors: the empty string and the absence
// of network activity (a bare client with no baseURL would fail any call).
func TestDriftClient_FetchAccountName_StubReturnsEmpty(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	cases := []struct {
		name        string
		accountType string
	}{
		{"main", "main"},
		{"sub_account", "sub_account"},
		{"vault", "vault"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acct := &models.ExchangeAccount{
				ID:                uuid.NewString(),
				AccountIdentifier: "C13FZykQ123abc",
				AccountType:       tc.accountType,
			}
			name, err := client.FetchAccountName(context.Background(), acct)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != "" {
				t.Errorf("name = %q, want empty", name)
			}
		})
	}

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("expected zero HTTP calls for stub, got %d", got)
	}
}
