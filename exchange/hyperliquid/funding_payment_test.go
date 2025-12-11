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

func TestHyperliquidClient_FetchFundingPayments_Success(t *testing.T) {
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

		if reqBody["type"] != "userFunding" {
			t.Errorf("Expected type 'userFunding', got '%v'", reqBody["type"])
		}

		// Return mock response - API returns direct array, not wrapped
		response := []hyperliquidFundingPayment{
			{
				Hash:    "0x123",
				Coin:    "BTC-USDC",
				Funding: "10.5",
				Time:    time.Now().UnixMilli() - 10000, // 10 seconds ago
			},
			{
				Hash:    "0x456",
				Coin:    "ETH-USDC",
				Funding: "-5.25",
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
	payments, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err != nil {
		t.Fatalf("FetchFundingPayments failed: %v", err)
	}

	if len(payments) != 2 {
		t.Fatalf("Expected 2 payments, got %d", len(payments))
	}

	// Verify first payment (should be oldest due to sorting)
	if payments[0].PaymentID != "0x123" {
		t.Errorf("Expected payment ID '0x123', got '%s'", payments[0].PaymentID)
	}
	if payments[0].Amount != "10.5" {
		t.Errorf("Expected amount '10.5', got '%s'", payments[0].Amount)
	}
	if payments[0].BaseAsset != "BTC" {
		t.Errorf("Expected base asset 'BTC', got '%s'", payments[0].BaseAsset)
	}
	if payments[0].QuoteAsset != "USDC" {
		t.Errorf("Expected quote asset 'USDC', got '%s'", payments[0].QuoteAsset)
	}

	// Verify second payment
	if payments[1].PaymentID != "0x456" {
		t.Errorf("Expected payment ID '0x456', got '%s'", payments[1].PaymentID)
	}
	if payments[1].Amount != "-5.25" {
		t.Errorf("Expected amount '-5.25', got '%s'", payments[1].Amount)
	}

	// Verify sorting (oldest first)
	if payments[0].Timestamp.After(payments[1].Timestamp) {
		t.Error("Payments should be sorted oldest first")
	}
}

func TestHyperliquidClient_FetchFundingPayments_RateLimit(t *testing.T) {
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
	_, err := client.FetchFundingPayments(ctx, account, time.Time{})
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

func TestHyperliquidClient_FetchFundingPayments_ContextCancellation(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]hyperliquidFundingPayment{})
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

	_, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}
}

func TestHyperliquidClient_FetchFundingPayments_FiltersBySince(t *testing.T) {
	now := time.Now()
	oldPaymentTime := now.Add(-20 * time.Second)
	newPaymentTime := now.Add(-5 * time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []hyperliquidFundingPayment{
			{
				Hash:    "0xold",
				Coin:    "BTC-USDC",
				Funding: "10.0",
				Time:    oldPaymentTime.UnixMilli(),
			},
			{
				Hash:    "0xnew",
				Coin:    "ETH-USDC",
				Funding: "-5.0",
				Time:    newPaymentTime.UnixMilli(),
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
	// Filter to only get payments after oldPaymentTime
	since := oldPaymentTime.Add(1 * time.Second)
	payments, err := client.FetchFundingPayments(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchFundingPayments failed: %v", err)
	}

	// Should only get the new payment
	if len(payments) != 1 {
		t.Fatalf("Expected 1 payment after filtering, got %d", len(payments))
	}

	if payments[0].PaymentID != "0xnew" {
		t.Errorf("Expected payment ID '0xnew', got '%s'", payments[0].PaymentID)
	}
}

func TestHyperliquidClient_FetchFundingPayments_InvalidAccountID(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                "invalid-uuid",
		AccountIdentifier: "0x1234567890123456789012345678901234567890",
	}

	ctx := context.Background()
	_, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error for invalid account ID")
	}
}

func TestHyperliquidClient_FetchFundingPayments_EmptyAccountIdentifier(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "",
	}

	ctx := context.Background()
	_, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error for empty account identifier")
	}
}

func TestHyperliquidClient_FetchFundingPayments_MissingHash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []hyperliquidFundingPayment{
			{
				Hash:    "", // Missing hash
				Coin:    "BTC-USDC",
				Funding: "10.0",
				Time:    time.Now().UnixMilli(),
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
	_, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error for missing hash")
	}
}

func TestHyperliquidClient_FetchFundingPayments_AssetParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []hyperliquidFundingPayment{
			{
				Hash:    "0x123",
				Coin:    "BTC", // No separator - should default to USDC quote
				Funding: "10.0",
				Time:    time.Now().UnixMilli(),
			},
			{
				Hash:    "0x456",
				Coin:    "ETH-USDT", // With separator
				Funding: "-5.0",
				Time:    time.Now().UnixMilli(),
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
	payments, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err != nil {
		t.Fatalf("FetchFundingPayments failed: %v", err)
	}

	if len(payments) != 2 {
		t.Fatalf("Expected 2 payments, got %d", len(payments))
	}

	// First payment: BTC with no separator -> should default to USDC
	if payments[0].BaseAsset != "BTC" {
		t.Errorf("Expected base asset 'BTC', got '%s'", payments[0].BaseAsset)
	}
	if payments[0].QuoteAsset != "USDC" {
		t.Errorf("Expected quote asset 'USDC', got '%s'", payments[0].QuoteAsset)
	}

	// Second payment: ETH-USDT -> should parse correctly
	if payments[1].BaseAsset != "ETH" {
		t.Errorf("Expected base asset 'ETH', got '%s'", payments[1].BaseAsset)
	}
	if payments[1].QuoteAsset != "USDT" {
		t.Errorf("Expected quote asset 'USDT', got '%s'", payments[1].QuoteAsset)
	}
}
