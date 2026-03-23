package drift

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zif-terminal/lib/models"
)

func newTestDriftClient(handler http.HandlerFunc) (*Client, *httptest.Server) {
	server := httptest.NewServer(handler)
	client := NewClient()
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client, server
}

func TestDriftClient_FetchBalances_Success(t *testing.T) {
	client, server := newTestDriftClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/user/test-account" {
			json.NewEncoder(w).Encode(driftUserResponse{
				Balances: []driftUserBalance{
					{Symbol: "USDC", MarketIndex: 0, Balance: "5000.5"},
					{Symbol: "SOL", MarketIndex: 1, Balance: "100"},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(driftEarnResponse{Success: true})
	})
	defer server.Close()

	account := &models.ExchangeAccount{
		ID:                "test-id",
		AccountIdentifier: "test-account",
	}

	balances, err := client.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("FetchBalances failed: %v", err)
	}

	if len(balances) != 2 {
		t.Fatalf("Expected 2 balances, got %d", len(balances))
	}
}

func TestDriftClient_FetchBalances_EmptyIdentifier(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{AccountIdentifier: ""}

	_, err := client.FetchBalances(context.Background(), account)
	if err == nil {
		t.Fatal("Expected error for empty identifier")
	}
}

func TestDriftClient_FetchBalances_LiveEndpoint(t *testing.T) {
	client, server := newTestDriftClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/user/test-account" {
			json.NewEncoder(w).Encode(driftUserResponse{
				Balances: []driftUserBalance{
					{Symbol: "USDC", MarketIndex: 0, Balance: "5000"},
					{Symbol: "SOL", MarketIndex: 1, Balance: "100"},
					{Symbol: "JUP", MarketIndex: 7, Balance: "-213.845"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	account := &models.ExchangeAccount{
		ID:                "test-id",
		AccountIdentifier: "test-account",
	}

	balances, err := client.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("FetchBalances failed: %v", err)
	}

	if len(balances) != 3 {
		t.Fatalf("Expected 3 balances from /user endpoint, got %d", len(balances))
	}

	assetMap := map[string]float64{}
	for _, b := range balances {
		assetMap[b.Asset] = b.Balance
	}
	if assetMap["USDC"] != 5000 {
		t.Errorf("Expected USDC=5000, got %f", assetMap["USDC"])
	}
	if assetMap["SOL"] != 100 {
		t.Errorf("Expected SOL=100, got %f", assetMap["SOL"])
	}
	if assetMap["JUP"] >= 0 {
		t.Errorf("Expected negative JUP balance (borrow), got %f", assetMap["JUP"])
	}
}

func TestDriftClient_FetchBalances_NoMetadata(t *testing.T) {
	client, server := newTestDriftClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/user/test-account" {
			json.NewEncoder(w).Encode(driftUserResponse{
				Balances: []driftUserBalance{
					{Symbol: "USDC", MarketIndex: 0, Balance: "1000"},
					{Symbol: "SOL", MarketIndex: 1, Balance: "50"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	account := &models.ExchangeAccount{
		ID:                "test-id",
		AccountIdentifier: "test-account",
	}

	balances, err := client.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("FetchBalances failed: %v", err)
	}

	if len(balances) != 2 {
		t.Fatalf("Expected 2 balances from fallback, got %d", len(balances))
	}
}

func TestDriftClient_ToFloat(t *testing.T) {
	tests := []struct {
		input interface{}
		want  float64
	}{
		{"150.5", 150.5},
		{float64(100), 100},
		{int(42), 42},
		{int64(99), 99},
		{"invalid", 0},
		{nil, 0},
		{true, 0},
	}

	for _, tt := range tests {
		got := toFloat(tt.input)
		if got != tt.want {
			t.Errorf("toFloat(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDriftClient_GetWalletFromAccount(t *testing.T) {
	client := NewClient()

	// With authority in metadata
	metadata := json.RawMessage(`{"authority":"wallet123"}`)
	account := &models.ExchangeAccount{
		AccountTypeMetadata: metadata,
	}
	wallet := client.getWalletFromAccount(account)
	if wallet != "wallet123" {
		t.Errorf("Expected wallet123, got %s", wallet)
	}

	// With wallet key
	metadata2 := json.RawMessage(`{"wallet":"wallet456"}`)
	account2 := &models.ExchangeAccount{
		AccountTypeMetadata: metadata2,
	}
	wallet2 := client.getWalletFromAccount(account2)
	if wallet2 != "wallet456" {
		t.Errorf("Expected wallet456, got %s", wallet2)
	}

	// No metadata
	account3 := &models.ExchangeAccount{}
	wallet3 := client.getWalletFromAccount(account3)
	if wallet3 != "" {
		t.Errorf("Expected empty, got %s", wallet3)
	}
}
