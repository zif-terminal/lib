package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
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
				Hash: "0x123",
				Time: time.Now().UnixMilli() - 10000, // 10 seconds ago
				Delta: struct {
					Type        string      `json:"type"`
					Coin        string      `json:"coin"`
					USDC        interface{} `json:"usdc"`
					SZI         interface{} `json:"szi"`
					FundingRate interface{} `json:"fundingRate"`
					NSamples    interface{} `json:"nSamples"`
				}{
					Type: "funding",
					Coin: "BTC",
					USDC: "10.5",
				},
			},
			{
				Hash: "0x456",
				Time: time.Now().UnixMilli() - 5000, // 5 seconds ago
				Delta: struct {
					Type        string      `json:"type"`
					Coin        string      `json:"coin"`
					USDC        interface{} `json:"usdc"`
					SZI         interface{} `json:"szi"`
					FundingRate interface{} `json:"fundingRate"`
					NSamples    interface{} `json:"nSamples"`
				}{
					Type: "funding",
					Coin: "ETH",
					USDC: "-5.25",
				},
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
	// PaymentID format: {timestamp_ms}_{coin}
	expectedPaymentID1 := fmt.Sprintf("%d_BTC", payments[0].Timestamp.UnixMilli())
	if payments[0].PaymentID != expectedPaymentID1 {
		t.Errorf("Expected payment ID '%s', got '%s'", expectedPaymentID1, payments[0].PaymentID)
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
	expectedPaymentID2 := fmt.Sprintf("%d_ETH", payments[1].Timestamp.UnixMilli())
	if payments[1].PaymentID != expectedPaymentID2 {
		t.Errorf("Expected payment ID '%s', got '%s'", expectedPaymentID2, payments[1].PaymentID)
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
				Hash: "0xold",
				Time: oldPaymentTime.UnixMilli(),
				Delta: struct {
					Type        string      `json:"type"`
					Coin        string      `json:"coin"`
					USDC        interface{} `json:"usdc"`
					SZI         interface{} `json:"szi"`
					FundingRate interface{} `json:"fundingRate"`
					NSamples    interface{} `json:"nSamples"`
				}{
					Type: "funding",
					Coin: "BTC",
					USDC: "10.0",
				},
			},
			{
				Hash: "0xnew",
				Time: newPaymentTime.UnixMilli(),
				Delta: struct {
					Type        string      `json:"type"`
					Coin        string      `json:"coin"`
					USDC        interface{} `json:"usdc"`
					SZI         interface{} `json:"szi"`
					FundingRate interface{} `json:"fundingRate"`
					NSamples    interface{} `json:"nSamples"`
				}{
					Type: "funding",
					Coin: "ETH",
					USDC: "-5.0",
				},
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

	// PaymentID format: {timestamp_ms}_{coin}
	expectedPaymentID := fmt.Sprintf("%d_ETH", payments[0].Timestamp.UnixMilli())
	if payments[0].PaymentID != expectedPaymentID {
		t.Errorf("Expected payment ID '%s', got '%s'", expectedPaymentID, payments[0].PaymentID)
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

func TestHyperliquidClient_FetchFundingPayments_MissingCoin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []hyperliquidFundingPayment{
			{
				Hash: "0x123", // Hash is no longer required, but we include it for completeness
				Time: time.Now().UnixMilli(),
				Delta: struct {
					Type        string      `json:"type"`
					Coin        string      `json:"coin"`
					USDC        interface{} `json:"usdc"`
					SZI         interface{} `json:"szi"`
					FundingRate interface{} `json:"fundingRate"`
					NSamples    interface{} `json:"nSamples"`
				}{
					Type: "funding",
					Coin: "", // Missing coin - should cause error
					USDC: "10.0",
				},
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
		t.Fatal("Expected error for missing coin (base asset)")
	}
}

func TestHyperliquidClient_FetchFundingPayments_AssetParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []hyperliquidFundingPayment{
			{
				Hash: "0x123",
				Time: time.Now().UnixMilli(),
				Delta: struct {
					Type        string      `json:"type"`
					Coin        string      `json:"coin"`
					USDC        interface{} `json:"usdc"`
					SZI         interface{} `json:"szi"`
					FundingRate interface{} `json:"fundingRate"`
					NSamples    interface{} `json:"nSamples"`
				}{
					Type: "funding",
					Coin: "BTC", // No separator - should default to USDC quote
					USDC: "10.0",
				},
			},
			{
				Hash: "0x456",
				Time: time.Now().UnixMilli(),
				Delta: struct {
					Type        string      `json:"type"`
					Coin        string      `json:"coin"`
					USDC        interface{} `json:"usdc"`
					SZI         interface{} `json:"szi"`
					FundingRate interface{} `json:"fundingRate"`
					NSamples    interface{} `json:"nSamples"`
				}{
					Type: "funding",
					Coin: "ETH-USDT", // With separator
					USDC: "-5.0",
				},
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
