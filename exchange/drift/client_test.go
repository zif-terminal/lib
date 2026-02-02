package drift

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
				},
			},
			Meta: driftMeta{NextPage: nil},
		}

		w.Header().Set("Content-Type", "application/json")
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
	trades, err := client.FetchTrades(ctx, account, time.Time{})
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
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var response driftTradesResponse
		if callCount == 1 {
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
	// Use a recent since time (within last 31 days) to avoid historical backfilling
	recentSince := time.Now().Add(-7 * 24 * time.Hour)
	trades, err := client.FetchTrades(ctx, account, recentSince)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount)
	}

	if len(trades) != 2 {
		t.Fatalf("Expected 2 trades from pagination, got %d", len(trades))
	}
}

func TestDriftClient_FetchTrades_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
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

	if rateLimitErr.Exchange != "drift" {
		t.Errorf("Expected exchange 'drift', got '%s'", rateLimitErr.Exchange)
	}

	if rateLimitErr.RetryAfter != 60*time.Second {
		t.Errorf("Expected retry after 60s, got %v", rateLimitErr.RetryAfter)
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

	_, err := client.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}
}

func TestDriftClient_FetchTrades_FiltersBySince(t *testing.T) {
	now := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	since := now.Add(-10 * time.Second)
	trades, err := client.FetchTrades(ctx, account, since)
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
	_, err := client.FetchTrades(ctx, account, time.Time{})
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
	_, err := client.FetchTrades(ctx, account, time.Time{})
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
	payments, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err != nil {
		t.Fatalf("FetchFundingPayments failed: %v", err)
	}

	if len(payments) != 2 {
		t.Fatalf("Expected 2 payments, got %d", len(payments))
	}

	// Verify first payment (oldest)
	if payments[0].BaseAsset != "SOL" {
		t.Errorf("Expected base asset 'SOL', got '%s'", payments[0].BaseAsset)
	}
	if payments[0].Amount != "0.1" {
		t.Errorf("Expected amount '0.1', got '%s'", payments[0].Amount)
	}

	// Verify second payment (negative)
	if payments[1].BaseAsset != "BTC" {
		t.Errorf("Expected base asset 'BTC', got '%s'", payments[1].BaseAsset)
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
