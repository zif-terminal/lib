package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

func TestDriftClient_Name(t *testing.T) {
	client := NewClient()
	if client.Name() != "drift" {
		t.Errorf("Expected name 'drift', got '%s'", client.Name())
	}
}

func TestDriftClient_FetchTrades_Success(t *testing.T) {
	// Mock Drift API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")

		// Handle swaps endpoint - return empty response
		if strings.Contains(r.URL.Path, "/swaps") {
			json.NewEncoder(w).Encode(driftSwapsResponse{
				Success: true,
				Records: []driftSwapRecord{},
				Meta:    driftMeta{NextPage: nil},
			})
			return
		}

		// Return mock response - API returns already-formatted decimal strings
		response := driftTradesResponse{
			Success: true,
			Records: []driftTradeRecord{
				{
					Ts:                     time.Now().Unix() - 10,
					TxSig:                  "tx123",
					FillRecordID:           "fill-001",
					BaseAssetAmountFilled:  "1.000000000",  // Already decimal
					QuoteAssetAmountFilled: "50.000000",    // Already decimal
					TakerFee:               "0.050000",     // Already decimal
					MakerFee:               "0",
					TakerOrderDirection:    "long",
					MakerOrderDirection:    "short",
					TakerOrderID:           "order-001",
					MakerOrderID:           "order-002",
					Taker:                  "test-account",
					Maker:                  "maker-account",
					User:                   "test-account",
					Symbol:                 "SOL-PERP",
					MarketIndex:            0,
					MarketType:             "perp",
					OraclePrice:            "50.123456",
				},
				{
					Ts:                     time.Now().Unix() - 5,
					TxSig:                  "tx456",
					FillRecordID:           "fill-002",
					BaseAssetAmountFilled:  "2.000000000",  // Already decimal
					QuoteAssetAmountFilled: "6.000000",     // Already decimal
					TakerFee:               "0",
					MakerFee:               "0.030000",     // Already decimal
					TakerOrderDirection:    "short",
					MakerOrderDirection:    "long",
					TakerOrderID:           "order-003",
					MakerOrderID:           "order-004",
					Taker:                  "taker-account",
					Maker:                  "test-account",
					User:                   "test-account",
					Symbol:                 "BTC-PERP",
					MarketIndex:            1,
					MarketType:             "perp",
					OraclePrice:            "68131.590312",
				},
			},
			Meta: driftMeta{NextPage: nil},
		}

		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client with test server URL
	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx := context.Background()
	// Use a recent since time (within 31 days) to avoid historical month fetching
	// which would hit the mock server multiple times
	since := time.Now().AddDate(0, 0, -30)
	trades, _, err := client.FetchTrades(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	if len(trades) != 2 {
		t.Fatalf("Expected 2 trades, got %d", len(trades))
	}

	// Verify first trade (should be oldest due to sorting)
	if trades[0].TradeID != "fill-001" {
		t.Errorf("Expected trade ID 'fill-001', got '%s'", trades[0].TradeID)
	}
	if trades[0].Side != "buy" { // long -> buy
		t.Errorf("Expected side 'buy', got '%s'", trades[0].Side)
	}
	if trades[0].BaseAsset != "SOL" {
		t.Errorf("Expected base asset 'SOL', got '%s'", trades[0].BaseAsset)
	}
	if trades[0].QuoteAsset != "USDC" {
		t.Errorf("Expected quote asset 'USDC', got '%s'", trades[0].QuoteAsset)
	}
	if trades[0].Quantity != "1" {
		t.Errorf("Expected quantity '1', got '%s'", trades[0].Quantity)
	}

	// Verify second trade (maker)
	if trades[1].TradeID != "fill-002" {
		t.Errorf("Expected trade ID 'fill-002', got '%s'", trades[1].TradeID)
	}
	if trades[1].Side != "buy" { // maker is long
		t.Errorf("Expected side 'buy', got '%s'", trades[1].Side)
	}

	// Verify sorting (oldest first)
	if trades[0].Timestamp.After(trades[1].Timestamp) {
		t.Error("Trades should be sorted oldest first")
	}
}

func TestDriftClient_FetchTrades_Pagination(t *testing.T) {
	tradeCallCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Handle swaps endpoint - return empty response
		if strings.Contains(r.URL.Path, "/swaps") {
			json.NewEncoder(w).Encode(driftSwapsResponse{
				Success: true,
				Records: []driftSwapRecord{},
				Meta:    driftMeta{NextPage: nil},
			})
			return
		}

		tradeCallCount++

		var response driftTradesResponse
		if tradeCallCount == 1 {
			// First page
			response = driftTradesResponse{
				Success: true,
				Records: []driftTradeRecord{
					{
						Ts:                     time.Now().Unix() - 20,
						FillRecordID:           "fill-page1",
						BaseAssetAmountFilled:  "1.000000000",
						QuoteAssetAmountFilled: "50.000000",
						TakerFee:               "0.050000",
						TakerOrderDirection:    "long",
						Taker:                  "test-account",
						User:                   "test-account",
						Symbol:                 "SOL-PERP",
						MarketType:             "perp",
					},
				},
				Meta: driftMeta{NextPage: "2"},
			}
		} else {
			// Second page (last)
			response = driftTradesResponse{
				Success: true,
				Records: []driftTradeRecord{
					{
						Ts:                     time.Now().Unix() - 10,
						FillRecordID:           "fill-page2",
						BaseAssetAmountFilled:  "2.000000000",
						QuoteAssetAmountFilled: "100.000000",
						TakerFee:               "0.100000",
						TakerOrderDirection:    "short",
						Taker:                  "test-account",
						User:                   "test-account",
						Symbol:                 "BTC-PERP",
						MarketType:             "perp",
					},
				},
				Meta: driftMeta{NextPage: nil},
			}
		}

		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx := context.Background()
	// Use a recent since time (within last 31 days) to avoid historical backfilling
	recentSince := time.Now().Add(-7 * 24 * time.Hour)
	trades, _, err := client.FetchTrades(ctx, account, recentSince)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	if tradeCallCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", tradeCallCount)
	}

	if len(trades) != 2 {
		t.Fatalf("Expected 2 trades from pagination, got %d", len(trades))
	}
}

