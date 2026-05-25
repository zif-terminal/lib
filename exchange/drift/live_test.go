package drift

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

	assetMap := map[string]string{}
	for _, b := range balances {
		assetMap[b.Asset] = b.Balance
	}
	if assetMap["USDC"] != "5000" {
		t.Errorf("Expected USDC=5000, got %s", assetMap["USDC"])
	}
	if assetMap["SOL"] != "100" {
		t.Errorf("Expected SOL=100, got %s", assetMap["SOL"])
	}
	if len(assetMap["JUP"]) == 0 || assetMap["JUP"][0] != '-' {
		t.Errorf("Expected negative JUP balance (borrow), got %s", assetMap["JUP"])
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

// TestDriftClient_FetchBalances_PopulatesPrices verifies that FetchBalances
// attaches OraclePrice + UsdValue to each non-stablecoin balance row using
// the latest candle's oracleClose. Regression test for the bug where every
// Drift snapshot row was being persisted with NULL oracle_price/usd_value,
// causing the dashboard Total Balance to under-count Drift accounts by the
// full sum of non-USDC holdings.
func TestDriftClient_FetchBalances_PopulatesPrices(t *testing.T) {
	// Map of perp symbol -> oracleClose to return from the mocked candles
	// endpoint. SOL gets a real-ish price, DRIFT gets a small number,
	// wBTC routes through BTC-PERP (the spotAssetToPerpSymbol mapping
	// asserts that wrapped tokens resolve to their underlying perp).
	candleOracle := map[string]float64{
		"SOL-PERP":   125.5,
		"DRIFT-PERP": 0.42,
		"BTC-PERP":   95000.0,
	}

	client, server := newTestDriftClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/user/test-account" {
			_ = json.NewEncoder(w).Encode(driftUserResponse{
				Balances: []driftUserBalance{
					{Symbol: "USDC", MarketIndex: 0, Balance: "1000"},
					{Symbol: "SOL", MarketIndex: 1, Balance: "2"},
					{Symbol: "DRIFT", MarketIndex: 15, Balance: "100"},
					{Symbol: "wBTC", MarketIndex: 3, Balance: "0.5"},
					// Asset with no candle data -> nil price, row still emitted.
					{Symbol: "JLP", MarketIndex: 19, Balance: "10"},
				},
			})
			return
		}

		// Match candle URLs like /market/SOL-PERP/candles/D
		if strings.HasPrefix(r.URL.Path, "/market/") && strings.Contains(r.URL.Path, "/candles/") {
			parts := strings.Split(r.URL.Path, "/")
			// parts: ["", "market", "SOL-PERP", "candles", "D"]
			if len(parts) < 5 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			sym := parts[2]
			price, ok := candleOracle[sym]
			if !ok {
				_ = json.NewEncoder(w).Encode(driftCandlesResponse{Success: true, Records: nil})
				return
			}
			_ = json.NewEncoder(w).Encode(driftCandlesResponse{
				Success: true,
				Records: []driftCandleRecord{
					{Ts: 1700000000, OracleOpen: price, OracleHigh: price, OracleClose: price, OracleLow: price},
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
	if len(balances) != 5 {
		t.Fatalf("Expected 5 balances, got %d", len(balances))
	}

	byAsset := map[string]*models.BalanceSnapshot{}
	for _, b := range balances {
		byAsset[b.Asset] = b
	}

	// USDC is a stablecoin -> price = "1", usd_value = balance.
	usdc := byAsset["USDC"]
	if usdc == nil {
		t.Fatal("Expected USDC balance")
	}
	if usdc.OraclePrice == nil || *usdc.OraclePrice != "1" {
		t.Errorf("USDC: expected OraclePrice=\"1\", got %v", usdc.OraclePrice)
	}
	if usdc.UsdValue == nil || *usdc.UsdValue != "1000" {
		t.Errorf("USDC: expected UsdValue=\"1000\", got %v", usdc.UsdValue)
	}

	// SOL @ 125.5 with balance 2 -> usd_value 251.
	sol := byAsset["SOL"]
	if sol == nil {
		t.Fatal("Expected SOL balance")
	}
	if sol.OraclePrice == nil || *sol.OraclePrice != "125.5" {
		t.Errorf("SOL: expected OraclePrice=\"125.5\", got %v", sol.OraclePrice)
	}
	if sol.UsdValue == nil || *sol.UsdValue != "251" {
		t.Errorf("SOL: expected UsdValue=\"251\", got %v", sol.UsdValue)
	}

	// DRIFT @ 0.42 with balance 100 -> usd_value 42.
	drift := byAsset["DRIFT"]
	if drift == nil {
		t.Fatal("Expected DRIFT balance")
	}
	if drift.OraclePrice == nil || *drift.OraclePrice != "0.42" {
		t.Errorf("DRIFT: expected OraclePrice=\"0.42\", got %v", drift.OraclePrice)
	}
	if drift.UsdValue == nil || *drift.UsdValue != "42" {
		t.Errorf("DRIFT: expected UsdValue=\"42\", got %v", drift.UsdValue)
	}

	// wBTC must route through BTC-PERP -- @ 95000 with balance 0.5 -> 47500.
	wbtc := byAsset["wBTC"]
	if wbtc == nil {
		t.Fatal("Expected wBTC balance")
	}
	if wbtc.OraclePrice == nil || *wbtc.OraclePrice != "95000" {
		t.Errorf("wBTC: expected OraclePrice=\"95000\" (via BTC-PERP mapping), got %v", wbtc.OraclePrice)
	}
	if wbtc.UsdValue == nil || *wbtc.UsdValue != "47500" {
		t.Errorf("wBTC: expected UsdValue=\"47500\", got %v", wbtc.UsdValue)
	}

	// JLP has no candle data -> price is nil, but the row IS emitted with
	// the balance preserved. This matches the HL behavior for exotic
	// tokens (better to lose USD-coverage on one asset than to drop the
	// balance fact entirely).
	jlp := byAsset["JLP"]
	if jlp == nil {
		t.Fatal("Expected JLP balance to be emitted even without a price")
	}
	if jlp.OraclePrice != nil {
		t.Errorf("JLP: expected nil OraclePrice for missing candle, got %v", *jlp.OraclePrice)
	}
	if jlp.UsdValue != nil {
		t.Errorf("JLP: expected nil UsdValue for missing candle, got %v", *jlp.UsdValue)
	}
}

// TestSpotAssetToPerpSymbol pins the spot-asset -> perp-symbol mapping so
// it can't drift silently. If a new wrapped token shows up in production
// the table here will need updating in lockstep.
func TestSpotAssetToPerpSymbol(t *testing.T) {
	cases := []struct {
		asset string
		want  string
	}{
		{"SOL", "SOL-PERP"},
		{"DRIFT", "DRIFT-PERP"},
		{"JUP", "JUP-PERP"},
		{"FARTCOIN", "FARTCOIN-PERP"},
		{"TRUMP", "TRUMP-PERP"},
		{"wBTC", "BTC-PERP"},
		{"cbBTC", "BTC-PERP"},
		{"zBTC", "BTC-PERP"},
		{"LBTC", "BTC-PERP"},
		{"wETH", "ETH-PERP"},
		{"mSOL", "SOL-PERP"},
		{"jitoSOL", "SOL-PERP"},
		{"bSOL", "SOL-PERP"},
		{"dSOL", "SOL-PERP"},
		{"JTO-2", "JTO-PERP"},
		{"JTO-3", "JTO-PERP"},
	}
	for _, c := range cases {
		got := spotAssetToPerpSymbol(c.asset)
		if got != c.want {
			t.Errorf("spotAssetToPerpSymbol(%q) = %q, want %q", c.asset, got, c.want)
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
