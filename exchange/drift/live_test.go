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

func TestDriftClient_FetchPositions_Success(t *testing.T) {
	client, server := newTestDriftClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Handle user account request
		if r.URL.Path == "/user/test-account-id" {
			json.NewEncoder(w).Encode(driftUserResponse{
				Positions: []driftUserPosition{
					{
						Symbol:           "SOL-PERP",
						MarketIndex:      0,
						BaseAssetAmount:  "100",
						QuoteEntryAmount: "-15000",
					},
				},
				Balances: []driftUserBalance{
					{Symbol: "USDC", MarketIndex: 0, Balance: "5000"},
				},
			})
			return
		}
		// Handle DLOB price request
		if r.URL.Path == "/l2" {
			json.NewEncoder(w).Encode(dlobL2Response{
				Oracle:    "150000000", // 150 * QuotePrecision
				MarkPrice: "151000000",
			})
			return
		}
		// Handle earn snapshots
		if r.URL.Path == "/authority/wallet123/snapshots/earn" {
			json.NewEncoder(w).Encode(driftEarnResponse{Success: true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	// Override DLOB URL too
	account := &models.ExchangeAccount{
		ID:                "test-id",
		AccountIdentifier: "test-account-id",
	}

	positions, err := client.FetchPositions(context.Background(), account)
	if err != nil {
		t.Fatalf("FetchPositions failed: %v", err)
	}

	if len(positions) != 1 {
		t.Fatalf("Expected 1 position, got %d", len(positions))
	}

	if positions[0].Symbol != "SOL-PERP" {
		t.Errorf("Expected SOL-PERP, got %s", positions[0].Symbol)
	}
	if positions[0].MarketType != "perp" {
		t.Errorf("Expected perp, got %s", positions[0].MarketType)
	}
}

func TestDriftClient_FetchPositions_EmptyIdentifier(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{AccountIdentifier: ""}

	_, err := client.FetchPositions(context.Background(), account)
	if err == nil {
		t.Fatal("Expected error for empty identifier")
	}
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
		// Earn snapshots
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

func TestDriftClient_FetchOpenOrders_Success(t *testing.T) {
	client, server := newTestDriftClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(driftUserResponse{
			Orders: []driftUserOrder{
				{
					Symbol:          "SOL-PERP",
					Direction:       "long",
					Price:           "150.5",
					BaseAssetAmount: "10",
					OrderType:       "limit",
					ReduceOnly:      false,
					Status:          "open",
				},
				{
					Symbol: "BTC-PERP",
					Status: "filled", // Not open, should be filtered
				},
			},
		})
	})
	defer server.Close()

	account := &models.ExchangeAccount{
		ID:                "test-id",
		AccountIdentifier: "test-account",
	}

	orders, err := client.FetchOpenOrders(context.Background(), account)
	if err != nil {
		t.Fatalf("FetchOpenOrders failed: %v", err)
	}

	if len(orders) != 1 {
		t.Fatalf("Expected 1 order (filtered non-open), got %d", len(orders))
	}

	if orders[0].Symbol != "SOL-PERP" {
		t.Errorf("Expected SOL-PERP, got %s", orders[0].Symbol)
	}
}

func TestDriftClient_FetchOpenOrders_EmptyIdentifier(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{AccountIdentifier: ""}

	_, err := client.FetchOpenOrders(context.Background(), account)
	if err == nil {
		t.Fatal("Expected error for empty identifier")
	}
}

func TestDriftClient_FetchAccountValue_NoWallet(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                "test-id",
		AccountIdentifier: "test-account",
	}

	value, err := client.FetchAccountValue(context.Background(), account)
	if err != nil {
		t.Fatalf("FetchAccountValue failed: %v", err)
	}

	if value == nil {
		t.Fatal("Expected non-nil value")
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