func TestDriftClient_FetchTrades_RateLimit(t *testing.T) {
	retryCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	// Use a short timeout to limit retry wait times in tests
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, err := client.FetchTrades(ctx, account, time.Now().Add(-24*time.Hour)) // Recent since to avoid historical fetch
	if err == nil {
		t.Fatal("Expected error")
	}

	// Should have retried at least once before giving up or timing out
	if retryCount < 2 {
		t.Errorf("Expected at least 2 retry attempts, got %d", retryCount)
	}
}

func TestDriftClient_FetchTrades_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(driftTradesResponse{Success: true})
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, err := client.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}
}

func TestDriftClient_FetchTrades_FiltersBySince(t *testing.T) {
	now := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Handle swaps endpoint - return empty response
		if strings.Contains(r.URL.Path, "/swaps") {
			json.NewEncoder(w).Encode(driftSwapsResponse{
				Success: true,
				Records: []driftSwapRecord{},
				Meta:    driftMeta{NextPage: nil},
			})
			return
		}

		response := driftTradesResponse{
			Success: true,
			Records: []driftTradeRecord{
				{
					Ts:                     now.Add(-20 * time.Second).Unix(),
					FillRecordID:           "old-trade",
					BaseAssetAmountFilled:  "1.000000000",
					QuoteAssetAmountFilled: "50.000000",
					TakerFee:               "0.050000",
					TakerOrderDirection:    "long",
					Taker:                  "test-account",
					User:                   "test-account",
					Symbol:                 "SOL-PERP",
					MarketType:             "perp",
				},
				{
					Ts:                     now.Add(-5 * time.Second).Unix(),
					FillRecordID:           "new-trade",
					BaseAssetAmountFilled:  "2.000000000",
					QuoteAssetAmountFilled: "100.000000",
					TakerFee:               "0.100000",
					TakerOrderDirection:    "short",
					Taker:                  "test-account",
					User:                   "test-account",
					Symbol:                 "BTC-PERP",
					MarketType:             "perp",
				},
			},
			Meta: driftMeta{NextPage: nil},
		}

		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx := context.Background()
	since := now.Add(-10 * time.Second)
	trades, _, err := client.FetchTrades(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	// Should only get the new trade
	if len(trades) != 1 {
		t.Fatalf("Expected 1 trade after filtering, got %d", len(trades))
	}

	if trades[0].TradeID != "new-trade" {
		t.Errorf("Expected trade ID 'new-trade', got '%s'", trades[0].TradeID)
	}
}

func TestDriftClient_FetchTrades_InvalidAccountID(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                "invalid-uuid",
		AccountIdentifier: "test-account",
	}

	ctx := context.Background()
	_, _, err := client.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error for invalid account ID")
	}
}

func TestDriftClient_FetchTrades_EmptyAccountIdentifier(t *testing.T) {
	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "",
	}

	ctx := context.Background()
	_, _, err := client.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error for empty account identifier")
	}
}

