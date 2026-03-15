package lighter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)
	assert.Equal(t, defaultBaseURL, client.baseURL)
	assert.NotNil(t, client.httpClient)
	assert.NotNil(t, client.marketCache)
}

func TestClient_Name(t *testing.T) {
	client := NewClient()
	assert.Equal(t, "lighter", client.Name())
}

func TestClient_FetchTrades_Success(t *testing.T) {
	// Setup mock server
	marketsServed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check auth header
		auth := r.Header.Get("Authorization")
		assert.Equal(t, "ro:123:all:9999999999:abc123", auth)

		switch r.URL.Path {
		case "/api/v1/orderBookDetails":
			marketsServed = true
			json.NewEncoder(w).Encode(lighterMarketsResponse{
				Code: 200,
				OrderBookDetails: []lighterMarket{
					{MarketID: 1, Symbol: "ETH", MarketType: "perp"},
					{MarketID: 2, Symbol: "BTC", MarketType: "perp"},
				},
			})
		case "/api/v1/trades":
			assert.True(t, marketsServed, "markets should be fetched first")
			assert.Equal(t, "123", r.URL.Query().Get("account_index"))
			assert.Equal(t, "timestamp", r.URL.Query().Get("sort_by"))
			json.NewEncoder(w).Encode(lighterTradesResponse{
				Code: 200,
				Trades: []lighterTrade{
					{
						TradeID:      1001,
						MarketID:     1,
						Size:         "1.5",
						Price:        "3000",
						USDAmount:    "4500",
						AskID:        200,
						BidID:        100,
						AskAccountID: 999,  // other account
						BidAccountID: 123,  // our account is buyer
						IsMakerAsk:   true,
						Timestamp:    1700000000000, // Unix milliseconds
						TakerFee:     200,           // 0.02%
					},
					{
						TradeID:      1002,
						MarketID:     2,
						Size:         "0.1",
						Price:        "35000",
						USDAmount:    "3500",
						AskID:        300,
						BidID:        400,
						AskAccountID: 123,  // our account is seller
						BidAccountID: 888,
						IsMakerAsk:   false,
						Timestamp:    1700000100000,
						TakerFee:     200,
					},
				},
				NextCursor: "",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{
		"auth_token": "ro:123:all:9999999999:abc123",
		"l1_address": "0xabc123",
	})

	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	trades, err := client.FetchTrades(context.Background(), account, time.Time{})
	require.NoError(t, err)
	assert.Len(t, trades, 2)

	// Verify first trade (our account was buyer)
	assert.Equal(t, "1001", trades[0].TradeID)
	assert.Equal(t, "ETH", trades[0].BaseAsset)
	assert.Equal(t, "USDC", trades[0].QuoteAsset)
	assert.Equal(t, "buy", trades[0].Side)
	assert.Equal(t, "3000", trades[0].Price)
	assert.Equal(t, "1.5", trades[0].Quantity)
	assert.Equal(t, "100", trades[0].OrderID) // bid_id since we were buyer

	// Verify second trade (our account was seller)
	assert.Equal(t, "1002", trades[1].TradeID)
	assert.Equal(t, "BTC", trades[1].BaseAsset)
	assert.Equal(t, "sell", trades[1].Side)
	assert.Equal(t, "35000", trades[1].Price)
	assert.Equal(t, "300", trades[1].OrderID) // ask_id since we were seller
}

func TestClient_FetchTrades_WithPagination(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orderBookDetails":
			json.NewEncoder(w).Encode(lighterMarketsResponse{
				Code:             200,
				OrderBookDetails: []lighterMarket{{MarketID: 1, Symbol: "ETH"}},
			})
		case "/api/v1/trades":
			cursor := r.URL.Query().Get("cursor")
			if page == 0 && cursor == "" {
				page++
				json.NewEncoder(w).Encode(lighterTradesResponse{
					Code:       200,
					Trades:     []lighterTrade{{TradeID: 1001, MarketID: 1, Size: "1", Price: "3000", BidAccountID: 123, AskAccountID: 999, Timestamp: 1700000000000}},
					NextCursor: "page2",
				})
			} else if cursor == "page2" {
				json.NewEncoder(w).Encode(lighterTradesResponse{
					Code:       200,
					Trades:     []lighterTrade{{TradeID: 1002, MarketID: 1, Size: "2", Price: "3000", BidAccountID: 123, AskAccountID: 999, Timestamp: 1700000100000}},
					NextCursor: "",
				})
			}
		}
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{"auth_token": "ro:123:all:9999999999:abc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	trades, err := client.FetchTrades(context.Background(), account, time.Time{})
	require.NoError(t, err)
	assert.Len(t, trades, 2)
}

func TestClient_FetchTrades_SinceFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orderBookDetails":
			json.NewEncoder(w).Encode(lighterMarketsResponse{
				Code:             200,
				OrderBookDetails: []lighterMarket{{MarketID: 1, Symbol: "ETH"}},
			})
		case "/api/v1/trades":
			// Return trades with different timestamps (milliseconds)
			json.NewEncoder(w).Encode(lighterTradesResponse{
				Code: 200,
				Trades: []lighterTrade{
					{TradeID: 1001, MarketID: 1, Size: "1", Price: "3000", BidAccountID: 123, AskAccountID: 999, Timestamp: 1700000000000},
					{TradeID: 1002, MarketID: 1, Size: "1", Price: "3000", BidAccountID: 123, AskAccountID: 999, Timestamp: 1700100000000},
				},
			})
		}
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{"auth_token": "ro:123:all:9999999999:abc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	// Set since to filter out the old trade
	since := time.Unix(1700050000, 0) // Between the two timestamps

	trades, err := client.FetchTrades(context.Background(), account, since)
	require.NoError(t, err)
	assert.Len(t, trades, 1)
	assert.Equal(t, "1002", trades[0].TradeID)
}

func TestClient_FetchTrades_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orderBookDetails":
			json.NewEncoder(w).Encode(lighterMarketsResponse{Code: 200, OrderBookDetails: []lighterMarket{}})
		case "/api/v1/trades":
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{"auth_token": "ro:123:all:9999999999:abc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	_, err := client.FetchTrades(context.Background(), account, time.Time{})
	require.Error(t, err)

	var rateLimitErr *iface.RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
	assert.Equal(t, "lighter", rateLimitErr.Exchange)
	assert.Equal(t, 30*time.Second, rateLimitErr.RetryAfter)
}

func TestClient_FetchTrades_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay response
		time.Sleep(100 * time.Millisecond)
		json.NewEncoder(w).Encode(lighterOrdersResponse{})
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{"auth_token": "ro:123:all:9999999999:abc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.FetchTrades(ctx, account, time.Time{})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestClient_FetchTrades_MissingAuthToken(t *testing.T) {
	client := NewClient()

	// No auth_token in metadata
	metadata, _ := json.Marshal(map[string]interface{}{"l1_address": "0xabc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	_, err := client.FetchTrades(context.Background(), account, time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_token not found")
}

func TestClient_FetchTrades_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orderBookDetails":
			json.NewEncoder(w).Encode(lighterMarketsResponse{Code: 200, OrderBookDetails: []lighterMarket{}})
		case "/api/v1/trades":
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{"auth_token": "ro:123:all:9999999999:abc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	_, err := client.FetchTrades(context.Background(), account, time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestClient_FetchFundingPayments_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orderBookDetails":
			json.NewEncoder(w).Encode(lighterMarketsResponse{
				Code:             200,
				OrderBookDetails: []lighterMarket{{MarketID: 1, Symbol: "ETH"}},
			})
		case "/api/v1/positionFunding":
			assert.Equal(t, "123", r.URL.Query().Get("account_index"))
			json.NewEncoder(w).Encode(lighterFundingResponse{
				Code: 200,
				PositionFundings: []lighterPositionFunding{
					{
						FundingID:    1001,
						MarketID:     1,
						Change:       "0.5",
						Timestamp:    1700000000,
						PositionSide: "long",
					},
					{
						FundingID:    1002,
						MarketID:     1,
						Change:       "-0.25",
						Timestamp:    1700000100,
						PositionSide: "short",
					},
				},
				NextCursor: "",
			})
		}
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{"auth_token": "ro:123:all:9999999999:abc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	payments, err := client.FetchFundingPayments(context.Background(), account, time.Time{})
	require.NoError(t, err)
	assert.Len(t, payments, 2)

	// Verify all payments have type="funding"
	for _, p := range payments {
		assert.Equal(t, models.TypeFunding, p.Type)
	}

	// Verify first payment (positive - received)
	assert.Equal(t, "1001_1", payments[0].Metadata["payment_id"])
	assert.Equal(t, "ETH", payments[0].Metadata["market"])
	assert.Equal(t, "USDC", payments[0].Asset)
	assert.Equal(t, "0.5", payments[0].Amount)

	// Verify second payment (negative - paid)
	assert.Equal(t, "-0.25", payments[1].Amount)
}

func TestClient_FetchFundingPayments_WithPagination(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orderBookDetails":
			json.NewEncoder(w).Encode(lighterMarketsResponse{
				Code:             200,
				OrderBookDetails: []lighterMarket{{MarketID: 1, Symbol: "ETH"}},
			})
		case "/api/v1/positionFunding":
			cursor := r.URL.Query().Get("cursor")
			if page == 0 && cursor == "" {
				page++
				json.NewEncoder(w).Encode(lighterFundingResponse{
					Code:             200,
					PositionFundings: []lighterPositionFunding{{FundingID: 1, MarketID: 1, Change: "1", Timestamp: 1700000000}},
					NextCursor:       "page2",
				})
			} else if cursor == "page2" {
				json.NewEncoder(w).Encode(lighterFundingResponse{
					Code:             200,
					PositionFundings: []lighterPositionFunding{{FundingID: 2, MarketID: 1, Change: "2", Timestamp: 1700000100}},
					NextCursor:       "",
				})
			}
		}
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{"auth_token": "ro:123:all:9999999999:abc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	payments, err := client.FetchFundingPayments(context.Background(), account, time.Time{})
	require.NoError(t, err)
	assert.Len(t, payments, 2)
}

func TestCalculatePrice(t *testing.T) {
	tests := []struct {
		name        string
		quoteAmount string
		baseAmount  string
		expected    string
	}{
		{"normal price", "4500", "1.5", "3000"},
		{"exact division", "1000", "2", "500"},
		{"zero base", "1000", "0", "0"},
		{"empty base", "1000", "", "0"},
		{"zero quote", "0", "1", "0"},
		{"whole number result", "3000", "1.5", "2000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePrice(tt.quoteAmount, tt.baseAmount)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculatePriceFractional(t *testing.T) {
	// Test fractional division - check prefix since floating point may vary
	result := calculatePrice("1000", "3")
	assert.True(t, len(result) > 10, "Should have significant precision")
	assert.Contains(t, result, "333.333", "Should start with correct fractional value")
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"seconds", "30", 30 * time.Second},
		{"empty", "", 60 * time.Second},
		{"invalid", "abc", 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRetryAfter(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClient_MarketCacheLoading(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orderBookDetails" {
			callCount++
			json.NewEncoder(w).Encode(lighterMarketsResponse{
				Code:             200,
				OrderBookDetails: []lighterMarket{{MarketID: 1, Symbol: "ETH"}},
			})
			return
		}
		json.NewEncoder(w).Encode(lighterOrdersResponse{Code: 200})
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	metadata, _ := json.Marshal(map[string]interface{}{"auth_token": "ro:123:all:9999999999:abc"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "123",
		AccountTypeMetadata: metadata,
	}

	// First call should load market cache
	_, _ = client.FetchTrades(context.Background(), account, time.Time{})
	assert.Equal(t, 1, callCount)

	// Second call should use cached data
	_, _ = client.FetchTrades(context.Background(), account, time.Time{})
	assert.Equal(t, 1, callCount) // Should not increment
}

func TestClient_GetAuthToken(t *testing.T) {
	client := NewClient()

	tests := []struct {
		name        string
		metadata    json.RawMessage
		expectError bool
		expected    string
	}{
		{
			name:        "valid token",
			metadata:    json.RawMessage(`{"auth_token": "ro:123:all:999:abc", "l1_address": "0x123"}`),
			expectError: false,
			expected:    "ro:123:all:999:abc",
		},
		{
			name:        "missing token",
			metadata:    json.RawMessage(`{"l1_address": "0x123"}`),
			expectError: true,
		},
		{
			name:        "empty token",
			metadata:    json.RawMessage(`{"auth_token": "", "l1_address": "0x123"}`),
			expectError: true,
		},
		{
			name:        "nil metadata",
			metadata:    nil,
			expectError: true,
		},
		{
			name:        "invalid json",
			metadata:    json.RawMessage(`{invalid}`),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &models.ExchangeAccount{
				AccountTypeMetadata: tt.metadata,
			}
			token, err := client.getAuthToken(account)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, token)
			}
		})
	}
}
