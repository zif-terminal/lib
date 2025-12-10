package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

func TestHyperliquidClient_Name(t *testing.T) {
	client := NewClient()
	if client.Name() != "hyperliquid" {
		t.Errorf("Expected name 'hyperliquid', got '%s'", client.Name())
	}
}

func TestHyperliquidClient_FetchTrades_Success(t *testing.T) {
	// Mock Hyperliquid API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/info" {
			t.Errorf("Expected path /info, got %s", r.URL.Path)
		}

		// Verify request body
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if reqBody["type"] != "userFills" {
			t.Errorf("Expected type 'userFills', got '%v'", reqBody["type"])
		}

		// Return mock response - API returns direct array, not wrapped
		response := []hyperliquidFill{
			{
				Hash:    "0x123",
				Tid:     111111111111111, // Unique fill ID
				Oid:     123456789,
				Coin:    "BTC-USDC",
				Side:    "B",
				Px:      "50000.5",
				Sz:      "0.1",
				Fee:     "5.0",
				Time:    time.Now().UnixMilli() - 10000, // 10 seconds ago
			},
			{
				Hash:    "0x456",
				Tid:     222222222222222, // Unique fill ID
				Oid:     987654321,
				Coin:    "ETH-USDC",
				Side:    "S",
				Px:      "3000.25",
				Sz:      "1.5",
				Fee:     "4.5",
				Time:    time.Now().UnixMilli() - 5000, // 5 seconds ago
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client with test server URL
	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890123456789012345678901234567890",
	}

	ctx := context.Background()
	trades, err := client.FetchTrades(ctx, account, time.Time{})
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	if len(trades) != 2 {
		t.Fatalf("Expected 2 trades, got %d", len(trades))
	}

	// Verify first trade (should be oldest due to sorting)
	// TradeID should now be the tid (fill ID), not the hash
	if trades[0].TradeID != "111111111111111" {
		t.Errorf("Expected trade ID '111111111111111' (tid), got '%s'", trades[0].TradeID)
	}
	if trades[0].Side != "buy" {
		t.Errorf("Expected side 'buy', got '%s'", trades[0].Side)
	}
	if trades[0].BaseAsset != "BTC" {
		t.Errorf("Expected base asset 'BTC', got '%s'", trades[0].BaseAsset)
	}
	if trades[0].QuoteAsset != "USDC" {
		t.Errorf("Expected quote asset 'USDC', got '%s'", trades[0].QuoteAsset)
	}

	// Verify sorting (oldest first)
	if trades[0].Timestamp.After(trades[1].Timestamp) {
		t.Error("Trades should be sorted oldest first")
	}
}

func TestHyperliquidClient_FetchTrades_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890123456789012345678901234567890",
	}

	ctx := context.Background()
	_, err := client.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected rate limit error")
	}

	if !iface.IsRateLimitError(err) {
		t.Errorf("Expected RateLimitError, got: %v", err)
	}

	rateLimitErr, ok := err.(*iface.RateLimitError)
	if !ok {
		t.Fatalf("Expected *RateLimitError, got %T", err)
	}

	if rateLimitErr.Exchange != "hyperliquid" {
		t.Errorf("Expected exchange 'hyperliquid', got '%s'", rateLimitErr.Exchange)
	}

	if rateLimitErr.RetryAfter != 60*time.Second {
		t.Errorf("Expected retry after 60s, got %v", rateLimitErr.RetryAfter)
	}
}

func TestHyperliquidClient_FetchTrades_ContextCancellation(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]hyperliquidFill{})
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890123456789012345678901234567890",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}
}

func TestHyperliquidClient_FetchTrades_FiltersBySince(t *testing.T) {
	now := time.Now()
	oldTradeTime := now.Add(-20 * time.Second)
	newTradeTime := now.Add(-5 * time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []hyperliquidFill{
			{
				Hash:    "0xold",
				Tid:     333333333333333, // Unique fill ID
				Oid:     111111111,
				Coin:    "BTC-USDC",
				Side:    "B",
				Px:      "50000.0",
				Sz:      "0.1",
				Fee:     "5.0",
				Time:    oldTradeTime.UnixMilli(),
			},
			{
				Hash:    "0xnew",
				Tid:     444444444444444, // Unique fill ID
				Oid:     222222222,
				Coin:    "ETH-USDC",
				Side:    "S",
				Px:      "3000.0",
				Sz:      "1.0",
				Fee:     "4.0",
				Time:    newTradeTime.UnixMilli(),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890123456789012345678901234567890",
	}

	ctx := context.Background()
	// Filter to only get trades after oldTradeTime
	since := oldTradeTime.Add(1 * time.Second)
	trades, err := client.FetchTrades(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	// Should only get the new trade
	if len(trades) != 1 {
		t.Fatalf("Expected 1 trade after filtering, got %d", len(trades))
	}

	// TradeID should now be the tid (fill ID), not the hash
	if trades[0].TradeID != "444444444444444" {
		t.Errorf("Expected trade ID '444444444444444' (tid), got '%s'", trades[0].TradeID)
	}
}

func TestHyperliquidClient_FetchTrades_InvalidAccountID(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                "invalid-uuid",
		AccountIdentifier: "0x1234567890123456789012345678901234567890",
	}

	ctx := context.Background()
	_, err := client.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error for invalid account ID")
	}
}

func TestHyperliquidClient_FetchTrades_EmptyAccountIdentifier(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "",
	}

	ctx := context.Background()
	_, err := client.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error for empty account identifier")
	}
}

// Test contract compliance (basic tests without real API)
func TestHyperliquidClient_Contract(t *testing.T) {
	t.Run("Name", func(t *testing.T) {
		client := NewClient()
		name := client.Name()
		if name == "" {
			t.Error("Name() must return non-empty string")
		}
		if name != "hyperliquid" {
			t.Errorf("Expected name 'hyperliquid', got '%s'", name)
		}
	})
}