func TestDriftClient_FetchFundingPayments_Success(t *testing.T) {
	// First call will be for markets, second for funding
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		if r.URL.Path == "/stats/markets" {
			// Return mock markets
			response := driftMarketsResponse{
				Success: true,
				Markets: []driftMarket{
					{MarketIndex: 0, Symbol: "SOL-PERP", BaseAsset: "SOL", MarketType: "perp"},
					{MarketIndex: 1, Symbol: "BTC-PERP", BaseAsset: "BTC", MarketType: "perp"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		// Return mock funding payments (API returns decimal strings)
		response := driftFundingResponse{
			Success: true,
			Records: []driftFundingPayment{
				{
					Ts:              time.Now().Unix() - 10,
					TxSig:           "tx123",
					MarketIndex:     0, // SOL-PERP
					FundingPayment:  "0.1",
					User:            "test-account",
					BaseAssetAmount: "1.000000000",
				},
				{
					Ts:              time.Now().Unix() - 5,
					TxSig:           "tx456",
					MarketIndex:     1, // BTC-PERP
					FundingPayment:  "-0.05",
					User:            "test-account",
					BaseAssetAmount: "0.500000000",
				},
			},
			Meta: driftMeta{NextPage: nil},
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

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx := context.Background()
	// Use a recent since time (within 31 days) to avoid historical month fetching
	since := time.Now().AddDate(0, 0, -30)
	payments, err := client.FetchFundingPayments(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchFundingPayments failed: %v", err)
	}

	if len(payments) != 2 {
		t.Fatalf("Expected 2 payments, got %d", len(payments))
	}

	// Verify all payments have type="funding"
	for _, p := range payments {
		if p.Type != models.TypeFunding {
			t.Errorf("Expected type 'funding', got '%s'", p.Type)
		}
	}

	// Verify first payment (oldest) - market stored in metadata
	if payments[0].Metadata["market"] != "SOL" {
		t.Errorf("Expected market 'SOL', got '%s'", payments[0].Metadata["market"])
	}
	if payments[0].Amount != "0.1" {
		t.Errorf("Expected amount '0.1', got '%s'", payments[0].Amount)
	}

	// Verify second payment (negative)
	if payments[1].Metadata["market"] != "BTC" {
		t.Errorf("Expected market 'BTC', got '%s'", payments[1].Metadata["market"])
	}
	if payments[1].Amount != "-0.05" {
		t.Errorf("Expected amount '-0.05', got '%s'", payments[1].Amount)
	}

	// Verify sorting
	if payments[0].Timestamp.After(payments[1].Timestamp) {
		t.Error("Payments should be sorted oldest first")
	}
}

func TestDriftClient_FetchFundingPayments_ContextCancellation(t *testing.T) {
	client := NewClient()

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}
}

// Test helper functions

func TestNormalizeSide(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"long", "buy"},
		{"LONG", "buy"},
		{"Long", "buy"},
		{"short", "sell"},
		{"SHORT", "sell"},
		{"Short", "sell"},
		{"unknown", "buy"}, // Default
		{"", "buy"},        // Default
	}

	for _, tc := range tests {
		result := normalizeSide(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeSide(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestParseSymbol(t *testing.T) {
	tests := []struct {
		symbol     string
		baseAsset  string
		quoteAsset string
	}{
		{"SOL-PERP", "SOL", "USDC"},
		{"BTC-PERP", "BTC", "USDC"},
		{"ETH-PERP", "ETH", "USDC"},
		{"SOL", "SOL", "USDC"}, // No suffix
	}

	for _, tc := range tests {
		base, quote := parseSymbol(tc.symbol)
		if base != tc.baseAsset {
			t.Errorf("parseSymbol(%q) base = %q, expected %q", tc.symbol, base, tc.baseAsset)
		}
		if quote != tc.quoteAsset {
			t.Errorf("parseSymbol(%q) quote = %q, expected %q", tc.symbol, quote, tc.quoteAsset)
		}
	}
}

func TestCleanDecimalString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1.000000000", "1"},
		{"50.000000", "50"},
		{"0.050000", "0.05"},
		{"1.500000000", "1.5"},
		{"0.123456789", "0.123456789"},
		{"-0.05", "-0.05"},
		{"-1.000000", "-1"},
		{"0", "0"},
		{"0.0", "0"},
		{"", "0"},
		{"100", "100"},
	}

	for _, tc := range tests {
		result := cleanDecimalString(tc.input)
		if result != tc.expected {
			t.Errorf("cleanDecimalString(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestConvertFromBasePrecision(t *testing.T) {
	tests := []struct {
		raw      string
		expected string
	}{
		{"1000000000", "1"},           // 1.0
		{"1500000000", "1.5"},         // 1.5
		{"500000000", "0.5"},          // 0.5
		{"123456789", "0.123456789"},  // Fractional
		{"0", "0"},
		{"", "0"},
	}

	for _, tc := range tests {
		result := convertFromBasePrecision(tc.raw)
		if result != tc.expected {
			t.Errorf("convertFromBasePrecision(%q) = %q, expected %q", tc.raw, result, tc.expected)
		}
	}
}

func TestConvertFromQuotePrecision(t *testing.T) {
	tests := []struct {
		raw      string
		expected string
	}{
		{"1000000", "1"},       // 1.0
		{"1500000", "1.5"},     // 1.5
		{"500000", "0.5"},      // 0.5
		{"100000", "0.1"},      // 0.1
		{"50000", "0.05"},      // 0.05
		{"-100000", "-0.1"},    // Negative
		{"0", "0"},
		{"", "0"},
	}

	for _, tc := range tests {
		result := convertFromQuotePrecision(tc.raw)
		if result != tc.expected {
			t.Errorf("convertFromQuotePrecision(%q) = %q, expected %q", tc.raw, result, tc.expected)
		}
	}
}

func TestCalculatePrice(t *testing.T) {
	tests := []struct {
		quote    string
		base     string
		expected string
	}{
		{"50", "1", "50"},
		{"100", "2", "50"},
		{"33", "3", "11"},
		{"0", "1", "0"},
		{"50", "0", "0"}, // Division by zero
		{"50", "", "0"},  // Empty base
	}

	for _, tc := range tests {
		result := calculatePrice(tc.quote, tc.base)
		if result != tc.expected {
			t.Errorf("calculatePrice(%q, %q) = %q, expected %q", tc.quote, tc.base, result, tc.expected)
		}
	}
}

// Test contract compliance
func TestDriftClient_Contract(t *testing.T) {
	t.Run("Name", func(t *testing.T) {
		client := NewClient()
		name := client.Name()
		if name == "" {
			t.Error("Name() must return non-empty string")
		}
		if name != "drift" {
			t.Errorf("Expected name 'drift', got '%s'", name)
		}
	})
}

func TestDriftClient_FetchTrades_WithSwaps(t *testing.T) {
	now := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Handle swaps endpoint
		if strings.Contains(r.URL.Path, "/swaps") {
			response := driftSwapsResponse{
				Success: true,
				Records: []driftSwapRecord{
					{
						Ts:             now.Add(-15 * time.Second).Unix(),
						TxSig:          "swap-tx-123",
						TxSigIndex:     0,
						Slot:           12345,
						User:           "test-account",
						OutMarketIndex: 1,
						InMarketIndex:  0,
						AmountOut:      "1.000000",   // User receives 1 SOL
						AmountIn:       "100.000000", // User spends 100 USDC
						OutOraclePrice: "100.000000",
						InOraclePrice:  "1.000000",
						Fee:            "0.100000",
						OutSymbol:      "SOL",  // User receives SOL
						InSymbol:       "USDC", // User spends USDC
					},
				},
				Meta: driftMeta{NextPage: nil},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		// Handle trades endpoint
		response := driftTradesResponse{
			Success: true,
			Records: []driftTradeRecord{
				{
					Ts:                     now.Add(-10 * time.Second).Unix(),
					TxSig:                  "tx123",
					FillRecordID:           "fill-001",
					BaseAssetAmountFilled:  "1.000000000",
					QuoteAssetAmountFilled: "50.000000",
					TakerFee:               "0.050000",
					MakerFee:               "0",
					TakerOrderDirection:    "long",
					MakerOrderDirection:    "short",
					TakerOrderID:           "order-001",
					MakerOrderID:           "order-002",
					Taker:                  "test-account",
					Maker:                  "maker-account",
					User:                   "test-account",
					Symbol:                 "SOL-PERP",
					MarketIndex:            0,
					MarketType:             "perp",
				},
			},
			Meta: driftMeta{NextPage: nil},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx := context.Background()
	// Use a recent since time (within 31 days) to avoid historical month fetching
	since := time.Now().AddDate(0, 0, -30)
	trades, prices, err := client.FetchTrades(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	// Should have 2 trades: 1 perp trade + 1 swap
	if len(trades) != 2 {
		t.Fatalf("Expected 2 trades, got %d", len(trades))
	}

	// Find swap trade
	var swapTrade *models.TradeInput
	var perpTrade *models.TradeInput
	for _, trade := range trades {
		if trade.MarketType == "swap" {
			swapTrade = trade
		} else if trade.MarketType == "perp" {
			perpTrade = trade
		}
	}

	if swapTrade == nil {
		t.Fatal("Expected to find swap trade")
	}
	if perpTrade == nil {
		t.Fatal("Expected to find perp trade")
	}

	// Verify swap trade fields
	if swapTrade.BaseAsset != "SOL" {
		t.Errorf("Expected swap base asset 'SOL', got '%s'", swapTrade.BaseAsset)
	}
	if swapTrade.QuoteAsset != "USDC" {
		t.Errorf("Expected swap quote asset 'USDC', got '%s'", swapTrade.QuoteAsset)
	}
	if swapTrade.Side != "buy" {
		t.Errorf("Expected swap side 'buy', got '%s'", swapTrade.Side)
	}
	if swapTrade.Quantity != "1" {
		t.Errorf("Expected swap quantity '1', got '%s'", swapTrade.Quantity)
	}
	if swapTrade.Price != "100" {
		t.Errorf("Expected swap price '100', got '%s'", swapTrade.Price)
	}
	if swapTrade.Fee != "0.1" {
		t.Errorf("Expected swap fee '0.1', got '%s'", swapTrade.Fee)
	}
	if swapTrade.TradeID != "swap_swap-tx-123_0" {
		t.Errorf("Expected swap trade ID 'swap_swap-tx-123_0', got '%s'", swapTrade.TradeID)
	}

	// Verify trades are sorted (oldest first - swap is older)
	if trades[0].MarketType != "swap" {
		t.Error("Expected swap trade to be first (oldest)")
	}

	// Verify oracle prices from swap
	// Swap has OutOraclePrice="100.000000" (SOL) and InOraclePrice="1.000000" (USDC)
	if len(prices) != 2 {
		t.Fatalf("Expected 2 oracle prices from swap, got %d", len(prices))
	}
	// Find SOL price
	var solPrice, usdcPrice *models.PriceRecord
	for _, p := range prices {
		if p.Asset == "SOL" {
			solPrice = p
		}
		if p.Asset == "USDC" {
			usdcPrice = p
		}
	}
	if solPrice == nil {
		t.Fatal("Expected SOL oracle price")
	}
	if solPrice.Price != "100.000000" {
		t.Errorf("Expected SOL oracle price '100.000000', got '%s'", solPrice.Price)
	}
	if solPrice.Denomination != "USDC" {
		t.Errorf("Expected denomination 'USDC', got '%s'", solPrice.Denomination)
	}
	if solPrice.Source != "oracle" {
		t.Errorf("Expected source 'oracle', got '%s'", solPrice.Source)
	}
	if usdcPrice == nil {
		t.Fatal("Expected USDC oracle price")
	}
	if usdcPrice.Price != "1.000000" {
		t.Errorf("Expected USDC oracle price '1.000000', got '%s'", usdcPrice.Price)
	}
}

func TestTransformSwap(t *testing.T) {
	client := NewClient()
	accountUUID := uuid.New()

	t.Run("buy swap (spent USDC, received SOL)", func(t *testing.T) {
		// Drift API: OutSymbol=what user RECEIVES, InSymbol=what user SPENDS
		record := driftSwapRecord{
			Ts:             1700000000,
			TxSig:          "test-tx-sig",
			TxSigIndex:     0,
			Slot:           12345,
			User:           "test-user",
			OutMarketIndex: 1,
			InMarketIndex:  0,
			AmountOut:      "2.500000",   // User receives 2.5 SOL
			AmountIn:       "200.000000", // User spends 200 USDC
			OutOraclePrice: "80.000000",
			InOraclePrice:  "1.000000",
			Fee:            "0.200000",
			OutSymbol:      "SOL",  // User receives SOL
			InSymbol:       "USDC", // User spends USDC
		}

		trade, _ := client.transformSwap(record, accountUUID)

		if trade.BaseAsset != "SOL" {
			t.Errorf("Expected base asset 'SOL', got '%s'", trade.BaseAsset)
		}
		if trade.QuoteAsset != "USDC" {
			t.Errorf("Expected quote asset 'USDC', got '%s'", trade.QuoteAsset)
		}
		if trade.Side != "buy" {
			t.Errorf("Expected side 'buy', got '%s'", trade.Side)
		}
		if trade.Quantity != "2.5" {
			t.Errorf("Expected quantity '2.5', got '%s'", trade.Quantity)
		}
		if trade.Price != "80" {
			t.Errorf("Expected price '80', got '%s'", trade.Price)
		}
		if trade.MarketType != "swap" {
			t.Errorf("Expected market type 'swap', got '%s'", trade.MarketType)
		}
	})

	t.Run("sell swap (spent SOL, received USDC)", func(t *testing.T) {
		// Drift API: OutSymbol=what user RECEIVES, InSymbol=what user SPENDS
		record := driftSwapRecord{
			Ts:             1700000000,
			TxSig:          "test-tx-sig-2",
			TxSigIndex:     0,
			Slot:           12345,
			User:           "test-user",
			OutMarketIndex: 0,
			InMarketIndex:  1,
			AmountOut:      "200.000000", // User receives 200 USDC
			AmountIn:       "2.500000",   // User spends 2.5 SOL
			OutOraclePrice: "1.000000",
			InOraclePrice:  "80.000000",
			Fee:            "0.200000",
			OutSymbol:      "USDC", // User receives USDC
			InSymbol:       "SOL",  // User spends SOL
		}

		trade, _ := client.transformSwap(record, accountUUID)

		if trade.BaseAsset != "SOL" {
			t.Errorf("Expected base asset 'SOL', got '%s'", trade.BaseAsset)
		}
		if trade.QuoteAsset != "USDC" {
			t.Errorf("Expected quote asset 'USDC', got '%s'", trade.QuoteAsset)
		}
		if trade.Side != "sell" {
			t.Errorf("Expected side 'sell', got '%s'", trade.Side)
		}
		if trade.Quantity != "2.5" {
			t.Errorf("Expected quantity '2.5', got '%s'", trade.Quantity)
		}
		if trade.Price != "80" {
			t.Errorf("Expected price '80', got '%s'", trade.Price)
		}
		if trade.MarketType != "swap" {
			t.Errorf("Expected market type 'swap', got '%s'", trade.MarketType)
		}
	})

	t.Run("non-USDC swap (SOL to mSOL)", func(t *testing.T) {
		// Drift API: OutSymbol=what user RECEIVES, InSymbol=what user SPENDS
		record := driftSwapRecord{
			Ts:             1700000000,
			TxSig:          "test-tx-sig-3",
			TxSigIndex:     0,
			Slot:           12345,
			User:           "test-user",
			OutMarketIndex: 2,
			InMarketIndex:  1,
			AmountOut:      "9.500000",  // User receives 9.5 mSOL
			AmountIn:       "10.000000", // User spends 10 SOL
			OutOraclePrice: "84.000000",
			InOraclePrice:  "80.000000",
			Fee:            "0.010000",
			OutSymbol:      "mSOL", // User receives mSOL
			InSymbol:       "SOL",  // User spends SOL
		}

		trade, _ := client.transformSwap(record, accountUUID)

		// For non-USDC swaps, outSymbol (received) is base, inSymbol (spent) is quote
		if trade.BaseAsset != "mSOL" {
			t.Errorf("Expected base asset 'mSOL', got '%s'", trade.BaseAsset)
		}
		if trade.QuoteAsset != "SOL" {
			t.Errorf("Expected quote asset 'SOL', got '%s'", trade.QuoteAsset)
		}
		if trade.Side != "buy" {
			t.Errorf("Expected side 'buy', got '%s'", trade.Side)
		}
		if trade.MarketType != "swap" {
			t.Errorf("Expected market type 'swap', got '%s'", trade.MarketType)
		}
	})
}

func TestDriftClient_FetchDeposits_Success(t *testing.T) {
	// Use recent timestamps so they pass the since filter
	now := time.Now()
	depositTs := now.Add(-10 * 24 * time.Hour).Unix() // 10 days ago
	withdrawTs := now.Add(-5 * 24 * time.Hour).Unix() // 5 days ago

	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle spot markets request for market info lookup
		if strings.Contains(r.URL.Path, "/spotMarkets") {
			response := `{
				"success": true,
				"markets": [
					{"marketIndex": 0, "symbol": "USDC", "baseAsset": "USDC", "quoteAsset": "USD"},
					{"marketIndex": 1, "symbol": "SOL", "baseAsset": "SOL", "quoteAsset": "USDC"}
				]
			}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
			return
		}

		// Handle deposits request
		if strings.Contains(r.URL.Path, "/deposits") {
			response := fmt.Sprintf(`{
				"success": true,
				"records": [
					{
						"ts": %d,
						"txSig": "test-tx-1",
						"slot": 12345,
						"amount": "100.000000",
						"marketIndex": 0,
						"depositRecordId": "deposit-1",
						"direction": "deposit",
						"oraclePrice": "1.000000",
						"user": "test-user"
					},
					{
						"ts": %d,
						"txSig": "test-tx-2",
						"slot": 12346,
						"amount": "50.000000",
						"marketIndex": 0,
						"depositRecordId": "withdraw-1",
						"direction": "withdraw",
						"oraclePrice": "1.000000",
						"user": "test-user"
					}
				],
				"meta": {
					"nextPage": null
				}
			}`, depositTs, withdrawTs)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create client with mock server URL
	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account-pubkey",
	}

	// Use a recent since time (within 31 days) to avoid historical month fetching
	since := time.Now().AddDate(0, 0, -30)
	transfers, _, err := client.FetchDeposits(context.Background(), account, since)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(transfers) != 2 {
		t.Errorf("Expected 2 transfers, got %d", len(transfers))
	}

	// Verify first transfer (deposit)
	if transfers[0].Type != models.TypeDeposit {
		t.Errorf("Expected type '%s', got '%s'", models.TypeDeposit, transfers[0].Type)
	}
	if transfers[0].Amount != "100" {
		t.Errorf("Expected amount '100', got '%s'", transfers[0].Amount)
	}

	// Verify second transfer (withdrawal)
	if transfers[1].Type != models.TypeWithdraw {
		t.Errorf("Expected type '%s', got '%s'", models.TypeWithdraw, transfers[1].Type)
	}
	if transfers[1].Amount != "50" {
		t.Errorf("Expected amount '50', got '%s'", transfers[1].Amount)
	}
}

func TestDriftClient_FetchDeposits_InvalidAccountID(t *testing.T) {
	client := NewClient()

	account := &models.ExchangeAccount{
		ID:                "invalid-uuid",
		AccountIdentifier: "test-account",
	}

	_, _, err := client.FetchDeposits(context.Background(), account, time.Time{})
	if err == nil {
		t.Error("Expected error for invalid account ID")
	}
}

func TestDriftClient_FetchDeposits_EmptyAccountIdentifier(t *testing.T) {
	client := NewClient()

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "",
	}

	_, _, err := client.FetchDeposits(context.Background(), account, time.Time{})
	if err == nil {
		t.Error("Expected error for empty account identifier")
	}
}

func TestDriftClient_FetchDeposits_FiltersBySince(t *testing.T) {
	// Use timestamps within the last 31 days to test filtering without triggering historical fetch
	now := time.Now()
	oldTimestamp := now.Add(-20 * 24 * time.Hour).Unix()  // 20 days ago
	newTimestamp := now.Add(-10 * 24 * time.Hour).Unix()  // 10 days ago
	sinceTimestamp := now.Add(-15 * 24 * time.Hour).Unix() // 15 days ago (filters out old)

	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/spotMarkets") {
			response := `{
				"success": true,
				"markets": [
					{"marketIndex": 0, "symbol": "USDC", "baseAsset": "USDC", "quoteAsset": "USD"}
				]
			}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
			return
		}

		if strings.Contains(r.URL.Path, "/deposits") {
			// Return deposits with different timestamps
			response := fmt.Sprintf(`{
				"success": true,
				"records": [
					{
						"ts": %d,
						"txSig": "old-tx",
						"slot": 12345,
						"amount": "100.000000",
						"marketIndex": 0,
						"depositRecordId": "old-deposit",
						"direction": "deposit",
						"oraclePrice": "1.000000",
						"user": "test-user"
					},
					{
						"ts": %d,
						"txSig": "new-tx",
						"slot": 12346,
						"amount": "200.000000",
						"marketIndex": 0,
						"depositRecordId": "new-deposit",
						"direction": "deposit",
						"oraclePrice": "1.000000",
						"user": "test-user"
					}
				],
				"meta": {"nextPage": null}
			}`, oldTimestamp, newTimestamp)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account-pubkey",
	}

	// Filter to only get deposits after sinceTimestamp
	since := time.Unix(sinceTimestamp, 0)
	transfers, _, err := client.FetchDeposits(context.Background(), account, since)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should only get the newer deposit
	if len(transfers) != 1 {
		t.Errorf("Expected 1 transfer after filtering, got %d", len(transfers))
	}
	if len(transfers) > 0 && transfers[0].Amount != "200" {
		t.Errorf("Expected amount '200', got '%s'", transfers[0].Amount)
	}
}

func TestDriftClient_FetchDeposits_ContextCancellation(t *testing.T) {
	client := NewClient()

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, err := client.FetchDeposits(ctx, account, time.Time{})
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

// TestDriftClient_HistoricalFetchFiltersOverlap verifies that historical fetching
// correctly filters out records that would overlap with the recent API window.
func TestDriftClient_HistoricalFetchFiltersOverlap(t *testing.T) {
	// Setup: Create timestamps that span the historical/recent boundary
	now := time.Now()
	thirtyOneDaysAgo := now.AddDate(0, 0, -31)

	// Records in the "overlap zone" (within the last 31 days but in the same month as thirtyOneDaysAgo)
	overlappingTs := thirtyOneDaysAgo.Add(12 * time.Hour).Unix() // Just after the boundary
	historicalTs := thirtyOneDaysAgo.Add(-48 * time.Hour).Unix() // Before the boundary (should be kept)
	recentTs := now.Add(-5 * 24 * time.Hour).Unix()              // Clearly in recent window

	var historicalCalled, recentCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Handle spot markets
		if strings.Contains(r.URL.Path, "/spotMarkets") {
			w.Write([]byte(`{"success": true, "markets": [{"marketIndex": 0, "symbol": "USDC", "baseAsset": "USDC", "quoteAsset": "USD"}]}`))
			return
		}

		// Check if this is a historical request (has year/month in path)
		// Historical paths look like: /user/id/deposits/2026/1
		// Recent paths look like: /user/id/deposits
		path := r.URL.Path
		isHistorical := false
		// Count path segments after /deposits
		if idx := strings.Index(path, "/deposits"); idx != -1 {
			afterDeposits := path[idx+len("/deposits"):]
			// If there's more path after /deposits, it's historical (year/month)
			if len(afterDeposits) > 0 && afterDeposits[0] == '/' {
				isHistorical = true
			}
		}

		if strings.Contains(r.URL.Path, "/deposits") {
			if isHistorical {
				historicalCalled = true
				// Return records that include some in the overlap zone
				response := fmt.Sprintf(`{
					"success": true,
					"records": [
						{"ts": %d, "txSig": "hist-tx", "slot": 1, "amount": "100", "marketIndex": 0, "depositRecordId": "hist-deposit", "direction": "deposit", "oraclePrice": "1", "user": "u"},
						{"ts": %d, "txSig": "overlap-tx", "slot": 2, "amount": "200", "marketIndex": 0, "depositRecordId": "overlap-deposit", "direction": "deposit", "oraclePrice": "1", "user": "u"}
					],
					"meta": {"nextPage": null}
				}`, historicalTs, overlappingTs)
				w.Write([]byte(response))
			} else {
				recentCalled = true
				// Recent endpoint returns only recent records
				response := fmt.Sprintf(`{
					"success": true,
					"records": [
						{"ts": %d, "txSig": "recent-tx", "slot": 3, "amount": "300", "marketIndex": 0, "depositRecordId": "recent-deposit", "direction": "deposit", "oraclePrice": "1", "user": "u"}
					],
					"meta": {"nextPage": null}
				}`, recentTs)
				w.Write([]byte(response))
			}
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	// Request data from before the 31-day window to trigger historical fetch
	since := thirtyOneDaysAgo.Add(-72 * time.Hour) // 3 days before the 31-day window
	deposits, _, err := client.FetchDeposits(context.Background(), account, since)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify both endpoints were called
	if !historicalCalled {
		t.Error("Expected historical endpoint to be called")
	}
	if !recentCalled {
		t.Error("Expected recent endpoint to be called")
	}

	// Should have exactly 2 transfers: historical (before overlap) and recent
	// The overlapping deposit should be filtered out by the historical fetch
	if len(deposits) != 2 {
		t.Errorf("Expected 2 transfers (no overlap), got %d", len(deposits))
		for _, d := range deposits {
			t.Logf("Transfer: %s at %v", d.Asset, d.Timestamp)
		}
	}

	// Verify we have the correct transfers by checking timestamps
	// Historical should be at historicalTs, recent at recentTs, overlap filtered out
	historicalTime := time.Unix(historicalTs, 0).UTC()
	recentTime := time.Unix(recentTs, 0).UTC()

	hasHistorical := false
	hasRecent := false
	hasOverlap := false
	overlappingTime := time.Unix(overlappingTs, 0).UTC()
	for _, d := range deposits {
		if d.Timestamp.Equal(historicalTime) {
			hasHistorical = true
		}
		if d.Timestamp.Equal(recentTime) {
			hasRecent = true
		}
		if d.Timestamp.Equal(overlappingTime) {
			hasOverlap = true
		}
	}

	if !hasHistorical {
		t.Error("Expected historical transfer to be included")
	}
	if !hasRecent {
		t.Error("Expected recent transfer to be included")
	}
	if hasOverlap {
		t.Error("Overlapping transfer should have been filtered out")
	}
}

func TestDriftClient_FetchTrades_OraclePrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/swaps") {
			json.NewEncoder(w).Encode(driftSwapsResponse{
				Success: true,
				Records: []driftSwapRecord{},
				Meta:    driftMeta{NextPage: nil},
			})
			return
		}

		response := driftTradesResponse{
			Success: true,
			Records: []driftTradeRecord{
				{
					Ts:                     time.Now().Unix() - 10,
					FillRecordID:           "fill-with-oracle",
					BaseAssetAmountFilled:  "1.000000000",
					QuoteAssetAmountFilled: "50.000000",
					TakerFee:               "0.050000",
					TakerOrderDirection:    "long",
					Taker:                  "test-account",
					User:                   "test-account",
					Symbol:                 "SOL-PERP",
					MarketType:             "perp",
					OraclePrice:            "50.123456",
				},
				{
					Ts:                     time.Now().Unix() - 5,
					FillRecordID:           "fill-no-oracle",
					BaseAssetAmountFilled:  "2.000000000",
					QuoteAssetAmountFilled: "100.000000",
					TakerFee:               "0.100000",
					TakerOrderDirection:    "short",
					Taker:                  "test-account",
					User:                   "test-account",
					Symbol:                 "BTC-PERP",
					MarketType:             "perp",
					OraclePrice:            "", // Empty oracle price
				},
				{
					Ts:                     time.Now().Unix() - 3,
					FillRecordID:           "fill-zero-oracle",
					BaseAssetAmountFilled:  "0.500000000",
					QuoteAssetAmountFilled: "25.000000",
					TakerFee:               "0.025000",
					TakerOrderDirection:    "long",
					Taker:                  "test-account",
					User:                   "test-account",
					Symbol:                 "ETH-PERP",
					MarketType:             "perp",
					OraclePrice:            "0", // Zero oracle price
				},
			},
			Meta: driftMeta{NextPage: nil},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	ctx := context.Background()
	since := time.Now().AddDate(0, 0, -30)
	trades, prices, err := client.FetchTrades(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	if len(trades) != 3 {
		t.Fatalf("Expected 3 trades, got %d", len(trades))
	}

	// Only one trade has a valid oracle price
	if len(prices) != 1 {
		t.Fatalf("Expected 1 oracle price (empty and zero should be skipped), got %d", len(prices))
	}

	p := prices[0]
	if p.Asset != "SOL" {
		t.Errorf("Expected oracle price asset 'SOL', got '%s'", p.Asset)
	}
	if p.Denomination != "USDC" {
		t.Errorf("Expected oracle price denomination 'USDC', got '%s'", p.Denomination)
	}
	if p.Price != "50.123456" {
		t.Errorf("Expected oracle price '50.123456', got '%s'", p.Price)
	}
	if p.Source != "oracle" {
		t.Errorf("Expected source 'oracle', got '%s'", p.Source)
	}
	if p.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp on oracle price")
	}
}

func TestTransformSwap_OraclePrices(t *testing.T) {
	client := NewClient()
	accountUUID := uuid.New()

	t.Run("swap with oracle prices", func(t *testing.T) {
		record := driftSwapRecord{
			Ts:             1700000000,
			TxSig:          "test-tx",
			TxSigIndex:     0,
			OutSymbol:      "SOL",
			InSymbol:       "USDC",
			AmountOut:      "2.500000",
			AmountIn:       "200.000000",
			OutOraclePrice: "80.000000",
			InOraclePrice:  "1.000000",
			Fee:            "0.200000",
		}

		_, prices := client.transformSwap(record, accountUUID)
		if len(prices) != 2 {
			t.Fatalf("Expected 2 oracle prices, got %d", len(prices))
		}

		// Both prices should use the oracleDenomination constant
		for _, p := range prices {
			if p.Denomination != oracleDenomination {
				t.Errorf("Expected denomination '%s', got '%s'", oracleDenomination, p.Denomination)
			}
			if p.Source != "oracle" {
				t.Errorf("Expected source 'oracle', got '%s'", p.Source)
			}
		}
	})

	t.Run("swap with empty oracle prices", func(t *testing.T) {
		record := driftSwapRecord{
			Ts:             1700000000,
			TxSig:          "test-tx-2",
			TxSigIndex:     0,
			OutSymbol:      "SOL",
			InSymbol:       "USDC",
			AmountOut:      "1.000000",
			AmountIn:       "100.000000",
			OutOraclePrice: "",
			InOraclePrice:  "",
			Fee:            "0.100000",
		}

		_, prices := client.transformSwap(record, accountUUID)
		if len(prices) != 0 {
			t.Errorf("Expected 0 oracle prices for empty values, got %d", len(prices))
		}
	})

	t.Run("swap with zero oracle prices", func(t *testing.T) {
		record := driftSwapRecord{
			Ts:             1700000000,
			TxSig:          "test-tx-3",
			TxSigIndex:     0,
			OutSymbol:      "SOL",
			InSymbol:       "USDC",
			AmountOut:      "1.000000",
			AmountIn:       "100.000000",
			OutOraclePrice: "0",
			InOraclePrice:  "0",
			Fee:            "0.100000",
		}

		_, prices := client.transformSwap(record, accountUUID)
		if len(prices) != 0 {
			t.Errorf("Expected 0 oracle prices for zero values, got %d", len(prices))
		}
	})

	t.Run("swap with partial oracle prices", func(t *testing.T) {
		record := driftSwapRecord{
			Ts:             1700000000,
			TxSig:          "test-tx-4",
			TxSigIndex:     0,
			OutSymbol:      "SOL",
			InSymbol:       "USDC",
			AmountOut:      "1.000000",
			AmountIn:       "100.000000",
			OutOraclePrice: "80.000000",
			InOraclePrice:  "", // Missing
			Fee:            "0.100000",
		}

		_, prices := client.transformSwap(record, accountUUID)
		if len(prices) != 1 {
			t.Fatalf("Expected 1 oracle price, got %d", len(prices))
		}
		if prices[0].Asset != "SOL" {
			t.Errorf("Expected asset 'SOL', got '%s'", prices[0].Asset)
		}
	})
}

func TestDriftClient_FetchDeposits_OraclePrices(t *testing.T) {
	now := time.Now()
	depositTs := now.Add(-10 * 24 * time.Hour).Unix()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/stats/markets") {
			response := `{
				"success": true,
				"markets": [
					{"marketIndex": 1, "symbol": "SOL", "baseAsset": "SOL", "quoteAsset": "USDC", "marketType": "spot"}
				]
			}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
			return
		}

		if strings.Contains(r.URL.Path, "/deposits") {
			response := fmt.Sprintf(`{
				"success": true,
				"records": [
					{
						"ts": %d,
						"txSig": "test-tx-1",
						"slot": 12345,
						"amount": "10.000000",
						"marketIndex": 1,
						"depositRecordId": "deposit-1",
						"direction": "deposit",
						"oraclePrice": "145.678900",
						"user": "test-user"
					}
				],
				"meta": {"nextPage": null}
			}`, depositTs)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		marketCache: newMarketCache(1 * time.Hour),
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "test-account",
	}

	since := time.Now().AddDate(0, 0, -30)
	transfers, prices, err := client.FetchDeposits(context.Background(), account, since)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(transfers) != 1 {
		t.Fatalf("Expected 1 transfer, got %d", len(transfers))
	}

	if len(prices) != 1 {
		t.Fatalf("Expected 1 oracle price, got %d", len(prices))
	}

	p := prices[0]
	if p.Asset != "SOL" {
		t.Errorf("Expected asset 'SOL', got '%s'", p.Asset)
	}
	if p.Denomination != "USDC" {
		t.Errorf("Expected denomination 'USDC', got '%s'", p.Denomination)
	}
	if p.Price != "145.678900" {
		t.Errorf("Expected price '145.678900', got '%s'", p.Price)
	}
	if p.Source != "oracle" {
		t.Errorf("Expected source 'oracle', got '%s'", p.Source)
	}
}
